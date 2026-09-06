package check

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	schemaURL     = "https://raw.githubusercontent.com/telegramdesktop/tdesktop/dev/Telegram/SourceFiles/mtproto/scheme/api.tl"
	commitsURL    = "https://api.github.com/repos/telegramdesktop/tdesktop/commits?path=Telegram/SourceFiles/mtproto/scheme/api.tl&per_page=12"
	defaultChatID = int64(7506666666)
	userAgent     = "notif-layer/1.0"
	defaultLayer  = 214
)

var (
	layerRE          = regexp.MustCompile(`(?m)^//\s*LAYER\s+(\d+)\s*$`)
	telegramStateRE  = regexp.MustCompile(`^notif-layer\s+(\d+)(?:\s+n=([0-9a-f]+))?$`)
	runMu            sync.Mutex
	claimRecheckWait = 400 * time.Millisecond
)

type State struct {
	Layer      int    `json:"layer"`
	SHA        string `json:"sha"`
	NotifiedAt string `json:"notified_at"`
	Nonce      string `json:"-"`
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
	runMu.Lock()
	defer runMu.Unlock()

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

	changes := changelog(commits, state.SHA, state.Layer, latest)
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

	claimed, err := claimLayer(state, latest, res.SHA)
	if err != nil {
		return res, err
	}
	if !claimed {
		res.SkipReason = "sudah dikirim proses lain"
		res.Notified = false
		return res, nil
	}

	if err := sendTelegram(text); err != nil {
		_ = revertClaim(state)
		return res, err
	}
	res.Notified = true

	state.Layer = latest
	state.SHA = res.SHA
	state.NotifiedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveState(state); err != nil {
		return res, fmt.Errorf("notif terkirim, tapi state gagal disimpan: %w", err)
	}
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

func changelog(commits []githubCommit, lastSHA string, previous, latest int) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range commits {
		if lastSHA != "" && c.SHA == lastSHA {
			break
		}
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
	best := State{}
	for _, path := range statePaths() {
		if data, err := os.ReadFile(path); err == nil {
			var state State
			if json.Unmarshal(data, &state) == nil && state.Layer > best.Layer {
				best = state
			}
		}
	}
	if remote, err := loadTelegramState(); err == nil && remote.Layer > best.Layer {
		best = remote
	}
	if best.Layer > 0 {
		return best, nil
	}
	if raw := strings.TrimSpace(os.Getenv("LAST_LAYER")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			return State{Layer: n}, nil
		}
	}
	return State{Layer: defaultLayer}, nil
}

func saveState(state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	var fileErr error
	for _, path := range statePaths() {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			fileErr = err
			continue
		}
		fileErr = nil
		break
	}
	if err := saveTelegramState(state, ""); err != nil {
		if fileErr != nil {
			return fmt.Errorf("file: %v; telegram: %w", fileErr, err)
		}
		return err
	}
	return nil
}

func statePaths() []string {
	var paths []string
	if custom := strings.TrimSpace(os.Getenv("STATE_FILE")); custom != "" {
		paths = append(paths, custom)
	}
	paths = append(paths, "last_layer.json", filepath.Join(os.TempDir(), "notif-layer-last.json"))
	return paths
}

func claimLayer(previous State, latest int, sha string) (bool, error) {
	nonce, err := newNonce()
	if err != nil {
		return false, err
	}
	current, err := loadTelegramState()
	if err != nil {
		current = previous
	}
	if latest <= current.Layer {
		return false, nil
	}
	claim := State{Layer: latest, SHA: sha, Nonce: nonce}
	if err := saveTelegramState(claim, nonce); err != nil {
		// VPS tanpa API deskripsi bot tetap bisa lanjut; file lock di Run() menahan proses yang sama.
		return true, nil
	}
	time.Sleep(claimRecheckWait)
	again, err := loadTelegramState()
	if err != nil {
		return false, err
	}
	return again.Layer == latest && again.Nonce == nonce, nil
}

func revertClaim(previous State) error {
	if previous.Layer <= 0 {
		return nil
	}
	return saveTelegramState(previous, "")
}

func loadTelegramState() (State, error) {
	body, err := telegramAPI("getMyShortDescription", nil)
	if err != nil {
		return State{}, err
	}
	var resp struct {
		OK     bool `json:"ok"`
		Result struct {
			ShortDescription string `json:"short_description"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return State{}, err
	}
	if !resp.OK {
		return State{}, fmt.Errorf("getMyShortDescription: %s", strings.TrimSpace(string(body)))
	}
	state, ok := parseTelegramState(resp.Result.ShortDescription)
	if !ok {
		return State{}, fmt.Errorf("deskripsi bot belum berisi state")
	}
	return state, nil
}

func saveTelegramState(state State, nonce string) error {
	_, err := telegramAPI("setMyShortDescription", map[string]any{
		"short_description": formatTelegramState(state.Layer, nonce),
	})
	return err
}

func parseTelegramState(raw string) (State, bool) {
	match := telegramStateRE.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return State{}, false
	}
	layer, err := strconv.Atoi(match[1])
	if err != nil || layer <= 0 {
		return State{}, false
	}
	return State{Layer: layer, Nonce: match[2]}, true
}

func formatTelegramState(layer int, nonce string) string {
	if nonce == "" {
		return fmt.Sprintf("notif-layer %d", layer)
	}
	return fmt.Sprintf("notif-layer %d n=%s", layer, nonce)
}

func newNonce() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func sendTelegram(text string) error {
	payload, err := json.Marshal(map[string]any{
		"chat_id": chatID(),
		"text":    text,
	})
	if err != nil {
		return err
	}
	body, err := telegramAPI("sendMessage", payload)
	if err != nil {
		return err
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram sendMessage: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func chatID() int64 {
	if raw := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n
		}
	}
	return defaultChatID
}

func botToken() (string, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return "", fmt.Errorf("TELEGRAM_BOT_TOKEN kosong")
	}
	return token, nil
}

func telegramAPI(method string, payload any) ([]byte, error) {
	token, err := botToken()
	if err != nil {
		return nil, err
	}
	url := "https://api.telegram.org/bot" + token + "/" + method
	var body io.Reader
	if payload != nil {
		raw, ok := payload.([]byte)
		if !ok {
			raw, err = json.Marshal(payload)
			if err != nil {
				return nil, err
			}
		}
		body = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram %s status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
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
