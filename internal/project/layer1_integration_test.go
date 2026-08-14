package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

// seedClient creates a user holding an active client-role membership in the
// organization.
func seedClient(t *testing.T, db *sql.DB, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var clientID, clientRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Client', 'User')
		RETURNING id
	`, "client-"+uuid.NewString()+"@example.com").Scan(&clientID); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'client' AND organization_id IS NULL
	`).Scan(&clientRoleID); err != nil {
		t.Fatalf("client role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, clientID, orgID, clientRoleID); err != nil {
		t.Fatalf("client membership: %v", err)
	}

	return clientID
}

func assignClient(t *testing.T, svc *Service, actorID, orgID, projectID, clientID uuid.UUID) {
	t.Helper()
	if _, err := svc.AssignClient(context.Background(), actorID, orgID, projectID, &clientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}
}

// setProjectRevisionLimit stamps the project-level revision limit directly
// (no API setter exists yet; the schema supports it).
func setProjectRevisionLimit(t *testing.T, db *sql.DB, projectID uuid.UUID, limit int) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
		UPDATE projects SET revision_limit = $1 WHERE id = $2
	`, limit, projectID); err != nil {
		t.Fatalf("set project revision limit: %v", err)
	}
}

func addDeliverable(t *testing.T, svc *Service, userID, orgID, projectID, milestoneID uuid.UUID) uuid.UUID {
	t.Helper()
	d, err := svc.CreateDeliverable(context.Background(), userID, orgID, projectID, milestoneID,
		"https://figma.com/file/"+uuid.NewString(), nil, nil)
	if err != nil {
		t.Fatalf("create deliverable: %v", err)
	}
	return d.ID
}

func countAuditRows(t *testing.T, db *sql.DB, action string, entityID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM audit_logs WHERE action = $1 AND entity_id = $2
	`, action, entityID).Scan(&count); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return count
}

