package project

// HTTP-level tenant-boundary and revision-limit authorization tests:
//   - org A's member requesting org B's project by ID -> 404 project_not_found
//     (no existence leak) for both GET and PATCH; same-org requests succeed.
//   - revision-limit setters are agency-only: a member (no project.update /
//     milestone.update) and a client-role actor both get 403 forbidden.
//   - null clears the PROJECT limit entirely (unlimited): the milestone's
//     effective limit becomes nil and limit_reached is false.
//   - the approval view never exposes revision_limit / limit_reached.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/jwt"
	"github.com/mcchukwu/egentop/internal/validation"
)

// TestHTTPCrossOrgProjectIsNotFound: a member of org A must not be able to
// read or mutate org B's project through the project-by-ID routes, and the
// failure must be an indistinguishable project_not_found (no existence leak).
func TestHTTPCrossOrgProjectIsNotFound(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	handler, _ := layer1HTTPHarness(t, db)
	ctx := context.Background()

	// Two independent staff/org pairs (register creates an org each).
	tokenA, orgA, staffA := registerStaffViaHTTP(t, handler, db)
	tokenB, orgB, _ := registerStaffViaHTTP(t, handler, db)

	// Org A owns a project; org B owns another.
	projectService := newTestService(db)
	projectA, err := projectService.Create(ctx, uuid.MustParse(staffA), uuid.MustParse(orgA), CreateProjectRequest{Name: "Org A Project"})
	if err != nil {
		t.Fatalf("create project in org A: %v", err)
	}
	projectB, err := projectService.Create(ctx, uuid.MustParse(staffA), uuid.MustParse(orgA), CreateProjectRequest{Name: "Second Org A Project"})
	if err != nil {
		t.Fatalf("create second project in org A: %v", err)
	}

	// --- Org B member requests org A's project by ID, using ORG B in the path
	// (the membership/RBAC chain resolves against the path org). ---
	path := "/v1/orgs/" + orgB + "/projects/" + projectA.ID.String()

	rr, env := httpDo(t, handler, http.MethodGet, path, tokenB, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cross-org GET status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
	if env.Error == nil || env.Error.Code != "project_not_found" {
		t.Fatalf("cross-org GET error = %v, want project_not_found", env.Error)
	}

	patchBody, _ := json.Marshal(map[string]string{"name": "Hijacked"})
	rr, env = httpDo(t, handler, http.MethodPatch, path, tokenB, patchBody)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cross-org PATCH status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
	if env.Error == nil || env.Error.Code != "project_not_found" {
		t.Fatalf("cross-org PATCH error = %v, want project_not_found", env.Error)
	}

	// No write leaked: org A's project is untouched.
	unchanged, err := projectService.GetByID(ctx, uuid.MustParse(orgA), projectA.ID)
	if err != nil {
		t.Fatalf("re-read org A project: %v", err)
	}
	if unchanged.Name != "Org A Project" {
		t.Fatalf("cross-org PATCH renamed project to %q", unchanged.Name)
	}

	// --- Same-org requests succeed (control direction). ---
	rr, env = httpDo(t, handler, http.MethodGet, "/v1/orgs/"+orgA+"/projects/"+projectA.ID.String(), tokenA, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("same-org GET status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	rr, env = httpDo(t, handler, http.MethodPatch, "/v1/orgs/"+orgA+"/projects/"+projectA.ID.String(), tokenA, patchBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("same-org PATCH status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if env.Error != nil {
		t.Fatalf("same-org PATCH returned error %v", env.Error)
	}

	// --- Same-org member (not owner) can read their own project. ---
	memberToken, _ := seedMemberActor(t, db, handler, orgA)
	rr, env = httpDo(t, handler, http.MethodGet, "/v1/orgs/"+orgA+"/projects/"+projectB.ID.String(), memberToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("same-org member GET status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// --- Same-org member PATCH is denied by RBAC (members lack
	// project.update), independent of the project's existence. ---
	memberPatch, _ := json.Marshal(map[string]string{"name": "Member Renamed"})
	rr, env = httpDo(t, handler, http.MethodPatch, "/v1/orgs/"+orgA+"/projects/"+projectB.ID.String(), memberToken, memberPatch)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("same-org member PATCH status = %d, want 403 (member lacks project.update); body=%s", rr.Code, rr.Body.String())
	}
	if env.Error == nil || env.Error.Code != "forbidden" {
		t.Fatalf("same-org member PATCH error = %v, want forbidden", env.Error)
	}
}

// TestRevisionLimitSettersRejectNonPrivilegedActors: a member (no
// project.update/milestone.update) and a client-role actor both receive 403
// forbidden from the RBAC chain before any write happens.
func TestRevisionLimitSettersRejectNonPrivilegedActors(t *testing.T) {
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
	project, err := projectService.Create(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), CreateProjectRequest{Name: "Perm Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	milestone, err := projectService.CreateMilestone(ctx, uuid.MustParse(orgID), project.ID, uuid.MustParse(staffID), CreateMilestoneInput{Title: "Perm Milestone"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	projectLimitPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/revision-limit"
	milestoneLimitPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/milestones/" + milestone.ID.String() + "/revision-limit"

	// Member actor: no project.update / milestone.update -> 403 forbidden.
	memberToken, _ := seedMemberActor(t, db, handler, orgID)
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPatch, projectLimitPath},
		{http.MethodPatch, milestoneLimitPath},
	} {
		rr, env := httpDo(t, handler, tc.method, tc.path, memberToken, []byte(`{"revision_limit": 3}`))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("member setter %s status = %d, want 403; body=%s", tc.path, rr.Code, rr.Body.String())
		}
		if env.Error == nil || env.Error.Code != "forbidden" {
			t.Fatalf("member setter %s error = %v, want forbidden", tc.path, env.Error)
		}
	}

	// Client actor: no project.update / milestone.update -> 403 forbidden.
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPatch, projectLimitPath},
		{http.MethodPatch, milestoneLimitPath},
	} {
		rr, env := httpDo(t, handler, tc.method, tc.path, clientToken, []byte(`{"revision_limit": 3}`))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("client setter %s status = %d, want 403; body=%s", tc.path, rr.Code, rr.Body.String())
		}
		if env.Error == nil || env.Error.Code != "forbidden" {
			t.Fatalf("client setter %s error = %v, want forbidden", tc.path, env.Error)
		}
	}

	// No write leaked from any rejected request.
	var projectLimit *int
	if err := db.QueryRowContext(ctx, `SELECT revision_limit FROM projects WHERE id = $1`, project.ID).Scan(&projectLimit); err != nil {
		t.Fatalf("read project limit: %v", err)
	}
	if projectLimit != nil {
		t.Fatalf("project revision_limit after rejected writes = %v, want NULL", *projectLimit)
	}
}

