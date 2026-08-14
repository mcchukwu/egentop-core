package main

import (
	"database/sql"
	"net/http"

	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/assignment"
	"github.com/mcchukwu/egentop/internal/auth"
	"github.com/mcchukwu/egentop/internal/client"
	"github.com/mcchukwu/egentop/internal/health"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/organization"
	"github.com/mcchukwu/egentop/internal/project"
	"github.com/mcchukwu/egentop/internal/user"
)

// handlers bundles the HTTP handlers referenced by the route table.
type handlers struct {
	auth       *auth.Handler
	user       *user.Handler
	org        *organization.Handler
	membership *membership.Handler
	client     *client.Handler
	project    *project.Handler
	assignment *assignment.Handler
	activity   *activity.Handler
}

// routeDeps carries everything registerRoutes needs to wire the mux.
type routeDeps struct {
	db *sql.DB

	authMiddleware   *middleware.AuthMiddleware
	orgMiddleware    *middleware.OrgMiddleware
	accessMiddleware *middleware.OrgAccessMiddleware
	rbacMiddleware   *middleware.RBACMiddleware

	loginLimiter    func(http.Handler) http.Handler
	registerLimiter func(http.Handler) http.Handler
	refreshLimiter  func(http.Handler) http.Handler
	passwordLimiter func(http.Handler) http.Handler

	h handlers
}

// protectedRoute is one RequireAuth-wrapped route. gated=false is reserved
// for POST /v1/me/password: users with must_change_password may only rotate
// their credential (or log out / refresh via the cookie-authenticated routes,
// which are NOT RequireAuth-wrapped at all) until the gate lifts.
type protectedRoute struct {
	method  string
	pattern string
	handler http.Handler
	gated   bool
}

