package client

// Client-removal vs assign race verification.
//
// DELETE /orgs/{orgID}/clients/X (client.provision) and
// PUT .../projects/{projectID}/client {client_id: X} (project.client.assign)
// are expected to produce EXACTLY ONE consistent outcome under concurrency:
//   - assign wins  -> removal 409 client_attached_to_project, membership intact
//   - removal wins -> assign 404 client_not_found, project.client_id stays NULL
//
// Never: both succeed with an orphaned project reference (project.client_id
// pointing at a user who no longer holds a client membership), a dangling
// membership, or a 500.
//
// The deterministic test replays the removal's statement ordering as a raw
// transaction and runs the REAL assign service while the removal's FOR UPDATE
// membership lock is held open: the assign's FOR SHARE read (IsActiveClientMember)
// blocks on it — that blocking IS the serialization the lock design adds. When
// the removal commits, the assign re-evaluates against the committed state and
// aborts with client_not_found. The stress test runs the two real service calls
// concurrently for many iterations.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/project"
)

// raceFixture holds the entities one assign-vs-remove iteration needs.
type raceFixture struct {
	orgID, staffID, clientID, projectID uuid.UUID
}

// newRaceFixture seeds an org, a staff owner, an unassigned client X, and a
// project P with no client.
func newRaceFixture(t *testing.T, db *sql.DB) raceFixture {
	t.Helper()
	ctx := context.Background()

	var f raceFixture
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Race', 'Staff') RETURNING id
	`, "race-staff-"+uuid.NewString()+"@example.com").Scan(&f.staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO organizations (name) VALUES ($1) RETURNING id
	`, "Race Org "+uuid.NewString()).Scan(&f.orgID); err != nil {
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
	`, f.staffID, f.orgID, ownerRoleID); err != nil {
		t.Fatalf("staff membership: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Race', 'Client') RETURNING id
	`, "race-client-"+uuid.NewString()+"@example.com").Scan(&f.clientID); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	var clientRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'client' AND organization_id IS NULL
	`).Scan(&clientRoleID); err != nil {
		t.Fatalf("client role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, f.clientID, f.orgID, clientRoleID); err != nil {
		t.Fatalf("client membership: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		INSERT INTO projects (organization_id, created_by, name, status)
		VALUES ($1, $2, $3, 'active') RETURNING id
	`, f.orgID, f.staffID, "Race Project "+uuid.NewString()).Scan(&f.projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	return f
}

