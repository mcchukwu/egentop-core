package auth

// HTTP-level regression for the register endpoint's conflict behavior: the
// login anti-enumeration change (uniform 401 invalid_credentials) must NOT
// have changed register semantics — a duplicate email or phone still returns
// its documented 409s.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/validation"
)

// registerPhoneCounter keeps phones unique across the whole run (users.phone
// is UNIQUE; the same counter pattern is used in the project package).
var registerPhoneCounter atomic.Int64

func newRegisterPhone() string {
	n := registerPhoneCounter.Add(1)
	return fmt.Sprintf("+2348%08d%01d", time.Now().UnixNano()%100000000, n%10)
}

// newTestRegisterHandler mirrors the production register route chain: the
// register rate limiter wrapping the auth register handler.
func newTestRegisterHandler(t *testing.T, db *sql.DB) http.Handler {
	t.Helper()

	h := newTestAuthCore(t, db)
	limiter := middleware.NewRateLimiterMiddleware(50, time.Minute)

	return limiter.Limit(http.HandlerFunc(h.Register))
}

func doRegister(t *testing.T, h http.Handler, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal register body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.20:12345"

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr
}

// TestRegisterDuplicateEmailAndPhoneStill409: a second register with the same
// email (or phone) returns 409 email_already_exists / phone_already_exists.
func TestRegisterDuplicateEmailAndPhoneStill409(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	h := newTestRegisterHandler(t, db)

	email := "dup-email-" + uuid.NewString() + "@example.com"
	phone := newRegisterPhone()

	// First register succeeds.
	if rr := doRegister(t, h, map[string]string{
		"email":      email,
		"password":   "password123",
		"first_name": "Dup",
		"last_name":  "Email",
	}); rr.Code != http.StatusOK {
		t.Fatalf("first register status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	// Duplicate email -> 409 email_already_exists.
	rr := doRegister(t, h, map[string]string{
		"email":      email,
		"password":   "password123",
		"first_name": "Dup",
		"last_name":  "Email",
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate email status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeLoginEnvelope(t, rr.Body.Bytes()).Error.Code; got != "email_already_exists" {
		t.Fatalf("duplicate email error code = %q, want email_already_exists", got)
	}

	// A different account carrying the same phone -> 409 phone_already_exists.
	if rr := doRegister(t, h, map[string]string{
		"phone":      phone,
		"password":   "password123",
		"first_name": "Dup",
		"last_name":  "Phone",
	}); rr.Code != http.StatusOK {
		t.Fatalf("phone register status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	rr = doRegister(t, h, map[string]string{
		"phone":      phone,
		"password":   "password123",
		"first_name": "Other",
		"last_name":  "Phone",
	})
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate phone status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if got := decodeLoginEnvelope(t, rr.Body.Bytes()).Error.Code; got != "phone_already_exists" {
		t.Fatalf("duplicate phone error code = %q, want phone_already_exists", got)
	}
}
