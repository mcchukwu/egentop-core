package middleware

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
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

// RequirePermission denies the request unless the active membership of the
// authenticated user in the loaded organization holds the given permission key.
// Every decision (allowed or denied) is recorded in authz_decisions.
func (m *RBACMiddleware) RequirePermission(permissionKey string) func(http.Handler) http.Handler {
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

			var (
				role    string
				allowed bool
			)

			err := m.DB.QueryRowContext(r.Context(), `
				SELECT r.name, EXISTS(
					SELECT 1
					FROM role_permissions rp
					JOIN permissions p ON p.id = rp.permission_id
					WHERE rp.role_id = r.id AND p.key = $3
				)
				FROM memberships m
				JOIN roles r ON r.id = m.role_id
				WHERE m.user_id = $1
				AND m.organization_id = $2
				AND m.status = 'active'
				LIMIT 1
			`, userID, organizationID, permissionKey).Scan(&role, &allowed)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					_ = audit.RecordDecision(r.Context(), m.DB, organizationID, userID, permissionKey, "", uuid.Nil, false, "not a member")
					response.HandleError(w, apperrors.ErrForbidden)
					return
				}

				response.HandleError(w, apperrors.ErrDatabase)
				return
			}

			if !allowed {
				_ = audit.RecordDecision(r.Context(), m.DB, organizationID, userID, permissionKey, "", uuid.Nil, false, "role lacks permission")
				response.HandleError(w, apperrors.ErrForbidden)
				return
			}

			_ = audit.RecordDecision(r.Context(), m.DB, organizationID, userID, permissionKey, "", uuid.Nil, true, "ok")

			ctx := requestctx.WithRole(r.Context(), role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
