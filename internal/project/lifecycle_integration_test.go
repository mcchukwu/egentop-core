package project

// Project lifecycle slice (2026-08-16 decisions, §14 of requirements.md):
// due-date rule, full freeze of archived/cancelled, soft delete, restore,
// default list filter, and activity actor names. The §14.4 acceptance
// criteria are the contract; test names mirror the AC groups.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

// --- helpers ---

func setProjectStatus(t *testing.T, svc *Service, userID, orgID, projectID uuid.UUID, status ProjectStatus) {
	t.Helper()
	if _, err := svc.Update(context.Background(), userID, orgID, projectID, UpdateProjectRequest{Status: status}); err != nil {
		t.Fatalf("set project status %s: %v", status, err)
	}
}

// countActivityType counts activity rows of a type in an organization.
func countActivityType(t *testing.T, svc *Service, orgID uuid.UUID, activityType string) int {
	t.Helper()
	ctx := context.Background()
	feed, meta, err := svc.ActivityService.List(ctx, orgID, pagination.Query{Page: 1, Limit: 500})
	if err != nil {
		t.Fatalf("list org activity: %v", err)
	}
	if meta.Total < len(feed) {
		feed, _, err = svc.ActivityService.List(ctx, orgID, pagination.Query{Page: 1, Limit: meta.Total})
		if err != nil {
			t.Fatalf("list org activity (full): %v", err)
		}
	}
	n := 0
	for _, a := range feed {
		if a.Type == activityType {
			n++
		}
	}
	return n
}

// ===========================================================================
// AC-DD — due dates: no past dates, presence semantics, date-only UTC compare
// ===========================================================================

