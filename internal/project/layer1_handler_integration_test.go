package project

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/auth"
	"github.com/mcchukwu/egentop/internal/client"
	"github.com/mcchukwu/egentop/internal/jwt"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/user"
	"github.com/mcchukwu/egentop/internal/validation"
	"github.com/mcchukwu/egentop/pkg/config"
)

// layer1HTTPHarness mirrors the production route wiring for the routes the
// end-to-end tests exercise: register/login (public), password change
// (RequireAuth, no gate), /v1/me (RequireAuth + gate), client provision,
// reset-credential, approval view, milestone detail, changes-requested, and
// the membership guard routes.
func layer1HTTPHarness(t *testing.T, db *sql.DB) (http.Handler, *config.Config) {
	t.Helper()

	cfg := &config.Config{
		AppEnv:             "development",
		JWTSecret:          "0123456789abcdef0123456789abcdef",
		JWTAccessTokenTTL:  15 * time.Minute,
		JWTRefreshTokenTTL: 720 * time.Hour,
		CORSAllowedOrigins: []string{"http://localhost:3000"},
	}

	authMiddleware := middleware.NewAuthMiddleware(db, []byte(cfg.JWTSecret))
	orgMiddleware := middleware.NewOrgMiddleware(db)
	accessMiddleware := middleware.NewOrgAccessMiddleware(db)
	rbacMiddleware := middleware.NewRBACMiddleware(db)
	gate := middleware.RequirePasswordChanged

	auditService := audit.NewService(db)
	activityService := activity.NewService(activity.NewRepository(db))
	jwtManager := jwt.NewManager(cfg.JWTSecret, cfg.JWTAccessTokenTTL)

	authService := auth.NewService(db, auditService, jwtManager, cfg)
	authHandler := auth.NewHandler(authService, cfg)

	userService := user.NewService(db, user.NewRepository(db), auditService, cfg)
	userHandler := user.NewHandler(userService)

	clientService := client.NewService(db, client.NewRepository(db), auditService, activityService)
	clientHandler := client.NewHandler(clientService)

	membershipService := membership.NewService(db, auditService)
	membershipHandler := membership.NewHandler(membershipService)

	projectService := newTestService(db)
	projectHandler := NewHandler(projectService)

	mux := http.NewServeMux()

	orgScoped := func(permission string, next http.Handler) http.Handler {
		return authMiddleware.RequireAuth(gate(orgMiddleware.LoadOrg(accessMiddleware.RequireMembership(rbacMiddleware.RequirePermission(permission)(next)))))
	}

	mux.Handle("POST /v1/auth/register", http.HandlerFunc(authHandler.Register))
	mux.Handle("POST /v1/auth/login", http.HandlerFunc(authHandler.Login))
	mux.Handle("POST /v1/me/password", authMiddleware.RequireAuth(http.HandlerFunc(userHandler.ChangePassword)))
	mux.Handle("GET /v1/me", authMiddleware.RequireAuth(gate(http.HandlerFunc(userHandler.Me))))
	mux.Handle("POST /v1/orgs/{orgID}/clients", orgScoped("client.provision", http.HandlerFunc(clientHandler.Provision)))
	mux.Handle("POST /v1/orgs/{orgID}/clients/{userID}/reset-credential", orgScoped("client.provision", http.HandlerFunc(clientHandler.ResetCredential)))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/approval", orgScoped("milestone.view", http.HandlerFunc(projectHandler.GetApprovalView)))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}", orgScoped("milestone.view", http.HandlerFunc(projectHandler.GetMilestoneByID)))
	mux.Handle("POST /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/changes-requested", orgScoped("milestone.revision.request", http.HandlerFunc(projectHandler.RequestMilestoneChanges)))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}/revision-limit", orgScoped("project.update", http.HandlerFunc(projectHandler.UpdateProjectRevisionLimit)))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/revision-limit", orgScoped("milestone.update", http.HandlerFunc(projectHandler.UpdateMilestoneRevisionLimit)))
	mux.Handle("PATCH /v1/orgs/{orgID}/members/{userID}", orgScoped("member.role.update", http.HandlerFunc(membershipHandler.UpdateOrgMemberRole)))
	mux.Handle("DELETE /v1/orgs/{orgID}/members/{userID}", orgScoped("member.remove", http.HandlerFunc(membershipHandler.RemoveOrgMember)))

	return mux, cfg
}

type apiEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func httpDo(t *testing.T, handler http.Handler, method, path, accessToken string, body []byte) (*httptest.ResponseRecorder, apiEnvelope) {
	t.Helper()

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var env apiEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil && len(rr.Body.Bytes()) > 0 {
		t.Fatalf("decode envelope: %v; body=%s", err, rr.Body.String())
	}
	return rr, env
}

// TestEndToEndProvisionForcedChangeClientAccess is the full wedge flow over
// HTTP: staff provisions a client -> client logs in with the one-time
// credential -> every gated route is 403 password_change_required -> password
// change lifts the gate -> the approval deep link returns 200.
func TestEndToEndProvisionForcedChangeClientAccess(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	handler, _ := layer1HTTPHarness(t, db)
	ctx := context.Background()

	// --- Staff registration (via service: creates org + owner membership) ---
	cfg := &config.Config{
		JWTSecret:          "0123456789abcdef0123456789abcdef",
		JWTAccessTokenTTL:  15 * time.Minute,
		JWTRefreshTokenTTL: 720 * time.Hour,
	}
	auditService := audit.NewService(db)
	jwtManager := jwt.NewManager(cfg.JWTSecret, cfg.JWTAccessTokenTTL)
	authService := auth.NewService(db, auditService, jwtManager, cfg)

	staffEmail := "staff-" + uuid.NewString() + "@example.com"
	staffPassword := "staff-password-123"
	staffAccess, _, err := authService.Register(ctx, auth.RegisterRequest{
		Email:     staffEmail,
		Password:  staffPassword,
		FirstName: "Staff",
		LastName:  "Owner",
	})
	if err != nil {
		t.Fatalf("register staff: %v", err)
	}

	// Resolve the staff's default org.
	var orgID, staffID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT o.id, m.user_id
		FROM organizations o
		JOIN memberships m ON m.organization_id = o.id
		WHERE m.user_id = (SELECT id FROM users WHERE email = $1)
		AND o.name = $2
	`, staffEmail, "Staff's Organization").Scan(&orgID, &staffID); err != nil {
		t.Fatalf("resolve staff org: %v", err)
	}

	// --- Staff provisions a client over HTTP ---
	provisionBody := map[string]string{
		"email":      "client-" + uuid.NewString() + "@example.com",
		"first_name": "Client",
		"last_name":  "Person",
	}
	rawBody, _ := json.Marshal(provisionBody)
	rr, env := httpDo(t, handler, http.MethodPost, "/v1/orgs/"+orgID.String()+"/clients", staffAccess, rawBody)
	if rr.Code != http.StatusCreated {
		t.Fatalf("provision status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var provisioned struct {
		ClientID         uuid.UUID `json:"client_id"`
		CredentialIssued bool      `json:"credential_issued"`
		OneTimePassword  string    `json:"one_time_password"`
	}
	if err := json.Unmarshal(env.Data, &provisioned); err != nil {
		t.Fatalf("decode provision data: %v", err)
	}
	if !provisioned.CredentialIssued || provisioned.OneTimePassword == "" {
		t.Fatalf("expected issued credential, got %+v", provisioned)
	}

	// --- Client logs in with the one-time credential ---
	loginBody, _ := json.Marshal(map[string]string{"identifier": provisionBody["email"], "password": provisioned.OneTimePassword})
	rr, env = httpDo(t, handler, http.MethodPost, "/v1/auth/login", "", loginBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("client login status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var loginData struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(env.Data, &loginData); err != nil {
		t.Fatalf("decode login data: %v", err)
	}

	// --- The gate blocks every authenticated route except password change ---
	rr, env = httpDo(t, handler, http.MethodGet, "/v1/me", loginData.AccessToken, nil)
	if rr.Code != http.StatusForbidden || env.Error == nil || env.Error.Code != "password_change_required" {
		t.Fatalf("gated /v1/me = %d (%v), want 403 password_change_required", rr.Code, env.Error)
	}

	rr, env = httpDo(t, handler, http.MethodGet, "/v1/orgs/"+orgID.String()+"/projects/"+uuid.NewString()+"/approval", loginData.AccessToken, nil)
	if rr.Code != http.StatusForbidden || env.Error == nil || env.Error.Code != "password_change_required" {
		t.Fatalf("gated approval view = %d (%v), want 403 password_change_required", rr.Code, env.Error)
	}

	// --- Password change lifts the gate ---
	pwBody, _ := json.Marshal(map[string]string{
		"current_password": provisioned.OneTimePassword,
		"new_password":     "rotated-password-456",
	})
	rr, _ = httpDo(t, handler, http.MethodPost, "/v1/me/password", loginData.AccessToken, pwBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("password change status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// --- Set up a project + milestone + deliverable + submission so the
	// approval view has real content, then re-login with the new password. ---
	projectService := newTestService(db)
	project, err := projectService.Create(ctx, staffID, orgID, CreateProjectRequest{Name: "Client Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := projectService.AssignClient(ctx, staffID, orgID, project.ID, &provisioned.ClientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}
	milestone, err := projectService.CreateMilestone(ctx, orgID, project.ID, staffID, CreateMilestoneInput{Title: "Phase One"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	if _, err := projectService.UpdateMilestoneStatus(ctx, staffID, orgID, project.ID, milestone.ID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	if _, err := projectService.CreateDeliverable(ctx, staffID, orgID, project.ID, milestone.ID, "https://figma.com/file/abc", nil, nil); err != nil {
		t.Fatalf("create deliverable: %v", err)
	}
	if _, err := projectService.SubmitMilestone(ctx, staffID, orgID, project.ID, milestone.ID); err != nil {
		t.Fatalf("submit milestone: %v", err)
	}

	loginBody, _ = json.Marshal(map[string]string{"identifier": provisionBody["email"], "password": "rotated-password-456"})
	rr, env = httpDo(t, handler, http.MethodPost, "/v1/auth/login", "", loginBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("re-login status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(env.Data, &loginData); err != nil {
		t.Fatalf("decode re-login: %v", err)
	}

	// The gate is gone and the approval deep link resolves.
	rr, env = httpDo(t, handler, http.MethodGet, "/v1/orgs/"+orgID.String()+"/projects/"+project.ID.String()+"/approval", loginData.AccessToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("approval view status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var view ApprovalView
	if err := json.Unmarshal(env.Data, &view); err != nil {
		t.Fatalf("decode approval view: %v", err)
	}
	if view.Project.ID != project.ID || len(view.Milestones) != 1 {
		t.Fatalf("approval view = project %v with %d milestones, want project %v with 1", view.Project.ID, len(view.Milestones), project.ID)
	}
}

// phoneCounter guarantees distinct phone numbers across a test run (the
// users.phone column is UNIQUE).
var phoneCounter atomic.Int64

// newNGPhone returns a valid Nigerian phone number (regex: +234[7-9]\d{9}).
func newNGPhone() string {
	n := phoneCounter.Add(1)
	return fmt.Sprintf("+2348%08d%01d", time.Now().UnixNano()%100000000, n%10)
}

// registerStaffViaHTTP registers a staff user through the register route and
// resolves their auto-created organization id from the database.
func registerStaffViaHTTP(t *testing.T, handler http.Handler, db *sql.DB) (accessToken, orgID, staffID string) {
	t.Helper()

	email := "http-staff-" + uuid.NewString() + "@example.com"
	body, _ := json.Marshal(map[string]string{
		"email":      email,
		"password":   "staff-password-123",
		"first_name": "HTTP",
		"last_name":  "Staff",
	})
	rr, env := httpDo(t, handler, http.MethodPost, "/v1/auth/register", "", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var data struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode register data: %v", err)
	}

	if err := db.QueryRowContext(t.Context(), `
		SELECT o.id, m.user_id
		FROM organizations o
		JOIN memberships m ON m.organization_id = o.id
		WHERE m.user_id = (SELECT id FROM users WHERE email = $1)
	`, email).Scan(&orgID, &staffID); err != nil {
		t.Fatalf("resolve staff org: %v", err)
	}

	return data.AccessToken, orgID, staffID
}

// provisionClientViaHTTP provisions a client and returns id + one-time password.
func provisionClientViaHTTP(t *testing.T, handler http.Handler, staffToken, orgID string) (clientID, oneTime string) {
	t.Helper()

	body, _ := json.Marshal(map[string]string{
		"email":      "http-client-" + uuid.NewString() + "@example.com",
		"first_name": "HTTP",
		"last_name":  "Client",
	})
	rr, env := httpDo(t, handler, http.MethodPost, "/v1/orgs/"+orgID+"/clients", staffToken, body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("provision status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var data struct {
		ClientID         uuid.UUID `json:"client_id"`
		CredentialIssued bool      `json:"credential_issued"`
		OneTimePassword  string    `json:"one_time_password"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode provision data: %v", err)
	}
	if !data.CredentialIssued || data.OneTimePassword == "" {
		t.Fatalf("expected issued credential, got %+v", data)
	}
	return data.ClientID.String(), data.OneTimePassword
}

