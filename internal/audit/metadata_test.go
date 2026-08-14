package audit

import (
	"encoding/json"
	"testing"
)

// TestVersionedMetadata pins the standardized Layer-1 audit metadata shape:
// {"schema_version": 1, "before": ..., "after": ..., "reason": ...}, with
// reason omitted when empty.
func TestVersionedMetadata(t *testing.T) {
	t.Run("with reason", func(t *testing.T) {
		md := VersionedMetadata(
			map[string]any{"status": "awaiting_approval"},
			map[string]any{"status": "changes_requested"},
			"please fix the hero section",
		)

		if md["schema_version"] != 1 {
			t.Fatalf("schema_version = %v, want 1", md["schema_version"])
		}
		if got, ok := md["reason"].(string); !ok || got != "please fix the hero section" {
			t.Fatalf("reason = %v, want the notes string", md["reason"])
		}

		before, ok := md["before"].(map[string]any)
		if !ok || before["status"] != "awaiting_approval" {
			t.Fatalf("before = %v, want status awaiting_approval", md["before"])
		}
		after, ok := md["after"].(map[string]any)
		if !ok || after["status"] != "changes_requested" {
			t.Fatalf("after = %v, want status changes_requested", md["after"])
		}
	})

	t.Run("reason omitted when empty", func(t *testing.T) {
		md := VersionedMetadata(map[string]any{"a": 1}, map[string]any{"b": 2}, "")
		if _, ok := md["reason"]; ok {
			t.Fatalf("reason should be omitted when empty, got %v", md["reason"])
		}
		if md["schema_version"] != 1 {
			t.Fatalf("schema_version = %v, want 1", md["schema_version"])
		}
	})

	t.Run("nil before/after serialize as JSON null", func(t *testing.T) {
		md := VersionedMetadata(nil, map[string]any{"deliverable_id": "x"}, "")
		raw, err := json.Marshal(md)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, ok := decoded["before"]; !ok {
			t.Fatal("before key must be present (null) for schema stability")
		}
		if decoded["before"] != nil {
			t.Fatalf("before = %v, want null", decoded["before"])
		}
	})
}
