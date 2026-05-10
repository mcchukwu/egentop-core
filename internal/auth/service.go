package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	DB        *sql.DB
	JWTSecret []byte
}

func NewAuthService(db *sql.DB, secret []byte) *AuthService {
	return &AuthService{
		DB:        db,
		JWTSecret: secret,
	}
}

// Register creates a new user account
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

// Login validates the user credentials and returns a JWT access token
func (s *AuthService) Login(ctx context.Context, req LoginRequest) (string, string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}

	defer tx.Rollback()

	var userID string
	var passwordHash string

	// Find user
	err = tx.QueryRowContext(ctx, `
		SELECT id, password_hash
		FROM users
		WHERE email = $1 OR phone = $1
	`, req.Identifier).Scan(&userID, &passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", errors.New("user not found")
		}
		return "", "", err
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password))
	if err != nil {
		return "", "", errors.New("invalid password")
	}

	// Generate refresh token
	refreshTokenBytes := make([]byte, 32)
	_, err = rand.Read(refreshTokenBytes)
	if err != nil {
		return "", "", err
	}

	refreshToken := hex.EncodeToString(refreshTokenBytes)

	hashedRefreshToken, err := hashRefreshToken(refreshToken)
	if err != nil {
		return "", "", err
	}

	// Create sessiong and Store session
	var sessionID string

	err = tx.QueryRowContext(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, expires_at, revoked, created_at)
		VALUES ($1, $2, $3, false, NOW())
		RETURNING id
	`,
		userID,
		hashedRefreshToken,
		time.Now().Add(30*24*time.Hour),
	).Scan(&sessionID)
	if err != nil {
		return "", "", err
	}

	// Create JWT access token
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":    userID,
		"session_id": sessionID,
		"exp":        time.Now().Add(15 * time.Minute).Unix(),
	})

	// Sign JWT
	tokenString, err := accessToken.SignedString(s.JWTSecret)
	if err != nil {
		return "", "", err
	}

	// Audit Log
	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_logs (user_id, action, metadata)
		VALUES ($1, $2, $3)
	`, userID, "user.login", `{}`)
	if err != nil {
		return "", "", err
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		return "", "", err
	}

	return tokenString, refreshToken, nil
}

// RefreshToken refreshes the session
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (string, string, error) {
	// Find session
	rows, err := s.DB.QueryContext(ctx, `
			SELECT id, refresh_token_hash, user_id
			FROM sessions
			WHERE revoked = false
	`)
	if err != nil {
		return "", "", err
	}

	// Loop through sessions
	defer rows.Close()

	var sessionID string
	var hashedRefreshToken string
	var userID string
	var found bool

	for rows.Next() {
		err = rows.Scan(&sessionID, &hashedRefreshToken, &userID)
		if err != nil {
			return "", "", err
		}

		err = bcrypt.CompareHashAndPassword([]byte(hashedRefreshToken), []byte(refreshToken))
		if err == nil {
			found = true
			break
		}
	}

	if !found {
		return "", "", errors.New("invalid refresh token")
	}

	// Revoke old session
	_, err = s.DB.ExecContext(ctx, `
		UPDATE sessions
		SET revoked = true,
	    revoked_at = NOW()
		WHERE id = $1
	`,
		sessionID)

	// Create new refresh token
	refreshBytes := make([]byte, 32)

	_, err = rand.Read(refreshBytes)
	if err != nil {
		return "", "", err
	}

	newRefreshToken := hex.EncodeToString(refreshBytes)

	// Hash new refresh token
	newRefreshTokenHash, err := hashRefreshToken(newRefreshToken)
	if err != nil {
		return "", "", err
	}

	// Create new session
	var newSessionID string
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id
`,
		userID,
		newRefreshTokenHash,
		time.Now().Add(30*24*time.Hour),
	).Scan(&newSessionID)

	// Issue new JWT access token
	newAccessToken := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		jwt.MapClaims{
			"user_id":    userID,
			"session_id": newSessionID,
			"exp": time.Now().
				Add(15 * time.Minute).
				Unix(),
		},
	)

	// Sign access token
	newTokenString, err := newAccessToken.SignedString(
		s.JWTSecret,
	)

	return newTokenString, newRefreshToken, nil
}

// LogoutAllDevices revokes all sessions for a user
func (s *AuthService) LogoutAllDevices(ctx context.Context, userID string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE sessions
		SET revoked = true,
		    revoked_at = NOW()
		WHERE user_id = $1
		AND revoked = false
		`,
		userID,
	)

	return err
}

// hashPassword hashes the password using bcrypt
func hashPassword(pw string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(bytes), err
}

// hashRefreshToken hashes the refresh token using bcrypt
func hashRefreshToken(rt string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(rt), 12)
	return string(bytes), err
}
