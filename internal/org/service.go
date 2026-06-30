package org

import (
	"context"
	"database/sql"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
)

type OrgService struct {
	DB           *sql.DB
	AuditService *audit.AuditService
}

func NewOrgService(db *sql.DB, auditService *audit.AuditService) *OrgService {
	return &OrgService{
		DB:           db,
		AuditService: auditService,
	}
}

// CreateOrg creates a new organization
func (s *OrgService) CreateOrg(ctx context.Context, name string, slug string, ownerID string) (string, error) {
	var orgID string

	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if name == "" || slug == "" {
			return apperrors.ErrInvalidRequestBody
		}

		// Create organization and return orgID
		err := tx.QueryRowContext(dbCtx, `
		INSERT INTO organizations (name, slug, status, created_at, updated_at)
		VALUES ($1, $2, 'active', NOW(), NOW())
		RETURNING id
		`, name, slug).Scan(&orgID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Create owner membership
		_, err = tx.ExecContext(dbCtx, `
		INSERT INTO memberships (user_id, organization_id, role, status, created_at)
		VALUES ($1, $2, 'owner', 'active', NOW())
		`, ownerID, orgID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &ownerID,
			Action:         "organization.created",
			Metadata:       map[string]any{},
		})
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

// GetUserOrg returns all organizations a user belongs to
func (s *OrgService) GetUserOrg(ctx context.Context, userID string) ([]Membership, error) {
	var result []Membership

	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

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

// GetOrgMembers returns all members of an organization
func (s *OrgService) GetOrgMembers(ctx context.Context, orgID string) ([]Membership, error) {
	var members []Membership

	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Find org rows in memberships table
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

// AddOrgMember adds a user to an organization
func (s *OrgService) AddOrgMember(ctx context.Context, orgID string, userID string, role Role) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if _, ok := RoleHierarchy[role]; !ok {
			return apperrors.ErrMembershipRoleNotFound
		}

		// Add to memberships table
		_, err := tx.ExecContext(dbCtx, `
		INSERT INTO memberships (
			user_id,
			organization_id,
			role,
			status,
			created_at
		)
		VALUES ($1, $2, $3, $4, NOW())
	`,
			userID, orgID, role, StatusActive)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "membership.added",
			Metadata:       map[string]any{},
		})
		if err != nil {
			return apperrors.ErrInternalServer
		}

		return nil
	})
	if err != nil {
		return apperrors.ErrInternalServer
	}

	return nil
}

// RemoveOrgMember removes a user from an organization
func (s *OrgService) RemoveOrgMember(ctx context.Context, orgID string, userID string) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		var role Role

		// Get user role
		err := tx.QueryRowContext(dbCtx, `
		SELECT role
		FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
	`, orgID, userID).Scan(&role)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Check if user is the owner and stop it
		if role == RoleOwner {
			return apperrors.ErrForbidden
		}

		// Remove from memberships table
		_, err = tx.ExecContext(dbCtx, `
		DELETE FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
	`, orgID, userID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "membership.removed",
			Metadata:       map[string]any{},
		})

		return nil
	})
	if err != nil {
		return apperrors.ErrInternalServer
	}

	return nil
}

// UpdateOrgMember updates a user's role in an organization
func (s *OrgService) UpdateOrgMemberRole(ctx context.Context, orgID string, userID string, role Role) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if _, ok := RoleHierarchy[role]; !ok {
			return apperrors.ErrMembershipRoleNotFound
		}

		var currentRole Role

		// Get user role
		err := tx.QueryRowContext(dbCtx, `
		SELECT role
		FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
	`, orgID, userID).Scan(&currentRole)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Owner role immutable
		if currentRole == RoleOwner {
			return apperrors.ErrForbidden
		}

		// Update role
		_, err = tx.ExecContext(dbCtx, `
		UPDATE memberships
		SET role = $1
		WHERE organization_id = $2
		AND user_id = $3
	`, role, orgID, userID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "membership.role_changed",
			Metadata:       map[string]any{},
		})
		if err != nil {
			return apperrors.ErrInternalServer
		}

		return nil
	})
	if err != nil {
		return apperrors.ErrInternalServer
	}

	return nil
}
