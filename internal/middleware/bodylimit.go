package middleware

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// maxBodyBytes bounds every request body (1 MiB). Larger payloads fail decode
// in the handler; the overrun detection below upgrades those 400s to 413.
const maxBodyBytes = 1 << 20

// BodyLimitMiddleware wraps every request body with http.MaxBytesReader so no
// route (public, cookie-authenticated, or protected) can stream unbounded
// input. It also detects MaxBytesError overruns and rewrites the handler's
// resulting 400 invalid_request_body into 413 payload_too_large.
type BodyLimitMiddleware struct{}

func NewBodyLimitMiddleware() *BodyLimitMiddleware {
	return &BodyLimitMiddleware{}
}

func (m *BodyLimitMiddleware) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		overrun := false
		r.Body = &overrunTrackingBody{
			ReadCloser: http.MaxBytesReader(w, r.Body, maxBodyBytes),
			overrun:    &overrun,
		}

		next.ServeHTTP(&overrunResponseWriter{ResponseWriter: w, overrun: &overrun}, r)
	})
}

// overrunTrackingBody records when the wrapped body hits the MaxBytesReader
// limit so the response writer can upgrade the resulting 400 to 413.
type overrunTrackingBody struct {
	io.ReadCloser
	overrun *bool
}

func (b *overrunTrackingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		*b.overrun = true
	}
	return n, err
}

// overrunResponseWriter rewrites a 400 written after a body overrun into a
// 413 payload_too_large. Once the limit is hit the handler's decode MUST have
// failed, so the 400 is always the invalid_request_body response; no other
// 400 can follow an overrun.
type overrunResponseWriter struct {
	http.ResponseWriter
	overrun     *bool
	status      int
	bodyRewrote bool
}

func (w *overrunResponseWriter) WriteHeader(status int) {
	if *w.overrun && status == http.StatusBadRequest {
		status = http.StatusRequestEntityTooLarge
		w.bodyRewrote = true
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *overrunResponseWriter) Write(b []byte) (int, error) {
	if w.bodyRewrote {
		w.bodyRewrote = false
		payload, _ := json.Marshal(map[string]any{
			"success": false,
			"error": map[string]any{
				"code":    "payload_too_large",
				"message": "request body exceeds the 1MB limit",
			},
		})
		return w.ResponseWriter.Write(payload)
	}
	return w.ResponseWriter.Write(b)
}
