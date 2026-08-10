package assignment

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
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

func newTestService(db *sql.DB) *Service {
	activityService := activity.NewService(activity.NewRepository(db))
	return NewService(db, NewRepository(db), audit.NewService(db), activityService)
}

type seededOrg struct {
	UserID    uuid.UUID
	OrgID     uuid.UUID
	ProjectID uuid.UUID
	Milestone uuid.UUID
	MemberID  uuid.UUID
}

// seedOrg creates a user, an org, a project, a milestone and a second member
// that can be assigned to.
func seedOrg(t *testing.T, db *sql.DB, suffix string) seededOrg {
	t.Helper()

	ctx := context.Background()
	var s seededOrg

	err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Test', 'User')
		RETURNING id
	`, "owner-"+suffix+"@example.com").Scan(&s.UserID)
	if err != nil {
		t.Fatalf("insert owner: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO organizations (name)
		VALUES ($1)
		RETURNING id
	`, "Org "+suffix).Scan(&s.OrgID)
	if err != nil {
		t.Fatalf("insert org: %v", err)
	}

	var ownerRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'owner' AND organization_id IS NULL
	`).Scan(&ownerRoleID); err != nil {
		t.Fatalf("owner role: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, s.UserID, s.OrgID, ownerRoleID)
	if err != nil {
		t.Fatalf("owner membership: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO projects (organization_id, created_by, name, status)
		VALUES ($1, $2, 'Project ' || $3, 'active')
		RETURNING id
	`, s.OrgID, s.UserID, suffix).Scan(&s.ProjectID)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO milestones (organization_id, project_id, created_by, title, status)
		VALUES ($1, $2, $3, 'Milestone ' || $4, 'pending')
		RETURNING id
	`, s.OrgID, s.ProjectID, s.UserID, suffix).Scan(&s.Milestone)
	if err != nil {
		t.Fatalf("insert milestone: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Member', 'User')
		RETURNING id
	`, "member-"+suffix+"@example.com").Scan(&s.MemberID)
	if err != nil {
		t.Fatalf("insert member: %v", err)
	}

	var memberRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'member' AND organization_id IS NULL
	`).Scan(&memberRoleID); err != nil {
		t.Fatalf("member role: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, s.MemberID, s.OrgID, memberRoleID)
	if err != nil {
		t.Fatalf("member membership: %v", err)
	}

	return s
}

func TestAssignmentCreateAndViewScopedToOrg(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	s := seedOrg(t, db, uuid.NewString())

	assignment, err := svc.Create(ctx, s.OrgID, s.UserID, s.ProjectID, s.Milestone, s.MemberID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// fetch within the same org succeeds
	fetched, err := svc.GetByID(ctx, s.OrgID, assignment.ID)
	if err != nil {
		t.Fatalf("GetByID same org: %v", err)
	}
	if fetched.ID != assignment.ID {
		t.Fatalf("expected assignment %s, got %s", assignment.ID, fetched.ID)
	}
	if fetched.AssignedTo != s.MemberID {
		t.Fatalf("expected assigned_to %s, got %s", s.MemberID, fetched.AssignedTo)
	}

	// a different org cannot read the assignment (tenant isolation)
	other := seedOrg(t, db, uuid.NewString())
	_, err = svc.GetByID(ctx, other.OrgID, assignment.ID)
	if !errors.Is(err, apperrors.ErrAssignmentNotFound) {
		t.Fatalf("expected ErrAssignmentNotFound for cross-org fetch, got %v", err)
	}
}

func TestAssignmentListUpdateDelete(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	s := seedOrg(t, db, uuid.NewString())

	assignment, err := svc.Create(ctx, s.OrgID, s.UserID, s.ProjectID, s.Milestone, s.MemberID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// list within the same org
	assignments, meta, err := svc.ListByProjectID(ctx, s.OrgID, s.ProjectID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("ListByProjectID: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if meta.Total != 1 {
		t.Fatalf("expected total 1, got %d", meta.Total)
	}

	// cross-org list returns no assignments
	other := seedOrg(t, db, uuid.NewString())
	crossOrg, _, err := svc.ListByProjectID(ctx, other.OrgID, s.ProjectID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("cross-org ListByProjectID: %v", err)
	}
	if len(crossOrg) != 0 {
		t.Fatalf("expected no cross-org assignments, got %d", len(crossOrg))
	}

	// reassign to the owner
	updated, err := svc.Update(ctx, s.OrgID, s.UserID, s.ProjectID, assignment.ID, s.UserID)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.AssignedTo != s.UserID {
		t.Fatalf("expected assigned_to %s after update, got %s", s.UserID, updated.AssignedTo)
	}

	// cross-org update is blocked
	_, err = svc.Update(ctx, other.OrgID, s.UserID, s.ProjectID, assignment.ID, s.UserID)
	if !errors.Is(err, apperrors.ErrAssignmentNotFound) {
		t.Fatalf("expected ErrAssignmentNotFound for cross-org update, got %v", err)
	}

	// delete
	if err := svc.Delete(ctx, s.OrgID, s.UserID, s.ProjectID, assignment.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = svc.GetByID(ctx, s.OrgID, assignment.ID)
	if !errors.Is(err, apperrors.ErrAssignmentNotFound) {
		t.Fatalf("expected ErrAssignmentNotFound after delete, got %v", err)
	}
}
