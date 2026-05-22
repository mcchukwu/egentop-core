package main

import (
	"context"
	"net/http"

	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcchukwu/egentop/internal/auth"
	"github.com/mcchukwu/egentop/internal/handler"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/org"
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

	mux := http.NewServeMux()

	// Connect to database
	if err := db.Connect(cfg.DatabaseURL); err != nil {
		logger.Error("Failed to connect to database")
		os.Exit(1)
	}
	logger.Info("Connected to database")

	// Configure middleware
	rateLimiterMiddleware := middleware.NewRateLimiterMiddleware(100, time.Minute)
	loginLimiterMiddleware := middleware.NewRateLimiterMiddleware(5, time.Minute)
	registerLimiterMiddleware := middleware.NewRateLimiterMiddleware(3, time.Minute)
	refreshLimiterMiddleware := middleware.NewRateLimiterMiddleware(10, time.Minute)

	authMiddleware := middleware.NewAuthMiddleware(db.DB, []byte(cfg.JWTSecret))

	orgMiddleware := middleware.NewOrgMiddleware(db.DB)
	rbacMiddleware := middleware.NewRBACMiddleware(db.DB)

	requestIDMiddleware := middleware.NewRequestIDMiddleware()
	loggingMiddleware := middleware.NewLoggingMiddleware()
	securityHeadersMiddleware := middleware.NewSecurityHeadersMiddleware()
	corsMiddleware := middleware.NewCorsMiddleware(cfg.CORSAllowedOrigins)
	recoveryMiddleware := middleware.NewRecoveryMiddleware()

	// Configure services and handlers
	authService := auth.NewAuthService(db.DB, []byte(cfg.JWTSecret))
	authHandler := handler.NewAuthHandler(authService)

	orgService := org.NewOrgService(db.DB)
	orgHandler := handler.NewOrgHandler(orgService)

	// Protected routes
	mux.Handle("GET /v1/me", authMiddleware.RequireAuth(http.HandlerFunc(handler.MeHandler)))
	mux.Handle("POST /v1/auth/logout", authMiddleware.RequireAuth(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("POST /v1/auth/logout-all", authMiddleware.RequireAuth(http.HandlerFunc(authHandler.LogoutAllDevices)))

	mux.Handle("POST /v1/orgs", authMiddleware.RequireAuth(http.HandlerFunc(orgHandler.CreateOrgs)))
	mux.Handle("GET /v1/orgs", authMiddleware.RequireAuth(http.HandlerFunc(orgHandler.GetOrgs)))

	// RBAC on organizations
	mux.Handle("GET /v1/orgs/{orgID}/members", authMiddleware.RequireAuth(orgMiddleware.LoadOrg(rbacMiddleware.RequireRole(string(org.RoleAdmin), string(org.RoleOwner))(http.HandlerFunc(orgHandler.GetOrgMembers)))))

	// Other routes
	mux.Handle("POST /v1/auth/register", registerLimiterMiddleware.Limit(http.HandlerFunc(authHandler.Register)))
	mux.Handle("POST /v1/auth/login", loginLimiterMiddleware.Limit(http.HandlerFunc(authHandler.Login)))
	mux.Handle("POST /v1/auth/refresh", refreshLimiterMiddleware.Limit(http.HandlerFunc(authHandler.RefreshToken)))

	// Health check route (for load balancers)
	healthHandler := handler.NewHealthHandler(db.DB)

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
		logger.Info("Egento API starting on port " + cfg.AppPort)
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
