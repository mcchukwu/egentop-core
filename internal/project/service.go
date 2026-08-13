package project

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Service struct {
	DB              *sql.DB
	Repo            *Repository
	AuditService    *audit.Service
	ActivityService *activity.Service
}

func NewService(db *sql.DB, repo *Repository, auditService *audit.Service, activityService *activity.Service) *Service {
	return &Service{
		DB:              db,
		Repo:            repo,
		AuditService:    auditService,
		ActivityService: activityService,
	}
}

// CreateProject creates a new project
func (s *Service) Create(ctx context.Context, createdBy uuid.UUID, organizationID uuid.UUID, req CreateProjectRequest) (*Project, error) {
	priority := ProjectPriorityMedium
	var dueDate *time.Time

	if req.Priority != "" {
		switch ProjectPriority(req.Priority) {
		case ProjectPriorityLow, ProjectPriorityMedium, ProjectPriorityHigh:
			priority = ProjectPriority(req.Priority)

		default:
			return nil, apperrors.ErrValidation
		}
	}

	if req.DueDate != nil {
		dueDate = req.DueDate
	}

	project := &Project{
		OrganizationID: organizationID,
		CreatedBy:      createdBy,
		Name:           req.Name,
		Status:         ProjectStatusDraft,
		Priority:       priority,
		DueDate:        dueDate,
	}

	if req.Description != "" {
		project.Description = &req.Description
	}

	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := s.Repo.Create(dbCtx, tx, project); err != nil {
			return err
		}

		err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &createdBy,
			Action:         "project.created",
			EntityType:     "project",
			EntityID:       &project.ID,
			Metadata: map[string]any{
				"project_id": &project.ID,
				"name":       &project.Name,
			},
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(organizationID, createdBy, &project.ID, nil, activity.ActivityProjectCreated, "Project created", map[string]any{
			"project_id": &project.ID,
			"name":       &project.Name,
		})

		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})

	return project, err
}

// ListProjects lists all projects for an organization
func (s *Service) ListByOrganizationID(ctx context.Context, organizationID uuid.UUID, q pagination.Query) ([]Project, pagination.Meta, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	projects, total, err := s.Repo.ListByOrganizationID(dbCtx, organizationID, q)
	if err != nil {
		return nil, pagination.Meta{}, err
	}

	return projects, pagination.NewMeta(q, total), nil
}

// GetProjectByID gets a project by ID, scoped to the actor's organization.
func (s *Service) GetByID(ctx context.Context, organizationID uuid.UUID, projectID uuid.UUID) (*Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.GetProjectByIDAndOrganizationID(dbCtx, projectID, organizationID)
}

