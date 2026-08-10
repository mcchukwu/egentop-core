package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/slug"
	"github.com/mcchukwu/egentop/pkg/db"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

const slugConstraint = "organizations_slug_key"

type Service struct {
	db    *sql.DB
	audit *audit.Service
}

func NewService(db *sql.DB, audit *audit.Service) *Service {
	return &Service{
		db:    db,
		audit: audit,
	}
}

// Create creates a new organization
func (s *Service) Create(ctx context.Context, name string, ownerID uuid.UUID) (uuid.UUID, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	if name == "" {
		return uuid.Nil, apperrors.ErrOrganizationNameInvalid
	}

	//TODO: Generate unique organization slug upon creation attempt
	const maxSlugAttempts = 5
	var orgID uuid.UUID

	err := db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		// Create organization and return orgID
		for i := 0; i < maxSlugAttempts; i++ {
			var generatedSlug string

			if i == 0 {
				generatedSlug = slug.Generate(name)
			} else {
				var err error
				generatedSlug, err = slug.GenerateWithSuffix(name)
				if err != nil {
					return err
				}
			}

			// Each insert attempt runs behind a savepoint so a unique
			// violation on the slug does not abort the whole transaction.
			spName := fmt.Sprintf("org_slug_%d", i)
			if _, err := tx.ExecContext(dbCtx, `SAVEPOINT `+spName); err != nil {
				return apperrors.ErrDatabase
			}

			err := tx.QueryRowContext(dbCtx, `
			INSERT INTO organizations (name, slug, status, created_at, updated_at)
			VALUES ($1, $2, $3, NOW(), NOW())
			RETURNING id
			`, name, string(generatedSlug), OrganizationStatusActive).Scan(&orgID)
			if err != nil {
				// Roll back to the savepoint, discarding the failed insert.
				if _, rbErr := tx.ExecContext(dbCtx, `ROLLBACK TO SAVEPOINT `+spName); rbErr != nil {
					return apperrors.ErrDatabase
				}

				// Slug collision: retry with a suffix.
				if db.IsUniqueConstraintViolation(err, slugConstraint) {
					continue
				}

				return apperrors.ErrDatabase
			}

			// Insert succeeded; orgID is set.
			if _, err := tx.ExecContext(dbCtx, `RELEASE SAVEPOINT `+spName); err != nil {
				return apperrors.ErrDatabase
			}
			break
		}

		if orgID == uuid.Nil {
			return apperrors.ErrOrganizationSlugExists
		}

		// Create owner membership
		ownerRoleID, err := membership.ResolveSystemRoleID(dbCtx, tx, membership.RoleOwner)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(dbCtx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, $4)
		`, ownerID, orgID, ownerRoleID, membership.MembershipStatusActive)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Audit Log
		err = s.audit.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &ownerID,
			Action:         "organization.created",
			EntityType:     "organization",
			EntityID:       &orgID,
			Metadata:       map[string]any{},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return orgID, err
}

// List returns all organizations an active user belongs to
func (s *Service) List(ctx context.Context, userID uuid.UUID, q pagination.Query) ([]membership.Membership, pagination.Meta, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var memberships []membership.Membership
	var total int

	err := db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(dbCtx, `
			SELECT count(*)
			FROM memberships
			WHERE user_id = $1
			AND status = $2
		`, userID, membership.MembershipStatusActive).Scan(&total); err != nil {
			return apperrors.ErrDatabase
		}

		// Find user rows in memberships table
		rows, err := tx.QueryContext(dbCtx, `
			SELECT
				m.id,
				m.user_id,
				m.organization_id,
				m.role_id,
				r.name AS role,
				m.status,
				m.joined_at
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			WHERE m.user_id = $1
			AND m.status = $2
			ORDER BY m.joined_at DESC
			LIMIT $3 OFFSET $4
		`, userID, membership.MembershipStatusActive, q.Limit, q.Offset())
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrUserNotFound
			}

			return apperrors.ErrDatabase
		}

		defer rows.Close()

		// Loop through rows and populate memberships
		for rows.Next() {
			var m membership.Membership

			err := rows.Scan(&m.ID, &m.UserID, &m.OrganizationID, &m.RoleID, &m.Role, &m.Status, &m.JoinedAt)
			if err != nil {
				return apperrors.ErrInternalServer
			}

			memberships = append(memberships, m)
		}
		if rows.Err() != nil {
			return apperrors.ErrDatabase
		}

		return nil
	})

	return memberships, pagination.NewMeta(q, total), err
}

func (s *Service) GetByID(ctx context.Context, orgID uuid.UUID) (*Organization, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result = &Organization{}

	err := db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(dbCtx,
			`SELECT
			id,
			name,
			slug,
			status,
			created_at,
			updated_at
			FROM organizations
			WHERE id = $1
			AND status = $2
		`, orgID, OrganizationStatusActive).Scan(&result.ID, &result.Name, &result.Slug, &result.Status, &result.CreatedAt, &result.UpdatedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrOrganizationNotFound
			}
			return apperrors.ErrDatabase
		}

		return nil
	})

	return result, err
}

// Update renames an organization.
func (s *Service) Update(ctx context.Context, orgID uuid.UUID, name string) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	if name == "" {
		return apperrors.ErrOrganizationNameInvalid
	}

	err := db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		var currentName string

		err := tx.QueryRowContext(dbCtx, `
			SELECT name
			FROM organizations
			WHERE id = $1
			AND status = $2
		`, orgID, OrganizationStatusActive).Scan(&currentName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrOrganizationNotFound
			}

			return apperrors.ErrDatabase
		}

		if currentName == name {
			return nil
		}

		_, err = tx.ExecContext(dbCtx, `
			UPDATE organizations
			SET name = $1, updated_at = NOW()
			WHERE id = $2
		`, name, orgID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		return s.audit.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			Action:         "organization.updated",
			EntityType:     "organization",
			EntityID:       &orgID,
			Metadata: map[string]any{
				"old_name": currentName,
				"new_name": name,
			},
		})
	})

	return err
}

// nullableString returns a pointer to the string if it's not empty
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
