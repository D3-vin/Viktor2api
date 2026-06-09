package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// PatchFile updates specific key:value lines in a YAML file in-place,
// preserving all comments, blank lines, and structure.
func PatchFile(path string, fields map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")

	for key, value := range fields {
		if value == "" {
			continue
		}
		// Matches:   key: "old"  or  key: old  (with optional leading spaces/comments after)
		re := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*:\s*).*$`)
		for i, line := range lines {
			if m := re.FindStringSubmatchIndex(line); m != nil {
				prefix := line[m[2]:m[3]] // "  key: "
				lines[i] = prefix + fmt.Sprintf("%q", value)
				break
			}
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type Config struct {
	Proxy    ProxyConfig    `yaml:"proxy"`
	Viktor   ViktorConfig   `yaml:"viktor"`
	Git      GitConfig      `yaml:"git"`
	Sessions SessionsConfig `yaml:"sessions"`
	Accounts AccountsConfig `yaml:"accounts"`
}

type ProxyConfig struct {
	Port   int    `yaml:"port"`
	APIKey string `yaml:"api_key"`
}

type ViktorConfig struct {
	SlackToken      string  `yaml:"slack_token"`
	SlackCookieD    string  `yaml:"slack_cookie_d"`
	ViktorDM        string  `yaml:"viktor_dm"`
	ViktorUser      string  `yaml:"viktor_user"`
	JWT             string  `yaml:"jwt"`
	PollInterval    float64 `yaml:"poll_interval"`
	ResponseQuiet   float64 `yaml:"response_quiet"`
	ResponseHardCap float64 `yaml:"response_hard_cap"`
}

type GitConfig struct {
	Enabled       bool   `yaml:"enabled"`
	GitHubOwner   string `yaml:"github_owner"`
	LocalRepoPath string `yaml:"local_repo_path"`
}

type SessionsConfig struct {
	Enabled       bool   `yaml:"enabled"`
	MaxTurns      int    `yaml:"max_turns"`
	CreateSummary bool   `yaml:"create_summary"`
	StateFile     string `yaml:"state_file"`
}

type AccountsConfig struct {
	Enabled    bool   `yaml:"enabled"`
	File       string `yaml:"file"`
	MinCredits int    `yaml:"min_credits"`
}

// Load loads config from a YAML file and applies defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Proxy.Port == 0 {
		cfg.Proxy.Port = 8799
	}
	if cfg.Viktor.PollInterval == 0 {
		cfg.Viktor.PollInterval = 3
	}
	if cfg.Viktor.ResponseQuiet == 0 {
		cfg.Viktor.ResponseQuiet = 25
	}
	if cfg.Viktor.ResponseHardCap == 0 {
		cfg.Viktor.ResponseHardCap = 900
	}
	if cfg.Sessions.MaxTurns == 0 {
		cfg.Sessions.MaxTurns = 8
	}
	if cfg.Sessions.StateFile == "" {
		cfg.Sessions.StateFile = ".sessions.json"
	}

	return &cfg, nil
}
