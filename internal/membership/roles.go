package membership

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
)

// Queryer is the minimal interface satisfied by both *sql.DB and *sql.Tx
// so system role lookups can run inside or outside a transaction.
type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ResolveSystemRoleID resolves a system template role (organization_id IS NULL)
// by name. Memberships always point at system template roles; org-scoped
// custom roles can be layered on later without changing this lookup.
func ResolveSystemRoleID(ctx context.Context, q Queryer, role Role) (uuid.UUID, error) {
	var roleID uuid.UUID

	err := q.QueryRowContext(ctx, `
		SELECT id
		FROM roles
		WHERE name = $1
		AND organization_id IS NULL
	`, string(role)).Scan(&roleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, apperrors.ErrMembershipRoleNotFound
		}

		return uuid.Nil, apperrors.ErrDatabase
	}

	return roleID, nil
}
