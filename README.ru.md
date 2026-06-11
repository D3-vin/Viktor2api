# 🤖 Viktor2api

OpenAI-совместимый прокси к Viktor AI агенту для работы с OpenCode.

[![Version](https://img.shields.io/badge/version-1.1.0-blue)](https://github.com/D3-vin/Viktor2api/releases)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

[![Telegram Channel](https://img.shields.io/badge/Telegram-Channel-blue?logo=telegram)](https://t.me/D3_vin)
[![Telegram Chat](https://img.shields.io/badge/Telegram-Chat-blue?logo=telegram)](https://t.me/D3_vin_chat)
[![GitHub](https://img.shields.io/badge/GitHub-Repository-black?logo=github)](https://github.com/D3-vin/Viktor2api)

[Возможности](#возможности) • [Быстрый старт](#быстрый-старт) • [Использование](#использование) • [Конфигурация](#конфигурация) • [Контакты](#контакты)

[English](README.md) | [Русский](#)

---

## Возможности

- 🔄 **Стриминг в реальном времени** — ответы Viktor'а приходят в OpenCode по мере готовности
- 🧠 **Reasoning-блоки** — стриминг мыслительного процесса Viktor'а (требует JWT)
- 📁 **Автоскачивание файлов** — сгенерированные файлы сохраняются в `viktor_media/`
- 🔀 **Git-синхронизация** — Viktor видит твой код, его правки появляются на диске
- 🖥️ **Дашборд** — статус, выбор модели, уровень reasoning, история сессий на `localhost:6061/dashboard`
- 🔑 **Chrome-расширение** — извлечение токенов Slack и JWT без DevTools
- 🌐 **Кросс-платформенный** — Windows, macOS, Linux
- 📦 **Standalone** — один бинарник, никаких зависимостей

---

## Быстрый старт

### 1. Скачать

Готовые бинарники для всех платформ:
- [Windows (64-bit)](https://github.com/D3-vin/Viktor2api/releases/latest)
- [Linux (64-bit)](https://github.com/D3-vin/Viktor2api/releases/latest)
- [macOS Intel](https://github.com/D3-vin/Viktor2api/releases/latest)
- [macOS Apple Silicon](https://github.com/D3-vin/Viktor2api/releases/latest)

Там же скачай `config.yaml.example`.

Или собрать из исходников:
```bash
git clone https://github.com/D3-vin/Viktor2api.git
cd Viktor2api
go build -o viktor2api .
```

### 2. Настроить

Установи Chrome-расширение (папка `chrome-extension/`) — самый быстрый способ:
1. `chrome://extensions` → Режим разработчика → Загрузить распакованное → папка `chrome-extension/`
2. Открой `app.slack.com` → расширение → вкладка **Slack** → **Read** → **Send to proxy**
3. Открой `app.viktor.com` → расширение → вкладка **JWT** → **Read JWT** → **Send to proxy**

Или вручную скопируй `config.yaml.example` в `config.yaml` и заполни:

```yaml
viktor:
  slack_token: "xoxc-..."      # DevTools → Network → любой Slack API запрос → Payload → token
  slack_cookie_d: "xoxd-..."   # DevTools → Application → Cookies → slack.com → кука "d"
  viktor_dm: "D..."            # URL Slack в открытом DM: /client/T.../D... → последний сегмент
  viktor_user: "U..."          # Профиль Viktor → ⋮ → Copy member ID
  jwt: "eyJ..."                # Опционально, но рекомендуется — app.viktor.com → DevTools → Network
```

### 3. Запустить

```bash
./viktor2api
# или на Windows:
viktor2api-windows-amd64.exe
# или из исходников:
go run .
```

### 4. Подключить в OpenCode

Добавить в `opencode.json` (корень проекта или `~/.config/opencode/opencode.json`):

> Windows: `C:\Users\<имя>\.config\opencode\opencode.json`

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

Выбери модель **`viktor/viktor`** в OpenCode и перезапусти его.

---

## Использование

### Основное

Открой любой проект в OpenCode, выбери модель `viktor/viktor` и начинай чат. Viktor получает задачу через Slack DM и отвечает прямо в OpenCode.

### Дашборд

Открой `http://127.0.0.1:6061/dashboard`:
- Статус прокси и конфигурации
- **Смена модели Viktor и уровня reasoning** (N/L/M/H) без входа на app.viktor.com
- История активных сессий

### Git-синхронизация

Viktor видит твой локальный код, его правки появляются на диске автоматически.

```bash
winget install GitHub.cli   # Windows
# brew install gh           # macOS
gh auth login
```

Затем подключи GitHub на `app.viktor.com` → **Settings** → **Integrations**.

Включи в `config.yaml`:
```yaml
git:
  enabled: true
  github_owner: "твой-username"
```

### Команды сессии (вводить в чате OpenCode)

| Команда | Эффект |
|---|---|
| `/nopull` | Отключить git pull в этой сессии |
| `/pullask` | Спрашивать перед pull (по умолчанию) |
| `/pullauto` | Тянуть автоматически |

---

## Конфигурация

Все параметры — в [`config.yaml.example`](config.yaml.example). Основные:

```yaml
proxy:
  port: 6061              # изменить если порт занят

viktor:
  poll_interval: 3        # секунды между опросами Slack
  response_quiet: 25      # секунд тишины = ответ завершён
  response_hard_cap: 900  # максимум ожидания (15 мин)

sessions:
  max_turns: 8            # ходов до авто-компакта (0 = отключено)
```

---

## Решение проблем

### `slack error: invalid_auth`
Токены протухли. Открой `app.slack.com` → расширение → **Slack** → **Read** → **Send to proxy**. Перезапуск не нужен.

### JWT ошибки / reasoning не работает
JWT протух (~14 дней). Открой `app.viktor.com` → расширение → **JWT** → **Read JWT** → **Send to proxy**.

### Viktor не пушит файлы / "no GitHub access"
Подключи GitHub на `app.viktor.com` → Settings → Integrations → GitHub.

### OpenCode не видит модель
Перезапусти OpenCode после добавления провайдера в `opencode.json`.

---

## Структура проекта

```
Viktor2api/
├── main.go                      # Точка входа
├── config.yaml                  # Твой конфиг (gitignored)
├── config.yaml.example          # Шаблон
├── chrome-extension/            # Chrome-расширение
├── internal/
│   ├── config/                  # Загрузка конфига + патчинг YAML
│   ├── server/                  # HTTP обработчики, стриминг, дашборд
│   ├── slack/                   # Slack API (отправка, чтение, скачивание)
│   ├── git/                     # GitHub синхронизация
│   ├── sessions/                # Состояние сессий, авто-компакт
│   └── viktorapi/               # Viktor internal API (завершение, reasoning, модели)
└── .github/workflows/           # CI/CD для авто-релизов
```

---

## Контакты

- **GitHub**: https://github.com/D3-vin/Viktor2api
- **Telegram**: [@D3_vin](https://t.me/D3_vin)
- **Автор**: [@D3vin_dev](https://t.me/D3vin_dev)

---

## Лицензия

MIT License — см. файл [LICENSE](LICENSE)

---

> **⚠️ Дисклеймер**: Инструмент использует токены браузерной сессии Slack. Используй на свой страх и риск в соответствии с правилами использования Slack.
