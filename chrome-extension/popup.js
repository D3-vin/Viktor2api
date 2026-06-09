'use strict';

const state = { token: '', cookie: '', dm: '', user: '', jwt: '' };
const $ = id => document.getElementById(id);

// ── Helpers ────────────────────────────────────────────────────
function setField(id, value) {
  const el = $(id);
  if (!el) return;
  if (value) {
    el.className = 'field-value ok';
    el.textContent = value.length > 60 ? value.slice(0, 30) + '…' + value.slice(-16) : value;
  } else {
    el.className = 'field-value empty';
    el.textContent = 'not found';
  }
}

function setStatus(text, type) {
  const el = $('status');
  el.textContent = text;
  el.className = 'status' + (type ? ' ' + type : '');
}

function updateButtons() {
  const hasSlack = !!(state.token || state.cookie);
  const hasJwt   = !!state.jwt;
  $('btnCopySlack').disabled = !hasSlack;
  $('btnSendSlack').disabled = !hasSlack;
  $('btnCopyJwt').disabled   = !hasJwt;
  $('btnSendJwt').disabled   = !hasJwt;
}

// ── Proxy ──────────────────────────────────────────────────────
function proxyBase() {
  return `http://127.0.0.1:${($('portInput').value || '6061').trim()}`;
}

async function checkProxy() {
  const badge = $('proxyBadge');
  try {
    const r = await fetch(`${proxyBase()}/health`, { signal: AbortSignal.timeout(2000) });
    if (r.ok) { badge.textContent = '🟢 online'; badge.className = 'proxy-badge online'; return true; }
  } catch (_) {}
  badge.textContent = '🔴 offline'; badge.className = 'proxy-badge offline';
  return false;
}

// ── Detect Slack ───────────────────────────────────────────────
async function detectSlack() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.url?.includes('slack.com')) {
    setStatus('❌ Open app.slack.com', 'err'); return;
  }
  setStatus('🔍 Reading Slack…', 'run');

  // DM from URL
  const dmMatch = tab.url.match(/\/client\/T[A-Z0-9]+\/(D[A-Z0-9]+)/);
  if (dmMatch) state.dm = dmMatch[1];

  // Cookie d
  for (const domain of ['https://app.slack.com', 'https://slack.com']) {
    try {
      const c = await new Promise(r => chrome.cookies.get({ url: domain, name: 'd' }, r));
      if (c?.value) { state.cookie = c.value; break; }
    } catch (_) {}
  }

  // Token from localStorage
  try {
    const res = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: () => {
        try {
          const raw = localStorage.localConfig_v2;
          if (raw) {
            const cfg = JSON.parse(raw);
            if (cfg?.teams) {
              for (const id of Object.keys(cfg.teams)) {
                const t = cfg.teams[id];
                if (t?.token?.startsWith('xoxc-')) return t.token;
              }
            }
          }
        } catch (_) {}
        for (let i = 0; i < localStorage.length; i++) {
          try {
            const v = localStorage.getItem(localStorage.key(i));
            const m = v?.match(/\b(xoxc-[A-Za-z0-9\-_]+)\b/);
            if (m) return m[1];
          } catch (_) {}
        }
        return null;
      },
    });
    if (res?.[0]?.result) state.token = res[0].result;
  } catch (_) {}

  // Viktor user via API
  if (state.token) {
    try {
      const r = await fetch('https://slack.com/api/conversations.list', {
        method: 'POST',
        headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
        body: new URLSearchParams({ token: state.token, types: 'im', limit: '200' }),
      });
      const json = await r.json();
      for (const ch of json.channels || []) {
        if (!ch.user) continue;
        if (state.dm && ch.id === state.dm) { state.user = ch.user; break; }
        try {
          const ui = await fetch('https://slack.com/api/users.info', {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
            body: new URLSearchParams({ token: state.token, user: ch.user }),
          });
          const uJson = await ui.json();
          const name = (uJson.user?.real_name || uJson.user?.name || '').toLowerCase();
          if (name.includes('viktor')) { if (!state.dm) state.dm = ch.id; state.user = ch.user; break; }
        } catch (_) {}
      }
    } catch (_) {}
  }

  setField('v-token',  state.token);
  setField('v-cookie', state.cookie);
  setField('v-dm',     state.dm);
  setField('v-user',   state.user);

  const found = [state.token, state.cookie, state.dm, state.user].filter(Boolean).length;
  setStatus(found === 4 ? '✅ All fields found' : `⚠️ Found ${found}/4`, found === 4 ? 'ok' : '');
  updateButtons();
}

