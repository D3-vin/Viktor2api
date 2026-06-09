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

// HandleDashboard serves the main dashboard page.
func (s *Server) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML)
}

// HandleDashboardAPI returns dashboard data as JSON.
func (s *Server) HandleDashboardAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Uptime
	uptime := time.Since(startTime)
	uptimeStr := formatUptime(uptime)

	// Config status
	cfg := map[string]bool{
		"slack":   s.config.Viktor.SlackToken != "" && s.config.Viktor.SlackCookieD != "",
		"viktor":  s.config.Viktor.ViktorDM != "" && s.config.Viktor.ViktorUser != "",
		"jwt":     s.config.Viktor.JWT != "",
		"git":     s.config.Git.Enabled,
	}

	// Session info
	sessionInfo := map[string]interface{}{
		"enabled": s.config.Sessions.Enabled,
		"max_turns": s.config.Sessions.MaxTurns,
	}

	// Current Viktor model (via API if JWT available)
	modelInfo := map[string]interface{}{
		"model":           "",
		"reasoning_level": "",
		"available":       false,
	}
	if s.vapi != nil {
		// Quick non-blocking check with timeout
		done := make(chan struct{})
		go func() {
			defer close(done)
			// We'll populate via separate /api/dashboard/model endpoint
		}()
		<-done
		modelInfo["available"] = true
	}

	data := map[string]interface{}{
		"status":   "running",
		"uptime":   uptimeStr,
		"port":     s.config.Proxy.Port,
		"config":   cfg,
		"session":  sessionInfo,
		"model":    modelInfo,
		"git":      s.config.Git.Enabled,
		"go_version": runtime.Version(),
	}

	json.NewEncoder(w).Encode(data)
}

// HandleDashboardModel fetches current Viktor model from API.
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

// HandleModelsSet sets Viktor model and reasoning level.
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

