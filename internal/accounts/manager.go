// Package accounts manages multiple Viktor accounts with manual switching.
// Data is stored in accounts.json (gitignored).
// If accounts.json doesn't exist, the proxy seeds a single "default" account from config.yaml.
package accounts

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"viktor2api/internal/config"
)

// Account holds credentials for one Viktor account.
type Account struct {
	Name         string `json:"name"`
	SlackToken   string `json:"slack_token"`
	SlackCookieD string `json:"slack_cookie_d"`
	ViktorDM     string `json:"viktor_dm"`
	ViktorUser   string `json:"viktor_user"`
	JWT          string `json:"jwt"`
	Enabled      bool   `json:"enabled"`
	Model        string `json:"model"`
	ReasoningLvl string `json:"reasoning_level"`
}

func mask(s string) string {
	if len(s) <= 10 {
		return strings.Repeat("•", len(s))
	}
	return fmt.Sprintf("%s…%s (%d)", s[:6], s[len(s)-4:], len(s))
}

// SafeView returns a copy safe to send to the dashboard (secrets masked).
func (a Account) SafeView() map[string]interface{} {
	return map[string]interface{}{
		"name":           a.Name,
		"enabled":        a.Enabled,
		"model":          a.Model,
		"reasoning_level": a.ReasoningLvl,
		"has_slack":      a.SlackToken != "" && a.SlackCookieD != "",
		"has_dm":         a.ViktorDM != "",
		"has_jwt":        a.JWT != "",
		"slack_token":    mask(a.SlackToken),
		"slack_cookie_d": mask(a.SlackCookieD),
		"viktor_dm":      a.ViktorDM,
		"viktor_user":    a.ViktorUser,
		"jwt":            mask(a.JWT),
	}
}

type store struct {
	Accounts   []Account `json:"accounts"`
	ActiveName string    `json:"active"`
}

// Manager handles multiple accounts.
type Manager struct {
	mu       sync.RWMutex
	path     string
	accounts []Account
	active   string
}

// New loads or creates the account manager.
// If the file doesn't exist and cfg has credentials, seeds a "default" account.
func New(path string, cfg *config.Config) *Manager {
	m := &Manager{path: path}
	m.load(cfg)
	return m
}

func (m *Manager) load(cfg *config.Config) {
	data, err := os.ReadFile(m.path)
	if err == nil {
		var s store
		if json.Unmarshal(data, &s) == nil && len(s.Accounts) > 0 {
			m.accounts = s.Accounts
			m.active = s.ActiveName
			if m.active == "" && len(m.accounts) > 0 {
				m.active = m.accounts[0].Name
			}
			return
		}
	}

	// Seed from config.yaml
	if cfg != nil && cfg.Viktor.SlackToken != "" {
		m.accounts = []Account{{
			Name:         "default",
			SlackToken:   cfg.Viktor.SlackToken,
			SlackCookieD: cfg.Viktor.SlackCookieD,
			ViktorDM:     cfg.Viktor.ViktorDM,
			ViktorUser:   cfg.Viktor.ViktorUser,
			JWT:          cfg.Viktor.JWT,
			Enabled:      true,
			ReasoningLvl: "N",
		}}
		m.active = "default"
		m.save()
	}
}

func (m *Manager) save() {
	s := store{Accounts: m.accounts, ActiveName: m.active}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, m.path)
}

// Active returns the currently active account or nil.
func (m *Manager) Active() *Account {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.accounts {
		if m.accounts[i].Name == m.active && m.accounts[i].Enabled {
			return &m.accounts[i]
		}
	}
	// Fallback: first enabled
	for i := range m.accounts {
		if m.accounts[i].Enabled {
			return &m.accounts[i]
		}
	}
	return nil
}

// ActiveName returns the name of the active account.
func (m *Manager) ActiveName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// SetActive switches the active account by name.
func (m *Manager) SetActive(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.accounts {
		if a.Name == name {
			m.active = name
			m.save()
			return true
		}
	}
	return false
}

// List returns all accounts (safe view).
func (m *Manager) List() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]map[string]interface{}, 0, len(m.accounts))
	for _, a := range m.accounts {
		v := a.SafeView()
		v["active"] = a.Name == m.active
		out = append(out, v)
	}
	return out
}

// Upsert adds or updates an account.
func (m *Manager) Upsert(data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name, _ := data["name"].(string)
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}

	// Find existing
	for i := range m.accounts {
		if m.accounts[i].Name == name {
			applyFields(&m.accounts[i], data)
			m.save()
			return nil
		}
	}

	// New account
	a := Account{Name: name, Enabled: true, ReasoningLvl: "N"}
	applyFields(&a, data)
	m.accounts = append(m.accounts, a)
	if m.active == "" {
		m.active = name
	}
	m.save()
	return nil
}

// Remove deletes an account by name.
func (m *Manager) Remove(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, a := range m.accounts {
		if a.Name == name {
			m.accounts = append(m.accounts[:i], m.accounts[i+1:]...)
			if m.active == name && len(m.accounts) > 0 {
				m.active = m.accounts[0].Name
			}
			m.save()
			return true
		}
	}
	return false
}

func applyFields(a *Account, data map[string]interface{}) {
	if v, ok := data["slack_token"].(string); ok && v != "" {
		a.SlackToken = v
	}
	if v, ok := data["slack_cookie_d"].(string); ok && v != "" {
		a.SlackCookieD = v
	}
	if v, ok := data["viktor_dm"].(string); ok && v != "" {
		a.ViktorDM = v
	}
	if v, ok := data["viktor_user"].(string); ok && v != "" {
		a.ViktorUser = v
	}
	if v, ok := data["jwt"].(string); ok && v != "" {
		a.JWT = v
	}
	if v, ok := data["model"].(string); ok {
		a.Model = v
	}
	if v, ok := data["reasoning_level"].(string); ok && v != "" {
		a.ReasoningLvl = v
	}
	if v, ok := data["enabled"].(bool); ok {
		a.Enabled = v
	}
}
