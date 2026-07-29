package assignment

import (
	"context"
	"database/sql"

	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
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

func (s *Service) Create(ctx context.Context, orgID string, userID string, projectID string, milestoneID string, req CreateAssignmentRequest) (*Assignment, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	assignment := &Assignment{
		OrganizationID: orgID,
		ProjectID:      &projectID,
		MilestoneID:    &req.MilestoneID,
		AssignedTo:     req.AssignedTo,
		AssignedBy:     userID,
	}

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
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

func (s *Service) GetByID(ctx context.Context, orgID string, assignmentID string) (*Assignment, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	assignment := &Assignment{}

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		err := s.Repo.GetByID(dbCtx, tx, assignmentID, assignment)
		if err != nil {
			return err
		}

		return nil
	})

	return assignment, err
}
