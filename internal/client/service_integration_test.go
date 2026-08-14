package client

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/auth"
	"github.com/mcchukwu/egentop/internal/jwt"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/user"
	"github.com/mcchukwu/egentop/pkg/config"
	"github.com/mcchukwu/egentop/pkg/pagination"
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
	return NewService(db, NewRepository(db), audit.NewService(db), activity.NewService(activity.NewRepository(db)))
}

// seedClientOrg creates an org with an owner membership (staff side) and
// returns the staff actor id and org id.
func seedClientOrg(t *testing.T, db *sql.DB) (staffID, orgID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Staff', 'User')
		RETURNING id
	`, "staff-"+uuid.NewString()+"@example.com").Scan(&staffID); err != nil {
		t.Fatalf("insert staff: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		INSERT INTO organizations (name)
		VALUES ($1)
		RETURNING id
	`, "Client Org "+uuid.NewString()).Scan(&orgID); err != nil {
		t.Fatalf("insert org: %v", err)
	}

	var ownerRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'owner' AND organization_id IS NULL
	`).Scan(&ownerRoleID); err != nil {
		t.Fatalf("owner role: %v", err)
	}

	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, staffID, orgID, ownerRoleID); err != nil {
		t.Fatalf("staff membership: %v", err)
	}

	return staffID, orgID
}

// TestProvisionCreatesClientWithOneTimeCredential covers the full lifecycle:
// provision -> user created with must_change_password=true and a bcrypt hash
// matching the returned credential -> login with the credential succeeds ->
// password change clears the flag -> re-provisioning the same email conflicts.
func TestProvisionCreatesClientWithOneTimeCredential(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)
	email := "client-" + uuid.NewString() + "@example.com"

	result, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     email,
		FirstName: "Client",
		LastName:  "User",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if !result.CredentialIssued {
		t.Fatal("expected credential_issued = true for a new user")
	}
	if result.OneTimePassword == "" {
		t.Fatal("expected a one-time password for a new user")
	}
	if result.Email == nil || *result.Email != email {
		t.Fatalf("email = %v, want %q", result.Email, email)
	}

	// The user must exist with the flag set and the credential bcrypt-hashed.
	var mustChange bool
	var passwordHash string
	var roleName string
	if err := db.QueryRowContext(ctx, `
		SELECT u.must_change_password, u.password_hash, r.name
		FROM users u
		JOIN memberships m ON m.user_id = u.id
		JOIN roles r ON r.id = m.role_id
		WHERE u.id = $1 AND m.organization_id = $2
	`, result.ClientID, orgID).Scan(&mustChange, &passwordHash, &roleName); err != nil {
		t.Fatalf("read client row: %v", err)
	}
	if !mustChange {
		t.Fatal("must_change_password should be true for a provisioned credential")
	}
	if roleName != "client" {
		t.Fatalf("role = %q, want client", roleName)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(result.OneTimePassword)); err != nil {
		t.Fatalf("stored hash does not match the one-time credential: %v", err)
	}

	// The provisioned client must be able to log in with the credential.
	cfg := &config.Config{
		JWTSecret:          "0123456789abcdef0123456789abcdef",
		JWTAccessTokenTTL:  15 * time.Minute,
		JWTRefreshTokenTTL: 720 * time.Hour,
	}
	authService := auth.NewService(db, audit.NewService(db), jwt.NewManager(cfg.JWTSecret, cfg.JWTAccessTokenTTL), cfg)
	if _, _, err := authService.Login(ctx, auth.LoginRequest{
		Identifier: email,
		Password:   result.OneTimePassword,
	}); err != nil {
		t.Fatalf("login with one-time credential: %v", err)
	}

	// Changing the password clears the flag (must_change_password -> false).
	userRepo := user.NewRepository(db)
	userService := user.NewService(db, userRepo, audit.NewService(db), cfg)
	if err := userService.ChangePassword(ctx, result.ClientID, uuid.New(), user.ChangePasswordRequest{
		CurrentPassword: result.OneTimePassword,
		NewPassword:     "a-brand-new-password-123",
	}); err != nil {
		t.Fatalf("ChangePassword with one-time credential: %v", err)
	}

	if err := db.QueryRowContext(ctx, `
		SELECT must_change_password FROM users WHERE id = $1
	`, result.ClientID).Scan(&mustChange); err != nil {
		t.Fatalf("re-read flag: %v", err)
	}
	if mustChange {
		t.Fatal("must_change_password should be false after password change")
	}

	// The old credential no longer works; the new one does.
	if _, _, err := authService.Login(ctx, auth.LoginRequest{Identifier: email, Password: result.OneTimePassword}); err == nil {
		t.Fatal("login with rotated-away credential should fail")
	}
	if _, _, err := authService.Login(ctx, auth.LoginRequest{Identifier: email, Password: "a-brand-new-password-123"}); err != nil {
		t.Fatalf("login with new password: %v", err)
	}

	// Provisioning the same email again conflicts (already a member).
	_, err = svc.Provision(ctx, staffID, orgID, ProvisionRequest{Email: email, FirstName: "X", LastName: "Y"})
	if !errors.Is(err, apperrors.ErrAlreadyMember) {
		t.Fatalf("re-provision error = %v, want ErrAlreadyMember", err)
	}
}

// TestProvisionReusesExistingUser: an existing user who is not yet a member of
// the org is reused — active client membership created, NO credential issued,
// must_change_password stays false.
func TestProvisionReusesExistingUser(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)

	existingEmail := "existing-" + uuid.NewString() + "@example.com"
	var existingID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Existing', 'User')
		RETURNING id
	`, existingEmail).Scan(&existingID); err != nil {
		t.Fatalf("insert existing user: %v", err)
	}

	result, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     existingEmail,
		FirstName: "Ignored",
		LastName:  "Names",
	})
	if err != nil {
		t.Fatalf("Provision reuse: %v", err)
	}

	if result.ClientID != existingID {
		t.Fatalf("client_id = %v, want reused user %v", result.ClientID, existingID)
	}
	if result.CredentialIssued {
		t.Fatal("credential_issued should be false for an existing user")
	}
	if result.OneTimePassword != "" {
		t.Fatal("no one-time password should be returned for an existing user")
	}

	var mustChange bool
	if err := db.QueryRowContext(ctx, `
		SELECT u.must_change_password
		FROM users u
		JOIN memberships m ON m.user_id = u.id
		WHERE u.id = $1 AND m.organization_id = $2
	`, existingID, orgID).Scan(&mustChange); err != nil {
		t.Fatalf("read flag: %v", err)
	}
	if mustChange {
		t.Fatal("must_change_password should stay false when reusing an existing user")
	}
}