// Update updates a project's metadata (name, description, priority, due date
// and/or status). Fields left empty in the request are unchanged.
func (s *Service) Update(ctx context.Context, userID uuid.UUID, organizationID uuid.UUID, projectID uuid.UUID, req UpdateProjectRequest) (*Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var updated *Project

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Verify the project belongs to the actor organization
		project, err := s.ensureProjectAccess(dbCtx, projectID, organizationID)
		if err != nil {
			return err
		}

		oldStatus := project.Status

		var name *string
		var description *string
		var priority *ProjectPriority
		var status *ProjectStatus
		var dueDate *time.Time

		if req.Name != "" {
			name = &req.Name
		}
		if req.Description != "" {
			description = &req.Description
		}
		if req.Priority != "" {
			p := ProjectPriority(req.Priority)
			switch p {
			case ProjectPriorityLow, ProjectPriorityMedium, ProjectPriorityHigh:
				priority = &p
			default:
				return apperrors.ErrInvalidProjectPriority
			}
		}
		if req.Status != "" {
			st := ProjectStatus(req.Status)
			if err := validateProjectStatusTransition(oldStatus, st); err != nil {
				return err
			}
			status = &st
		}
		if req.DueDate != nil {
			dueDate = req.DueDate
		}

		if err := s.Repo.UpdateDetails(dbCtx, tx, projectID, name, description, priority, status, dueDate); err != nil {
			return err
		}

		// Audit log
		metadata := map[string]any{"project_id": projectID}
		if name != nil {
			metadata["new_name"] = *name
		}
		if priority != nil {
			metadata["old_priority"] = project.Priority
			metadata["new_priority"] = *priority
		}
		if status != nil {
			metadata["old_status"] = oldStatus
			metadata["new_status"] = *status
		}
		if dueDate != nil {
			metadata["new_due_date"] = dueDate.String()
		}

		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "project.updated",
			EntityType:     "project",
			EntityID:       &projectID,
			Metadata:       metadata,
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(organizationID, userID, &projectID, nil, activity.ActivityProjectUpdated, "Project updated", map[string]any{
			"project_id": &projectID,
			"name":       &project.Name,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if updated == nil {
		return s.GetByID(ctx, organizationID, projectID)
	}

	return updated, nil
}

// CreateMilestone creates a new milestone
func (s *Service) CreateMilestone(ctx context.Context, organizationID uuid.UUID, projectID uuid.UUID, userID uuid.UUID, input CreateMilestoneInput) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	// Validate input
	var dueDate *time.Time

	if input.DueDate != nil {
		dueDate = input.DueDate
	}

	milestone := &Milestone{
		OrganizationID: organizationID,
		CreatedBy:      userID,
		Title:          input.Title,
		Description:    &input.Description,
		Status:         MilestoneStatusPending,
		DueDate:        dueDate,
	}

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Verify if project belong to actor organization
		project, err := s.ensureProjectAccess(dbCtx, projectID, organizationID)
		if err != nil {
			return err
		}

		milestone.ProjectID = project.ID

		// Create milestone
		err = s.Repo.CreateMilestone(dbCtx, tx, milestone)
		if err != nil {
			return err
		}

		// Audit log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "milestone.created",
			EntityType:     "milestone",
			EntityID:       &milestone.ID,
			Metadata: map[string]any{
				"project_id":   project.ID,
				"milestone_id": milestone.ProjectID,
				"title":        milestone.Title,
			},
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(organizationID, userID, &project.ID, &milestone.ID, activity.ActivityMilestoneCreated, "Milestone created", map[string]any{
			"project_id":   &project.ID,
			"milestone_id": &milestone.ID,
			"title":        &milestone.Title,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})

	return milestone, err
}

// ListMilestones lists all milestones for a project, scoped to the actor's organization.
func (s *Service) ListMilestonesByProjectID(ctx context.Context, organizationID uuid.UUID, projectID uuid.UUID, q pagination.Query) ([]Milestone, pagination.Meta, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	milestones, total, err := s.Repo.ListMilestonesByProjectID(dbCtx, s.DB, projectID, organizationID, q)
	if err != nil {
		return nil, pagination.Meta{}, err
	}

	return milestones, pagination.NewMeta(q, total), nil
}

// GetMilestoneByID gets a milestone by ID, scoped to the actor's organization.
func (s *Service) GetMilestoneByID(ctx context.Context, organizationID uuid.UUID, milestoneID uuid.UUID) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.GetMilestoneByIDAndOrganizationID(dbCtx, milestoneID, organizationID)
}

