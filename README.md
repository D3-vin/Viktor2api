# 🤖 Viktor2api

OpenAI-compatible proxy to Viktor AI agent for use with OpenCode.

[![Version](https://img.shields.io/badge/version-1.1.0-blue)](https://github.com/D3-vin/Viktor2api/releases)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[![Telegram Channel](https://img.shields.io/badge/Telegram-Channel-blue?logo=telegram)](https://t.me/D3_vin)
[![Telegram Chat](https://img.shields.io/badge/Telegram-Chat-blue?logo=telegram)](https://t.me/D3_vin_chat)
[![GitHub](https://img.shields.io/badge/GitHub-Repository-black?logo=github)](https://github.com/D3-vin/Viktor2api)

[Features](#features) • [Quick Start](#quick-start) • [Usage](#usage) • [Configuration](#configuration) • [Contact](#contact)

[English](#) | [Русский](README.ru.md)

---

## Features

- 🔄 **Real-time streaming** — Viktor's responses stream directly into OpenCode as they arrive
- 🧠 **Reasoning blocks** — streams Viktor's thinking process in real time (requires JWT)
- 📁 **Auto file download** — Viktor's generated files saved to `viktor_media/` automatically
- 🔀 **Git sync** — Viktor sees your code, his changes appear on disk via `git pull`
- 🖥️ **Dashboard** — status, model selector, reasoning level, session activity at `localhost:6061/dashboard`
- 🔑 **Chrome extension** — extract Slack tokens and JWT without DevTools
- 🌐 **Cross-platform** — Windows, macOS, Linux
- 📦 **Standalone** — single binary, no dependencies

---

## Quick Start

### 1. Download

Download the latest release for your platform:
- [Windows (64-bit)](https://github.com/D3-vin/Viktor2api/releases/latest)
- [Linux (64-bit)](https://github.com/D3-vin/Viktor2api/releases/latest)
- [macOS Intel](https://github.com/D3-vin/Viktor2api/releases/latest)
- [macOS Apple Silicon](https://github.com/D3-vin/Viktor2api/releases/latest)

Also grab `config.yaml.example` from the release.

Or build from source:
```bash
git clone https://github.com/D3-vin/Viktor2api.git
cd Viktor2api
go build -o viktor2api .
```

### 2. Configure

Install the Chrome extension (`chrome-extension/` folder) — fastest way to get credentials:
1. `chrome://extensions` → Enable Developer mode → Load unpacked → select `chrome-extension/`
2. Open `app.slack.com` → click extension → **Slack tab** → **Read** → **Send to proxy**
3. Open `app.viktor.com` → click extension → **JWT tab** → **Read JWT** → **Send to proxy**

Or manually copy `config.yaml.example` to `config.yaml` and fill in:

```yaml
viktor:
  slack_token: "xoxc-..."      # DevTools → Network → any Slack API request → Payload → token
  slack_cookie_d: "xoxd-..."   # DevTools → Application → Cookies → slack.com → cookie "d"
  viktor_dm: "D..."            # Slack URL when DM is open: /client/T.../D... → last segment
  viktor_user: "U..."          # Viktor profile → ⋮ → Copy member ID
  jwt: "eyJ..."                # Optional but recommended — app.viktor.com → DevTools → Network
```

### 3. Run

```bash
./viktor2api
# or on Windows:
viktor2api-windows-amd64.exe
# or from source:
go run .
```

### 4. Connect in OpenCode

Add to `opencode.json` (project root or `~/.config/opencode/opencode.json`):

```json
{
  "provider": {
    "viktor": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Viktor",
      "options": {
        "baseURL": "http://127.0.0.1:6061/v1",
        "apiKey": "viktor"
      },
      "models": { "viktor": { "name": "Viktor" } }
    }
  }
}
```

Select model **`viktor/viktor`** in OpenCode and restart it.

---

## Usage

### Basic

Open any project in OpenCode, select `viktor/viktor` model and start chatting. Viktor receives your task via Slack DM and responds directly in OpenCode.

### Dashboard

Open `http://127.0.0.1:6061/dashboard` to:
- Check proxy status and configuration
- **Change Viktor's model and reasoning level** (N/L/M/H) without opening app.viktor.com
- View recent session activity

### Git Sync

When enabled, Viktor sees your local code and his file changes appear on disk automatically.

```bash
winget install GitHub.cli   # Windows
# brew install gh           # macOS
gh auth login
```

Then connect GitHub on `app.viktor.com` → **Settings** → **Integrations**.

Enable in `config.yaml`:
```yaml
git:
  enabled: true
  github_owner: "your-username"
```

### Session commands (type in OpenCode chat)

| Command | Effect |
|---|---|
| `/nopull` | Disable git pull for this session |
| `/pullask` | Prompt before each pull (default) |
| `/pullauto` | Always pull automatically |

---

## Configuration

See [`config.yaml.example`](config.yaml.example) for all options. Key settings:

```yaml
proxy:
  port: 6061              # change if port is taken

viktor:
  poll_interval: 3        # seconds between Slack polls
  response_quiet: 25      # seconds of silence = response complete
  response_hard_cap: 900  # max wait time (15 min)

sessions:
  max_turns: 8            # turns before auto-compact (0 = disabled)
```

---

## Troubleshooting

### `slack error: invalid_auth`
Tokens expired. Open `app.slack.com` → Chrome extension → **Slack tab** → **Read** → **Send to proxy**. No restart needed.

### JWT errors / reasoning not working
JWT expired (~14 days). Open `app.viktor.com` → Chrome extension → **JWT tab** → **Read JWT** → **Send to proxy**.

### Viktor doesn't push files / "no GitHub access"
Connect GitHub on `app.viktor.com` → Settings → Integrations → GitHub.

### OpenCode doesn't see the model
Restart OpenCode after adding the provider to `opencode.json`.

---

## Project Structure

```
Viktor2api/
├── main.go                      # Entry point
├── config.yaml                  # Your config (gitignored)
├── config.yaml.example          # Template
├── chrome-extension/            # Browser extension
├── internal/
│   ├── config/                  # Config loading + in-place YAML patching
│   ├── server/                  # HTTP handlers, streaming, dashboard
│   ├── slack/                   # Slack API (post, fetch, download files)
│   ├── git/                     # GitHub sync
│   ├── sessions/                # Session state, auto-compact
│   └── viktorapi/               # Viktor internal API (completion, reasoning, models)
└── .github/workflows/           # Auto-release CI/CD
```

---

## Contact

- **GitHub**: https://github.com/D3-vin/Viktor2api
- **Telegram**: [@D3_vin](https://t.me/D3_vin)
- **Author**: [@D3vin_dev](https://t.me/D3vin_dev)

---

## License

MIT License — see [LICENSE](LICENSE) file

## ⚠️ Disclaimer

This software is provided **for educational and interoperability purposes only**, "AS IS", without warranty of any kind. The tool uses browser session tokens from your own Slack/Viktor account — use at your own risk and in accordance with Slack's and Viktor's Terms of Service. The author is not responsible for any misuse or consequences. Not affiliated with Slack or Viktor.

> ПО предоставляется исключительно в образовательных и ознакомительных целях, «как есть». Автор не несёт ответственности за использование.