func countAuthzDenials(t *testing.T, db *sql.DB, userID, resourceID uuid.UUID, permissionKey string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*)
		FROM authz_decisions
		WHERE user_id = $1
		AND resource_id = $2
		AND resource_type = 'project'
		AND allowed = false
		AND permission_key = $3
	`, userID, resourceID, permissionKey).Scan(&count); err != nil {
		t.Fatalf("count denial rows: %v", err)
	}
	return count
}

// TestClientScopeEnforcement proves the no-existence-leak rule: a client can
// read their own project but a cross-project read is indistinguishable from
// not-found AND records a denial row with resource identity.
func TestClientScopeEnforcement(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectA, milestoneA := seedProject(t, db)
	projectB, milestoneB := seedAdditionalProject(t, db, orgID, ownerID)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectA, clientID)

	// The client can read their own project and its milestones.
	if _, err := svc.ViewProject(ctx, clientID, "client", orgID, projectA); err != nil {
		t.Fatalf("client read own project: %v", err)
	}
	if _, err := svc.GetMilestoneDetail(ctx, clientID, "client", orgID, projectA, milestoneA); err != nil {
		t.Fatalf("client read own milestone: %v", err)
	}

	// A staff actor sees both projects.
	if _, err := svc.ViewProject(ctx, ownerID, "owner", orgID, projectB); err != nil {
		t.Fatalf("staff read project B: %v", err)
	}

	// The client cannot read project B: 404, never 403, plus a denial row.
	// The milestone read resolves the same way: the project itself is
	// invisible to the client, so the milestone is project_not_found too.
	_, err := svc.ViewProject(ctx, clientID, "client", orgID, projectB)
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client read of project B error = %v, want ErrProjectNotFound", err)
	}
	_, err = svc.GetMilestoneDetail(ctx, clientID, "client", orgID, projectB, milestoneB)
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client read of milestone B error = %v, want ErrProjectNotFound", err)
	}
	if got := countAuthzDenials(t, db, clientID, projectB, "project.view"); got != 2 {
		t.Fatalf("expected 2 denial rows for cross-project access, got %d", got)
	}
}

// TestApprovalLifecycle drives the full happy path:
// pending -> in_progress -> submit -> awaiting_approval -> approve -> approved.
func TestApprovalLifecycle(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)

	// pending -> in_progress (generic PATCH).
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}

	// Submit without deliverables -> deliverable_required.
	_, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if !errors.Is(err, apperrors.ErrDeliverableRequired) {
		t.Fatalf("submit without deliverables error = %v, want ErrDeliverableRequired", err)
	}

	addDeliverable(t, svc, ownerID, orgID, projectID, milestoneID)

	submitted, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.Status != MilestoneStatusAwaitingApproval {
		t.Fatalf("status after submit = %s, want awaiting_approval", submitted.Status)
	}
	if submitted.RevisionCount != 1 {
		t.Fatalf("revision_count after first submit = %d, want 1", submitted.RevisionCount)
	}

	// The revision history row exists.
	var revisionNumber int
	if err := db.QueryRowContext(ctx, `
		SELECT revision_number FROM milestone_revisions
		WHERE milestone_id = $1 AND organization_id = $2
	`, milestoneID, orgID).Scan(&revisionNumber); err != nil {
		t.Fatalf("read revision row: %v", err)
	}
	if revisionNumber != 1 {
		t.Fatalf("revision_number = %d, want 1", revisionNumber)
	}

	// Submit again while awaiting_approval: idempotent no-op, no extra row.
	resubmitted, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("resubmit (idempotent): %v", err)
	}
	if resubmitted.RevisionCount != 1 {
		t.Fatalf("revision_count after idempotent resubmit = %d, want 1", resubmitted.RevisionCount)
	}

	// Approve (client) -> approved.
	approved, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != MilestoneStatusApproved {
		t.Fatalf("status after approve = %s, want approved", approved.Status)
	}

	// Double-approve: idempotent, exactly one approved audit row.
	_, err = svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("double-approve: %v", err)
	}
	if got := countAuditRows(t, db, "milestone.approved", milestoneID); got != 1 {
		t.Fatalf("approved audit rows = %d, want 1", got)
	}

	// Approve again while already approved must stay a no-op even from a
	// different actor (still idempotent by state, scope check enforces actor).
	if got := countAuditRows(t, db, "milestone.approved", milestoneID); got != 1 {
		t.Fatalf("approved audit rows after third approve = %d, want 1", got)
	}

	// approved -> completed via generic PATCH stamps completed_at.
	completed, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusCompleted)
	if err != nil {
		t.Fatalf("complete approved milestone: %v", err)
	}
	if completed.Status != MilestoneStatusCompleted || completed.CompletedAt == nil {
		t.Fatalf("completed milestone = %+v, want status completed with completed_at", completed)
	}
}

// TestChangesRequestedLifecycle: submit -> changes-requested -> resubmit
// increments the revision counter; changes-requested is not idempotent.
func TestChangesRequestedLifecycle(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)

	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	addDeliverable(t, svc, ownerID, orgID, projectID, milestoneID)

	if _, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Approve from a non-client actor is a 404 (never reveals state).
	_, err := svc.ApproveMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("staff approve error = %v, want ErrProjectNotFound", err)
	}

	// Client requests changes.
	changed, err := svc.RequestMilestoneChanges(ctx, clientID, orgID, projectID, milestoneID, "the hero section needs rework")
	if err != nil {
		t.Fatalf("request changes: %v", err)
	}
	if changed.Status != MilestoneStatusChangesRequested {
		t.Fatalf("status after changes-requested = %s", changed.Status)
	}

	// changes-requested is NOT idempotent: a second call hits the stale state.
	_, err = svc.RequestMilestoneChanges(ctx, clientID, orgID, projectID, milestoneID, "again")
	if !errors.Is(err, apperrors.ErrMilestoneNotAwaitingApproval) {
		t.Fatalf("second changes-requested error = %v, want ErrMilestoneNotAwaitingApproval", err)
	}

	// Resubmission from changes_requested increments the revision counter.
	revised, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("resubmit after changes: %v", err)
	}
	if revised.Status != MilestoneStatusAwaitingApproval || revised.RevisionCount != 2 {
		t.Fatalf("resubmitted milestone = %+v, want awaiting_approval with revision_count 2", revised)
	}

	// Approval now succeeds; exactly two revision rows exist.
	if _, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID); err != nil {
		t.Fatalf("approve after revision: %v", err)
	}
	var revisionCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM milestone_revisions WHERE milestone_id = $1
	`, milestoneID).Scan(&revisionCount); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if revisionCount != 2 {
		t.Fatalf("revision rows = %d, want 2", revisionCount)
	}
}

