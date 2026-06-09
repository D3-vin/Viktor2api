package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"viktor2api/internal/config"
	"viktor2api/internal/git"
	"viktor2api/internal/sessions"
	"viktor2api/internal/slack"
	"viktor2api/internal/viktorapi"
)

type Server struct {
	config   *config.Config
	slack    *slack.Client
	sessions *sessions.Store
	vapi     *viktorapi.Client // optional: Viktor API for reliable completion signal
}

func New(cfg *config.Config) *Server {
	var slackClient *slack.Client
	if cfg.Viktor.SlackToken != "" {
		slackClient = slack.NewClient(
			cfg.Viktor.SlackToken,
			cfg.Viktor.SlackCookieD,
			cfg.Viktor.ViktorDM,
			cfg.Viktor.ViktorUser,
		)
	}

	var vapi *viktorapi.Client
	if cfg.Viktor.JWT != "" {
		vapi = viktorapi.New(cfg.Viktor.JWT)
		log.Printf("Viktor API: enabled (JWT configured)")
	}

	store := sessions.NewStore(cfg.Sessions.StateFile)

	return &Server{
		config:   cfg,
		slack:    slackClient,
		sessions: store,
		vapi:     vapi,
	}
}

// HandleModels - OpenAI /v1/models endpoint
func (s *Server) HandleModels(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	response := map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{
				"id":      "viktor",
				"object":  "model",
				"created": time.Now().Unix(),
				"owned_by": "viktor2api",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleChatCompletions - OpenAI /v1/chat/completions endpoint
func (s *Server) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if s.slack == nil {
		http.Error(w, "Viktor not configured (check config.yaml)", http.StatusServiceUnavailable)
		return
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Bad request decode error: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Tool followup (e.g. git pull answer) — last message role is "tool", no need to forward to Viktor
	if isToolFollowup(req.Messages) {
		log.Printf("Tool followup — skipping Viktor")
		sendTextResponse(w, req.Stream, "")
		return
	}

	// OpenCode internal: "Generate a short title for this conversation" — answer locally
	if isTitleRequest(req.Messages) {
		task := s.extractTask(req.Messages)
		title := makeTitleLocal(task)
		log.Printf("Title request — answering locally: %q", title)
		sendTextResponse(w, req.Stream, title)
		return
	}

	// Extract task from last user message
	task := s.extractTask(req.Messages)
	if task == "" {
		http.Error(w, "No user message found", http.StatusBadRequest)
		return
	}

	log.Printf("Task: %s", truncate(task, 100))

	// Extract project path from system prompt (OpenCode sends it as "working directory: /path")
	projectPath := extractProjectPath(req.Messages, s.config.Git.LocalRepoPath)
	// Also check X-Project-Path header
	if h := r.Header.Get("X-Project-Path"); h != "" && projectPath == "" {
		projectPath = strings.TrimSpace(h)
	}

	// Resolve session identity — pass real body so session headers/metadata work
	bodyMap := map[string]interface{}{
		"messages": req.Messages,
	}
	if req.Stream {
		bodyMap["stream"] = true
	}
	sinfo := sessions.ExtractSession(r, bodyMap)
	sess := sinfo.Hash
	log.Printf("Session: %s (%s) project=%q", sess, sinfo.Source, projectPath)

	// Pull commands (/nopull, /pullask, /pullauto)
	if pullCmd := sessions.PullCommand(task); pullCmd != "" {
		s.sessions.SetPullPref(sess, pullCmd, float64(time.Now().Unix()))
		log.Printf("Pull pref -> %s", pullCmd)
		sendTextResponse(w, req.Stream, sessions.PullPrefMessage(pullCmd))
		return
	}

	// Session directive: new / continue / compact
	directive, needsCompact := s.sessionDirective(sess, projectPath)

	// Git sync: push local state, create session branch, wrap task with repo context
	sessionBranch := ""
	savedBranch   := ""
	if s.config.Git.Enabled && projectPath != "" {
		task, sessionBranch, savedBranch = s.gitSync(w, req.Stream, task, projectPath)
		if task == "" {
			return // gitSync already wrote error response
		}
	}

	// Prepend session directive (new session preamble or compact summary)
	if directive != "" {
		if needsCompact {
			// Collect Viktor's handoff summary synchronously before this task
			summary := s.collectSummary()
			task = sessions.CompactPreamble(summary) + task
		} else {
			task = directive + task
		}
	}

	// Bump session turn counter
	s.sessions.Bump(sess, float64(time.Now().Unix()), projectPath, "")

	// Streaming response
	if req.Stream {
		s.handleStreamingResponse(w, r, task, projectPath, sessionBranch, savedBranch, sess)
	} else {
		s.handleNonStreamingResponse(w, task, projectPath, sessionBranch, savedBranch, sess)
	}
}

// sessionDirective decides if a new preamble or compact is needed.
// Returns (directive string, needsCompact bool).
// needsCompact=true means we need to collect Viktor's summary before prepending.
func (s *Server) sessionDirective(sess, projectPath string) (string, bool) {
	if !s.config.Sessions.Enabled {
		return "", false
	}
	turns := s.sessions.Turns(sess)
	prevPath := s.sessions.Folder(sess)
	folderChanged := prevPath != "" && projectPath != "" && prevPath != projectPath

	if turns == 0 || folderChanged {
		why := "new session"
		if folderChanged { why = "folder changed" }
		log.Printf("Session %s: %s -> fresh context", sess, why)
		s.sessions.Reset(sess, float64(time.Now().Unix()), projectPath)
		return sessions.NewSessionPreamble(), false
	}

	if s.config.Sessions.MaxTurns > 0 && turns >= s.config.Sessions.MaxTurns {
		log.Printf("Session %s: rotation after %d turns", sess, turns)
		s.sessions.Reset(sess, float64(time.Now().Unix()), projectPath)
		return "", true // caller will collect summary and call CompactPreamble
	}

	log.Printf("Session %s: continue (turn %d)", sess, turns+1)
	return "", false
}

// collectSummary sends the handoff prompt to Viktor and collects the response.
func (s *Server) collectSummary() string {
	if s.slack == nil {
		return ""
	}
	ts, err := s.slack.PostMessage(sessions.HandoffPrompt)
	if err != nil {
		log.Printf("Session compact: post error: %v", err)
		return ""
	}

	var parts []string
	lastTS := ts
	start := time.Now()
	quietTime := time.Duration(s.config.Viktor.ResponseQuiet * float64(time.Second))
	lastMsg := time.Now()

	for time.Since(start) < 3*time.Minute {
		msgs, err := s.slack.FetchThread(ts, lastTS)
		if err == nil {
			for _, m := range msgs {
				lastTS = m.TS
				if !slack.IsAck(m.Text) {
					parts = append(parts, m.Text)
				}
				lastMsg = time.Now()
			}
		}
		if len(parts) > 0 && time.Since(lastMsg) >= quietTime {
			break
		}
		time.Sleep(time.Duration(s.config.Viktor.PollInterval * float64(time.Second)))
	}

	return strings.Join(parts, "\n")
}

// gitSync pushes local state, creates session branch, wraps task with repo context.
// Returns (wrappedTask, sessionBranch, savedBranch).
func (s *Server) gitSync(_ http.ResponseWriter, _ bool, task, projectPath string) (string, string, string) {
	owner, err := git.DetectOwner(s.config.Git.GitHubOwner)
	if err != nil {
		log.Printf("Git: cannot detect owner: %v", err)
		return task, "", ""
	}

	repo, err := git.EnsureRepo(projectPath, owner)
	if err != nil {
		log.Printf("Git: ensure repo error: %v", err)
		return task, "", ""
	}

	git.EnsureLocalExclude(projectPath, "viktor_media/")

	if pushReport, err := git.PushLocal(projectPath, repo.Branch); err != nil {
		log.Printf("Git: push error: %v", err)
	} else if pushReport != "" {
		log.Printf("Git: %s/%s branch=%s: %s", repo.Owner, repo.Name, repo.Branch, pushReport)
	}

	savedBranch := git.CurrentBranch(projectPath)
	sessionID := fmt.Sprintf("%d", time.Now().Unix())
	sessionBranch, err := git.SessionBranch(projectPath, sessionID)
	if err != nil {
		log.Printf("Git: session branch error: %v", err)
		return git.WrapTask(task, repo), "", ""
	}

	repo.Branch = sessionBranch
	log.Printf("Git: session branch %s, repo %s/%s", sessionBranch, repo.Owner, repo.Name)
	return git.WrapTask(task, repo), sessionBranch, savedBranch
}

// handleStreamingResponse отправляет SSE поток
func (s *Server) handleStreamingResponse(w http.ResponseWriter, r *http.Request, task, projectPath, sessionBranch, savedBranch, sess string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send message to Viktor
	threadTS, err := s.slack.PostMessage(task)
	if err != nil {
		s.sendSSEError(w, flusher, fmt.Sprintf("Slack error: %v", err))
		return
	}

	log.Printf("Message sent to Viktor, thread_ts=%s", threadTS)

	// Poll for Viktor's responses
	lastTS      := threadTS
	seenReal    := false
	seenDone    := false
	seenClarify := false
	expectMore  := false
	startTime   := time.Now()
	lastMsgTime := time.Now()
	lastHB      := time.Now()
	slackFails  := 0
	apiFails    := 0
	threadSeen  := false          // Viktor API: thread appeared
	var idleSince *time.Time      // Viktor API: when thread became idle
	reasoningSeen := map[string]bool{}

	pollInterval := time.Duration(s.config.Viktor.PollInterval*float64(time.Second))
	quietTime    := time.Duration(s.config.Viktor.ResponseQuiet*float64(time.Second))
	hardCap      := time.Duration(s.config.Viktor.ResponseHardCap*float64(time.Second))

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// --- Slack messages ---
			messages, err := s.slack.FetchThread(threadTS, lastTS)
			if err != nil {
				slackFails++
				log.Printf("Slack poll error (%d): %v", slackFails, err)
				if slackFails >= 5 {
					s.sendSSEChunk(w, flusher, fmt.Sprintf("\n[Slack unavailable: %v]\n", err))
					break
				}
			} else {
				slackFails = 0
				for _, msg := range messages {
					lastTS = msg.TS
					text := msg.Text
					s.sendSSEChunk(w, flusher, text+"\n\n")
					lastMsgTime = time.Now()
					lastHB = lastMsgTime

					// Download file attachments (e.g. Viktor's generated code files)
					for _, f := range msg.Files {
						mediaDir := "viktor_media"
						if projectPath != "" {
							mediaDir = projectPath + "/viktor_media"
						}
						if localPath, err := s.slack.DownloadFile(f, mediaDir); err != nil {
							log.Printf("File download error %s: %v", f.Name, err)
							s.sendSSEChunk(w, flusher, fmt.Sprintf("\n⚠️ failed to download: %s\n", f.Name))
						} else {
							log.Printf("Downloaded: %s", localPath)
							s.sendSSEChunk(w, flusher, fmt.Sprintf("\n📎 saved: `%s`\n", localPath))
						}
					}

					if !slack.IsAck(text) {
						seenReal = true
						if slack.ExpectsMore(text) {
							expectMore = true; seenDone = false; seenClarify = false
						} else {
							expectMore = false
							if slack.LooksDone(text)    { seenDone = true }
							if slack.LooksClarify(text) { seenClarify = true }
						}
					}
				}
			}

			// --- Viktor API: thread status (primary completion signal) ---
			useAPI := s.vapi != nil && apiFails < 3
			if useAPI {
				status, err := s.vapi.ThreadStatus(threadTS)
				if err != nil {
					apiFails++
					log.Printf("Viktor API error (%d): %v", apiFails, err)
				} else if status != "" {
					threadSeen = true
					if status == "idle" {
						if idleSince == nil {
							now := time.Now()
							idleSince = &now
						}
					} else {
						idleSince = nil
					}
				}
			}

			// --- Viktor API: reasoning blocks ---
			if s.vapi != nil && threadSeen && apiFails < 3 {
				blocks, err := s.vapi.GetReasoning(threadTS)
				if err == nil {
					for _, b := range blocks {
						if !reasoningSeen[b.ID] {
							reasoningSeen[b.ID] = true
							s.sendSSEChunk(w, flusher, b.Text+"\n\n")
							lastMsgTime = time.Now()
							lastHB = lastMsgTime
							log.Printf("Reasoning block %d (%d chars)", len(reasoningSeen), len(b.Text))
						}
					}
				}
			}

			now   := time.Now()
			quiet := now.Sub(lastMsgTime)

			// Heartbeat every 10s
			if now.Sub(lastHB) >= 10*time.Second {
				lastHB = now
				s.sendSSEHeartbeat(w, flusher)
			}

			// --- Completion: Viktor API idle signal (most reliable) ---
			if threadSeen && idleSince != nil && now.Sub(*idleSince) >= 5*time.Second {
				log.Printf("Done (API idle, %.0fs after idle)", now.Sub(*idleSince).Seconds())
				// Grace poll: Slack messages and files may arrive slightly after API goes idle
				time.Sleep(2 * time.Second)
				if finalMsgs, err := s.slack.FetchThread(threadTS, lastTS); err == nil {
					for _, msg := range finalMsgs {
						lastTS = msg.TS
						s.sendSSEChunk(w, flusher, msg.Text+"\n\n")
						for _, f := range msg.Files {
							mediaDir := "viktor_media"
							if projectPath != "" { mediaDir = projectPath + "/viktor_media" }
							if localPath, err := s.slack.DownloadFile(f, mediaDir); err == nil {
								log.Printf("Downloaded (grace): %s", localPath)
								s.sendSSEChunk(w, flusher, fmt.Sprintf("\n📎 saved: `%s`\n", localPath))
							}
						}
					}
				}
				break
			}

			// --- Completion: text heuristics fallback ---
			if !(threadSeen && idleSince != nil) {
				if !expectMore && seenClarify && quiet >= minDuration(quietTime, 6*time.Second) {
					log.Printf("Done (clarify, quiet %.0fs)", quiet.Seconds())
					break
				}
				if !expectMore && seenDone && quiet >= quietTime {
					log.Printf("Done (done marker, quiet %.0fs)", quiet.Seconds())
					break
				}
				if expectMore && quiet >= maxDuration(quietTime*2, 60*time.Second) {
					log.Printf("Done (expect_more timeout, quiet %.0fs)", quiet.Seconds())
					break
				}
				if !expectMore && seenReal && quiet >= quietTime*3 {
					log.Printf("Done (real+quiet*3, quiet %.0fs)", quiet.Seconds())
					break
				}
			}

			if now.Sub(startTime) >= hardCap {
				log.Printf("Hard cap reached (%.0fs)", now.Sub(startTime).Seconds())
				s.sendSSEChunk(w, flusher, "\n[response timeout]\n")
				break
			}
			continue // keep looping

		case <-r.Context().Done():
			log.Printf("Client disconnected")
			return
		}
		break // exit for loop when inner select breaks
	}

	// After Viktor is done — git pull his changes, then switch back to main branch
	if projectPath != "" && sessionBranch != "" {
		s.doGitPull(w, flusher, projectPath, sessionBranch, savedBranch, sess)
	} else if sessionBranch != "" && savedBranch != "" {
		if err := git.SwitchBack(projectPath, savedBranch); err != nil {
			log.Printf("Git: switch back error: %v", err)
		} else {
			s.sendSSEChunk(w, flusher,
				fmt.Sprintf("\n🌿 _Viktor worked in branch `%s`. To apply: `git merge %s`_\n",
					sessionBranch, sessionBranch))
		}
	}

	s.sendSSEDone(w, flusher)
}

