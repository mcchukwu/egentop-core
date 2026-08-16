package project

// HTTP-contract tests for the project lifecycle slice: the due-date rule's
// 400 validation_error shape (create paths run in the handler), the DELETE
// 200/403/404 contract, the include_cancelled list parameter, the restore
// transition, and the freeze 400 on field edits.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/validation"
)

// lifecycleHandler wires the org/access/RBAC chain for the project lifecycle
// routes, mirroring the production route table.
func lifecycleHandler(db *sql.DB, service *Service) http.Handler {
	h := NewHandler(service)
	org := middleware.NewOrgMiddleware(db)
	access := middleware.NewOrgAccessMiddleware(db)
	rbac := middleware.NewRBACMiddleware(db)

	wrap := func(permission string, fn http.HandlerFunc) http.Handler {
		return org.LoadOrg(access.RequireMembership(rbac.RequirePermission(permission)(fn)))
	}

	mux := http.NewServeMux()
	mux.Handle("POST /v1/orgs/{orgID}/projects", wrap("project.create", h.Create))
	mux.Handle("GET /v1/orgs/{orgID}/projects", wrap("project.list", h.ListProjectsByOrganizationID))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}", wrap("project.view", h.GetProjectByID))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}", wrap("project.update", h.Update))
	mux.Handle("DELETE /v1/orgs/{orgID}/projects/{projectID}", wrap("project.update", h.Delete))
	mux.Handle("POST /v1/orgs/{orgID}/projects/{projectID}/milestones", wrap("milestone.create", h.CreateMilestone))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}", wrap("milestone.update", h.UpdateMilestone))
	return mux
}

func doLifecycleRequest(t *testing.T, handler http.Handler, method, path string, userID uuid.UUID, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req = req.WithContext(requestctx.WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// validationBody extracts the fields map from a 400 validation_error envelope.
func decodeValidationFields(t *testing.T, rr *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var env struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode validation error: %v; body=%s", err, rr.Body.String())
	}
	return env.Error.Fields
}

func decodeErrorCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error: %v; body=%s", err, rr.Body.String())
	}
	return env.Error.Code
}

