package organization

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"regexp"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
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

	// the creation is audited with the organization and owner set
	var auditCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM audit_logs WHERE action = 'organization.created' AND organization_id = $1 AND user_id = $2
	`, orgID, ownerID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected 1 organization.created audit row, got %d", auditCount)
	}
}

func TestCreateOrgSlugCollisionRetriesWithSuffix(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	seedOwner := func() uuid.UUID {
		var ownerID uuid.UUID
		if err := db.QueryRowContext(ctx, `
			INSERT INTO users (email, password_hash, first_name, last_name)
			VALUES ($1, 'hash', 'Owner', 'User')
			RETURNING id
		`, "org-slug-owner-"+uuid.NewString()+"@example.com").Scan(&ownerID); err != nil {
			t.Fatalf("insert owner: %v", err)
		}
		return ownerID
	}

	slugRe := regexp.MustCompile(`^[a-z0-9-]+$`)

	// Two organizations with the SAME name must both succeed with distinct,
	// non-empty slugs (the second exercises the retry/savepoint path).
	const name = "Acme Corp"

	firstID, err := svc.Create(ctx, name, seedOwner())
	if err != nil {
		t.Fatalf("Create (1st): %v", err)
	}
	secondID, err := svc.Create(ctx, name, seedOwner())
	if err != nil {
		t.Fatalf("Create (2nd, slug collision): %v", err)
	}

	if firstID == uuid.Nil || secondID == uuid.Nil {
		t.Fatalf("expected non-nil org IDs")
	}
	if firstID == secondID {
		t.Fatalf("expected distinct org IDs")
	}

	fetchSlug := func(orgID uuid.UUID) string {
		var s string
		if err := db.QueryRowContext(ctx, `SELECT slug FROM organizations WHERE id = $1`, orgID).Scan(&s); err != nil {
			t.Fatalf("fetch slug: %v", err)
		}
		return s
	}

	firstSlug, secondSlug := fetchSlug(firstID), fetchSlug(secondID)

	if firstSlug == "" || secondSlug == "" {
		t.Fatalf("expected non-empty slugs, got %q and %q", firstSlug, secondSlug)
	}
	if firstSlug == secondSlug {
		t.Fatalf("expected distinct slugs, both %q", firstSlug)
	}
	if !slugRe.MatchString(firstSlug) || !slugRe.MatchString(secondSlug) {
		t.Fatalf("slugs must match ^[a-z0-9-]+$, got %q and %q", firstSlug, secondSlug)
	}

	// both orgs exist under the same name; the count is lower-bounded (>= 2)
	// rather than exact so the test stays idempotent on a persistent DB
	// where previous runs may have created additional orgs with this name.
	var nameCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM organizations WHERE name = $1`, name).Scan(&nameCount); err != nil {
		t.Fatalf("count orgs: %v", err)
	}
	if nameCount < 2 {
		t.Fatalf("expected at least 2 orgs named %q, got %d", name, nameCount)
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
