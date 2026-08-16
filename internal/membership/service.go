package membership

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Service struct {
	DB           *sql.DB
	AuditService *audit.Service
}

// requireOwnerToGrantOwner prevents non-owners from granting the owner role.
// The actor's membership is checked in the same transaction as the change so
// this authorization cannot be bypassed by calling the service directly.
func requireOwnerToGrantOwner(ctx context.Context, tx *sql.Tx, orgID, actorID uuid.UUID, role Role) error {
	if role != RoleOwner {
		return nil
	}

	var actorRole Role
	err := tx.QueryRowContext(ctx, `
		SELECT r.name
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1
		AND m.user_id = $2
		AND m.status = 'active'
		FOR UPDATE OF m
	`, orgID, actorID).Scan(&actorRole)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrForbidden
		}

		return apperrors.ErrDatabase
	}

	if actorRole != RoleOwner {
		return apperrors.ErrForbidden
	}

	return nil
}

func NewService(db *sql.DB, auditService *audit.Service) *Service {
	return &Service{
		DB:           db,
		AuditService: auditService,
	}
}

// InviteOrgMember invites a user (by email) to an organization. The membership
// is created in the 'invited' state; it becomes active when accepted.
func (s *Service) InviteOrgMember(ctx context.Context, orgID uuid.UUID, actorID uuid.UUID, email string, role Role) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := requireOwnerToGrantOwner(dbCtx, tx, orgID, actorID, role); err != nil {
			return err
		}

		// Resolve the invited user by email
		var invitedUserID uuid.UUID

		err := tx.QueryRowContext(dbCtx, `
			SELECT id
			FROM users
			WHERE email = $1
		`, email).Scan(&invitedUserID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrUserNotFound
			}

			return apperrors.ErrDatabase
		}

		// Don't allow inviting the actor into their own org twice
		if invitedUserID == actorID {
			return apperrors.ErrAlreadyMember
		}

		roleID, err := ResolveSystemRoleID(dbCtx, tx, role)
		if err != nil {
			return err
		}

		var membershipID uuid.UUID

		err = tx.QueryRowContext(dbCtx, `
			INSERT INTO memberships (
				user_id,
				organization_id,
				role_id,
				status
			)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`,
			invitedUserID, orgID, roleID, MembershipStatusInvited,
		).Scan(&membershipID)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperrors.ErrInvitationPending
			}

			if db.IsForeignKeyViolation(err) {
				return apperrors.ErrUserNotFound
			}

			return apperrors.ErrDatabase
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &actorID,
			Action:         "membership.invited",
			EntityType:     "membership",
			EntityID:       &membershipID,
			Metadata: map[string]any{
				"invited_user_id": invitedUserID.String(),
				"email":           email,
				"role":            string(role),
			},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

// AddOrgMember adds a user to an organization
func (s *Service) AddOrgMember(ctx context.Context, orgID uuid.UUID, actorID uuid.UUID, newMemberID uuid.UUID, newMemberRole Role) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := requireOwnerToGrantOwner(dbCtx, tx, orgID, actorID, newMemberRole); err != nil {
			return err
		}

		roleID, err := ResolveSystemRoleID(dbCtx, tx, newMemberRole)
		if err != nil {
			return err
		}

		var membershipID uuid.UUID

		err = tx.QueryRowContext(dbCtx, `
			INSERT INTO memberships (
				user_id,
				organization_id,
				role_id,
				status
			)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`,
			newMemberID, orgID, roleID, MembershipStatusActive,
		).Scan(&membershipID)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return apperrors.ErrAlreadyMember
			}

			if db.IsForeignKeyViolation(err) {
				return apperrors.ErrUserNotFound
			}

			return apperrors.ErrDatabase
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &actorID,
			Action:         "membership.added",
			EntityType:     "membership",
			EntityID:       &membershipID,
			Metadata: map[string]any{
				"new_member_id": newMemberID.String(),
				"role":          string(newMemberRole),
			},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

