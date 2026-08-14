package project

import (
	"errors"
	"testing"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

// TestMilestoneStatusTransitionTable pins the canonical generic PATCH /status
// state machine. Every listed pair is the complete contract; anything not
// listed must be rejected.
func TestMilestoneStatusTransitionTable(t *testing.T) {
	valid := []struct {
		current MilestoneStatus
		next    MilestoneStatus
	}{
		// pending ↔ in_progress
		{MilestoneStatusPending, MilestoneStatusInProgress},
		{MilestoneStatusInProgress, MilestoneStatusPending},
		// in_progress ↔ blocked
		{MilestoneStatusInProgress, MilestoneStatusBlocked},
		{MilestoneStatusBlocked, MilestoneStatusInProgress},
		// cancellation is the escape hatch from every non-terminal state
		{MilestoneStatusPending, MilestoneStatusCancelled},
		{MilestoneStatusInProgress, MilestoneStatusCancelled},
		{MilestoneStatusBlocked, MilestoneStatusCancelled},
		{MilestoneStatusAwaitingApproval, MilestoneStatusCancelled},
		{MilestoneStatusChangesRequested, MilestoneStatusCancelled},
		// awaiting_approval: escape hatch only (never completed)
		{MilestoneStatusAwaitingApproval, MilestoneStatusBlocked},
		// changes_requested: back to work, blocked, or cancelled
		{MilestoneStatusChangesRequested, MilestoneStatusInProgress},
		{MilestoneStatusChangesRequested, MilestoneStatusBlocked},
		// approved → completed (client sign-off then done)
		{MilestoneStatusApproved, MilestoneStatusCompleted},
	}

	for _, tc := range valid {
		if err := validateMilestoneStatusTransition(tc.current, tc.next); err != nil {
			t.Errorf("transition %s -> %s should be valid, got %v", tc.current, tc.next, err)
		}
	}

	invalid := []struct {
		current MilestoneStatus
		next    MilestoneStatus
	}{
		// action-only target statuses can never be PATCHed to
		{MilestoneStatusPending, MilestoneStatusAwaitingApproval},
		{MilestoneStatusInProgress, MilestoneStatusAwaitingApproval},
		{MilestoneStatusPending, MilestoneStatusApproved},
		{MilestoneStatusChangesRequested, MilestoneStatusApproved},
		{MilestoneStatusPending, MilestoneStatusChangesRequested},
		{MilestoneStatusAwaitingApproval, MilestoneStatusChangesRequested},
		// the wedge: awaiting_approval cannot jump to completed
		{MilestoneStatusAwaitingApproval, MilestoneStatusCompleted},
		{MilestoneStatusAwaitingApproval, MilestoneStatusApproved},
		// non-adjacent / unlisted transitions
		{MilestoneStatusPending, MilestoneStatusBlocked},
		{MilestoneStatusBlocked, MilestoneStatusPending},
		{MilestoneStatusApproved, MilestoneStatusAwaitingApproval},
		{MilestoneStatusApproved, MilestoneStatusCancelled},
		{MilestoneStatusApproved, MilestoneStatusChangesRequested},
		{MilestoneStatusApproved, MilestoneStatusInProgress},
		{MilestoneStatusApproved, MilestoneStatusBlocked},
		// terminal states have no outgoing transitions
		{MilestoneStatusCompleted, MilestoneStatusInProgress},
		{MilestoneStatusCompleted, MilestoneStatusPending},
		{MilestoneStatusCancelled, MilestoneStatusInProgress},
		{MilestoneStatusCancelled, MilestoneStatusPending},
	}

	for _, tc := range invalid {
		if err := validateMilestoneStatusTransition(tc.current, tc.next); err == nil {
			t.Errorf("transition %s -> %s should be invalid, got nil", tc.current, tc.next)
		} else if !errors.Is(err, apperrors.ErrInvalidStatusTransition) {
			t.Errorf("transition %s -> %s should return ErrInvalidStatusTransition, got %v", tc.current, tc.next, err)
		}
	}

	// Same-state is a no-op success for every status (callers short-circuit
	// action-only same-states before reaching the validator).
	for _, status := range []MilestoneStatus{
		MilestoneStatusPending,
		MilestoneStatusInProgress,
		MilestoneStatusAwaitingApproval,
		MilestoneStatusCompleted,
		MilestoneStatusBlocked,
		MilestoneStatusCancelled,
		MilestoneStatusApproved,
		MilestoneStatusChangesRequested,
	} {
		if err := validateMilestoneStatusTransition(status, status); err != nil {
			t.Errorf("same-state %s should be a no-op, got %v", status, err)
		}
	}
}

// TestRevisionLimitReached pins the limit_reached computation.
func TestRevisionLimitReached(t *testing.T) {
	three := 3

	cases := []struct {
		name          string
		revisionCount int
		limit         *int
		want          bool
	}{
		{"no limit", 0, nil, false},
		{"no limit with submissions", 7, nil, false},
		{"below limit", 1, &three, false},
		{"at limit", 3, &three, true},
		{"over limit", 4, &three, true},
		{"zero limit impossible via CHECK, treat as reached", 0, intPtr(0), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := revisionLimitReached(tc.revisionCount, tc.limit); got != tc.want {
				t.Fatalf("revisionLimitReached(%d, %v) = %v, want %v", tc.revisionCount, tc.limit, got, tc.want)
			}
		})
	}
}

// TestIsHTTPURL pins the deliverable URL scheme check.
func TestIsHTTPURL(t *testing.T) {
	valid := []string{
		"http://example.com/file",
		"https://drive.google.com/d/folder",
		"https://figma.com/proto/abc",
		"http://localhost:8080/file",
	}
	for _, u := range valid {
		if !isHTTPURL(u) {
			t.Errorf("isHTTPURL(%q) = false, want true", u)
		}
	}

	invalid := []string{
		"",
		"ftp://example.com/file",
		"file:///tmp/x",
		"//example.com/no-scheme",
		"example.com/file",
		"javascript:alert(1)",
	}
	for _, u := range invalid {
		if isHTTPURL(u) {
			t.Errorf("isHTTPURL(%q) = true, want false", u)
		}
	}
}

func intPtr(v int) *int {
	return &v
}
