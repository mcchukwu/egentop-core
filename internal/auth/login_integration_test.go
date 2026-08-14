package auth

// HTTP-level regression tests for the unified login failure behavior: any bad
// login (unknown identifier, suspended user, wrong password) returns
// 401 invalid_credentials so a failed login never reveals account state.
//
// Mirrors the production route chain: the login rate limiter wraps the auth
// handler. The integration test DB must have all migrations applied.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/validation"
)

type loginEnvelope struct {
	Success bool `json:"success"`
	Data    struct {
		AccessToken string `json:"access_token"`
	} `json:"data"`
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

// newTestLoginHandler builds the production chain for the login route: login
// rate limiter (5/min) wrapping the auth login handler.
func newTestLoginHandler(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()

	h := newTestAuthCore(t, db)
	limiter := middleware.NewRateLimiterMiddleware(5, time.Minute)

	return limiter.Limit(http.HandlerFunc(h.Login))
}

func doLogin(t *testing.T, h http.Handler, identifier, password string) *httptest.ResponseRecorder {
	t.Helper()

	body, _ := json.Marshal(map[string]string{"identifier": identifier, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.10:12345"

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr
}

func decodeLoginEnvelope(t *testing.T, body []byte) loginEnvelope {
	t.Helper()

	var env loginEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode body: %v; body=%s", err, string(body))
	}
	return env
}

// TestLoginFailuresAreUniform401 covers the anti-enumeration contract over
// HTTP: unknown identifier, suspended user, and wrong password all produce
// 401 invalid_credentials, while a valid login still succeeds.
func TestLoginFailuresAreUniform401(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	h := newTestLoginHandler(t, db)

	email := "login-h-" + uuid.NewString() + "@example.com"
	register(t, svc, email)

	// Wrong password -> 401 invalid_credentials.
	rr := doLogin(t, h, email, "definitely-wrong")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeLoginEnvelope(t, rr.Body.Bytes()).Error.Code; got != "invalid_credentials" {
		t.Fatalf("wrong-password error code = %q, want invalid_credentials", got)
	}

	// Unknown identifier -> 401 invalid_credentials (indistinguishable).
	rr = doLogin(t, h, "ghost-"+uuid.NewString()+"@example.com", "password123")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unknown-identifier status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeLoginEnvelope(t, rr.Body.Bytes()).Error.Code; got != "invalid_credentials" {
		t.Fatalf("unknown-identifier error code = %q, want invalid_credentials", got)
	}

	// Suspended user -> 401 invalid_credentials (indistinguishable).
	if _, err := db.ExecContext(ctx, `
		UPDATE users SET status = 'suspended' WHERE email = $1
	`, email); err != nil {
		t.Fatalf("suspend user: %v", err)
	}
	rr = doLogin(t, h, email, "password123")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("suspended status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeLoginEnvelope(t, rr.Body.Bytes()).Error.Code; got != "invalid_credentials" {
		t.Fatalf("suspended error code = %q, want invalid_credentials", got)
	}

	// A valid login still succeeds (re-activate first).
	if _, err := db.ExecContext(ctx, `
		UPDATE users SET status = 'active' WHERE email = $1
	`, email); err != nil {
		t.Fatalf("re-activate user: %v", err)
	}
	rr = doLogin(t, h, email, "password123")
	if rr.Code != http.StatusOK {
		t.Fatalf("valid login status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeLoginEnvelope(t, rr.Body.Bytes()).Data.AccessToken; got == "" {
		t.Fatal("expected an access token from a valid login")
	}
}
