// Package sessions tracks per-session state: turn count, folder, auto-compact.
//
// Viktor holds the full DM history as context. After many turns it gets confused.
// Solution: count turns per session, after MaxTurns ask Viktor for a handoff summary
// and prepend it to the next task as a clean-slate preamble.
//
// Session identity: x-opencode-session-id header → body metadata fields → FNV hash of first user message.
package sessions

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
)

// HANDOFF_PROMPT is sent to Viktor to get a summary before rotating the session.
const HandoffPrompt = "Write a brief HANDOFF SUMMARY of this session so the next assistant can continue without losing context. Plain text, no preamble. Include:\n- overall goal / what we're building\n- key decisions and constraints\n- created/changed files (with paths)\n- current state and what's left\n- immediate next step"

// Header and body candidates for session identity (in priority order).
var hdrCandidates = []string{
	"x-session-id", "x-opencode-session-id", "session_id",
	"session-id", "x-amp-thread-id", "x-client-request-id",
}

// SessionInfo is the resolved session identity.
type SessionInfo struct {
	Hash     string
	Source   string
	Explicit bool
}

// fnv1a returns an 8-char hex FNV-1a hash of the input.
func fnv1a(s string) string {
	h := fnv.New32a()
	h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// ExtractSession resolves session identity from request headers and body.
func ExtractSession(r *http.Request, body map[string]interface{}) SessionInfo {
	// 1. Headers
	if r != nil {
		for _, k := range hdrCandidates {
			v := strings.TrimSpace(r.Header.Get(k))
			if v != "" && v != "undefined" && v != "null" {
				if len(v) > 500 {
					v = v[:500]
				}
				return SessionInfo{fnv1a("header:" + k + ":" + v), "header:" + k, true}
			}
		}
	}

	// 2. Body metadata
	type bodyPath []string
	paths := []bodyPath{
		{"metadata", "user_id"}, {"metadata", "session_id"},
		{"metadata", "conversation_id"}, {"metadata", "thread_id"},
		{"session_id"}, {"conversation_id"}, {"thread_id"},
	}
	for _, path := range paths {
		node := interface{}(body)
		for _, key := range path {
			m, ok := node.(map[string]interface{})
			if !ok {
				node = nil
				break
			}
			node = m[key]
		}
		if v, ok := node.(string); ok {
			v = strings.TrimSpace(v)
			if v != "" && v != "undefined" && v != "null" {
				key := strings.Join(path, ".")
				return SessionInfo{fnv1a("body:" + key + ":" + v), "body:" + key, true}
			}
		}
	}

	// 3. Fallback: hash of first user message
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, m := range msgs {
			msg, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			if msg["role"] == "user" {
				text := extractMsgText(msg["content"])
				if text != "" {
					if len(text) > 4000 {
						text = text[:4000]
					}
					return SessionInfo{fnv1a("fallback:first-user:" + text), "fallback:first-user", false}
				}
			}
		}
	}

	return SessionInfo{fnv1a("fallback:empty"), "fallback:empty", false}
}

