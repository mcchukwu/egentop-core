package activity

import (
	"time"

	"github.com/google/uuid"
)

type Activity struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	ProjectID      *uuid.UUID      `json:"project_id,omitempty"`
	ActorID        *uuid.UUID      `json:"actor_id,omitempty"`
	MilestoneID    *uuid.UUID      `json:"milestone_id,omitempty"`
	Type           string          `json:"type"`
	Message        string          `json:"message"`
	Metadata       map[string]any  `json:"metadata"`
	CreatedAt      time.Time       `json:"created_at"`
}
