package audit

// VersionedMetadata builds the standardized audit metadata object adopted for
// Layer-1 events (AI-readiness convention):
//
//	{"schema_version": 1, "before": {...}, "after": {...}, "reason": "..."}
//
// before and after hold the relevant pre/post state of the mutation (usually
// maps or structs; nil becomes JSON null). reason is omitted when empty so the
// metadata stays compact for events that carry no explanation.
func VersionedMetadata(before, after any, reason string) map[string]any {
	m := map[string]any{
		"schema_version": 1,
		"before":         before,
		"after":          after,
	}

	if reason != "" {
		m["reason"] = reason
	}

	return m
}
