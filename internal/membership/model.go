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
	// MemberName is the member's display name ("{first_name} {last_name}"),
	// resolved by a users join at read time (same pattern as activity
	// actor_name). Nil when the user row does not resolve (defensive —
	// memberships reference users via FK and users are never hard-deleted).
	// user_id is retained; the UI renders the name separately.
	MemberName *string `json:"member_name,omitempty"`
}
