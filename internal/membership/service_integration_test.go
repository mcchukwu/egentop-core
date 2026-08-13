package membership

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("EGTEST_DB_URL")
	if dsn == "" {
		t.Skip("EGTEST_DB_URL not set; skipping integration test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	return db
}

func seedOwnerMembership(t *testing.T, db *sql.DB, email string) (userID, orgID uuid.UUID) {
	t.Helper()

	ctx := context.Background()

	err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, email, "hash", "Test", "User").Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO organizations (name)
		VALUES ($1)
		RETURNING id
	`, "Test Org").Scan(&orgID)
	if err != nil {
		t.Fatalf("insert org: %v", err)
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

func seedUser(t *testing.T, db *sql.DB, email string) uuid.UUID {
	t.Helper()

	var userID uuid.UUID
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Test', 'User')
		RETURNING id
	`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	return userID
}

func seedMembershipRole(t *testing.T, db *sql.DB, userID, orgID uuid.UUID, role Role) {
	t.Helper()

	roleID, err := ResolveSystemRoleID(context.Background(), db, role)
	if err != nil {
		t.Fatalf("resolve role: %v", err)
	}

	_, err = db.ExecContext(context.Background(), `
		INSERT INTO memberships (user_id, organization_id, role_id)
		VALUES ($1, $2, $3)
	`, userID, orgID, roleID)
	if err != nil {
		t.Fatalf("insert membership: %v", err)
	}
}

func TestMembershipServiceIntegration(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	suffix := uuid.NewString()
	ownerID, orgID := seedOwnerMembership(t, db, "owner-"+suffix+"@example.com")

	svc := NewService(db, audit.NewService(db))

	var newMemberID uuid.UUID
	err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "member-"+suffix+"@example.com", "hash", "New", "Member").Scan(&newMemberID)
	if err != nil {
		t.Fatalf("insert new member: %v", err)
	}

	t.Run("add member", func(t *testing.T) {
		err := svc.AddOrgMember(ctx, orgID, ownerID, newMemberID, RoleMember)
		if err != nil {
			t.Fatalf("AddOrgMember: %v", err)
		}

		var role string
		err = db.QueryRowContext(ctx, `
			SELECT r.name
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			WHERE m.user_id = $1 AND m.organization_id = $2
		`, newMemberID, orgID).Scan(&role)
		if err != nil {
			t.Fatalf("verify role: %v", err)
		}
		if role != "member" {
			t.Fatalf("expected role member, got %s", role)
		}

		// duplicate add is rejected
		err = svc.AddOrgMember(ctx, orgID, ownerID, newMemberID, RoleMember)
		if !errors.Is(err, apperrors.ErrAlreadyMember) {
			t.Fatalf("expected ErrAlreadyMember, got %v", err)
		}
	})

	t.Run("list members", func(t *testing.T) {
		members, _, err := svc.GetOrgMembers(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
		if err != nil {
			t.Fatalf("GetOrgMembers: %v", err)
		}
		if len(members) != 2 {
			t.Fatalf("expected 2 members, got %d", len(members))
		}
	})

	t.Run("update member role", func(t *testing.T) {
		err := svc.UpdateOrgMemberRole(ctx, orgID, ownerID, newMemberID, RoleAdmin)
		if err != nil {
			t.Fatalf("UpdateOrgMemberRole: %v", err)
		}

		var role string
		err = db.QueryRowContext(ctx, `
			SELECT r.name
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			WHERE m.user_id = $1 AND m.organization_id = $2
		`, newMemberID, orgID).Scan(&role)
		if err != nil {
			t.Fatalf("verify role: %v", err)
		}
		if role != "admin" {
			t.Fatalf("expected role admin, got %s", role)
		}

		// owner cannot be demoted
		err = svc.UpdateOrgMemberRole(ctx, orgID, ownerID, ownerID, RoleMember)
		if !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for owner demote, got %v", err)
		}
	})

	t.Run("remove member", func(t *testing.T) {
		// owner cannot be removed
		err := svc.RemoveOrgMember(ctx, orgID, ownerID, ownerID)
		if !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for owner removal, got %v", err)
		}

		// non-owner removal succeeds
		err = svc.RemoveOrgMember(ctx, orgID, ownerID, newMemberID)
		if err != nil {
			t.Fatalf("RemoveOrgMember: %v", err)
		}

		members, _, err := svc.GetOrgMembers(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
		if err != nil {
			t.Fatalf("GetOrgMembers: %v", err)
		}
		if len(members) != 1 {
			t.Fatalf("expected 1 member after removal, got %d", len(members))
		}
	})

	t.Run("invalid role is rejected", func(t *testing.T) {
		err := svc.AddOrgMember(ctx, orgID, ownerID, newMemberID, Role("nonexistent"))
		if !errors.Is(err, apperrors.ErrMembershipRoleNotFound) {
			t.Fatalf("expected ErrMembershipRoleNotFound, got %v", err)
		}
	})
}

