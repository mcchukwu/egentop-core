package client

import (
	"time"

	"github.com/google/uuid"
)

// Client is a user holding a client-role membership in an organization.
// Clients are modeled as real users + memberships (architect Option A); there
// is deliberately no separate clients table.
type Client struct {
	UserID    uuid.UUID `json:"user_id"`
	Email     *string   `json:"email,omitempty"`
	Phone     *string   `json:"phone,omitempty"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	JoinedAt  time.Time `json:"joined_at"`
}
