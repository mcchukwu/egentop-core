package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/response"
)

// decodeHandler mirrors a production body-reading handler: it decodes the
// request body and maps decode failures to ErrInvalidRequestBody (400).
func decodeHandler(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// oversizedJSONBody returns a syntactically valid JSON body whose total size
// exceeds the 1MB cap (the decoder must keep reading to hit the limit).
func oversizedJSONBody() string {
	return `{"data": "` + strings.Repeat("a", maxBodyBytes+10) + `"}`
}

// TestBodyLimitAllowsNormalBody: a body under the 1MB cap passes through
// unchanged.
func TestBodyLimitAllowsNormalBody(t *testing.T) {
	m := NewBodyLimitMiddleware()
	handler := m.Limit(http.HandlerFunc(decodeHandler))

	req := httptest.NewRequest(http.MethodPost, "/v1/orgs", strings.NewReader(`{"name": "small"}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestBodyLimitRejectsOversizedBody: a valid JSON body over the 1MB cap makes
// the handler's decode fail with a MaxBytesError; the middleware upgrades the
// resulting 400 invalid_request_body into 413 payload_too_large.
func TestBodyLimitRejectsOversizedBody(t *testing.T) {
	m := NewBodyLimitMiddleware()
	handler := m.Limit(http.HandlerFunc(decodeHandler))

	req := httptest.NewRequest(http.MethodPost, "/v1/orgs", strings.NewReader(oversizedJSONBody()))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rr.Code, rr.Body.String())
	}

	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil || env.Error == nil || env.Error.Code != "payload_too_large" {
		t.Fatalf("error body = %s, want code payload_too_large", rr.Body.String())
	}
}
