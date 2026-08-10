package user

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/config"
	"github.com/mcchukwu/egentop/pkg/db"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db    *sql.DB
	repo  *Repository
	audit *audit.Service
	cfg   *config.Config
}

func NewService(db *sql.DB, repo *Repository, audit *audit.Service, cfg *config.Config) *Service {
	return &Service{
		db:    db,
		repo:  repo,
		audit: audit,
		cfg:   cfg,
	}
}

// GetProfile returns the profile of the authenticated user.
func (s *Service) GetProfile(ctx context.Context, userID uuid.UUID) (*User, error) {
	return s.repo.GetByID(ctx, userID)
}

// UpdateProfile updates the display name of the authenticated user.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, req UpdateProfileRequest) (*User, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	if err := s.repo.UpdateProfile(dbCtx, userID, req.FirstName, req.LastName); err != nil {
		return nil, err
	}

	err := s.audit.Log(dbCtx, nil, audit.LogEntry{
		UserID:     &userID,
		Action:     "user.profile_updated",
		EntityType: "user",
		EntityID:   &userID,
		Metadata:   map[string]any{},
	})
	if err != nil {
		return nil, err
	}

	return s.repo.GetByID(dbCtx, userID)
}

// ChangePassword changes the password for the authenticated user. The user
// must supply their current password; when the configured email-verification
// gate is enabled, accounts with an email are required to have it verified.
// All other sessions are revoked on success.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, currentSessionID uuid.UUID, req ChangePasswordRequest) error {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	hash, err := s.repo.GetPasswordHash(dbCtx, userID)
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*hash), []byte(req.CurrentPassword)); err != nil {
		return apperrors.ErrInvalidPassword
	}

	if req.CurrentPassword == req.NewPassword {
		return apperrors.ErrWeakPassword
	}

	if s.cfg.RequireEmailVerification {
		verified, err := s.repo.GetEmailVerified(dbCtx, userID)
		if err != nil {
			return err
		}

		if !verified {
			return apperrors.ErrEmailNotVerified
		}
	}

	newHash, err := hashPassword(req.NewPassword)
	if err != nil {
		return apperrors.ErrInternalServer
	}

	err = db.WithTransaction(dbCtx, s.db, func(tx *sql.Tx) error {
		if err := s.repo.UpdatePasswordHash(dbCtx, userID, newHash); err != nil {
			return err
		}

		// Revoke every other active session; the current one stays valid.
		if _, err := tx.ExecContext(dbCtx, `
			UPDATE sessions
			SET revoked = true,
			    revoked_at = NOW()
			WHERE user_id = $1
			AND id <> $2
			AND revoked = false
		`, userID, currentSessionID); err != nil {
			return apperrors.ErrDatabase
		}

		return s.audit.Log(dbCtx, tx, audit.LogEntry{
			UserID:     &userID,
			Action:     "user.password_changed",
			EntityType: "user",
			EntityID:   &userID,
			Metadata:   map[string]any{},
		})
	})
	if err != nil {
		return err
	}

	return nil
}

func hashPassword(pw string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(bytes), err
}