func extractMsgText(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	if parts, ok := content.([]interface{}); ok {
		var sb strings.Builder
		for _, p := range parts {
			if m, ok := p.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	}
	return ""
}

// NewSessionPreamble is prepended to the first message of a new/reset session.
func NewSessionPreamble() string {
	return "🆕 New independent task (new thread). Do not pull context from other threads or past tasks — work only from this one.\n\n"
}

// CompactPreamble is prepended after session rotation, including the handoff summary.
func CompactPreamble(summary string) string {
	head := "🔄 NEW SESSION. Completely forget the detailed context of previous messages in this chat — starting fresh."
	if summary != "" {
		head += "\n\nPREVIOUS SESSION SUMMARY (continue seamlessly, don't restart):\n" + summary
	}
	return head + "\n\n"
}

// Pull commands that users can type in OpenCode chat.
var pullCmds = map[string]string{
	"/nopull": "never", "/pullnever": "never", "/pull-never": "never",
	"/pullask": "ask", "/pull-ask": "ask", "/askpull": "ask",
	"/pullauto": "auto", "/pull-auto": "auto", "/autopull": "auto",
}

var pullPrefMsg = map[string]string{
	"never": "🚫 git pull disabled for this session. To re-enable: /pullask or /pullauto.",
	"ask":   "🔄 Will ask about git pull after each Viktor response. Disable: /nopull, always pull: /pullauto.",
	"auto":  "⏩ git pull will run automatically. Ask instead: /pullask, disable: /nopull.",
}

// PullCommand returns pull preference if text is a pull command, or "".
func PullCommand(text string) string {
	return pullCmds[strings.ToLower(strings.TrimSpace(text))]
}

// PullPrefMessage returns the confirmation message for a pull preference change.
func PullPrefMessage(pref string) string {
	return pullPrefMsg[pref]
}

// sessionState is the persisted state for one session.
type sessionState struct {
	Turns   int     `json:"turns"`
	Created float64 `json:"created"`
	Last    float64 `json:"last"`
	Path    string  `json:"path"`
	Root    string  `json:"root"`
	Pull    string  `json:"pull"`
	Acct    string  `json:"acct"`
}

// Store is a JSON-backed session state store.
type Store struct {
	mu   sync.Mutex
	path string
	data map[string]*sessionState
}

// NewStore loads or creates a session store at the given path.
func NewStore(path string) *Store {
	s := &Store{path: path, data: make(map[string]*sessionState)}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s.data)
	}
	return s
}

func (s *Store) get(key string) *sessionState {
	st := s.data[key]
	if st == nil {
		st = &sessionState{Pull: "ask"}
		s.data[key] = st
	}
	return st
}

func (s *Store) Turns(key string) int {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.get(key).Turns
}

func (s *Store) Folder(key string) string {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.get(key).Path
}

func (s *Store) PullPref(key string) string {
	s.mu.Lock(); defer s.mu.Unlock()
	p := s.get(key).Pull
	if p == "" { return "ask" }
	return p
}

func (s *Store) Account(key string) string {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.get(key).Acct
}

func (s *Store) SetAccount(key, name string, ts float64) {
	s.mu.Lock(); defer s.mu.Unlock()
	st := s.get(key)
	st.Acct = name
	if st.Last == 0 { st.Created = ts; st.Last = ts }
	s.save()
}

func (s *Store) SetPullPref(key, val string, ts float64) {
	s.mu.Lock(); defer s.mu.Unlock()
	st := s.get(key)
	st.Pull = val
	st.Last = ts
	s.save()
}

func (s *Store) Bump(key string, ts float64, path, root string) int {
	s.mu.Lock(); defer s.mu.Unlock()
	st := s.get(key)
	st.Turns++
	st.Last = ts
	if st.Created == 0 { st.Created = ts }
	if path != "" { st.Path = path }
	if root != "" { st.Root = root }
	s.save()
	return st.Turns
}

func (s *Store) Reset(key string, ts float64, path string) {
	s.mu.Lock(); defer s.mu.Unlock()
	pull := s.get(key).Pull
	s.data[key] = &sessionState{Created: ts, Last: ts, Path: path, Pull: pull}
	s.save()
}

func (s *Store) save() {
	// keep only last 200 sessions
	if len(s.data) > 200 {
		type kv struct{ k string; t float64 }
		var all []kv
		for k, v := range s.data { all = append(all, kv{k, v.Last}) }
		sort.Slice(all, func(i, j int) bool { return all[i].t < all[j].t })
		for _, item := range all[:len(all)-200] { delete(s.data, item.k) }
	}
	tmp := s.path + ".tmp"
	if data, err := json.Marshal(s.data); err == nil {
		if err := os.WriteFile(tmp, data, 0644); err == nil {
			_ = os.Rename(tmp, s.path)
		}
	}
}

// Snapshot returns the last N sessions for the dashboard, sorted by last activity.
func (s *Store) Snapshot(n int) []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	type kv struct {
		k string
		v *sessionState
	}
	var all []kv
	for k, v := range s.data {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v.Last > all[j].v.Last })
	if len(all) > n {
		all = all[:n]
	}

	result := make([]map[string]interface{}, 0, len(all))
	for _, item := range all {
		result = append(result, map[string]interface{}{
			"hash":  item.k,
			"turns": item.v.Turns,
			"last":  item.v.Last,
			"path":  item.v.Path,
			"pull":  item.v.Pull,
		})
	}
	return result
}
