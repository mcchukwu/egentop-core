package organization

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestBackfillPersonalWorkspace executes the 000007 backfill UPDATE verbatim
// against seeded fixtures and asserts each outcome. It mirrors the composite
// evidence-based rule: registration naming pattern + same-tx creation
// timestamp (organizations.created_at == owner users.created_at, the
// PostgreSQL transaction-start NOW() signal) + exactly one non-client
// membership holding the owner role. Client memberships are excluded from the
// staff count — a personal org may already have provisioned clients.
func TestBackfillPersonalWorkspace(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	type memberSeed struct {
		role   string
		status string
	}
	seed := func(orgName string, orgCreatedAt, userCreatedAt time.Time, members []memberSeed) uuid.UUID {
		t.Helper()

		var userIDs []uuid.UUID
		for range members {
			var id uuid.UUID
			if err := db.QueryRowContext(ctx, `
				INSERT INTO users (email, password_hash, first_name, last_name, created_at)
				VALUES ($1, 'hash', 'Fixture', 'User', $2)
				RETURNING id
			`, "backfill-"+uuid.NewString()+"@example.com", userCreatedAt).Scan(&id); err != nil {
				t.Fatalf("insert fixture user: %v", err)
			}
			userIDs = append(userIDs, id)
		}

		var orgID uuid.UUID
		if err := db.QueryRowContext(ctx, `
			INSERT INTO organizations (name, created_at)
			VALUES ($1, $2)
			RETURNING id
		`, orgName, orgCreatedAt).Scan(&orgID); err != nil {
			t.Fatalf("insert fixture org: %v", err)
		}

		for i := range members {
			m := members[i]
			var roleID uuid.UUID
			if err := db.QueryRowContext(ctx, `
				SELECT id FROM roles WHERE name = $1 AND organization_id IS NULL
			`, m.role).Scan(&roleID); err != nil {
				t.Fatalf("resolve role %q: %v", m.role, err)
			}
			if _, err := db.ExecContext(ctx, `
				INSERT INTO memberships (user_id, organization_id, role_id, status)
				VALUES ($1, $2, $3, $4)
			`, userIDs[i], orgID, roleID, m.status); err != nil {
				t.Fatalf("insert fixture membership: %v", err)
			}
		}

		return orgID
	}

	orgs := map[string]uuid.UUID{}

	// 1. Eligible default org (registration shape) -> TRUE.
	orgs["eligible"] = seed("Ada's Organization", t0, t0, []memberSeed{{"owner", "active"}})
	// 2. Renamed default: name no longer matches the registration pattern -> FALSE.
	orgs["renamed"] = seed("Renamed Agency", t0, t0, []memberSeed{{"owner", "active"}})
	// 3. Workspace named "X's Organization" with two staff members -> FALSE.
	orgs["twoStaff"] = seed("Grace's Organization", t0, t0, []memberSeed{{"owner", "active"}, {"member", "active"}})
	// 4. Eligible org WITH clients (client memberships excluded from staff count) -> TRUE.
	orgs["withClient"] = seed("Linus's Organization", t0, t0, []memberSeed{{"owner", "active"}, {"client", "active"}})
	// 5. Org created in a later tx than its owner user (POST /v1/orgs shape) -> FALSE.
	orgs["laterTx"] = seed("Margaret's Organization", t0.Add(time.Hour), t0, []memberSeed{{"owner", "active"}})
	// 6. Org with an invited staff membership -> FALSE (2 non-client memberships).
	orgs["invited"] = seed("Ken's Organization", t0, t0, []memberSeed{{"owner", "active"}, {"viewer", "invited"}})

	// Execute the migration's UPDATE verbatim.
	if _, err := db.ExecContext(ctx, `
		UPDATE organizations o
		SET is_personal = TRUE
		WHERE o.is_personal = FALSE
		  AND o.name LIKE '%''s Organization'
		  AND o.created_at = (
		        SELECT u.created_at
		        FROM memberships m
		        JOIN roles r  ON r.id = m.role_id
		        JOIN users u  ON u.id = m.user_id
		        WHERE m.organization_id = o.id
		          AND r.name <> 'client'
		        LIMIT 1
		  )
		  AND (SELECT count(*) FROM memberships m JOIN roles r ON r.id = m.role_id
		       WHERE m.organization_id = o.id AND r.name <> 'client') = 1
		  AND (SELECT count(*) FROM memberships m JOIN roles r ON r.id = m.role_id
		       WHERE m.organization_id = o.id AND r.name = 'owner') = 1;
	`); err != nil {
		t.Fatalf("backfill UPDATE failed: %v", err)
	}

	for name, orgID := range orgs {
		var isPersonal bool
		if err := db.QueryRowContext(ctx, `
			SELECT is_personal FROM organizations WHERE id = $1
		`, orgID).Scan(&isPersonal); err != nil {
			t.Fatalf("read is_personal for %s: %v", name, err)
		}
		want := map[string]bool{
			"eligible":   true,
			"renamed":    false,
			"twoStaff":   false,
			"withClient": true,
			"laterTx":    false,
			"invited":    false,
		}[name]
		if isPersonal != want {
			t.Fatalf("fixture %q: is_personal = %v, want %v", name, isPersonal, want)
		}
	}
}