// protectedRoutes is the single source of truth for the protected route
// table. It is data-driven so the gate-coverage invariant is testable (see
// routes_test.go): every gated=true route must return 403 password_change_
// required for a must-change user, and the sole gated=false route must not.
func protectedRoutes(d routeDeps) []protectedRoute {
	// orgScoped wraps a handler with the org/access/RBAC chain used by every
	// organization-scoped route.
	orgScoped := func(permission string, next http.Handler) http.Handler {
		return d.orgMiddleware.LoadOrg(
			d.accessMiddleware.RequireMembership(
				d.rbacMiddleware.RequirePermission(permission)(next),
			),
		)
	}

	return []protectedRoute{
		// Current user
		{method: "GET", pattern: "/v1/me", handler: http.HandlerFunc(d.h.user.Me), gated: true},
		{method: "PATCH", pattern: "/v1/me", handler: http.HandlerFunc(d.h.user.UpdateProfile), gated: true},
		{method: "POST", pattern: "/v1/me/password", handler: d.passwordLimiter(http.HandlerFunc(d.h.user.ChangePassword)), gated: false},

		// Organizations
		{method: "POST", pattern: "/v1/orgs", handler: http.HandlerFunc(d.h.org.Create), gated: true},
		{method: "GET", pattern: "/v1/orgs", handler: http.HandlerFunc(d.h.org.List), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}", handler: orgScoped("org.view", http.HandlerFunc(d.h.org.GetByID)), gated: true},
		{method: "PATCH", pattern: "/v1/orgs/{orgID}", handler: orgScoped("org.update", http.HandlerFunc(d.h.org.Update)), gated: true},

		// Memberships
		{method: "GET", pattern: "/v1/orgs/{orgID}/members", handler: orgScoped("member.list", http.HandlerFunc(d.h.membership.GetOrgMembers)), gated: true},
		{method: "POST", pattern: "/v1/orgs/{orgID}/members", handler: orgScoped("member.invite", http.HandlerFunc(d.h.membership.AddOrgMember)), gated: true},
		{method: "POST", pattern: "/v1/orgs/{orgID}/members/invite", handler: orgScoped("member.invite", http.HandlerFunc(d.h.membership.InviteOrgMember)), gated: true},
		{method: "PATCH", pattern: "/v1/orgs/{orgID}/members/{userID}", handler: orgScoped("member.role.update", http.HandlerFunc(d.h.membership.UpdateOrgMemberRole)), gated: true},
		{method: "DELETE", pattern: "/v1/orgs/{orgID}/members/{userID}", handler: orgScoped("member.remove", http.HandlerFunc(d.h.membership.RemoveOrgMember)), gated: true},

		// Clients
		{method: "POST", pattern: "/v1/orgs/{orgID}/clients", handler: orgScoped("client.provision", http.HandlerFunc(d.h.client.Provision)), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}/clients", handler: orgScoped("client.list", http.HandlerFunc(d.h.client.List)), gated: true},
		{method: "POST", pattern: "/v1/orgs/{orgID}/clients/{userID}/reset-credential", handler: orgScoped("client.provision", http.HandlerFunc(d.h.client.ResetCredential)), gated: true},

		// Projects
		{method: "POST", pattern: "/v1/orgs/{orgID}/projects", handler: orgScoped("project.create", http.HandlerFunc(d.h.project.Create)), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}/projects", handler: orgScoped("project.list", http.HandlerFunc(d.h.project.ListProjectsByOrganizationID)), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}/projects/{projectID}", handler: orgScoped("project.view", http.HandlerFunc(d.h.project.GetProjectByID)), gated: true},
		{method: "PATCH", pattern: "/v1/orgs/{orgID}/projects/{projectID}", handler: orgScoped("project.update", http.HandlerFunc(d.h.project.Update)), gated: true},
		{method: "PUT", pattern: "/v1/orgs/{orgID}/projects/{projectID}/client", handler: orgScoped("project.client.assign", http.HandlerFunc(d.h.project.AssignClient)), gated: true},

		// Milestones
		{method: "POST", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones", handler: orgScoped("milestone.create", http.HandlerFunc(d.h.project.CreateMilestone)), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones", handler: orgScoped("milestone.list", http.HandlerFunc(d.h.project.ListMilestonesByProjectID)), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}", handler: orgScoped("milestone.view", http.HandlerFunc(d.h.project.GetMilestoneByID)), gated: true},
		{method: "PATCH", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}", handler: orgScoped("milestone.update", http.HandlerFunc(d.h.project.UpdateMilestone)), gated: true},

		// Milestone state machine
		{method: "POST", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/submit", handler: orgScoped("milestone.submit", http.HandlerFunc(d.h.project.SubmitMilestone)), gated: true},
		{method: "POST", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/approve", handler: orgScoped("milestone.approve", http.HandlerFunc(d.h.project.ApproveMilestone)), gated: true},
		{method: "POST", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/changes-requested", handler: orgScoped("milestone.revision.request", http.HandlerFunc(d.h.project.RequestMilestoneChanges)), gated: true},
		{method: "PATCH", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/status", handler: orgScoped("milestone.status.update", http.HandlerFunc(d.h.project.UpdateMilestoneStatus)), gated: true},

		// Deliverables
		{method: "POST", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/deliverables", handler: orgScoped("deliverable.submit", http.HandlerFunc(d.h.project.CreateDeliverable)), gated: true},
		{method: "DELETE", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/deliverables/{deliverableID}", handler: orgScoped("deliverable.submit", http.HandlerFunc(d.h.project.DeleteDeliverable)), gated: true},

		// Payment status
		{method: "PATCH", pattern: "/v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/payment-status", handler: orgScoped("milestone.payment_status.update", http.HandlerFunc(d.h.project.UpdateMilestonePaymentStatus)), gated: true},

		// Client-facing surface (approval deep link + project-scoped activity)
		{method: "GET", pattern: "/v1/orgs/{orgID}/projects/{projectID}/approval", handler: orgScoped("milestone.view", http.HandlerFunc(d.h.project.GetApprovalView)), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}/projects/{projectID}/activities", handler: orgScoped("activity.project.list", http.HandlerFunc(d.h.project.ListProjectActivities)), gated: true},

		// Assignments
		{method: "POST", pattern: "/v1/orgs/{orgID}/projects/{projectID}/assignments", handler: orgScoped("assignment.create", http.HandlerFunc(d.h.assignment.Create)), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}/projects/{projectID}/assignments", handler: orgScoped("assignment.list", http.HandlerFunc(d.h.assignment.ListByProjectID)), gated: true},
		{method: "GET", pattern: "/v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", handler: orgScoped("assignment.view", http.HandlerFunc(d.h.assignment.GetByID)), gated: true},
		{method: "PATCH", pattern: "/v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", handler: orgScoped("assignment.update", http.HandlerFunc(d.h.assignment.Update)), gated: true},
		{method: "DELETE", pattern: "/v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", handler: orgScoped("assignment.remove", http.HandlerFunc(d.h.assignment.Delete)), gated: true},

		// Activity feed
		{method: "GET", pattern: "/v1/orgs/{orgID}/activities", handler: orgScoped("activity.list", http.HandlerFunc(d.h.activity.List)), gated: true},
	}
}

// registerRoutes wires every route onto mux. The password gate wraps every
// authenticated route except password change (protectedRoutes' gated flag);
// cookie-authenticated and public routes are registered without RequireAuth.
func registerRoutes(mux *http.ServeMux, d routeDeps) {
	authGate := middleware.RequirePasswordChanged

	// Public + cookie-authenticated + health routes (no RequireAuth, no gate).
	mux.Handle("POST /v1/auth/refresh", d.refreshLimiter(http.HandlerFunc(d.h.auth.RefreshToken)))
	mux.Handle("POST /v1/auth/logout", http.HandlerFunc(d.h.auth.Logout))
	mux.Handle("POST /v1/auth/logout-all", http.HandlerFunc(d.h.auth.LogoutAllDevices))
	mux.Handle("POST /v1/auth/register", d.registerLimiter(http.HandlerFunc(d.h.auth.Register)))
	mux.Handle("POST /v1/auth/login", d.loginLimiter(http.HandlerFunc(d.h.auth.Login)))

	healthHandler := health.NewHealthHandler(d.db)
	mux.HandleFunc("GET /v1/health", healthHandler.Health)
	mux.HandleFunc("GET /v1/ready", healthHandler.Ready)
	mux.HandleFunc("GET /v1/live", healthHandler.Live)

	// Protected routes: RequireAuth always; RequirePasswordChanged unless the
	// route is the explicit password-rotation exception.
	for _, route := range protectedRoutes(d) {
		handler := route.handler
		if route.gated {
			handler = authGate(handler)
		}
		mux.Handle(route.method+" "+route.pattern, d.authMiddleware.RequireAuth(handler))
	}
}
