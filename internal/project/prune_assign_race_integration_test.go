package project

// Prune-vs-assign race verification.
//
// A reassign on P1 (X -> Y) displaces client X and prunes X's membership once
// X holds no other project in the org. Concurrently, X can be assigned to
// another project P2. Under the pre-fix code the prune's project check and the
// P2-assign's membership check could interleave so that both committed: P2
// referenced X, X's membership was deleted underneath it, and the org was left
// with an orphaned project reference.
//
// The fix serializes both paths on the displaced client's membership row: the
// prune acquires it FOR UPDATE, the assign's IsActiveClientMember acquires it
// FOR SHARE. Exactly one consistent outcome results:
//   - the other assign commits first  -> the prune's re-evaluated project check
//     sees P2 and SKIPS the delete (X stays a client on P2);
//   - the prune wins                  -> the P2-assign's re-evaluated membership
//     check finds the row gone and aborts with client_not_found.
//
// The deterministic tests force each ordering with a raw replay transaction
// while the real service runs; the stress test runs the two real service calls
// concurrently for many iterations. Never: an orphaned project reference, a
// dangling membership, or a 500.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
)

// newPruneRaceFixture seeds an org with clients X and Y, project P1 assigned
// to X, and an unassigned project P2: the substrate for the prune race — a
// concurrent reassign of P1 (X -> Y, which prunes X) and an assign of X to P2.
func newPruneRaceFixture(t *testing.T, db *sql.DB, svc *Service) (ownerID, orgID, p1ID, p2ID, clientX, clientY uuid.UUID) {
	t.Helper()

	ownerID, orgID, p1ID, _ = seedProject(t, db)
	p2ID, _ = seedAdditionalProject(t, db, orgID, ownerID)
	clientX = seedClient(t, db, orgID)
	clientY = seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, p1ID, clientX)

	t.Cleanup(func() { cleanupPruneFixture(t, db, orgID, ownerID, clientX, clientY) })
	return ownerID, orgID, p1ID, p2ID, clientX, clientY
}

// cleanupPruneFixture deletes every row the fixture created, in FK-safe order,
// so repeated runs and stress iterations never grow the shared test database.
func cleanupPruneFixture(t *testing.T, db *sql.DB, orgID uuid.UUID, userIDs ...uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM activities WHERE organization_id = $1`,
		`DELETE FROM audit_logs WHERE organization_id = $1`,
		`DELETE FROM authz_decisions WHERE organization_id = $1`,
		`DELETE FROM projects WHERE organization_id = $1`,
		`DELETE FROM memberships WHERE organization_id = $1`,
	} {
		if _, err := db.ExecContext(ctx, stmt, orgID); err != nil {
			t.Errorf("cleanup %q: %v", stmt, err)
			return
		}
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM organizations WHERE id = $1`, orgID); err != nil {
		t.Errorf("cleanup organization: %v", err)
		return
	}
	for _, userID := range userIDs {
		if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user %s: %v", userID, err)
			return
		}
	}
}

// orphanedClientCount counts projects whose client_id is set but references a
// user holding no active client-role membership in the org — the corruption
// the prune race must never produce.
func orphanedClientCount(t *testing.T, db *sql.DB, orgID uuid.UUID) int {
	t.Helper()
	ctx := context.Background()
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM projects p
		WHERE p.organization_id = $1
		AND p.client_id IS NOT NULL
		AND NOT EXISTS (
			SELECT 1
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			WHERE m.organization_id = p.organization_id
			AND m.user_id = p.client_id
			AND m.status = 'active'
			AND r.name = 'client'
		)
	`, orgID).Scan(&count); err != nil {
		t.Fatalf("count orphaned project clients: %v", err)
	}
	return count
}

// projectClientID reads the project's client_id (nil when unassigned).
func projectClientID(t *testing.T, db *sql.DB, projectID uuid.UUID) *uuid.UUID {
	t.Helper()
	var clientID *uuid.UUID
	if err := db.QueryRowContext(context.Background(),
		`SELECT client_id FROM projects WHERE id = $1`, projectID).Scan(&clientID); err != nil {
		t.Fatalf("read project client_id: %v", err)
	}
	return clientID
}

// clientMembershipCount counts the user's membership rows in the org.
func clientMembershipCount(t *testing.T, db *sql.DB, orgID, userID uuid.UUID) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM memberships WHERE organization_id = $1 AND user_id = $2
	`, orgID, userID).Scan(&count); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	return count
}

