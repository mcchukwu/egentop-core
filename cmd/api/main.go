package main

import (
	//	"context"
	"log"
	"net/http"
	//	"os"
	//	"os/signal"
	//	"syscall"
	//	"time"

	"github.com/mcchukwu/egentop/pkg/config"
	"github.com/mcchukwu/egentop/pkg/logger"
)

func main() {
	cfg := config.Load()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("egento-core-ok"))
	})

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: mux,
	}

	logger.Info("Egento API starting on port " + cfg.Port)

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