// TestProvisionWithPhoneAndListCovers phone-based provisioning and the client
// list excluding staff memberships.
func TestProvisionWithPhoneAndList(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)

	phone := "+2348" + uuid.NewString()[0:9]
	result, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Phone:     phone,
		FirstName: "Phone",
		LastName:  "Client",
	})
	if err != nil {
		t.Fatalf("Provision by phone: %v", err)
	}
	if !result.CredentialIssued {
		t.Fatal("expected credential for a phone-only new user")
	}
	if result.Phone == nil || *result.Phone != phone {
		t.Fatalf("phone = %v, want %q", result.Phone, phone)
	}

	// A second client so the list has multiple entries.
	if _, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     "list-client-" + uuid.NewString() + "@example.com",
		FirstName: "List",
		LastName:  "Client",
	}); err != nil {
		t.Fatalf("Provision second client: %v", err)
	}

	clients, meta, err := svc.List(ctx, orgID, pagination.Query{Page: 1, Limit: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if meta.Total != 2 {
		t.Fatalf("client list total = %d, want 2 (staff membership must be excluded)", meta.Total)
	}
	for _, c := range clients {
		if c.UserID == staffID {
			t.Fatal("staff member must not appear in the client list")
		}
	}
}

// TestProvisionNeverCreatesOrganization: provisioning must not create a
// default org for the client (unlike registration).
func TestProvisionNeverCreatesOrganization(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)

	result, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     "no-org-" + uuid.NewString() + "@example.com",
		FirstName: "No",
		LastName:  "Org",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1
	`, result.ClientID).Scan(&membershipCount); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("client has %d memberships, want exactly 1 (the client membership; no default org)", membershipCount)
	}

	var orgCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM organizations o
		JOIN memberships m ON m.organization_id = o.id
		WHERE m.user_id = $1 AND o.id <> $2
	`, result.ClientID, orgID).Scan(&orgCount); err != nil {
		t.Fatalf("count orgs: %v", err)
	}
	if orgCount != 0 {
		t.Fatalf("provisioning created %d unexpected organizations", orgCount)
	}
}

