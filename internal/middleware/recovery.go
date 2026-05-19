package middleware

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/pkg/logger"
)

type RecoveryMiddleware struct{}

func NewRecoveryMiddleware() *RecoveryMiddleware {
	return &RecoveryMiddleware{}
}

func (m *RecoveryMiddleware) Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				requestID, _ := r.Context().Value(RequestIDKey).(string)

				logger.Error(fmt.Sprintf("panic recovered request_id=%s panic=%v stack=%s", requestID, err, debug.Stack()))
				response.HandleError(w, apperrors.ErrInternalServer)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
