package middleware

import (
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/organization"
)

type RBACMiddleware struct {
	DB *sql.DB
}

func NewRBACMiddleware(db *sql.DB) *RBACMiddleware {
	return &RBACMiddleware{
		DB: db,
	}
}

func (m *RBACMiddleware) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(UserIDKey).(string)
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			org, ok := r.Context().Value(OrganizationKey).(*organization.Organization)
			if !ok {
				http.Error(w, "organization missing", http.StatusInternalServerError)
				return
			}

			var role string

			err := m.DB.QueryRowContext(r.Context(),
				`
				SELECT role
				FROM memberships
				WHERE user_id = $1
				  AND organization_id = $2
				  AND status = 'active'
				`,
				userID,
				org.ID,
			).Scan(&role)

			if err != nil {
				if err == sql.ErrNoRows {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}

				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			allowed := false

			for _, allowedRole := range allowedRoles {
				if role == allowedRole {
					allowed = true
					break
				}
			}

			if !allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
