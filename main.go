package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"viktor2api/internal/config"
	"viktor2api/internal/server"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	r := mux.NewRouter()
	srv := server.New(cfg)

	// OpenAI endpoints
	r.HandleFunc("/v1/models", srv.HandleModels).Methods("GET")
	r.HandleFunc("/v1/chat/completions", srv.HandleChatCompletions).Methods("POST")

	// Accounts API
	r.HandleFunc("/api/accounts", srv.HandleAccountsList).Methods("GET")
	r.HandleFunc("/api/accounts", srv.HandleAccountsUpsert).Methods("POST")
	r.HandleFunc("/api/accounts/activate", srv.HandleAccountsActivate).Methods("POST")
	r.HandleFunc("/api/accounts/remove", srv.HandleAccountsRemove).Methods("POST")

	// Config API (Chrome extension)
	r.HandleFunc("/api/config", srv.HandleGetConfig).Methods("GET")
	r.HandleFunc("/api/config", srv.HandleUpdateConfig).Methods("POST", "PATCH")

	// Dashboard API
	r.HandleFunc("/api/dashboard", srv.HandleDashboardAPI).Methods("GET")
	r.HandleFunc("/api/dashboard/model", srv.HandleDashboardModel).Methods("GET")
	r.HandleFunc("/api/dashboard/sessions", srv.HandleDashboardSessions).Methods("GET")
	r.HandleFunc("/api/dashboard/credits", srv.HandleDashboardCredits).Methods("GET")
	r.HandleFunc("/api/models/set", srv.HandleModelsSet).Methods("POST")

	// Dashboard UI
	r.HandleFunc("/", srv.HandleDashboard).Methods("GET")
	r.HandleFunc("/dashboard", srv.HandleDashboard).Methods("GET")

	// Health
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}).Methods("GET")

	// CORS for Chrome extensions only
	c := cors.New(cors.Options{
		AllowOriginFunc: func(origin string) bool {
			return len(origin) > 17 && origin[:17] == "chrome-extension:"
		},
		AllowedMethods: []string{"GET", "POST", "PATCH", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	})

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Proxy.Port)

	fmt.Println()
	fmt.Println("  Viktor2api")
	fmt.Println("  GitHub:    https://github.com/D3-vin/Viktor2api")
	fmt.Println("  Telegram:  https://t.me/D3_vin")
	fmt.Println("  Author:    @D3vin_dev")
	fmt.Println()
	log.Printf("Started on http://%s", addr)
	log.Printf("Dashboard: http://%s/dashboard", addr)
	log.Printf("Endpoint:  http://%s/v1", addr)
	log.Printf("Press Ctrl+C to stop")

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: c.Handler(r),
	}

	// Catch Ctrl+C, SIGTERM, and console close (Windows)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	registerConsoleCloseHandler(quit)

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-quit
	log.Printf("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpSrv.Shutdown(ctx)
	log.Printf("Stopped.")
}
