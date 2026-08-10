package membership

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/pagination"
	_ "github.com/jackc/pgx/v5/stdlib"
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