// ensure membership package role constant is exercised (guards against drift).
var _ = membership.RoleClient

func newTestAuthService(db *sql.DB) *auth.Service {
	cfg := &config.Config{
		JWTSecret:          "0123456789abcdef0123456789abcdef",
		JWTAccessTokenTTL:  15 * time.Minute,
		JWTRefreshTokenTTL: 720 * time.Hour,
	}
	return auth.NewService(db, audit.NewService(db), jwt.NewManager(cfg.JWTSecret, cfg.JWTAccessTokenTTL), cfg)
}

func countClientSessions(t *testing.T, db *sql.DB, userID uuid.UUID, revoked bool) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked = $2
	`, userID, revoked).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return count
}

// TestResetCredentialRotatesAndRevokesSessions covers the security-critical
// credential-rotation path: old password stops working, new credential works
// and forces a change, ALL of the client's sessions are revoked, and the
// rotation is audited.
func TestResetCredentialRotatesAndRevokesSessions(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)
	email := "rotate-" + uuid.NewString() + "@example.com"

	provisioned, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
		Email:     email,
		FirstName: "Rotate",
		LastName:  "Me",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	authService := newTestAuthService(db)

	// The client logs in with the original credential -> active session.
	if _, _, err := authService.Login(ctx, auth.LoginRequest{Identifier: email, Password: provisioned.OneTimePassword}); err != nil {
		t.Fatalf("login before rotation: %v", err)
	}
	if got := countClientSessions(t, db, provisioned.ClientID, false); got != 1 {
		t.Fatalf("active sessions before rotation = %d, want 1", got)
	}

	// Rotate.
	rotated, err := svc.ResetCredential(ctx, staffID, orgID, provisioned.ClientID)
	if err != nil {
		t.Fatalf("ResetCredential: %v", err)
	}
	if !rotated.CredentialIssued || rotated.OneTimePassword == "" {
		t.Fatalf("rotation result = %+v, want issued credential", rotated)
	}
	if rotated.ClientID != provisioned.ClientID {
		t.Fatalf("rotated client id = %v, want %v", rotated.ClientID, provisioned.ClientID)
	}

	// ALL sessions that existed before the rotation are revoked: a client
	// still logged in with the old credential must not retain access.
	if got := countClientSessions(t, db, provisioned.ClientID, false); got != 0 {
		t.Fatalf("active sessions after rotation = %d, want 0", got)
	}

	// The old credential no longer logs in.
	if _, _, err := authService.Login(ctx, auth.LoginRequest{Identifier: email, Password: provisioned.OneTimePassword}); err == nil {
		t.Fatal("login with rotated-away credential should fail")
	}

	// The new credential works and re-arms the forced-change gate.
	if _, _, err := authService.Login(ctx, auth.LoginRequest{Identifier: email, Password: rotated.OneTimePassword}); err != nil {
		t.Fatalf("login with rotated credential: %v", err)
	}
	var mustChange bool
	if err := db.QueryRowContext(ctx, `
		SELECT must_change_password FROM users WHERE id = $1
	`, provisioned.ClientID).Scan(&mustChange); err != nil {
		t.Fatalf("read flag: %v", err)
	}
	if !mustChange {
		t.Fatal("must_change_password should be true after rotation")
	}

	// The stored hash matches the new credential.
	var passwordHash string
	if err := db.QueryRowContext(ctx, `
		SELECT password_hash FROM users WHERE id = $1
	`, provisioned.ClientID).Scan(&passwordHash); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(rotated.OneTimePassword)); err != nil {
		t.Fatalf("stored hash does not match rotated credential: %v", err)
	}

	// The fresh login after rotation created exactly one new active session.
	if got := countClientSessions(t, db, provisioned.ClientID, false); got != 1 {
		t.Fatalf("active sessions after fresh login = %d, want 1", got)
	}

	// The rotation is audited.
	var auditCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM audit_logs
		WHERE action = 'client.credential_rotated' AND entity_id = $1 AND entity_type = 'client'
	`, provisioned.ClientID).Scan(&auditCount); err != nil {
		t.Fatalf("count rotation audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("rotation audit rows = %d, want 1", auditCount)
	}
}

