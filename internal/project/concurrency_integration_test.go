package project

// Live concurrency-lock tests: same-resource mutations must serialize on the
// org-scoped FOR UPDATE row locks. Two deterministic lock-holder tests prove a
// blocked writer observes the committed state after the holder commits (no
// stale reads, no lost updates), and a double-submit stress proves the
// revision counter increments exactly once under true concurrency.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

// TestConcurrentProjectUpdateObservesCommittedState holds the project row
// FOR UPDATE in a raw transaction, then fires svc.Update concurrently. The
// Update must block until the holder commits and must apply its change on top
// of the committed state — the raw tx's name and the service's description
// both survive (no lost update).
func TestConcurrentProjectUpdateObservesCommittedState(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)

	// Holder acquires the project row lock and changes the name.
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	if _, err := holder.ExecContext(ctx, `
		SELECT id FROM projects WHERE id = $1 AND organization_id = $2 FOR UPDATE
	`, projectID, orgID); err != nil {
		_ = holder.Rollback()
		t.Fatalf("hold project lock: %v", err)
	}
	if _, err := holder.ExecContext(ctx, `
		UPDATE projects SET name = 'held-name' WHERE id = $1
	`, projectID); err != nil {
		_ = holder.Rollback()
		t.Fatalf("update in holder: %v", err)
	}

	// The service update blocks on the row lock.
	type result struct {
		project *Project
		err     error
	}
	done := make(chan result, 1)
	go func() {
		p, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Description: "written-after-holder"})
		done <- result{p, err}
	}()

	// Give the service goroutine time to reach the blocking read.
	time.Sleep(300 * time.Millisecond)
	if err := holder.Commit(); err != nil {
		t.Fatalf("commit holder: %v", err)
	}

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("concurrent Update after holder commit: %v", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Update never returned after holder committed (blocked beyond timeout)")
	}

	// Both writes survived: no lost update, no stale overwrite.
	project, err := svc.GetByID(ctx, orgID, projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	if project.Name != "held-name" {
		t.Fatalf("project name = %q, want holder's committed 'held-name'", project.Name)
	}
	if project.Description == nil || *project.Description != "written-after-holder" {
		t.Fatalf("project description = %v, want the service's committed description", project.Description)
	}
}

// TestConcurrentMilestoneTransitionObservesCommittedState holds the milestone
// row FOR UPDATE in a raw transaction and fires UpdateMilestoneStatus
// (pending -> in_progress) concurrently. After the holder commits a state
// change, the blocked transition must observe the committed status and reject
// the now-invalid transition — never a 500 and never a stale-state write.
func TestConcurrentMilestoneTransitionObservesCommittedState(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)

	// Holder locks the milestone row and cancels the milestone directly
	// (bypassing the state machine — this is a raw DB writer, not the API).
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	if _, err := holder.ExecContext(ctx, `
		SELECT id FROM milestones WHERE id = $1 AND project_id = $2 AND organization_id = $3 FOR UPDATE
	`, milestoneID, projectID, orgID); err != nil {
		_ = holder.Rollback()
		t.Fatalf("hold milestone lock: %v", err)
	}
	if _, err := holder.ExecContext(ctx, `
		UPDATE milestones SET status = 'cancelled' WHERE id = $1
	`, milestoneID); err != nil {
		_ = holder.Rollback()
		t.Fatalf("cancel in holder: %v", err)
	}

	type result struct {
		milestone *Milestone
		err       error
	}
	done := make(chan result, 1)
	go func() {
		m, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress)
		done <- result{m, err}
	}()

	time.Sleep(300 * time.Millisecond)
	if err := holder.Commit(); err != nil {
		t.Fatalf("commit holder: %v", err)
	}

	var res result
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("UpdateMilestoneStatus never returned after holder committed")
	}

	// The service saw the committed 'cancelled' state: the pending->in_progress
	// transition is invalid and must be rejected, not silently applied.
	if !errors.Is(res.err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("concurrent transition error = %v, want ErrInvalidStatusTransition (stale read or 500)", res.err)
	}

	milestone, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone: %v", err)
	}
	if milestone.Status != MilestoneStatusCancelled {
		t.Fatalf("milestone status = %s, want cancelled (holder's committed state preserved)", milestone.Status)
	}
}

// TestConcurrentDoubleSubmitSingleRevision: two concurrent submissions of the
// same milestone must serialize on the milestone row. Exactly one transition
// may commit; the second must observe awaiting_approval and no-op. The
// revision counter must be exactly 1 and exactly one milestone_revisions row
// may exist (no lost update / double increment).
func TestConcurrentDoubleSubmitSingleRevision(t *testing.T) {
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

	const workers = 2
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent submit error: %v (no 500s or state-machine failures allowed)", err)
		}
	}

	// revision_count must be exactly 1 (the second submit must have no-op'd).
	var revisionCount int
	if err := db.QueryRowContext(ctx, `SELECT revision_count FROM milestones WHERE id = $1`, milestoneID).Scan(&revisionCount); err != nil {
		t.Fatalf("read revision_count: %v", err)
	}
	if revisionCount != 1 {
		t.Fatalf("revision_count after concurrent double submit = %d, want 1 (lost-update or double increment)", revisionCount)
	}

	var revisionRows int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM milestone_revisions WHERE milestone_id = $1
	`, milestoneID).Scan(&revisionRows); err != nil {
		t.Fatalf("count revision rows: %v", err)
	}
	if revisionRows != 1 {
		t.Fatalf("milestone_revisions rows = %d, want exactly 1", revisionRows)
	}

	milestone, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone: %v", err)
	}
	if milestone.Status != MilestoneStatusAwaitingApproval {
		t.Fatalf("milestone status = %s, want awaiting_approval", milestone.Status)
	}
}
