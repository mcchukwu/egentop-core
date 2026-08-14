package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/assignment"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/auth"
	"github.com/mcchukwu/egentop/internal/client"
	"github.com/mcchukwu/egentop/internal/jwt"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/organization"
	"github.com/mcchukwu/egentop/internal/project"
	"github.com/mcchukwu/egentop/internal/user"
	"github.com/mcchukwu/egentop/internal/validation"
	"github.com/mcchukwu/egentop/pkg/config"
)

func routesIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("EGTEST_DB_URL")
	if dsn == "" {
		t.Skip("EGTEST_DB_URL not set; skipping integration test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	return db
}

const routesTestSecret = "0123456789abcdef0123456789abcdef"

// testRouteDeps builds the full handler/service/middleware stack exactly the
// way main() does, against the test database.
func testRouteDeps(t *testing.T, db *sql.DB) routeDeps {
	t.Helper()

	cfg := testConfig()

	auditService := audit.NewService(db)
	activityService := activity.NewService(activity.NewRepository(db))
	jwtManager := jwt.NewManager(routesTestSecret, 15*time.Minute)

	return routeDeps{
		db:               db,
		authMiddleware:   middleware.NewAuthMiddleware(db, []byte(routesTestSecret)),
		orgMiddleware:    middleware.NewOrgMiddleware(db),
		accessMiddleware: middleware.NewOrgAccessMiddleware(db),
		rbacMiddleware:   middleware.NewRBACMiddleware(db),
		loginLimiter:     func(h http.Handler) http.Handler { return h },
		registerLimiter:  func(h http.Handler) http.Handler { return h },
		refreshLimiter:   func(h http.Handler) http.Handler { return h },
		passwordLimiter:  func(h http.Handler) http.Handler { return h },
		h: handlers{
			auth:       auth.NewHandler(auth.NewService(db, auditService, jwtManager, cfg), cfg),
			user:       user.NewHandler(user.NewService(db, user.NewRepository(db), auditService, cfg)),
			org:        organization.NewHandler(organization.NewService(db, auditService)),
			membership: membership.NewHandler(membership.NewService(db, auditService)),
			client:     client.NewHandler(client.NewService(db, client.NewRepository(db), auditService, activityService)),
			project:    project.NewHandler(newProjectService(db, auditService, activityService)),
			assignment: assignment.NewHandler(newAssignmentService(db, auditService, activityService)),
			activity:   activity.NewHandler(activityService),
		},
	}
}

func testConfig() *config.Config {
	return &config.Config{
		AppEnv:             "development",
		AppPort:            "8080",
		JWTSecret:          routesTestSecret,
		JWTAccessTokenTTL:  15 * time.Minute,
		JWTRefreshTokenTTL: 720 * time.Hour,
		CORSAllowedOrigins: []string{"http://localhost:3000"},
	}
}

func newProjectService(db *sql.DB, auditService *audit.Service, activityService *activity.Service) *project.Service {
	return project.NewService(db, project.NewRepository(db), auditService, activityService)
}

func newAssignmentService(db *sql.DB, auditService *audit.Service, activityService *activity.Service) *assignment.Service {
	return assignment.NewService(db, assignment.NewRepository(db), newProjectService(db, auditService, activityService), auditService, activityService)
}

// seedMustChangeUser creates a user with must_change_password=true and a valid
// active session, returning the signed access token.
func seedMustChangeUser(t *testing.T, db *sql.DB) string {
	t.Helper()

	var userID, sessionID uuid.UUID
	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO users (email, password_hash, first_name, last_name, must_change_password)
		VALUES ($1, 'hash', 'Gate', 'User', TRUE)
		RETURNING id
	`, "gate-"+uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert gate user: %v", err)
	}

	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO sessions (user_id, token_family_id, refresh_token_hash, token_lookup_hash, expires_at)
		VALUES ($1, gen_random_uuid(), $2, $3, NOW() + interval '1 hour')
		RETURNING id
	`, userID, "hash-"+uuid.NewString(), "lookup-"+uuid.NewString()).Scan(&sessionID); err != nil {
		t.Fatalf("insert gate session: %v", err)
	}

	token, err := jwt.NewManager(routesTestSecret, 15*time.Minute).GenerateAccessToken(userID, sessionID)
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}

	return token
}

