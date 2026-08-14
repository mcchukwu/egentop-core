package activity

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
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

func TestActivityListScopedToOrg(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := NewService(NewRepository(db))

	// seed orgs
	var orgA, orgB uuid.UUID
	var userA, userB uuid.UUID

	err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'A', 'B')
		RETURNING id
	`, "act-owner-"+uuid.NewString()+"@example.com").Scan(&userA)
	if err != nil {
		t.Fatalf("insert user A: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'A', 'B')
		RETURNING id
	`, "act-owner-"+uuid.NewString()+"@example.com").Scan(&userB)
	if err != nil {
		t.Fatalf("insert user B: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, slug)
		VALUES ($1, $2)
		RETURNING id
	`, "Activity Org A", "act-a-"+uuid.NewString()).Scan(&orgA)
	if err != nil {
		t.Fatalf("insert org A: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, slug)
		VALUES ($1, $2)
		RETURNING id
	`, "Activity Org B", "act-b-"+uuid.NewString()).Scan(&orgB)
	if err != nil {
		t.Fatalf("insert org B: %v", err)
	}

	// log activity into org A only
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}

	err = svc.Log(ctx, tx, LogActivityEntry{
		OrganizationID: orgA,
		ActorID:        &userA,
		Type:           "project.created",
		Message:        "Project created",
		Metadata:       map[string]any{"foo": "bar"},
	})
	if err != nil {
		tx.Rollback()
		t.Fatalf("Log org A: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx 2: %v", err)
	}

	err = svc.Log(ctx, tx2, LogActivityEntry{
		OrganizationID: orgA,
		ActorID:        &userA,
		Type:           "milestone.created",
		Message:        "Milestone created",
		Metadata:       map[string]any{},
	})
	if err != nil {
		tx2.Rollback()
		t.Fatalf("Log org A second: %v", err)
	}

	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit tx 2: %v", err)
	}

	// org A sees both entries, newest first
	activities, meta, err := svc.List(ctx, orgA, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("List org A: %v", err)
	}
	if len(activities) != 2 {
		t.Fatalf("expected 2 activities for org A, got %d", len(activities))
	}
	if meta.Total != 2 {
		t.Fatalf("expected total 2, got %d", meta.Total)
	}
	if activities[0].Type != "milestone.created" {
		t.Fatalf("expected newest activity first (milestone.created), got %s", activities[0].Type)
	}
	if activities[0].Metadata["foo"] == "bar" {
		t.Fatalf("metadata should not leak across rows")
	}
	if activities[1].Metadata["foo"] != "bar" {
		t.Fatalf("expected metadata foo=bar, got %v", activities[1].Metadata)
	}

	// org B sees nothing (tenant isolation)
	activitiesB, _, err := svc.List(ctx, orgB, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("List org B: %v", err)
	}
	if len(activitiesB) != 0 {
		t.Fatalf("expected no activities for org B, got %d", len(activitiesB))
	}
}
