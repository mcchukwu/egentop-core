package project

// HTTP-level regression tests for the agency-only revision-limit setters:
//   - PATCH /v1/orgs/{orgID}/projects/{projectID}/revision-limit
//   - PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/revision-limit
//
// Covers: project default set + read back, milestone override + effective
// limit, null clears the override (falls back to the project default), values
// < 1 are rejected with a field validation error, the change is audited, and
// the client-facing surfaces still hide the limit fields.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/validation"
)

// TestRevisionLimitSettersHTTP drives both setters over HTTP and verifies the
// DB column values, the effective read-side limit, audit rows, and the
// client-facing field hiding.
func TestRevisionLimitSettersHTTP(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	handler, _ := layer1HTTPHarness(t, db)
	ctx := context.Background()

	staffToken, orgID, staffID := registerStaffViaHTTP(t, handler, db)
	clientID, oneTime := provisionClientViaHTTP(t, handler, staffToken, orgID)
	var clientEmail string
	if err := db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, uuid.MustParse(clientID)).Scan(&clientEmail); err != nil {
		t.Fatalf("resolve client email: %v", err)
	}
	clientToken := clientLoginAndRotate(t, handler, clientEmail, oneTime)

	projectService := newTestService(db)
	project, err := projectService.Create(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), CreateProjectRequest{Name: "Limit Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	parsedClientID := uuid.MustParse(clientID)
	if _, err := projectService.AssignClient(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, &parsedClientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}
	milestone, err := projectService.CreateMilestone(ctx, uuid.MustParse(orgID), project.ID, uuid.MustParse(staffID), CreateMilestoneInput{Title: "Limit Milestone"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	projectLimitPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/revision-limit"
	milestoneLimitPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/milestones/" + milestone.ID.String() + "/revision-limit"

	// --- Set the project-level limit via HTTP (staff) ---
	rr, env := httpDo(t, handler, http.MethodPatch, projectLimitPath, staffToken, []byte(`{"revision_limit": 3}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("set project limit status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if !env.Success {
		t.Fatalf("set project limit envelope = %+v, want success", env.Error)
	}

	// The column is set and the milestone read exposes the effective limit.
	var projectLimit *int
	if err := db.QueryRowContext(ctx, `SELECT revision_limit FROM projects WHERE id = $1`, project.ID).Scan(&projectLimit); err != nil {
		t.Fatalf("read project limit: %v", err)
	}
	if projectLimit == nil || *projectLimit != 3 {
		t.Fatalf("project revision_limit = %v, want 3", projectLimit)
	}

	milestonePath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/milestones/" + milestone.ID.String()
	rr, env = httpDo(t, handler, http.MethodGet, milestonePath, staffToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("read milestone status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("decode milestone: %v", err)
	}
	if raw["revision_limit"] != float64(3) {
		t.Fatalf("effective milestone revision_limit = %v, want 3", raw["revision_limit"])
	}

	// --- Per-milestone override beats the project default ---
	rr, env = httpDo(t, handler, http.MethodPatch, milestoneLimitPath, staffToken, []byte(`{"revision_limit": 5}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("set milestone override status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("decode milestone override response: %v", err)
	}
	if raw["revision_limit"] != float64(5) {
		t.Fatalf("effective limit after override = %v, want 5", raw["revision_limit"])
	}
	var milestoneOverride *int
	if err := db.QueryRowContext(ctx, `SELECT revision_limit FROM milestones WHERE id = $1`, milestone.ID).Scan(&milestoneOverride); err != nil {
		t.Fatalf("read milestone override: %v", err)
	}
	if milestoneOverride == nil || *milestoneOverride != 5 {
		t.Fatalf("milestone revision_limit = %v, want 5", milestoneOverride)
	}

	// --- null clears the override; the project default becomes effective ---
	rr, env = httpDo(t, handler, http.MethodPatch, milestoneLimitPath, staffToken, []byte(`{"revision_limit": null}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("clear milestone override status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("decode cleared override response: %v", err)
	}
	if raw["revision_limit"] != float64(3) {
		t.Fatalf("effective limit after clearing override = %v, want 3 (project default)", raw["revision_limit"])
	}
	if err := db.QueryRowContext(ctx, `SELECT revision_limit FROM milestones WHERE id = $1`, milestone.ID).Scan(&milestoneOverride); err != nil {
		t.Fatalf("re-read milestone override: %v", err)
	}
	if milestoneOverride != nil {
		t.Fatalf("milestone revision_limit after clear = %v, want NULL", milestoneOverride)
	}

	// --- Absent field also clears the override (nullable DTO convention) ---
	rr, env = httpDo(t, handler, http.MethodPatch, milestoneLimitPath, staffToken, []byte(`{}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("absent-field clear status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// --- Values below 1 are rejected with a field validation error ---
	for _, body := range []string{`{"revision_limit": 0}`, `{"revision_limit": -3}`} {
		rr, env = httpDo(t, handler, http.MethodPatch, projectLimitPath, staffToken, []byte(body))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("reject %s status = %d, want 400; body=%s", body, rr.Code, rr.Body.String())
		}
		if env.Error == nil || env.Error.Code != "validation_error" {
			t.Fatalf("reject %s error = %v, want validation_error", body, env.Error)
		}
		var vresp ValidationErrorResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &vresp); err != nil {
			t.Fatalf("decode validation response: %v", err)
		}
		if _, ok := vresp.Error.Fields["revision_limit"]; !ok {
			t.Fatalf("reject %s fields = %v, want revision_limit field", body, vresp.Error.Fields)
		}
	}

	// The rejected writes left the project limit untouched.
	if err := db.QueryRowContext(ctx, `SELECT revision_limit FROM projects WHERE id = $1`, project.ID).Scan(&projectLimit); err != nil {
		t.Fatalf("re-read project limit: %v", err)
	}
	if projectLimit == nil || *projectLimit != 3 {
		t.Fatalf("project revision_limit after rejected writes = %v, want 3", projectLimit)
	}

	// --- The changes are audited (versioned before/after metadata) ---
	if got := countAuditRows(t, db, "project.revision_limit_changed", project.ID); got != 1 {
		t.Fatalf("project.revision_limit_changed audit rows = %d, want 1", got)
	}
	if got := countAuditRows(t, db, "milestone.revision_limit_changed", milestone.ID); got != 3 {
		t.Fatalf("milestone.revision_limit_changed audit rows = %d, want 3 (override, clear, absent-clear)", got)
	}

	// --- Client-facing milestone detail still hides the limit fields ---
	rr, env = httpDo(t, handler, http.MethodGet, milestonePath, clientToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("client milestone detail status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var clientRaw map[string]any
	if err := json.Unmarshal(env.Data, &clientRaw); err != nil {
		t.Fatalf("decode client detail: %v", err)
	}
	if _, ok := clientRaw["revision_limit"]; ok {
		t.Fatalf("client detail must NOT expose revision_limit after the setter ran; raw=%v", clientRaw)
	}
	if _, ok := clientRaw["limit_reached"]; ok {
		t.Fatalf("client detail must NOT expose limit_reached after the setter ran; raw=%v", clientRaw)
	}
}

// TestProjectRevisionLimitOnProjectPayloadsHTTP verifies that the project
// list + detail payloads expose the stored project-level revision_limit as-is
// for staff actors, while client-role actors never see it: client detail over
// HTTP (project.view), and the client list endpoint is blocked at the RBAC
// boundary (project.list is staff-only).
func TestProjectRevisionLimitOnProjectPayloadsHTTP(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	handler, _ := layer1HTTPHarness(t, db)
	ctx := context.Background()

	staffToken, orgID, staffID := registerStaffViaHTTP(t, handler, db)
	clientID, oneTime := provisionClientViaHTTP(t, handler, staffToken, orgID)
	var clientEmail string
	if err := db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, uuid.MustParse(clientID)).Scan(&clientEmail); err != nil {
		t.Fatalf("resolve client email: %v", err)
	}
	clientToken := clientLoginAndRotate(t, handler, clientEmail, oneTime)

	projectService := newTestService(db)
	project, err := projectService.Create(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), CreateProjectRequest{Name: "Limit Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	setProjectRevisionLimit(t, db, project.ID, 3)
	parsedClientID := uuid.MustParse(clientID)
	if _, err := projectService.AssignClient(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, &parsedClientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}

	projectPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String()
	listPath := "/v1/orgs/" + orgID + "/projects"

	// --- Staff detail exposes the stored project limit as-is ---
	rr, env := httpDo(t, handler, http.MethodGet, projectPath, staffToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("staff project detail status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("decode staff detail: %v", err)
	}
	if raw["revision_limit"] != float64(3) {
		t.Fatalf("staff detail revision_limit = %v, want 3; raw=%v", raw["revision_limit"], raw)
	}

	// --- Staff list exposes the stored project limit per item ---
	rr, env = httpDo(t, handler, http.MethodGet, listPath, staffToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("staff list status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var listResp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(env.Data, &listResp); err != nil {
		t.Fatalf("decode staff list: %v", err)
	}
	if len(listResp.Items) != 1 {
		t.Fatalf("staff list items = %d, want 1; items=%v", len(listResp.Items), listResp.Items)
	}
	if listResp.Items[0]["revision_limit"] != float64(3) {
		t.Fatalf("staff list item revision_limit = %v, want 3; item=%v", listResp.Items[0]["revision_limit"], listResp.Items[0])
	}

	// --- Client detail hides the limit fields (project.view) ---
	rr, env = httpDo(t, handler, http.MethodGet, projectPath, clientToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("client detail status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var clientRaw map[string]any
	if err := json.Unmarshal(env.Data, &clientRaw); err != nil {
		t.Fatalf("decode client detail: %v", err)
	}
	if _, ok := clientRaw["revision_limit"]; ok {
		t.Fatalf("client detail must NOT expose revision_limit; raw=%v", clientRaw)
	}

	// --- Client list is blocked at the RBAC boundary (project.list is
	// staff-only) ---
	rr, env = httpDo(t, handler, http.MethodGet, listPath, clientToken, nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("client list status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}

// TestClientProjectListExcludesRevisionLimit drives the list handler directly
// with a client role injected into the request context (bypassing RBAC) to
// prove the handler itself excludes revision_limit from client-role list
// responses — defense in depth behind the project.list permission boundary.
func TestClientProjectListExcludesRevisionLimit(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	svc := newTestService(db)
	_, orgID, projectID, _ := seedProject(t, db)
	setProjectRevisionLimit(t, db, projectID, 4)

	handler := NewHandler(svc)
	mux := http.NewServeMux()
	mux.Handle("GET /v1/orgs/{orgID}/projects", http.HandlerFunc(handler.ListProjectsByOrganizationID))

	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+orgID.String()+"/projects", nil)
	req = req.WithContext(requestctx.WithOrganizationID(req.Context(), orgID))
	req = req.WithContext(requestctx.WithRole(req.Context(), "client"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("client-role list status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var env apiEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v; body=%s", err, rr.Body.String())
	}
	var listResp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(env.Data, &listResp); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	if len(listResp.Items) != 1 {
		t.Fatalf("client-role list items = %d, want 1; items=%v", len(listResp.Items), listResp.Items)
	}
	if _, ok := listResp.Items[0]["revision_limit"]; ok {
		t.Fatalf("client-role list item must NOT expose revision_limit; item=%v", listResp.Items[0])
	}
}

// ValidationErrorResponse mirrors response.ValidationErrorResponse for
// asserting the fields payload.
type ValidationErrorResponse struct {
	Error struct {
		Code   string            `json:"code"`
		Fields map[string]string `json:"fields"`
	} `json:"error"`
}
