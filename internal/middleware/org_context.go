package middleware

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/organization"
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
			http.Error(w, "missing organization id", http.StatusBadRequest)
			return
		}

		var org organization.Organization

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
		).Scan(&org.ID, &org.Name, &org.Slug, &org.Status, &org.CreatedAt, &org.UpdatedAt)

		if err != nil {
			if err == sql.ErrNoRows {
				response.HandleError(w, apperrors.ErrOrganizationNotFound)
				return
			}

			response.HandleError(w, apperrors.ErrInternalServer)
			return
		}

		// critical tenant validation
		if org.Status != "active" {
			http.Error(w, "organization unavailable", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), OrganizationKey, &org)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
