package auth

// HTTP-level regression tests for POST /v1/auth/refresh.
//
// There is no full-API test harness and package main cannot be imported, so
// this replicates the production refresh route chain (rate limiter wrapping the
// auth handler). The integration test DB must have migration 000004
// (token_lookup_hash) applied before running with EGTEST_DB_URL set.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/jwt"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/pkg/config"
)

type refreshEnvelope struct {
	Success bool `json:"success"`
	Data    struct {
		AccessToken string `json:"access_token"`
	} `json:"data"`
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// newTestRefreshHandler builds the production chain for the refresh route:
// refresh rate limiter (10/min) wrapping the auth refresh handler.
func newTestRefreshHandler(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()

	cfg := &config.Config{AppEnv: "development", JWTRefreshTokenTTL: 30 * 24 * time.Hour}
	manager := jwt.NewManager("test-secret-0123456789abcdef0123456789abcdef", 15*time.Minute)
	svc := NewService(db, audit.NewService(db), manager, cfg)
	h := NewHandler(svc, cfg)
	limiter := middleware.NewRateLimiterMiddleware(10, time.Minute)

	return limiter.Limit(http.HandlerFunc(h.RefreshToken))
}

// doRefresh performs a POST /v1/auth/refresh with the given cookie value,
// optional Bearer auth header and fixed remote address.
func doRefresh(t *testing.T, h http.Handler, cookieValue, authHeader, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", nil)
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "refresh_token", Value: cookieValue})
	}
	if authHeader != "" {
		req.Header.Set("Authorization", "Bearer "+authHeader)
	}
	req.RemoteAddr = remoteAddr

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr
}

func decodeRefreshEnvelope(t *testing.T, body []byte) refreshEnvelope {
	t.Helper()

	var env refreshEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, string(body))
	}

	return env
}

func refreshCookieValue(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()

	for _, c := range rr.Result().Cookies() {
		if c.Name == "refresh_token" {
			return c.Value
		}
	}

	return ""
}

func TestRefreshHandlerSucceedsWithoutAccessToken(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestRefreshHandler(t, db)

	email := "refresh-h-succeeds-" + uuid.NewString() + "@example.com"
	_, refreshToken := register(t, svc, email)

	// no Authorization header at all — the cookie alone must suffice
	rr := doRefresh(t, h, refreshToken, "", "192.0.2.1:12345")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	env := decodeRefreshEnvelope(t, rr.Body.Bytes())
	if !env.Success {
		t.Fatalf("expected success envelope, got %+v", env)
	}
	if env.Data.AccessToken == "" {
		t.Fatalf("expected non-empty access_token")
	}

	newRefreshToken := refreshCookieValue(t, rr)
	if newRefreshToken == "" {
		t.Fatalf("expected refresh_token cookie in response")
	}
	if newRefreshToken == refreshToken {
		t.Fatalf("expected rotated refresh token to differ from the original")
	}

	var userID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	// the original session is revoked and exactly one active session remains
	var oldRevoked bool
	err = db.QueryRowContext(ctx, `
		SELECT revoked FROM sessions
		WHERE user_id = $1 AND revoked = true
		LIMIT 1
	`, userID).Scan(&oldRevoked)
	if err != nil {
		t.Fatalf("find revoked session: %v", err)
	}
	if !oldRevoked {
		t.Fatalf("expected the original session to be revoked")
	}

	var activeSessions int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&activeSessions)
	if err != nil {
		t.Fatalf("count active sessions: %v", err)
	}
	if activeSessions != 1 {
		t.Fatalf("expected exactly 1 active session, got %d", activeSessions)
	}
}

func TestRefreshHandlerIgnoresExpiredAccessToken(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestRefreshHandler(t, db)

	email := "refresh-h-expired-access-" + uuid.NewString() + "@example.com"
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
	// Previously RequireAuth rejected this with 401; the refresh route must
	// ignore the access token entirely and succeed from the cookie alone.
	expiredManager := jwt.NewManager("test-secret-0123456789abcdef0123456789abcdef", -time.Minute)
	expiredAccess, err := expiredManager.GenerateAccessToken(userID, sessionID)
	if err != nil {
		t.Fatalf("generate expired access token: %v", err)
	}

	rr := doRefresh(t, h, refreshToken, expiredAccess, "192.0.2.2:12345")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	env := decodeRefreshEnvelope(t, rr.Body.Bytes())
	if !env.Success {
		t.Fatalf("expected success envelope, got %+v", env)
	}
	if env.Data.AccessToken == "" {
		t.Fatalf("expected non-empty access_token")
	}
}

func TestRefreshHandlerRejectsRevokedToken(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestRefreshHandler(t, db)

	email := "refresh-h-revoked-" + uuid.NewString() + "@example.com"
	_, refreshToken := register(t, svc, email)

	first := doRefresh(t, h, refreshToken, "", "192.0.2.3:12345")
	if first.Code != http.StatusOK {
		t.Fatalf("first refresh status = %d, want 200; body=%s", first.Code, first.Body.String())
	}

	// reusing the same original cookie is treated as a theft signal: the whole
	// family is revoked and the request fails with session_revoked
	second := doRefresh(t, h, refreshToken, "", "192.0.2.3:12345")
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("second refresh status = %d, want 401; body=%s", second.Code, second.Body.String())
	}

	env := decodeRefreshEnvelope(t, second.Body.Bytes())
	if env.Error.Code != "session_revoked" {
		t.Fatalf("error code = %q, want session_revoked", env.Error.Code)
	}

	var userID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	var familyActive int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&familyActive)
	if err != nil {
		t.Fatalf("count active sessions: %v", err)
	}
	if familyActive != 0 {
		t.Fatalf("expected zero active sessions in the family, got %d", familyActive)
	}
}

func TestRefreshHandlerRejectsExpiredRefreshToken(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestRefreshHandler(t, db)

	email := "refresh-h-expired-rt-" + uuid.NewString() + "@example.com"
	_, refreshToken := register(t, svc, email)

	var userID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		UPDATE sessions
		SET expires_at = NOW() - INTERVAL '1 minute'
		WHERE user_id = $1 AND revoked = false
	`, userID)
	if err != nil {
		t.Fatalf("backdate session: %v", err)
	}

	rr := doRefresh(t, h, refreshToken, "", "192.0.2.4:12345")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}

	env := decodeRefreshEnvelope(t, rr.Body.Bytes())
	if env.Error.Code != "invalid_token" {
		t.Fatalf("error code = %q, want invalid_token", env.Error.Code)
	}
}

func TestRefreshHandlerRejectsMissingCookie(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	h := newTestRefreshHandler(t, db)

	rr := doRefresh(t, h, "", "", "192.0.2.5:12345")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}

	env := decodeRefreshEnvelope(t, rr.Body.Bytes())
	if env.Error.Code != "invalid_token" {
		t.Fatalf("error code = %q, want invalid_token", env.Error.Code)
	}
}

func TestRefreshHandlerRateLimited(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	h := newTestRefreshHandler(t, db)

	// 10 allowed requests (401 for a missing cookie), then the 11th is limited
	for i := 0; i < 10; i++ {
		rr := doRefresh(t, h, "", "", "198.51.100.1:12345")
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("request %d status = %d, want 401; body=%s", i+1, rr.Code, rr.Body.String())
		}
	}

	rr := doRefresh(t, h, "", "", "198.51.100.1:12345")
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("11th request status = %d, want 429; body=%s", rr.Code, rr.Body.String())
	}
}
