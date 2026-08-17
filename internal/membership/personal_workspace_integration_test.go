package membership

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

// seedPersonalOrg creates an org flagged is_personal=true with an active
// owner membership (mirrors the registration default org shape).
func seedPersonalOrg(t *testing.T, db *sql.DB, email string) (userID, orgID uuid.UUID) {
	t.Helper()

	ctx := context.Background()

	err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, email, "hash", "Personal", "Owner").Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, is_personal)
		VALUES ($1, TRUE)
		RETURNING id
	`, "Owner's Organization").Scan(&orgID)
	if err != nil {
		t.Fatalf("insert personal org: %v", err)
	}

	var ownerRoleID uuid.UUID
	err = db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'owner' AND organization_id IS NULL
	`).Scan(&ownerRoleID)
	if err != nil {
		t.Fatalf("resolve owner role: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id)
		VALUES ($1, $2, $3)
	`, userID, orgID, ownerRoleID)
	if err != nil {
		t.Fatalf("insert owner membership: %v", err)
	}

	return userID, orgID
}

// membershipCount reports how many memberships a user holds in an org (any
// role/status).
func membershipCount(t *testing.T, db *sql.DB, orgID, userID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM memberships WHERE organization_id = $1 AND user_id = $2
	`, orgID, userID).Scan(&count); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	return count
}

