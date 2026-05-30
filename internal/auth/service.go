package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/db"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	DB           *sql.DB
	JWTSecret    []byte
	AuditService *audit.AuditService
}

func NewAuthService(db *sql.DB, secret []byte) *AuthService {
	return &AuthService{
		DB:        db,
		JWTSecret: secret,
	}
}

// Register creates a new user account
func (s *AuthService) Register(ctx context.Context, req RegisterRequest) error {
	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		// Hash password
		hashedPassword, err := hashPassword(req.Password)
		if err != nil {
			return err
		}

		// Create user
		var userID string
		err = tx.QueryRowContext(dbCtx, `
		INSERT INTO users (email, phone, password_hash, first_name, last_name)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, req.Email, req.Phone, hashedPassword, req.FirstName, req.LastName).Scan(&userID)
		if err != nil {
			if strings.Contains(err.Error(), "users_email_key") {
				return apperrors.ErrEmailAlreadyExists
			}

			return apperrors.ErrInternalServer
		}

		// Create organization
		var orgID string
		err = tx.QueryRowContext(dbCtx, `
		INSERT INTO organizations (name)
		VALUES ($1)
		RETURNING id
	`, fmt.Sprintf("%s's Organization", req.FirstName)).Scan(&orgID)
		if err != nil {
			if strings.Contains(err.Error(), "organizations_name_key") {
				return apperrors.ErrOrganizationSlugExists
			}
			return apperrors.ErrInternalServer
		}

		// Create membership (owner)
		_, err = tx.ExecContext(dbCtx, `
		INSERT INTO memberships (user_id, organization_id, role, status)
		VALUES ($1, $2, $3, $4)
	`, userID, orgID, "owner", "active")
		if err != nil {
			if strings.Contains(err.Error(), "memberships_user_id_organization_id_key") {
				return apperrors.ErrAlreadyMember
			}
			return apperrors.ErrInternalServer
		}

		// Audit log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &userID,
			Action:         "user.registered",
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

// Login validates the user credentials and returns a JWT access token
func (s *AuthService) Login(ctx context.Context, req LoginRequest) (string, string, error) {
	var accessToken string
	var refreshToken string

	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		var userID string
		var passwordHash string

		// Find user
		err := tx.QueryRowContext(dbCtx, `
		SELECT id, password_hash
		FROM users
		WHERE email = $1 OR phone = $1
		`, req.Identifier).Scan(&userID, &passwordHash)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("user not found")
			}
			return apperrors.ErrInternalServer
		}

		// Verify password
		err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password))
		if err != nil {
			return apperrors.ErrInvalidPassword
		}

		// Generate refresh token
		refreshTokenBytes := make([]byte, 32)
		_, err = rand.Read(refreshTokenBytes)
		if err != nil {
			return err
		}

		refreshToken = hex.EncodeToString(refreshTokenBytes)

		hashedRefreshToken, err := hashRefreshToken(refreshToken)
		if err != nil {
			return err
		}

		// Create session and Store session
		var sessionID string

		err = tx.QueryRowContext(dbCtx, `
		INSERT INTO sessions (user_id, refresh_token_hash, expires_at, revoked, created_at)
		VALUES ($1, $2, $3, false, NOW())
		RETURNING id
	`,
			userID,
			hashedRefreshToken,
			time.Now().Add(30*24*time.Hour),
		).Scan(&sessionID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Create JWT access token
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id":    userID,
			"session_id": sessionID,
			"exp":        time.Now().Add(15 * time.Minute).Unix(),
		})

		// Sign JWT
		accessToken, err = token.SignedString(s.JWTSecret)
		if err != nil {
			return err
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			UserID:   &userID,
			Action:   "user.logged_in",
			Metadata: map[string]any{},
		})
		if err != nil {
			return apperrors.ErrInternalServer
		}

		return nil
	})

	if err != nil {
		return "", "", apperrors.ErrInternalServer
	}

	return accessToken, refreshToken, nil
}

// RefreshToken refreshes the session
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (string, string, error) {
	var newAccessToken string
	var newRefreshToken string

	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		// Find session
		rows, err := tx.QueryContext(dbCtx, `
			SELECT id, refresh_token_hash, user_id
			FROM sessions
			WHERE revoked = false
	`)
		if err != nil {
			return apperrors.ErrInternalServer
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
				return apperrors.ErrInternalServer
			}

			err = bcrypt.CompareHashAndPassword([]byte(hashedRefreshToken), []byte(refreshToken))
			if err == nil {
				found = true
				break
			}
		}

		if !found {
			return apperrors.ErrInvalidToken
		}

		// Revoke old session
		_, err = tx.ExecContext(dbCtx, `
		UPDATE sessions
		SET revoked = true,
	    revoked_at = NOW()
		WHERE id = $1
	`,
			sessionID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Create new refresh token
		refreshBytes := make([]byte, 32)

		_, err = rand.Read(refreshBytes)
		if err != nil {
			return err
		}

		newRefreshToken = hex.EncodeToString(refreshBytes)

		// Hash new refresh token
		newRefreshTokenHash, err := hashRefreshToken(newRefreshToken)
		if err != nil {
			return err
		}

		// Create new session
		var newSessionID string
		err = tx.QueryRowContext(dbCtx, `
		INSERT INTO sessions (user_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id
`,
			userID,
			newRefreshTokenHash,
			time.Now().Add(30*24*time.Hour),
		).Scan(&newSessionID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Issue new JWT access token
		newToken := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"user_id":    userID,
			"session_id": newSessionID,
			"exp":        time.Now().Add(15 * time.Minute).Unix(),
		})

		// Sign access token
		newAccessToken, err = newToken.SignedString(s.JWTSecret)
		if err != nil {
			return err
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			UserID:   &userID,
			Action:   "token.refreshed",
			Metadata: map[string]any{},
		})
		if err != nil {
			return apperrors.ErrInternalServer
		}

		return nil
	})
	if err != nil {
		return "", "", apperrors.ErrInternalServer
	}

	return newAccessToken, newRefreshToken, nil
}

// Logout revokes sessions for a user's device
func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		// Revoke session
		var userID string
		_, err := tx.ExecContext(dbCtx, `
		UPDATE sessions
		SET revoked = true,
	    revoked_at = NOW()
		WHERE id = $1
		RETURNING user_id
	`,
			sessionID)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			UserID:   &userID,
			Action:   "user.logged_out",
			Metadata: map[string]any{},
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

// LogoutAllDevices revokes all sessions for a user
func (s *AuthService) LogoutAllDevices(ctx context.Context, userID string) error {
	err := db.WithTransaction(ctx, s.DB, func(tx *sql.Tx) error {
		dbCtx, cancel := db.WithDBTimeout(ctx)
		defer cancel()

		_, err := tx.ExecContext(dbCtx, `
		UPDATE sessions
		SET revoked = true,
		    revoked_at = NOW()
		WHERE user_id = $1
		AND revoked = false
		`,
			userID,
		)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Audit Log
		err = s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			UserID:   &userID,
			Action:   "user.logged_out_all_devices",
			Metadata: map[string]any{},
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