// TestSubmitPreconditions covers project_has_no_client and invalid transitions.
func TestSubmitPreconditions(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)

	// Submit from 'pending' is an invalid transition.
	_, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("submit from pending error = %v, want ErrInvalidStatusTransition", err)
	}

	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}

	// In-progress with no client -> project_has_no_client.
	_, err = svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if !errors.Is(err, apperrors.ErrProjectHasNoClient) {
		t.Fatalf("submit with no client error = %v, want ErrProjectHasNoClient", err)
	}

	// PATCH to action-only statuses is rejected.
	for _, target := range []MilestoneStatus{MilestoneStatusAwaitingApproval, MilestoneStatusApproved, MilestoneStatusChangesRequested} {
		_, err = svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, target)
		if !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
			t.Fatalf("PATCH to %s error = %v, want ErrInvalidStatusTransition", target, err)
		}
	}
}

// TestStatusPatchBlockedOnArchivedProject: the generic status PATCH is a
// state-machine action and must be blocked on archived/cancelled projects,
// matching submit/approve/changes-requested.
func TestStatusPatchBlockedOnArchivedProject(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)

	// Archive the project (active -> archived is a valid project transition).
	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusArchived}); err != nil {
		t.Fatalf("archive project: %v", err)
	}

	// Every staff PATCH target is blocked while the project is archived.
	for _, target := range []MilestoneStatus{MilestoneStatusInProgress, MilestoneStatusCompleted, MilestoneStatusBlocked, MilestoneStatusCancelled} {
		_, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, target)
		if !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
			t.Fatalf("PATCH to %s on archived project error = %v, want ErrInvalidStatusTransition", target, err)
		}
	}

	// Same-state no-op is also blocked (the action is frozen with the project).
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusPending); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("same-state PATCH on archived project error = %v, want ErrInvalidStatusTransition", err)
	}

	// A cancelled project blocks it too; the project state machine forbids
	// leaving the terminal archived state via the API.
	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusCancelled}); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("archived -> cancelled should be rejected by project state machine, got %v", err)
	}

	// "Un-archive" by restoring the project row directly (no API path exists
	// to leave the terminal archived state), then the PATCH works again.
	if _, err := db.ExecContext(ctx, `
		UPDATE projects SET status = 'active' WHERE id = $1
	`, projectID); err != nil {
		t.Fatalf("restore project: %v", err)
	}
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("PATCH after project restored: %v", err)
	}
}

// TestConcurrentApproveSingleAuditRow proves the approve path serializes on
// the milestone row: two concurrent approvals produce exactly one audit row.
func TestConcurrentApproveSingleAuditRow(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)

	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	addDeliverable(t, svc, ownerID, orgID, projectID, milestoneID)
	if _, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Create a deterministic race window: the first approve holds the locks
	// after reading the milestone; the second blocks until it commits.
	hookCalled := make(chan struct{})
	release := make(chan struct{})
	testHookAfterMilestoneLock = func() {
		close(hookCalled)
		<-release
	}
	defer func() { testHookAfterMilestoneLock = nil }()

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID)
		errs <- err
	}()

	<-hookCalled // first approve holds the project+milestone locks

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID)
		errs <- err
	}()

	time.Sleep(150 * time.Millisecond) // let the second goroutine reach the lock
	close(release)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent approve error: %v", err)
		}
	}

	if got := countAuditRows(t, db, "milestone.approved", milestoneID); got != 1 {
		t.Fatalf("approved audit rows after concurrent approve = %d, want exactly 1", got)
	}

	milestone, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone: %v", err)
	}
	if milestone.Status != MilestoneStatusApproved {
		t.Fatalf("milestone status = %s, want approved", milestone.Status)
	}
}

