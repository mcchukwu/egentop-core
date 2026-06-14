package audit

type LogEntry struct {
	OrganizationID *string
	UserID         *string
	Action         string
	EntityType     string
	EntityID       *string
	Metadata       map[string]any
}
