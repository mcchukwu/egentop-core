package assignment

import (
	"context"
	"database/sql"

	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
)

type AssignmentService struct {
	DB              *sql.DB
	Repo            *AssignmentRepository
	AuditServie     *audit.AuditService
	ActivityService *activity.ActivityService
}

func NewAssignmentService(db *sql.DB, repo *AssignmentRepository, auditService *audit.AuditService, activityService *activity.ActivityService) *AssignmentService {
	return &AssignmentService{
		DB:              db,
		Repo:            repo,
		AuditServie:     auditService,
		ActivityService: activityService,
	}
}

func (s *AssignmentService) Create(ctx context.Context, orgID string, userID string, projectID string, milestoneID string, req CreateAssignmentRequest) (*Assignment, error) {
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
		activity := activity.NewActivity(assignment.OrganizationID, assignment.AssignedBy, assignment.ProjectID, assignment.MilestoneID, activity.ActivityAssignmentCreated, "Assignment created", map[string]any{
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

func (s *AssignmentService) GetByID(ctx context.Context, orgID string, assignmentID string) (*Assignment, error) {
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
