package activity

import "github.com/google/uuid"

type LogActivityEntry struct {
	OrganizationID uuid.UUID
	ActorID        *uuid.UUID
	ProjectID      *uuid.UUID
	MilestoneID    *uuid.UUID
	Type           string
	Message        string
	Metadata       map[string]any
}
