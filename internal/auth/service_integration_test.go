package auth

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/jwt"
	"github.com/mcchukwu/egentop/pkg/config"
	_ "github.com/jackc/pgx/v5/stdlib"
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
	manager := jwt.NewManager("test-secret-0123456789abcdef0123456789abcdef", 15*time.Minute)
	cfg := &config.Config{JWTRefreshTokenTTL: 30 * 24 * time.Hour}

	return NewService(db, audit.NewService(db), manager, cfg)
}

func register(t *testing.T, svc *Service, email string) (string, string) {
	t.Helper()

	access, refresh, err := svc.Register(context.Background(), RegisterRequest{
		Email:     email,
		Password:  "password123",
		FirstName: "John",
		LastName:  "Doe",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if access == "" || refresh == "" {
		t.Fatalf("expected tokens, got access=%q refresh=%q", access, refresh)
	}

	return access, refresh
}

func TestRegisterCreatesUserOrgMembershipSession(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	email := "reg-" + uuid.NewString() + "@example.com"

	register(t, svc, email)

	var userID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	// org membership with owner role
	var role string
	err = db.QueryRowContext(ctx, `
		SELECT r.name
		FROM organizations o
		JOIN memberships m ON m.organization_id = o.id
		JOIN roles r ON r.id = m.role_id
		WHERE m.user_id = $1
	`, userID).Scan(&role)
	if err != nil {
		t.Fatalf("find membership: %v", err)
	}
	if role != "owner" {
		t.Fatalf("expected owner role, got %s", role)
	}

	// one active session
	var activeSessions int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&activeSessions)
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if activeSessions != 1 {
		t.Fatalf("expected 1 active session, got %d", activeSessions)
	}
}

func TestLoginValidatesPassword(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	email := "login-" + uuid.NewString() + "@example.com"

	register(t, svc, email)

	_, _, err := svc.Login(ctx, LoginRequest{Identifier: email, Password: "password123"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	_, _, err = svc.Login(ctx, LoginRequest{Identifier: email, Password: "wrong-password"})
	if !errors.Is(err, apperrors.ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	email := "refresh-" + uuid.NewString() + "@example.com"

	_, refreshToken := register(t, svc, email)

	var userID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	var oldSessionID, familyID uuid.UUID
	err = db.QueryRowContext(ctx, `
		SELECT id, token_family_id
		FROM sessions
		WHERE user_id = $1 AND revoked = false
		LIMIT 1
	`, userID).Scan(&oldSessionID, &familyID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}

	// rotate
	newAccess, newRefresh, err := svc.RefreshToken(ctx, userID, refreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newAccess == "" || newRefresh == "" {
		t.Fatalf("expected new tokens")
	}

	// old session revoked
	var oldRevoked bool
	err = db.QueryRowContext(ctx, `SELECT revoked FROM sessions WHERE id = $1`, oldSessionID).Scan(&oldRevoked)
	if err != nil {
		t.Fatalf("check old session: %v", err)
	}
	if !oldRevoked {
		t.Fatal("expected old session to be revoked")
	}

	// new session exists in the same family with a future expiry
	var newSessionCount int
	var newExpiry time.Time
	err = db.QueryRowContext(ctx, `
		SELECT count(*), max(expires_at)
		FROM sessions
		WHERE user_id = $1 AND token_family_id = $2 AND revoked = false
	`, userID, familyID).Scan(&newSessionCount, &newExpiry)
	if err != nil {
		t.Fatalf("check new session: %v", err)
	}
	if newSessionCount != 1 {
		t.Fatalf("expected 1 active session in family, got %d", newSessionCount)
	}
	if !newExpiry.After(time.Now()) {
		t.Fatalf("expected new session expiry in the future, got %s", newExpiry)
	}

	// reusing the now-revoked token revokes the whole family
	_, _, err = svc.RefreshToken(ctx, userID, refreshToken)
	if !errors.Is(err, apperrors.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked on reuse, got %v", err)
	}

	var familyActive int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE token_family_id = $1 AND revoked = false
	`, familyID).Scan(&familyActive)
	if err != nil {
		t.Fatalf("count family active: %v", err)
	}
	if familyActive != 0 {
		t.Fatalf("expected whole family revoked, got %d active sessions", familyActive)
	}

	// a fresh refresh token from the rotation is also now dead (family revoked)
	_, _, err = svc.RefreshToken(ctx, userID, newRefresh)
	if !errors.Is(err, apperrors.ErrSessionRevoked) {
		t.Fatalf("expected ErrSessionRevoked for rotated token after family revocation, got %v", err)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	email := "logout-" + uuid.NewString() + "@example.com"

	register(t, svc, email)

	var userID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	var sessionID uuid.UUID
	err = db.QueryRowContext(ctx, `
		SELECT id FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&sessionID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}

	if err := svc.Logout(ctx, sessionID); err != nil {
		t.Fatalf("logout: %v", err)
	}

	var revoked bool
	err = db.QueryRowContext(ctx, `SELECT revoked FROM sessions WHERE id = $1`, sessionID).Scan(&revoked)
	if err != nil {
		t.Fatalf("check session: %v", err)
	}
	if !revoked {
		t.Fatal("expected session revoked after logout")
	}
}

func TestLogoutAllDevicesRevokesAllSessions(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	email := "logoutall-" + uuid.NewString() + "@example.com"

	register(t, svc, email)

	// create a second session via login
	_, _, err := svc.Login(ctx, LoginRequest{Identifier: email, Password: "password123"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	var userID uuid.UUID
	err = db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	var activeBefore int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&activeBefore)
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if activeBefore != 2 {
		t.Fatalf("expected 2 active sessions, got %d", activeBefore)
	}

	if err := svc.LogoutAllDevices(ctx, userID); err != nil {
		t.Fatalf("logout all: %v", err)
	}

	var activeAfter int
	err = db.QueryRowContext(ctx, `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&activeAfter)
	if err != nil {
		t.Fatalf("count sessions after: %v", err)
	}
	if activeAfter != 0 {
		t.Fatalf("expected 0 active sessions, got %d", activeAfter)
	}
}

func TestAuthAuditRowsWrittenWithoutOrg(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	email := "audit-" + uuid.NewString() + "@example.com"

	_, refreshToken := register(t, svc, email)

	_, _, err := svc.Login(ctx, LoginRequest{Identifier: email, Password: "password123"})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	var userID uuid.UUID
	err = db.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}

	_, _, err = svc.RefreshToken(ctx, userID, refreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	var sessionID uuid.UUID
	err = db.QueryRowContext(ctx, `
		SELECT id FROM sessions WHERE user_id = $1 AND revoked = false
	`, userID).Scan(&sessionID)
	if err != nil {
		t.Fatalf("find session: %v", err)
	}

	if err := svc.Logout(ctx, sessionID); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// audit rows for user-level events carry no organization
	var orglessCount int
	err = db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM audit_logs
		WHERE user_id = $1 AND organization_id IS NULL
	`, userID).Scan(&orglessCount)
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}

	// user.registered, user.logged_in, token.refreshed, user.logged_out
	if orglessCount != 4 {
		t.Fatalf("expected 4 org-less audit rows, got %d", orglessCount)
	}
}
