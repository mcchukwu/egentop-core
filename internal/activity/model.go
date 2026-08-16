package activity

import (
	"time"

	"github.com/google/uuid"
)

type Activity struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	ProjectID      *uuid.UUID `json:"project_id,omitempty"`
	ActorID        *uuid.UUID `json:"actor_id,omitempty"`
	// ActorName is the actor's display name ("{first_name} {last_name}"),
	// resolved by a users join at read time. Nil when the actor is unknown or
	// the row predates the enrichment. actor_id is retained (backward
	// compatible); the UI renders the name separately from the message.
	ActorName   *string        `json:"actor_name,omitempty"`
	MilestoneID *uuid.UUID     `json:"milestone_id,omitempty"`
	Type        string         `json:"type"`
	Message     string         `json:"message"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
}
