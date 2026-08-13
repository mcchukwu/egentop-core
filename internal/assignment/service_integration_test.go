package assignment

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

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
	fetched, err := svc.GetByID(ctx, s.OrgID, s.ProjectID, assignment.ID)
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
	_, err = svc.GetByID(ctx, other.OrgID, s.ProjectID, assignment.ID)
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

	_, err = svc.GetByID(ctx, s.OrgID, s.ProjectID, assignment.ID)
	if !errors.Is(err, apperrors.ErrAssignmentNotFound) {
		t.Fatalf("expected ErrAssignmentNotFound after delete, got %v", err)
	}
}

func TestAssignmentNestedParentScope(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	s := seedOrg(t, db, uuid.NewString())
	otherProjectID, _ := seedAdditionalAssignmentProject(t, db, s.OrgID, s.UserID)
	assignment, err := svc.Create(ctx, s.OrgID, s.UserID, s.ProjectID, s.Milestone, s.MemberID)
	if err != nil {
		t.Fatalf("same-project assignment create: %v", err)
	}
	if _, err := svc.GetByID(ctx, s.OrgID, otherProjectID, assignment.ID); !errors.Is(err, apperrors.ErrAssignmentNotFound) {
		t.Fatalf("wrong-parent assignment read error = %v, want ErrAssignmentNotFound", err)
	}
	if _, err := svc.Update(ctx, s.OrgID, s.UserID, otherProjectID, assignment.ID, s.UserID); !errors.Is(err, apperrors.ErrAssignmentNotFound) {
		t.Fatalf("wrong-parent assignment update error = %v, want ErrAssignmentNotFound", err)
	}
	unchanged, err := svc.GetByID(ctx, s.OrgID, s.ProjectID, assignment.ID)
	if err != nil {
		t.Fatalf("read assignment after rejected update: %v", err)
	}
	if unchanged.AssignedTo != s.MemberID {
		t.Fatalf("wrong-parent update changed assignee to %s", unchanged.AssignedTo)
	}
	if err := svc.Delete(ctx, s.OrgID, s.UserID, otherProjectID, assignment.ID); !errors.Is(err, apperrors.ErrAssignmentNotFound) {
		t.Fatalf("wrong-parent assignment delete error = %v, want ErrAssignmentNotFound", err)
	}
	if _, err := svc.GetByID(ctx, s.OrgID, s.ProjectID, assignment.ID); err != nil {
		t.Fatalf("wrong-parent delete removed assignment: %v", err)
	}
	if err := svc.Delete(ctx, s.OrgID, s.UserID, s.ProjectID, assignment.ID); err != nil {
		t.Fatalf("same-project assignment delete: %v", err)
	}
}

func TestAssignmentCreateValidatesRelationshipsAndAssignee(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	s := seedOrg(t, db, uuid.NewString())
	other := seedOrg(t, db, uuid.NewString())
	otherProjectID, otherMilestoneID := seedAdditionalAssignmentProject(t, db, s.OrgID, s.UserID)

	tests := []struct {
		name                                      string
		orgID, projectID, milestoneID, assignedTo uuid.UUID
		want                                      error
	}{
		{"cross-org project", s.OrgID, other.ProjectID, s.Milestone, s.MemberID, apperrors.ErrProjectNotFound},
		{"cross-project milestone", s.OrgID, s.ProjectID, otherMilestoneID, s.MemberID, apperrors.ErrMilestoneNotFound},
		{"cross-org milestone", s.OrgID, s.ProjectID, other.Milestone, s.MemberID, apperrors.ErrMilestoneNotFound},
		{"non-member assignee", s.OrgID, s.ProjectID, s.Milestone, uuid.New(), apperrors.ErrMembershipNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Create(ctx, tc.orgID, s.UserID, tc.projectID, tc.milestoneID, tc.assignedTo)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Create error = %v, want %v", err, tc.want)
			}
		})
	}

	suspended := seedAssignmentUser(t, db, "suspended-"+uuid.NewString()+"@example.com")
	seedAssignmentMembership(t, db, suspended, s.OrgID, "suspended")
	if _, err := svc.Create(ctx, s.OrgID, s.UserID, otherProjectID, otherMilestoneID, suspended); !errors.Is(err, apperrors.ErrMembershipNotFound) {
		t.Fatalf("suspended assignee error = %v, want ErrMembershipNotFound", err)
	}
	if _, err := svc.Create(ctx, s.OrgID, s.UserID, otherProjectID, otherMilestoneID, s.MemberID); err != nil {
		t.Fatalf("valid same-project assignment create: %v", err)
	}
}

func TestAssignmentUpdateRejectsInactiveAssigneeAndPreservesAssignment(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	s := seedOrg(t, db, uuid.NewString())
	assignment, err := svc.Create(ctx, s.OrgID, s.UserID, s.ProjectID, s.Milestone, s.MemberID)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	suspended := seedAssignmentUser(t, db, "reassign-suspended-"+uuid.NewString()+"@example.com")
	seedAssignmentMembership(t, db, suspended, s.OrgID, "suspended")

	for _, tc := range []struct {
		name       string
		assignedTo uuid.UUID
	}{
		{name: "non-member", assignedTo: uuid.New()},
		{name: "suspended-member", assignedTo: suspended},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Update(ctx, s.OrgID, s.UserID, s.ProjectID, assignment.ID, tc.assignedTo); !errors.Is(err, apperrors.ErrMembershipNotFound) {
				t.Fatalf("Update error = %v, want ErrMembershipNotFound", err)
			}

			unchanged, err := svc.GetByID(ctx, s.OrgID, s.ProjectID, assignment.ID)
			if err != nil {
				t.Fatalf("read assignment after rejected update: %v", err)
			}
			if unchanged.AssignedTo != s.MemberID {
				t.Fatalf("rejected update changed assignee to %s", unchanged.AssignedTo)
			}
		})
	}
}

func seedAdditionalAssignmentProject(t *testing.T, db *sql.DB, orgID, ownerID uuid.UUID) (projectID, milestoneID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := db.QueryRowContext(ctx, `INSERT INTO projects (organization_id, created_by, name, status) VALUES ($1, $2, $3, 'active') RETURNING id`, orgID, ownerID, "Assignment Project "+uuid.NewString()).Scan(&projectID); err != nil {
		t.Fatalf("insert assignment project: %v", err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO milestones (organization_id, project_id, created_by, title, status) VALUES ($1, $2, $3, $4, 'pending') RETURNING id`, orgID, projectID, ownerID, "Assignment Milestone "+uuid.NewString()).Scan(&milestoneID); err != nil {
		t.Fatalf("insert assignment milestone: %v", err)
	}
	return projectID, milestoneID
}

func seedAssignmentUser(t *testing.T, db *sql.DB, email string) uuid.UUID {
	t.Helper()
	var userID uuid.UUID
	if err := db.QueryRowContext(context.Background(), `INSERT INTO users (email, password_hash, first_name, last_name) VALUES ($1, 'hash', 'Assignment', 'User') RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatalf("insert assignment user: %v", err)
	}
	return userID
}

func seedAssignmentMembership(t *testing.T, db *sql.DB, userID, orgID uuid.UUID, status string) {
	t.Helper()
	var roleID uuid.UUID
	if err := db.QueryRowContext(context.Background(), `SELECT id FROM roles WHERE name = 'member' AND organization_id IS NULL`).Scan(&roleID); err != nil {
		t.Fatalf("member role: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO memberships (user_id, organization_id, role_id, status) VALUES ($1, $2, $3, $4)`, userID, orgID, roleID, status); err != nil {
		t.Fatalf("insert assignment membership: %v", err)
	}
}
