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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/jwt"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/pkg/config"
	"github.com/mcchukwu/egentop/pkg/db"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db    *sql.DB
	audit *audit.Service
	jwt   *jwt.Manager
	cfg   *config.Config
}

func NewService(db *sql.DB, audit *audit.Service, jwt *jwt.Manager, cfg *config.Config) *Service {
	return &Service{
		db:    db,
		audit: audit,
		jwt:   jwt,
		cfg:   cfg,
	}
}

// Register() creates a new user account and returns an active session
func (s *Service) Register(ctx context.Context, req RegisterRequest) (string, string, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var (
		accessToken, refreshToken string
		err                       error
	)

	// Validate identifier
	if req.Email == "" && req.Phone == "" {
		return "", "", apperrors.ErrUserIdentifierInvalid
	}

	// Hash password
	hashedPassword, err := hashPassword(req.Password)
	if err != nil {
		return "", "", apperrors.ErrInternalServer
	}

	// Start transaction
	err = db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		var userID uuid.UUID

		// Create user and get the new user ID
		err = tx.QueryRowContext(dbCtx, `
		INSERT INTO users (email, phone, password_hash, first_name, last_name)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, nullableString(req.Email), nullableString(req.Phone), string(hashedPassword), nullableString(req.FirstName), nullableString(req.LastName)).Scan(&userID)
		if err != nil {
			var pqErr *pgconn.PgError
			if errors.As(err, &pqErr) && pqErr.Code == "23505" {
				switch {
				case strings.Contains(pqErr.ConstraintName, "email"):
					return apperrors.ErrEmailAlreadyExists
				case strings.Contains(pqErr.ConstraintName, "phone"):
					return apperrors.ErrPhoneAlreadyExists
				}
			}

			return apperrors.ErrDatabase
		}

		// Create organization
		// TODO: Create random default slug upon organization creation
		var orgID uuid.UUID

		err = tx.QueryRowContext(dbCtx, `
		INSERT INTO organizations (name)
		VALUES ($1)
		RETURNING id
	`, fmt.Sprintf("%s's Organization", req.FirstName)).Scan(&orgID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Create membership (owner)
		ownerRoleID, err := membership.ResolveSystemRoleID(dbCtx, tx, membership.RoleOwner)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(dbCtx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, $4)
	`, userID, orgID, ownerRoleID, membership.MembershipStatusActive)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Create session (auto-login)
		accessToken, refreshToken, err = createSession(dbCtx, tx, userID, s.jwt, s.cfg)
		if err != nil {
			return err
		}

		// Audit log
		err = s.audit.Log(dbCtx, tx, audit.LogEntry{
			UserID:     &userID,
			Action:     "user.registered",
			EntityType: "user",
			EntityID:   &userID,
			Metadata:   map[string]any{},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return accessToken, refreshToken, err
}

// Login() validates the user credentials and returns a JWT access token
func (s *Service) Login(ctx context.Context, req LoginRequest) (string, string, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var (
		accessToken, refreshToken string
		err                       error
	)

	err = db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		// detect if identifier is email or phone, query the right column and scan into userID, passwordHash, and status
		var (
			userID       uuid.UUID
			passwordHash string
			status       string
		)

		if strings.Contains(req.Identifier, "@") {
			err = tx.QueryRowContext(dbCtx, `
                SELECT id, password_hash, status 
								FROM users 
								WHERE email = $1
            `, req.Identifier).Scan(&userID, &passwordHash, &status)
		} else {
			err = tx.QueryRowContext(dbCtx, `
                SELECT id, password_hash, status 
								FROM users 
								WHERE phone = $1
            `, req.Identifier).Scan(&userID, &passwordHash, &status)
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrUserNotFound
			}

			return apperrors.ErrDatabase
		}

		// Check if user is active
		if status != "active" {
			return apperrors.ErrUserSuspended
		}

		// Verify user password
		err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password))
		if err != nil {
			return apperrors.ErrInvalidPassword
		}

		// Create session
		accessToken, refreshToken, err = createSession(dbCtx, tx, userID, s.jwt, s.cfg)
		if err != nil {
			return err
		}

		// Audit Log
		err = s.audit.Log(dbCtx, tx, audit.LogEntry{
			UserID:     &userID,
			Action:     "user.logged_in",
			EntityType: "user",
			EntityID:   &userID,
			Metadata:   map[string]any{},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return accessToken, refreshToken, err
}

// RefreshToken rotates a refresh token within its session lineage. Reusing a
// revoked token is treated as a theft signal and revokes the whole family.
func (s *Service) RefreshToken(ctx context.Context, userID uuid.UUID, refreshToken string) (string, string, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var (
		newAccessToken  string
		newRefreshToken string

		err error
	)

	// reuseDetected is set when a revoked token is presented; the family
	// revocation must be committed even though the operation fails.
	var reuseDetected bool

	err = db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(dbCtx, `
			SELECT id, token_family_id, refresh_token_hash, revoked
			FROM sessions
			WHERE user_id = $1
		`, userID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		var (
			sessionID       uuid.UUID
			familyID        uuid.UUID
			hashedToken     string
			revoked         bool
			foundSessionID  uuid.UUID
			foundFamilyID   uuid.UUID
			foundRevoked    bool
			hasMatch        bool
		)

		for rows.Next() {
			err = rows.Scan(&sessionID, &familyID, &hashedToken, &revoked)
			if err != nil {
				return apperrors.ErrInternalServer
			}

			err = bcrypt.CompareHashAndPassword([]byte(hashedToken), []byte(refreshToken))
			if err == nil {
				foundSessionID = sessionID
				foundFamilyID = familyID
				foundRevoked = revoked
				hasMatch = true
				break
			}
		}
		if rows.Err() != nil {
			rows.Close()
			return apperrors.ErrDatabase
		}

		rows.Close()

		if !hasMatch {
			return apperrors.ErrInvalidToken
		}

		// Reuse of a revoked token: the token was stolen, revoke the whole family
		if foundRevoked {
			_, err = tx.ExecContext(dbCtx, `
				UPDATE sessions
				SET revoked = true,
					revoked_at = NOW()
				WHERE token_family_id = $1
				AND revoked = false
			`, foundFamilyID)
			if err != nil {
				return apperrors.ErrDatabase
			}

			err = s.audit.Log(dbCtx, tx, audit.LogEntry{
				UserID:   &userID,
				Action:   "session.family_revoked",
				Metadata: map[string]any{"reason": "revoked token reuse detected"},
			})
			if err != nil {
				return err
			}

			reuseDetected = true
			return nil
		}

		// Revoke the old session
		_, err = tx.ExecContext(dbCtx, `
			UPDATE sessions
			SET revoked = true,
				revoked_at = NOW()
			WHERE id = $1
		`, foundSessionID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Create new refresh token
		refreshBytes := make([]byte, 32)

		_, err = rand.Read(refreshBytes)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		newRefreshToken = hex.EncodeToString(refreshBytes)

		newRefreshTokenHash, err := hashRefreshToken(newRefreshToken)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		// Create new session in the same lineage
		var newSessionID uuid.UUID

		err = tx.QueryRowContext(dbCtx, `
			INSERT INTO sessions (user_id, token_family_id, refresh_token_hash, expires_at)
			VALUES ($1, $2, $3, $4)
			RETURNING id
		`, userID, foundFamilyID, newRefreshTokenHash, time.Now().Add(s.cfg.JWTRefreshTokenTTL)).Scan(&newSessionID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		// Sign access token
		newAccessToken, err = s.jwt.GenerateAccessToken(userID, newSessionID)

		// Audit Log
		err = s.audit.Log(dbCtx, tx, audit.LogEntry{
			UserID:   &userID,
			Action:   "token.refreshed",
			Metadata: map[string]any{},
		})
		if err != nil {
			return err
		}

		return nil
	})

	if reuseDetected {
		return "", "", apperrors.ErrSessionRevoked
	}

	return newAccessToken, newRefreshToken, err
}

// Logout revokes sessions for a user's device
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		var userID uuid.UUID

		// Revoke session
		err := tx.QueryRowContext(dbCtx, `
			UPDATE sessions
			SET revoked = true,
				revoked_at = NOW()
			WHERE id = $1
			RETURNING user_id
		`, sessionID).Scan(&userID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrSessionRevoked
			}

			return apperrors.ErrDatabase
		}

		// Audit Log
		err = s.audit.Log(dbCtx, tx, audit.LogEntry{
			UserID:     &userID,
			Action:     "user.logged_out",
			EntityType: "user",
			EntityID:   &userID,
			Metadata:   map[string]any{},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

// LogoutAllDevices revokes all sessions for a user
func (s *Service) LogoutAllDevices(ctx context.Context, userID uuid.UUID) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	err := db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
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
			return apperrors.ErrDatabase
		}

		// Audit Log
		err = s.audit.Log(dbCtx, tx, audit.LogEntry{
			UserID:     &userID,
			Action:     "user.logged_out_all_devices",
			EntityType: "user",
			EntityID:   &userID,
			Metadata:   map[string]any{},
		})
		if err != nil {
			return err
		}

		return nil
	})

	return err
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

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

