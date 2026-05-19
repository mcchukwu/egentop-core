package middleware

import (
	"fmt"
	"net"
	"net/http"
	"time"

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

		requestID := r.Context().Value(RequestIDKey).(string)

		ip, _, _ := net.SplitHostPort(r.RemoteAddr)

		logger.Info(fmt.Sprintf("request_id=%s method=%s path=%s status=%s ip=%s duration=%s", requestID, r.Method, r.URL.Path, http.StatusText(rec.StatusCode), ip, duration))
	})
}

type ResponseRecorder struct {
	http.ResponseWriter
	StatusCode int
}

func (r *ResponseRecorder) WriteHeader(statusCode int) {
	r.StatusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{
		ResponseWriter: w,
		StatusCode:     http.StatusOK,
	}
}