// isOrphaned reports the corruption the race must never produce: the project
// references X as its client while X holds no active client membership in the
// org (the membership the assignment relied on was removed underneath it).
func isOrphaned(t *testing.T, db *sql.DB, orgID, clientID, projectID uuid.UUID) bool {
	t.Helper()
	ctx := context.Background()

	var clientRef *uuid.UUID
	if err := db.QueryRowContext(ctx, `SELECT client_id FROM projects WHERE id = $1`, projectID).Scan(&clientRef); err != nil {
		t.Fatalf("read project client_id: %v", err)
	}
	if clientRef == nil || *clientRef != clientID {
		return false
	}

	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1 AND m.user_id = $2 AND m.status = 'active' AND r.name = 'client'
	`, orgID, clientID).Scan(&membershipCount); err != nil {
		t.Fatalf("count client memberships: %v", err)
	}
	return membershipCount == 0
}

// cleanupRaceFixture deletes every row a fixture created, in FK-safe order, so
// the long-running stress iterations and repeated runs never grow the shared
// test database. Registered via t.Cleanup so it also runs on fatal failures.
func cleanupRaceFixture(t *testing.T, db *sql.DB, orgID uuid.UUID, userIDs ...uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
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
	})
}

// TestAssignRemoveRaceDeterministicRepro pins the serialization the lock
// design adds: the removal's FOR UPDATE membership lock makes the assign's
// FOR SHARE membership read (IsActiveClientMember) block until the removal
// commits, after which the assign re-evaluates against the committed state and
// aborts with client_not_found. The replay transaction holds the removal's
// lock window open while the REAL assign service runs, so the interleaving
// that previously corrupted state (assign succeeding inside the removal
// window) is provably impossible: the assign simply cannot pass its membership
// check until the removal has finished.
func TestAssignRemoveRaceDeterministicRepro(t *testing.T) {
	db := integrationDB(t)
	// Registered first so it runs LAST (t.Cleanup is LIFO): the fixture
	// cleanup deletes rows against a live connection.
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	clientSvc := newTestService(db)
	projectSvc := project.NewService(db, project.NewRepository(db), audit.NewService(db), activity.NewService(activity.NewRepository(db)))

	f := newRaceFixture(t, db)
	cleanupRaceFixture(t, db, f.orgID, f.staffID, f.clientID)

	// Replay the removal's ordering in a raw transaction (same statements the
	// service runs, same order: GetClientUser, LockClientMembership,
	// ClientHasProjects). The transaction is deliberately kept open after the
	// project-attachment check so the real assign blocks on the FOR UPDATE
	// membership lock — that blocking is the serialization being verified.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin replay tx: %v", err)
	}

	// GetClientUser (plain read — client membership exists).
	if err := tx.QueryRowContext(ctx, `
		SELECT u.id FROM memberships m
		JOIN users u ON u.id = m.user_id
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1 AND u.id = $2 AND m.status = 'active' AND r.name = 'client'
	`, f.orgID, f.clientID).Scan(new(uuid.UUID)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay GetClientUser: %v", err)
	}

	// LockClientMembership (FOR UPDATE on the membership row).
	if err := tx.QueryRowContext(ctx, `
		SELECT id FROM memberships WHERE organization_id = $1 AND user_id = $2 FOR UPDATE
	`, f.orgID, f.clientID).Scan(new(uuid.UUID)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay LockClientMembership: %v", err)
	}

	// ClientHasProjects — evaluated while the replay holds the lock, before
	// the assign can possibly commit; it sees no project reference.
	var hasProjects bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM projects WHERE organization_id = $1 AND client_id = $2)
	`, f.orgID, f.clientID).Scan(&hasProjects); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay ClientHasProjects: %v", err)
	}
	if hasProjects {
		_ = tx.Rollback()
		t.Fatal("precondition broken: project already references the client")
	}

	// Start the REAL assign service. Its IsActiveClientMember read takes
	// FOR SHARE on the membership row, which conflicts with the replay tx's
	// FOR UPDATE lock, so the assign blocks until the removal commits. The
	// assign's own 5s query timeout is the worst-case bound on the block; the
	// replay commits promptly after the delete below, so a healthy run
	// finishes well inside the window. The goroutine/join pattern makes a
	// genuinely stuck lock surface as a test failure, not a hang.
	type assignOutcome struct {
		err error
	}
	assignDone := make(chan assignOutcome, 1)
	go func() {
		_, err := projectSvc.AssignClient(ctx, f.staffID, f.orgID, f.projectID, &f.clientID)
		assignDone <- assignOutcome{err}
	}()

	// Give the assign goroutine time to reach the blocking FOR SHARE read.
	time.Sleep(300 * time.Millisecond)

	// Replay DeleteClientMembership + commit, exactly as the service would.
	if err := tx.QueryRowContext(ctx, `
		DELETE FROM memberships WHERE organization_id = $1 AND user_id = $2 RETURNING id
	`, f.orgID, f.clientID).Scan(new(uuid.UUID)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("replay DeleteClientMembership: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit replay tx: %v", err)
	}

	// Join the assign. The blocked FOR SHARE read re-evaluates against the
	// committed state, finds the membership row gone (the removal won), and the
	// assign aborts with client_not_found. Any other outcome — especially nil,
	// the old broken semantics — is the corruption this test pins.
	var outcome assignOutcome
	select {
	case outcome = <-assignDone:
	case <-time.After(10 * time.Second):
		t.Fatal("assign never returned after replay commit (stuck lock: deadlock or hung goroutine)")
	}
	if !errors.Is(outcome.err, apperrors.ErrClientNotFound) {
		t.Fatalf("assign after removal committed = %v, want ErrClientNotFound (removal won; "+
			"an assign success here is the old broken semantics)", outcome.err)
	}

	// Final state: removal won, so the project has no client and the
	// membership row is gone — never an orphaned project reference.
	var clientRef *uuid.UUID
	if err := db.QueryRowContext(ctx, `SELECT client_id FROM projects WHERE id = $1`, f.projectID).Scan(&clientRef); err != nil {
		t.Fatalf("read project client_id: %v", err)
	}
	if clientRef != nil {
		t.Fatalf("project client_id = %v after removal won, want NULL", clientRef)
	}
	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, f.clientID, f.orgID).Scan(&membershipCount); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if membershipCount != 0 {
		t.Fatalf("membership count = %d after removal won, want 0", membershipCount)
	}

	// Real removal service on the same target: the membership is gone, so it
	// must 404 — proving the removal half alone behaves.
	if err := clientSvc.Remove(ctx, f.staffID, f.orgID, f.clientID); !errors.Is(err, apperrors.ErrClientNotFound) {
		t.Fatalf("Remove after replay = %v, want ErrClientNotFound", err)
	}
}

