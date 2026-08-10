package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
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

			var role string

			err := m.DB.QueryRowContext(r.Context(), `
				SELECT r.name
				FROM memberships m
				JOIN roles r ON r.id = m.role_id
				WHERE m.user_id = $1
				AND m.organization_id = $2
				AND m.status = 'active'
				LIMIT 1
			`, userID, organizationID).Scan(&role)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					_ = recordAuthzDecision(r.Context(), m.DB, organizationID, userID, permissionKey, "", uuid.Nil, false, "not a member")
					response.HandleError(w, apperrors.ErrForbidden)
					return
				}

				response.HandleError(w, apperrors.ErrDatabase)
				return
			}

			var allowed bool

			err = m.DB.QueryRowContext(r.Context(), `
				SELECT EXISTS (
					SELECT 1
					FROM memberships m
					JOIN roles r          ON r.id = m.role_id
					JOIN role_permissions rp ON rp.role_id = r.id
					JOIN permissions p    ON p.id = rp.permission_id
					WHERE m.user_id = $1
					AND m.organization_id = $2
					AND m.status = 'active'
					AND p.key = $3
				)
			`, userID, organizationID, permissionKey).Scan(&allowed)
			if err != nil {
				response.HandleError(w, apperrors.ErrDatabase)
				return
			}

			if !allowed {
				_ = recordAuthzDecision(r.Context(), m.DB, organizationID, userID, permissionKey, "", uuid.Nil, false, "role lacks permission")
				response.HandleError(w, apperrors.ErrForbidden)
				return
			}

			_ = recordAuthzDecision(r.Context(), m.DB, organizationID, userID, permissionKey, "", uuid.Nil, true, "ok")

			ctx := requestctx.WithRole(r.Context(), role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// recordAuthzDecision writes an authorization decision to the audit log.
func recordAuthzDecision(ctx context.Context, db *sql.DB, organizationID uuid.UUID, userID uuid.UUID, permissionKey string, resourceType string, resourceID uuid.UUID, allowed bool, reason string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO authz_decisions (
			organization_id,
			user_id,
			permission_key,
			resource_type,
			resource_id,
			allowed,
			reason
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, organizationID, userID, permissionKey, nullableString(resourceType), nullableUUID(resourceID), allowed, reason)
	if err != nil {
		return err
	}

	return nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func nullableUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}

	return &id
}
