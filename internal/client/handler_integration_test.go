package client

// HTTP-level integration tests for DELETE /v1/orgs/{orgID}/clients/{userID}:
// unassigned client removed (membership gone, user row survives), assigned
// client -> 409 client_attached_to_project, non-client target -> 404, and the
// route is staff-only (client.provision).

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/requestctx"
)

type clientHTTPErrorResponse struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// clientRemoveHandler mirrors the production route chain after
// authentication: LoadOrg -> RequireMembership -> RequirePermission
// (client.provision) -> handler.
func clientRemoveHandler(db *sql.DB, service *Service) http.Handler {
	h := NewHandler(service)
	org := middleware.NewOrgMiddleware(db)
	access := middleware.NewOrgAccessMiddleware(db)
	rbac := middleware.NewRBACMiddleware(db)

	mux := http.NewServeMux()
	mux.Handle("DELETE /v1/orgs/{orgID}/clients/{userID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("client.provision")(http.HandlerFunc(h.Remove)))))
	return mux
}

// clientListRemoveHandler adds the list route (client.list) to the remove
// harness so a test can observe the membership disappearance through the API.
func clientListRemoveHandler(db *sql.DB, service *Service) http.Handler {
	h := NewHandler(service)
	org := middleware.NewOrgMiddleware(db)
	access := middleware.NewOrgAccessMiddleware(db)
	rbac := middleware.NewRBACMiddleware(db)

	mux := http.NewServeMux()
	mux.Handle("GET /v1/orgs/{orgID}/clients", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("client.list")(http.HandlerFunc(h.List)))))
	mux.Handle("DELETE /v1/orgs/{orgID}/clients/{userID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("client.provision")(http.HandlerFunc(h.Remove)))))
	return mux
}

func doClientRemoveRequest(t *testing.T, handler http.Handler, orgID, userID, actorID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	path := "/v1/orgs/" + orgID.String() + "/clients/" + userID.String()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req = req.WithContext(requestctx.WithUserID(req.Context(), actorID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeClientHTTPError(t *testing.T, rr *httptest.ResponseRecorder) clientHTTPErrorResponse {
	t.Helper()

	var response clientHTTPErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, rr.Body.String())
	}
	return response
}

// TestRemoveUnassignedClientHTTP: a provisioned-but-unassigned client is
// removed (membership gone, users row survives) and the removal is audited.
func TestRemoveUnassignedClientHTTP(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)
	provisioned, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     "remove-" + uuid.NewString() + "@example.com",
		FirstName: "Remove",
		LastName:  "Me",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	handler := clientRemoveHandler(db, svc)
	rr := doClientRemoveRequest(t, handler, orgID, provisioned.ClientID, staffID)
	if rr.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Membership is gone; the user row survives.
	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, provisioned.ClientID, orgID).Scan(&membershipCount); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if membershipCount != 0 {
		t.Fatalf("membership count = %d, want 0", membershipCount)
	}

	var userCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM users WHERE id = $1
	`, provisioned.ClientID).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("users row count = %d, want 1 (the user is never deleted)", userCount)
	}

	// The removal is audited with the prune-path metadata convention.
	var auditCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE action = 'membership.removed'
		AND organization_id = $1
		AND metadata->>'reason' = 'client removed by agency'
	`, orgID).Scan(&auditCount); err != nil {
		t.Fatalf("count removal audits: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("removal audit rows = %d, want 1", auditCount)
	}
}

