package assignment

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/project"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/validation"
)

type assignmentHTTPErrorResponse struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// assignmentHandler mirrors the production assignment route chain after
// authentication. The authenticated user is injected into the request context
// so the organization, membership, and permission middleware are exercised.
func assignmentHandler(db *sql.DB, service *Service) http.Handler {
	h := NewHandler(service)
	org := middleware.NewOrgMiddleware(db)
	access := middleware.NewOrgAccessMiddleware(db)
	rbac := middleware.NewRBACMiddleware(db)

	mux := http.NewServeMux()
	mux.Handle("POST /v1/orgs/{orgID}/projects/{projectID}/assignments", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("assignment.create")(http.HandlerFunc(h.Create)))))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("assignment.view")(http.HandlerFunc(h.GetByID)))))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("assignment.update")(http.HandlerFunc(h.Update)))))
	mux.Handle("DELETE /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("assignment.remove")(http.HandlerFunc(h.Delete)))))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/assignments", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("assignment.list")(http.HandlerFunc(h.ListByProjectID)))))
	return mux
}

func newAssignmentHTTPService(db *sql.DB) *Service {
	activityService := activity.NewService(activity.NewRepository(db))
	projectService := project.NewService(db, project.NewRepository(db), audit.NewService(db), activityService)
	return NewService(db, NewRepository(db), projectService, audit.NewService(db), activityService)
}

func doAssignmentRequest(t *testing.T, handler http.Handler, method, path string, userID uuid.UUID, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req = req.WithContext(requestctx.WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeAssignmentHTTPError(t *testing.T, rr *httptest.ResponseRecorder) assignmentHTTPErrorResponse {
	t.Helper()
	var response assignmentHTTPErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v; body=%s", err, rr.Body.String())
	}
	return response
}

func TestAssignmentHTTPWrongProjectIsNotFound(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	s := seedOrg(t, db, uuid.NewString())
	otherProjectID, _ := seedAdditionalAssignmentProject(t, db, s.OrgID, s.UserID)
	service := newTestService(db)
	assignment, err := service.Create(t.Context(), s.OrgID, s.UserID, s.ProjectID, s.Milestone, s.MemberID)
	if err != nil {
		t.Fatalf("create assignment: %v", err)
	}

	handler := assignmentHandler(db, service)
	path := "/v1/orgs/" + s.OrgID.String() + "/projects/" + otherProjectID.String() + "/assignments/" + assignment.ID.String()

	for _, tc := range []struct {
		name   string
		method string
		body   []byte
	}{
		{name: "read", method: http.MethodGet},
		{name: "update", method: http.MethodPatch, body: []byte(`{"assigned_to":"` + s.UserID.String() + `"}`)},
		{name: "delete", method: http.MethodDelete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := doAssignmentRequest(t, handler, tc.method, path, s.UserID, tc.body)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
			}
			if got := decodeAssignmentHTTPError(t, rr).Error.Code; got != "assignment_not_found" {
				t.Fatalf("error code = %q, want assignment_not_found", got)
			}

			unchanged, err := service.GetByID(t.Context(), s.OrgID, s.ProjectID, assignment.ID)
			if err != nil {
				t.Fatalf("read assignment after rejected wrong-project %s: %v", tc.name, err)
			}
			if unchanged.AssignedTo != assignment.AssignedTo {
				t.Fatalf("wrong-project HTTP %s changed assignee to %s", tc.name, unchanged.AssignedTo)
			}
			if unchanged.ProjectID == nil || *unchanged.ProjectID != s.ProjectID {
				t.Fatalf("wrong-project HTTP %s changed parent project to %v", tc.name, unchanged.ProjectID)
			}
		})
	}

	if _, err := service.GetByID(t.Context(), s.OrgID, s.ProjectID, assignment.ID); err != nil {
		t.Fatalf("wrong-project HTTP operations removed assignment: %v", err)
	}
}

func TestAssignmentCreateHTTPRejectsInvalidReferencesWithoutRows(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	s := seedOrg(t, db, uuid.NewString())
	other := seedOrg(t, db, uuid.NewString())
	_, otherMilestoneID := seedAdditionalAssignmentProject(t, db, s.OrgID, s.UserID)
	service := newTestService(db)
	handler := assignmentHandler(db, service)

	tests := []struct {
		name       string
		projectID  uuid.UUID
		milestone  uuid.UUID
		assignedTo uuid.UUID
		wantCode   string
	}{
		{name: "cross-org project", projectID: other.ProjectID, milestone: s.Milestone, assignedTo: s.MemberID, wantCode: "project_not_found"},
		{name: "cross-project milestone", projectID: s.ProjectID, milestone: otherMilestoneID, assignedTo: s.MemberID, wantCode: "milestone_not_found"},
		{name: "cross-org milestone", projectID: s.ProjectID, milestone: other.Milestone, assignedTo: s.MemberID, wantCode: "milestone_not_found"},
		{name: "invalid assignee", projectID: s.ProjectID, milestone: s.Milestone, assignedTo: uuid.New(), wantCode: "membership_not_found"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"milestone_id":"` + tc.milestone.String() + `","assigned_to":"` + tc.assignedTo.String() + `"}`)
			rr := doAssignmentRequest(t, handler, http.MethodPost, "/v1/orgs/"+s.OrgID.String()+"/projects/"+tc.projectID.String()+"/assignments", s.UserID, body)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
			}
			if got := decodeAssignmentHTTPError(t, rr).Error.Code; got != tc.wantCode {
				t.Fatalf("error code = %q, want %q", got, tc.wantCode)
			}

			var count int
			if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM assignments WHERE organization_id = $1`, s.OrgID).Scan(&count); err != nil {
				t.Fatalf("count assignments: %v", err)
			}
			if count != 0 {
				t.Fatalf("expected no assignment rows, got %d", count)
			}
		})
	}
}