// doGitPull pulls Viktor's changes and reports the result via SSE.
func (s *Server) doGitPull(w http.ResponseWriter, flusher http.Flusher, projectPath, sessionBranch, savedBranch, sess string) {
	pullPref := "auto"
	if sess != "" {
		pullPref = s.sessions.PullPref(sess)
	}

	if pullPref == "never" {
		log.Printf("Git pull: skipped (pref=never)")
		return
	}

	if err := git.SwitchBack(projectPath, savedBranch); err != nil {
		log.Printf("Git: switch back error: %v", err)
	}

	result, err := git.Pull(projectPath, sessionBranch)
	if err != nil {
		log.Printf("Git pull error: %v", err)
		s.sendSSEChunk(w, flusher, fmt.Sprintf("\n⚠️ git pull error: %v\n", err))
		return
	}

	log.Printf("Git pull: %s", strings.SplitN(result, "\n", 2)[0])
	s.sendSSEChunk(w, flusher, fmt.Sprintf("\n📥 **git pull** (branch `%s`)\n```\n%s\n```\n", sessionBranch, result))
}

// handleNonStreamingResponse возвращает полный ответ
func (s *Server) handleNonStreamingResponse(w http.ResponseWriter, task, projectPath, sessionBranch, savedBranch, sess string) {
	threadTS, err := s.slack.PostMessage(task)
	if err != nil {
		http.Error(w, fmt.Sprintf("Slack error: %v", err), http.StatusInternalServerError)
		return
	}

	var fullText strings.Builder
	lastTS       := threadTS
	seenReal     := false
	seenDone     := false
	seenClarify  := false
	expectMore   := false
	startTime    := time.Now()
	lastMsgTime  := time.Now()
	pollInterval := time.Duration(s.config.Viktor.PollInterval*float64(time.Second))
	quietTime    := time.Duration(s.config.Viktor.ResponseQuiet*float64(time.Second))
	hardCap      := time.Duration(s.config.Viktor.ResponseHardCap*float64(time.Second))

	for {
		if msgs, err := s.slack.FetchThread(threadTS, lastTS); err == nil {
			for _, msg := range msgs {
				lastTS = msg.TS
				fullText.WriteString(msg.Text)
				fullText.WriteString("\n\n")
				lastMsgTime = time.Now()

				if !slack.IsAck(msg.Text) {
					seenReal = true
					if slack.ExpectsMore(msg.Text) {
						expectMore = true; seenDone = false; seenClarify = false
					} else {
						expectMore = false
						if slack.LooksDone(msg.Text)    { seenDone = true }
						if slack.LooksClarify(msg.Text) { seenClarify = true }
					}
				}
			}
		}

		now   := time.Now()
		quiet := now.Sub(lastMsgTime)

		if !expectMore && seenClarify && quiet >= minDuration(quietTime, 6*time.Second) { break }
		if !expectMore && seenDone    && quiet >= quietTime                              { break }
		if  expectMore               && quiet >= maxDuration(quietTime*2, 60*time.Second) { break }
		if !expectMore && seenReal   && quiet >= quietTime*3                             { break }
		if now.Sub(startTime) >= hardCap {
			fullText.WriteString("\n[response timeout]\n")
			break
		}

		time.Sleep(pollInterval)
	}

	response := ChatCompletionResponse{
		ID: fmt.Sprintf("chatcmpl-%d", time.Now().Unix()), Object: "chat.completion",
		Created: time.Now().Unix(), Model: "viktor",
		Choices: []Choice{{
			Index:        0,
			Message:      AssistantMessage{Role: "assistant", Content: fullText.String()},
			FinishReason: "stop",
		}},
	}

	// Switch back + git pull
	if projectPath != "" && sessionBranch != "" {
		pullPref := "auto"
		if sess != "" { pullPref = s.sessions.PullPref(sess) }
		git.SwitchBack(projectPath, savedBranch)
		if pullPref != "never" {
			if result, err := git.Pull(projectPath, sessionBranch); err == nil {
				fullText.WriteString(fmt.Sprintf("\n📥 **git pull** (branch `%s`)\n```\n%s\n```\n", sessionBranch, result))
			}
		}
	} else if sessionBranch != "" && savedBranch != "" {
		git.SwitchBack(projectPath, savedBranch)
		fullText.WriteString(fmt.Sprintf(
			"\n🌿 _Viktor worked in branch `%s`. To apply: `git merge %s`_\n",
			sessionBranch, sessionBranch))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleGetConfig returns current viktor config fields (masked secrets).
func (s *Server) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":           true,
		"has_token":    s.config.Viktor.SlackToken != "",
		"has_cookie":   s.config.Viktor.SlackCookieD != "",
		"has_dm":       s.config.Viktor.ViktorDM != "",
		"has_user":     s.config.Viktor.ViktorUser != "",
		"has_jwt":      s.config.Viktor.JWT != "",
		"viktor_dm":    s.config.Viktor.ViktorDM,
		"viktor_user":  s.config.Viktor.ViktorUser,
	})
}

