package organization

import "time"

type OrganizationStatus string

const (
	OrganizationStatusActive    OrganizationStatus = "active"
	OrganizationStatusSuspended OrganizationStatus = "suspended"
	OrganizationStatusDeleted   OrganizationStatus = "deleted"
)

type Organization struct {
	ID        string
	Name      string
	Slug      string
	Status    OrganizationStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}