// nullableString returns a pointer to the string if it's not empty
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// createSession() creates a session in the database
func createSession(ctx context.Context, tx *sql.Tx, userID uuid.UUID, jwt *jwt.Manager, cfg *config.Config) (accessToken, refreshToken string, err error) {
	refreshTokenBytes := make([]byte, 32)

	if _, err = rand.Read(refreshTokenBytes); err != nil {
		return "", "", apperrors.ErrInternalServer
	}

	refreshToken = hex.EncodeToString(refreshTokenBytes)

	hashedRefreshToken, err := bcrypt.GenerateFromPassword([]byte(refreshToken), bcrypt.DefaultCost)
	if err != nil {
		return "", "", apperrors.ErrInternalServer
	}

	var sessionID uuid.UUID

	err = tx.QueryRowContext(ctx, `
        INSERT INTO sessions (user_id, refresh_token_hash, expires_at, revoked, created_at)
        VALUES ($1, $2, $3, false, NOW())
        RETURNING id
    `, userID, string(hashedRefreshToken), time.Now().Add(cfg.JWTRefreshTokenTTL)).Scan(&sessionID)
	if err != nil {
		return "", "", apperrors.ErrDatabase
	}

	accessToken, err = jwt.GenerateAccessToken(userID, sessionID)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}
