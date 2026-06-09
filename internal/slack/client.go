package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const baseURL = "https://slack.com/api"

type Client struct {
	token    string
	cookie   string
	dm       string
	userID   string
	httpClient *http.Client
}

type Message struct {
	TS    string       `json:"ts"`
	Text  string       `json:"text"`
	Files []SlackFile  `json:"files,omitempty"`
}

type SlackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Size               int    `json:"size"`
	MimeType           string `json:"mimetype"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
}

type SlackResponse struct {
	OK       bool           `json:"ok"`
	Error    string         `json:"error,omitempty"`
	TS       string         `json:"ts,omitempty"`
	Messages []SlackMessage `json:"messages,omitempty"`
}

type SlackMessage struct {
	TS           string      `json:"ts"`
	User         string      `json:"user"`
	Text         string      `json:"text"`
	ReplyCount   int         `json:"reply_count"`
	Files        []SlackFile `json:"files"`
}

func NewClient(token, cookie, dm, userID string) *Client {
	return &Client{
		token:  token,
		cookie: cookie,
		dm:     dm,
		userID: userID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// PostMessage отправляет сообщение Viktor'у
func (c *Client) PostMessage(text string) (string, error) {
	data := url.Values{}
	data.Set("token", c.token)
	data.Set("channel", c.dm)
	data.Set("text", text)

	resp, err := c.call("chat.postMessage", data)
	if err != nil {
		return "", err
	}

	return resp.TS, nil
}

// FetchResponses получает ВСЕ ответы Viktor'а: из треда нашего сообщения,
// top-level сообщения в DM, и под-треды Viktor'а — как в Python версии.
func (c *Client) FetchThread(ourTS, afterTS string) ([]Message, error) {
	after := parseTS(afterTS)
	if after == 0 {
		after = parseTS(ourTS)
	}

	seen := map[string]string{}       // ts -> text
	seenFiles := map[string][]SlackFile{} // ts -> files

	// 1. Тред нашего сообщения (conversations.replies)
	data := url.Values{}
	data.Set("token", c.token)
	data.Set("channel", c.dm)
	data.Set("ts", ourTS)
	data.Set("limit", "100")

	if resp, err := c.call("conversations.replies", data); err == nil {
		for _, m := range resp.Messages {
			if m.TS == ourTS || m.User != c.userID {
				continue
			}
			if parseTS(m.TS) > after {
				seen[m.TS] = cleanSlackText(m.Text)
				if len(m.Files) > 0 { seenFiles[m.TS] = m.Files }
			}
		}
	}

	// 2. Top-level сообщения в DM после нашего
	data2 := url.Values{}
	data2.Set("token", c.token)
	data2.Set("channel", c.dm)
	data2.Set("oldest", ourTS)
	data2.Set("inclusive", "false")
	data2.Set("limit", "50")

	var subThreads []string
	if resp, err := c.call("conversations.history", data2); err == nil {
		for _, m := range resp.Messages {
			if m.User != c.userID {
				continue
			}
			if parseTS(m.TS) > after {
				seen[m.TS] = cleanSlackText(m.Text)
				if len(m.Files) > 0 { seenFiles[m.TS] = m.Files }
			}
			// Viktor создал под-тред — заберём его тоже
			if m.TS != ourTS && m.ReplyCount > 0 {
				subThreads = append(subThreads, m.TS)
			}
		}
	}

	// 3. Под-треды Viktor'а ("детали ниже в треде")
	for _, ts := range subThreads {
		data3 := url.Values{}
		data3.Set("token", c.token)
		data3.Set("channel", c.dm)
		data3.Set("ts", ts)
		data3.Set("limit", "100")

		if resp, err := c.call("conversations.replies", data3); err == nil {
			for _, m := range resp.Messages {
				if m.TS == ts || m.User != c.userID {
					continue
				}
				if parseTS(m.TS) > after {
					seen[m.TS] = cleanSlackText(m.Text)
					if len(m.Files) > 0 { seenFiles[m.TS] = m.Files }
				}
			}
		}
	}

	// Сортируем по timestamp
	var result []Message
	for ts, text := range seen {
		result = append(result, Message{TS: ts, Text: text, Files: seenFiles[ts]})
	}
	// Простая сортировка (обычно < 10 сообщений)
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if parseTS(result[i].TS) > parseTS(result[j].TS) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result, nil
}

// call выполняет API запрос к Slack
func (c *Client) call(method string, data url.Values) (*SlackResponse, error) {
	reqURL := fmt.Sprintf("%s/%s", baseURL, method)
	
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", fmt.Sprintf("d=%s", c.cookie))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var slackResp SlackResponse
	if err := json.Unmarshal(body, &slackResp); err != nil {
		return nil, err
	}

	if !slackResp.OK {
		return nil, fmt.Errorf("slack error: %s", slackResp.Error)
	}

	return &slackResp, nil
}

// DownloadFile downloads a Slack file attachment to destDir.
// Returns the local path or "" on error.
func (c *Client) DownloadFile(f SlackFile, destDir string) (string, error) {
	dlURL := f.URLPrivateDownload
	if dlURL == "" {
		dlURL = f.URLPrivate
	}
	if dlURL == "" {
		return "", fmt.Errorf("no download URL")
	}

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", err
	}

	name := sanitizeFilename(f.Name)
	if name == "" {
		name = f.ID + ".bin"
	}
	dest := filepath.Join(destDir, name)

	// Skip if same size already exists
	if info, err := os.Stat(dest); err == nil && f.Size > 0 && info.Size() == int64(f.Size) {
		return dest, nil
	}

	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Cookie", fmt.Sprintf("d=%s", c.cookie))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(dest, body, 0644); err != nil {
		return "", err
	}

	return dest, nil
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	// Replace invalid chars
	invalid := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	name = invalid.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	if name == "" {
		return "file"
	}
	return name
}
func cleanSlackText(text string) string {
	// <http://url|text> -> text
	linkRe := regexp.MustCompile(`<(https?://[^|>]+)(?:\|([^>]+))?>`)
	text = linkRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkRe.FindStringSubmatch(match)
		if len(parts) > 2 && parts[2] != "" {
			return parts[2]
		}
		return parts[1]
	})

	// <@U123> -> @U123
	text = regexp.MustCompile(`<@([A-Z0-9]+)>`).ReplaceAllString(text, "@$1")

	// HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")

	return text
}