// TestRevisionLimitReachedIntegration pins limit_reached read-side computation
// with a project-level limit.
func TestRevisionLimitReachedIntegration(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)
	setProjectRevisionLimit(t, db, projectID, 2)

	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	addDeliverable(t, svc, ownerID, orgID, projectID, milestoneID)

	// First submission: revision_count 1, limit 2 -> not reached.
	first, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if first.RevisionLimit == nil || *first.RevisionLimit != 2 {
		t.Fatalf("effective revision limit = %v, want 2", first.RevisionLimit)
	}
	if first.LimitReached {
		t.Fatal("limit_reached should be false at revision 1 of 2")
	}

	// changes-requested, revise, resubmit -> revision_count 2, limit reached.
	if _, err := svc.RequestMilestoneChanges(ctx, clientID, orgID, projectID, milestoneID, "round two please"); err != nil {
		t.Fatalf("request changes: %v", err)
	}
	second, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if second.RevisionCount != 2 || !second.LimitReached {
		t.Fatalf("second submit = revision %d limit_reached %v, want 2 and true", second.RevisionCount, second.LimitReached)
	}

	// Read-back path also computes the flag.
	read, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone: %v", err)
	}
	if !read.LimitReached {
		t.Fatal("read-back limit_reached = false, want true")
	}

	// Milestone-level override beats the project default.
	var override = 10
	if _, err := db.ExecContext(ctx, `
		UPDATE milestones SET revision_limit = $1 WHERE id = $2
	`, override, milestoneID); err != nil {
		t.Fatalf("set milestone revision limit: %v", err)
	}
	read, err = svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone after override: %v", err)
	}
	if read.RevisionLimit == nil || *read.RevisionLimit != 10 {
		t.Fatalf("effective limit after override = %v, want 10", read.RevisionLimit)
	}
	if read.LimitReached {
		t.Fatal("limit_reached should be false with the override (2 < 10)")
	}
}

// TestDeliverablesFrozenStates covers create/delete allowed states and the
// completed/cancelled freeze.
func TestDeliverablesFrozenStates(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)

	// Deliverables are allowed on a pending milestone.
	d1 := addDeliverable(t, svc, ownerID, orgID, projectID, milestoneID)

	// And on an approved milestone (create a second milestone for this check).
	project2, milestone2 := seedAdditionalProject(t, db, orgID, ownerID)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, project2, clientID)
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, project2, milestone2, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone2: %v", err)
	}
	addDeliverable(t, svc, ownerID, orgID, project2, milestone2)
	if _, err := svc.SubmitMilestone(ctx, ownerID, orgID, project2, milestone2); err != nil {
		t.Fatalf("submit milestone2: %v", err)
	}
	if _, err := svc.ApproveMilestone(ctx, clientID, orgID, project2, milestone2); err != nil {
		t.Fatalf("approve milestone2: %v", err)
	}
	if _, err := svc.CreateDeliverable(ctx, ownerID, orgID, project2, milestone2, "https://example.com/approved-state", nil, nil); err != nil {
		t.Fatalf("deliverable on approved milestone should be allowed: %v", err)
	}

	// Freeze after completion.
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, project2, milestone2, MilestoneStatusCompleted); err != nil {
		t.Fatalf("complete milestone2: %v", err)
	}
	_, err := svc.CreateDeliverable(ctx, ownerID, orgID, project2, milestone2, "https://example.com/frozen", nil, nil)
	if !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("deliverable on completed milestone error = %v, want ErrInvalidStatusTransition", err)
	}

	// Delete is allowed on the pending milestone, then frozen on completion.
	if err := svc.DeleteDeliverable(ctx, ownerID, orgID, projectID, milestoneID, d1); err != nil {
		t.Fatalf("delete deliverable on pending milestone: %v", err)
	}
	if err := svc.DeleteDeliverable(ctx, ownerID, orgID, projectID, milestoneID, d1); !errors.Is(err, apperrors.ErrDeliverableNotFound) {
		t.Fatalf("second delete error = %v, want ErrDeliverableNotFound", err)
	}

	// Cancelled milestones are frozen too. The canonical state machine only
	// allows cancellation from awaiting_approval / changes_requested, so take
	// this milestone through a submission first.
	project3, milestone3 := seedAdditionalProject(t, db, orgID, ownerID)
	assignClient(t, svc, ownerID, orgID, project3, clientID)
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, project3, milestone3, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone3: %v", err)
	}
	addDeliverable(t, svc, ownerID, orgID, project3, milestone3)
	if _, err := svc.SubmitMilestone(ctx, ownerID, orgID, project3, milestone3); err != nil {
		t.Fatalf("submit milestone3: %v", err)
	}
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, project3, milestone3, MilestoneStatusCancelled); err != nil {
		t.Fatalf("cancel milestone3: %v", err)
	}
	_, err = svc.CreateDeliverable(ctx, ownerID, orgID, project3, milestone3, "https://example.com/cancelled", nil, nil)
	if !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("deliverable on cancelled milestone error = %v, want ErrInvalidStatusTransition", err)
	}

	// Non-http URL is rejected at the service boundary.
	if _, err := svc.CreateDeliverable(ctx, ownerID, orgID, project3, milestone3, "ftp://example.com/x", nil, nil); !errors.Is(err, apperrors.ErrValidation) {
		t.Fatalf("ftp url error = %v, want ErrValidation", err)
	}
}