// Update updates a milestone's metadata (title, description, due date and/or
// position). Fields left empty in the request are unchanged.
func (s *Service) UpdateMilestone(ctx context.Context, orgID uuid.UUID, userID uuid.UUID, milestoneID uuid.UUID, req UpdateMilestoneRequest) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var updated *Milestone

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Verify the milestone belongs to the actor organization
		milestone, err := s.ensureMilestoneAccess(dbCtx, milestoneID, orgID)
		if err != nil {
			return err
		}

		var title *string
		var description *string
		var dueDate *time.Time
		var position *int

		if req.Title != "" {
			title = &req.Title
		}
		if req.Description != "" {
			description = &req.Description
		}
		if req.DueDate != nil {
			dueDate = req.DueDate
		}
		if req.Position != 0 {
			position = &req.Position
		}

		if title == nil && description == nil && dueDate == nil && position == nil {
			updated = milestone
			return nil
		}

		if err := s.Repo.UpdateMilestoneDetails(dbCtx, tx, milestoneID, title, description, dueDate, position); err != nil {
			return err
		}

		// Audit log
		metadata := map[string]any{"milestone_id": milestoneID}
		if title != nil {
			metadata["old_title"] = milestone.Title
			metadata["new_title"] = *title
		}
		if dueDate != nil {
			metadata["new_due_date"] = dueDate.String()
		}

		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "milestone.updated",
			EntityType:     "milestone",
			EntityID:       &milestoneID,
			Metadata:       metadata,
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(orgID, userID, &milestone.ProjectID, &milestoneID, activity.ActivityMilestoneUpdated, "Milestone updated", map[string]any{
			"project_id":   &milestone.ProjectID,
			"milestone_id": &milestoneID,
			"title":        &milestone.Title,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if updated == nil {
		return s.GetMilestoneByID(ctx, orgID, milestoneID)
	}

	return updated, nil
}

// --- Helpers ---

// Eusure project is accessible by the user
func (s *Service) ensureProjectAccess(ctx context.Context, projectID uuid.UUID, organizationID uuid.UUID) (*Project, error) {
	project, err := s.Repo.GetProjectByIDAndOrganizationID(ctx, projectID, organizationID)

	return project, err
}

// Ensure milestone is accessible by the user
func (s *Service) ensureMilestoneAccess(ctx context.Context, milestoneID uuid.UUID, organizationID uuid.UUID) (*Milestone, error) {
	milestone, err := s.Repo.GetMilestoneByIDAndOrganizationID(ctx, milestoneID, organizationID)

	return milestone, err
}

// Validate the transition between project statuses
func validateProjectStatusTransition(current ProjectStatus, next ProjectStatus) error {
	if current == next {
		return nil
	}

	switch current {
	case ProjectStatusDraft:
		if next == ProjectStatusActive || next == ProjectStatusArchived || next == ProjectStatusCancelled {
			return nil
		}
	case ProjectStatusActive:
		if next == ProjectStatusCompleted || next == ProjectStatusArchived || next == ProjectStatusCancelled {
			return nil
		}
	case ProjectStatusCompleted:
		if next == ProjectStatusArchived {
			return nil
		}

	case ProjectStatusArchived:
		return apperrors.ErrInvalidStatusTransition
	case ProjectStatusCancelled:
		return apperrors.ErrInvalidStatusTransition
	}

	return apperrors.ErrInvalidStatusTransition
}

// Validate the transition between milestone statuses
func validateMilestoneStatusTransition(current MilestoneStatus, next MilestoneStatus) error {
	if current == next {
		return nil
	}

	switch current {
	case MilestoneStatusPending:
		if next == MilestoneStatusInProgress || next == MilestoneStatusCancelled || next == MilestoneStatusBlocked {
			return nil
		}
	case MilestoneStatusInProgress:
		if next == MilestoneStatusAwaitingApproval || next == MilestoneStatusCancelled || next == MilestoneStatusBlocked {
			return nil
		}
	case MilestoneStatusAwaitingApproval:
		if next == MilestoneStatusCompleted || next == MilestoneStatusCancelled || next == MilestoneStatusBlocked {
			return nil
		}

	case MilestoneStatusCompleted:
		return apperrors.ErrInvalidStatusTransition
	case MilestoneStatusCancelled:
		return apperrors.ErrInvalidStatusTransition
	case MilestoneStatusBlocked:
		return apperrors.ErrInvalidStatusTransition
	}

	return apperrors.ErrInvalidStatusTransition
}
