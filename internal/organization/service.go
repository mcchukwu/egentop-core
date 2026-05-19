package organization

import (
	"context"
	"database/sql"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/pkg/db"
)

type OrgService struct {
	DB *sql.DB
}

func NewOrgService(db *sql.DB) *OrgService {
	return &OrgService{
		DB: db,
	}
}

func (s *OrgService) CreateOrg(ctx context.Context, name string, slug string, ownerID string) (string, error) {
	var org Organization
	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		if name == "" || slug == "" {
			return apperrors.ErrInvalidRequestBody
		}

		err := tx.QueryRowContext(dbCtx, `
		INSERT INTO organizations (
			name,
			slug,
			status,
			created_at,
			updated_at
		) VALUES ($1, $2, 'active', NOW())
		RETURNING id`, name, slug).Scan(&org.ID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		return nil
	})
	if err != nil {
		return "", apperrors.ErrInternalServer
	}

	return org.ID, nil
}
