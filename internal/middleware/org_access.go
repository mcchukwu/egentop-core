package middleware

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/org"
	"github.com/mcchukwu/egentop/internal/response"
)

type OrgAccessMiddleware struct {
	DB *sql.DB
}

func NewOrgAccessMiddleware(db *sql.DB) *OrgAccessMiddleware {
	return &OrgAccessMiddleware{
		DB: db,
	}
}

func (m *OrgAccessMiddleware) RequireMembership(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserID(r.Context())
		if userID == "" {
			response.HandleError(w, apperrors.ErrUnauthorized)
			return
		}

		organization := GetOrganization(r.Context())
		if organization == nil {
			response.HandleError(w, apperrors.ErrOrganizationNotFound)
			return
		}

		var membership org.Membership

		err := m.DB.QueryRowContext(r.Context(), `
			SELECT
				user_id,
				organization_id,
				role,
				status,
				created_at
			FROM memberships
			WHERE user_id = $1
			AND organization_id = $2
			AND status = 'active'
			LIMIT 1
		`, userID, organization.ID).Scan(&membership.UserID, &membership.OrganizationID, &membership.Role, &membership.Status, &membership.CreatedAt)
		if err != nil {
			if err == sql.ErrNoRows {
				response.HandleError(w, apperrors.ErrForbidden)
				return
			}

			response.HandleError(w, apperrors.ErrInternalServer)
			return
		}

		ctx := context.WithValue(r.Context(), MembershipKey, &membership)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
