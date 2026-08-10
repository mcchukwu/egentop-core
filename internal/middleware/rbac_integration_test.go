package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/requestctx"
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

func seedUser(t *testing.T, db *sql.DB, email string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO users (email, password_hash, first_name, last_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, email, "hash", "Test", "User").Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	return id
}

func seedOrg(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := db.QueryRowContext(context.Background(), `
		INSERT INTO organizations (name)
		VALUES ($1)
		RETURNING id
	`, name).Scan(&id)
	if err != nil {
		t.Fatalf("insert org: %v", err)
	}

	return id
}

func seedMembership(t *testing.T, db *sql.DB, userID, orgID uuid.UUID, role membership.Role) {
	t.Helper()

	roleID, err := membership.ResolveSystemRoleID(context.Background(), db, role)
	if err != nil {
		t.Fatalf("resolve role: %v", err)
	}

	_, err = db.ExecContext(context.Background(), `
		INSERT INTO memberships (user_id, organization_id, role_id)
		VALUES ($1, $2, $3)
	`, userID, orgID, roleID)
	if err != nil {
		t.Fatalf("insert membership: %v", err)
	}
}

func okHandler(capturedRole *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if role, ok := requestctx.Role(r.Context()); ok {
			*capturedRole = role
		}

		w.WriteHeader(http.StatusOK)
	})
}

func TestRequirePermissionIntegration(t *testing.T) {
	db := integrationDB(t)
	defer db.Close()

	orgID := seedOrg(t, db, "RBAC Test Org")

	ownerID := seedUser(t, db, "rbac-owner-"+uuid.NewString()+"@example.com")
	seedMembership(t, db, ownerID, orgID, membership.RoleOwner)

	viewerID := seedUser(t, db, "rbac-viewer-"+uuid.NewString()+"@example.com")
	seedMembership(t, db, viewerID, orgID, membership.RoleViewer)

	mw := NewRBACMiddleware(db)

	tests := []struct {
		name       string
		userID     uuid.UUID
		permission string
		want       int
	}{
		{"owner can invite", ownerID, "member.invite", http.StatusOK},
		{"owner can update role", ownerID, "member.role.update", http.StatusOK},
		{"owner can create project", ownerID, "project.create", http.StatusOK},
		{"viewer can list projects", viewerID, "project.list", http.StatusOK},
		{"viewer cannot invite", viewerID, "member.invite", http.StatusForbidden},
		{"viewer cannot create project", viewerID, "project.create", http.StatusForbidden},
		{"viewer cannot update role", viewerID, "member.role.update", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRole string

			handler := mw.RequirePermission(tt.permission)(okHandler(&capturedRole))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(requestctx.WithUserID(req.Context(), tt.userID))
			req = req.WithContext(requestctx.WithOrganizationID(req.Context(), orgID))

			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.want {
				t.Fatalf("status = %d, want %d", rr.Code, tt.want)
			}

			// role name is propagated to the wrapped handler on success
			if tt.want == http.StatusOK && capturedRole == "" {
				t.Fatalf("expected role in context, got none")
			}
			if tt.want != http.StatusOK && capturedRole != "" {
				t.Fatalf("expected no role in context on denial, got %q", capturedRole)
			}
		})
	}

	ctx := context.Background()

	var ownerAllows, viewerAllows, viewerDenies int

	err := db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM authz_decisions
		WHERE user_id = $1 AND organization_id = $2 AND allowed = true
	`, ownerID, orgID).Scan(&ownerAllows)
	if err != nil {
		t.Fatalf("count owner allows: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM authz_decisions
		WHERE user_id = $1 AND organization_id = $2 AND allowed = true
	`, viewerID, orgID).Scan(&viewerAllows)
	if err != nil {
		t.Fatalf("count viewer allows: %v", err)
	}

	err = db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM authz_decisions
		WHERE user_id = $1 AND organization_id = $2 AND allowed = false
	`, viewerID, orgID).Scan(&viewerDenies)
	if err != nil {
		t.Fatalf("count viewer denies: %v", err)
	}

	if ownerAllows != 3 {
		t.Fatalf("owner allows = %d, want 3", ownerAllows)
	}
	if viewerAllows != 1 {
		t.Fatalf("viewer allows = %d, want 1", viewerAllows)
	}
	if viewerDenies != 3 {
		t.Fatalf("viewer denies = %d, want 3", viewerDenies)
	}
}
