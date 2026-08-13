package main

import (
	"context"
	"net/http"

	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/assignment"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/auth"
	"github.com/mcchukwu/egentop/internal/health"
	"github.com/mcchukwu/egentop/internal/jwt"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/organization"
	"github.com/mcchukwu/egentop/internal/project"
	"github.com/mcchukwu/egentop/internal/user"
	"github.com/mcchukwu/egentop/internal/validation"
	"github.com/mcchukwu/egentop/pkg/config"
	"github.com/mcchukwu/egentop/pkg/db"
	"github.com/mcchukwu/egentop/pkg/logger"
)

func main() {
	cfg := config.Load()

	if err := cfg.Validate(); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}

	validation.Init()

	mux := http.NewServeMux()

	// Connect to database
	if err := db.Connect(cfg.DBURL); err != nil {
		logger.Error(err.Error())
		logger.Error("Failed to connect to database")
		os.Exit(1)
	}
	logger.Info("Connected to database")

	// Configure middleware
	rateLimiterMiddleware := middleware.NewRateLimiterMiddleware(100, time.Minute)
	loginLimiterMiddleware := middleware.NewRateLimiterMiddleware(5, time.Minute)
	registerLimiterMiddleware := middleware.NewRateLimiterMiddleware(3, time.Minute)
	refreshLimiterMiddleware := middleware.NewRateLimiterMiddleware(10, time.Minute)
	passwordChangeLimiterMiddleware := middleware.NewRateLimiterMiddleware(5, time.Minute)

	authMiddleware := middleware.NewAuthMiddleware(db.DB, []byte(cfg.JWTSecret))

	orgMiddleware := middleware.NewOrgMiddleware(db.DB)
	rbacMiddleware := middleware.NewRBACMiddleware(db.DB)
	orgAccessMiddleware := middleware.NewOrgAccessMiddleware(db.DB)

	requestIDMiddleware := middleware.NewRequestIDMiddleware()
	loggingMiddleware := middleware.NewLoggingMiddleware()
	securityHeadersMiddleware := middleware.NewSecurityHeadersMiddleware()
	corsMiddleware := middleware.NewCorsMiddleware(cfg.CORSAllowedOrigins)
	recoveryMiddleware := middleware.NewRecoveryMiddleware()

	// Configure repositories, services and handlers
	auditService := audit.NewService(db.DB)

	activityRepository := activity.NewRepository(db.DB)
	activityService := activity.NewService(activityRepository)
	activityHandler := activity.NewHandler(activityService)

	jwtManager := jwt.NewManager(cfg.JWTSecret, cfg.JWTAccessTokenTTL)
	authService := auth.NewService(db.DB, auditService, jwtManager, cfg)
	authHandler := auth.NewHandler(authService, cfg)

	orgService := organization.NewService(db.DB, auditService)
	orgHandler := organization.NewHandler(orgService)

	membershipService := membership.NewService(db.DB, auditService)
	membershipHandler := membership.NewHandler(membershipService)

	projectRepo := project.NewRepository(db.DB)
	projectService := project.NewService(db.DB, projectRepo, auditService, activityService)
	projectHandler := project.NewHandler(projectService)

	assignmentRepo := assignment.NewRepository(db.DB)
	assignmentService := assignment.NewService(db.DB, assignmentRepo, auditService, activityService)
	assignmentHandler := assignment.NewHandler(assignmentService)

	userRepo := user.NewRepository(db.DB)
	userService := user.NewService(db.DB, userRepo, auditService, cfg)
	userHandler := user.NewHandler(userService)

	// Protected routes
	mux.Handle("GET /v1/me", authMiddleware.RequireAuth(http.HandlerFunc(userHandler.Me)))
	mux.Handle("PATCH /v1/me", authMiddleware.RequireAuth(http.HandlerFunc(userHandler.UpdateProfile)))
	mux.Handle("POST /v1/me/password", authMiddleware.RequireAuth(passwordChangeLimiterMiddleware.Limit(http.HandlerFunc(userHandler.ChangePassword))))

	mux.Handle("POST /v1/auth/refresh", refreshLimiterMiddleware.Limit(http.HandlerFunc(authHandler.RefreshToken)))
	mux.Handle("POST /v1/auth/logout", http.HandlerFunc(authHandler.Logout))
	mux.Handle("POST /v1/auth/logout-all", http.HandlerFunc(authHandler.LogoutAllDevices))

	mux.Handle("POST /v1/orgs", authMiddleware.RequireAuth(http.HandlerFunc(orgHandler.Create)))
	mux.Handle("GET /v1/orgs", authMiddleware.RequireAuth(http.HandlerFunc(orgHandler.List)))
	mux.Handle("GET /v1/orgs/{orgID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("org.view")(http.HandlerFunc(orgHandler.GetByID))))))
	mux.Handle("PATCH /v1/orgs/{orgID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("org.update")(http.HandlerFunc(orgHandler.Update))))))

	// RBAC on organizations
	mux.Handle("GET /v1/orgs/{orgID}/members", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("member.list")(http.HandlerFunc(membershipHandler.GetOrgMembers))))))
	mux.Handle("POST /v1/orgs/{orgID}/members", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("member.invite")(http.HandlerFunc(membershipHandler.AddOrgMember))))))
	mux.Handle("POST /v1/orgs/{orgID}/members/invite", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("member.invite")(http.HandlerFunc(membershipHandler.InviteOrgMember))))))
	mux.Handle("PATCH /v1/orgs/{orgID}/members/{userID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("member.role.update")(http.HandlerFunc(membershipHandler.UpdateOrgMemberRole))))))
	mux.Handle("DELETE /v1/orgs/{orgID}/members/{userID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("member.remove")(http.HandlerFunc(membershipHandler.RemoveOrgMember))))))

	// Projects
	mux.Handle("POST /v1/orgs/{orgID}/projects", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("project.create")(http.HandlerFunc(projectHandler.Create))))))
	mux.Handle("GET /v1/orgs/{orgID}/projects", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("project.list")(http.HandlerFunc(projectHandler.ListProjectsByOrganizationID))))))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("project.view")(http.HandlerFunc(projectHandler.GetProjectByID))))))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("project.update")(http.HandlerFunc(projectHandler.Update))))))

	// Milestones
	mux.Handle("POST /v1/orgs/{orgID}/projects/{projectID}/milestones", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("milestone.create")(http.HandlerFunc(projectHandler.CreateMilestone))))))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/milestones", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("milestone.list")(http.HandlerFunc(projectHandler.ListMilestonesByProjectID))))))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("milestone.view")(http.HandlerFunc(projectHandler.GetMilestoneByID))))))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("milestone.update")(http.HandlerFunc(projectHandler.UpdateMilestone))))))

	// Assignments
	mux.Handle("POST /v1/orgs/{orgID}/projects/{projectID}/assignments", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("assignment.create")(http.HandlerFunc(assignmentHandler.Create))))))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/assignments", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("assignment.list")(http.HandlerFunc(assignmentHandler.ListByProjectID))))))
	mux.Handle("GET /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("assignment.view")(http.HandlerFunc(assignmentHandler.GetByID))))))
	mux.Handle("PATCH /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("assignment.update")(http.HandlerFunc(assignmentHandler.Update))))))
	mux.Handle("DELETE /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("assignment.remove")(http.HandlerFunc(assignmentHandler.Delete))))))

	// Activity feed
	mux.Handle("GET /v1/orgs/{orgID}/activities", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(orgAccessMiddleware.RequireMembership(rbacMiddleware.RequirePermission("activity.list")(http.HandlerFunc(activityHandler.List))))))

	// Public routes
	mux.Handle("POST /v1/auth/register", registerLimiterMiddleware.Limit(http.HandlerFunc(authHandler.Register)))
	mux.Handle("POST /v1/auth/login", loginLimiterMiddleware.Limit(http.HandlerFunc(authHandler.Login)))

	// Health check route (for load balancers)
	healthHandler := health.NewHealthHandler(db.DB)

	mux.HandleFunc("GET /v1/health", healthHandler.Health)
	mux.HandleFunc("GET /v1/ready", healthHandler.Ready)
	mux.HandleFunc("GET /v1/live", healthHandler.Live)

	// chain middleware
	handlerChain := recoveryMiddleware.Recover((requestIDMiddleware.Assign(loggingMiddleware.Log(securityHeadersMiddleware.Secure(corsMiddleware.Cors(rateLimiterMiddleware.Limit(mux)))))))

	server := &http.Server{
		Addr:         ":" + cfg.AppPort,
		Handler:      handlerChain,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server safely
	go func() {
		logger.Info("Server is running")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start server")
			os.Exit(1)
		}
	}()

	// Shutdown signal listener
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// Graceful shutdown context
	logger.Info("Shutting down server")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown server
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Graceful shutdown failed")
		server.Close()
	}

	// Close database connection
	if err := db.DB.Close(); err != nil {
		logger.Error("Database connection close failed")
	}

	logger.Info("Server exiting gracefully")
}
