package user

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
)

type Repository struct {
	DB *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		DB: db,
	}
}

const userColumns = `
	id,
	email,
	phone,
	first_name,
	last_name,
	status,
	email_verified,
	phone_verified,
	created_at,
	updated_at
`

func scanUser(scan func(dest ...any) error) (*User, error) {
	var u User

	err := scan(&u.ID, &u.Email, &u.Phone, &u.FirstName, &u.LastName, &u.Status, &u.EmailVerified, &u.PhoneVerified, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

// GetByID returns a user's profile by ID.
func (r *Repository) GetByID(ctx context.Context, userID uuid.UUID) (*User, error) {
	u, err := scanUser(func(dest ...any) error {
		return r.DB.QueryRowContext(ctx, `
			SELECT `+userColumns+`
			FROM users
			WHERE id = $1
		`, userID).Scan(dest...)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrUserNotFound
		}

		return nil, apperrors.ErrDatabase
	}

	return u, nil
}

// GetByEmail returns a user's profile by their (case-insensitive) email.
func (r *Repository) GetByEmail(ctx context.Context, email string) (*User, error) {
	u, err := scanUser(func(dest ...any) error {
		return r.DB.QueryRowContext(ctx, `
			SELECT `+userColumns+`
			FROM users
			WHERE email = $1
		`, email).Scan(dest...)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrUserNotFound
		}

		return nil, apperrors.ErrDatabase
	}

	return u, nil
}

// UpdateProfile updates the display name of a user.
func (r *Repository) UpdateProfile(ctx context.Context, userID uuid.UUID, firstName, lastName string) error {
	result, err := r.DB.ExecContext(ctx, `
		UPDATE users
		SET first_name = $1, last_name = $2
		WHERE id = $3
	`, firstName, lastName, userID)
	if err != nil {
		return apperrors.ErrDatabase
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return apperrors.ErrDatabase
	}

	if rows == 0 {
		return apperrors.ErrUserNotFound
	}

	return nil
}

// GetPasswordHash returns the password hash for a user, or nil if not found.
func (r *Repository) GetPasswordHash(ctx context.Context, userID uuid.UUID) (*string, error) {
	var hash string

	err := r.DB.QueryRowContext(ctx, `
		SELECT password_hash
		FROM users
		WHERE id = $1
	`, userID).Scan(&hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrUserNotFound
		}

		return nil, apperrors.ErrDatabase
	}

	return &hash, nil
}

// GetEmailVerified returns the email_verified flag for a user.
func (r *Repository) GetEmailVerified(ctx context.Context, userID uuid.UUID) (bool, error) {
	var verified bool

	err := r.DB.QueryRowContext(ctx, `
		SELECT email_verified
		FROM users
		WHERE id = $1
	`, userID).Scan(&verified)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, apperrors.ErrUserNotFound
		}

		return false, apperrors.ErrDatabase
	}

	return verified, nil
}

// UpdatePasswordHash sets a new password hash for a user.
func (r *Repository) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, hash string) error {
	result, err := r.DB.ExecContext(ctx, `
		UPDATE users
		SET password_hash = $1
		WHERE id = $2
	`, hash, userID)
	if err != nil {
		return apperrors.ErrDatabase
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return apperrors.ErrDatabase
	}

	if rows == 0 {
		return apperrors.ErrUserNotFound
	}

	return nil
}
