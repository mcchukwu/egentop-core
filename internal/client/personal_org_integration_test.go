package client

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

// seedPersonalClientOrg creates a PERSONAL workspace (is_personal = true) with
// an owner membership — the registration default org shape. Staff-membership
// mutations are blocked on it, but client flows must keep working.
func seedPersonalClientOrg(t *testing.T, db *sql.DB) (staffID, orgID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Staff', 'User')
		RETURNING id
	`, "personal-staff-"+uuid.NewString()+"@example.com").Scan(&staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO organizations (name, is_personal)
		VALUES ($1, TRUE)
		RETURNING id
	`, "Owner's Organization").Scan(&orgID); err != nil {
		t.Fatalf("insert personal org: %v", err)
	}

	var ownerRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'owner' AND organization_id IS NULL
	`).Scan(&ownerRoleID); err != nil {
		t.Fatalf("owner role: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, staffID, orgID, ownerRoleID); err != nil {
		t.Fatalf("staff membership: %v", err)
	}

	return staffID, orgID
}

// TestClientFlowsOnPersonalOrg prove the wedge does not break: provision,
// list, reset-credential, and remove all succeed on a personal workspace —
// the personal guard covers staff memberships only, clients remain allowed.
func TestClientFlowsOnPersonalOrg(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedPersonalClientOrg(t, db)
	email := "personal-client-" + uuid.NewString() + "@example.com"

	// Provision a client.
	provisioned, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     email,
		FirstName: "Client",
		LastName:  "User",
	})
	if err != nil {
		t.Fatalf("Provision on personal org: %v", err)
	}
	if !provisioned.CredentialIssued {
		t.Fatal("expected a one-time credential for a new client")
	}

	// List includes the client (and never the staff owner).
	clients, meta, err := svc.List(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("List on personal org: %v", err)
	}
	if meta.Total != 1 {
		t.Fatalf("client list total = %d, want 1", meta.Total)
	}
	if len(clients) != 1 || clients[0].UserID != provisioned.ClientID {
		t.Fatalf("client list = %+v, want the provisioned client", clients)
	}

	// Reset the credential.
	rotated, err := svc.ResetCredential(ctx, staffID, orgID, provisioned.ClientID)
	if err != nil {
		t.Fatalf("ResetCredential on personal org: %v", err)
	}
	if !rotated.CredentialIssued || rotated.OneTimePassword == "" {
		t.Fatalf("rotation result = %+v, want issued credential", rotated)
	}

	// Remove the unassigned client.
	if err := svc.Remove(ctx, staffID, orgID, provisioned.ClientID); err != nil {
		t.Fatalf("Remove on personal org: %v", err)
	}

	clients, meta, err = svc.List(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("List after remove: %v", err)
	}
	if meta.Total != 0 {
		t.Fatalf("client list total after remove = %d, want 0", meta.Total)
	}
}