// HandleUpdateConfig patches config.yaml with fields from the request body.
// Accepts: slack_token, slack_cookie_d, viktor_dm, viktor_user, jwt (any subset).
func (s *Server) HandleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	updated := []string{}

	if v, ok := body["slack_token"]; ok && v != "" {
		s.config.Viktor.SlackToken = v
		updated = append(updated, "slack_token")
	}
	if v, ok := body["slack_cookie_d"]; ok && v != "" {
		s.config.Viktor.SlackCookieD = v
		updated = append(updated, "slack_cookie_d")
	}
	if v, ok := body["viktor_dm"]; ok && v != "" {
		s.config.Viktor.ViktorDM = v
		updated = append(updated, "viktor_dm")
	}
	if v, ok := body["viktor_user"]; ok && v != "" {
		s.config.Viktor.ViktorUser = v
		updated = append(updated, "viktor_user")
	}
	if v, ok := body["jwt"]; ok && v != "" {
		s.config.Viktor.JWT = v
		updated = append(updated, "jwt")
	}

	if len(updated) == 0 {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	// Patch config.yaml in-place — preserves comments and structure
	patches := map[string]string{}
	for _, k := range updated {
		switch k {
		case "slack_token":    patches["slack_token"]    = s.config.Viktor.SlackToken
		case "slack_cookie_d": patches["slack_cookie_d"] = s.config.Viktor.SlackCookieD
		case "viktor_dm":      patches["viktor_dm"]      = s.config.Viktor.ViktorDM
		case "viktor_user":    patches["viktor_user"]    = s.config.Viktor.ViktorUser
		case "jwt":            patches["jwt"]            = s.config.Viktor.JWT
		}
	}
	if err := config.PatchFile("config.yaml", patches); err != nil {
		log.Printf("Config patch error: %v", err)
		http.Error(w, fmt.Sprintf("save error: %v", err), http.StatusInternalServerError)
		return
	}

	// Hot-reload slack client and vapi if credentials changed
	if contains(updated, "slack_token") || contains(updated, "slack_cookie_d") ||
		contains(updated, "viktor_dm") || contains(updated, "viktor_user") {
		s.slack = slack.NewClient(
			s.config.Viktor.SlackToken,
			s.config.Viktor.SlackCookieD,
			s.config.Viktor.ViktorDM,
			s.config.Viktor.ViktorUser,
		)
		log.Printf("Slack client reloaded")
	}
	if contains(updated, "jwt") && s.config.Viktor.JWT != "" {
		s.vapi = viktorapi.New(s.config.Viktor.JWT)
		log.Printf("Viktor API client reloaded")
	}

	log.Printf("Config updated: %v", updated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"updated": updated,
	})
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s { return true }
	}
	return false
}