// GetOrgMembers returns all staff members of an organization. Client-role
// memberships are excluded at the query level (both the count and the list):
// clients are project-scoped and must never appear in the staff directory.
func (s *Service) GetOrgMembers(ctx context.Context, orgID uuid.UUID, q pagination.Query) ([]Membership, pagination.Meta, error) {
	var members []Membership
	var total int

	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(dbCtx, `
			SELECT count(*)
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			WHERE m.organization_id = $1
			AND r.name <> 'client'
		`, orgID).Scan(&total); err != nil {
			return apperrors.ErrDatabase
		}

		rows, err := tx.QueryContext(dbCtx, `
			SELECT
				m.id,
				m.user_id,
				m.organization_id,
				m.role_id,
				r.name AS role,
				m.status,
				m.joined_at,
				u.first_name || ' ' || u.last_name AS member_name
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			LEFT JOIN users u ON u.id = m.user_id
			WHERE m.organization_id = $1
			AND r.name <> 'client'
			ORDER BY m.joined_at DESC
			LIMIT $2 OFFSET $3
		`, orgID, q.Limit, q.Offset())
		if err != nil {
			return err
		}

		defer rows.Close()

		for rows.Next() {
			var m Membership
			var memberName sql.NullString

			err := rows.Scan(&m.ID, &m.UserID, &m.OrganizationID, &m.RoleID, &m.Role, &m.Status, &m.JoinedAt, &memberName)
			if err != nil {
				return apperrors.ErrInternalServer
			}

			if memberName.Valid {
				m.MemberName = &memberName.String
			}

			members = append(members, m)
		}
		if rows.Err() != nil {
			return apperrors.ErrDatabase
		}

		return nil
	})

	return members, pagination.NewMeta(q, total), err
}

// RemoveOrgMember removes a user from an organization
func (s *Service) RemoveOrgMember(ctx context.Context, orgID uuid.UUID, actorID uuid.UUID, userID uuid.UUID) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		// Never allow the organization owner to be removed
		var targetRole Role

		err := tx.QueryRowContext(dbCtx, `
			SELECT r.name
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			WHERE m.organization_id = $1
			AND m.user_id = $2
		`, orgID, userID).Scan(&targetRole)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrMembershipNotFound
			}

			return apperrors.ErrDatabase
		}

		if targetRole == RoleOwner {
			return apperrors.ErrForbidden
		}

		// Client memberships are removed exclusively through the project
		// unassign flow (PUT .../projects/{projectID}/client with null): a
		// client's access is project-scoped and removing their membership out
		// from under an assigned project would strand the project reference.
		if targetRole == RoleClient {
			return apperrors.ErrClientAttachedToProject
		}

		// Remove from memberships table
		var membershipID uuid.UUID

		err = tx.QueryRowContext(dbCtx, `
			DELETE FROM memberships
			WHERE organization_id = $1
			AND user_id = $2
			RETURNING id
		`, orgID, userID).Scan(&membershipID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrMembershipNotFound
			}

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
				"removed_member_id": userID.String(),
			},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

// UpdateOrgMemberRole updates a user's role in an organization. The client
// role is fully protected: it can never be granted via role update (clients
// are provisioned through POST /orgs/{orgID}/clients), and an existing client
// membership can never be re-role'd away (it is removed exclusively via the
// project unassign flow). This blocks both escalating client memberships to
// staff roles and demoting staff into the client role.
func (s *Service) UpdateOrgMemberRole(ctx context.Context, orgID uuid.UUID, actorID uuid.UUID, userID uuid.UUID, newRole Role) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	if newRole == RoleClient {
		return apperrors.ErrForbidden
	}

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		if err := requireOwnerToGrantOwner(dbCtx, tx, orgID, actorID, newRole); err != nil {
			return err
		}

		roleID, err := ResolveSystemRoleID(dbCtx, tx, newRole)
		if err != nil {
			return err
		}

		// Get the target user's current role and ensure it's neither the
		// owner nor a client.
		var currentRole Role

		err = tx.QueryRowContext(dbCtx, `
			SELECT r.name
			FROM memberships m
			JOIN roles r ON r.id = m.role_id
			WHERE m.organization_id = $1
			AND m.user_id = $2
		`, orgID, userID).Scan(&currentRole)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrMembershipNotFound
			}

			return apperrors.ErrDatabase
		}

		if currentRole == RoleOwner {
			return apperrors.ErrForbidden
		}

		if currentRole == RoleClient {
			return apperrors.ErrForbidden
		}

		// Update role
		var membershipID uuid.UUID

		err = tx.QueryRowContext(dbCtx, `
			UPDATE memberships
			SET role_id = $1
			WHERE organization_id = $2
			AND user_id = $3
			RETURNING id
		`, roleID, orgID, userID).Scan(&membershipID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &actorID,
			Action:         "membership.role_changed",
			EntityType:     "membership",
			EntityID:       &membershipID,
			Metadata: map[string]any{
				"user_id":  userID.String(),
				"old_role": string(currentRole),
				"new_role": string(newRole),
			},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return err
}
