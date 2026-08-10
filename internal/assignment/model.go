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
	CreatedAt      time.Time  `json:"created_at"`
}