// TestAssignRemoveRaceStress runs the two real service calls concurrently for
// many iterations and asserts the final state is always one consistent outcome
// — no orphan reference, no dangling membership, no 500.
func TestAssignRemoveRaceStress(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	clientSvc := newTestService(db)
	projectSvc := project.NewService(db, project.NewRepository(db), audit.NewService(db), activity.NewService(activity.NewRepository(db)))

	const iterations = 40

	for i := 0; i < iterations; i++ {
		f := newRaceFixture(t, db)

		var wg sync.WaitGroup
		var assignErr, removeErr error
		var mu sync.Mutex

		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := projectSvc.AssignClient(ctx, f.staffID, f.orgID, f.projectID, &f.clientID)
			mu.Lock()
			assignErr = err
			mu.Unlock()
		}()
		go func() {
			defer wg.Done()
			err := clientSvc.Remove(ctx, f.staffID, f.orgID, f.clientID)
			mu.Lock()
			removeErr = err
			mu.Unlock()
		}()
		wg.Wait()

		// No unexpected errors may surface (assign: nil or client_not_found;
		// remove: nil or client_attached_to_project).
		if assignErr != nil && !errors.Is(assignErr, apperrors.ErrClientNotFound) {
			t.Fatalf("iteration %d: assign error = %v (unexpected)", i, assignErr)
		}
		if removeErr != nil && !errors.Is(removeErr, apperrors.ErrClientAttachedToProject) {
			t.Fatalf("iteration %d: remove error = %v (unexpected)", i, removeErr)
		}

		// Exactly one consistent outcome.
		assignWon := assignErr == nil
		removeWon := removeErr == nil
		if !assignWon && !removeWon {
			t.Fatalf("iteration %d: both failed (assign=%v remove=%v), impossible under the intended semantics", i, assignErr, removeErr)
		}

		// Cross-check the invariant for whichever side won.
		if isOrphaned(t, db, f.orgID, f.clientID, f.projectID) {
			t.Fatalf(
				"iteration %d: ORPHANED project reference (project %s -> client %s) with no active client membership; "+
					"assign=%v remove=%v. The race produced a corrupt state instead of exactly one consistent outcome.",
				i, f.projectID, f.clientID, assignErr, removeErr,
			)
		}

		// Users row must always survive removal.
		var userCount int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = $1`, f.clientID).Scan(&userCount); err != nil {
			t.Fatalf("iteration %d: count users: %v", i, err)
		}
		if userCount != 1 {
			t.Fatalf("iteration %d: users row count = %d, want 1 (removal never deletes the user)", i, userCount)
		}

		// Outcome-specific sanity: if assign won, membership must be intact and
		// the project must reference the client; if removal won, the project
		// must have no client.
		var membershipCount int
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
		`, f.clientID, f.orgID).Scan(&membershipCount); err != nil {
			t.Fatalf("iteration %d: count memberships: %v", i, err)
		}
		var clientRef *uuid.UUID
		if err := db.QueryRowContext(ctx, `SELECT client_id FROM projects WHERE id = $1`, f.projectID).Scan(&clientRef); err != nil {
			t.Fatalf("iteration %d: read project client_id: %v", i, err)
		}
		if assignWon && (membershipCount != 1 || clientRef == nil || *clientRef != f.clientID) {
			t.Fatalf("iteration %d: assign won but membership=%d client_ref=%v (want 1 and client)", i, membershipCount, clientRef)
		}
		if removeWon && (membershipCount != 0 || clientRef != nil) {
			t.Fatalf("iteration %d: remove won but membership=%d client_ref=%v (want 0 and nil)", i, membershipCount, clientRef)
		}
	}

	// Summarize the outcome mix for the report.
	t.Log(fmt.Sprintf("assign/remove race stress: %d iterations, consistent outcomes only", iterations))
}
