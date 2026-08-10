package organization

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
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

func newTestService(db *sql.DB) *Service {
	return NewService(db, audit.NewService(db))
}

func TestCreateOrgSuccess(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	// seed owner
	var ownerID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Owner', 'User')
		RETURNING id
	`, "org-owner-"+uuid.NewString()+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}

	orgID, err := svc.Create(ctx, "Test Org "+uuid.NewString(), ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if orgID == uuid.Nil {
		t.Fatalf("expected non-nil org ID")
	}

	// the owner should have an active membership
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, ownerID, orgID).Scan(&count); err != nil {
		t.Fatalf("count membership: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 membership, got %d", count)
	}
}

func TestUpdateOrgRenames(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	var ownerID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Owner', 'User')
		RETURNING id
	`, "org-owner-"+uuid.NewString()+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}

	orgID, err := svc.Create(ctx, "Original Name", ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Update(ctx, orgID, "Renamed Org"); err != nil {
		t.Fatalf("Update: %v", err)
	}

	org, err := svc.GetByID(ctx, orgID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if org.Name != "Renamed Org" {
		t.Fatalf("expected name 'Renamed Org', got %q", org.Name)
	}
}

func TestUpdateOrgNotFound(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	err := svc.Update(ctx, uuid.New(), "New Name")
	if !errors.Is(err, apperrors.ErrOrganizationNotFound) {
		t.Fatalf("expected ErrOrganizationNotFound, got %v", err)
	}
}
