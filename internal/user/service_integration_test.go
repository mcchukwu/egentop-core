package user

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/pkg/config"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("EGTEST_DB_URL")
	if dsn == "" {
		t.Skip("EGTEST_DB_URL not set; skipping integration test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}

	return db
}

func newTestService(db *sql.DB) *Service {
	cfg := &config.Config{}
	return NewService(db, NewRepository(db), audit.NewService(db), cfg)
}

type seededUser struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	Password  string
}

func seedUser(t *testing.T, db *sql.DB, email string, emailVerified bool) seededUser {
	t.Helper()

	password := "current-password-123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	ctx := context.Background()
	var s seededUser
	s.Password = password

	err = db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name, email_verified)
		VALUES ($1, $2, 'First', 'Last', $3)
		RETURNING id
	`, email, string(hash), emailVerified).Scan(&s.UserID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '1 day')
		RETURNING id
	`, s.UserID, "token-hash-"+uuid.NewString()).Scan(&s.SessionID)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	return s
}

func TestGetProfile(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	s := seedUser(t, db, "profile-"+uuid.NewString()+"@example.com", false)

	u, err := svc.GetProfile(ctx, s.UserID)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if u.FirstName != "First" || u.LastName != "Last" {
		t.Fatalf("unexpected profile: %+v", u)
	}
}

func TestUpdateProfile(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	s := seedUser(t, db, "update-"+uuid.NewString()+"@example.com", false)

	u, err := svc.UpdateProfile(ctx, s.UserID, UpdateProfileRequest{
		FirstName: "Jane",
		LastName:  "Smith",
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if u.FirstName != "Jane" || u.LastName != "Smith" {
		t.Fatalf("unexpected updated profile: %+v", u)
	}
}

func TestChangePasswordGates(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	s := seedUser(t, db, "cp-"+uuid.NewString()+"@example.com", true)

	t.Run("wrong current password", func(t *testing.T) {
		err := svc.ChangePassword(ctx, s.UserID, s.SessionID, ChangePasswordRequest{
			CurrentPassword: "wrong-password",
			NewPassword:     "new-password-456",
		})
		if !errors.Is(err, apperrors.ErrInvalidPassword) {
			t.Fatalf("expected ErrInvalidPassword, got %v", err)
		}
	})

	t.Run("same as current password", func(t *testing.T) {
		err := svc.ChangePassword(ctx, s.UserID, s.SessionID, ChangePasswordRequest{
			CurrentPassword: s.Password,
			NewPassword:     s.Password,
		})
		if !errors.Is(err, apperrors.ErrWeakPassword) {
			t.Fatalf("expected ErrWeakPassword, got %v", err)
		}
	})
}

func TestChangePasswordSuccessRevokesOtherSessions(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)
	s := seedUser(t, db, "revoke-"+uuid.NewString()+"@example.com", true)

	// add a second active session for the same user
	_, err := db.ExecContext(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, expires_at)
		VALUES ($1, $2, NOW() + INTERVAL '1 day')
	`, s.UserID, "other-session-"+uuid.NewString())
	if err != nil {
		t.Fatalf("insert second session: %v", err)
	}

	err = svc.ChangePassword(ctx, s.UserID, s.SessionID, ChangePasswordRequest{
		CurrentPassword: s.Password,
		NewPassword:     "new-password-456",
	})
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	var revokedCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions
		WHERE user_id = $1 AND revoked = true
	`, s.UserID).Scan(&revokedCount); err != nil {
		t.Fatalf("count revoked: %v", err)
	}
	if revokedCount != 1 {
		t.Fatalf("expected 1 revoked session (the other one), got %d", revokedCount)
	}

	var currentRevoked bool
	if err := db.QueryRowContext(ctx, `
		SELECT revoked FROM sessions WHERE id = $1
	`, s.SessionID).Scan(&currentRevoked); err != nil {
		t.Fatalf("read current session: %v", err)
	}
	if currentRevoked {
		t.Fatalf("current session should remain valid")
	}
}