// clientLoginAndRotate logs a client in with their one-time credential and
// rotates the password so the gate lifts; returns a usable access token.
func clientLoginAndRotate(t *testing.T, handler http.Handler, email, oneTime string) string {
	t.Helper()

	loginBody, _ := json.Marshal(map[string]string{"identifier": email, "password": oneTime})
	rr, env := httpDo(t, handler, http.MethodPost, "/v1/auth/login", "", loginBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("client login status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var data struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("decode login data: %v", err)
	}

	pwBody, _ := json.Marshal(map[string]string{
		"current_password": oneTime,
		"new_password":     "rotated-http-456",
	})
	rr, _ = httpDo(t, handler, http.MethodPost, "/v1/me/password", data.AccessToken, pwBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("password change status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	return data.AccessToken
}

// TestPhoneOnlyProvisionAndRegister: the phone is a primary identity channel;
// phone-only registration (200) and phone-only provisioning (201) must work at
// the HTTP boundary.
func TestPhoneOnlyProvisionAndRegister(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	handler, _ := layer1HTTPHarness(t, db)

	// Phone-only registration succeeds (the register DTO must not require email).
	phone := newNGPhone()
	body, _ := json.Marshal(map[string]string{
		"phone":      phone,
		"password":   "phone-password-123",
		"first_name": "Phone",
		"last_name":  "Registrant",
	})
	rr, env := httpDo(t, handler, http.MethodPost, "/v1/auth/register", "", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("phone-only register status = %d, want 200; body=%s (code=%v)", rr.Code, rr.Body.String(), env.Error)
	}
	var data struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.AccessToken == "" {
		t.Fatalf("phone-only register returned no access token: %v", err)
	}

	// Phone-only provisioning succeeds (the provision DTO must not require email).
	staffToken, orgID, _ := registerStaffViaHTTP(t, handler, db)
	provisionBody, _ := json.Marshal(map[string]string{
		"phone":      newNGPhone(),
		"first_name": "Phone",
		"last_name":  "Client",
	})
	rr, env = httpDo(t, handler, http.MethodPost, "/v1/orgs/"+orgID+"/clients", staffToken, provisionBody)
	if rr.Code != http.StatusCreated {
		t.Fatalf("phone-only provision status = %d, want 201; body=%s (code=%v)", rr.Code, rr.Body.String(), env.Error)
	}
	var provisioned struct {
		ClientID         uuid.UUID `json:"client_id"`
		CredentialIssued bool      `json:"credential_issued"`
		OneTimePassword  string    `json:"one_time_password"`
	}
	if err := json.Unmarshal(env.Data, &provisioned); err != nil {
		t.Fatalf("decode phone provision: %v", err)
	}
	if !provisioned.CredentialIssued || provisioned.OneTimePassword == "" {
		t.Fatalf("phone-only provision should issue a credential, got %+v", provisioned)
	}
}

// TestClientMilestoneDetailHidesLimitFields: the milestone detail response for
// a client actor must not expose limit_reached/revision_limit, while staff see
// them.
func TestClientMilestoneDetailHidesLimitFields(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	handler, _ := layer1HTTPHarness(t, db)
	ctx := context.Background()

	staffToken, orgID, staffID := registerStaffViaHTTP(t, handler, db)
	clientID, oneTime := provisionClientViaHTTP(t, handler, staffToken, orgID)
	clientEmail := ""
	_ = db.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, clientID).Scan(&clientEmail)
	clientToken := clientLoginAndRotate(t, handler, clientEmail, oneTime)

	// Setup via service: project with a revision limit, milestone, client.
	projectService := newTestService(db)
	project, err := projectService.Create(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), CreateProjectRequest{Name: "Detail Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	setProjectRevisionLimit(t, db, project.ID, 3)
	parsedClientID := uuid.MustParse(clientID)
	if _, err := projectService.AssignClient(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, &parsedClientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}
	milestone, err := projectService.CreateMilestone(ctx, uuid.MustParse(orgID), project.ID, uuid.MustParse(staffID), CreateMilestoneInput{Title: "Detail Milestone"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	milestonePath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/milestones/" + milestone.ID.String()

	// Client detail: revision_count + payment_status present, limit fields absent.
	rr, env := httpDo(t, handler, http.MethodGet, milestonePath, clientToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("client milestone detail status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("decode client detail: %v", err)
	}
	if _, ok := raw["revision_count"]; !ok {
		t.Fatal("client detail must expose revision_count")
	}
	if _, ok := raw["payment_status"]; !ok {
		t.Fatal("client detail must expose payment_status")
	}
	if _, ok := raw["limit_reached"]; ok {
		t.Fatal("client detail must NOT expose limit_reached")
	}
	if _, ok := raw["revision_limit"]; ok {
		t.Fatal("client detail must NOT expose revision_limit")
	}

	// Staff detail: the limit fields are present.
	rr, env = httpDo(t, handler, http.MethodGet, milestonePath, staffToken, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("staff milestone detail status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatalf("decode staff detail: %v", err)
	}
	if _, ok := raw["limit_reached"]; !ok {
		t.Fatal("staff detail must expose limit_reached")
	}
	if _, ok := raw["revision_limit"]; !ok {
		t.Fatal("staff detail must expose revision_limit")
	}
}

// TestResetCredentialPermissionDeniedForMember: client.provision is
// owner/admin-only; a member actor gets 403.
func TestResetCredentialPermissionDeniedForMember(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	handler, _ := layer1HTTPHarness(t, db)
	ctx := context.Background()

	staffToken, orgID, _ := registerStaffViaHTTP(t, handler, db)
	clientID, _ := provisionClientViaHTTP(t, handler, staffToken, orgID)

	// A member-role user in the org with a valid session + access token.
	var memberID, memberRoleID, sessionID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'HTTP', 'Member') RETURNING id
	`, "http-member-"+uuid.NewString()+"@example.com").Scan(&memberID); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'member' AND organization_id IS NULL
	`).Scan(&memberRoleID); err != nil {
		t.Fatalf("member role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, memberID, uuid.MustParse(orgID), memberRoleID); err != nil {
		t.Fatalf("member membership: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
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

	rr, env := httpDo(t, handler, http.MethodPost, "/v1/orgs/"+orgID+"/clients/"+clientID+"/reset-credential", memberToken, nil)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("member reset-credential status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
	if env.Error == nil || env.Error.Code != "forbidden" {
		t.Fatalf("member reset-credential error = %v, want forbidden", env.Error)
	}
}

// TestHTTPMembershipAndStateGuards drives the client guard routes over HTTP:
// changes-requested on a stale milestone (409), member.role.update on a client
// (403), and member.remove on a client (409).
func TestHTTPMembershipAndStateGuards(t *testing.T) {
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

	// Setup via service: project assigned to the client, milestone submitted.
	projectService := newTestService(db)
	project, err := projectService.Create(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), CreateProjectRequest{Name: "Guard Project"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	parsedClientID := uuid.MustParse(clientID)
	if _, err := projectService.AssignClient(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, &parsedClientID); err != nil {
		t.Fatalf("assign client: %v", err)
	}
	milestone, err := projectService.CreateMilestone(ctx, uuid.MustParse(orgID), project.ID, uuid.MustParse(staffID), CreateMilestoneInput{Title: "Guard Milestone"})
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	if _, err := projectService.UpdateMilestoneStatus(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, milestone.ID, MilestoneStatusInProgress); err != nil {
		t.Fatalf("start milestone: %v", err)
	}
	if _, err := projectService.CreateDeliverable(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, milestone.ID, "https://example.com/guard", nil, nil); err != nil {
		t.Fatalf("create deliverable: %v", err)
	}
	if _, err := projectService.SubmitMilestone(ctx, uuid.MustParse(staffID), uuid.MustParse(orgID), project.ID, milestone.ID); err != nil {
		t.Fatalf("submit milestone: %v", err)
	}

	changesPath := "/v1/orgs/" + orgID + "/projects/" + project.ID.String() + "/milestones/" + milestone.ID.String() + "/changes-requested"
	notesBody, _ := json.Marshal(map[string]string{"notes": "please revise the layout"})

	// First changes-request succeeds.
	rr, env := httpDo(t, handler, http.MethodPost, changesPath, clientToken, notesBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("first changes-request status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Second changes-request hits the stale state: 409, not idempotent.
	rr, env = httpDo(t, handler, http.MethodPost, changesPath, clientToken, notesBody)
	if rr.Code != http.StatusConflict {
		t.Fatalf("second changes-request status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if env.Error == nil || env.Error.Code != "milestone_not_awaiting_approval" {
		t.Fatalf("second changes-request error = %v, want milestone_not_awaiting_approval", env.Error)
	}

	// member.role.update on a client membership is 403.
	roleBody, _ := json.Marshal(map[string]string{"role": "member"})
	rr, env = httpDo(t, handler, http.MethodPatch, "/v1/orgs/"+orgID+"/members/"+clientID, staffToken, roleBody)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("role.update on client status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
	if env.Error == nil || env.Error.Code != "forbidden" {
		t.Fatalf("role.update on client error = %v, want forbidden", env.Error)
	}

	// member.remove on a client membership is 409 client_attached_to_project.
	rr, env = httpDo(t, handler, http.MethodDelete, "/v1/orgs/"+orgID+"/members/"+clientID, staffToken, nil)
	if rr.Code != http.StatusConflict {
		t.Fatalf("member.remove on client status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if env.Error == nil || env.Error.Code != "client_attached_to_project" {
		t.Fatalf("member.remove on client error = %v, want client_attached_to_project", env.Error)
	}

	// The client membership still exists after the rejected removal.
	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, parsedClientID, uuid.MustParse(orgID)).Scan(&membershipCount); err != nil {
		t.Fatalf("count client memberships: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("client membership count = %d, want 1 (both guards rejected)", membershipCount)
	}
}