// TestRemoveAssignedClientHTTP: a client attached to a project cannot be
// removed directly -> 409 client_attached_to_project and the membership
// survives.
func TestRemoveAssignedClientHTTP(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)
	provisioned, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     "attached-" + uuid.NewString() + "@example.com",
		FirstName: "Attached",
		LastName:  "Client",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Attach the client to a project (direct SQL; the project package owns the
	// assign flow, we only need the FK state).
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (organization_id, created_by, name, status, client_id)
		VALUES ($1, $2, 'Attached Project', 'active', $3)
	`, orgID, staffID, provisioned.ClientID); err != nil {
		t.Fatalf("insert attached project: %v", err)
	}

	handler := clientRemoveHandler(db, svc)
	rr := doClientRemoveRequest(t, handler, orgID, provisioned.ClientID, staffID)
	if rr.Code != http.StatusConflict {
		t.Fatalf("remove status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeClientHTTPError(t, rr).Error.Code; got != "client_attached_to_project" {
		t.Fatalf("error code = %q, want client_attached_to_project", got)
	}

	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, provisioned.ClientID, orgID).Scan(&membershipCount); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("membership count = %d, want 1 (rejected removal)", membershipCount)
	}
}

// TestRemoveNonClientHTTP: a user who is not an active client of the org (and
// an unknown user) resolves to 404 client_not_found.
func TestRemoveNonClientHTTP(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)

	// A staff member in the org is not a client.
	var staffMemberID, memberRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Not', 'Client') RETURNING id
	`, "not-client-"+uuid.NewString()+"@example.com").Scan(&staffMemberID); err != nil {
		t.Fatalf("insert staff user: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = $1 AND organization_id IS NULL
	`, membership.RoleMember).Scan(&memberRoleID); err != nil {
		t.Fatalf("member role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, staffMemberID, orgID, memberRoleID); err != nil {
		t.Fatalf("staff membership: %v", err)
	}

	handler := clientRemoveHandler(db, svc)

	rr := doClientRemoveRequest(t, handler, orgID, staffMemberID, staffID)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("remove staff-member status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeClientHTTPError(t, rr).Error.Code; got != "client_not_found" {
		t.Fatalf("error code = %q, want client_not_found", got)
	}

	// Unknown user: same 404.
	rr = doClientRemoveRequest(t, handler, orgID, uuid.New(), staffID)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("remove unknown status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeClientHTTPError(t, rr).Error.Code; got != "client_not_found" {
		t.Fatalf("error code = %q, want client_not_found", got)
	}
}

// TestRemoveRequiresClientProvisionPermission: the route is staff-only; a
// member-role actor (no client.provision key) gets 403 forbidden.
func TestRemoveRequiresClientProvisionPermission(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)
	provisioned, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     "perm-" + uuid.NewString() + "@example.com",
		FirstName: "Perm",
		LastName:  "Client",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	// A member-role actor in the org.
	var memberID, memberRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Member', 'Actor') RETURNING id
	`, "member-actor-"+uuid.NewString()+"@example.com").Scan(&memberID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = $1 AND organization_id IS NULL
	`, membership.RoleMember).Scan(&memberRoleID); err != nil {
		t.Fatalf("member role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, memberID, orgID, memberRoleID); err != nil {
		t.Fatalf("member membership: %v", err)
	}

	handler := clientRemoveHandler(db, svc)
	rr := doClientRemoveRequest(t, handler, orgID, provisioned.ClientID, memberID)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("member remove status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeClientHTTPError(t, rr).Error.Code; got != "forbidden" {
		t.Fatalf("error code = %q, want forbidden", got)
	}

	// The client still holds their membership.
	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, provisioned.ClientID, orgID).Scan(&membershipCount); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("membership count = %d, want 1", membershipCount)
	}
}

// TestRemoveDisappearsFromClientsListHTTP: after a successful removal, GET
// /clients no longer lists the client (observed through the API, not just the
// DB), while the users row still exists.
func TestRemoveDisappearsFromClientsListHTTP(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)
	provisioned, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     "list-" + uuid.NewString() + "@example.com",
		FirstName: "List",
		LastName:  "Me",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	handler := clientListRemoveHandler(db, svc)

	// The client is listed before removal.
	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+orgID.String()+"/clients", nil)
	req = req.WithContext(requestctx.WithUserID(req.Context(), staffID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list before removal status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var listEnv struct {
		Data struct {
			Pagination struct {
				Total int `json:"total"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode list before removal: %v; body=%s", err, rr.Body.String())
	}
	if listEnv.Data.Pagination.Total != 1 {
		t.Fatalf("clients listed before removal = %d, want 1", listEnv.Data.Pagination.Total)
	}

	// Remove through the API.
	rr = doClientRemoveRequest(t, handler, orgID, provisioned.ClientID, staffID)
	if rr.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// The client is gone from the API listing.
	req = httptest.NewRequest(http.MethodGet, "/v1/orgs/"+orgID.String()+"/clients", nil)
	req = req.WithContext(requestctx.WithUserID(req.Context(), staffID))
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if err := json.Unmarshal(rr.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode list after removal: %v; body=%s", err, rr.Body.String())
	}
	if listEnv.Data.Pagination.Total != 0 {
		t.Fatalf("clients listed after removal = %d, want 0", listEnv.Data.Pagination.Total)
	}

	// The users row survives (removal never deletes the user).
	var userCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = $1`, provisioned.ClientID).Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("users row count = %d, want 1", userCount)
	}
}