// TestPaymentStatusAnyToAny covers the agency-only payment tracking surface.
func TestPaymentStatusAnyToAny(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)

	updated, err := svc.UpdateMilestonePaymentStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestonePaymentStatusPartial)
	if err != nil {
		t.Fatalf("unpaid -> partial: %v", err)
	}
	if updated.PaymentStatus != MilestonePaymentStatusPartial {
		t.Fatalf("payment status = %s, want partial", updated.PaymentStatus)
	}

	if _, err := svc.UpdateMilestonePaymentStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestonePaymentStatusPaid); err != nil {
		t.Fatalf("partial -> paid: %v", err)
	}

	// Payment status is unrestricted by milestone state: even completed.
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("in progress: %v", err)
	}
	if _, err := svc.UpdateMilestonePaymentStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestonePaymentStatusUnpaid); err != nil {
		t.Fatalf("paid -> unpaid: %v", err)
	}

	// Each transition is audited.
	if got := countAuditRows(t, db, "milestone.payment_status_changed", milestoneID); got != 3 {
		t.Fatalf("payment audit rows = %d, want 3", got)
	}
}

// TestApprovalViewShape pins the client-facing deep-link payload: milestones
// carry revision_count + payment_status + deliverables but NOT limit fields.
func TestApprovalViewShape(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)
	setProjectRevisionLimit(t, db, projectID, 1)

	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	addDeliverable(t, svc, ownerID, orgID, projectID, milestoneID)
	if _, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := svc.UpdateMilestonePaymentStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestonePaymentStatusPartial); err != nil {
		t.Fatalf("set payment status: %v", err)
	}

	view, err := svc.GetApprovalView(ctx, clientID, "client", orgID, projectID)
	if err != nil {
		t.Fatalf("approval view: %v", err)
	}
	if view.Project.ID != projectID {
		t.Fatalf("view project id = %v, want %v", view.Project.ID, projectID)
	}
	if len(view.Milestones) != 1 {
		t.Fatalf("view milestones = %d, want 1", len(view.Milestones))
	}

	m := view.Milestones[0]
	if m.RevisionCount != 1 {
		t.Fatalf("view revision_count = %d, want 1", m.RevisionCount)
	}
	if m.PaymentStatus != MilestonePaymentStatusPartial {
		t.Fatalf("view payment_status = %s, want partial", m.PaymentStatus)
	}
	if len(m.Deliverables) != 1 {
		t.Fatalf("view deliverables = %d, want 1", len(m.Deliverables))
	}

	// The client view must not expose the effective limit or the flag.
	rawJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal approval milestone: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(rawJSON, &raw); err != nil {
		t.Fatalf("unmarshal approval milestone: %v", err)
	}
	if _, ok := raw["limit_reached"]; ok {
		t.Fatal("approval view must not expose limit_reached")
	}
	if _, ok := raw["revision_limit"]; ok {
		t.Fatal("approval view must not expose revision_limit")
	}

	// Cross-project approval view is a 404 for the client.
	project2, _ := seedAdditionalProject(t, db, orgID, ownerID)
	if _, err := svc.GetApprovalView(ctx, clientID, "client", orgID, project2); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("cross-project approval view error = %v, want ErrProjectNotFound", err)
	}
}

