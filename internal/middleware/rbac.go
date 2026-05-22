package middleware

import (
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/org"
	"github.com/mcchukwu/egentop/internal/response"
)

type RBACMiddleware struct {
	DB *sql.DB
}

func NewRBACMiddleware(db *sql.DB) *RBACMiddleware {
	return &RBACMiddleware{
		DB: db,
	}
}

func (m *RBACMiddleware) RequireRole(allowedRoles ...org.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			membership := GetMembership(r.Context())
			if membership == nil {
				response.HandleError(w, apperrors.ErrMembershipNotFound)
				return
			}

			userRoleLevel := org.RoleHierarchy[membership.Role]

			allowed := false

			for _, role := range allowedRoles {
				requiredRoleLevel := org.RoleHierarchy[role]

				if userRoleLevel >= requiredRoleLevel {
					allowed = true
					break
				}
			}

			if !allowed {
				response.HandleError(w, apperrors.ErrForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