// TestPersonalOrgRejectsStaffMutations drives every staff-membership mutation
// against a personal workspace: add, invite, role change (incl. owner-target),
// and remove (incl. owner self-remove) all return ErrPersonalWorkspace, and
// nothing is written.
func TestPersonalOrgRejectsStaffMutations(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	ownerID, orgID := seedPersonalOrg(t, db, "personal-owner-"+uuid.NewString()+"@example.com")

	targetID := seedUser(t, db, "personal-target-"+uuid.NewString()+"@example.com")
	inviteeID := seedUser(t, db, "personal-invitee-"+uuid.NewString()+"@example.com")
	memberID := seedUser(t, db, "personal-member-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, memberID, orgID, RoleMember)

	svc := NewService(db, audit.NewService(db))

	t.Run("add member rejected", func(t *testing.T) {
		err := svc.AddOrgMember(ctx, orgID, ownerID, targetID, RoleMember)
		if !errors.Is(err, apperrors.ErrPersonalWorkspace) {
			t.Fatalf("AddOrgMember error = %v, want ErrPersonalWorkspace", err)
		}
		if got := membershipCount(t, db, orgID, targetID); got != 0 {
			t.Fatalf("membership created on personal org: count = %d, want 0", got)
		}
	})

	t.Run("invite rejected", func(t *testing.T) {
		err := svc.InviteOrgMember(ctx, orgID, ownerID, "personal-"+uuid.NewString()+"@example.com", RoleMember)
		if !errors.Is(err, apperrors.ErrPersonalWorkspace) {
			t.Fatalf("InviteOrgMember error = %v, want ErrPersonalWorkspace", err)
		}
		// The invitee user exists; no membership may have been created.
		if got := membershipCount(t, db, orgID, inviteeID); got != 0 {
			t.Fatalf("invited membership created on personal org: count = %d, want 0", got)
		}
	})

	t.Run("role change rejected", func(t *testing.T) {
		err := svc.UpdateOrgMemberRole(ctx, orgID, ownerID, memberID, RoleAdmin)
		if !errors.Is(err, apperrors.ErrPersonalWorkspace) {
			t.Fatalf("UpdateOrgMemberRole error = %v, want ErrPersonalWorkspace", err)
		}
		var role string
		if err := db.QueryRowContext(ctx, `
			SELECT r.name FROM memberships m JOIN roles r ON r.id = m.role_id
			WHERE m.organization_id = $1 AND m.user_id = $2
		`, orgID, memberID).Scan(&role); err != nil {
			t.Fatalf("verify unchanged role: %v", err)
		}
		if role != string(RoleMember) {
			t.Fatalf("role changed on personal org: got %q, want %q", role, RoleMember)
		}
	})

	t.Run("owner-target role change rejected", func(t *testing.T) {
		// Even re-rolling the OWNER is blocked: the personal-workspace guard
		// fires before the owner-demotion check.
		err := svc.UpdateOrgMemberRole(ctx, orgID, ownerID, ownerID, RoleMember)
		if !errors.Is(err, apperrors.ErrPersonalWorkspace) {
			t.Fatalf("owner-target role change error = %v, want ErrPersonalWorkspace", err)
		}
	})

	t.Run("member removal rejected", func(t *testing.T) {
		err := svc.RemoveOrgMember(ctx, orgID, ownerID, memberID)
		if !errors.Is(err, apperrors.ErrPersonalWorkspace) {
			t.Fatalf("RemoveOrgMember error = %v, want ErrPersonalWorkspace", err)
		}
		if got := membershipCount(t, db, orgID, memberID); got != 1 {
			t.Fatalf("membership removed on personal org: count = %d, want 1", got)
		}
	})

	t.Run("owner self-remove rejected", func(t *testing.T) {
		// The guard fires before the owner-protection check too.
		err := svc.RemoveOrgMember(ctx, orgID, ownerID, ownerID)
		if !errors.Is(err, apperrors.ErrPersonalWorkspace) {
			t.Fatalf("owner self-remove error = %v, want ErrPersonalWorkspace", err)
		}
	})
}

// TestGetOrgMembersOnPersonalOrgUnchanged: the member list stays available on
// a personal org (the owner sees themselves; the UI renders the personal
// state from it) and carries is_personal=true.
func TestGetOrgMembersOnPersonalOrgUnchanged(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	ownerID, orgID := seedPersonalOrg(t, db, "personal-list-"+uuid.NewString()+"@example.com")

	svc := NewService(db, audit.NewService(db))

	members, _, err := svc.GetOrgMembers(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("GetOrgMembers on personal org: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("member count = %d, want 1 (the owner)", len(members))
	}
	if members[0].UserID != ownerID {
		t.Fatalf("member = %v, want owner %v", members[0].UserID, ownerID)
	}
	if !members[0].IsPersonal {
		t.Fatal("member list item is_personal = false, want true on a personal org")
	}
}

// TestPersonalGuardDoesNotLeakCrossOrg: with a personal org AND a normal
// workspace in the same database, every staff mutation still succeeds on the
// workspace — the personal guard is strictly org-scoped.
func TestPersonalGuardDoesNotLeakCrossOrg(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()

	// The personal org exists in the same DB as the workspace under test.
	_, personalOrgID := seedPersonalOrg(t, db, "leak-personal-"+uuid.NewString()+"@example.com")

	ownerID, orgID := seedOwnerMembership(t, db, "leak-workspace-owner-"+uuid.NewString()+"@example.com")
	targetID := seedUser(t, db, "leak-workspace-target-"+uuid.NewString()+"@example.com")
	inviteeEmail := "leak-workspace-invitee-" + uuid.NewString() + "@example.com"
	seedUser(t, db, inviteeEmail)
	memberID := seedUser(t, db, "leak-workspace-member-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, memberID, orgID, RoleMember)

	if personalOrgID == orgID {
		t.Fatal("seed error: personal org and workspace share an id")
	}

	svc := NewService(db, audit.NewService(db))

	// All four mutations succeed on the normal workspace.
	if err := svc.AddOrgMember(ctx, orgID, ownerID, targetID, RoleMember); err != nil {
		t.Fatalf("AddOrgMember on workspace: %v", err)
	}
	if err := svc.InviteOrgMember(ctx, orgID, ownerID, inviteeEmail, RoleViewer); err != nil {
		t.Fatalf("InviteOrgMember on workspace: %v", err)
	}
	if err := svc.UpdateOrgMemberRole(ctx, orgID, ownerID, targetID, RoleAdmin); err != nil {
		t.Fatalf("UpdateOrgMemberRole on workspace: %v", err)
	}
	if err := svc.RemoveOrgMember(ctx, orgID, ownerID, targetID); err != nil {
		t.Fatalf("RemoveOrgMember on workspace: %v", err)
	}

	// And the same mutations are still blocked on the personal org.
	if err := svc.AddOrgMember(ctx, personalOrgID, ownerID, memberID, RoleMember); !errors.Is(err, apperrors.ErrPersonalWorkspace) {
		t.Fatalf("AddOrgMember on personal org error = %v, want ErrPersonalWorkspace", err)
	}
}
