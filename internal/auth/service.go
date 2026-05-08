package auth

import (
	"context"
	"database/sql"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	DB *sql.DB
}

func NewAuthService(db *sql.DB) *AuthService {
	return &AuthService{DB: db}
}

func (s *AuthService) Register(ctx context.Context, req RegisterRequest) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer tx.Rollback()

	// Hash password
	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		return err
	}

	// Create user
	var userID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO users (email, phone, password_hash, first_name, last_name)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, req.Email, req.Phone, hashedPassword, req.FirstName, req.LastName).Scan(&userID)
	if err != nil {
		return err
	}

	// Create organization
	var orgID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO organizations (name)
		VALUES ($1)
		RETURNING id
	`, fmt.Sprintf("%s's Organization", req.FirstName)).Scan(&orgID)
	if err != nil {
		return err
	}

	// Create membership (owner)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role, status)
		VALUES ($1, $2, $3, $4)
	`, userID, orgID, "owner", "active")
	if err != nil {
		return err
	}

	// Audit log
	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_logs (organization_id, user_id, action, metadata)
		VALUES ($1, $2, $3, $4)
	`, orgID, userID, "user.registered", `{}`)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// hashPassword hashes the password using bcrypt
func hashPassword(pw string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(bytes), err
}
