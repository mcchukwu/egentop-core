package audit

import "github.com/google/uuid"

type LogEntry struct {
	OrganizationID *uuid.UUID
	UserID         *uuid.UUID
	Action         string
	EntityType     string
	EntityID       *uuid.UUID
	Metadata       map[string]any
}
