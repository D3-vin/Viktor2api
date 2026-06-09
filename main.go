package main

import (
	"fmt"
	"log"
	"net/http"

	"viktor2api/internal/config"
	"viktor2api/internal/server"

	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

func main() {
	// Загрузка конфигурации
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Не удалось загрузить конфиг: %v", err)
	}

	// Создание HTTP роутера
	r := mux.NewRouter()

	// Инициализация сервера
	srv := server.New(cfg)

	// OpenAI-совместимые эндпоинты
	r.HandleFunc("/v1/models", srv.HandleModels).Methods("GET")
	r.HandleFunc("/v1/chat/completions", srv.HandleChatCompletions).Methods("POST")

	// Config API (используется Chrome-расширением)
	r.HandleFunc("/api/config", srv.HandleGetConfig).Methods("GET")
	r.HandleFunc("/api/config", srv.HandleUpdateConfig).Methods("POST", "PATCH")

	// Dashboard API
	r.HandleFunc("/api/dashboard", srv.HandleDashboardAPI).Methods("GET")
	r.HandleFunc("/api/dashboard/model", srv.HandleDashboardModel).Methods("GET")
	r.HandleFunc("/api/dashboard/sessions", srv.HandleDashboardSessions).Methods("GET")
	r.HandleFunc("/api/models/set", srv.HandleModelsSet).Methods("POST")

	// Дашборд
	r.HandleFunc("/", srv.HandleDashboard).Methods("GET")
	r.HandleFunc("/dashboard", srv.HandleDashboard).Methods("GET")

	// Healthcheck
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}).Methods("GET")

	// CORS — только для Chrome-расширений (chrome-extension://)
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
	log.Printf("Viktor2api started on http://%s", addr)
	log.Printf("Dashboard:        http://%s/dashboard", addr)
	log.Printf("OpenAI endpoint:  http://%s/v1", addr)
	log.Printf("Config API:       http://%s/api/config", addr)

	if err := http.ListenAndServe(addr, c.Handler(r)); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
