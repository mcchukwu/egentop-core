package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"github.com/mcchukwu/egentop/internal/organization"
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

		// Create default organization (slug + owner membership + audit included)
		if _, err := organization.CreateTx(dbCtx, tx, fmt.Sprintf("%s's Organization", req.FirstName), userID, s.audit); err != nil {
			return err
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
			// Unknown identifier and inactive/suspended status both collapse
			// into invalid_credentials so a failed login never reveals whether
			// an account exists (anti-enumeration; registration keeps its own
			// 409s by accepted trade-off).
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrInvalidCredentials
			}

			return apperrors.ErrDatabase
		}

		// Check if user is active
		if status != "active" {
			return apperrors.ErrInvalidCredentials
		}

		// Verify user password
		err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password))
		if err != nil {
			return apperrors.ErrInvalidCredentials
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
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (string, string, error) {
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
		var (
			sessionID   uuid.UUID
			userID      uuid.UUID
			familyID    uuid.UUID
			hashedToken string
			revoked     bool
			expiresAt   time.Time
		)

		// The deterministic fingerprint locates the candidate session by raw
		// token value; the bcrypt hash below remains the authoritative check.
		err = tx.QueryRowContext(dbCtx, `
			SELECT id, user_id, token_family_id, refresh_token_hash, revoked, expires_at
			FROM sessions
			WHERE token_lookup_hash = $1
			FOR UPDATE
		`, lookupHashRefreshToken(refreshToken)).Scan(&sessionID, &userID, &familyID, &hashedToken, &revoked, &expiresAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return apperrors.ErrInvalidToken
			}

			return apperrors.ErrDatabase
		}

		// Defense in depth: the fingerprint only narrows the lookup; the
		// bcrypt hash must still match the presented token.
		err = bcrypt.CompareHashAndPassword([]byte(hashedToken), []byte(refreshToken))
		if err != nil {
			return apperrors.ErrInvalidToken
		}

		// Reuse of a revoked token: the token was stolen, revoke the whole family
		if revoked {
			_, err = tx.ExecContext(dbCtx, `
				UPDATE sessions
				SET revoked = true,
					revoked_at = NOW()
				WHERE token_family_id = $1
				AND revoked = false
			`, familyID)
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

		// Expired refresh tokens are rejected outright.
		if !expiresAt.After(time.Now()) {
			return apperrors.ErrInvalidToken
		}

		// Revoke the old session
		_, err = tx.ExecContext(dbCtx, `
			UPDATE sessions
			SET revoked = true,
				revoked_at = NOW()
			WHERE id = $1
		`, sessionID)
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
			INSERT INTO sessions (user_id, token_family_id, refresh_token_hash, token_lookup_hash, expires_at)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id
		`, userID, familyID, newRefreshTokenHash, lookupHashRefreshToken(newRefreshToken), time.Now().Add(s.cfg.JWTRefreshTokenTTL)).Scan(&newSessionID)
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

// Logout revokes the session identified by the presented refresh token.
// Idempotent: an unknown, revoked, or expired token is a no-op (the user is
// already logged out) and returns nil. No family revocation is triggered —
// logout grants nothing, so replaying a revoked cookie is harmless.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		sessionID, userID, revoked, expiresAt, found, err := resolveSessionForLogout(dbCtx, tx, refreshToken)
		if err != nil {
			return err
		}
		if !found || revoked || !expiresAt.After(time.Now()) {
			return nil // already logged out
		}

		// Guard with revoked=false so a concurrent replay cannot double-revoke
		// or double-audit. ErrNoRows here means the race was lost: no-op.
		err = tx.QueryRowContext(dbCtx, `
			UPDATE sessions
			SET revoked = true,
				revoked_at = NOW()
			WHERE id = $1
			  AND revoked = false
			RETURNING user_id
		`, sessionID).Scan(&userID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return apperrors.ErrDatabase
		}

		return s.audit.Log(dbCtx, tx, audit.LogEntry{
			UserID:     &userID,
			Action:     "user.logged_out",
			EntityType: "user",
			EntityID:   &userID,
			Metadata:   map[string]any{},
		})
	})
}

// LogoutAllDevices revokes every active session of the user identified by
// the presented refresh token. Idempotent when the token is unknown or the
// session is already revoked or expired: returns nil.
func (s *Service) LogoutAllDevices(ctx context.Context, refreshToken string) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	return db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		_, userID, revoked, expiresAt, found, err := resolveSessionForLogout(dbCtx, tx, refreshToken)
		if err != nil {
			return err
		}
		if !found || revoked || !expiresAt.After(time.Now()) {
			return nil // cannot resolve an active session; idempotent no-op
		}

		_, err = tx.ExecContext(dbCtx, `
			UPDATE sessions
			SET revoked = true,
				revoked_at = NOW()
			WHERE user_id = $1
			  AND revoked = false
		`, userID)
		if err != nil {
			return apperrors.ErrDatabase
		}

		return s.audit.Log(dbCtx, tx, audit.LogEntry{
			UserID:     &userID,
			Action:     "user.logged_out_all_devices",
			EntityType: "user",
			EntityID:   &userID,
			Metadata:   map[string]any{},
		})
	})
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

// lookupHashRefreshToken returns a fast, deterministic fingerprint of the
// raw refresh token used for DB lookups. The authoritative stored secret
// remains the bcrypt refresh_token_hash.
func lookupHashRefreshToken(rt string) string {
	sum := sha256.Sum256([]byte(rt))
	return hex.EncodeToString(sum[:])
}

// resolveSessionForLogout locates the session row by the deterministic
// token fingerprint and verifies the presented token against the stored
// bcrypt hash. found=false means the token matches no verifiable session;
// callers treat that as an idempotent no-op (already logged out).
func resolveSessionForLogout(ctx context.Context, tx *sql.Tx, refreshToken string) (sessionID, userID uuid.UUID, revoked bool, expiresAt time.Time, found bool, err error) {
	var hashedToken string
	err = tx.QueryRowContext(ctx, `
		SELECT id, user_id, refresh_token_hash, revoked, expires_at
		FROM sessions
		WHERE token_lookup_hash = $1
		FOR UPDATE
	`, lookupHashRefreshToken(refreshToken)).Scan(&sessionID, &userID, &hashedToken, &revoked, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, uuid.Nil, false, time.Time{}, false, nil
		}
		return uuid.Nil, uuid.Nil, false, time.Time{}, false, apperrors.ErrDatabase
	}
	if bcrypt.CompareHashAndPassword([]byte(hashedToken), []byte(refreshToken)) != nil {
		return uuid.Nil, uuid.Nil, false, time.Time{}, false, nil
	}
	return sessionID, userID, revoked, expiresAt, true, nil
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
        INSERT INTO sessions (user_id, refresh_token_hash, token_lookup_hash, expires_at, revoked, created_at)
        VALUES ($1, $2, $3, $4, false, NOW())
        RETURNING id
    `, userID, string(hashedRefreshToken), lookupHashRefreshToken(refreshToken), time.Now().Add(cfg.JWTRefreshTokenTTL)).Scan(&sessionID)
	if err != nil {
		return "", "", apperrors.ErrDatabase
	}

	accessToken, err = jwt.GenerateAccessToken(userID, sessionID)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}
