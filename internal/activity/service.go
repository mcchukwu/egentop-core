package activity

import (
	"context"
	"database/sql"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

type ActivityService struct {
	Repo *ActivityRepository
}

func NewActivityService(repo *ActivityRepository) *ActivityService {
	return &ActivityService{
		Repo: repo,
	}
}

func (s *ActivityService) Log(ctx context.Context, tx *sql.Tx, entry LogActivityEntry) error {
	if entry.OrganizationID == "" {
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

// NewActivity builds a new activity
func NewActivity(orgID string, actorID string, projectID *string, milestoneID *string, activityType string, message string, metadata map[string]any) LogActivityEntry {
	return LogActivityEntry{
		OrganizationID: orgID,
		ProjectID:      projectID,

		ActorID:     &actorID,
		MilestoneID: milestoneID,

		Type:     activityType,
		Message:  message,
		Metadata: map[string]any{},
	}
}