// Вспомогательные методы

func (s *Server) checkAuth(r *http.Request) bool {
	if s.config.Proxy.APIKey == "" {
		return true
	}

	auth := r.Header.Get("Authorization")
	expected := "Bearer " + s.config.Proxy.APIKey
	return auth == expected
}

func (s *Server) extractTask(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Text())
		}
	}
	return ""
}

func (s *Server) sendSSEChunk(w http.ResponseWriter, flusher http.Flusher, text string) {
	chunk := SSEChunk{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   "viktor",
		Choices: []SSEChoice{
			{
				Index: 0,
				Delta: Delta{Content: text},
			},
		},
	}

	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func (s *Server) sendSSEHeartbeat(w http.ResponseWriter, flusher http.Flusher) {
	// Empty comment — keeps SSE connection alive without producing visible text
	fmt.Fprintf(w, ": heartbeat\n\n")
	flusher.Flush()
}

func (s *Server) sendSSEDone(w http.ResponseWriter, flusher http.Flusher) {
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func (s *Server) sendSSEError(w http.ResponseWriter, flusher http.Flusher, errMsg string) {
	s.sendSSEChunk(w, flusher, fmt.Sprintf("\n[error: %s]\n", errMsg))
	s.sendSSEDone(w, flusher)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b { return a }
	return b
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b { return a }
	return b
}

// extractProjectPath detects the open project folder from OpenCode system prompt.
var pathMarkerRe = regexp.MustCompile(
	`(?i)(?:working directory|current working directory|current directory|cwd|project root|directory)\s*[:=]\s*` + "`?" + `"?'?([^\n` + "`" + `"']+)`)

func extractProjectPath(messages []Message, fallback string) string {
	for _, m := range messages {
		if m.Role != "system" && m.Role != "user" {
			continue
		}
		text := m.Text()
		if match := pathMarkerRe.FindStringSubmatch(text); len(match) > 1 {
			p := strings.TrimRight(strings.TrimSpace(match[1]), `/\`)
			if p != "" && os.IsPathSeparator(p[0]) {
				return p
			}
			// Windows absolute path (C:\...)
			if len(p) > 2 && p[1] == ':' {
				return p
			}
		}
	}
	return fallback
}
func isToolFollowup(messages []Message) bool {
	if len(messages) == 0 {
		return false
	}
	return messages[len(messages)-1].Role == "tool"
}

// isTitleRequest — OpenCode просит сгенерировать заголовок чата
var titleRe = regexp.MustCompile(`(?i)^generate\s+(?:a\s+)?(?:short\s+)?title\s+for\s+(?:this\s+)?(?:conversation|chat|session)`)

func isTitleRequest(messages []Message) bool {
	for _, m := range messages {
		if m.Role == "user" || m.Role == "system" {
			if titleRe.MatchString(strings.TrimSpace(m.Text())) {
				return true
			}
		}
	}
	return false
}

// makeTitleLocal — короткий заголовок из текста задачи (без обращения к Viktor)
func makeTitleLocal(task string) string {
	// Убираем префикс "Generate a short title for this conversation:"
	task = titleRe.ReplaceAllString(task, "")
	task = strings.TrimLeft(task, ": \n")
	words := strings.Fields(task)
	if len(words) > 6 {
		words = words[:6]
	}
	if len(words) == 0 {
		return "Conversation"
	}
	return strings.Join(words, " ")
}

// sendTextResponse — быстрый текстовый ответ без обращения к Viktor
func sendTextResponse(w http.ResponseWriter, stream bool, text string) {
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		id := fmt.Sprintf("chatcmpl-%d", time.Now().Unix())
		created := time.Now().Unix()

		// role chunk
		roleChunk := SSEChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: "viktor",
			Choices: []SSEChoice{{Index: 0, Delta: Delta{}}}}
		data, _ := json.Marshal(roleChunk)
		fmt.Fprintf(w, "data: %s\n\n", data)

		if text != "" {
			textChunk := SSEChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: "viktor",
				Choices: []SSEChoice{{Index: 0, Delta: Delta{Content: text}}}}
			data, _ = json.Marshal(textChunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}

		doneChunk := SSEChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: "viktor",
			Choices: []SSEChoice{{Index: 0, Delta: Delta{}, FinishReason: "stop"}}}
		data, _ = json.Marshal(doneChunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		fmt.Fprintf(w, "data: [DONE]\n\n")

		if ok { flusher.Flush() }
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id": fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
		"object": "chat.completion", "created": time.Now().Unix(), "model": "viktor",
		"choices": []map[string]interface{}{{
			"index": 0,
			"message": map[string]string{"role": "assistant", "content": text},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	})
}

// Типы для OpenAI API

type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// Message.Content может быть строкой или массивом частей — оба варианта валидны в OpenAI API.
// OpenCode шлёт массив: [{"type":"text","text":"..."}]
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// Text извлекает текст из Content независимо от формата (string или array)
func (m Message) Text() string {
	if len(m.Content) == 0 {
		return ""
	}
	// Попытка 1: просто строка "..."
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		return s
	}
	// Попытка 2: массив частей [{"type":"text","text":"..."}]
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Content, &parts); err == nil {
		var sb strings.Builder
		for _, p := range parts {
			if p.Type == "text" || p.Text != "" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return ""
}

type ChatCompletionResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []Choice       `json:"choices"`
}

type Choice struct {
	Index        int             `json:"index"`
	Message      AssistantMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// AssistantMessage — исходящий, content всегда строка
type AssistantMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type SSEChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []SSEChoice `json:"choices"`
}

type SSEChoice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type Delta struct {
	Content string `json:"content,omitempty"`
}
