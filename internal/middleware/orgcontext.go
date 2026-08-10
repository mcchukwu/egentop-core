package middleware

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
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
		orgIDStr := r.PathValue("orgID")
		if orgIDStr == "" {
			response.HandleError(w, apperrors.ErrInvalidRequestBody)
			return
		}

		// parse and validate orgID
		orgID, err := uuid.Parse(orgIDStr)
		if err != nil {
			response.HandleError(w, apperrors.ErrOrganizationIDInvalid)
			return
		}

		// get organization status
		var organizationStatus string

		err = m.DB.QueryRowContext(r.Context(),
			`
			SELECT status
			FROM organizations
			WHERE id = $1
			`,
			orgID,
		).Scan(&organizationStatus)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				response.HandleError(w, apperrors.ErrOrganizationNotFound)
				return
			}

			response.HandleError(w, apperrors.ErrDatabase)
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
