package assignment

import "time"

type Assignment struct {
	ID string

	OrganizationID string

	ProjectID *string

	MilestoneID *string

	AssignedTo string

	AssignedBy string

	CreatedAt time.Time
}
