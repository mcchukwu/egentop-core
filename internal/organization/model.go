package organization

import (
	"time"

	"github.com/google/uuid"
)

type OrganizationStatus string

const (
	OrganizationStatusActive    OrganizationStatus = "active"
	OrganizationStatusSuspended OrganizationStatus = "suspended"
	OrganizationStatusDeleted   OrganizationStatus = "deleted"
)

type Organization struct {
	ID        uuid.UUID          `json:"id"`
	Name      string             `json:"name"`
	Slug      string             `json:"slug"`
	Status    OrganizationStatus `json:"status"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}
