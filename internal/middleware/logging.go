package middleware

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/pkg/logger"
)

type LoggingMiddleware struct{}

func NewLoggingMiddleware() *LoggingMiddleware {
	return &LoggingMiddleware{}
}

func (m *LoggingMiddleware) Log(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rec := NewResponseRecorder(w)

		next.ServeHTTP(rec, r)

		duration := time.Since(start)

		requestID := r.Context().Value(requestctx.RequestIDKey).(string)

		ip, _, _ := net.SplitHostPort(r.RemoteAddr)

		logger.Info(fmt.Sprintf("request_id=%s method=%s path=%s status=%s ip=%s duration=%s", requestID, r.Method, r.URL.Path, http.StatusText(rec.StatusCode), ip, duration))
	})
}

// ResponseRecorder is wrapper of http.ResponseWriter that
// provides additional information about written status code
type ResponseRecorder struct {
	http.ResponseWriter
	StatusCode int
}

func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{
		ResponseWriter: w,
		StatusCode:     http.StatusOK,
	}
}

func (r *ResponseRecorder) WriteHeader(statusCode int) {
	r.StatusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}
