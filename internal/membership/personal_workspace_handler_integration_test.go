package membership

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/validation"
)

// memberRoutes builds the four staff-membership mutation routes with the same
// middleware chain as production (org load -> membership -> RBAC permission).
func memberRoutes(db *sql.DB, service *Service) http.Handler {
	h := NewHandler(service)
	org := middleware.NewOrgMiddleware(db)
	access := middleware.NewOrgAccessMiddleware(db)
	rbac := middleware.NewRBACMiddleware(db)

	mux := http.NewServeMux()
	mux.Handle("POST /v1/orgs/{orgID}/members", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("member.invite")(http.HandlerFunc(h.AddOrgMember)))))
	mux.Handle("POST /v1/orgs/{orgID}/members/invite", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("member.invite")(http.HandlerFunc(h.InviteOrgMember)))))
	mux.Handle("PATCH /v1/orgs/{orgID}/members/{userID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("member.role.update")(http.HandlerFunc(h.UpdateOrgMemberRole)))))
	mux.Handle("DELETE /v1/orgs/{orgID}/members/{userID}", org.LoadOrg(access.RequireMembership(rbac.RequirePermission("member.remove")(http.HandlerFunc(h.RemoveOrgMember)))))
	return mux
}

func doMemberRouteRequest(t *testing.T, handler http.Handler, method, path string, actorID uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req = req.WithContext(requestctx.WithUserID(req.Context(), actorID))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

// TestPersonalOrgMemberRoutesReturn409: every staff-membership mutation route
// on a personal workspace returns 409 with the personal_workspace error code —
// including owner-target role change and owner self-remove (the actor isn't
// lacking permission; even the owner is blocked).
func TestPersonalOrgMemberRoutesReturn409(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	ownerID, orgID := seedPersonalOrg(t, db, "http-personal-owner-"+uuid.NewString()+"@example.com")
	targetID := seedUser(t, db, "http-personal-target-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, targetID, orgID, RoleMember)
	inviteeEmail := "http-personal-invitee-" + uuid.NewString() + "@example.com"
	seedUser(t, db, inviteeEmail)

	handler := memberRoutes(db, NewService(db, audit.NewService(db)))
	base := "/v1/orgs/" + orgID.String()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"add member", http.MethodPost, base + "/members", `{"user_id":"` + uuid.NewString() + `","role":"member"}`},
		{"invite member", http.MethodPost, base + "/members/invite", `{"email":"` + "fresh-http-" + uuid.NewString() + `@example.com` + `","role":"member"}`},
		{"change role", http.MethodPatch, base + "/members/" + targetID.String(), `{"role":"admin"}`},
		{"remove member", http.MethodDelete, base + "/members/" + targetID.String(), ""},
		{"owner-target role change", http.MethodPatch, base + "/members/" + ownerID.String(), `{"role":"member"}`},
		{"owner self-remove", http.MethodDelete, base + "/members/" + ownerID.String(), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doMemberRouteRequest(t, handler, tc.method, tc.path, ownerID, tc.body)
			if rr.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
			}
			var payload struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error body: %v; body=%s", err, rr.Body.String())
			}
			if payload.Error.Code != "personal_workspace" {
				t.Fatalf("error code = %q, want %q; body=%s", payload.Error.Code, "personal_workspace", rr.Body.String())
			}
		})
	}
}

// TestWorkspaceMemberRoutesUnchanged: the same four routes on a normal
// workspace keep their established status codes (201 for create, 200 for
// mutate) — the personal guard must not touch non-personal orgs.
func TestWorkspaceMemberRoutesUnchanged(t *testing.T) {
	validation.Init()
	db := integrationDB(t)
	defer db.Close()

	ownerID, orgID := seedOwnerMembership(t, db, "http-workspace-owner-"+uuid.NewString()+"@example.com")
	// addTarget is NOT yet a member (POST /members adds it); targetID is
	// seeded as a member for the role-change/remove cases.
	addTargetID := seedUser(t, db, "http-workspace-addtarget-"+uuid.NewString()+"@example.com")
	targetID := seedUser(t, db, "http-workspace-target-"+uuid.NewString()+"@example.com")
	seedMembershipRole(t, db, targetID, orgID, RoleMember)
	inviteeEmail := "http-workspace-invitee-" + uuid.NewString() + "@example.com"
	seedUser(t, db, inviteeEmail)

	handler := memberRoutes(db, NewService(db, audit.NewService(db)))
	base := "/v1/orgs/" + orgID.String()

	cases := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"add member", http.MethodPost, base + "/members", `{"user_id":"` + addTargetID.String() + `","role":"member"}`, http.StatusCreated},
		{"invite member", http.MethodPost, base + "/members/invite", `{"email":"` + inviteeEmail + `","role":"member"}`, http.StatusCreated},
		{"change role", http.MethodPatch, base + "/members/" + targetID.String(), `{"role":"admin"}`, http.StatusOK},
		{"remove member", http.MethodDelete, base + "/members/" + targetID.String(), "", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doMemberRouteRequest(t, handler, tc.method, tc.path, ownerID, tc.body)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tc.wantStatus, rr.Body.String())
			}
		})
	}
}
