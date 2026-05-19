package audit

type LogEntry struct {
	OrganizationID *string
	UserID         *string
	Action         string
	Metadata       string
}
