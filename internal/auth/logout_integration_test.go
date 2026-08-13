package auth

// HTTP-level regression tests for POST /v1/auth/logout and
// POST /v1/auth/logout-all.
//
// Both routes authenticate via the refresh_token HttpOnly cookie ONLY — the
// access token is ignored (expired or absent). Both are idempotent: a missing
// cookie, unknown token, revoked session, or expired session yields 204 with a
// cleared cookie. There is no full-API test harness and package main cannot be
// imported, so this replicates the production logout wiring. The integration
// test DB must have migration 000004 (token_lookup_hash) applied before
// running with EGTEST_DB_URL set.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/jwt"
)

// newTestLogoutHandler builds the production logout handler (no auth
// middleware — the cookie is the sole credential).
func newTestLogoutHandler(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()

	return http.HandlerFunc(newTestAuthCore(t, db).Logout)
}

// newTestLogoutAllHandler builds the production logout-all handler (no auth
// middleware — the cookie is the sole credential).
func newTestLogoutAllHandler(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()

	return http.HandlerFunc(newTestAuthCore(t, db).LogoutAllDevices)
}

// doLogoutRequest performs a POST to path with the given cookie value and
// optional Bearer auth header.
func doLogoutRequest(t *testing.T, h http.Handler, path, cookieValue, authHeader string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, nil)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "refresh_token", Value: cookieValue})
	}
	if authHeader != "" {
		req.Header.Set("Authorization", "Bearer "+authHeader)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr
}

// assertClearCookie asserts the response clears the refresh_token cookie
// (empty value, negative MaxAge).
func assertClearCookie(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()

	for _, c := range rr.Result().Cookies() {
		if c.Name == "refresh_token" {
			if c.Value != "" {
				t.Fatalf("expected empty refresh_token cookie value, got %q", c.Value)
			}
			if c.MaxAge >= 0 {
				t.Fatalf("expected refresh_token cookie MaxAge < 0, got %d", c.MaxAge)
			}
			return
		}
	}

	t.Fatal("expected refresh_token clear cookie in response")
}

func TestLogoutHandlerSucceedsWithCookieOnly(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestLogoutHandler(t, db)

	email := "logout-h-succeeds-" + uuid.NewString() + "@example.com"
	_, refreshToken := register(t, svc, email)

	rr := doLogoutRequest(t, h, "/v1/auth/logout", refreshToken, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	assertClearCookie(t, rr)

	var revoked int
	err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions
		WHERE token_lookup_hash = $1 AND revoked = true
	`, lookupHashRefreshToken(refreshToken)).Scan(&revoked)
	if err != nil {
		t.Fatalf("count revoked sessions: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("expected the presented session revoked, got %d revoked rows", revoked)
	}
}

func TestLogoutHandlerIgnoresExpiredAccessToken(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestLogoutHandler(t, db)

	email := "logout-h-expired-access-" + uuid.NewString() + "@example.com"
	_, refreshToken := register(t, svc, email)

	var userID, sessionID uuid.UUID
	err := db.QueryRowContext(ctx, `
		SELECT s.user_id, s.id
		FROM sessions s
		WHERE s.user_id = (SELECT id FROM users WHERE email = $1)
		AND s.revoked = false
	`, email).Scan(&userID, &sessionID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}

	// a genuinely expired access token (negative TTL puts exp in the past).
	// The logout route must ignore the access token entirely and succeed from
	// the cookie alone.
	expiredManager := jwt.NewManager("test-secret-0123456789abcdef0123456789abcdef", -time.Minute)
	expiredAccess, err := expiredManager.GenerateAccessToken(userID, sessionID)
	if err != nil {
		t.Fatalf("generate expired access token: %v", err)
	}

	rr := doLogoutRequest(t, h, "/v1/auth/logout", refreshToken, expiredAccess)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	assertClearCookie(t, rr)

	var revoked bool
	err = db.QueryRowContext(ctx, `SELECT revoked FROM sessions WHERE id = $1`, sessionID).Scan(&revoked)
	if err != nil {
		t.Fatalf("check session: %v", err)
	}
	if !revoked {
		t.Fatal("expected presented session revoked after logout")
	}
}

func TestLogoutHandlerMissingCookieIdempotent(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	h := newTestLogoutHandler(t, db)

	rr := doLogoutRequest(t, h, "/v1/auth/logout", "", "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	assertClearCookie(t, rr)
}

func TestLogoutHandlerReplayCookieIdempotent(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestLogoutHandler(t, db)

	email := "logout-h-replay-" + uuid.NewString() + "@example.com"
	_, refreshToken := register(t, svc, email)

	// a second, unrelated session via login
	_, _, err := svc.Login(ctx, LoginRequest{Identifier: email, Password: "password123"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	var userID uuid.UUID
	err = db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	first := doLogoutRequest(t, h, "/v1/auth/logout", refreshToken, "")
	if first.Code != http.StatusNoContent {
		t.Fatalf("first logout status = %d, want 204; body=%s", first.Code, first.Body.String())
	}
	assertClearCookie(t, first)

	// replaying the same cookie is an idempotent no-op: still 204, and no
	// duplicate revocation or audit row.
	second := doLogoutRequest(t, h, "/v1/auth/logout", refreshToken, "")
	if second.Code != http.StatusNoContent {
		t.Fatalf("replay status = %d, want 204; body=%s", second.Code, second.Body.String())
	}
	assertClearCookie(t, second)

	var activeSessions int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&activeSessions)
	if err != nil {
		t.Fatalf("count active sessions: %v", err)
	}
	if activeSessions != 1 {
		t.Fatalf("expected 1 active session (the login), got %d", activeSessions)
	}

	var loggedOutAudits int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE user_id = $1 AND action = 'user.logged_out'
	`, userID).Scan(&loggedOutAudits)
	if err != nil {
		t.Fatalf("count logged_out audits: %v", err)
	}
	if loggedOutAudits != 1 {
		t.Fatalf("expected exactly 1 logged_out audit row, got %d", loggedOutAudits)
	}
}