// TestResetCredentialRejectsNonClient: rotation targets must be active
// client-role members of the org; anything else is client_not_found.
func TestResetCredentialRejectsNonClient(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)

	// A staff user is not a client.
	var staffUserID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Not', 'Client')
		RETURNING id
	`, "not-client-"+uuid.NewString()+"@example.com").Scan(&staffUserID); err != nil {
		t.Fatalf("insert staff user: %v", err)
	}
	var memberRoleID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		SELECT id FROM roles WHERE name = 'member' AND organization_id IS NULL
	`).Scan(&memberRoleID); err != nil {
		t.Fatalf("member role: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO memberships (user_id, organization_id, role_id, status)
		VALUES ($1, $2, $3, 'active')
	`, staffUserID, orgID, memberRoleID); err != nil {
		t.Fatalf("staff membership: %v", err)
	}

	if _, err := svc.ResetCredential(ctx, staffID, orgID, staffUserID); !errors.Is(err, apperrors.ErrClientNotFound) {
		t.Fatalf("reset on non-client error = %v, want ErrClientNotFound", err)
	}

	// An unknown user is also client_not_found.
	if _, err := svc.ResetCredential(ctx, staffID, orgID, uuid.New()); !errors.Is(err, apperrors.ErrClientNotFound) {
		t.Fatalf("reset on unknown user error = %v, want ErrClientNotFound", err)
	}
}

// TestConcurrentDoubleProvision: two concurrent provisions of the same
// existing user must yield exactly one success and one 409 already_member —
// never a 500 from the memberships unique-index race.
func TestConcurrentDoubleProvision(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	ctx := context.Background()
	svc := newTestService(db)

	staffID, orgID := seedClientOrg(t, db)
	email := "race-" + uuid.NewString() + "@example.com"

	// The target user exists but is not yet a member of the org.
	var existingID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, 'hash', 'Race', 'Target')
		RETURNING id
	`, email).Scan(&existingID); err != nil {
		t.Fatalf("insert target user: %v", err)
	}

	const workers = 2
	results := make(chan error, workers)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Provision(ctx, staffID, orgID, ProvisionRequest{
				Email:     email,
				FirstName: "Race",
				LastName:  "Provision",
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, apperrors.ErrAlreadyMember):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent provision error: %v", err)
		}
	}

	if successes != 1 {
		t.Fatalf("concurrent provisions succeeded %d times, want exactly 1", successes)
	}
	if conflicts != 1 {
		t.Fatalf("concurrent provisions conflicted %d times, want exactly 1 (409 already_member)", conflicts)
	}

	// Exactly one client membership exists.
	var membershipCount int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM memberships WHERE user_id = $1 AND organization_id = $2
	`, existingID, orgID).Scan(&membershipCount); err != nil {
		t.Fatalf("count memberships: %v", err)
	}
	if membershipCount != 1 {
		t.Fatalf("client membership count = %d, want 1", membershipCount)
	}
}
