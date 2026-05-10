package main

import (
	//	"context"
	"log"
	"net/http"
	"time"

	//	"os"
	//	"os/signal"
	//	"syscall"
	//	"time"

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
	rateLimiter := middleware.NewRateLimiter(100, time.Minute)
	loginLimiter := middleware.NewRateLimiter(5, time.Minute)
	registerLimiter := middleware.NewRateLimiter(3, time.Minute)
	refreshLimiter := middleware.NewRateLimiter(10, time.Minute)

	authMiddleware := middleware.NewAuthMiddleware(db.DB, []byte(cfg.JWTSecret))

	// Configure services and handlers
	authService := auth.NewAuthService(db.DB, []byte(cfg.JWTSecret))
	authHandler := handler.NewAuthHandler(authService)

	// Protected routes
	mux.Handle("GET /v1/me", authMiddleware.RequireAuth(http.HandlerFunc(handler.Me)))
	mux.Handle("POST /v1/auth/logout", authMiddleware.RequireAuth(http.HandlerFunc(authHandler.Logout)))
	mux.Handle("POST /v1/auth/logout-all", authMiddleware.RequireAuth(http.HandlerFunc(authHandler.LogoutAllDevices)))

	// Auth routes
	mux.Handle("POST /v1/auth/register", registerLimiter.Middleware(http.HandlerFunc(authHandler.Register)))
	mux.Handle("POST /v1/auth/login", loginLimiter.Middleware(http.HandlerFunc(authHandler.Login)))
	mux.Handle("POST /v1/auth/refresh", refreshLimiter.Middleware(http.HandlerFunc(authHandler.RefreshToken)))

	// Health check (for load balancers)
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("egento-core-ok"))
	})

	// chain middleware
	handlerChain := middleware.Recovery(middleware.Logging(rateLimiter.Middleware(middleware.SecurityHeaders(mux))))

	server := &http.Server{
		Addr:    ":" + cfg.AppPort,
		Handler: handlerChain,
	}

	logger.Info("Egento API starting on port " + cfg.AppPort)

	log.Fatal(server.ListenAndServe())

	/*
		// GRACEFUL SHUTDOWN
		-------------------------------

			go func() {
				logger.Info("Egento API starting on port " + cfg.Port)
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatal(err)
				}
			}()

			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := server.Shutdown(ctx); err != nil {
				log.Fatal(err)
			}

			logger.Info("Server exiting gracefully")
	*/
}