func TestLogoutAllHandlerRevokesAllSessions(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestLogoutAllHandler(t, db)

	email := "logoutall-h-" + uuid.NewString() + "@example.com"
	_, refreshToken := register(t, svc, email)

	_, _, err := svc.Login(ctx, LoginRequest{Identifier: email, Password: "password123"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	var userID uuid.UUID
	err = db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	rr := doLogoutRequest(t, h, "/v1/auth/logout-all", refreshToken, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	assertClearCookie(t, rr)

	var activeSessions int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&activeSessions)
	if err != nil {
		t.Fatalf("count active sessions: %v", err)
	}
	if activeSessions != 0 {
		t.Fatalf("expected 0 active sessions, got %d", activeSessions)
	}

	var logoutAllAudits int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE user_id = $1 AND action = 'user.logged_out_all_devices'
	`, userID).Scan(&logoutAllAudits)
	if err != nil {
		t.Fatalf("count logout-all audits: %v", err)
	}
	if logoutAllAudits != 1 {
		t.Fatalf("expected exactly 1 logged_out_all_devices audit row, got %d", logoutAllAudits)
	}
}

func TestLogoutAllHandlerMissingCookieIdempotent(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	h := newTestLogoutAllHandler(t, db)

	rr := doLogoutRequest(t, h, "/v1/auth/logout-all", "", "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	assertClearCookie(t, rr)
}

func TestLogoutAllThenRefreshRejected(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	svc := newTestService(db)
	h := newTestLogoutAllHandler(t, db)

	email := "logoutall-h-then-refresh-" + uuid.NewString() + "@example.com"
	_, refreshToken := register(t, svc, email)

	rr := doLogoutRequest(t, h, "/v1/auth/logout-all", refreshToken, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("logout-all status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	assertClearCookie(t, rr)

	// the revoked cookie is now a theft signal on refresh: family revoked, 401
	refreshRR := doRefresh(t, newTestRefreshHandler(t, db), refreshToken, "", "203.0.113.1:12345")
	if refreshRR.Code != http.StatusUnauthorized {
		t.Fatalf("refresh status = %d, want 401; body=%s", refreshRR.Code, refreshRR.Body.String())
	}
	env := decodeRefreshEnvelope(t, refreshRR.Body.Bytes())
	if env.Error.Code != "session_revoked" {
		t.Fatalf("error code = %q, want session_revoked", env.Error.Code)
	}
}

func TestLogoutHandlerOnlyRevokesPresentedSession(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestLogoutHandler(t, db)

	email := "logout-h-single-" + uuid.NewString() + "@example.com"
	_, refresh1 := register(t, svc, email)

	// second session via login; its cookie must survive the logout of refresh1
	_, refresh2, err := svc.Login(ctx, LoginRequest{Identifier: email, Password: "password123"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	var userID uuid.UUID
	err = db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	rr := doLogoutRequest(t, h, "/v1/auth/logout", refresh1, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204; body=%s", rr.Code, rr.Body.String())
	}
	assertClearCookie(t, rr)

	var activeSessions int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&activeSessions)
	if err != nil {
		t.Fatalf("count active sessions: %v", err)
	}
	if activeSessions != 1 {
		t.Fatalf("expected 1 active session (the login), got %d", activeSessions)
	}

	// the other session still refreshes fine
	refreshRR := doRefresh(t, newTestRefreshHandler(t, db), refresh2, "", "203.0.113.2:12345")
	if refreshRR.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200; body=%s", refreshRR.Code, refreshRR.Body.String())
	}
	env := decodeRefreshEnvelope(t, refreshRR.Body.Bytes())
	if !env.Success {
		t.Fatalf("expected success envelope, got %+v", env)
	}
	if env.Data.AccessToken == "" {
		t.Fatalf("expected non-empty access_token")
	}
}
