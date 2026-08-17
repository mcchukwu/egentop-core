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

// TestCreateOrgIsNotPersonal: POST /v1/orgs (Service.Create) creates a normal
// workspace — is_personal is false — even though registration default orgs are
// personal.
func TestCreateOrgIsNotPersonal(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	var ownerID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Owner', 'User')
		RETURNING id
	`, "org-not-personal-"+uuid.NewString()+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}

	orgID, err := svc.Create(ctx, "Workspace "+uuid.NewString(), ownerID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var isPersonal bool
	if err := db.QueryRowContext(ctx, `
		SELECT is_personal FROM organizations WHERE id = $1
	`, orgID).Scan(&isPersonal); err != nil {
		t.Fatalf("read is_personal: %v", err)
	}
	if isPersonal {
		t.Fatal("POST /v1/orgs created org must have is_personal = false")
	}

	// The DTO carries it too (GET /v1/orgs/{orgID}).
	org, err := svc.GetByID(ctx, orgID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if org.IsPersonal {
		t.Fatal("GetByID returned is_personal = true for a workspace org")
	}
}

// TestOrgDTOExposesIsPersonal: the org detail payload and the org switcher
// (GET /v1/orgs) both carry the is_personal flag, populated from the
// organizations row — never silently false.
func TestOrgDTOExposesIsPersonal(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	// Workspace org (is_personal = false).
	var wsOwnerID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Ws', 'Owner')
		RETURNING id
	`, "org-dto-ws-"+uuid.NewString()+"@example.com").Scan(&wsOwnerID); err != nil {
		t.Fatalf("insert ws owner: %v", err)
	}
	wsOrgID, err := svc.Create(ctx, "DTO Workspace", wsOwnerID)
	if err != nil {
		t.Fatalf("Create workspace: %v", err)
	}

	// Personal org (is_personal = true), same owner for the list check.
	// The explicit slug mirrors what CreateTx always writes for production orgs.
	var personalOrgID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, slug, is_personal)
		VALUES ($1, $2, TRUE)
		RETURNING id
	`, "DTO Personal's Organization", "dto-personal-"+uuid.NewString()).Scan(&personalOrgID); err != nil {
		t.Fatalf("insert personal org: %v", err)
	}
	var ownerRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'owner' AND organization_id IS NULL
	`).Scan(&ownerRoleID); err != nil {
		t.Fatalf("owner role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id)
		VALUES ($1, $2, $3)
	`, wsOwnerID, personalOrgID, ownerRoleID); err != nil {
		t.Fatalf("insert personal membership: %v", err)
	}

	// GET /v1/orgs/{orgID} detail for both.
	ws, err := svc.GetByID(ctx, wsOrgID)
	if err != nil {
		t.Fatalf("GetByID workspace: %v", err)
	}
	if ws.IsPersonal {
		t.Fatal("workspace detail is_personal = true, want false")
	}

	personal, err := svc.GetByID(ctx, personalOrgID)
	if err != nil {
		t.Fatalf("GetByID personal: %v", err)
	}
	if !personal.IsPersonal {
		t.Fatal("personal detail is_personal = false, want true")
	}

	// GET /v1/orgs (switcher) carries the flag on each membership item.
	memberships, _, err := svc.List(ctx, wsOwnerID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	seen := map[uuid.UUID]bool{wsOrgID: false, personalOrgID: false}
	for _, m := range memberships {
		if _, ok := seen[m.OrganizationID]; !ok {
			continue
		}
		seen[m.OrganizationID] = m.IsPersonal
	}
	if seen[wsOrgID] {
		t.Fatal("workspace list item is_personal = true, want false")
	}
	if !seen[personalOrgID] {
		t.Fatal("personal list item is_personal = false, want true")
	}
}
