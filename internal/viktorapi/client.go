// Package viktorapi is a client for the Viktor internal API (api.viktor.com).
//
// Used for:
//   - thread_status: reliable "Viktor is done" signal (idle vs running)
//   - credits: balance check for multi-account failover
//
// JWT expires ~14 days. If unavailable, the proxy falls back to text heuristics.
package viktorapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const base = "https://api.viktor.com"

// Client talks to the Viktor internal API using a JWT.
type Client struct {
	JWT        string
	httpClient *http.Client
}

// New creates a Viktor API client.
func New(jwt string) *Client {
	return &Client{
		JWT: jwt,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(path string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.JWT)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

// RawGet performs a raw GET request and returns body, status, error.
func (c *Client) RawGet(path string) ([]byte, int, error) {
	return c.get(path)
}
// Returns -1 on error.
func (c *Client) Credits() (int, error) {
	body, status, err := c.get("/api/team")
	if err != nil {
		return -1, err
	}
	if status != 200 {
		return -1, fmt.Errorf("HTTP %d", status)
	}
	var j struct {
		Credits       int `json:"credits_balance"`
		RewardCredits int `json:"reward_credits_balance"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		return -1, err
	}
	return j.Credits + j.RewardCredits, nil
}

// ThreadStatus returns the status of the agent-run thread for a given Slack message ts.
//   - ""        — thread not found yet (Viktor hasn't picked it up)
//   - "running" — Viktor is working
//   - "idle"    — Viktor finished everything (primary completion signal)
func (c *Client) ThreadStatus(slackTS string) (string, error) {
	body, status, err := c.get("/api/flow/tasks?path=/")
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("HTTP %d", status)
	}

	key := strings.ReplaceAll(slackTS, ".", "_")

	var j struct {
		Threads []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		return "", err
	}
	for _, t := range j.Threads {
		if strings.Contains(t.Path, key) {
			if t.Status == "" {
				return "running", nil // null during work
			}
			return t.Status, nil
		}
	}
	return "", nil // not found yet
}

// findThreadID returns the internal thread ID for a given Slack ts.
func (c *Client) findThreadID(slackTS string) (string, error) {
	body, status, err := c.get("/api/flow/tasks?path=/")
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("HTTP %d", status)
	}
	key := strings.ReplaceAll(slackTS, ".", "_")
	var j struct {
		Threads []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		return "", err
	}
	for _, t := range j.Threads {
		if strings.Contains(t.Path, key) {
			return t.ID, nil
		}
	}
	return "", nil
}

// ReasoningBlock is a single reasoning step from Viktor.
type ReasoningBlock struct {
	ID   string
	Text string
}

// GetReasoning returns reasoning blocks for a given Slack ts.
func (c *Client) GetReasoning(slackTS string) ([]ReasoningBlock, error) {
	tid, err := c.findThreadID(slackTS)
	if err != nil || tid == "" {
		return nil, err
	}

	body, status, err := c.get("/api/threads/" + tid + "/messages")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d", status)
	}

	var messages []struct {
		ID      string `json:"id"`
		Role    string `json:"role"`
		Summary []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(body, &messages); err != nil {
		return nil, err
	}

	var result []ReasoningBlock
	for _, m := range messages {
		if m.Role != "reasoning" {
			continue
		}
		for _, s := range m.Summary {
			if s.Type == "reasoning_summary" && s.Text != "" {
				result = append(result, ReasoningBlock{ID: m.ID, Text: s.Text})
			}
		}
	}
	return result, nil
}

// ── Model settings ─────────────────────────────────────────────

// ModelInfo represents one available model.
type ModelInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`
}

// ModelSettings holds current model + reasoning level.
type ModelSettings struct {
	ModelID        string `json:"model_id"`
	ReasoningLevel string `json:"reasoning_level"` // N/L/M/H
	Raw            string `json:"raw"`
}

var rlToHuman = map[string]string{"none": "N", "low": "L", "medium": "M", "high": "H"}
var humanToRL = map[string]string{"N": "none", "L": "low", "M": "medium", "H": "high"}

// decodeComposite splits "model#ReasoningLevel:high" → ("model", "H")
func decodeComposite(s string) (string, string) {
	const sep = "#ReasoningLevel:"
	i := strings.Index(s, sep)
	if i < 0 {
		return s, "N"
	}
	level := rlToHuman[s[i+len(sep):]]
	if level == "" {
		level = "N"
	}
	return s[:i], level
}

// encodeComposite builds "model#ReasoningLevel:high" from model + N/L/M/H
func encodeComposite(modelID, level string) string {
	if level == "N" || level == "" {
		return modelID
	}
	vkLevel := humanToRL[level]
	if vkLevel == "" {
		vkLevel = "none"
	}
	return modelID + "#ReasoningLevel:" + vkLevel
}

// GetModelSettings returns the current model and reasoning level.
func (c *Client) GetModelSettings() (*ModelSettings, error) {
	body, status, err := c.get("/api/coworker/model-settings")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d", status)
	}
	var j map[string]interface{}
	if err := json.Unmarshal(body, &j); err != nil {
		return nil, err
	}
	raw, _ := j["default_agent_model"].(string)
	modelID, level := decodeComposite(raw)
	return &ModelSettings{ModelID: modelID, ReasoningLevel: level, Raw: raw}, nil
}

// GetAvailableModels returns models from /api/coworker/model-settings options field.
func (c *Client) GetAvailableModels() ([]ModelInfo, error) {
	body, status, err := c.get("/api/coworker/model-settings")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d", status)
	}

	var j map[string]interface{}
	if err := json.Unmarshal(body, &j); err != nil {
		return nil, err
	}

	// Try to find models list in various fields
	var models []ModelInfo
	for _, key := range []string{"options", "models", "agent_models", "available_models"} {
		if raw, ok := j[key]; ok {
			if list, ok := raw.([]interface{}); ok {
				for _, item := range list {
					if m, ok := item.(map[string]interface{}); ok {
						id, _ := m["id"].(string)
						if id == "" {
							id, _ = m["model"].(string)
						}
						if id == "" {
							continue
						}
						name, _ := m["name"].(string)
						if name == "" {
							name, _ = m["display_name"].(string)
						}
						if name == "" {
							name = id
						}
						kind, _ := m["kind"].(string)
						models = append(models, ModelInfo{ID: id, Name: name, Kind: kind})
					}
				}
				if len(models) > 0 {
					break
				}
			}
		}
	}

	// Fallback: at least return the current model
	if len(models) == 0 {
		if ms, err := c.GetModelSettings(); err == nil && ms.ModelID != "" {
			models = append(models, ModelInfo{ID: ms.ModelID, Name: ms.ModelID})
		}
	}

	return models, nil
}

// SetModel updates the model and reasoning level via Viktor API.
func (c *Client) SetModel(modelID, reasoningLevel string) error {
	composite := encodeComposite(modelID, reasoningLevel)

	payload := map[string]string{"default_agent_model": composite}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	for _, method := range []string{"PUT", "PATCH"} {
		req, err := http.NewRequest(method, base+"/api/coworker/model-settings", strings.NewReader(string(body)))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+c.JWT)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
	}
	return fmt.Errorf("failed to update model settings")
}
