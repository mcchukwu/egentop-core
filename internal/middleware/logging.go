package middleware

import (
	"fmt"
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

		next.ServeHTTP(w, r)

		duration := time.Since(start)

		requestID := r.Context().Value(RequestIDKey).(string)

		logger.Info(fmt.Sprintf("request_id=%s method=%s path=%s duration=%s", requestID, r.Method, r.URL.Path, duration))
	})
}
