package project

import (
	"context"
	"database/sql"
	"time"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/org"
	"github.com/mcchukwu/egentop/pkg/db"
)

type ProjectService struct {
	DB           *sql.DB
	Repo         *ProjectRepository
	AuditService *audit.AuditService
}

func NewProjectService(repo *ProjectRepository) *ProjectService {
	return &ProjectService{Repo: repo}
}

// CreateProject creates a new project
func (s *ProjectService) CreateProject(ctx context.Context, organizationID string, createdBy string, req CreateProjectRequest) (*Project, error) {
	project := &Project{
		OrganizationID: organizationID,
		CreatedBy:      createdBy,
		Name:           req.Name,
		Status:         StatusDraft,
		Priority:       PriorityMedium,
	}

	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if req.Description != "" {
			project.Description = &req.Description
		}

		if req.Priority != "" {
			project.Priority = Priority(req.Priority)
		}

		if req.DueDate != "" {
			parsed, err := time.Parse(time.RFC3339, req.DueDate)
			if err == nil {
				project.DueDate = &parsed
			}
		}

		if err := s.Repo.Create(dbCtx, tx, project); err != nil {
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
func (s *ProjectService) ListProjects(ctx context.Context, organizationID string) ([]Project, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return s.Repo.ListByOrganization(dbCtx, s.DB, organizationID)
}

// UpdateProjectStatus updates the status of a project
func (s *ProjectService) UpdateProjectStatus(ctx context.Context, userID string, organizationID string, actor org.Membership, projectID string, status Status) error {
	if !canManageProjects(actor.Role) {
		return apperrors.ErrForbidden
	}

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

		err = s.Repo.UpdateStatus(dbCtx, tx, projectID, status)
		if err != nil {
			return err
		}

		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &actor.OrganizationID,
			UserID:         &actor.ID,
			Action:         "project.status_changed",
			Metadata: map[string]any{
				"project_id": project.ID,
				"status":     status,
			},
		})
		if err != nil {
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
func (s *ProjectService) CreateMilestone(ctx context.Context, actor org.Membership, input CreateMilestoneInput) (*Milestone, error) {
	if !canManageProjects(actor.Role) {
		return nil, apperrors.ErrForbidden
	}

	milestone := &Milestone{
		ProjectID:      input.ProjectID,
		OrganizationID: actor.OrganizationID,
		Title:          input.Title,
		Description:    input.Description,
		Status:         MilestoneStatusTodo,
		DueDate:        input.DueDate,
		CreatedBy:      actor.UserID,
	}

	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		project, err := s.ensureProjectAccess(dbCtx, input.ProjectID, actor.OrganizationID)
		if err != nil {
			return apperrors.ErrForbidden
		}

		err = s.Repo.CreateMilestone(dbCtx, tx, milestone)
		if err != nil {
			return err
		}

		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &actor.OrganizationID,
			UserID:         &actor.UserID,
			Action:         "milestone.created",
			Metadata: map[string]any{
				"project_id":   project.ID,
				"milestone_id": milestone.ProjectID,
			},
		})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return milestone, nil
}

// UpdateMilestoneStatus updates the status of a milestone
func (s *ProjectService) UpdateMilestoneStatus(ctx context.Context, actor org.Membership, milestoneID string, status MilestoneStatus) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		err := s.Repo.UpdateMilestoneStatus(dbCtx, tx, milestoneID, status)
		if err != nil {
			return err
		}

		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &actor.OrganizationID,
			UserID:         &actor.UserID,
			Action:         "milestone.status_changed",
			Metadata: map[string]any{
				"milestone_id": milestoneID,
				"status":       status,
			},
		})
		if err != nil {
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
	project, err := s.Repo.GetProjectByIDAndOrganization(ctx, s.DB, projectID, organizationID)
	if err != nil {
		return nil, err
	}

	return project, nil
}

// Ensure milestone is accessible by the user
func (s *ProjectService) ensureMilestoneAccess(ctx context.Context, milestoneID string, organizationID string) (*Milestone, error) {
	milestone, err := s.Repo.GetMilestoneByIDAndOrganization(ctx, s.DB, milestoneID, organizationID)
	if err != nil {
		return nil, err
	}

	return milestone, nil
}

// Ensure user role is allowed to manage projects
func canManageProjects(role org.Role) bool {
	switch role {
	case org.RoleOwner, org.RoleAdmin:
		return true
	default:
		return false
	}
}

// Validate the transition between project statuses
func validateProjectStatusTransition(current Status, next Status) error {
	switch current {
	case StatusArchived:
		return apperrors.ErrForbidden

	default:
		return nil
	}
}
