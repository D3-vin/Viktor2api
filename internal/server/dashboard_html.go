package server

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Viktor2api</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0d1117; color: #c9d1d9; min-height: 100vh; padding: 24px; }
.header { display: flex; align-items: center; gap: 12px; margin-bottom: 24px; }
.header h1 { font-size: 20px; font-weight: 600; color: #e6edf3; }
.header .sub { font-size: 13px; color: #8b949e; }
.dot { width: 10px; height: 10px; border-radius: 50%; background: #56d364; box-shadow: 0 0 8px #56d364; animation: pulse 2s infinite; }
@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.5} }
.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 16px; }
.card { background: #161b22; border: 1px solid #21262d; border-radius: 8px; padding: 18px; }
.card-title { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: #8b949e; margin-bottom: 14px; }
.row { display: flex; justify-content: space-between; align-items: center; padding: 6px 0; border-bottom: 1px solid #21262d; }
.row:last-child { border-bottom: none; }
.row-label { font-size: 13px; color: #8b949e; }
.row-value { font-size: 13px; color: #e6edf3; font-weight: 500; }
.badge { padding: 2px 8px; border-radius: 12px; font-size: 11px; font-weight: 600; }
.badge-green { background: #1a3a1a; color: #56d364; border: 1px solid #238636; }
.badge-red   { background: #3a1a1a; color: #f85149; border: 1px solid #da3633; }
.badge-gray  { background: #21262d; color: #8b949e; border: 1px solid #30363d; }
.badge-blue  { background: #1a2a3a; color: #79c0ff; border: 1px solid #1f6feb; }
.model-name { font-family: monospace; font-size: 13px; color: #79c0ff; }
.loading { color: #484f58; font-style: italic; font-size: 13px; }
.session-list { max-height: 200px; overflow-y: auto; }
.session-item { padding: 8px; background: #0d1117; border-radius: 4px; margin-bottom: 6px; font-size: 12px; }
.session-id { font-family: monospace; color: #58a6ff; }
.session-meta { color: #8b949e; margin-top: 2px; }
.refresh-btn { background: none; border: 1px solid #30363d; color: #8b949e; padding: 4px 12px; border-radius: 6px; font-size: 12px; cursor: pointer; margin-left: auto; display: block; margin-top: 12px; transition: all .15s; }
.refresh-btn:hover { border-color: #58a6ff; color: #58a6ff; }
.endpoint { font-family: monospace; font-size: 12px; color: #56d364; }
.acct-item { display: flex; align-items: center; gap: 8px; padding: 8px 10px; background: #0d1117; border: 1px solid #21262d; border-radius: 6px; margin-bottom: 6px; }
.acct-item.is-active { border-color: #238636; }
.acct-name { font-size: 13px; font-weight: 500; color: #e6edf3; flex: 1; }
.acct-meta { font-size: 11px; color: #8b949e; }
.acct-btn { background: none; border: 1px solid #30363d; color: #8b949e; padding: 3px 10px; border-radius: 4px; font-size: 11px; cursor: pointer; transition: all .15s; }
.acct-btn:hover { border-color: #58a6ff; color: #58a6ff; }
.acct-btn.danger:hover { border-color: #da3633; color: #f85149; }
input.f { width: 100%; background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; font-size: 12px; padding: 6px 8px; outline: none; margin-bottom: 6px; }
select.f { width: 100%; background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; padding: 6px 8px; font-size: 12px; outline: none; }
.btn-green { background: #238636; border: none; color: #fff; padding: 7px; border-radius: 6px; font-size: 12px; cursor: pointer; font-weight: 500; width: 100%; margin-top: 4px; }
.status-msg { font-size: 11px; color: #8b949e; text-align: center; min-height: 14px; margin-top: 4px; }
details summary { cursor: pointer; font-size: 12px; color: #8b949e; user-select: none; margin-top: 10px; }
</style>
</head>
<body>

<div class="header">
  <div class="dot"></div>
  <div>
    <h1>Viktor2api</h1>
    <div class="sub" id="uptimeStr">loading…</div>
  </div>
</div>

<div class="grid">

  <!-- Status -->
  <div class="card">
    <div class="card-title">Status</div>
    <div class="row"><span class="row-label">Proxy</span><span class="badge badge-green">running</span></div>
    <div class="row"><span class="row-label">Port</span><span class="row-value" id="portVal">—</span></div>
    <div class="row"><span class="row-label">Endpoint</span><span class="endpoint" id="endpointVal">—</span></div>
    <div class="row"><span class="row-label">Go</span><span class="row-value" id="goVer">—</span></div>
    <div class="row"><span class="row-label">Active account</span><span class="row-value" id="activeAcct">—</span></div>
  </div>

  <!-- Account status -->
  <div class="card">
    <div class="card-title">Account</div>
    <div class="row"><span class="row-label">Slack credentials</span><span class="badge" id="cfgSlack">—</span></div>
    <div class="row"><span class="row-label">Viktor DM / User</span><span class="badge" id="cfgViktor">—</span></div>
    <div class="row"><span class="row-label">JWT (Viktor API)</span><span class="badge" id="cfgJwt">—</span></div>
    <div class="row"><span class="row-label">Git sync</span><span class="badge" id="cfgGit">—</span></div>
    <div class="row"><span class="row-label">Credits</span><span class="row-value" id="creditsVal"><span class="loading">loading…</span></span></div>
    <button class="refresh-btn" onclick="loadCredits()">↻ Refresh credits</button>
  </div>

  <!-- Model -->
  <div class="card">
    <div class="card-title">Viktor Model</div>
    <div class="row"><span class="row-label">Current</span><span class="model-name" id="modelName"><span class="loading">loading…</span></span></div>
    <div class="row"><span class="row-label">Reasoning</span><span class="row-value" id="modelReasoning">—</span></div>
    <div style="margin-top:12px;display:flex;flex-direction:column;gap:6px">
      <select id="modelSelect" class="f"><option value="">— select model —</option></select>
      <select id="reasoningSelect" class="f">
        <option value="N">Reasoning: None</option>
        <option value="L">Reasoning: Low</option>
        <option value="M">Reasoning: Medium</option>
        <option value="H">Reasoning: High</option>
      </select>
      <button class="btn-green" onclick="applyModel()">✓ Apply</button>
      <div class="status-msg" id="modelStatus"></div>
    </div>
    <button class="refresh-btn" onclick="loadModel()">↻ Refresh</button>
  </div>

  <!-- Sessions -->
  <div class="card">
    <div class="card-title">Recent Sessions</div>
    <div class="session-list" id="sessionList"><span class="loading">loading…</span></div>
    <button class="refresh-btn" onclick="loadSessions()">↻ Refresh</button>
  </div>

  <!-- Accounts management -->
  <div class="card" style="grid-column: 1 / -1;">
    <div class="card-title">Accounts</div>
    <div id="accountsList"></div>
    <details>
      <summary>+ Add / Update account</summary>
      <div style="margin-top:10px">
        <input id="acctName"   class="f" placeholder="Account name (required)">
        <input id="acctToken"  class="f" placeholder="slack_token (xoxc)">
        <input id="acctCookie" class="f" placeholder="slack_cookie_d (xoxd)">
        <input id="acctDM"     class="f" placeholder="viktor_dm (D...)">
        <input id="acctUser"   class="f" placeholder="viktor_user (U...)">
        <input id="acctJWT"    class="f" placeholder="jwt (eyJ...) — optional">
        <button class="btn-green" onclick="saveAccount()">Save account</button>
        <div class="status-msg" id="acctStatus"></div>
      </div>
    </details>
    <button class="refresh-btn" onclick="loadAccounts()">↻ Refresh</button>
  </div>

</div>

<script>
const api = (path) => fetch(path).then(r => r.json()).catch(() => null);
const post = (path, body) => fetch(path, {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify(body)}).then(r => r.json()).catch(() => null);

function badgeHtml(ok, yes, no) {
  return ok
    ? '<span class="badge badge-green">' + yes + '</span>'
    : '<span class="badge badge-red">' + no + '</span>';
}

function reasoningLabel(l) {
  return {N:'None', L:'Low', M:'Medium', H:'High'}[l] || l || '—';
}

// ── Status ──────────────────────────────────
async function loadStatus() {
  const d = await api('/api/dashboard');
  if (!d) return;
  document.getElementById('uptimeStr').textContent = 'uptime: ' + d.uptime;
  document.getElementById('portVal').textContent = d.port;
  document.getElementById('endpointVal').textContent = 'http://127.0.0.1:' + d.port + '/v1';
  document.getElementById('goVer').textContent = d.go_version;
  document.getElementById('activeAcct').textContent = d.active_account || '—';

  function setBadge(id, ok, yes, no, yesClass, noClass) {
    const el = document.getElementById(id);
    if (!el) return;
    el.className = 'badge ' + (ok ? (yesClass || 'badge-green') : (noClass || 'badge-red'));
    el.textContent = ok ? yes : no;
  }
  setBadge('cfgSlack',  d.config.slack,  '✓ ok',      '✗ missing',  'badge-green', 'badge-red');
  setBadge('cfgViktor', d.config.viktor, '✓ ok',      '✗ missing',  'badge-green', 'badge-red');
  setBadge('cfgJwt',    d.config.jwt,    '✓ set',     '— not set',  'badge-green', 'badge-gray');
  setBadge('cfgGit',    d.config.git,    '✓ enabled', '— disabled', 'badge-blue',  'badge-gray');
}

// ── Credits ──────────────────────────────────
async function loadCredits() {
  document.getElementById('creditsVal').innerHTML = '<span class="loading">loading…</span>';
  const d = await api('/api/dashboard/credits');
  if (!d || d.error) {
    document.getElementById('creditsVal').innerHTML = '<span class="loading">' + (d && d.error ? d.error : 'unavailable') + '</span>';
    return;
  }
  const c = d.credits;
  const color = c > 1000 ? '#56d364' : c > 500 ? '#e3b341' : '#f85149';
  document.getElementById('creditsVal').innerHTML = '<span style="color:' + color + ';font-weight:600">' + c.toLocaleString() + '</span>';
}

// ── Model ──────────────────────────────────
async function loadModel() {
  document.getElementById('modelName').innerHTML = '<span class="loading">loading…</span>';
  document.getElementById('modelReasoning').textContent = '—';
  const d = await api('/api/dashboard/model');
  if (!d || d.error) {
    document.getElementById('modelName').innerHTML = '<span class="loading">' + ((d && d.error) || 'unavailable') + '</span>';
    return;
  }
  document.getElementById('modelName').textContent = d.model || '—';
  document.getElementById('modelReasoning').textContent = reasoningLabel(d.reasoning_level);
  const sel = document.getElementById('modelSelect');
  sel.innerHTML = '';
  const models = d.available || [];
  if (!models.length && d.model) models.push({id: d.model, name: d.model});
  models.forEach(m => {
    const opt = document.createElement('option');
    opt.value = m.id; opt.textContent = m.name || m.id;
    if (m.id === d.model) opt.selected = true;
    sel.appendChild(opt);
  });
  for (const opt of document.getElementById('reasoningSelect').options)
    if (opt.value === d.reasoning_level) { opt.selected = true; break; }
}

async function applyModel() {
  const model = document.getElementById('modelSelect').value;
  const level = document.getElementById('reasoningSelect').value;
  const st = document.getElementById('modelStatus');
  if (!model) { st.textContent = 'Select a model first'; return; }
  st.style.color = '#79c0ff'; st.textContent = 'Applying…';
  const res = await post('/api/models/set', {model, reasoning_level: level});
  if (res && res.ok) {
    st.style.color = '#56d364'; st.textContent = '✓ Applied';
    setTimeout(loadModel, 500);
  } else {
    st.style.color = '#f85149'; st.textContent = '✗ ' + ((res && res.error) || 'error');
  }
}

// ── Sessions ──────────────────────────────────
async function loadSessions() {
  document.getElementById('sessionList').innerHTML = '<span class="loading">loading…</span>';
  const sessions = await api('/api/dashboard/sessions');
  if (!sessions || !sessions.length) {
    document.getElementById('sessionList').innerHTML = '<span class="loading">no sessions yet</span>';
    return;
  }
  document.getElementById('sessionList').innerHTML = sessions.map(s => {
    const ago = s.last ? timeAgo(s.last) : '—';
    const path = s.path ? s.path.replace(/\\/g, '/').split('/').pop() : '';
    return '<div class="session-item"><div class="session-id">' + s.hash + '</div>' +
      '<div class="session-meta">turns: ' + s.turns + ' · ' + ago + (path ? ' · ' + path : '') + '</div></div>';
  }).join('');
}

// ── Accounts ──────────────────────────────────
async function loadAccounts() {
  const d = await api('/api/accounts');
  if (!d) return;
  const list = d.accounts || [];
  if (!list.length) {
    document.getElementById('accountsList').innerHTML = '<span class="loading">no accounts</span>';
    return;
  }
  document.getElementById('accountsList').innerHTML = list.map(a => {
    const activeClass = a.active ? ' is-active' : '';
    const activeBadge = a.active ? '<span class="badge badge-green" style="margin-right:6px">active</span>' : '';
    const meta = (a.has_slack ? 'slack ✓' : 'slack ✗') + ' · ' + (a.has_jwt ? 'jwt ✓' : 'no jwt');
    const switchBtn = !a.active
      ? '<button class="acct-btn" onclick="switchAccount(this)" data-name="' + a.name.replace(/"/g,'&quot;') + '">Switch</button>'
      : '';
    const removeBtn = '<button class="acct-btn danger" onclick="removeAccount(this)" data-name="' + a.name.replace(/"/g,'&quot;') + '">Remove</button>';
    return '<div class="acct-item' + activeClass + '">' +
      activeBadge +
      '<div><div class="acct-name">' + a.name + '</div><div class="acct-meta">' + meta + '</div></div>' +
      '<div style="display:flex;gap:4px">' + switchBtn + removeBtn + '</div>' +
      '</div>';
  }).join('');
}

async function switchAccount(btn) {
  const name = btn.dataset.name;
  const res = await post('/api/accounts/activate', {name});
  if (res && res.ok) {
    loadAccounts();
    loadStatus();
    loadCredits();
  } else {
    alert('Failed to switch: ' + (res && res.error ? res.error : 'unknown error'));
  }
}

async function removeAccount(btn) {
  const name = btn.dataset.name;
  if (!confirm('Remove account "' + name + '"?')) return;
  const res = await post('/api/accounts/remove', {name});
  if (res) loadAccounts();
}

async function saveAccount() {
  const name = document.getElementById('acctName').value.trim();
  const st = document.getElementById('acctStatus');
  if (!name) { st.style.color = '#f85149'; st.textContent = 'Name is required'; return; }
  const body = {
    name,
    slack_token:    document.getElementById('acctToken').value.trim(),
    slack_cookie_d: document.getElementById('acctCookie').value.trim(),
    viktor_dm:      document.getElementById('acctDM').value.trim(),
    viktor_user:    document.getElementById('acctUser').value.trim(),
    jwt:            document.getElementById('acctJWT').value.trim(),
  };
  st.style.color = '#79c0ff'; st.textContent = 'Saving…';
  const res = await post('/api/accounts', body);
  if (res && res.ok) {
    st.style.color = '#56d364'; st.textContent = '✓ Saved';
    ['acctName','acctToken','acctCookie','acctDM','acctUser','acctJWT'].forEach(id => document.getElementById(id).value = '');
    loadAccounts();
  } else {
    st.style.color = '#f85149'; st.textContent = '✗ ' + ((res && res.error) || 'error');
  }
}

// ── Helpers ──────────────────────────────────
function timeAgo(ts) {
  const diff = Math.floor(Date.now() / 1000 - ts);
  if (diff < 60) return diff + 's ago';
  if (diff < 3600) return Math.floor(diff/60) + 'm ago';
  return Math.floor(diff/3600) + 'h ago';
}

// ── Init ──────────────────────────────────────
loadStatus();
loadCredits();
loadModel();
loadSessions();
loadAccounts();
setInterval(loadStatus, 10000);
</script>
</body>
</html>
`
