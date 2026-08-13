package assignment

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Service struct {
	DB              *sql.DB
	Repo            *Repository
	AuditServie     *audit.Service
	ActivityService *activity.Service
}

func NewService(db *sql.DB, repo *Repository, auditService *audit.Service, activityService *activity.Service) *Service {
	return &Service{
		DB:              db,
		Repo:            repo,
		AuditServie:     auditService,
		ActivityService: activityService,
	}
}

func (s *Service) Create(ctx context.Context, orgID uuid.UUID, userID uuid.UUID, projectID uuid.UUID, milestoneID uuid.UUID, assignedTo uuid.UUID) (*Assignment, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	assignment := &Assignment{
		OrganizationID: orgID,
		ProjectID:      &projectID,
		MilestoneID:    &milestoneID,
		AssignedTo:     assignedTo,
		AssignedBy:     userID,
	}

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := s.Repo.EnsureProjectInOrganization(dbCtx, tx, orgID, projectID); err != nil {
			return err
		}
		if err := s.Repo.EnsureMilestoneInProject(dbCtx, tx, orgID, projectID, milestoneID); err != nil {
			return err
		}
		if err := s.Repo.EnsureActiveMember(dbCtx, tx, orgID, assignedTo); err != nil {
			return err
		}

		err := s.Repo.Create(dbCtx, tx, assignment)
		if err != nil {
			return err
		}

		err = s.AuditServie.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &assignment.OrganizationID,
			UserID:         &assignment.AssignedBy,
			Action:         "assignment.created",
			EntityType:     "assignment",
			EntityID:       &assignment.ID,
			Metadata: map[string]any{
				"project_id":   assignment.ProjectID,
				"milestone_id": assignment.MilestoneID,
				"assigned_to":  assignment.AssignedTo,
				"assigned_by":  assignment.AssignedBy,
			},
		})
		if err != nil {
			return err
		}

		// Log activity
		activity := activity.NewActivity(assignment.OrganizationID, assignment.AssignedBy, assignment.ProjectID, assignment.MilestoneID, activity.ActivityAssignmentCreated, " created", map[string]any{
			"project_id":   assignment.ProjectID,
			"milestone_id": assignment.MilestoneID,
			"assigned_to":  assignment.AssignedTo,
			"assigned_by":  assignment.AssignedBy,
		})
		if err := s.ActivityService.Log(dbCtx, tx, activity); err != nil {
			return err
		}

		return nil
	})

	return assignment, err
}

func (s *Service) GetByID(ctx context.Context, orgID uuid.UUID, projectID uuid.UUID, assignmentID uuid.UUID) (*Assignment, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	assignment := &Assignment{}

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		err := s.Repo.GetByIDAndProjectIDAndOrganizationID(dbCtx, tx, orgID, projectID, assignmentID, assignment)
		if err != nil {
			return err
		}

		return nil
	})

	return assignment, err
}

// ListByProjectID returns all assignments for a project within an organization.
func (s *Service) ListByProjectID(ctx context.Context, orgID uuid.UUID, projectID uuid.UUID, q pagination.Query) ([]Assignment, pagination.Meta, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	assignments, total, err := s.Repo.ListByProjectID(dbCtx, orgID, projectID, q)
	if err != nil {
		return nil, pagination.Meta{}, err
	}

	return assignments, pagination.NewMeta(q, total), nil
}

// Update reassigns an assignment to a new user.
func (s *Service) Update(ctx context.Context, orgID uuid.UUID, userID uuid.UUID, projectID uuid.UUID, assignmentID uuid.UUID, assignedTo uuid.UUID) (*Assignment, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	assignment := &Assignment{}

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := s.Repo.GetByIDAndProjectIDAndOrganizationID(dbCtx, tx, orgID, projectID, assignmentID, assignment); err != nil {
			return err
		}

		if err := s.Repo.EnsureActiveMember(dbCtx, tx, orgID, assignedTo); err != nil {
			return err
		}

		if err := s.Repo.UpdateAssignedTo(dbCtx, tx, orgID, projectID, assignmentID, assignedTo); err != nil {
			return err
		}

		oldAssignedTo := assignment.AssignedTo
		assignment.AssignedTo = assignedTo

		if err := s.AuditServie.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "assignment.updated",
			EntityType:     "assignment",
			EntityID:       &assignmentID,
			Metadata: map[string]any{
				"project_id":      assignment.ProjectID,
				"milestone_id":    assignment.MilestoneID,
				"old_assigned_to": oldAssignedTo,
				"new_assigned_to": assignedTo,
			},
		}); err != nil {
			return err
		}

		activity := activity.NewActivity(orgID, userID, assignment.ProjectID, assignment.MilestoneID, activity.ActivityAssignmentUpdated, "Assignment reassigned", map[string]any{
			"project_id":      assignment.ProjectID,
			"milestone_id":    assignment.MilestoneID,
			"old_assigned_to": oldAssignedTo,
			"new_assigned_to": assignedTo,
		})
		return s.ActivityService.Log(dbCtx, tx, activity)
	})
	if err != nil {
		return nil, err
	}

	return assignment, nil
}

// Delete removes an assignment.
func (s *Service) Delete(ctx context.Context, orgID uuid.UUID, userID uuid.UUID, projectID uuid.UUID, assignmentID uuid.UUID) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		current := &Assignment{}

		if err := s.Repo.GetByIDAndProjectIDAndOrganizationID(dbCtx, tx, orgID, projectID, assignmentID, current); err != nil {
			return err
		}

		if err := s.Repo.Delete(dbCtx, tx, orgID, projectID, assignmentID); err != nil {
			return err
		}

		if err := s.AuditServie.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "assignment.removed",
			EntityType:     "assignment",
			EntityID:       &assignmentID,
			Metadata: map[string]any{
				"project_id":   current.ProjectID,
				"milestone_id": current.MilestoneID,
				"assigned_to":  current.AssignedTo,
			},
		}); err != nil {
			return err
		}

		activity := activity.NewActivity(orgID, userID, current.ProjectID, current.MilestoneID, activity.ActivityAssignmentRemoved, "Assignment removed", map[string]any{
			"project_id":   current.ProjectID,
			"milestone_id": current.MilestoneID,
			"assigned_to":  current.AssignedTo,
		})
		return s.ActivityService.Log(dbCtx, tx, activity)
	})
}
