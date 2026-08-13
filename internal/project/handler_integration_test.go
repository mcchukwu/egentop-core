package project

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/validation"
)

type projectHTTPErrorResponse struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// projectMilestoneHandler mirrors the production route chain after
// authentication. The test supplies an authenticated user in the request
// context, while the organization, membership, and permission checks remain
// part of the HTTP harness.
func projectMilestoneHandler(db *sql.DB, service *Service) http.Handler {
	h := NewHandler(service)
	org := middleware.NewOrgMiddleware(db)
	access := middleware.NewOrgAccessMiddleware(db)
	rbac := middleware.NewRBACMiddleware(db)

	mux := http.NewServeMux()
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("milestone.view")(http.HandlerFunc(h.GetMilestoneByID)))))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("milestone.update")(http.HandlerFunc(h.UpdateMilestone)))))
	return mux
}

func doProjectMilestoneRequest(t *testing.T, handler http.Handler, method string, path string, userID uuid.UUID, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req = req.WithContext(requestctx.WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeProjectHTTPError(t *testing.T, rr *httptest.ResponseRecorder) projectHTTPErrorResponse {
	t.Helper()

	var response projectHTTPErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, rr.Body.String())
	}
	return response
}

func TestMilestoneHTTPWrongProjectIsNotFound(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)
	otherProjectID, _ := seedAdditionalProject(t, db, orgID, ownerID)
	adminID := seedProjectHandlerUser(t, db, orgID, membership.RoleAdmin)
	handler := projectMilestoneHandler(db, newTestService(db))
	path := "/v1/orgs/" + orgID.String() + "/projects/" + otherProjectID.String() + "/milestones/" + milestoneID.String()

	t.Run("read", func(t *testing.T) {
		rr := doProjectMilestoneRequest(t, handler, http.MethodGet, path, adminID, nil)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
		}
		if got := decodeProjectHTTPError(t, rr).Error.Code; got != "milestone_not_found" {
			t.Fatalf("error code = %q, want milestone_not_found", got)
		}
	})

	t.Run("update", func(t *testing.T) {
		rr := doProjectMilestoneRequest(t, handler, http.MethodPatch, path, adminID, []byte(`{"title":"Should Not Update"}`))
		if rr.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
		}
		if got := decodeProjectHTTPError(t, rr).Error.Code; got != "milestone_not_found" {
			t.Fatalf("error code = %q, want milestone_not_found", got)
		}
	})

	milestone, err := newTestService(db).GetMilestoneByID(t.Context(), orgID, projectID, milestoneID)
	if err != nil {
		t.Fatalf("read milestone after rejected HTTP update: %v", err)
	}
	if milestone.Title != "Milestone" {
		t.Fatalf("wrong-project HTTP update changed title to %q", milestone.Title)
	}
}

func seedProjectHandlerUser(t *testing.T, db *sql.DB, orgID uuid.UUID, role membership.Role) uuid.UUID {
	t.Helper()

	var userID, roleID uuid.UUID
	if err := db.QueryRowContext(t.Context(), `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Handler', 'User') RETURNING id
	`, "project-handler-"+uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert handler user: %v", err)
	}
	if err := db.QueryRowContext(t.Context(), `
		SELECT id FROM roles WHERE name = $1 AND organization_id IS NULL
	`, role).Scan(&roleID); err != nil {
		t.Fatalf("resolve handler role: %v", err)
	}
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, userID, orgID, roleID); err != nil {
		t.Fatalf("insert handler membership: %v", err)
	}
	return userID
}
