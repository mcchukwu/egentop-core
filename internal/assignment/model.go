package assignment

import (
	"time"

	"github.com/google/uuid"
)

type Assignment struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	ProjectID      *uuid.UUID `json:"project_id,omitempty"`
	MilestoneID    *uuid.UUID `json:"milestone_id,omitempty"`
	AssignedTo     uuid.UUID  `json:"assigned_to"`
	AssignedBy     uuid.UUID  `json:"assigned_by"`
	// AssigneeName is the assignee's display name ("{first_name} {last_name}"),
	// resolved by a users join at read time (list/detail). Nil when the user
	// row does not resolve. assigned_to is retained; the UI renders the name
	// separately.
	AssigneeName *string `json:"assignee_name,omitempty"`
	// AssignedByName is the assigning user's display name, resolved by a
	// users join at read time (list/detail). Nil when the user row does not
	// resolve. assigned_by is retained.
	AssignedByName *string   `json:"assigned_by_name,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
