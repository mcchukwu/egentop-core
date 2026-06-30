package activity

import "time"

type Activity struct {
	ID             string
	OrganizationID string
	ProjectID      *string
	ActorID        *string
	MilestoneID    *string
	Type           string
	Message        string
	Metadata       map[string]any
	CreatedAt      time.Time
}