// TestReassignmentAndUnassign covers client reassignment (old access ends
// immediately AND the displaced client's membership is pruned when they hold
// no other project) and unassignment (membership dropped when no other
// project).
func TestReassignmentAndUnassign(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	clientA := seedClient(t, db, orgID)
	clientB := seedClient(t, db, orgID)

	assignClient(t, svc, ownerID, orgID, projectID, clientA)

	// Client A has access before reassignment.
	if _, err := svc.ViewProject(ctx, clientA, "client", orgID, projectID); err != nil {
		t.Fatalf("client A read before reassign: %v", err)
	}

	// Reassign to client B: client A's access ends immediately.
	assignClient(t, svc, ownerID, orgID, projectID, clientB)

	if _, err := svc.ViewProject(ctx, clientA, "client", orgID, projectID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client A read after reassign error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.ViewProject(ctx, clientB, "client", orgID, projectID); err != nil {
		t.Fatalf("client B read after reassign: %v", err)
	}

	// Client A held no other project, so their membership was pruned by the
	// reassignment (same rule as unassign).
	var clientAMemberships int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, clientA, orgID).Scan(&clientAMemberships); err != nil {
		t.Fatalf("count client A memberships: %v", err)
	}
	if clientAMemberships != 0 {
		t.Fatalf("client A membership count = %d, want 0 after displacement from last project", clientAMemberships)
	}

	// The membership pruning is audited explicitly.
	var pruneAudits int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE action = 'membership.removed' AND organization_id = $1
	`, orgID).Scan(&pruneAudits); err != nil {
		t.Fatalf("count prune audit rows: %v", err)
	}
	if pruneAudits < 1 {
		t.Fatalf("prune audit rows = %d, want >= 1", pruneAudits)
	}

	// Assigning a non-client user is client_not_found.
	staffID := seedProjectHandlerUser(t, db, orgID, membership.RoleMember)
	_, err := svc.AssignClient(ctx, ownerID, orgID, projectID, &staffID)
	if !errors.Is(err, apperrors.ErrClientNotFound) {
		t.Fatalf("assign non-client error = %v, want ErrClientNotFound", err)
	}

	// Unassign client B: B is no longer on any project -> membership removed.
	if _, err := svc.AssignClient(ctx, ownerID, orgID, projectID, nil); err != nil {
		t.Fatalf("unassign: %v", err)
	}

	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, clientB, orgID).Scan(&membershipCount); err != nil {
		t.Fatalf("count client B memberships: %v", err)
	}
	if membershipCount != 0 {
		t.Fatalf("client B membership count = %d, want 0 after unassign from last project", membershipCount)
	}

	// Audit rows for assign + remove.
	if got := countAuditRows(t, db, "project.client_assigned", projectID); got != 2 {
		t.Fatalf("client_assigned audit rows = %d, want 2", got)
	}
	if got := countAuditRows(t, db, "project.client_removed", projectID); got != 1 {
		t.Fatalf("client_removed audit rows = %d, want 1", got)
	}
}

// TestReassignKeepsClientOnOtherProjects: a displaced client who is still the
// client of another project in the org keeps their membership.
func TestReassignKeepsClientOnOtherProjects(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, project1, _ := seedProject(t, db)
	project2, _ := seedAdditionalProject(t, db, orgID, ownerID)
	clientID := seedClient(t, db, orgID)
	replacement := seedClient(t, db, orgID)

	// Client holds TWO projects.
	assignClient(t, svc, ownerID, orgID, project1, clientID)
	assignClient(t, svc, ownerID, orgID, project2, clientID)

	// Reassign on project1 only: client is still on project2 -> membership kept.
	assignClient(t, svc, ownerID, orgID, project1, replacement)

	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, clientID, orgID).Scan(&membershipCount); err != nil {
		t.Fatalf("count client memberships: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("client membership count = %d, want 1 (still on project2)", membershipCount)
	}

	// The client still has access to project2, and no longer to project1.
	if _, err := svc.ViewProject(ctx, clientID, "client", orgID, project2); err != nil {
		t.Fatalf("client read project2 after partial reassign: %v", err)
	}
	if _, err := svc.ViewProject(ctx, clientID, "client", orgID, project1); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client read project1 after partial reassign error = %v, want ErrProjectNotFound", err)
	}
}

// TestTwoOrgClientScoping: a user who is a client in two organizations can
// only read their own assigned project in each, and cross-org reads are 404.
func TestTwoOrgClientScoping(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	var userID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Two', 'Org')
		RETURNING id
	`, "two-org-"+uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert two-org client: %v", err)
	}

	var clientRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'client' AND organization_id IS NULL
	`).Scan(&clientRoleID); err != nil {
		t.Fatalf("client role: %v", err)
	}

	addClientMembership := func(orgID uuid.UUID) {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO memberships (user_id, organization_id, role_id, status)
			VALUES ($1, $2, $3, 'active')
		`, userID, orgID, clientRoleID); err != nil {
			t.Fatalf("insert client membership: %v", err)
		}
	}

	// Org A: owner, project A1 (assigned), project A2 (not assigned).
	ownerA, orgA, projectA1, _ := seedProject(t, db)
	projectA2, _ := seedAdditionalProject(t, db, orgA, ownerA)
	addClientMembership(orgA)
	assignClient(t, svc, ownerA, orgA, projectA1, userID)

	// Org B: owner, project B1 (assigned), project B2 (not assigned).
	ownerB, orgB, projectB1, _ := seedProject(t, db)
	projectB2, _ := seedAdditionalProject(t, db, orgB, ownerB)
	addClientMembership(orgB)
	assignClient(t, svc, ownerB, orgB, projectB1, userID)

	// Reads resolve within each org.
	if _, err := svc.ViewProject(ctx, userID, "client", orgA, projectA1); err != nil {
		t.Fatalf("client read A1: %v", err)
	}
	if _, err := svc.ViewProject(ctx, userID, "client", orgB, projectB1); err != nil {
		t.Fatalf("client read B1: %v", err)
	}

	// Unassigned projects in the same org are 404.
	if _, err := svc.ViewProject(ctx, userID, "client", orgA, projectA2); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client read A2 error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.ViewProject(ctx, userID, "client", orgB, projectB2); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client read B2 error = %v, want ErrProjectNotFound", err)
	}

	// Cross-org reads are 404 (no existence leak across tenants).
	if _, err := svc.ViewProject(ctx, userID, "client", orgA, projectB1); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client read B1 through org A error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.ViewProject(ctx, userID, "client", orgB, projectA1); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client read A1 through org B error = %v, want ErrProjectNotFound", err)
	}
}