// parseTS конвертирует Slack timestamp в float для сравнения
func parseTS(ts string) float64 {
	var f float64
	fmt.Sscanf(ts, "%f", &f)
	return f
}

// IsAck проверяет, является ли сообщение ack-подтверждением
func IsAck(text string) bool {
	lower := strings.ToLower(text)
	ackMarkers := []string{
		"напишу, когда", "напишу когда", "когда будет готово",
		"дам знать", "сообщу, когда", "принял задачу",
		"создаю", "создам", "делаю", "пушу",
		"i'll let you know", "working on it",
	}
	for _, marker := range ackMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// LooksDone проверяет признаки завершения
func LooksDone(text string) bool {
	doneRe := regexp.MustCompile(`(?i)готов|запуш|committed|pushed|\bdone\b|✅|github\.com|ошибк`)
	return doneRe.MatchString(text)
}

// ExpectsMore — Viktor обещает продолжение («Ниже…», «👇», «в треде»)
func ExpectsMore(text string) bool {
	moreRe := regexp.MustCompile(`(?i)ниже\b|в треде|👇|разлож.*ниже|детал.*(?:ниже|треде)|подробн.*(?:ниже|треде)|см\.?\s*ниже`)
	return moreRe.MatchString(text)
}

// LooksClarify — Viktor задаёт вопрос / ждёт задачу, продолжения не будет
func LooksClarify(text string) bool {
	t := strings.TrimSpace(text)
	if strings.HasSuffix(t, "?") {
		return true
	}
	clarifyRe := regexp.MustCompile(`(?i)опиши|уточни|что именно|какую задач|что нужно сделать|чем (?:могу )?помоч|жду (?:задач|детал)|напиши,? что|готов(?:а)? (?:взять|помоч|приступ)`)
	return clarifyRe.MatchString(t)
}