func TestAssignmentListHTTPRejectsMissingOrCrossOrgProject(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	s := seedOrg(t, db, uuid.NewString())
	other := seedOrg(t, db, uuid.NewString())
	handler := assignmentHandler(db, newAssignmentHTTPService(db))

	for _, tc := range []struct {
		name      string
		orgID     uuid.UUID
		projectID uuid.UUID
	}{
		{name: "missing project", orgID: s.OrgID, projectID: uuid.New()},
		{name: "cross-org project", orgID: s.OrgID, projectID: other.ProjectID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/v1/orgs/" + tc.orgID.String() + "/projects/" + tc.projectID.String() + "/assignments"
			rr := doAssignmentRequest(t, handler, http.MethodGet, path, s.UserID, nil)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body=%s", rr.Code, rr.Body.String())
			}
			if got := decodeAssignmentHTTPError(t, rr).Error.Code; got != "project_not_found" {
				t.Fatalf("error code = %q, want project_not_found", got)
			}
		})
	}
}

func TestAssignmentListHTTPAllowsValidEmptyProject(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	s := seedOrg(t, db, uuid.NewString())
	emptyProjectID, milestoneID := seedAdditionalAssignmentProject(t, db, s.OrgID, s.UserID)
	if _, err := db.ExecContext(t.Context(), `DELETE FROM milestones WHERE id = $1`, milestoneID); err != nil {
		t.Fatalf("remove empty project's milestone: %v", err)
	}

	handler := assignmentHandler(db, newAssignmentHTTPService(db))
	path := "/v1/orgs/" + s.OrgID.String() + "/projects/" + emptyProjectID.String() + "/assignments"
	rr := doAssignmentRequest(t, handler, http.MethodGet, path, s.UserID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// assignmentDetailPayload mirrors the assignment detail JSON envelope.
type assignmentDetailPayload struct {
	Data struct {
		AssigneeName   *string   `json:"assignee_name"`
		AssignedByName *string   `json:"assigned_by_name"`
		CreatedAt      time.Time `json:"created_at"`
	} `json:"data"`
}

// assignmentListPayload mirrors the paginated assignment list JSON envelope.
type assignmentListPayload struct {
	Data struct {
		Items []struct {
			AssigneeName   *string   `json:"assignee_name"`
			AssignedByName *string   `json:"assigned_by_name"`
			CreatedAt      time.Time `json:"created_at"`
		} `json:"items"`
	} `json:"data"`
}

// TestAssignmentHTTPPayloadCarriesNamesAndCreatedAt: the assignment detail
// and list responses the frontend renders carry assignee_name,
// assigned_by_name, and a real (non-zero) created_at.
func TestAssignmentHTTPPayloadCarriesNamesAndCreatedAt(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	s := seedOrg(t, db, uuid.NewString())
	service := newAssignmentHTTPService(db)
	assignment, err := service.Create(t.Context(), s.OrgID, s.UserID, s.ProjectID, s.Milestone, s.MemberID)
	if err != nil {
		t.Fatalf("create assignment: %v", err)
	}

	handler := assignmentHandler(db, service)
	base := "/v1/orgs/" + s.OrgID.String() + "/projects/" + s.ProjectID.String() + "/assignments"

	// Detail payload.
	rr := doAssignmentRequest(t, handler, http.MethodGet, base+"/"+assignment.ID.String(), s.UserID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var detail assignmentDetailPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v; body=%s", err, rr.Body.String())
	}
	if detail.Data.AssigneeName == nil || *detail.Data.AssigneeName != "Member User" {
		t.Fatalf("detail assignee_name = %v, want %q", detail.Data.AssigneeName, "Member User")
	}
	if detail.Data.AssignedByName == nil || *detail.Data.AssignedByName != "Test User" {
		t.Fatalf("detail assigned_by_name = %v, want %q", detail.Data.AssignedByName, "Test User")
	}
	if detail.Data.CreatedAt.IsZero() {
		t.Fatalf("detail created_at = %v, want non-zero", detail.Data.CreatedAt)
	}

	// List payload.
	rr = doAssignmentRequest(t, handler, http.MethodGet, base, s.UserID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var list assignmentListPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v; body=%s", err, rr.Body.String())
	}
	if len(list.Data.Items) != 1 {
		t.Fatalf("list items = %d, want 1; body=%s", len(list.Data.Items), rr.Body.String())
	}
	item := list.Data.Items[0]
	if item.AssigneeName == nil || *item.AssigneeName != "Member User" {
		t.Fatalf("list assignee_name = %v, want %q", item.AssigneeName, "Member User")
	}
	if item.AssignedByName == nil || *item.AssignedByName != "Test User" {
		t.Fatalf("list assigned_by_name = %v, want %q", item.AssignedByName, "Test User")
	}
	if item.CreatedAt.IsZero() {
		t.Fatalf("list created_at = %v, want non-zero", item.CreatedAt)
	}
}
