package project

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
)

type ProjectService struct {
	DB              *sql.DB
	Repo            *ProjectRepository
	AuditService    *audit.AuditService
	ActivityService *activity.ActivityService
}

func NewProjectService(db *sql.DB, repo *ProjectRepository, auditService *audit.AuditService, activityService *activity.ActivityService) *ProjectService {
	return &ProjectService{
		DB:              db,
		Repo:            repo,
		AuditService:    auditService,
		ActivityService: activityService,
	}
}

// CreateProject creates a new project
func (s *ProjectService) Create(ctx context.Context, createdBy string, organizationID string, req CreateProjectRequest) (*Project, error) {
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
		parsed, err := time.Parse(time.RFC3339, req.DueDate.String())
		if err != nil {
			return nil, apperrors.ErrValidation
		}

		dueDate = &parsed
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
	if err != nil {
		return nil, err
	}

	return project, nil
}

// ListProjects lists all projects for an organization
func (s *ProjectService) ListByOrganizationID(ctx context.Context, organizationID string) ([]Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.ListByOrganizationID(dbCtx, organizationID)
}

// GetProjectByID gets a project by ID
func (s *ProjectService) GetByID(ctx context.Context, projectID string) (*Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.GetByID(dbCtx, projectID)
}

// UpdateProjectStatus updates the status of a project
func (s *ProjectService) UpdateStatus(ctx context.Context, userID string, organizationID string, projectID string, status ProjectStatus) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Verify if project belong to actor organization
		project, err := s.ensureProjectAccess(dbCtx, projectID, organizationID)
		if err != nil {
			return err
		}

		// Validate status transition
		err = validateProjectStatusTransition(project.Status, status)
		if err != nil {
			return err
		}

		// Update project status
		err = s.Repo.UpdateStatus(dbCtx, tx, projectID, status)
		if err != nil {
			return err
		}

		// Audit log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &organizationID,
			UserID:         &userID,
			Action:         "project.status_changed",
			Metadata: map[string]any{
				"project_id": project.ID,
				"old_status": project.Status,
				"new_status": status,
			},
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(organizationID, userID, &project.ID, nil, activity.ActivityProjectStatusChanged, fmt.Sprintf("Project '%s' status changed to '%s'", project.Name, status), map[string]any{
			"project_id": &project.ID,
			"name":       &project.Name,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// CreateMilestone creates a new milestone
func (s *ProjectService) CreateMilestone(ctx context.Context, organizationID string, projectID string, userID string, input CreateMilestoneInput) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	// Validate input
	var dueDate *time.Time

	if input.DueDate != nil {
		parsed, err := time.Parse(time.RFC3339, input.DueDate.String())
		if err != nil {
			return nil, apperrors.ErrInvalidDueDate
		}
		dueDate = &parsed
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
	if err != nil {
		return nil, err
	}

	return milestone, nil
}

// ListMilestones lists all milestones for a project
func (s *ProjectService) ListMilestonesByProjectID(ctx context.Context, projectID string) ([]Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.ListMilestonesByProjectID(dbCtx, s.DB, projectID)
}

// GetMilestoneByID gets a milestone by ID
func (s *ProjectService) GetMilestoneByID(ctx context.Context, milestoneID string) (*Milestone, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.GetMilestoneByID(dbCtx, s.DB, milestoneID)
}

// UpdateMilestoneStatus updates the status of a milestone
func (s *ProjectService) UpdateMilestoneStatus(ctx context.Context, orgID string, userID string, milestoneID string, status MilestoneStatus) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Verify if milestone belong to actor organization
		milestone, err := s.ensureMilestoneAccess(dbCtx, milestoneID, orgID)
		if err != nil {
			return err
		}

		// Validate status transition
		err = validateMilestoneStatusTransition(milestone.Status, status)
		if err != nil {
			return err
		}

		// Update milestone status
		err = s.Repo.UpdateMilestoneStatus(dbCtx, tx, milestone.ID, status)
		if err != nil {
			return err
		}

		// Audit log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "milestone.status_changed",
			EntityType:     "milestone",
			EntityID:       &milestoneID,
			Metadata: map[string]any{
				"milestone_id": milestone.ID,
				"old_status":   milestone.Status,
				"new_status":   status,
			},
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(orgID, userID, &milestone.ProjectID, &milestone.ID, activity.ActivityMilestoneStatusChanged, fmt.Sprintf("Milestone '%s' status changed to '%s'", milestone.Title, status), map[string]any{
			"project_id":   &milestone.ProjectID,
			"milestone_id": &milestone.ID,
			"title":        &milestone.Title,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

// --- Helpers ---

// Eusure project is accessible by the user
func (s *ProjectService) ensureProjectAccess(ctx context.Context, projectID string, organizationID string) (*Project, error) {
	project, err := s.Repo.GetProjectByIDAndOrganizationID(ctx, projectID, organizationID)
	if err != nil {
		return nil, err
	}

	return project, nil
}

// Ensure milestone is accessible by the user
func (s *ProjectService) ensureMilestoneAccess(ctx context.Context, milestoneID string, organizationID string) (*Milestone, error) {
	milestone, err := s.Repo.GetMilestoneByIDAndOrganizationID(ctx, milestoneID, organizationID)
	if err != nil {
		return nil, err
	}

	return milestone, nil
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
