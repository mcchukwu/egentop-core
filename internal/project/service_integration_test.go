package project

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/activity"
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

func seedAdditionalProject(t *testing.T, db *sql.DB, orgID, ownerID uuid.UUID) (projectID, milestoneID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := db.QueryRowContext(ctx, `
		INSERT INTO projects (organization_id, created_by, name, status)
		VALUES ($1, $2, $3, 'active') RETURNING id
	`, orgID, ownerID, "Project "+uuid.NewString()).Scan(&projectID); err != nil {
		t.Fatalf("insert additional project: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO milestones (organization_id, project_id, created_by, title, status)
		VALUES ($1, $2, $3, $4, 'pending') RETURNING id
	`, orgID, projectID, ownerID, "Milestone "+uuid.NewString()).Scan(&milestoneID); err != nil {
		t.Fatalf("insert additional milestone: %v", err)
	}
	return projectID, milestoneID
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
		DueDate:     OptionalTime{Present: true, Value: &due},
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

func TestUpdateProjectRejectsCrossOrganizationProject(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, ownerOrgID, projectID, _ := seedProject(t, db)
	_, otherOrgID, _, _ := seedProject(t, db)

	_, err := svc.Update(ctx, ownerID, otherOrgID, projectID, UpdateProjectRequest{Name: "Should Not Update"})
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound for cross-org update, got %v", err)
	}

	unchanged, err := svc.GetByID(ctx, ownerOrgID, projectID)
	if err != nil {
		t.Fatalf("read project after rejected update: %v", err)
	}
	if unchanged.Name != "Project" {
		t.Fatalf("cross-org update changed project name to %q", unchanged.Name)
	}
}

func TestUpdateMilestoneMetadata(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)

	updated, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{
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

// TestProjectReadsScopedToOrg proves that project/milestone read paths are
// org-scoped: a second organization cannot read (or even detect the existence
// of) another org's project or milestone.
func TestProjectReadsScopedToOrg(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	// Org A owns the project and milestone; Org B must not be able to read them.
	ownerAID, orgAID, projectAID, milestoneAID := seedProject(t, db)
	_, orgBID, _, _ := seedProject(t, db)

	// Positive: reads within the owning org succeed.
	if _, err := svc.GetByID(ctx, orgAID, projectAID); err != nil {
		t.Fatalf("GetByID same org: %v", err)
	}
	if _, err := svc.GetMilestoneByID(ctx, orgAID, projectAID, milestoneAID); err != nil {
		t.Fatalf("GetMilestoneByID same org: %v", err)
	}
	milestones, meta, err := svc.ListMilestonesByProjectID(ctx, ownerAID, "owner", orgAID, projectAID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("ListMilestonesByProjectID same org: %v", err)
	}
	if len(milestones) != 1 || meta.Total != 1 {
		t.Fatalf("expected 1 milestone in owning org, got %d (total %d)", len(milestones), meta.Total)
	}

	// Cross-org reads must not leak existence: they look identical to not-found.
	_, err = svc.GetByID(ctx, orgBID, projectAID)
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound for cross-org GetByID, got %v", err)
	}
	_, err = svc.GetMilestoneByID(ctx, orgBID, projectAID, milestoneAID)
	if !errors.Is(err, apperrors.ErrMilestoneNotFound) {
		t.Fatalf("expected ErrMilestoneNotFound for cross-org GetMilestoneByID, got %v", err)
	}
	_, _, err = svc.ListMilestonesByProjectID(ctx, ownerAID, "owner", orgBID, projectAID, pagination.Query{Page: 1, Limit: 20})
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound for cross-org milestone list, got %v", err)
	}
}

func TestMilestoneListValidEmptyParent(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	ownerID, orgID, _, _ := seedProject(t, db)
	emptyProjectID := seedEmptyProject(t, db, orgID, ownerID)

	milestones, meta, err := svc.ListMilestonesByProjectID(ctx, ownerID, "owner", orgID, emptyProjectID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("list milestones for valid empty project: %v", err)
	}
	if len(milestones) != 0 || meta.Total != 0 {
		t.Fatalf("expected empty milestone list, got %d (total %d)", len(milestones), meta.Total)
	}

	_, missingOrgID, _, _ := seedProject(t, db)
	_, _, err = svc.ListMilestonesByProjectID(ctx, ownerID, "owner", missingOrgID, emptyProjectID, pagination.Query{Page: 1, Limit: 20})
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound for cross-org milestone list, got %v", err)
	}
	_, _, err = svc.ListMilestonesByProjectID(ctx, ownerID, "owner", orgID, uuid.New(), pagination.Query{Page: 1, Limit: 20})
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound for missing project milestone list, got %v", err)
	}
}

func TestMilestoneNestedParentScope(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	otherProjectID, _ := seedAdditionalProject(t, db, orgID, ownerID)

	if _, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID); err != nil {
		t.Fatalf("same-project milestone read: %v", err)
	}
	if _, err := svc.GetMilestoneByID(ctx, orgID, otherProjectID, milestoneID); !errors.Is(err, apperrors.ErrMilestoneNotFound) {
		t.Fatalf("wrong-parent milestone read error = %v, want ErrMilestoneNotFound", err)
	}
	if _, err := svc.UpdateMilestone(ctx, orgID, ownerID, otherProjectID, milestoneID, UpdateMilestoneRequest{Title: "Should Not Update"}); !errors.Is(err, apperrors.ErrMilestoneNotFound) {
		t.Fatalf("wrong-parent milestone update error = %v, want ErrMilestoneNotFound", err)
	}
	unchanged, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone after rejected update: %v", err)
	}
	if unchanged.Title != "Milestone" {
		t.Fatalf("wrong-parent update changed milestone title to %q", unchanged.Title)
	}
	updated, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{Title: "Updated Correctly"})
	if err != nil {
		t.Fatalf("same-project milestone update: %v", err)
	}
	if updated.Title != "Updated Correctly" {
		t.Fatalf("same-project milestone title = %q", updated.Title)
	}
}

func seedEmptyProject(t *testing.T, db *sql.DB, orgID, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	var projectID uuid.UUID
	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO projects (organization_id, created_by, name, status)
		VALUES ($1, $2, $3, 'active') RETURNING id
	`, orgID, ownerID, "Empty Project "+uuid.NewString()).Scan(&projectID); err != nil {
		t.Fatalf("insert empty project: %v", err)
	}
	return projectID
}
