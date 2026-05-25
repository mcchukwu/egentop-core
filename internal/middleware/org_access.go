package middleware

import (
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
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

		var membershipID string

		err := m.DB.QueryRowContext(r.Context(), `
			SELECT
				id,
			FROM memberships
			WHERE user_id = $1
			AND organization_id = $2
			AND status = 'active'
			LIMIT 1
		`, userID, organizationID).Scan(&membershipID)
		if err != nil {
			if err == sql.ErrNoRows {
				response.HandleError(w, apperrors.ErrForbidden)
				return
			}

			response.HandleError(w, apperrors.ErrInternalServer)
			return
		}

		ctx := requestctx.WithMembershipID(r.Context(), membershipID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
