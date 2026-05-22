package org

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
	var orgID string
	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		if name == "" || slug == "" {
			return apperrors.ErrInvalidRequestBody
		}

		err := tx.QueryRowContext(dbCtx, `
		INSERT INTO organizations (name, slug, status, created_at, updated_at)
		VALUES ($1, $2, 'active', NOW(), NOW())
		RETURNING id
		`, name, slug).Scan(&orgID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Owner membership (critical bootstrap step)
		_, err = tx.ExecContext(dbCtx, `
		INSERT INTO memberships (user_id, organization_id, role, status, created_at)
		VALUES ($1, $2, 'owner', 'active', NOW())
		`, ownerID, orgID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		return nil
	})
	if err != nil {
		return "", apperrors.ErrInternalServer
	}

	return orgID, nil
}

func (s *OrgService) GetUserOrg(ctx context.Context, userID string) ([]Membership, error) {
	var result []Membership
	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		rows, err := tx.QueryContext(dbCtx, `
		SELECT user_id, organization_id, role, status, created_at
		FROM memberships
		WHERE user_id = $1 AND status = 'active'
		`, userID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		defer rows.Close()

		for rows.Next() {
			var m Membership

			err := rows.Scan(&m.UserID, &m.OrganizationID, &m.Role, &m.Status, &m.CreatedAt)
			if err != nil {
				return err
			}

			result = append(result, m)
		}

		return nil
	})
	if err != nil {
		return nil, apperrors.ErrInternalServer
	}

	return result, nil
}

func (s *OrgService) GetOrgMembers(ctx context.Context, orgID string) ([]Membership, error) {
	var members []Membership

	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		rows, err := tx.QueryContext(dbCtx, `
			SELECT
				user_id,
				organization_id,
				role,
				status,
				created_at
			FROM memberships
			WHERE organization_id = $1
		`, orgID)
		if err != nil {
			return err
		}

		defer rows.Close()

		for rows.Next() {
			var m Membership

			err := rows.Scan(&m.UserID, &m.OrganizationID, &m.Role, &m.Status, &m.CreatedAt)
			if err != nil {
				return err
			}

			members = append(members, m)
		}

		return nil
	})
	if err != nil {
		return nil, apperrors.ErrInternalServer
	}

	return members, nil
}
