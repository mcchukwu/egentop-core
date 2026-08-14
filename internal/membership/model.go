package membership

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
	RoleClient Role = "client"
)

type MembershipStatus string

const (
	MembershipStatusActive    MembershipStatus = "active"
	MembershipStatusInvited   MembershipStatus = "invited"
	MembershipStatusSuspended MembershipStatus = "suspended"
)

type Membership struct {
	ID             uuid.UUID        `json:"id"`
	UserID         uuid.UUID        `json:"user_id"`
	OrganizationID uuid.UUID        `json:"organization_id"`
	RoleID         uuid.UUID        `json:"role_id"`
	Role           Role             `json:"role"`
	Status         MembershipStatus `json:"status"`
	JoinedAt       time.Time        `json:"joined_at"`
}
