package middleware

import (
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/org"
	"github.com/mcchukwu/egentop/internal/requestctx"
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
			userID, ok := requestctx.UserID(r.Context())
			if !ok {
				response.HandleError(w, apperrors.ErrUnauthorized)
				return
			}

			organizationID, ok := requestctx.OrganizationID(r.Context())
			if !ok {
				response.HandleError(w, apperrors.ErrUnauthorized)
				return
			}

			var role string

			err := m.DB.QueryRowContext(r.Context(), `
			SELECT role
			FROM memberships
			WHERE user_id = $1
			AND organization_id = $2
			AND status = 'active'
			LIMIT 1
		`, userID, organizationID).Scan(&role)
			if err != nil {
				if err == sql.ErrNoRows {
					response.HandleError(w, apperrors.ErrForbidden)
					return
				}

				response.HandleError(w, apperrors.ErrInternalServer)
				return
			}

			userRoleLevel := org.RoleHierarchy[org.Role(role)]

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
