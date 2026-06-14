package middleware

import (
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
)

type OrgMiddleware struct {
	DB *sql.DB
}

func NewOrgMiddleware(db *sql.DB) *OrgMiddleware {
	return &OrgMiddleware{
		DB: db,
	}
}

func (m *OrgMiddleware) LoadOrg(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := r.PathValue("orgID")
		if orgID == "" {
			response.HandleError(w, apperrors.ErrOrganizationNotFound)
			return
		}

		var organizationStatus string

		err := m.DB.QueryRowContext(r.Context(),
			`
			SELECT
				status,
			FROM organizations
			WHERE id = $1
			`,
			orgID,
		).Scan(&organizationStatus)
		if err != nil {
			if err == sql.ErrNoRows {
				response.HandleError(w, apperrors.ErrOrganizationNotFound)
				return
			}

			response.HandleError(w, apperrors.ErrInternalServer)
			return
		}

		// critical tenant validation
		if organizationStatus != "active" {
			response.HandleError(w, apperrors.ErrOrganizationSuspended)
			return
		}

		ctx := requestctx.WithOrganizationID(r.Context(), orgID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
