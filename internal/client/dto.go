package client

import "github.com/google/uuid"

// ProvisionRequest provisions a client account. At least one of email or
// phone is required (matching the users table CHECK constraint); both are
// optional individually so phone-only provisioning works. first_name and
// last_name are only used when a NEW user must be created; they are validated
// in the service for that path.
type ProvisionRequest struct {
	Email     string `json:"email,omitempty" validate:"omitempty,email,max=100"`
	Phone     string `json:"phone,omitempty" validate:"omitempty,ngphone"`
	FirstName string `json:"first_name,omitempty" validate:"omitempty,min=2,max=50"`
	LastName  string `json:"last_name,omitempty" validate:"omitempty,min=2,max=50"`
}

// ProvisionResult is returned from POST /orgs/{orgID}/clients. The one-time
// password is returned exactly once, only when a new user was created. When
// an existing user is reused, no credential is issued (their existing
// password remains authoritative).
type ProvisionResult struct {
	ClientID         uuid.UUID `json:"client_id"`
	Email            *string   `json:"email,omitempty"`
	Phone            *string   `json:"phone,omitempty"`
	CredentialIssued bool      `json:"credential_issued"`
	OneTimePassword  string    `json:"one_time_password,omitempty"`
}