// TestDueDateCreateValidation (AC-DD-1/2): POST project/milestone with a past
// due_date -> 400 validation_error with fields.DueDate and no row created;
// today/future/absent -> 201.
func TestDueDateCreateValidation(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	ctx := t.Context()
	svc := newTestService(db)
	handler := lifecycleHandler(db, svc)

	ownerID, orgID, _, _ := seedProject(t, db)
	projectID, milestoneID := seedAdditionalProject(t, db, orgID, ownerID)

	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	future := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	today := time.Now().UTC().Truncate(24 * time.Hour).Format(time.RFC3339)

	t.Run("project create past date rejected and no row created", func(t *testing.T) {
		body := []byte(`{"name":"Past Project","due_date":"` + past + `"}`)
		rr := doLifecycleRequest(t, handler, http.MethodPost, "/v1/orgs/"+orgID.String()+"/projects", ownerID, body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
		}
		if fields := decodeValidationFields(t, rr); fields["DueDate"] != dueDateInPastMessage {
			t.Fatalf("fields.DueDate = %q, want %q", fields["DueDate"], dueDateInPastMessage)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE name = 'Past Project'`).Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 0 {
			t.Fatalf("project rows = %d, want 0 (create must not happen)", count)
		}
	})

	t.Run("milestone create past date rejected and no row created", func(t *testing.T) {
		body := []byte(`{"title":"Past Milestone","due_date":"` + past + `"}`)
		path := "/v1/orgs/" + orgID.String() + "/projects/" + projectID.String() + "/milestones"
		rr := doLifecycleRequest(t, handler, http.MethodPost, path, ownerID, body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
		}
		if fields := decodeValidationFields(t, rr); fields["DueDate"] != dueDateInPastMessage {
			t.Fatalf("fields.DueDate = %q, want %q", fields["DueDate"], dueDateInPastMessage)
		}
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM milestones WHERE title = 'Past Milestone'`).Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 0 {
			t.Fatalf("milestone rows = %d, want 0", count)
		}
	})

	t.Run("project create today accepted", func(t *testing.T) {
		body := []byte(`{"name":"Today Project","due_date":"` + today + `"}`)
		rr := doLifecycleRequest(t, handler, http.MethodPost, "/v1/orgs/"+orgID.String()+"/projects", ownerID, body)
		if rr.Code != http.StatusCreated {
			t.Fatalf("today status = %d, want 201; body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("milestone create future accepted", func(t *testing.T) {
		body := []byte(`{"title":"Future Milestone","due_date":"` + future + `"}`)
		path := "/v1/orgs/" + orgID.String() + "/projects/" + projectID.String() + "/milestones"
		rr := doLifecycleRequest(t, handler, http.MethodPost, path, ownerID, body)
		if rr.Code != http.StatusCreated {
			t.Fatalf("future status = %d, want 201; body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("project create absent due_date accepted", func(t *testing.T) {
		rr := doLifecycleRequest(t, handler, http.MethodPost, "/v1/orgs/"+orgID.String()+"/projects", ownerID, []byte(`{"name":"No Date Project"}`))
		if rr.Code != http.StatusCreated {
			t.Fatalf("absent status = %d, want 201; body=%s", rr.Code, rr.Body.String())
		}
		var env struct {
			Data *struct {
				DueDate *string `json:"due_date"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode created project: %v", err)
		}
		if env.Data == nil || env.Data.DueDate != nil {
			t.Fatalf("created project due_date = %v, want absent", env.Data.DueDate)
		}
	})

	_ = milestoneID
}

// TestUpdateDueDatePastValidation (AC-DD-3): PATCH with a past due_date
// returns 400 validation_error with fields.DueDate and the previous date is
// intact.
func TestUpdateDueDatePastValidation(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	svc := newTestService(db)
	handler := lifecycleHandler(db, svc)

	ownerID, orgID, projectID, _ := seedProject(t, db)

	// Set a known date first (tomorrow).
	future := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	if _, err := svc.Update(t.Context(), ownerID, orgID, projectID, UpdateProjectRequest{
		DueDate: OptionalTime{Present: true, Value: &future},
	}); err != nil {
		t.Fatalf("set due date: %v", err)
	}

	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	path := "/v1/orgs/" + orgID.String() + "/projects/" + projectID.String()
	rr := doLifecycleRequest(t, handler, http.MethodPatch, path, ownerID,
		[]byte(`{"name":"Should Not Apply","due_date":"`+past+`"}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if fields := decodeValidationFields(t, rr); fields["DueDate"] != dueDateInPastMessage {
		t.Fatalf("fields.DueDate = %q, want %q", fields["DueDate"], dueDateInPastMessage)
	}

	after, err := svc.GetByID(t.Context(), orgID, projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	if after.Name != "Project" {
		t.Fatalf("name = %q, want unchanged Project", after.Name)
	}
	if after.DueDate == nil || !after.DueDate.Equal(future) {
		t.Fatalf("due date = %v, want previous %v", after.DueDate, future)
	}
}

// TestUpdateMilestoneDueDatePastValidation (AC-DD-5): the milestone PATCH path
// maps the service's past-due-date error to the same validation_error shape.
func TestUpdateMilestoneDueDatePastValidation(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	svc := newTestService(db)
	handler := lifecycleHandler(db, svc)

	ownerID, orgID, projectID, milestoneID := seedProject(t, db)

	past := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second).Format(time.RFC3339)
	path := "/v1/orgs/" + orgID.String() + "/projects/" + projectID.String() + "/milestones/" + milestoneID.String()
	rr := doLifecycleRequest(t, handler, http.MethodPatch, path, ownerID,
		[]byte(`{"title":"Should Not Apply","due_date":"`+past+`"}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if fields := decodeValidationFields(t, rr); fields["DueDate"] != dueDateInPastMessage {
		t.Fatalf("fields.DueDate = %q, want %q", fields["DueDate"], dueDateInPastMessage)
	}
}

// TestDeleteProjectHTTPContract (AC-DEL-4 + route contract): owner/admin
// delete -> 200 with a success envelope and data null; member/viewer -> 403;
// client -> 403; cross-org -> 404; second delete -> 404.
func TestDeleteProjectHTTPContract(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	svc := newTestService(db)
	handler := lifecycleHandler(db, svc)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	clientID := seedClient(t, db, orgID)
	if _, err := svc.AssignClient(t.Context(), ownerID, orgID, projectID, &clientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}

	memberID := seedProjectHandlerUser(t, db, orgID, membership.RoleMember)
	viewerID := seedProjectHandlerUser(t, db, orgID, membership.RoleViewer)

	projectPath := "/v1/orgs/" + orgID.String() + "/projects/" + projectID.String()

	t.Run("member 403", func(t *testing.T) {
		rr := doLifecycleRequest(t, handler, http.MethodDelete, projectPath, memberID, nil)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("member status = %d, want 403; body=%s", rr.Code, rr.Body.String())
		}
	})
	t.Run("viewer 403", func(t *testing.T) {
		rr := doLifecycleRequest(t, handler, http.MethodDelete, projectPath, viewerID, nil)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("viewer status = %d, want 403; body=%s", rr.Code, rr.Body.String())
		}
	})
	t.Run("client 403", func(t *testing.T) {
		rr := doLifecycleRequest(t, handler, http.MethodDelete, projectPath, clientID, nil)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("client status = %d, want 403; body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("owner 200 then 404", func(t *testing.T) {
		rr := doLifecycleRequest(t, handler, http.MethodDelete, projectPath, ownerID, nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("owner delete status = %d, want 200; body=%s", rr.Code, rr.Body.String())
		}
		var env struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Data    any    `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode success envelope: %v; body=%s", err, rr.Body.String())
		}
		if !env.Success || env.Message != "project deleted" {
			t.Fatalf("envelope = %+v, want success with message project deleted", env)
		}
		if env.Data != nil {
			t.Fatalf("data = %v, want null", env.Data)
		}

		// Second delete: 404 (already gone, indistinguishable from missing).
		rr = doLifecycleRequest(t, handler, http.MethodDelete, projectPath, ownerID, nil)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("second delete status = %d, want 404; body=%s", rr.Code, rr.Body.String())
		}
		if code := decodeErrorCode(t, rr); code != "project_not_found" {
			t.Fatalf("second delete error = %s, want project_not_found", code)
		}
	})

	t.Run("cross-org 404", func(t *testing.T) {
		_, _, project2, _ := seedProject(t, db)
		// Delete project2 (owned by another org) through org1's scope with an
		// org1 owner: the middleware passes (membership + project.update), and
		// the org-scoped project lookup 404s — no existence leak.
		rr := doLifecycleRequest(t, handler, http.MethodDelete, "/v1/orgs/"+orgID.String()+"/projects/"+project2.String(), ownerID, nil)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("cross-org delete status = %d, want 404; body=%s", rr.Code, rr.Body.String())
		}
		if code := decodeErrorCode(t, rr); code != "project_not_found" {
			t.Fatalf("cross-org delete error = %s, want project_not_found", code)
		}
	})
}

// TestProjectListIncludeCancelled (§14.2.4): the default list excludes
// cancelled from items and total; include_cancelled=true opts them back in;
// deleted is never included by any parameter.
func TestProjectListIncludeCancelled(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	ctx := t.Context()
	svc := newTestService(db)
	handler := lifecycleHandler(db, svc)

	ownerID, orgID, liveProject, _ := seedProject(t, db)
	cancelledProject, _ := seedAdditionalProject(t, db, orgID, ownerID)
	if _, err := db.ExecContext(ctx, `UPDATE projects SET status = 'cancelled' WHERE id = $1`, cancelledProject); err != nil {
		t.Fatalf("cancel project: %v", err)
	}
	deletedProject, _ := seedAdditionalProject(t, db, orgID, ownerID)
	if err := svc.Delete(ctx, ownerID, orgID, deletedProject); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	type listEnv struct {
		Data struct {
			Items []struct {
				ID uuid.UUID `json:"id"`
			} `json:"items"`
			Pagination struct {
				Total int `json:"total"`
			} `json:"pagination"`
		} `json:"data"`
	}

	ids := func(env listEnv) map[uuid.UUID]bool {
		m := map[uuid.UUID]bool{}
		for _, it := range env.Data.Items {
			m[it.ID] = true
		}
		return m
	}

	listPath := "/v1/orgs/" + orgID.String() + "/projects"

	rr := doLifecycleRequest(t, handler, http.MethodGet, listPath, ownerID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("default list status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var env listEnv
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	got := ids(env)
	if !got[liveProject] {
		t.Fatal("default list missing the live project")
	}
	if got[cancelledProject] {
		t.Fatal("default list includes a cancelled project")
	}
	if got[deletedProject] {
		t.Fatal("default list includes a deleted project")
	}

	// include_cancelled=true brings cancelled back in (still no deleted).
	rr = doLifecycleRequest(t, handler, http.MethodGet, listPath+"?include_cancelled=true", ownerID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("include_cancelled list status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var withClosed listEnv
	if err := json.Unmarshal(rr.Body.Bytes(), &withClosed); err != nil {
		t.Fatalf("decode closed list: %v", err)
	}
	gotClosed := ids(withClosed)
	if !gotClosed[cancelledProject] {
		t.Fatal("include_cancelled list missing the cancelled project")
	}
	if gotClosed[deletedProject] {
		t.Fatal("include_cancelled list includes a deleted project (never allowed)")
	}

	// Anything other than the exact "true" excludes cancelled.
	rr = doLifecycleRequest(t, handler, http.MethodGet, listPath+"?include_cancelled=1", ownerID, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("include_cancelled=1 list status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var withBad listEnv
	if err := json.Unmarshal(rr.Body.Bytes(), &withBad); err != nil {
		t.Fatalf("decode bad param list: %v", err)
	}
	if ids(withBad)[cancelledProject] {
		t.Fatal("include_cancelled=1 must not include cancelled (exact true only)")
	}
}

// TestRestoreHTTP (AC-RESTORE-1/4): PATCH archived {status: active} -> 200
// with active; a role without project.update -> 403; a cancelled project's
// restore attempt -> 400 invalid_status_transition.
func TestRestoreHTTP(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	svc := newTestService(db)
	handler := lifecycleHandler(db, svc)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	memberID := seedProjectHandlerUser(t, db, orgID, membership.RoleMember)

	if _, err := svc.Update(t.Context(), ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusArchived}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	path := "/v1/orgs/" + orgID.String() + "/projects/" + projectID.String()

	t.Run("member restore 403", func(t *testing.T) {
		rr := doLifecycleRequest(t, handler, http.MethodPatch, path, memberID, []byte(`{"status":"active"}`))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("member restore status = %d, want 403; body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("owner restore 200", func(t *testing.T) {
		rr := doLifecycleRequest(t, handler, http.MethodPatch, path, ownerID, []byte(`{"status":"active"}`))
		if rr.Code != http.StatusOK {
			t.Fatalf("owner restore status = %d, want 200; body=%s", rr.Code, rr.Body.String())
		}
		var env struct {
			Data struct {
				Status ProjectStatus `json:"status"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode restore: %v", err)
		}
		if env.Data.Status != ProjectStatusActive {
			t.Fatalf("restored status = %s, want active", env.Data.Status)
		}
	})

	t.Run("cancelled restore 400", func(t *testing.T) {
		owner2, org2, cancelledProject, _ := seedProject(t, db)
		if _, err := svc.Update(t.Context(), owner2, org2, cancelledProject, UpdateProjectRequest{Status: ProjectStatusCancelled}); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		cancelledPath := "/v1/orgs/" + org2.String() + "/projects/" + cancelledProject.String()
		rr := doLifecycleRequest(t, handler, http.MethodPatch, cancelledPath, owner2, []byte(`{"status":"active"}`))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("cancelled restore status = %d, want 400; body=%s", rr.Code, rr.Body.String())
		}
		if code := decodeErrorCode(t, rr); code != "invalid_status_transition" {
			t.Fatalf("cancelled restore error = %s, want invalid_status_transition", code)
		}
	})
}

// TestFreezeHTTP (AC-LC-1/2): PATCH a field (no status) on archived/cancelled
// projects -> 400 invalid_status_transition and the GET shows the original.
func TestFreezeHTTP(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	svc := newTestService(db)
	handler := lifecycleHandler(db, svc)

	ownerID, orgID, projectID, _ := seedProject(t, db)
	path := "/v1/orgs/" + orgID.String() + "/projects/" + projectID.String()

	if _, err := svc.Update(t.Context(), ownerID, orgID, projectID, UpdateProjectRequest{Status: ProjectStatusArchived}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	rr := doLifecycleRequest(t, handler, http.MethodPatch, path, ownerID, []byte(`{"name":"Frozen Name"}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("archived field-edit status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if code := decodeErrorCode(t, rr); code != "invalid_status_transition" {
		t.Fatalf("archived field-edit error = %s, want invalid_status_transition", code)
	}

	after, err := svc.GetByID(t.Context(), orgID, projectID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	if after.Name != "Project" {
		t.Fatalf("name = %q, want unchanged Project (400 must mutate nothing)", after.Name)
	}

	// Cancelled blocks field edits too.
	if _, err := db.ExecContext(t.Context(), `UPDATE projects SET status = 'cancelled' WHERE id = $1`, projectID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	rr = doLifecycleRequest(t, handler, http.MethodPatch, path, ownerID, []byte(`{"priority":"high"}`))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cancelled field-edit status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if code := decodeErrorCode(t, rr); code != "invalid_status_transition" {
		t.Fatalf("cancelled field-edit error = %s, want invalid_status_transition", code)
	}
}
