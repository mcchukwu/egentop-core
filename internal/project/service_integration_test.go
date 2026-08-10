package project

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
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
	return NewService(db, NewRepository(db), audit.NewService(db), activity.NewService(activity.NewRepository(db)))
}

func seedProject(t *testing.T, db *sql.DB) (ownerID, orgID, projectID, milestoneID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Owner', 'User')
		RETURNING id
	`, "proj-owner-"+uuid.NewString()+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, slug)
		VALUES ($1, $2)
		RETURNING id
	`, "Project Org", "proj-"+uuid.NewString()).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}

	var ownerRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'owner' AND organization_id IS NULL
	`).Scan(&ownerRoleID); err != nil {
		t.Fatalf("owner role: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, ownerID, orgID, ownerRoleID); err != nil {
		t.Fatalf("owner membership: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO projects (organization_id, created_by, name, status)
		VALUES ($1, $2, 'Project', 'active')
		RETURNING id
	`, orgID, ownerID).Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO milestones (organization_id, project_id, created_by, title, status)
		VALUES ($1, $2, $3, 'Milestone', 'pending')
		RETURNING id
	`, orgID, projectID, ownerID).Scan(&milestoneID); err != nil {
		t.Fatalf("insert milestone: %v", err)
	}

	return ownerID, orgID, projectID, milestoneID
}

func TestUpdateProjectMetadata(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)

	due := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)

	updated, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		Name:        "Renamed Project",
		Description: "A description",
		Priority:    ProjectPriorityHigh,
		DueDate:     &due,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Renamed Project" {
		t.Fatalf("expected name 'Renamed Project', got %q", updated.Name)
	}
	if updated.Priority != ProjectPriorityHigh {
		t.Fatalf("expected priority high, got %s", updated.Priority)
	}
	if updated.Description == nil || *updated.Description != "A description" {
		t.Fatalf("expected description to be set, got %v", updated.Description)
	}
	if updated.DueDate == nil {
		t.Fatalf("expected due date to be set")
	}
}

func TestUpdateProjectStatusTransition(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)

	// active -> completed is valid
	updated, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		Status: ProjectStatusCompleted,
	})
	if err != nil {
		t.Fatalf("Update status: %v", err)
	}
	if updated.Status != ProjectStatusCompleted {
		t.Fatalf("expected status completed, got %s", updated.Status)
	}

	// completed -> draft is invalid
	_, err = svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		Status: ProjectStatusDraft,
	})
	if err == nil {
		t.Fatalf("expected error for invalid status transition")
	}
}

func TestUpdateMilestoneMetadata(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, _, milestoneID := seedProject(t, db)

	updated, err := svc.UpdateMilestone(ctx, orgID, ownerID, milestoneID, UpdateMilestoneRequest{
		Title:    "Renamed Milestone",
		Position: 3,
	})
	if err != nil {
		t.Fatalf("UpdateMilestone: %v", err)
	}
	if updated.Title != "Renamed Milestone" {
		t.Fatalf("expected title 'Renamed Milestone', got %q", updated.Title)
	}
	if updated.Position != 3 {
		t.Fatalf("expected position 3, got %d", updated.Position)
	}
}