// TestPruneAssignRaceDeterministicOtherAssignCommitsFirst pins the ordering
// where the concurrent assign of the displaced client to P2 wins. The replay
// transaction holds X's membership with FOR SHARE — exactly the lock the
// assign's IsActiveClientMember takes — while the REAL reassign service runs.
// The reassign's prune (FOR UPDATE) must block on that lock, the replay
// commits the P2 assignment, and the prune's re-evaluated project check must
// then see P2 and SKIP the delete: X stays a client on P2.
func TestPruneAssignRaceDeterministicOtherAssignCommitsFirst(t *testing.T) {
	db := integrationDB(t)
	// Registered first so it runs LAST (t.Cleanup is LIFO): the fixture
	// cleanup deletes rows against a live connection.
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, p1ID, p2ID, clientX, clientY := newPruneRaceFixture(t, db, svc)

	// Replay the concurrent P2-assign's window: hold X's membership FOR SHARE
	// (the lock IsActiveClientMember takes) and keep the transaction open.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin replay tx: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM memberships WHERE organization_id = $1 AND user_id = $2 FOR SHARE
	`, orgID, clientX).Scan(new(uuid.UUID)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay hold displaced membership FOR SHARE: %v", err)
	}

	// The REAL reassign: X is displaced by Y on P1, and the prune of X blocks
	// on the replay's FOR SHARE lock — the serialization under test. The
	// goroutine/join pattern makes a stuck lock surface as a failure.
	type outcome struct {
		err error
	}
	reassignDone := make(chan outcome, 1)
	go func() {
		_, err := svc.AssignClient(ctx, ownerID, orgID, p1ID, &clientY)
		reassignDone <- outcome{err}
	}()

	// Give the reassign time to reach the blocked prune.
	time.Sleep(300 * time.Millisecond)

	// The other assign commits first: X is now the client of P2.
	if _, err := tx.ExecContext(ctx, `
		UPDATE projects SET client_id = $1, updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`, clientX, p2ID, orgID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay commit P2 assignment: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit replay tx: %v", err)
	}

	var res outcome
	select {
	case res = <-reassignDone:
	case <-time.After(10 * time.Second):
		t.Fatal("reassign never returned after replay commit (stuck lock: deadlock or hung goroutine)")
	}
	if res.err != nil {
		t.Fatalf("reassign = %v, want nil: the displaced client is still on P2, so the prune "+
			"must SKIP the membership delete", res.err)
	}

	// The prune skipped the delete: X keeps its membership and stays the client
	// of P2; Y is the new client of P1. No orphan anywhere.
	if got := projectClientID(t, db, p1ID); got == nil || *got != clientY {
		t.Fatalf("P1 client = %v, want %s", got, clientY)
	}
	if got := projectClientID(t, db, p2ID); got == nil || *got != clientX {
		t.Fatalf("P2 client = %v, want %s (the other assign committed first)", got, clientX)
	}
	if n := clientMembershipCount(t, db, orgID, clientX); n != 1 {
		t.Fatalf("X membership count = %d, want 1 (prune must have skipped the delete)", n)
	}
	if n := clientMembershipCount(t, db, orgID, clientY); n != 1 {
		t.Fatalf("Y membership count = %d, want 1", n)
	}
	if n := orphanedClientCount(t, db, orgID); n != 0 {
		t.Fatalf("orphaned project references = %d, want 0", n)
	}
}

// TestPruneAssignRaceDeterministicPruneWins pins the reverse ordering: the
// reassign's prune gets the displaced client's membership lock first. The
// replay transaction replays the prune's exact statements (FOR UPDATE lock,
// no-other-project check, DELETE) and commits while BOTH real services run.
// The P2-assign's FOR SHARE membership check must block on the prune's lock
// and then re-evaluate against the committed delete -> client_not_found; the
// reassign commits cleanly.
func TestPruneAssignRaceDeterministicPruneWins(t *testing.T) {
	db := integrationDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, p1ID, p2ID, clientX, clientY := newPruneRaceFixture(t, db, svc)

	// Replay the prune's exact ordering in an open transaction:
	// LockClientMembership (FOR UPDATE), IsClientOnAnyOtherProject (X is on P1
	// only, which is excluded), DeleteClientMembership.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin replay tx: %v", err)
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM memberships WHERE organization_id = $1 AND user_id = $2 FOR UPDATE
	`, orgID, clientX).Scan(new(uuid.UUID)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay prune LockClientMembership: %v", err)
	}
	var onOther bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM projects
			WHERE organization_id = $1 AND client_id = $2 AND id <> $3
		)
	`, orgID, clientX, p1ID).Scan(&onOther); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay prune IsClientOnAnyOtherProject: %v", err)
	}
	if onOther {
		_ = tx.Rollback()
		t.Fatal("precondition broken: X is already on another project")
	}
	if err := tx.QueryRowContext(ctx, `
		DELETE FROM memberships WHERE organization_id = $1 AND user_id = $2 RETURNING id
	`, orgID, clientX).Scan(new(uuid.UUID)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay prune DeleteClientMembership: %v", err)
	}

	// Both REAL services run while the prune's lock is held: the reassign
	// displaces X on P1 (its own prune blocks on the replay lock) and the
	// P2-assign's membership check (FOR SHARE) blocks on the same lock.
	type outcome struct {
		err error
	}
	reassignDone := make(chan outcome, 1)
	assignDone := make(chan outcome, 1)
	go func() {
		_, err := svc.AssignClient(ctx, ownerID, orgID, p1ID, &clientY)
		reassignDone <- outcome{err}
	}()
	go func() {
		_, err := svc.AssignClient(ctx, ownerID, orgID, p2ID, &clientX)
		assignDone <- outcome{err}
	}()

	// Give both services time to reach their blocked reads.
	time.Sleep(300 * time.Millisecond)

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit replay tx: %v", err)
	}

	var reassignRes, assignRes outcome
	select {
	case reassignRes = <-reassignDone:
	case <-time.After(10 * time.Second):
		t.Fatal("reassign never returned after prune committed (stuck lock: deadlock or hung goroutine)")
	}
	select {
	case assignRes = <-assignDone:
	case <-time.After(10 * time.Second):
		t.Fatal("assign never returned after prune committed (stuck lock: deadlock or hung goroutine)")
	}

	if reassignRes.err != nil {
		t.Fatalf("reassign = %v, want nil (prune won; X's membership is gone so its own prune skips)", reassignRes.err)
	}
	if !errors.Is(assignRes.err, apperrors.ErrClientNotFound) {
		t.Fatalf("assign to P2 = %v, want ErrClientNotFound (prune won: X's membership was deleted)", assignRes.err)
	}

	// Prune won: X's membership is gone, P2 stayed unassigned, P1 now has Y.
	if got := projectClientID(t, db, p1ID); got == nil || *got != clientY {
		t.Fatalf("P1 client = %v, want %s", got, clientY)
	}
	if got := projectClientID(t, db, p2ID); got != nil {
		t.Fatalf("P2 client = %v, want NULL (the assign aborted with client_not_found)", got)
	}
	if n := clientMembershipCount(t, db, orgID, clientX); n != 0 {
		t.Fatalf("X membership count = %d, want 0 (pruned)", n)
	}
	if n := clientMembershipCount(t, db, orgID, clientY); n != 1 {
		t.Fatalf("Y membership count = %d, want 1", n)
	}
	if n := orphanedClientCount(t, db, orgID); n != 0 {
		t.Fatalf("orphaned project references = %d, want 0", n)
	}
}

// TestPruneAssignRaceStress runs the two real service calls concurrently for
// many iterations: a reassign on P1 (displacing X and pruning X's membership)
// racing an assign of X to P2. Every iteration must resolve to exactly one
// consistent outcome — the other assign wins (the prune skips, X stays a
// client on P2) or the prune wins (the assign 404s, P2 stays unassigned) —
// and never an orphaned project reference or a 500.
func TestPruneAssignRaceStress(t *testing.T) {
	db := integrationDB(t)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	svc := newTestService(db)

	const iterations = 40

	for i := 0; i < iterations; i++ {
		ownerID, orgID, p1ID, p2ID, clientX, clientY := newPruneRaceFixture(t, db, svc)

		var wg sync.WaitGroup
		var reassignErr, assignErr error
		var mu sync.Mutex

		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := svc.AssignClient(ctx, ownerID, orgID, p1ID, &clientY)
			mu.Lock()
			reassignErr = err
			mu.Unlock()
		}()
		go func() {
			defer wg.Done()
			_, err := svc.AssignClient(ctx, ownerID, orgID, p2ID, &clientX)
			mu.Lock()
			assignErr = err
			mu.Unlock()
		}()
		wg.Wait()

		// The reassign must always commit; the P2-assign either commits (the
		// prune skipped the delete) or 404s (the prune won). Nothing else may
		// surface.
		if reassignErr != nil {
			t.Fatalf("iteration %d: reassign error = %v (must always commit)", i, reassignErr)
		}
		if assignErr != nil && !errors.Is(assignErr, apperrors.ErrClientNotFound) {
			t.Fatalf("iteration %d: assign error = %v (unexpected)", i, assignErr)
		}

		// The corruption this race must never produce: any project referencing
		// a client without an active client-role membership.
		if n := orphanedClientCount(t, db, orgID); n != 0 {
			t.Fatalf("iteration %d: ORPHANED project references = %d (reassign=%v assign=%v). "+
				"The prune race produced a corrupt state instead of exactly one consistent outcome.",
				i, n, reassignErr, assignErr)
		}

		// Outcome-specific cross-checks.
		if got := projectClientID(t, db, p1ID); got == nil || *got != clientY {
			t.Fatalf("iteration %d: P1 client = %v, want %s", i, got, clientY)
		}
		assignWon := assignErr == nil
		if assignWon {
			// The other assign committed first -> the prune skipped -> X keeps
			// the membership and is the client of P2.
			if got := projectClientID(t, db, p2ID); got == nil || *got != clientX {
				t.Fatalf("iteration %d: assign won but P2 client = %v, want %s", i, got, clientX)
			}
			if n := clientMembershipCount(t, db, orgID, clientX); n != 1 {
				t.Fatalf("iteration %d: assign won but X membership = %d, want 1 (prune must have skipped)", i, n)
			}
		} else {
			// The prune won -> the assign 404'd -> P2 stayed unassigned, X's
			// membership is gone.
			if got := projectClientID(t, db, p2ID); got != nil {
				t.Fatalf("iteration %d: prune won but P2 client = %v, want NULL", i, got)
			}
			if n := clientMembershipCount(t, db, orgID, clientX); n != 0 {
				t.Fatalf("iteration %d: prune won but X membership = %d, want 0", i, n)
			}
		}
		if n := clientMembershipCount(t, db, orgID, clientY); n != 1 {
			t.Fatalf("iteration %d: Y membership = %d, want 1 (never touched by the race)", i, n)
		}
	}

	t.Log(fmt.Sprintf("prune/assign race stress: %d iterations, consistent outcomes only", iterations))
}