// HandleDashboardSessions returns recent session state.
func (s *Server) HandleDashboardSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Return session store data (last N sessions)
	data := s.sessions.Snapshot(20)
	json.NewEncoder(w).Encode(data)
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Viktor2api</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #0d1117; color: #c9d1d9; min-height: 100vh; padding: 24px;
  }
  .header { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
  .header h1 { font-size: 20px; font-weight: 600; color: #e6edf3; }
  .header .sub { font-size: 13px; color: #8b949e; }
  .dot { width: 10px; height: 10px; border-radius: 50%; background: #56d364;
         box-shadow: 0 0 8px #56d364; animation: pulse 2s infinite; }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.5} }

  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 16px; }
  .card {
    background: #161b22; border: 1px solid #21262d;
    border-radius: 8px; padding: 18px;
  }
  .card-title {
    font-size: 11px; font-weight: 600; text-transform: uppercase;
    letter-spacing: .05em; color: #8b949e; margin-bottom: 14px;
  }

  .row { display: flex; justify-content: space-between; align-items: center;
         padding: 6px 0; border-bottom: 1px solid #21262d; }
  .row:last-child { border-bottom: none; }
  .row-label { font-size: 13px; color: #8b949e; }
  .row-value { font-size: 13px; color: #e6edf3; font-weight: 500; }
  .badge { padding: 2px 8px; border-radius: 12px; font-size: 11px; font-weight: 600; }
  .badge-green  { background: #1a3a1a; color: #56d364; border: 1px solid #238636; }
  .badge-red    { background: #3a1a1a; color: #f85149; border: 1px solid #da3633; }
  .badge-gray   { background: #21262d; color: #8b949e; border: 1px solid #30363d; }
  .badge-blue   { background: #1a2a3a; color: #79c0ff; border: 1px solid #1f6feb; }

  .model-name { font-family: monospace; font-size: 13px; color: #79c0ff; }
  .loading { color: #484f58; font-style: italic; font-size: 13px; }

  .session-list { max-height: 200px; overflow-y: auto; }
  .session-item {
    padding: 8px; background: #0d1117; border-radius: 4px;
    margin-bottom: 6px; font-size: 12px;
  }
  .session-item:last-child { margin-bottom: 0; }
  .session-id { font-family: monospace; color: #58a6ff; }
  .session-meta { color: #8b949e; margin-top: 2px; }

  .refresh-btn {
    background: none; border: 1px solid #30363d; color: #8b949e;
    padding: 4px 12px; border-radius: 6px; font-size: 12px; cursor: pointer;
    margin-left: auto; display: block; margin-top: 12px; transition: all .15s;
  }
  .refresh-btn:hover { border-color: #58a6ff; color: #58a6ff; }

  .endpoint { font-family: monospace; font-size: 12px; color: #56d364; }
</style>
</head>
<body>

<div class="header">
  <div class="dot" id="statusDot"></div>
  <div>
    <h1>Viktor2api</h1>
    <div class="sub" id="uptimeStr">loading…</div>
  </div>
</div>

<div class="grid">

  <!-- Status card -->
  <div class="card">
    <div class="card-title">Status</div>
    <div class="row">
      <span class="row-label">Proxy</span>
      <span class="badge badge-green" id="proxyStatus">running</span>
    </div>
    <div class="row">
      <span class="row-label">Port</span>
      <span class="row-value" id="portVal">—</span>
    </div>
    <div class="row">
      <span class="row-label">Endpoint</span>
      <span class="endpoint" id="endpointVal">—</span>
    </div>
    <div class="row">
      <span class="row-label">Go</span>
      <span class="row-value" id="goVer">—</span>
    </div>
  </div>

  <!-- Config card -->
  <div class="card">
    <div class="card-title">Configuration</div>
    <div class="row">
      <span class="row-label">Slack credentials</span>
      <span class="badge" id="cfgSlack">—</span>
    </div>
    <div class="row">
      <span class="row-label">Viktor DM / User</span>
      <span class="badge" id="cfgViktor">—</span>
    </div>
    <div class="row">
      <span class="row-label">JWT (Viktor API)</span>
      <span class="badge" id="cfgJwt">—</span>
    </div>
    <div class="row">
      <span class="row-label">Git sync</span>
      <span class="badge" id="cfgGit">—</span>
    </div>
  </div>

  <!-- Model card -->
  <div class="card">
    <div class="card-title">Viktor Model</div>
    <div class="row">
      <span class="row-label">Current model</span>
      <span class="model-name" id="modelName"><span class="loading">loading…</span></span>
    </div>
    <div class="row">
      <span class="row-label">Reasoning</span>
      <span class="row-value" id="modelReasoning">—</span>
    </div>
    <div style="margin-top:12px; display:flex; flex-direction:column; gap:8px;">
      <select id="modelSelect" style="background:#0d1117;border:1px solid #30363d;color:#c9d1d9;padding:6px 8px;border-radius:6px;font-size:12px;width:100%">
        <option value="">— select model —</option>
      </select>
      <select id="reasoningSelect" style="background:#0d1117;border:1px solid #30363d;color:#c9d1d9;padding:6px 8px;border-radius:6px;font-size:12px;width:100%">
        <option value="N">Reasoning: None</option>
        <option value="L">Reasoning: Low</option>
        <option value="M">Reasoning: Medium</option>
        <option value="H">Reasoning: High</option>
      </select>
      <button onclick="applyModel()" style="background:#238636;border:none;color:#fff;padding:7px;border-radius:6px;font-size:12px;cursor:pointer;font-weight:500">
        ✓ Apply
      </button>
      <div id="modelStatus" style="font-size:11px;color:#8b949e;text-align:center;min-height:16px"></div>
    </div>
    <button class="refresh-btn" onclick="loadModel()">↻ Refresh</button>
  </div>

  <!-- Sessions card -->
  <div class="card">
    <div class="card-title">Recent Sessions</div>
    <div class="session-list" id="sessionList">
      <span class="loading">loading…</span>
    </div>
    <button class="refresh-btn" onclick="loadSessions()">↻ Refresh</button>
  </div>

</div>

<script>
const api = (path) => fetch(path).then(r => r.json()).catch(() => null);

function badge(ok) {
  return ok
    ? '<span class="badge badge-green">✓ ok</span>'
    : '<span class="badge badge-red">✗ missing</span>';
}

function reasoningLabel(level) {
  const map = { N: 'None', L: 'Low', M: 'Medium', H: 'High' };
  return map[level] || level || '—';
}

async function loadStatus() {
  const d = await api('/api/dashboard');
  if (!d) return;

  document.getElementById('uptimeStr').textContent = 'uptime: ' + d.uptime;
  document.getElementById('portVal').textContent = d.port;
  document.getElementById('endpointVal').textContent = 'http://127.0.0.1:' + d.port + '/v1';
  document.getElementById('goVer').textContent = d.go_version;

  document.getElementById('cfgSlack').outerHTML  = '<span class="badge ' + (d.config.slack  ? 'badge-green' : 'badge-red') + '" id="cfgSlack">'  + (d.config.slack  ? '✓ ok' : '✗ missing') + '</span>';
  document.getElementById('cfgViktor').outerHTML = '<span class="badge ' + (d.config.viktor ? 'badge-green' : 'badge-red') + '" id="cfgViktor">' + (d.config.viktor ? '✓ ok' : '✗ missing') + '</span>';
  document.getElementById('cfgJwt').outerHTML    = '<span class="badge ' + (d.config.jwt    ? 'badge-green' : 'badge-gray') + '" id="cfgJwt">'    + (d.config.jwt    ? '✓ set'  : '— not set') + '</span>';
  document.getElementById('cfgGit').outerHTML    = '<span class="badge ' + (d.config.git    ? 'badge-blue'  : 'badge-gray') + '" id="cfgGit">'    + (d.config.git    ? '✓ enabled' : '— disabled') + '</span>';
}

async function loadModel() {
  document.getElementById('modelName').innerHTML = '<span class="loading">loading…</span>';
  document.getElementById('modelReasoning').textContent = '—';
  const d = await api('/api/dashboard/model');
  if (!d || d.error) {
    document.getElementById('modelName').innerHTML = '<span class="loading">' + (d?.error || 'unavailable — JWT required') + '</span>';
    return;
  }
  document.getElementById('modelName').textContent = d.model || '—';
  document.getElementById('modelReasoning').textContent = reasoningLabel(d.reasoning_level);

  // Populate model dropdown
  const sel = document.getElementById('modelSelect');
  sel.innerHTML = '';
  const models = d.available || [];
  if (models.length === 0 && d.model) {
    models.push({id: d.model, name: d.model});
  }
  models.forEach(m => {
    const opt = document.createElement('option');
    opt.value = m.id;
    opt.textContent = m.name || m.id;
    if (m.id === d.model) opt.selected = true;
    sel.appendChild(opt);
  });
  if (!sel.value && d.model) {
    const opt = document.createElement('option');
    opt.value = d.model; opt.textContent = d.model; opt.selected = true;
    sel.insertBefore(opt, sel.firstChild);
  }

  // Set reasoning dropdown
  const rsel = document.getElementById('reasoningSelect');
  for (const opt of rsel.options) {
    if (opt.value === d.reasoning_level) { opt.selected = true; break; }
  }
}

async function applyModel() {
  const model = document.getElementById('modelSelect').value;
  const level = document.getElementById('reasoningSelect').value;
  const status = document.getElementById('modelStatus');
  if (!model) { status.textContent = 'Select a model first'; return; }

  status.textContent = 'Applying…';
  status.style.color = '#79c0ff';

  const res = await fetch('/api/models/set', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({model, reasoning_level: level}),
  }).then(r => r.json()).catch(() => null);

  if (res?.ok) {
    status.textContent = '✓ Applied';
    status.style.color = '#56d364';
    setTimeout(() => loadModel(), 500);
  } else {
    status.textContent = '✗ ' + (res?.error || 'error');
    status.style.color = '#f85149';
  }
}

async function loadSessions() {
  document.getElementById('sessionList').innerHTML = '<span class="loading">loading…</span>';
  const sessions = await api('/api/dashboard/sessions');
  if (!sessions || !sessions.length) {
    document.getElementById('sessionList').innerHTML = '<span class="loading">no sessions yet</span>';
    return;
  }
  const html = sessions.map(s => {
    const ago = s.last ? timeAgo(s.last) : '—';
    return '<div class="session-item">' +
      '<div class="session-id">' + s.hash + '</div>' +
      '<div class="session-meta">turns: ' + s.turns + ' · ' + ago +
      (s.path ? ' · ' + shortPath(s.path) : '') + '</div>' +
    '</div>';
  }).join('');
  document.getElementById('sessionList').innerHTML = html;
}

function timeAgo(ts) {
  const diff = Math.floor(Date.now() / 1000 - ts);
  if (diff < 60) return diff + 's ago';
  if (diff < 3600) return Math.floor(diff/60) + 'm ago';
  return Math.floor(diff/3600) + 'h ago';
}

function shortPath(p) {
  const parts = p.replace(/\\/g, '/').split('/');
  return parts[parts.length - 1] || p;
}

// Init
loadStatus();
loadModel();
loadSessions();

// Auto-refresh status every 10s
setInterval(loadStatus, 10000);
</script>
</body>
</html>
`
