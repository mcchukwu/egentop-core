package main

import (
	"context"
	"log"
	"net/http"

	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mcchukwu/egentop/internal/auth"
	"github.com/mcchukwu/egentop/internal/handler"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/pkg/config"
	"github.com/mcchukwu/egentop/pkg/db"
	"github.com/mcchukwu/egentop/pkg/logger"
)

func main() {
	cfg := config.Load()
	mux := http.NewServeMux()

	// Connect to database
	if err := db.Connect(cfg.DatabaseURL); err != nil {
		log.Fatal(err)
	}
	logger.Info("Connected to database")

	// Configure middleware
	rateLimiterMiddleware := middleware.NewRateLimiterMiddleware(100, time.Minute)
	loginLimiterMiddleware := middleware.NewRateLimiterMiddleware(5, time.Minute)
	registerLimiterMiddleware := middleware.NewRateLimiterMiddleware(3, time.Minute)
	refreshLimiterMiddleware := middleware.NewRateLimiterMiddleware(10, time.Minute)

	authMiddleware := middleware.NewAuthMiddleware(db.DB, []byte(cfg.JWTSecret))

	requestIDMiddleware := middleware.NewRequestIDMiddleware()
	loggingMiddleware := middleware.NewLoggingMiddleware()
	securityHeadersMiddleware := middleware.NewSecurityHeadersMiddleware()
	corsMiddleware := middleware.NewCorsMiddleware(cfg.CORSAllowedOrigins)

	// Configure services and handlers
	authService := auth.NewAuthService(db.DB, []byte(cfg.JWTSecret))
	authHandler := handler.NewAuthHandler(authService)

	// Protected routes
	mux.Handle("GET /v1/me", authMiddleware.RequireAuth(http.HandlerFunc(handler.Me)))
	mux.Handle("POST /v1/auth/logout", authMiddleware.RequireAuth(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("POST /v1/auth/logout-all", authMiddleware.RequireAuth(http.HandlerFunc(authHandler.LogoutAllDevices)))

	// Auth routes
	mux.Handle("POST /v1/auth/register", registerLimiterMiddleware.Limit(http.HandlerFunc(authHandler.Register)))
	mux.Handle("POST /v1/auth/login", loginLimiterMiddleware.Limit(http.HandlerFunc(authHandler.Login)))
	mux.Handle("POST /v1/auth/refresh", refreshLimiterMiddleware.Limit(http.HandlerFunc(authHandler.RefreshToken)))

	// Health check (for load balancers)
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Egento-core-ok"))
	})

	// chain middleware
	handlerChain := middleware.Recovery(requestIDMiddleware.Assign(loggingMiddleware.Log(securityHeadersMiddleware.Secure(corsMiddleware.Cors(rateLimiterMiddleware.Limit(mux))))))

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
			log.Fatal(err)
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
