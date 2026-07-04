package membership

import (
	"context"
	"database/sql"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
)

type MembershipService struct {
	DB           *sql.DB
	AuditService *audit.AuditService
}

func NewMembershipService(db *sql.DB, auditService *audit.AuditService) *MembershipService {
	return &MembershipService{
		DB:           db,
		AuditService: auditService,
	}
}

// AddOrgMember adds a user to an organization
func (s *MembershipService) AddOrgMember(ctx context.Context, orgID string, actorID string, userID string, userRole Role) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if _, ok := RoleHierarchy[userRole]; !ok {
			return apperrors.ErrMembershipRoleNotFound
		}

		var membershipID string

		// Add to memberships table
		err := tx.QueryRowContext(dbCtx, `
		INSERT INTO memberships (
			user_id,
			organization_id,
			role,
			status,
			created_at
		)
		VALUES ($1, $2, $3, $4, NOW())
		RETURNING id
		`,
			userID, orgID, userRole, MembershipStatusActive,
		).Scan(&membershipID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &actorID,
			Action:         "membership.added",
			EntityType:     "membership",
			EntityID:       &membershipID,
			Metadata: map[string]any{
				"new_member_id": &userID,
			},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

// GetOrgMembers returns all members of an organization
func (s *MembershipService) GetOrgMembers(ctx context.Context, orgID string) ([]Membership, error) {
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
				return apperrors.ErrInternalServer
			}

			members = append(members, m)
		}

		return nil
	})

	return members, err
}

// RemoveOrgMember removes a user from an organization
func (s *MembershipService) RemoveOrgMember(ctx context.Context, orgID string, actorID string, userID string) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {

		// Get user role and ensure it's not owner
		var role Role

		err := tx.QueryRowContext(dbCtx, `
		SELECT role
		FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
	`, orgID, actorID).Scan(&role)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		if role == RoleOwner {
			return apperrors.ErrForbidden
		}

		// Remove from memberships table
		var membershipID string

		err = tx.QueryRowContext(dbCtx, `
		DELETE FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
		RETURNING id
	`, orgID, userID).Scan(&membershipID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &actorID,
			Action:         "membership.removed",
			EntityType:     "membership",
			EntityID:       &membershipID,
			Metadata: map[string]any{
				"removed_member_id": &userID,
			},
		})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return apperrors.ErrInternalServer
	}

	return nil
}

// UpdateOrgMember updates a user's role in an organization
func (s *MembershipService) UpdateOrgMemberRole(ctx context.Context, orgID string, actorID string, userID string, newRole Role) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if _, ok := RoleHierarchy[newRole]; !ok {
			return apperrors.ErrMembershipRoleNotFound
		}

		// Get user current role and ensure it's not owner
		var currentRole Role

		err := tx.QueryRowContext(dbCtx, `
		SELECT role
		FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
		`, orgID, userID).Scan(&currentRole)
		if err != nil {
			return apperrors.ErrDatabase
		}

		if currentRole == RoleOwner {
			return apperrors.ErrForbidden
		}

		// Update role
		var membershipID string

		err = tx.QueryRowContext(dbCtx, `
		UPDATE memberships
		SET role = $1
		WHERE organization_id = $2
		AND user_id = $3
		RETURNING id
		`, newRole, orgID, userID).Scan(&membershipID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &actorID,
			Action:         "membership.role_changed",
			EntityType:     "membership",
			EntityID:       &membershipID,
			Metadata: map[string]any{
				"user_id":  &userID,
				"old_role": currentRole,
				"new_role": newRole,
			},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return err
}