// TestProjectScopedActivity verifies the project-scoped feed returns only that
// project's events, and is 404 for a client on another project.
func TestProjectScopedActivity(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectA, _ := seedProject(t, db)
	projectB, _ := seedAdditionalProject(t, db, orgID, ownerID)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectA, clientID)

	// Create an event on each project.
	if _, err := svc.Update(ctx, ownerID, orgID, projectA, UpdateProjectRequest{Name: "Renamed A"}); err != nil {
		t.Fatalf("update project A: %v", err)
	}
	if _, err := svc.Update(ctx, ownerID, orgID, projectB, UpdateProjectRequest{Name: "Renamed B"}); err != nil {
		t.Fatalf("update project B: %v", err)
	}

	// Staff sees only project A's events when scoped to A.
	feed, meta, err := svc.ListProjectActivities(ctx, ownerID, "owner", orgID, projectA, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("list project A activities: %v", err)
	}
	if meta.Total < 1 {
		t.Fatalf("project A activity total = %d, want >= 1", meta.Total)
	}
	for _, a := range feed {
		if a.ProjectID == nil || *a.ProjectID != projectA {
			t.Fatalf("activity %s attached to wrong project %v", a.Type, a.ProjectID)
		}
	}

	// Client on project A sees A's feed; client on project B is a 404.
	if _, _, err := svc.ListProjectActivities(ctx, clientID, "client", orgID, projectA, pagination.Query{Page: 1, Limit: 20}); err != nil {
		t.Fatalf("client project A activities: %v", err)
	}
	if _, _, err := svc.ListProjectActivities(ctx, clientID, "client", orgID, projectB, pagination.Query{Page: 1, Limit: 20}); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client project B activities error = %v, want ErrProjectNotFound", err)
	}
}