// ── Detect JWT ─────────────────────────────────────────────────
async function detectJwt() {
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tab?.url?.includes('viktor.com')) {
    setStatus('❌ Open app.viktor.com', 'err'); return;
  }
  setStatus('🔍 Looking for JWT…', 'run');

  // 1. All cookies
  try {
    const all = await new Promise(r => chrome.cookies.getAll({}, r));
    for (const c of all || []) {
      if (c.value?.startsWith('eyJ')) {
        state.jwt = c.value;
        setField('v-jwt', state.jwt);
        setStatus('✅ JWT found in cookies', 'ok');
        updateButtons(); return;
      }
    }
    for (const c of all || []) {
      if (/token|jwt|auth|session|bearer|access/i.test(c.name)
          && c.value?.length > 20 && c.value.length < 5000) {
        state.jwt = c.value;
        setField('v-jwt', state.jwt);
        setStatus(`✅ JWT from cookie "${c.name}"`, 'ok');
        updateButtons(); return;
      }
    }
  } catch (_) {}

  // 2. localStorage / sessionStorage
  if (tab?.id) {
    try {
      const res = await chrome.scripting.executeScript({
        target: { tabId: tab.id },
        func: () => {
          function scan(obj, depth) {
            if (!obj || depth <= 0) return null;
            if (typeof obj === 'string') {
              if (obj.startsWith('eyJ')) return obj;
              const m = obj.match(/\b(eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)\b/);
              if (m) return m[1];
              try { return scan(JSON.parse(obj), depth - 1); } catch (_) {}
              return null;
            }
            if (typeof obj === 'object') {
              for (const v of Object.values(obj)) { const r = scan(v, depth - 1); if (r) return r; }
            }
            return null;
          }
          for (let i = 0; i < localStorage.length; i++) {
            try { const r = scan(localStorage.getItem(localStorage.key(i)), 7); if (r) return r; } catch (_) {}
          }
          for (let i = 0; i < sessionStorage.length; i++) {
            try { const r = scan(sessionStorage.getItem(sessionStorage.key(i)), 7); if (r) return r; } catch (_) {}
          }
          return null;
        },
      });
      const val = res?.[0]?.result;
      if (val) {
        state.jwt = val;
        setField('v-jwt', state.jwt);
        setStatus('✅ JWT found', 'ok');
        updateButtons(); return;
      }
    } catch (_) {}
  }

  setStatus('⚠️ JWT not found. Try Network tab → Authorization header', 'err');
}

// ── Clipboard ──────────────────────────────────────────────────
async function clip(text) {
  try { await navigator.clipboard.writeText(text); return true; } catch (_) {}
  const ta = document.createElement('textarea');
  ta.value = text; ta.style.cssText = 'position:fixed;opacity:0;top:0;left:0';
  document.body.appendChild(ta); ta.focus(); ta.select();
  const ok = document.execCommand('copy');
  document.body.removeChild(ta); return ok;
}

// ── Send to proxy API ──────────────────────────────────────────
async function sendToProxy(data) {
  const online = await checkProxy();
  if (!online) { setStatus('❌ Proxy is not running', 'err'); return false; }
  try {
    const r = await fetch(`${proxyBase()}/api/config`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
      signal: AbortSignal.timeout(5000),
    });
    if (!r.ok) { const t = await r.text(); throw new Error(t); }
    const json = await r.json();
    return json.updated || [];
  } catch (e) {
    setStatus(`❌ ${e.message}`, 'err'); return false;
  }
}

// ── Tabs ───────────────────────────────────────────────────────
function switchTab(name) {
  document.querySelectorAll('.tab-btn').forEach(b => b.classList.toggle('active', b.dataset.tab === name));
  document.querySelectorAll('.tab-pane').forEach(p => p.classList.toggle('active', p.id === 'tab-' + name));
}

// ── Events ─────────────────────────────────────────────────────
document.querySelectorAll('.tab-btn').forEach(b => b.addEventListener('click', () => switchTab(b.dataset.tab)));

$('btnDetectSlack').addEventListener('click', detectSlack);
$('btnDetectJwt').addEventListener('click', detectJwt);
$('portInput').addEventListener('change', checkProxy);

$('btnCopySlack').addEventListener('click', async () => {
  const text = [
    '# viktor: (Slack fields)',
    `slack_token: "${state.token}"`,
    `slack_cookie_d: "${state.cookie}"`,
    `viktor_dm: "${state.dm}"`,
    `viktor_user: "${state.user}"`,
  ].join('\n');
  const ok = await clip(text);
  setStatus(ok ? '✅ Slack fields copied' : '❌ Copy failed', ok ? 'ok' : 'err');
});

$('btnSendSlack').addEventListener('click', async () => {
  setStatus('📤 Sending Slack credentials…', 'run');
  const updated = await sendToProxy({
    slack_token:    state.token,
    slack_cookie_d: state.cookie,
    viktor_dm:      state.dm,
    viktor_user:    state.user,
  });
  if (updated) setStatus(`✅ Saved: ${updated.join(', ')}`, 'ok');
});

$('btnCopyJwt').addEventListener('click', async () => {
  const ok = await clip(state.jwt);
  setStatus(ok ? '✅ JWT copied' : '❌ Copy failed', ok ? 'ok' : 'err');
});

$('btnSendJwt').addEventListener('click', async () => {
  setStatus('📤 Sending JWT…', 'run');
  const updated = await sendToProxy({ jwt: state.jwt });
  if (updated) setStatus(`✅ Saved: ${updated.join(', ')}`, 'ok');
});

// ── Init ───────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', async () => {
  checkProxy();
  // Auto-switch to JWT tab if on viktor.com
  const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
  if (tab?.url?.includes('viktor.com')) {
    switchTab('jwt');
    setStatus('Click "Read JWT"');
  }
});
