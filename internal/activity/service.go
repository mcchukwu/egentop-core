package activity

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Service struct {
	Repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{
		Repo: repo,
	}
}

// Log logs an activity
func (s *Service) Log(ctx context.Context, tx *sql.Tx, entry LogActivityEntry) error {
	if entry.OrganizationID == uuid.Nil {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.Type == "" {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.Message == "" {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}

	activity := &Activity{
		OrganizationID: entry.OrganizationID,

		ProjectID: entry.ProjectID,

		MilestoneID: entry.MilestoneID,

		ActorID: entry.ActorID,

		Type: entry.Type,

		Message: entry.Message,

		Metadata: entry.Metadata,
	}

	return s.Repo.Create(ctx, tx, activity)
}

// List returns the activity feed for an organization, newest first.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, q pagination.Query) ([]Activity, pagination.Meta, error) {
	activities, total, err := s.Repo.List(ctx, orgID, q)
	if err != nil {
		return nil, pagination.Meta{}, err
	}

	return activities, pagination.NewMeta(q, total), nil
}

// ListByProjectID returns the activity feed scoped to a single project. The
// caller (project service) enforces actor project scope before delegating.
func (s *Service) ListByProjectID(ctx context.Context, orgID uuid.UUID, projectID uuid.UUID, q pagination.Query) ([]Activity, pagination.Meta, error) {
	activities, total, err := s.Repo.ListByProjectID(ctx, orgID, projectID, q)
	if err != nil {
		return nil, pagination.Meta{}, err
	}

	return activities, pagination.NewMeta(q, total), nil
}

// NewActivity builds a new activity. The metadata argument is preserved;
// nil becomes an empty object.
func NewActivity(orgID uuid.UUID, actorID uuid.UUID, projectID *uuid.UUID, milestoneID *uuid.UUID, activityType string, message string, metadata map[string]any) LogActivityEntry {
	if metadata == nil {
		metadata = map[string]any{}
	}

	return LogActivityEntry{
		OrganizationID: orgID,
		ProjectID:      projectID,

		ActorID:     &actorID,
		MilestoneID: milestoneID,

		Type:     activityType,
		Message:  message,
		Metadata: metadata,
	}
}