// seedAuthedUser creates a normal user (must_change_password=false) with a
// valid active session, returning the signed access token. Used to reach
// handlers past the auth + password gate.
func seedAuthedUser(t *testing.T, db *sql.DB) string {
	t.Helper()

	var userID, sessionID uuid.UUID
	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO users (email, password_hash, first_name, last_name, must_change_password)
		VALUES ($1, 'hash', 'Authed', 'User', FALSE)
		RETURNING id
	`, "authed-"+uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert authed user: %v", err)
	}

	if err := db.QueryRowContext(context.Background(), `
		INSERT INTO sessions (user_id, token_family_id, refresh_token_hash, token_lookup_hash, expires_at)
		VALUES ($1, gen_random_uuid(), $2, $3, NOW() + interval '1 hour')
		RETURNING id
	`, userID, "hash-"+uuid.NewString(), "lookup-"+uuid.NewString()).Scan(&sessionID); err != nil {
		t.Fatalf("insert authed session: %v", err)
	}

	token, err := jwt.NewManager(routesTestSecret, 15*time.Minute).GenerateAccessToken(userID, sessionID)
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}

	return token
}

// concretePath substitutes placeholder UUIDs into a route pattern so it can be
// served. The password gate fires before any path parsing, so placeholder
// values are sufficient for gated routes.
func concretePath(pattern string) string {
	zero := uuid.Nil.String()
	for _, key := range []string{"orgID", "projectID", "milestoneID", "userID", "assignmentID", "deliverableID"} {
		pattern = strings.ReplaceAll(pattern, "{"+key+"}", zero)
	}
	return pattern
}

// TestPasswordGateCoversEveryProtectedRoute walks the actual route table and
// proves the gate-coverage invariant: every RequireAuth-wrapped route except
// POST /v1/me/password returns 403 password_change_required for a
// must-change user; the sole exception reaches its handler.
func TestPasswordGateCoversEveryProtectedRoute(t *testing.T) {
	validation.Init()
	db := routesIntegrationDB(t)
	defer db.Close()

	deps := testRouteDeps(t, db)
	mux := http.NewServeMux()
	registerRoutes(mux, deps)

	token := seedMustChangeUser(t, db)

	for _, route := range protectedRoutes(deps) {
		path := concretePath(route.pattern)
		body := []byte(nil)
		if route.method == http.MethodPost && route.pattern == "/v1/me/password" {
			body = []byte(`{}`)
		}

		req := httptest.NewRequest(route.method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)

		if route.gated {
			if rr.Code != http.StatusForbidden {
				t.Errorf("gated route %s %s returned %d, want 403 (gate missing)", route.method, route.pattern, rr.Code)
				continue
			}
			var env struct {
				Error *struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil || env.Error == nil || env.Error.Code != "password_change_required" {
				t.Errorf("gated route %s %s returned 403 with wrong body %s, want code password_change_required", route.method, route.pattern, rr.Body.String())
			}
			continue
		}

		if rr.Code == http.StatusForbidden {
			t.Errorf("ungated route %s %s returned 403, want the handler to run (gate must not apply)", route.method, route.pattern)
		}
	}

	// Sanity: the table contains both gated and the single ungated exception.
	var gated, ungated int
	for _, route := range protectedRoutes(deps) {
		if route.gated {
			gated++
		} else {
			ungated++
		}
	}
	if gated < 10 {
		t.Fatalf("expected a substantial gated route table, got %d gated routes", gated)
	}
	if ungated != 1 {
		t.Fatalf("expected exactly one ungated protected route (POST /v1/me/password), got %d", ungated)
	}
}

// TestMalformedJSONBodyReturns400: a malformed request body must map to
// 400 invalid_request_body (not a 500) — regression for the missing
// ErrInvalidRequestBody mapper case.
func TestMalformedJSONBodyReturns400(t *testing.T) {
	validation.Init()
	db := routesIntegrationDB(t)
	defer db.Close()

	deps := testRouteDeps(t, db)
	mux := http.NewServeMux()
	registerRoutes(mux, deps)

	token := seedAuthedUser(t, db)

	req := httptest.NewRequest(http.MethodPost, "/v1/orgs", strings.NewReader(`{"name": `))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil || env.Error == nil || env.Error.Code != "invalid_request_body" {
		t.Fatalf("malformed body error = %s, want code invalid_request_body", rr.Body.String())
	}
}

// TestInvalidOrgIDReturns400: a non-UUID orgID in the path must map to
// 400 invalid_organization_id (not a 500) — regression for the missing
// ErrOrganizationIDInvalid mapper case.
func TestInvalidOrgIDReturns400(t *testing.T) {
	validation.Init()
	db := routesIntegrationDB(t)
	defer db.Close()

	deps := testRouteDeps(t, db)
	mux := http.NewServeMux()
	registerRoutes(mux, deps)

	token := seedAuthedUser(t, db)

	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/not-a-uuid/members", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid orgID status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil || env.Error == nil || env.Error.Code != "invalid_organization_id" {
		t.Fatalf("invalid orgID error = %s, want code invalid_organization_id", rr.Body.String())
	}
}
