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
	"github.com/mcchukwu/egentop/internal/client"
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
	assignmentService := assignment.NewService(db.DB, assignmentRepo, projectService, auditService, activityService)
	assignmentHandler := assignment.NewHandler(assignmentService)

	userRepo := user.NewRepository(db.DB)
	userService := user.NewService(db.DB, userRepo, auditService, cfg)
	userHandler := user.NewHandler(userService)

	clientRepo := client.NewRepository(db.DB)
	clientService := client.NewService(db.DB, clientRepo, auditService, activityService)
	clientHandler := client.NewHandler(clientService)

	registerRoutes(mux, routeDeps{
		db:               db.DB,
		authMiddleware:   authMiddleware,
		orgMiddleware:    orgMiddleware,
		accessMiddleware: orgAccessMiddleware,
		rbacMiddleware:   rbacMiddleware,
		loginLimiter:     loginLimiterMiddleware.Limit,
		registerLimiter:  registerLimiterMiddleware.Limit,
		refreshLimiter:   refreshLimiterMiddleware.Limit,
		passwordLimiter:  passwordChangeLimiterMiddleware.Limit,
		h: handlers{
			auth:       authHandler,
			user:       userHandler,
			org:        orgHandler,
			membership: membershipHandler,
			client:     clientHandler,
			project:    projectHandler,
			assignment: assignmentHandler,
			activity:   activityHandler,
		},
	})

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