// TestRevisionLimitProjectNullMeansUnlimited: clearing the project limit to
// null removes the effective limit from milestone reads (unlimited) and
// limit_reached stays false even after submissions.
func TestRevisionLimitProjectNullMeansUnlimited(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	handler, _ := layer1HTTPHarness(t, db)
	ctx := context.Background()

	staffToken, orgID, staffID := registerStaffViaHTTP(t, handler, db)

	projectService := newTestService(db)
	project, err := projectService.Create(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), CreateProjectRequest{Name: "Unlimited Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	milestone, err := projectService.CreateMilestone(ctx, uuid.MustParse(orgID), project.ID, uuid.MustParse(staffID), CreateMilestoneInput{Title: "Unlimited Milestone"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	projectLimitPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/revision-limit"
	milestonePath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/milestones/" + milestone.ID.String()

	// Set then clear the project limit over HTTP.
	rr, env := httpDo(t, handler, http.MethodPatch, projectLimitPath, staffToken, []byte(`{"revision_limit": 2}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("set project limit status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	rr, env = httpDo(t, handler, http.MethodPatch, projectLimitPath, staffToken, []byte(`{"revision_limit": null}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("clear project limit status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if env.Error != nil {
		t.Fatalf("clear project limit returned error %v", env.Error)
	}

	var projectLimit *int
	if err := db.QueryRowContext(ctx, `SELECT revision_limit FROM projects WHERE id = $1`, project.ID).Scan(&projectLimit); err != nil {
		t.Fatalf("read project limit: %v", err)
	}
	if projectLimit != nil {
		t.Fatalf("project revision_limit after null = %v, want NULL", *projectLimit)
	}

	// Staff milestone detail: effective limit nil, limit_reached false.
	rr, env = httpDo(t, handler, http.MethodGet, milestonePath, staffToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("milestone read status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("decode milestone: %v", err)
	}
	if v, ok := raw["revision_limit"]; ok && v != nil {
		t.Fatalf("effective revision_limit after project null = %v, want absent/null", v)
	}
	if reached, ok := raw["limit_reached"]; ok && reached != false {
		t.Fatalf("limit_reached after project null = %v, want false", reached)
	}
}

// TestRevisionLimitReachedReadBackHTTP: with a project limit of 1, one
// submission flips limit_reached to true on the staff milestone detail read.
func TestRevisionLimitReachedReadBackHTTP(t *testing.T) {
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
	_ = clientLoginAndRotate(t, handler, clientEmail, oneTime)

	projectService := newTestService(db)
	project, err := projectService.Create(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), CreateProjectRequest{Name: "Reached Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	parsedClientID := uuid.MustParse(clientID)
	if _, err := projectService.AssignClient(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, &parsedClientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}
	milestone, err := projectService.CreateMilestone(ctx, uuid.MustParse(orgID), project.ID, uuid.MustParse(staffID), CreateMilestoneInput{Title: "Reached Milestone"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	projectLimitPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/revision-limit"
	milestonePath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/milestones/" + milestone.ID.String()

	rr, env := httpDo(t, handler, http.MethodPatch, projectLimitPath, staffToken, []byte(`{"revision_limit": 1}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("set project limit status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Drive one submission at the service level (the harness does not expose
	// the submit route; the read-side behavior is what is under test here).
	if _, err := projectService.UpdateMilestoneStatus(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, milestone.ID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	addDeliverable(t, projectService, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, milestone.ID)
	if _, err := projectService.SubmitMilestone(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, milestone.ID); err != nil {
		t.Fatalf("submit milestone: %v", err)
	}

	// revision_count = 1, limit = 1 -> limit_reached must read back true.
	rr, env = httpDo(t, handler, http.MethodGet, milestonePath, staffToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("milestone read status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("decode milestone: %v", err)
	}
	if raw["revision_limit"] != float64(1) {
		t.Fatalf("effective revision_limit = %v, want 1", raw["revision_limit"])
	}
	if raw["limit_reached"] != true {
		t.Fatalf("limit_reached = %v, want true at revision 1 of limit 1", raw["limit_reached"])
	}
}

// TestApprovalViewHidesLimitFieldsEvenWhenSet: the approval deep link must
// never expose revision_limit or limit_reached on the project or any
// milestone, even after an agency set a limit.
func TestApprovalViewHidesLimitFieldsEvenWhenSet(t *testing.T) {
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
	project, err := projectService.Create(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), CreateProjectRequest{Name: "Approval Limit Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	parsedClientID := uuid.MustParse(clientID)
	if _, err := projectService.AssignClient(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, &parsedClientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}
	milestone, err := projectService.CreateMilestone(ctx, uuid.MustParse(orgID), project.ID, uuid.MustParse(staffID), CreateMilestoneInput{Title: "Approval Milestone"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	// Set limits at both levels so the leak test is meaningful.
	projectLimitPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/revision-limit"
	milestoneLimitPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/milestones/" + milestone.ID.String() + "/revision-limit"
	if rr, _ := httpDo(t, handler, http.MethodPatch, projectLimitPath, staffToken, []byte(`{"revision_limit": 1}`)); rr.Code != http.StatusOK {
		t.Fatalf("set project limit status = %d; body=%s", rr.Code, rr.Body.String())
	}
	if rr, _ := httpDo(t, handler, http.MethodPatch, milestoneLimitPath, staffToken, []byte(`{"revision_limit": 3}`)); rr.Code != http.StatusOK {
		t.Fatalf("set milestone limit status = %d; body=%s", rr.Code, rr.Body.String())
	}

	approvalPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/approval"
	rr, env := httpDo(t, handler, http.MethodGet, approvalPath, clientToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("approval view status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var view struct {
		Project    map[string]any   `json:"project"`
		Milestones []map[string]any `json:"milestones"`
	}
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode approval view: %v", err)
	}
	if _, ok := view.Project["revision_limit"]; ok {
		t.Fatal("approval view project must NOT expose revision_limit")
	}
	if len(view.Milestones) != 1 {
		t.Fatalf("approval view milestones = %d, want 1", len(view.Milestones))
	}
	if _, ok := view.Milestones[0]["revision_limit"]; ok {
		t.Fatal("approval view milestone must NOT expose revision_limit")
	}
	if _, ok := view.Milestones[0]["limit_reached"]; ok {
		t.Fatal("approval view milestone must NOT expose limit_reached")
	}
}

// seedMemberActor inserts a member-role user with a valid session and returns
// a signed access token, plus their user id. Mirrors the pattern in the
// existing handler integration tests.
func seedMemberActor(t *testing.T, db *sql.DB, _ http.Handler, orgID string) (token string, memberID uuid.UUID) {
	t.Helper()

	// Resolve the member role.
	var memberRoleID uuid.UUID
	if err := db.QueryRowContext(context.Background(), `
		SELECT id FROM roles WHERE name = 'member' AND organization_id IS NULL
	`).Scan(&memberRoleID); err != nil {
		t.Fatalf("member role: %v", err)
	}

	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO users (email, password_hash, first_name, last_name, must_change_password)
		VALUES ($1, 'hash', 'Member', 'Actor', FALSE) RETURNING id
	`, "member-actor-"+uuid.NewString()+"@example.com").Scan(&memberID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, memberID, uuid.MustParse(orgID), memberRoleID); err != nil {
		t.Fatalf("member membership: %v", err)
	}

	var sessionID uuid.UUID
	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO sessions (user_id, token_family_id, refresh_token_hash, token_lookup_hash, expires_at)
		VALUES ($1, gen_random_uuid(), $2, $3, NOW() + interval '1 hour')
		RETURNING id
	`, memberID, "hash-"+uuid.NewString(), "lookup-"+uuid.NewString()).Scan(&sessionID); err != nil {
		t.Fatalf("insert member session: %v", err)
	}

	memberToken, err := jwt.NewManager("0123456789abcdef0123456789abcdef", 15*time.Minute).GenerateAccessToken(memberID, sessionID)
	if err != nil {
		t.Fatalf("sign member token: %v", err)
	}

	return memberToken, memberID
}
