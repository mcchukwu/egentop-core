package activity

type LogActivityEntry struct {
	OrganizationID string
	ActorID        *string
	ProjectID      *string
	MilestoneID    *string
	Type           string
	Message        string
	Metadata       map[string]any
}
