package middleware

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/organization"
)

type OrganizationMiddleware struct {
	DB *sql.DB
}

func NewOrganizationMiddleware(db *sql.DB) *OrganizationMiddleware {
	return &OrganizationMiddleware{
		DB: db,
	}
}

func (m *OrganizationMiddleware) LoadOrganization(next http.Handler) http.Handler {
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
				http.Error(w, "organization not found", http.StatusNotFound)
				return
			}

			http.Error(w, "internal server error", http.StatusInternalServerError)
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
