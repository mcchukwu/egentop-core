package project

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/client"
)

// seedPersonalOrgOwner creates a PERSONAL workspace (is_personal = true) with
// an active owner membership — the registration default org shape.
func seedPersonalOrgOwner(t *testing.T, db *sql.DB) (ownerID, orgID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Personal', 'Owner')
		RETURNING id
	`, "proj-personal-owner-"+uuid.NewString()+"@example.com").Scan(&ownerID); err != nil {
		t.Fatalf("insert owner: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, is_personal)
		VALUES ($1, TRUE)
		RETURNING id
	`, "Owner's Organization").Scan(&orgID); err != nil {
		t.Fatalf("insert personal org: %v", err)
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

	return ownerID, orgID
}

func newPersonalOrgClientService(db *sql.DB) *client.Service {
	return client.NewService(db, client.NewRepository(db), audit.NewService(db), activity.NewService(activity.NewRepository(db)))
}

// provisionClientOnPersonalOrg provisions a client-role membership on the
// personal org and returns the client user id.
func provisionClientOnPersonalOrg(t *testing.T, db *sql.DB, staffID, orgID uuid.UUID) uuid.UUID {
	t.Helper()

	svc := newPersonalOrgClientService(db)
	result, err := svc.Provision(context.Background(), staffID, orgID, client.ProvisionRequest{
		Email:     "proj-personal-client-" + uuid.NewString() + "@example.com",
		FirstName: "Client",
		LastName:  "User",
	})
	if err != nil {
		t.Fatalf("provision client on personal org: %v", err)
	}
	return result.ClientID
}

// TestAssignClientOnPersonalOrg: the project-client assign flow keys off
// projects.client_id and the client-role membership — it must keep working on
// a personal workspace.
func TestAssignClientOnPersonalOrg(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID := seedPersonalOrgOwner(t, db)
	clientID := provisionClientOnPersonalOrg(t, db, ownerID, orgID)

	project, err := svc.Create(ctx, ownerID, orgID, CreateProjectRequest{Name: "Personal Org Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	assigned, err := svc.AssignClient(ctx, ownerID, orgID, project.ID, &clientID)
	if err != nil {
		t.Fatalf("AssignClient on personal org: %v", err)
	}
	if assigned.ClientID == nil || *assigned.ClientID != clientID {
		t.Fatalf("client_id after assign = %v, want %v", assigned.ClientID, clientID)
	}
}

// TestApprovalLoopOnPersonalOrg proves the full client wedge on a personal
// workspace: provision -> assign -> milestone -> submit -> changes-requested
// -> resubmit -> approve -> GetApprovalView. Every step succeeds — the
// personal guard blocks staff membership mutations only.
func TestApprovalLoopOnPersonalOrg(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID := seedPersonalOrgOwner(t, db)
	clientID := provisionClientOnPersonalOrg(t, db, ownerID, orgID)

	project, err := svc.Create(ctx, ownerID, orgID, CreateProjectRequest{Name: "Personal Approval Loop"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.AssignClient(ctx, ownerID, orgID, project.ID, &clientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}

	milestone, err := svc.CreateMilestone(ctx, orgID, project.ID, ownerID, CreateMilestoneInput{
		Title:       "Milestone One",
		Description: "first milestone",
	})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	// pending -> in_progress, add a deliverable, submit.
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, project.ID, milestone.ID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	addDeliverable(t, svc, ownerID, orgID, project.ID, milestone.ID)

	submitted, err := svc.SubmitMilestone(ctx, ownerID, orgID, project.ID, milestone.ID)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.Status != MilestoneStatusAwaitingApproval {
		t.Fatalf("status after submit = %s, want awaiting_approval", submitted.Status)
	}

	// Client requests changes; staff resubmits (revision 2); client approves.
	changed, err := svc.RequestMilestoneChanges(ctx, clientID, orgID, project.ID, milestone.ID, "revise the hero")
	if err != nil {
		t.Fatalf("changes-requested: %v", err)
	}
	if changed.Status != MilestoneStatusChangesRequested {
		t.Fatalf("status after changes-requested = %s", changed.Status)
	}

	resubmitted, err := svc.SubmitMilestone(ctx, ownerID, orgID, project.ID, milestone.ID)
	if err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if resubmitted.Status != MilestoneStatusAwaitingApproval || resubmitted.RevisionCount != 2 {
		t.Fatalf("resubmitted = %+v, want awaiting_approval with revision_count 2", resubmitted)
	}

	approved, err := svc.ApproveMilestone(ctx, clientID, orgID, project.ID, milestone.ID)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != MilestoneStatusApproved {
		t.Fatalf("status after approve = %s, want approved", approved.Status)
	}

	// The shared approval view resolves for both the client and the staff owner.
	view, err := svc.GetApprovalView(ctx, clientID, "client", orgID, project.ID)
	if err != nil {
		t.Fatalf("GetApprovalView (client): %v", err)
	}
	if len(view.Milestones) != 1 {
		t.Fatalf("approval view milestones = %d, want 1", len(view.Milestones))
	}

	if _, err := svc.GetApprovalView(ctx, ownerID, "owner", orgID, project.ID); err != nil {
		t.Fatalf("GetApprovalView (staff): %v", err)
	}
}
