package client

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

// Queryer is the minimal interface satisfied by both *sql.DB and *sql.Tx so
// the lookup helpers can run inside or outside a transaction.
type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Repository struct {
	DB *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{DB: db}
}

// UserIdentity is the subset of the users row the client package needs.
type UserIdentity struct {
	ID        uuid.UUID
	Email     *string
	Phone     *string
	FirstName string
	LastName  string
}

// FindUserByIdentifier resolves a user by email, then by phone. It returns
// apperrors.ErrUserNotFound when neither matches.
func (r *Repository) FindUserByIdentifier(ctx context.Context, q Queryer, email, phone string) (*UserIdentity, error) {
	if email != "" {
		u, err := findUser(ctx, q, "email", email)
		if err == nil {
			return u, nil
		}
		if !errors.Is(err, apperrors.ErrUserNotFound) {
			return nil, err
		}
	}

	if phone != "" {
		return findUser(ctx, q, "phone", phone)
	}

	return nil, apperrors.ErrUserNotFound
}

func findUser(ctx context.Context, q Queryer, column, value string) (*UserIdentity, error) {
	u := &UserIdentity{}
	err := q.QueryRowContext(ctx, `
		SELECT id, email, phone, first_name, last_name
		FROM users
		WHERE `+column+` = $1
	`, value).Scan(&u.ID, &u.Email, &u.Phone, &u.FirstName, &u.LastName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrUserNotFound
		}
		return nil, apperrors.ErrDatabase
	}
	return u, nil
}

// CreateUser inserts a new user with the given bcrypt password hash and
// must_change_password = true (the one-time credential must be rotated before
// the client can act). It never creates an organization.
func (r *Repository) CreateUser(ctx context.Context, tx *sql.Tx, email, phone *string, firstName, lastName, passwordHash string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		INSERT INTO users (email, phone, password_hash, first_name, last_name, must_change_password)
		VALUES ($1, $2, $3, $4, $5, TRUE)
		RETURNING id
	`, email, phone, passwordHash, firstName, lastName).Scan(&userID)
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// IsMember reports whether the user already has any membership row in the
// organization (any status: the unique (user_id, organization_id) constraint
// means a second row is impossible).
func (r *Repository) IsMember(ctx context.Context, q Queryer, userID, orgID uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM memberships
			WHERE user_id = $1 AND organization_id = $2
		)
	`, userID, orgID).Scan(&exists)
	if err != nil {
		return false, apperrors.ErrDatabase
	}
	return exists, nil
}

// CreateClientMembership inserts an active membership pointing at the client
// role. A unique violation means the user already holds a membership.
func (r *Repository) CreateClientMembership(ctx context.Context, tx *sql.Tx, userID, orgID, clientRoleID uuid.UUID) (uuid.UUID, error) {
	var membershipID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
		RETURNING id
	`, userID, orgID, clientRoleID).Scan(&membershipID)
	if err != nil {
		return uuid.Nil, err
	}
	return membershipID, nil
}

// ListClients returns the organization's client-role memberships (staff
// memberships are excluded by the role filter), newest first.
func (r *Repository) ListClients(ctx context.Context, orgID uuid.UUID, q pagination.Query) ([]Client, int, error) {
	var clients []Client
	var total int

	if err := r.DB.QueryRowContext(ctx, `
		SELECT count(*)
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1
		AND r.name = 'client'
	`, orgID).Scan(&total); err != nil {
		return nil, 0, apperrors.ErrDatabase
	}

	rows, err := r.DB.QueryContext(ctx, `
		SELECT u.id, u.email, u.phone, u.first_name, u.last_name, m.joined_at
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1
		AND r.name = 'client'
		ORDER BY m.joined_at DESC
		LIMIT $2 OFFSET $3
	`, orgID, q.Limit, q.Offset())
	if err != nil {
		return nil, 0, apperrors.ErrDatabase
	}
	defer rows.Close()

	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.UserID, &c.Email, &c.Phone, &c.FirstName, &c.LastName, &c.JoinedAt); err != nil {
			return nil, 0, apperrors.ErrDatabase
		}
		clients = append(clients, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, apperrors.ErrDatabase
	}

	return clients, total, nil
}

// GetClientUser resolves a user's active client-role membership in the
// organization together with their identity. ErrClientNotFound when the user
// is not an active client of the org.
func (r *Repository) GetClientUser(ctx context.Context, q Queryer, orgID uuid.UUID, userID uuid.UUID) (*Client, error) {
	c := &Client{}
	err := q.QueryRowContext(ctx, `
		SELECT u.id, u.email, u.phone, u.first_name, u.last_name, m.joined_at
		FROM memberships m
		JOIN users u ON u.id = m.user_id
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1
		AND u.id = $2
		AND m.status = 'active'
		AND r.name = 'client'
	`, orgID, userID).Scan(&c.UserID, &c.Email, &c.Phone, &c.FirstName, &c.LastName, &c.JoinedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrClientNotFound
		}
		return nil, apperrors.ErrDatabase
	}
	return c, nil
}

// RotateCredential replaces the user's password hash with a fresh one-time
// credential and re-arms the forced-password-change gate.
func (r *Repository) RotateCredential(ctx context.Context, tx *sql.Tx, userID uuid.UUID, passwordHash string) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE users
		SET password_hash = $1,
		    must_change_password = TRUE
		WHERE id = $2
	`, passwordHash, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return apperrors.ErrUserNotFound
	}
	return nil
}

// RevokeAllSessions revokes every active session of the user. Used by
// credential rotation: a client still logged in with the old credential must
// not retain access after rotation.
func (r *Repository) RevokeAllSessions(ctx context.Context, tx *sql.Tx, userID uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET revoked = true,
		    revoked_at = NOW()
		WHERE user_id = $1
		AND revoked = false
	`, userID)
	if err != nil {
		return err
	}
	return nil
}

// LockClientMembership locks the user's membership row in the organization for
// the duration of the transaction, serializing the removal against concurrent
// role updates/removals. ErrClientNotFound when no row exists (the target
// stopped being a member between resolution and removal).
func (r *Repository) LockClientMembership(ctx context.Context, q Queryer, orgID uuid.UUID, userID uuid.UUID) (uuid.UUID, error) {
	var membershipID uuid.UUID
	err := q.QueryRowContext(ctx, `
		SELECT id
		FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
		FOR UPDATE
	`, orgID, userID).Scan(&membershipID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, apperrors.ErrClientNotFound
		}
		return uuid.Nil, apperrors.ErrDatabase
	}
	return membershipID, nil
}

// ClientHasProjects reports whether the user is the assigned client of any
// project in the organization. A client attached to a project cannot be
// removed directly (the project reference would be stranded).
func (r *Repository) ClientHasProjects(ctx context.Context, q Queryer, orgID uuid.UUID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM projects
			WHERE organization_id = $1
			AND client_id = $2
		)
	`, orgID, userID).Scan(&exists)
	if err != nil {
		return false, apperrors.ErrDatabase
	}
	return exists, nil
}

// DeleteClientMembership deletes the user's membership row in the organization
// (never the users row) and returns the deleted membership id.
func (r *Repository) DeleteClientMembership(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, userID uuid.UUID) (uuid.UUID, error) {
	var membershipID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		DELETE FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
		RETURNING id
	`, orgID, userID).Scan(&membershipID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, apperrors.ErrClientNotFound
		}
		return uuid.Nil, apperrors.ErrDatabase
	}
	return membershipID, nil
}
