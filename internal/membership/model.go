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
	// IsPersonal reports whether the organization is a registration-created
	// personal workspace (no staff members may be added/invited/re-role'd/
	// removed; clients remain allowed). Resolved from the organizations row at
	// read time so the org switcher and member list can render the personal
	// state — it is never silently false.
	IsPersonal bool `json:"is_personal"`
	// MemberName is the member's display name ("{first_name} {last_name}"),
	// resolved by a users join at read time (same pattern as activity
	// actor_name). Nil when the user row does not resolve (defensive —
	// memberships reference users via FK and users are never hard-deleted).
	// user_id is retained; the UI renders the name separately.
	MemberName *string `json:"member_name,omitempty"`
}
