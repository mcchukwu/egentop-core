package organization

import (
	"context"
	"database/sql"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/pkg/db"
)

type OrganizationService struct {
	DB    *sql.DB
	Audit *audit.AuditService
}

func NewOrganizationService(db *sql.DB, audit *audit.AuditService) *OrganizationService {
	return &OrganizationService{
		DB:    db,
		Audit: audit,
	}
}

// CreateOrganization creates a new organization
func (s *OrganizationService) CreateOrganization(ctx context.Context, name string, slug string, ownerID string) (string, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var orgID string

	if name == "" {
		return "", apperrors.ErrOrganizationNameInvalid
	}

	//TODO: Generate unique organization slug upon creation attempt

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Create organization and return orgID
		err := tx.QueryRowContext(dbCtx, `
		INSERT INTO organizations (name, slug, status, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		RETURNING id
		`, nullableString(slug), OrganizationStatusActive).Scan(&orgID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Create owner membership
		_, err = tx.ExecContext(dbCtx, `
		INSERT INTO memberships (user_id, organization_id, role, status, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		`, ownerID, orgID, membership.RoleOwner, membership.MembershipStatusActive)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Audit Log
		err = s.Audit.Log(dbCtx, tx, audit.LogEntry{
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

// GetUserOrg returns all organizations an active user belongs to
func (s *OrganizationService) GetUserOrganizations(ctx context.Context, userID string) ([]membership.Membership, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result []membership.Membership

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Find user rows in memberships table
		rows, err := tx.QueryContext(dbCtx, `
		SELECT user_id, organization_id, role, status, created_at
		FROM memberships
		WHERE user_id = $1 AND status = 'active'
		`, userID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		defer rows.Close()

		// Loop through rows and populate memberships
		for rows.Next() {
			var m membership.Membership

			err := rows.Scan(&m.UserID, &m.OrganizationID, &m.Role, &m.Status, &m.CreatedAt)
			if err != nil {
				return apperrors.ErrInternalServer
			}

			result = append(result, m)
		}

		return nil
	})

	return result, err
}

func (s *OrganizationService) GetOrganizationByID(ctx context.Context, userID string, orgID string) (*Organization, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *Organization

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
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
			AND status = 'active'
		`, orgID).Scan(&result.ID, &result.Name, &result.Slug, &result.Status, &result.CreatedAt, &result.UpdatedAt)
		if err != nil {
			return apperrors.ErrDatabase
		}

		return nil
	})

	return result, err
}

// ---------------------------------
// --------- Helpers ---------------
// ---------------------------------

// nullableString returns a pointer to the string if it's not empty
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