func TestInviteOrgMemberByEmail(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	ownerID, orgID := seedOwnerMembership(t, db, "invite-owner-"+uuid.NewString()+"@example.com")

	inviteEmail := "invitee-" + uuid.NewString() + "@example.com"
	var inviteeID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Invitee', 'User')
		RETURNING id
	`, inviteEmail).Scan(&inviteeID); err != nil {
		t.Fatalf("insert invitee: %v", err)
	}

	svc := NewService(db, audit.NewService(db))

	t.Run("invite creates invited membership", func(t *testing.T) {
		if err := svc.InviteOrgMember(ctx, orgID, ownerID, inviteEmail, RoleViewer); err != nil {
			t.Fatalf("InviteOrgMember: %v", err)
		}

		var status, role string
		err := db.QueryRowContext(ctx, `
			SELECT m.status, r.name
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			WHERE m.user_id = $1 AND m.organization_id = $2
		`, inviteeID, orgID).Scan(&status, &role)
		if err != nil {
			t.Fatalf("verify membership: %v", err)
		}
		if status != "invited" {
			t.Fatalf("expected status invited, got %s", status)
		}
		if role != "viewer" {
			t.Fatalf("expected role viewer, got %s", role)
		}
	})

	t.Run("duplicate invite is rejected", func(t *testing.T) {
		err := svc.InviteOrgMember(ctx, orgID, ownerID, inviteEmail, RoleViewer)
		if !errors.Is(err, apperrors.ErrInvitationPending) {
			t.Fatalf("expected ErrInvitationPending, got %v", err)
		}
	})

	t.Run("unknown email is rejected", func(t *testing.T) {
		err := svc.InviteOrgMember(ctx, orgID, ownerID, "nobody-"+uuid.NewString()+"@example.com", RoleMember)
		if !errors.Is(err, apperrors.ErrUserNotFound) {
			t.Fatalf("expected ErrUserNotFound, got %v", err)
		}
	})
}

func TestAdminCannotGrantOwner(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	_, orgID := seedOwnerMembership(t, db, "grant-owner-"+uuid.NewString()+"@example.com")
	adminID := seedUser(t, db, "grant-admin-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, adminID, orgID, RoleAdmin)

	addTargetID := seedUser(t, db, "add-owner-target-"+uuid.NewString()+"@example.com")
	updateTargetID := seedUser(t, db, "update-owner-target-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, updateTargetID, orgID, RoleMember)
	inviteEmail := "invite-owner-target-" + uuid.NewString() + "@example.com"
	inviteTargetID := seedUser(t, db, inviteEmail)

	svc := NewService(db, audit.NewService(db))

	t.Run("admin cannot add owner", func(t *testing.T) {
		err := svc.AddOrgMember(ctx, orgID, adminID, addTargetID, RoleOwner)
		if !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}

		var count int
		if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM memberships
		WHERE organization_id = $1 AND user_id = $2
		`, orgID, addTargetID).Scan(&count); err != nil {
			t.Fatalf("count added membership: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no membership, got %d", count)
		}
	})

	t.Run("admin cannot promote member to owner", func(t *testing.T) {
		err := svc.UpdateOrgMemberRole(ctx, orgID, adminID, updateTargetID, RoleOwner)
		if !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}

		var role string
		if err := db.QueryRowContext(ctx, `
			SELECT r.name
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1 AND m.user_id = $2
		`, orgID, updateTargetID).Scan(&role); err != nil {
			t.Fatalf("verify unchanged membership role: %v", err)
		}
		if role != string(RoleMember) {
			t.Fatalf("membership role = %s, want %s", role, RoleMember)
		}
	})

	t.Run("admin cannot invite owner", func(t *testing.T) {
		err := svc.InviteOrgMember(ctx, orgID, adminID, inviteEmail, RoleOwner)
		if !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}

		var count int
		if err := db.QueryRowContext(ctx, `
			SELECT count(*)
			FROM memberships
			WHERE organization_id = $1 AND user_id = $2
		`, orgID, inviteTargetID).Scan(&count); err != nil {
			t.Fatalf("count invited membership: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected no membership, got %d", count)
		}
	})
}

func TestOwnerCanGrantOwner(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	ownerID, orgID := seedOwnerMembership(t, db, "owner-grant-"+uuid.NewString()+"@example.com")
	addTargetID := seedUser(t, db, "owner-add-target-"+uuid.NewString()+"@example.com")
	updateTargetID := seedUser(t, db, "owner-update-target-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, updateTargetID, orgID, RoleMember)
	inviteEmail := "owner-invite-target-" + uuid.NewString() + "@example.com"
	inviteTargetID := seedUser(t, db, inviteEmail)

	svc := NewService(db, audit.NewService(db))

	if err := svc.AddOrgMember(ctx, orgID, ownerID, addTargetID, RoleOwner); err != nil {
		t.Fatalf("owner AddOrgMember: %v", err)
	}
	if err := svc.InviteOrgMember(ctx, orgID, ownerID, inviteEmail, RoleOwner); err != nil {
		t.Fatalf("owner InviteOrgMember: %v", err)
	}
	if err := svc.UpdateOrgMemberRole(ctx, orgID, ownerID, updateTargetID, RoleOwner); err != nil {
		t.Fatalf("owner UpdateOrgMemberRole: %v", err)
	}

	var addedRole, updatedRole, invitedRole string
	if err := db.QueryRowContext(ctx, `
		SELECT r.name
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1 AND m.user_id = $2
	`, orgID, addTargetID).Scan(&addedRole); err != nil {
		t.Fatalf("verify added owner membership: %v", err)
	}
	if addedRole != string(RoleOwner) {
		t.Fatalf("added role = %s, want owner", addedRole)
	}

	if err := db.QueryRowContext(ctx, `
		SELECT r.name
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1 AND m.user_id = $2
	`, orgID, updateTargetID).Scan(&updatedRole); err != nil {
		t.Fatalf("verify updated owner membership: %v", err)
	}
	if updatedRole != string(RoleOwner) {
		t.Fatalf("updated role = %s, want owner", updatedRole)
	}

	if err := db.QueryRowContext(ctx, `
		SELECT r.name
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1 AND m.user_id = $2
	`, orgID, inviteTargetID).Scan(&invitedRole); err != nil {
		t.Fatalf("verify invited owner membership: %v", err)
	}
	if invitedRole != string(RoleOwner) {
		t.Fatalf("invited role = %s, want owner", invitedRole)
	}
}