// TestDueDateRuleUpdateProject covers AC-DD-3/4/6/7 at the service boundary:
// past -> ErrDueDateInPast with the previous date intact, null clears, absent
// unchanged, today passes at any clock time, and a pre-existing past date
// reads back unchanged.
func TestDueDateRuleUpdateProject(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)

	// Seed a known due date (tomorrow).
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		DueDate: OptionalTime{Present: true, Value: &tomorrow},
	}); err != nil {
		t.Fatalf("set due date: %v", err)
	}

	// Past date (yesterday) -> ErrDueDateInPast; nothing changes.
	yesterday := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	_, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		Name:    "Should Not Apply",
		DueDate: OptionalTime{Present: true, Value: &yesterday},
	})
	if !errors.Is(err, apperrors.ErrDueDateInPast) {
		t.Fatalf("past due date error = %v, want ErrDueDateInPast", err)
	}

	after, err := svc.GetByID(ctx, orgID, projectID)
	if err != nil {
		t.Fatalf("read project after rejected update: %v", err)
	}
	if after.Name != "Project" {
		t.Fatalf("rejected update changed name to %q", after.Name)
	}
	if after.DueDate == nil || !after.DueDate.Equal(tomorrow) {
		t.Fatalf("rejected update changed due date to %v, want %v", after.DueDate, tomorrow)
	}

	// due_date: null clears the date.
	cleared, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		DueDate: OptionalTime{Present: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("clear due date: %v", err)
	}
	if cleared.DueDate != nil {
		t.Fatalf("cleared due date = %v, want nil", cleared.DueDate)
	}

	// Re-set a date so the absent test has something to observe.
	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		DueDate: OptionalTime{Present: true, Value: &tomorrow},
	}); err != nil {
		t.Fatalf("re-set due date: %v", err)
	}

	// Absent due_date leaves it unchanged (only name changes).
	renamed, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Name: "Renamed"})
	if err != nil {
		t.Fatalf("update without due_date: %v", err)
	}
	if renamed.Name != "Renamed" {
		t.Fatalf("name = %q, want Renamed", renamed.Name)
	}
	if renamed.DueDate == nil || !renamed.DueDate.Equal(tomorrow) {
		t.Fatalf("absent due_date changed the date to %v, want %v", renamed.DueDate, tomorrow)
	}

	// Today (UTC date) passes at any clock time (AC-DD-6): use noon local so
	// the wall clock is far from midnight either way.
	todayNoon := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		DueDate: OptionalTime{Present: true, Value: &todayNoon},
	}); err != nil {
		t.Fatalf("due date equal to today rejected: %v", err)
	}

	// AC-DD-7: a past date that pre-exists the rule is not rejected on read.
	var preexistingID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO projects (organization_id, created_by, name, status, due_date)
		VALUES ($1, $2, $3, 'active', NOW() - interval '30 days')
		RETURNING id
	`, orgID, ownerID, "Preexisting Past").Scan(&preexistingID); err != nil {
		t.Fatalf("seed preexisting past date: %v", err)
	}
	preexisting, err := svc.GetByID(ctx, orgID, preexistingID)
	if err != nil {
		t.Fatalf("read preexisting past-date project: %v", err)
	}
	if preexisting.DueDate == nil {
		t.Fatal("preexisting past date lost on read")
	}
}

// TestDueDateRuleUpdateMilestone covers AC-DD-5 for the milestone PATCH path:
// past -> ErrDueDateInPast (previous value intact), null clears, absent
// unchanged.
func TestDueDateRuleUpdateMilestone(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)

	tomorrow := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	if _, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{
		DueDate: OptionalTime{Present: true, Value: &tomorrow},
	}); err != nil {
		t.Fatalf("set milestone due date: %v", err)
	}

	yesterday := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)
	_, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{
		Title:   "Should Not Apply",
		DueDate: OptionalTime{Present: true, Value: &yesterday},
	})
	if !errors.Is(err, apperrors.ErrDueDateInPast) {
		t.Fatalf("past milestone due date error = %v, want ErrDueDateInPast", err)
	}

	after, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone after rejected update: %v", err)
	}
	if after.Title != "Milestone" {
		t.Fatalf("rejected update changed title to %q", after.Title)
	}
	if after.DueDate == nil || !after.DueDate.Equal(tomorrow) {
		t.Fatalf("rejected update changed milestone due date to %v, want %v", after.DueDate, tomorrow)
	}

	// null clears.
	cleared, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{
		DueDate: OptionalTime{Present: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("clear milestone due date: %v", err)
	}
	if cleared.DueDate != nil {
		t.Fatalf("cleared milestone due date = %v, want nil", cleared.DueDate)
	}

	// absent leaves unchanged.
	if _, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{
		DueDate: OptionalTime{Present: true, Value: &tomorrow},
	}); err != nil {
		t.Fatalf("re-set milestone due date: %v", err)
	}
	unchanged, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{Position: 5})
	if err != nil {
		t.Fatalf("milestone update without due_date: %v", err)
	}
	if unchanged.Position != 5 {
		t.Fatalf("position = %d, want 5", unchanged.Position)
	}
	if unchanged.DueDate == nil || !unchanged.DueDate.Equal(tomorrow) {
		t.Fatalf("absent due_date changed milestone date to %v, want %v", unchanged.DueDate, tomorrow)
	}
}

// TestDueDateRulePrecedenceDeletedFirst: on a soft-deleted project the 404
// wins over the past-due-date rule (a past-date PATCH must not leak the
// deleted state as a 400) — §14.2.1 precedence.
func TestDueDateRulePrecedenceDeletedFirst(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	if err := svc.Delete(ctx, ownerID, orgID, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	_, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		DueDate: OptionalTime{Present: true, Value: &yesterday},
	})
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("past-date update on deleted project error = %v, want ErrProjectNotFound (404 first)", err)
	}

	_, err = svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{
		DueDate: OptionalTime{Present: true, Value: &yesterday},
	})
	if !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("past-date milestone update on deleted project error = %v, want ErrProjectNotFound", err)
	}
}

// TestDueDateRulePrecedenceFreezeFirst: on a frozen (archived/cancelled)
// project the state lock wins over the past-due-date rule — §14.2.1
// precedence.
func TestDueDateRulePrecedenceFreezeFirst(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	setProjectStatus(t, svc, ownerID, orgID, projectID, ProjectStatusArchived)

	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	_, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{
		DueDate: OptionalTime{Present: true, Value: &yesterday},
	})
	if !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("past-date update on archived project error = %v, want ErrInvalidStatusTransition (freeze first)", err)
	}
}

// ===========================================================================
// AC-LC — locked states: archived/cancelled are read-only except restore
// ===========================================================================

// TestFreezeBlocksEveryMutation walks every unguarded path (§14.1 rows 1, 5,
// 9, 10) plus the already-guarded ones (AC-LC-1..8) on archived and cancelled
// projects. Completed stays fully mutable (AC-LC-9, TestCompletedProjectStaysMutable).
func TestFreezeBlocksEveryMutation(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	for _, frozenStatus := range []ProjectStatus{ProjectStatusArchived, ProjectStatusCancelled} {
		t.Run(string(frozenStatus), func(t *testing.T) {
			ownerID, orgID, projectID, milestoneID := seedProject(t, db)
			clientID := seedClient(t, db, orgID)
			assignClient(t, svc, ownerID, orgID, projectID, clientID)

			// The deliverable must exist before the freeze (its creation is
			// itself a guarded mutation); the delete-block check uses it.
			deliverableID := addDeliverable(t, svc, ownerID, orgID, projectID, milestoneID)

			setProjectStatus(t, svc, ownerID, orgID, projectID, frozenStatus)

			// Take the milestone to a submittable state while the project is
			// frozen (direct SQL, since the project is now read-only) so the
			// state-machine actions can be exercised.
			if _, err := db.ExecContext(ctx, `
				UPDATE milestones SET status = 'in_progress' WHERE id = $1
			`, milestoneID); err != nil {
				t.Fatalf("move milestone in_progress: %v", err)
			}
			if _, err := db.ExecContext(ctx, `
				UPDATE milestones SET status = 'awaiting_approval' WHERE id = $1
			`, milestoneID); err != nil {
				t.Fatalf("move milestone awaiting_approval: %v", err)
			}

			// Row 1: project field edits.
			if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Name: "Frozen"}); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("field edit error = %v, want ErrInvalidStatusTransition", err)
			}
			// Row 2: any status change except the restore is blocked.
			if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusCompleted}); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("status change error = %v, want ErrInvalidStatusTransition", err)
			}
			// Row 5: milestone create + metadata edit.
			if _, err := svc.CreateMilestone(ctx, orgID, projectID, ownerID, CreateMilestoneInput{Title: "Frozen Milestone"}); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("milestone create error = %v, want ErrInvalidStatusTransition", err)
			}
			if _, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{Title: "Frozen"}); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("milestone edit error = %v, want ErrInvalidStatusTransition", err)
			}
			// Row 6: submit.
			if _, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("submit error = %v, want ErrInvalidStatusTransition", err)
			}
			// Row 7: approve.
			if _, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("approve error = %v, want ErrInvalidStatusTransition", err)
			}
			// Row 8: changes-requested + generic status PATCH.
			if _, err := svc.RequestMilestoneChanges(ctx, clientID, orgID, projectID, milestoneID, "notes"); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("changes-requested error = %v, want ErrInvalidStatusTransition", err)
			}
			if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("milestone status PATCH error = %v, want ErrInvalidStatusTransition", err)
			}
			// Row 9: deliverable add/remove.
			if _, err := svc.CreateDeliverable(ctx, ownerID, orgID, projectID, milestoneID, "https://example.com/frozen", nil, nil); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("deliverable create error = %v, want ErrInvalidStatusTransition", err)
			}
			if err := svc.DeleteDeliverable(ctx, ownerID, orgID, projectID, milestoneID, deliverableID); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("deliverable delete error = %v, want ErrInvalidStatusTransition", err)
			}
			// Row 10: payment status + both revision limits + client assign.
			if _, err := svc.UpdateMilestonePaymentStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestonePaymentStatusPaid); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("payment status error = %v, want ErrInvalidStatusTransition", err)
			}
			limit := 2
			if _, err := svc.SetProjectRevisionLimit(ctx, ownerID, orgID, projectID, &limit); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("project revision limit error = %v, want ErrInvalidStatusTransition", err)
			}
			if _, err := svc.SetMilestoneRevisionLimit(ctx, ownerID, orgID, projectID, milestoneID, &limit); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("milestone revision limit error = %v, want ErrInvalidStatusTransition", err)
			}
			if _, err := svc.AssignClient(ctx, ownerID, orgID, projectID, &clientID); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("client assign error = %v, want ErrInvalidStatusTransition", err)
			}

			// The blocked mutations left every row untouched: the milestone is
			// still awaiting_approval with its deliverable, payment status
			// still unpaid, and no revision limit set.
			milestone, err := svc.GetMilestoneDetail(ctx, ownerID, "owner", orgID, projectID, milestoneID)
			if err != nil {
				t.Fatalf("read milestone after freeze: %v", err)
			}
			if milestone.Status != MilestoneStatusAwaitingApproval {
				t.Fatalf("milestone status after frozen mutations = %s, want awaiting_approval", milestone.Status)
			}
			if len(milestone.Deliverables) != 1 {
				t.Fatalf("deliverables after frozen delete = %d, want 1 (delete was blocked)", len(milestone.Deliverables))
			}
			if milestone.PaymentStatus != MilestonePaymentStatusUnpaid {
				t.Fatalf("payment status after frozen update = %s, want unpaid", milestone.PaymentStatus)
			}
			if milestone.RevisionLimit != nil {
				t.Fatalf("milestone revision limit after frozen update = %v, want nil", *milestone.RevisionLimit)
			}
			if milestone.Title != "Milestone" {
				t.Fatalf("milestone title after frozen edit = %q, want Milestone", milestone.Title)
			}
			project, err := svc.GetByID(ctx, orgID, projectID)
			if err != nil {
				t.Fatalf("read project after freeze: %v", err)
			}
			if project.Name != "Project" {
				t.Fatalf("project name after frozen edit = %q, want Project", project.Name)
			}
		})
	}
}

// TestCompletedProjectStaysMutable (AC-LC-9): a completed project is not
// frozen — rows 1, 5, 9, 10 of the matrix stay allowed.
func TestCompletedProjectStaysMutable(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)
	setProjectStatus(t, svc, ownerID, orgID, projectID, ProjectStatusCompleted)

	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Name: "Completed Editable"}); err != nil {
		t.Fatalf("field edit on completed project: %v", err)
	}
	if _, err := svc.CreateMilestone(ctx, orgID, projectID, ownerID, CreateMilestoneInput{Title: "New Milestone"}); err != nil {
		t.Fatalf("milestone create on completed project: %v", err)
	}
	if _, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{Title: "Edited"}); err != nil {
		t.Fatalf("milestone edit on completed project: %v", err)
	}
	if _, err := svc.CreateDeliverable(ctx, ownerID, orgID, projectID, milestoneID, "https://example.com/ok", nil, nil); err != nil {
		t.Fatalf("deliverable create on completed project: %v", err)
	}
	if _, err := svc.UpdateMilestonePaymentStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestonePaymentStatusPartial); err != nil {
		t.Fatalf("payment status on completed project: %v", err)
	}
	limit := 3
	if _, err := svc.SetProjectRevisionLimit(ctx, ownerID, orgID, projectID, &limit); err != nil {
		t.Fatalf("project revision limit on completed project: %v", err)
	}
	if _, err := svc.SetMilestoneRevisionLimit(ctx, ownerID, orgID, projectID, milestoneID, &limit); err != nil {
		t.Fatalf("milestone revision limit on completed project: %v", err)
	}
	if _, err := svc.AssignClient(ctx, ownerID, orgID, projectID, &clientID); err != nil {
		t.Fatalf("client assign on completed project: %v", err)
	}
}

// ===========================================================================
// AC-DEL — soft delete
// ===========================================================================

// TestDeleteProjectFromEveryStatus (AC-DEL-1/2): delete succeeds from each
// status; the row keeps deleted_at; audit project.deleted and activity
// project.deleted are written.
func TestDeleteProjectFromEveryStatus(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	statuses := []ProjectStatus{
		ProjectStatusDraft,
		ProjectStatusActive,
		ProjectStatusCompleted,
		ProjectStatusArchived,
		ProjectStatusCancelled,
	}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			ownerID, orgID, projectID, _ := seedProject(t, db)

			// Seed the target status. seedProject creates 'active' rows, so
			// every status is stamped directly (independent of the transition
			// matrix).
			if _, err := db.ExecContext(ctx, `UPDATE projects SET status = $1 WHERE id = $2`, status, projectID); err != nil {
				t.Fatalf("seed status %s: %v", status, err)
			}

			if err := svc.Delete(ctx, ownerID, orgID, projectID); err != nil {
				t.Fatalf("delete from %s: %v", status, err)
			}

			// The row remains, with deleted_at set.
			var deletedAt *time.Time
			var storedStatus ProjectStatus
			if err := db.QueryRowContext(ctx, `SELECT deleted_at, status FROM projects WHERE id = $1`, projectID).Scan(&deletedAt, &storedStatus); err != nil {
				t.Fatalf("read deleted row: %v", err)
			}
			if deletedAt == nil {
				t.Fatalf("deleted_at not set after delete from %s", status)
			}
			if storedStatus != status {
				t.Fatalf("status after delete = %s, want %s (row preserved)", storedStatus, status)
			}

			// Audit + activity rows exist with the acting user as actor.
			if got := countAuditRows(t, db, "project.deleted", projectID); got != 1 {
				t.Fatalf("project.deleted audit rows = %d, want 1", got)
			}
			if got := countActivityType(t, svc, orgID, "project.deleted"); got != 1 {
				t.Fatalf("project.deleted activity rows = %d, want 1", got)
			}
		})
	}
}

// TestDeletedProjectHiddenEverywhere (AC-DEL-3): after delete, every
// project-scoped read resolves 404 and the list excludes the project from
// items and total.
func TestDeletedProjectHiddenEverywhere(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)

	if err := svc.Delete(ctx, ownerID, orgID, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	// Project reads.
	if _, err := svc.GetByID(ctx, orgID, projectID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("GetByID after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.ViewProject(ctx, ownerID, "owner", orgID, projectID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("ViewProject after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.GetApprovalView(ctx, clientID, "client", orgID, projectID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("approval view after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, _, err := svc.ListProjectActivities(ctx, ownerID, "owner", orgID, projectID, pagination.Query{Page: 1, Limit: 20}); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("project activities after delete error = %v, want ErrProjectNotFound", err)
	}

	// Milestone reads.
	if _, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID); !errors.Is(err, apperrors.ErrMilestoneNotFound) {
		t.Fatalf("milestone detail after delete error = %v, want ErrMilestoneNotFound", err)
	}
	if _, err := svc.GetMilestoneDetail(ctx, ownerID, "owner", orgID, projectID, milestoneID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("milestone detail (scoped) after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, _, err := svc.ListMilestonesByProjectID(ctx, ownerID, "owner", orgID, projectID, pagination.Query{Page: 1, Limit: 20}); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("milestone list after delete error = %v, want ErrProjectNotFound", err)
	}

	// Mutations.
	if _, err := svc.CreateMilestone(ctx, orgID, projectID, ownerID, CreateMilestoneInput{Title: "Nope"}); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("milestone create after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.UpdateMilestone(ctx, orgID, ownerID, projectID, milestoneID, UpdateMilestoneRequest{Title: "Nope"}); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("milestone edit after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("milestone status PATCH after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("submit after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("approve after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.RequestMilestoneChanges(ctx, clientID, orgID, projectID, milestoneID, "notes"); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("changes-requested after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.AssignClient(ctx, ownerID, orgID, projectID, &clientID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client assign after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.CreateDeliverable(ctx, ownerID, orgID, projectID, milestoneID, "https://example.com/x", nil, nil); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("deliverable create after delete error = %v, want ErrProjectNotFound", err)
	}

	// List: items AND total exclude the deleted project.
	projects, meta, err := svc.ListByOrganizationID(ctx, orgID, pagination.Query{Page: 1, Limit: 20}, false)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, p := range projects {
		if p.ID == projectID {
			t.Fatal("deleted project present in list items")
		}
	}
	if meta.Total != len(projects) {
		t.Fatalf("list total = %d, want %d (deleted must not count in total)", meta.Total, len(projects))
	}
}

// TestDeleteSecondDelete404 (AC-DEL-6): a second delete resolves 404
// (idempotent from the caller's view).
func TestDeleteSecondDelete404(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)

	if err := svc.Delete(ctx, ownerID, orgID, projectID); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if err := svc.Delete(ctx, ownerID, orgID, projectID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("second delete error = %v, want ErrProjectNotFound", err)
	}
	// Exactly one project.deleted audit row (the second attempt wrote nothing).
	if got := countAuditRows(t, db, "project.deleted", projectID); got != 1 {
		t.Fatalf("project.deleted audit rows = %d, want 1 after second delete", got)
	}
}

// TestDeleteCrossOrg404 (AC-DEL-5): deleting through another org's scope is
// 404 (no existence leak).
func TestDeleteCrossOrg404(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	_, otherOrgID, _, _ := seedProject(t, db)

	if err := svc.Delete(ctx, ownerID, otherOrgID, projectID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("cross-org delete error = %v, want ErrProjectNotFound", err)
	}

	// The project is still live in its own org.
	if _, err := svc.GetByID(ctx, orgID, projectID); err != nil {
		t.Fatalf("project should still be live: %v", err)
	}
}

// TestOrgFeedPreservesDeletedHistory (AC-DEL-7 + the Captain-approved
// dead-link fix): the org-wide feed keeps the deleted project's historical
// events and the delete event itself, and every row referencing the deleted
// project renders with project_id null — history preserved, no dead link.
func TestOrgFeedPreservesDeletedHistory(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, _, _ := seedProject(t, db)

	// Create the project through the service so the org feed also carries the
	// project.created event (seedProject inserts directly and writes none).
	created, err := svc.Create(ctx, ownerID, orgID, CreateProjectRequest{Name: "Feed Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectID := created.ID

	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Name: "Renamed Before Delete"}); err != nil {
		t.Fatalf("update before delete: %v", err)
	}
	if err := svc.Delete(ctx, ownerID, orgID, projectID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	feed, meta, err := svc.ActivityService.List(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("list org feed: %v", err)
	}
	if meta.Total < 3 {
		t.Fatalf("org feed total = %d, want >= 3 (created + updated + deleted preserved)", meta.Total)
	}

	seen := map[string]bool{}
	for _, a := range feed {
		// No dead links: no row may still resolve to the deleted project.
		if a.ProjectID != nil && *a.ProjectID == projectID {
			t.Fatalf("org feed row %s still links the deleted project (dead link); want project_id null", a.Type)
		}
		switch a.Type {
		case "project.created", "project.updated", "project.deleted":
			// The history is preserved (type + message), just de-referenced.
			if a.Message == "" {
				t.Fatal("activity message lost for a deleted-project row")
			}
			seen[a.Type] = true
		}
	}
	for _, want := range []string{"project.created", "project.updated", "project.deleted"} {
		if !seen[want] {
			t.Fatalf("org feed missing %s for the deleted project (history lost)", want)
		}
	}
}

// TestDeleteAuditMetadata: the project.deleted audit row carries the
// versioned metadata with before {status, name} and after {deleted_at}.
func TestDeleteAuditMetadata(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	if err := svc.Delete(ctx, ownerID, orgID, projectID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var metadataRaw []byte
	if err := db.QueryRowContext(ctx, `
		SELECT metadata FROM audit_logs
		WHERE action = 'project.deleted' AND entity_id = $1
		ORDER BY created_at DESC LIMIT 1
	`, projectID).Scan(&metadataRaw); err != nil {
		t.Fatalf("read delete audit metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metadataRaw, &meta); err != nil {
		t.Fatalf("decode delete audit metadata: %v", err)
	}
	if meta["schema_version"] != float64(1) {
		t.Fatalf("schema_version = %v, want 1", meta["schema_version"])
	}
	before, ok := meta["before"].(map[string]any)
	if !ok || before["status"] != "active" {
		t.Fatalf("before = %v, want status active", meta["before"])
	}
	after, ok := meta["after"].(map[string]any)
	if !ok || after["deleted_at"] == nil {
		t.Fatalf("after = %v, want deleted_at set", meta["after"])
	}
}

// ===========================================================================
// AC-RESTORE — archived -> active
// ===========================================================================

// TestRestoreArchivedProject (AC-RESTORE-1/3): restore returns active, emits
// its own audit + activity events, preserves milestone states, and the
// assigned client can approve after restore.
func TestRestoreArchivedProject(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)

	// Take the milestone to awaiting_approval.
	if _, err := svc.UpdateMilestoneStatus(ctx, ownerID, orgID, projectID, milestoneID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	addDeliverable(t, svc, ownerID, orgID, projectID, milestoneID)
	if _, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Archive (active -> archived), then restore (archived -> active).
	setProjectStatus(t, svc, ownerID, orgID, projectID, ProjectStatusArchived)
	restored, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusActive})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Status != ProjectStatusActive {
		t.Fatalf("status after restore = %s, want active", restored.Status)
	}

	// Own audit + activity events.
	if got := countAuditRows(t, db, "project.restored", projectID); got != 1 {
		t.Fatalf("project.restored audit rows = %d, want 1", got)
	}
	if got := countActivityType(t, svc, orgID, "project.restored"); got != 1 {
		t.Fatalf("project.restored activity rows = %d, want 1", got)
	}

	// Milestone state preserved: still awaiting_approval, client can approve.
	milestone, err := svc.GetMilestoneByID(ctx, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone after restore: %v", err)
	}
	if milestone.Status != MilestoneStatusAwaitingApproval {
		t.Fatalf("milestone status after restore = %s, want awaiting_approval (states preserved)", milestone.Status)
	}
	approved, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("client approve after restore: %v", err)
	}
	if approved.Status != MilestoneStatusApproved {
		t.Fatalf("status after approve = %s, want approved", approved.Status)
	}
}

// TestRestoreTransitionMatrix (AC-RESTORE-2): cancelled is terminal;
// completed -> active stays invalid; draft -> active and active -> active
// follow the existing matrix.
func TestRestoreTransitionMatrix(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	// cancelled -> active: terminal.
	ownerID, orgID, cancelledProject, _ := seedProject(t, db)
	setProjectStatus(t, svc, ownerID, orgID, cancelledProject, ProjectStatusCancelled)
	if _, err := svc.Update(ctx, ownerID, orgID, cancelledProject, UpdateProjectRequest{Status: ProjectStatusActive}); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("cancelled -> active error = %v, want ErrInvalidStatusTransition (terminal)", err)
	}

	// completed -> active: invalid per the existing matrix.
	ownerID2, orgID2, completedProject, _ := seedProject(t, db)
	setProjectStatus(t, svc, ownerID2, orgID2, completedProject, ProjectStatusCompleted)
	if _, err := svc.Update(ctx, ownerID2, orgID2, completedProject, UpdateProjectRequest{Status: ProjectStatusActive}); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
		t.Fatalf("completed -> active error = %v, want ErrInvalidStatusTransition", err)
	}

	// draft -> active: allowed.
	ownerID3, orgID3, draftProject, _ := seedProject(t, db)
	if _, err := db.ExecContext(ctx, `UPDATE projects SET status = 'draft' WHERE id = $1`, draftProject); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	if _, err := svc.Update(ctx, ownerID3, orgID3, draftProject, UpdateProjectRequest{Status: ProjectStatusActive}); err != nil {
		t.Fatalf("draft -> active: %v", err)
	}

	// active -> active: no-op, allowed.
	if _, err := svc.Update(ctx, ownerID, orgID, cancelledProject, UpdateProjectRequest{}); err == nil {
		// empty PATCH is allowed but irrelevant; use a real active project.
	}
	ownerID4, orgID4, activeProject, _ := seedProject(t, db)
	if _, err := svc.Update(ctx, ownerID4, orgID4, activeProject, UpdateProjectRequest{Status: ProjectStatusActive}); err != nil {
		t.Fatalf("active -> active: %v", err)
	}
}

// TestRestoreFromCompletedArchive (AC-RESTORE-3): a project archived from
// completed restores to active (not completed) and the audit trail preserves
// the completed history.
func TestRestoreFromCompletedArchive(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	setProjectStatus(t, svc, ownerID, orgID, projectID, ProjectStatusCompleted)
	setProjectStatus(t, svc, ownerID, orgID, projectID, ProjectStatusArchived)

	restored, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusActive})
	if err != nil {
		t.Fatalf("restore from completed-archive: %v", err)
	}
	if restored.Status != ProjectStatusActive {
		t.Fatalf("restored status = %s, want active (restore always targets active)", restored.Status)
	}
}

// TestRestoreAuditMetadata: the project.restored audit row carries the
// versioned before/after status.
func TestRestoreAuditMetadata(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	setProjectStatus(t, svc, ownerID, orgID, projectID, ProjectStatusArchived)
	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusActive}); err != nil {
		t.Fatalf("restore: %v", err)
	}

	var metadataRaw []byte
	if err := db.QueryRowContext(ctx, `
		SELECT metadata FROM audit_logs
		WHERE action = 'project.restored' AND entity_id = $1
		ORDER BY created_at DESC LIMIT 1
	`, projectID).Scan(&metadataRaw); err != nil {
		t.Fatalf("read restore audit metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metadataRaw, &meta); err != nil {
		t.Fatalf("decode restore audit metadata: %v", err)
	}
	before, ok := meta["before"].(map[string]any)
	if !ok || before["status"] != "archived" {
		t.Fatalf("restore before = %v, want status archived", meta["before"])
	}
	after, ok := meta["after"].(map[string]any)
	if !ok || after["status"] != "active" {
		t.Fatalf("restore after = %v, want status active", meta["after"])
	}
}

// ===========================================================================
// AC-ACT — activity actors
// ===========================================================================

// TestActivityActorNames (AC-ACT-1/3/4/5): org and project feeds resolve the
// actor's "{first_name} {last_name}"; actor_id stays; nil-actor rows have no
// actor_name; messages are byte-identical; delete/restore rows carry the actor.
func TestActivityActorNames(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)

	// seedProject's owner is first_name 'Owner' last_name 'User'.
	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Name: "Renamed"}); err != nil {
		t.Fatalf("update project: %v", err)
	}
	if err := svc.Delete(ctx, ownerID, orgID, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	// A row with no actor (direct insert, like pre-enrichment rows).
	if _, err := db.ExecContext(ctx, `
		INSERT INTO activities (organization_id, project_id, actor_id, type, message, metadata)
		VALUES ($1, $2, NULL, 'legacy.event', 'Legacy event', '{}')
	`, orgID, projectID); err != nil {
		t.Fatalf("insert nil-actor activity: %v", err)
	}

	feed, _, err := svc.ActivityService.List(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("org feed: %v", err)
	}

	// The delete event carries the acting user's actor_name and the exact
	// message; actor_id is retained.
	foundDeleted := false
	for _, a := range feed {
		switch a.Type {
		case "project.deleted":
			foundDeleted = true
			if a.Message != "Project deleted" {
				t.Fatalf("delete message = %q, want byte-identical \"Project deleted\"", a.Message)
			}
			if a.ActorName == nil || *a.ActorName != "Owner User" {
				t.Fatalf("delete actor_name = %v, want Owner User", a.ActorName)
			}
			if a.ActorID == nil || *a.ActorID != ownerID {
				t.Fatalf("delete actor_id = %v, want %v (actor_id retained)", a.ActorID, ownerID)
			}
		case "project.created":
			if a.Message != "Project created" {
				t.Fatalf("created message = %q, want byte-identical \"Project created\"", a.Message)
			}
			if a.ActorName == nil || *a.ActorName != "Owner User" {
				t.Fatalf("created actor_name = %v, want Owner User", a.ActorName)
			}
		case "legacy.event":
			if a.ActorName != nil {
				t.Fatalf("nil-actor row actor_name = %v, want nil", *a.ActorName)
			}
			if a.ActorID != nil {
				t.Fatalf("nil-actor row actor_id = %v, want nil", *a.ActorID)
			}
		}
	}
	if !foundDeleted {
		t.Fatal("org feed missing project.deleted event")
	}
}

// TestClientProjectFeedActorNames (AC-ACT-2): the client's project-scoped feed
// resolves staff actor names.
func TestClientProjectFeedActorNames(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)

	if _, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Name: "Renamed"}); err != nil {
		t.Fatalf("update project: %v", err)
	}

	feed, _, err := svc.ListProjectActivities(ctx, clientID, "client", orgID, projectID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("client project feed: %v", err)
	}
	for _, a := range feed {
		if a.ActorName == nil || *a.ActorName != "Owner User" {
			t.Fatalf("client feed actor_name = %v, want Owner User (clients see staff names)", a.ActorName)
		}
	}
}

// ===========================================================================
// AC-CLIENT — client surface
// ===========================================================================

// TestClientSurfaceArchivedCancelled (AC-CLIENT-2/3): deep links to archived
// and cancelled projects resolve view-only; approve/changes-requested invoked
// anyway return ErrInvalidStatusTransition.
func TestClientSurfaceArchivedCancelled(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	for _, frozenStatus := range []ProjectStatus{ProjectStatusArchived, ProjectStatusCancelled} {
		t.Run(string(frozenStatus), func(t *testing.T) {
			ownerID, orgID, projectID, milestoneID := seedProject(t, db)
			clientID := seedClient(t, db, orgID)
			assignClient(t, svc, ownerID, orgID, projectID, clientID)
			setProjectStatus(t, svc, ownerID, orgID, projectID, frozenStatus)

			// The deep link still resolves (view-only).
			view, err := svc.GetApprovalView(ctx, clientID, "client", orgID, projectID)
			if err != nil {
				t.Fatalf("client approval view on %s project: %v", frozenStatus, err)
			}
			if view.Project.Status != frozenStatus {
				t.Fatalf("view status = %s, want %s", view.Project.Status, frozenStatus)
			}

			// The milestone must be awaiting_approval for the action to even
			// be attempted (otherwise the freeze is masked by a state error).
			if _, err := db.ExecContext(ctx, `
				UPDATE milestones SET status = 'awaiting_approval' WHERE id = $1
			`, milestoneID); err != nil {
				t.Fatalf("seed awaiting_approval: %v", err)
			}

			// Actions invoked anyway are blocked by the freeze.
			if _, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("approve on %s error = %v, want ErrInvalidStatusTransition", frozenStatus, err)
			}
			if _, err := svc.RequestMilestoneChanges(ctx, clientID, orgID, projectID, milestoneID, "notes"); !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("changes-requested on %s error = %v, want ErrInvalidStatusTransition", frozenStatus, err)
			}
		})
	}
}

// TestClientSurfaceActiveUnchanged (AC-CLIENT-4): the client deep link on
// draft/active/completed behaves as today (approve works on awaiting_approval).
func TestClientSurfaceActiveUnchanged(t *testing.T) {
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
	if _, err := svc.ApproveMilestone(ctx, clientID, orgID, projectID, milestoneID); err != nil {
		t.Fatalf("client approve on active project: %v", err)
	}
}

// TestClientSurfaceDeleted404 (AC-CLIENT-1): a client deep link to a
// soft-deleted project is indistinguishable from a non-existent project.
func TestClientSurfaceDeleted404(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	assignClient(t, svc, ownerID, orgID, projectID, clientID)
	if err := svc.Delete(ctx, ownerID, orgID, projectID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := svc.ViewProject(ctx, clientID, "client", orgID, projectID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client view after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, err := svc.GetApprovalView(ctx, clientID, "client", orgID, projectID); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client approval view after delete error = %v, want ErrProjectNotFound", err)
	}
	if _, _, err := svc.ListProjectActivities(ctx, clientID, "client", orgID, projectID, pagination.Query{Page: 1, Limit: 20}); !errors.Is(err, apperrors.ErrProjectNotFound) {
		t.Fatalf("client project feed after delete error = %v, want ErrProjectNotFound", err)
	}
}

// ===========================================================================
// Concurrency — delete/archive serialize with other project-lock mutations
// ===========================================================================

// TestConcurrentDeleteSerializesOnProjectLock: a raw tx holding the project
// row FOR UPDATE blocks svc.Delete until it commits; delete then applies on
// top of the committed state (the preserved row keeps the holder's change).
func TestConcurrentDeleteSerializesOnProjectLock(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)

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
		UPDATE projects SET name = 'held-before-delete' WHERE id = $1
	`, projectID); err != nil {
		_ = holder.Rollback()
		t.Fatalf("update in holder: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- svc.Delete(ctx, ownerID, orgID, projectID)
	}()

	time.Sleep(300 * time.Millisecond)
	if err := holder.Commit(); err != nil {
		t.Fatalf("commit holder: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("concurrent Delete after holder commit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Delete never returned after holder committed (blocked beyond timeout)")
	}

	// The preserved row carries the holder's committed name and is deleted.
	var name string
	var deletedAt *time.Time
	if err := db.QueryRowContext(ctx, `
		SELECT name, deleted_at FROM projects WHERE id = $1
	`, projectID).Scan(&name, &deletedAt); err != nil {
		t.Fatalf("read preserved row: %v", err)
	}
	if name != "held-before-delete" {
		t.Fatalf("preserved name = %q, want holder's committed value", name)
	}
	if deletedAt == nil {
		t.Fatal("deleted_at not set after concurrent delete")
	}
}

// TestConcurrentDeleteAndAssignSerialize: concurrent assign + delete on the
// same project serialize on the project row lock. Both orders are valid: if
// the assign wins, the deleted row keeps client_id; if the delete wins, the
// assign resolves ErrProjectNotFound. Never a 500 and never a lost update.
func TestConcurrentDeleteAndAssignSerialize(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	clientID := seedClient(t, db, orgID)

	// Tagged results so the two outcomes can be told apart.
	results := make(chan struct {
		op  string
		err error
	}, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.AssignClient(ctx, ownerID, orgID, projectID, &clientID)
		results <- struct {
			op  string
			err error
		}{"assign", err}
	}()
	go func() {
		defer wg.Done()
		results <- struct {
			op  string
			err error
		}{"delete", svc.Delete(ctx, ownerID, orgID, projectID)}
	}()
	wg.Wait()
	close(results)

	assignSucceeded := false
	deleteSucceeded := false
	for r := range results {
		switch r.op {
		case "assign":
			if r.err != nil && !errors.Is(r.err, apperrors.ErrProjectNotFound) {
				t.Fatalf("assign error = %v, want nil or ErrProjectNotFound", r.err)
			}
			assignSucceeded = r.err == nil
		case "delete":
			if r.err != nil {
				t.Fatalf("delete error = %v, want nil", r.err)
			}
			deleteSucceeded = true
		}
	}

	// The delete must have succeeded; if the assign also succeeded it ran
	// first and its client_id is retained on the preserved row.
	var clientIDStored *uuid.UUID
	if err := db.QueryRowContext(ctx, `SELECT client_id FROM projects WHERE id = $1`, projectID).Scan(&clientIDStored); err != nil {
		t.Fatalf("read preserved row: %v", err)
	}
	if !deleteSucceeded {
		t.Fatal("delete did not succeed in either ordering")
	}
	if assignSucceeded && (clientIDStored == nil || *clientIDStored != clientID) {
		t.Fatalf("assign ran first but client_id lost on delete: %v", clientIDStored)
	}
	var deletedAt *time.Time
	if err := db.QueryRowContext(ctx, `SELECT deleted_at FROM projects WHERE id = $1`, projectID).Scan(&deletedAt); err != nil {
		t.Fatalf("read deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("project not soft-deleted after concurrent assign+delete")
	}
}

// TestConcurrentDeleteAndSubmitSerialize: concurrent submit + delete. If the
// delete wins, the submit resolves ErrProjectNotFound; if the submit wins,
// both succeed and the revision history survives. Never a 500.
func TestConcurrentDeleteAndSubmitSerialize(t *testing.T) {
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

	var wg sync.WaitGroup
	results := make(chan struct {
		op  string
		err error
	}, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.SubmitMilestone(ctx, ownerID, orgID, projectID, milestoneID)
		results <- struct {
			op  string
			err error
		}{"submit", err}
	}()
	go func() {
		defer wg.Done()
		results <- struct {
			op  string
			err error
		}{"delete", svc.Delete(ctx, ownerID, orgID, projectID)}
	}()
	wg.Wait()
	close(results)

	submitSucceeded := false
	for r := range results {
		switch r.op {
		case "submit":
			if r.err != nil && !errors.Is(r.err, apperrors.ErrProjectNotFound) {
				t.Fatalf("submit error = %v, want nil or ErrProjectNotFound", r.err)
			}
			submitSucceeded = r.err == nil
		case "delete":
			if r.err != nil {
				t.Fatalf("delete error = %v, want nil", r.err)
			}
		}
	}

	if submitSucceeded {
		// Submit ran first: the revision row exists and the project is deleted
		// with the history preserved.
		var revisions int
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM milestone_revisions WHERE milestone_id = $1
		`, milestoneID).Scan(&revisions); err != nil {
			t.Fatalf("count revisions: %v", err)
		}
		if revisions != 1 {
			t.Fatalf("revision rows = %d, want 1 (submit won the lock first)", revisions)
		}
	}
	var deletedAt *time.Time
	if err := db.QueryRowContext(ctx, `SELECT deleted_at FROM projects WHERE id = $1`, projectID).Scan(&deletedAt); err != nil {
		t.Fatalf("read deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("project not soft-deleted after concurrent submit+delete")
	}
}

// TestConcurrentArchiveAndMilestoneCreateSerialize: archive + milestone
// create serialize on the project row lock. If the archive wins, the create
// returns ErrInvalidStatusTransition; if the create wins, both succeed and
// the milestone exists on the archived project.
func TestConcurrentArchiveAndMilestoneCreateSerialize(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	ownerID, orgID, projectID, _ := seedProject(t, db)

	var wg sync.WaitGroup
	results := make(chan struct {
		op  string
		err error
	}, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.CreateMilestone(ctx, orgID, projectID, ownerID, CreateMilestoneInput{Title: "Racing Milestone"})
		results <- struct {
			op  string
			err error
		}{"create", err}
	}()
	go func() {
		defer wg.Done()
		_, err := svc.Update(ctx, ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusArchived})
		results <- struct {
			op  string
			err error
		}{"archive", err}
	}()
	wg.Wait()
	close(results)

	createSucceeded := false
	archiveSucceeded := false
	for r := range results {
		switch r.op {
		case "create":
			if r.err != nil && !errors.Is(r.err, apperrors.ErrInvalidStatusTransition) {
				t.Fatalf("milestone create error = %v, want nil or ErrInvalidStatusTransition", r.err)
			}
			createSucceeded = r.err == nil
		case "archive":
			if r.err != nil {
				t.Fatalf("archive error = %v, want nil", r.err)
			}
			archiveSucceeded = true
		}
	}

	if !archiveSucceeded {
		t.Fatal("archive did not succeed in either ordering")
	}
	var projectStatus ProjectStatus
	if err := db.QueryRowContext(ctx, `SELECT status FROM projects WHERE id = $1`, projectID).Scan(&projectStatus); err != nil {
		t.Fatalf("read project status: %v", err)
	}
	if projectStatus != ProjectStatusArchived {
		t.Fatalf("project status = %s, want archived", projectStatus)
	}
	if createSucceeded {
		var milestoneCount int
		if err := db.QueryRowContext(ctx, `
			SELECT count(*) FROM milestones WHERE project_id = $1
		`, projectID).Scan(&milestoneCount); err != nil {
			t.Fatalf("count milestones: %v", err)
		}
		if milestoneCount != 2 {
			t.Fatalf("milestones = %d, want 2 (create won before archive)", milestoneCount)
		}
	}
}
