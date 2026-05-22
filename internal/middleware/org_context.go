package middleware

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/org"
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

		var organization org.Organization

		err := m.DB.QueryRowContext(r.Context(),
			`
			SELECT
				id,
				name,
				slug,
				status,
				created_at,
				updated_at
			FROM organizations
			WHERE id = $1
			`,
			orgID,
		).Scan(&organization.ID, &organization.Name, &organization.Slug, &organization.Status, &organization.CreatedAt, &organization.UpdatedAt)
		if err != nil {
			if err == sql.ErrNoRows {
				response.HandleError(w, apperrors.ErrOrganizationNotFound)
				return
			}

			response.HandleError(w, apperrors.ErrInternalServer)
			return
		}

		// critical tenant validation
		if organization.Status != "active" {
			response.HandleError(w, apperrors.ErrOrganizationSuspended)
			return
		}

		ctx := context.WithValue(r.Context(), OrganizationKey, &organization)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
