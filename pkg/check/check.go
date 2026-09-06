package check

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	schemaURL     = "https://raw.githubusercontent.com/telegramdesktop/tdesktop/dev/Telegram/SourceFiles/mtproto/scheme/api.tl"
	commitsURL    = "https://api.github.com/repos/telegramdesktop/tdesktop/commits?path=Telegram/SourceFiles/mtproto/scheme/api.tl&per_page=12"
	defaultChatID = int64(7506666666)
	userAgent     = "notif-layer/1.0"
)

var layerRE = regexp.MustCompile(`(?m)^//\s*LAYER\s+(\d+)\s*$`)

type State struct {
	Layer      int    `json:"layer"`
	SHA        string `json:"sha"`
	NotifiedAt string `json:"notified_at"`
}

type Result struct {
	Updated    bool     `json:"updated"`
	Previous   int      `json:"previous_layer"`
	Latest     int      `json:"latest_layer"`
	SHA        string   `json:"sha,omitempty"`
	Changes    []string `json:"changes"`
	Notified   bool     `json:"notified"`
	Message    string   `json:"message"`
	SkipReason string   `json:"skip_reason,omitempty"`
}

type githubCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date time.Time `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	HTMLURL string `json:"html_url"`
}

func Run() (Result, error) {
	latest, err := fetchLatestLayer()
	if err != nil {
		return Result{}, err
	}
	commits, err := fetchCommits()
	if err != nil {
		return Result{}, err
	}

	state, err := loadState()
	if err != nil {
		return Result{}, err
	}

	changes := changelog(commits, state.Layer, latest)
	res := Result{
		Previous: state.Layer,
		Latest:   latest,
		Changes:  changes,
	}
	if len(commits) > 0 {
		res.SHA = commits[0].SHA
	}

	if latest <= state.Layer {
		res.SkipReason = "layer belum berubah"
		res.Message = fmt.Sprintf("Layer tetap %d", latest)
		return res, nil
	}

	res.Updated = true
	text := formatMessage(state.Layer, latest, changes)
	res.Message = text

	if err := sendTelegram(text); err != nil {
		return res, err
	}
	res.Notified = true

	state.Layer = latest
	state.SHA = res.SHA
	state.NotifiedAt = time.Now().UTC().Format(time.RFC3339)
	_ = saveState(state)
	return res, nil
}

func fetchLatestLayer() (int, error) {
	body, err := httpGet(schemaURL)
	if err != nil {
		return 0, fmt.Errorf("gagal unduh api.tl: %w", err)
	}
	match := layerRE.FindSubmatch(body)
	if match == nil {
		return 0, fmt.Errorf("baris LAYER tidak ditemukan di api.tl")
	}
	return strconv.Atoi(string(match[1]))
}

func fetchCommits() ([]githubCommit, error) {
	body, err := httpGet(commitsURL)
	if err != nil {
		return nil, fmt.Errorf("gagal unduh commit GitHub: %w", err)
	}
	var commits []githubCommit
	if err := json.Unmarshal(body, &commits); err != nil {
		return nil, fmt.Errorf("gagal parse commit GitHub: %w", err)
	}
	return commits, nil
}

func changelog(commits []githubCommit, previous, latest int) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range commits {
		msg := strings.TrimSpace(strings.Split(c.Commit.Message, "\n")[0])
		if msg == "" || seen[msg] {
			continue
		}
		seen[msg] = true
		out = append(out, msg)
		if len(out) >= 8 {
			break
		}
	}
	if len(out) == 0 {
		out = append(out, fmt.Sprintf("Schema Telegram Desktop naik dari layer %d ke %d", previous, latest))
	}
	return out
}

func formatMessage(previous, latest int, changes []string) string {
	var b strings.Builder
	b.WriteString("Pembaruan Layer Telegram\n\n")
	b.WriteString(fmt.Sprintf("Layer terbaru: %d\n", latest))
	if previous > 0 {
		b.WriteString(fmt.Sprintf("Layer sebelumnya: %d\n", previous))
	}
	b.WriteString("\nPembaruan:\n")
	for _, item := range changes {
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}
	b.WriteString("\nSumber: https://github.com/telegramdesktop/tdesktop/blob/dev/Telegram/SourceFiles/mtproto/scheme/api.tl")
	return b.String()
}

func loadState() (State, error) {
	if data, err := os.ReadFile("last_layer.json"); err == nil {
		var state State
		if json.Unmarshal(data, &state) == nil && state.Layer > 0 {
			return state, nil
		}
	}
	if raw := strings.TrimSpace(os.Getenv("LAST_LAYER")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			return State{Layer: n}, nil
		}
	}
	return State{Layer: 214}, nil
}

func saveState(state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("last_layer.json", data, 0o644)
}

func sendTelegram(text string) error {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN kosong")
	}
	chatID := defaultChatID
	if raw := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			chatID = n
		}
	}
	payload, err := json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    text,
	})
	if err != nil {
		return err
	}
	url := "https://api.telegram.org/bot" + token + "/sendMessage"
	resp, err := http.Post(url, "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func httpGet(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}
