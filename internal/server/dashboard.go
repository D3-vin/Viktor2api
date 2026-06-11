package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"time"

	"viktor2api/internal/viktorapi"
)

var startTime = time.Now()

func (s *Server) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}

func (s *Server) HandleDashboardAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	uptime := time.Since(startTime)
	acct := s.accounts.Active()
	cfg := map[string]bool{
		"slack": acct != nil && acct.SlackToken != "" && acct.SlackCookieD != "",
		"viktor": acct != nil && acct.ViktorDM != "" && acct.ViktorUser != "",
		"jwt":   acct != nil && acct.JWT != "",
		"git":   s.config.Git.Enabled,
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "running",
		"uptime":     formatUptime(uptime),
		"port":       s.config.Proxy.Port,
		"config":     cfg,
		"git":        s.config.Git.Enabled,
		"go_version": runtime.Version(),
		"has_jwt":    s.vapi != nil,
		"active_account": s.accounts.ActiveName(),
	})
}

func (s *Server) HandleDashboardCredits(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.vapi == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "JWT not configured"})
		return
	}
	ch := make(chan struct {
		credits int
		err     error
	}, 1)
	go func() {
		c, err := s.vapi.Credits()
		ch <- struct {
			credits int
			err     error
		}{c, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"error": res.err.Error()})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{"credits": res.credits})
		}
	case <-time.After(5 * time.Second):
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "timeout"})
	}
}

func (s *Server) HandleDashboardModel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.vapi == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "JWT not configured"})
		return
	}
	type result struct {
		settings *viktorapi.ModelSettings
		models   []viktorapi.ModelInfo
		err      error
	}
	ch := make(chan result, 1)
	go func() {
		ms, err := s.vapi.GetModelSettings()
		if err != nil {
			ch <- result{err: err}
			return
		}
		models, _ := s.vapi.GetAvailableModels()
		ch <- result{settings: ms, models: models}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"error": res.err.Error()})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"model":           res.settings.ModelID,
				"reasoning_level": res.settings.ReasoningLevel,
				"raw":             res.settings.Raw,
				"available":       res.models,
			})
		}
	case <-time.After(5 * time.Second):
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "timeout"})
	}
}

func (s *Server) HandleModelsSet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.vapi == nil {
		http.Error(w, `{"error":"JWT not configured"}`, http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Model          string `json:"model"`
		ReasoningLevel string `json:"reasoning_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if err := s.vapi.SetModel(body.Model, body.ReasoningLevel); err != nil {
		log.Printf("SetModel error: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	log.Printf("Model set: %s reasoning=%s", body.Model, body.ReasoningLevel)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
}

func (s *Server) HandleDashboardSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.sessions.Snapshot(20))
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, sec)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, sec)
	}
	return fmt.Sprintf("%ds", sec)
}
