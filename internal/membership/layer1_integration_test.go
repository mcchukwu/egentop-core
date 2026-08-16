package membership

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

// TestMemberListExcludesClients: client-role memberships must never appear in
// the staff directory, in either the count or the list.
func TestMemberListExcludesClients(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := NewService(db, audit.NewService(db))

	_, orgID := seedOwnerMembership(t, db, "owner-"+uuid.NewString()+"@example.com")
	memberID := seedUser(t, db, "staff-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, memberID, orgID, RoleMember)

	// A staff member and two clients.
	clientA := seedUser(t, db, "client-a-"+uuid.NewString()+"@example.com")
	clientB := seedUser(t, db, "client-b-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, clientA, orgID, RoleClient)
	seedMembershipRole(t, db, clientB, orgID, RoleClient)

	members, meta, err := svc.GetOrgMembers(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("GetOrgMembers: %v", err)
	}

	if meta.Total != 2 {
		t.Fatalf("member list total = %d, want 2 (owner + member; clients excluded)", meta.Total)
	}
	if len(members) != 2 {
		t.Fatalf("member list length = %d, want 2", len(members))
	}
	for _, m := range members {
		if m.UserID == clientA || m.UserID == clientB {
			t.Fatalf("client membership %s leaked into the member list", m.UserID)
		}
	}
}

// TestMemberListEnrichesNames: the member roster payload carries each
// member's display name ("{first_name} {last_name}"), resolved by a users
// join at read time — the frontend renders names instead of raw user IDs.
func TestMemberListEnrichesNames(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := NewService(db, audit.NewService(db))

	ownerID, orgID := seedOwnerMembership(t, db, "names-owner-"+uuid.NewString()+"@example.com")

	// A member with a distinct name proves the join concatenates the actual
	// first/last columns rather than echoing the seed helper defaults.
	var namedID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Chiamaka', 'Okafor')
		RETURNING id
	`, "names-member-"+uuid.NewString()+"@example.com").Scan(&namedID); err != nil {
		t.Fatalf("insert named member: %v", err)
	}
	seedMembershipRole(t, db, namedID, orgID, RoleMember)

	members, meta, err := svc.GetOrgMembers(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("GetOrgMembers: %v", err)
	}
	if meta.Total != 2 {
		t.Fatalf("member list total = %d, want 2", meta.Total)
	}

	names := make(map[uuid.UUID]string, len(members))
	for _, m := range members {
		if m.MemberName == nil {
			t.Fatalf("member %s has nil member_name (expected %q)", m.UserID, "Test User")
		}
		names[m.UserID] = *m.MemberName
	}
	if names[ownerID] != "Test User" {
		t.Fatalf("owner member_name = %q, want %q", names[ownerID], "Test User")
	}
	if names[namedID] != "Chiamaka Okafor" {
		t.Fatalf("named member member_name = %q, want %q", names[namedID], "Chiamaka Okafor")
	}
}

// TestRoleUpdateRejectsClientRole: the client role cannot be granted through
// member.role.update (provisioning is the only path), which also blocks
// escalating client memberships to staff roles.
func TestRoleUpdateRejectsClientRole(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := NewService(db, audit.NewService(db))

	ownerID, orgID := seedOwnerMembership(t, db, "owner-"+uuid.NewString()+"@example.com")
	targetID := seedUser(t, db, "target-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, targetID, orgID, RoleMember)

	err := svc.UpdateOrgMemberRole(ctx, orgID, ownerID, targetID, RoleClient)
	if !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatalf("granting client role via role update error = %v, want ErrForbidden", err)
	}

	// The role is unchanged.
	var role string
	if err := db.QueryRowContext(ctx, `
		SELECT r.name
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.user_id = $1 AND m.organization_id = $2
	`, targetID, orgID).Scan(&role); err != nil {
		t.Fatalf("read role: %v", err)
	}
	if role != "member" {
		t.Fatalf("role = %q, want member (unchanged)", role)
	}

	// ResolveSystemRoleID still resolves the client role for provisioning.
	roleID, err := ResolveSystemRoleID(ctx, db, RoleClient)
	if err != nil {
		t.Fatalf("resolve client role: %v", err)
	}
	if roleID == uuid.Nil {
		t.Fatal("client role id must not be nil")
	}
}

// TestRoleUpdateRejectsExistingClientMembership: a client membership can only
// be created via provision and removed via the unassign flow — re-role'ing a
// client into a staff role is forbidden (403).
func TestRoleUpdateRejectsExistingClientMembership(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := NewService(db, audit.NewService(db))

	ownerID, orgID := seedOwnerMembership(t, db, "owner-"+uuid.NewString()+"@example.com")
	clientID := seedUser(t, db, "client-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, clientID, orgID, RoleClient)

	// Promoting the client membership to a staff role is forbidden.
	err := svc.UpdateOrgMemberRole(ctx, orgID, ownerID, clientID, RoleMember)
	if !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatalf("re-role'ing a client membership error = %v, want ErrForbidden", err)
	}

	// Same for admin/viewer targets.
	for _, r := range []Role{RoleAdmin, RoleViewer, RoleOwner} {
		if err := svc.UpdateOrgMemberRole(ctx, orgID, ownerID, clientID, r); !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatalf("re-role'ing a client to %s error = %v, want ErrForbidden", r, err)
		}
	}

	// The membership is untouched: still the client role.
	var role string
	if err := db.QueryRowContext(ctx, `
		SELECT r.name
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.user_id = $1 AND m.organization_id = $2
	`, clientID, orgID).Scan(&role); err != nil {
		t.Fatalf("read role: %v", err)
	}
	if role != "client" {
		t.Fatalf("role = %q, want client (unchanged)", role)
	}
}

// TestRemoveMemberRejectsClientMembership: client memberships are removed
// exclusively through the project unassign flow; a direct member.remove is a
// 409 client_attached_to_project. Staff memberships still remove normally.
func TestRemoveMemberRejectsClientMembership(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := NewService(db, audit.NewService(db))

	ownerID, orgID := seedOwnerMembership(t, db, "owner-"+uuid.NewString()+"@example.com")

	clientID := seedUser(t, db, "client-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, clientID, orgID, RoleClient)

	err := svc.RemoveOrgMember(ctx, orgID, ownerID, clientID)
	if !errors.Is(err, apperrors.ErrClientAttachedToProject) {
		t.Fatalf("removing a client membership error = %v, want ErrClientAttachedToProject", err)
	}

	// The client membership survives.
	var clientMemberships int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, clientID, orgID).Scan(&clientMemberships); err != nil {
		t.Fatalf("count client memberships: %v", err)
	}
	if clientMemberships != 1 {
		t.Fatalf("client membership count = %d, want 1 (removal rejected)", clientMemberships)
	}

	// Removing a staff membership still works.
	staffID := seedUser(t, db, "staff-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, staffID, orgID, RoleMember)
	if err := svc.RemoveOrgMember(ctx, orgID, ownerID, staffID); err != nil {
		t.Fatalf("removing a staff membership: %v", err)
	}
	var staffMemberships int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, staffID, orgID).Scan(&staffMemberships); err != nil {
		t.Fatalf("count staff memberships: %v", err)
	}
	if staffMemberships != 0 {
		t.Fatalf("staff membership count = %d, want 0 after removal", staffMemberships)
	}
}
