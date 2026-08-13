package membership

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/validation"
)

func membershipRoleHandler(db *sql.DB, service *Service) http.Handler {
	h := NewHandler(service)
	org := middleware.NewOrgMiddleware(db)
	access := middleware.NewOrgAccessMiddleware(db)
	rbac := middleware.NewRBACMiddleware(db)

	mux := http.NewServeMux()
	mux.Handle("PATCH /v1/orgs/{orgID}/members/{userID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("member.role.update")(http.HandlerFunc(h.UpdateOrgMemberRole)))))
	return mux
}

func doMembershipRoleRequest(t *testing.T, handler http.Handler, actorID, orgID, targetID uuid.UUID, role Role) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/v1/orgs/"+orgID.String()+"/members/"+targetID.String(), bytes.NewBufferString(`{"role":"`+string(role)+`"}`))
	req = req.WithContext(requestctx.WithUserID(req.Context(), actorID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestOwnerRoleEscalationThroughHTTPRoute(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	ownerID, orgID := seedOwnerMembership(t, db, "http-owner-"+uuid.NewString()+"@example.com")
	adminID := seedUser(t, db, "http-admin-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, adminID, orgID, RoleAdmin)
	targetID := seedUser(t, db, "http-target-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, targetID, orgID, RoleMember)

	handler := membershipRoleHandler(db, NewService(db, audit.NewService(db)))

	denied := doMembershipRoleRequest(t, handler, adminID, orgID, targetID, RoleOwner)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("admin escalation status = %d, want 403; body=%s", denied.Code, denied.Body.String())
	}

	allowed := doMembershipRoleRequest(t, handler, ownerID, orgID, targetID, RoleOwner)
	if allowed.Code != http.StatusOK {
		t.Fatalf("owner escalation status = %d, want 200; body=%s", allowed.Code, allowed.Body.String())
	}

	var role string
	if err := db.QueryRowContext(t.Context(), `
		SELECT r.name
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1 AND m.user_id = $2
	`, orgID, targetID).Scan(&role); err != nil {
		t.Fatalf("read target role: %v", err)
	}
	if role != string(RoleOwner) {
		t.Fatalf("target role = %q, want owner", role)
	}
}
