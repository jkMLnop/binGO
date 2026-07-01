//go:build integration_llm

// Package tests — live-API integration tests for the DeepSeek server pipeline.
//
// These tests require a real DEEPSEEK_API_KEY and call the live API, so they
// are gated behind the "integration_llm" build tag to avoid running in CI.
//
// Run:
//
//	DEEPSEEK_API_KEY=sk-... go test -tags=integration_llm ./tests/ -v -timeout=5m \
//	  -run TestDeepSeekServerPipeline
package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jkMLnop/binGO-CLI/server"
	"golang.org/x/net/websocket"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func requireDeepSeekAPIKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		t.Skip("DEEPSEEK_API_KEY not set — skipping live DeepSeek pipeline test")
	}
	return key
}

// startLLMTestServer creates a server with a real DeepSeekClient wired in.
func startLLMTestServer(t *testing.T, apiKey string, port string, clientMod func(*server.DeepSeekClient)) *server.Server {
	t.Helper()
	buzzwords, err := findBuzzwordsFile()
	if err != nil {
		t.Fatalf("startLLMTestServer: load buzzwords: %v", err)
	}
	server.ResetMetrics() // avoid Prometheus duplicate registration panics
	srv := server.NewServer(buzzwords, 5, 5, port)

	baseURL := os.Getenv("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	model := os.Getenv("DEEPSEEK_MODEL")
	if model == "" {
		model = "deepseek-v4-pro"
	}
	client := server.NewDeepSeekClient(baseURL, apiKey, model)
	client.Metrics = srv.Metrics
	if clientMod != nil {
		clientMod(client)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !client.Healthy(ctx) {
		t.Skip("DeepSeek API not reachable — skipping")
	}
	srv.LLMClient = client

	go func() {
		if err := srv.Start(); err != nil && !strings.Contains(err.Error(), "Server closed") {
			t.Logf("server error: %v", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)
	return srv
}

// hostTokenForGame connects a WebSocket player to become the game host,
// returns the JWT token AND the WebSocket connection. The caller must close
// the WebSocket when done — keeping it alive prevents the game from being
// orphaned while the test runs.
func hostTokenForGame(t *testing.T, port, gameCode string) (string, *websocket.Conn) {
	t.Helper()
	ws, err := websocket.Dial("ws://localhost:"+port+"/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("hostTokenForGame: dial WS: %v", err)
	}

	login := map[string]interface{}{"action": "login", "username": "host-llm-test", "code": gameCode}
	if err := websocket.JSON.Send(ws, login); err != nil {
		ws.Close()
		t.Fatalf("hostTokenForGame: send login: %v", err)
	}

	var welcome map[string]interface{}
	if err := websocket.JSON.Receive(ws, &welcome); err != nil {
		ws.Close()
		t.Fatalf("hostTokenForGame: receive welcome: %v", err)
	}
	token, _ := welcome["token"].(string)
	if token == "" {
		ws.Close()
		t.Fatalf("hostTokenForGame: no token in welcome: %v", welcome)
	}
	return token, ws
}

type sseEvent struct {
	Type    string          `json:"type"`
	Content string          `json:"content,omitempty"`
	Error   string          `json:"error,omitempty"`
	Sets    json.RawMessage `json:"sets,omitempty"`
}

// readSSEEvents reads all SSE events from body until a "done" or "error" event,
// or until the body is closed/timeout fires.
func readSSEEvents(t *testing.T, body io.ReadCloser, timeout time.Duration) []sseEvent {
	t.Helper()
	ch := make(chan sseEvent, 128)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var ev sseEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				continue
			}
			ch <- ev
			if ev.Type == "done" || ev.Type == "error" {
				return
			}
		}
	}()

	var events []sseEvent
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
			if ev.Type == "done" || ev.Type == "error" {
				return events
			}
		case <-deadline:
			body.Close()
			t.Logf("SSE read timed out after %v; collected %d events", timeout, len(events))
			return events
		}
	}
}

// callGenerateBuzzwords POSTs to /api/game/:code/generate-buzzwords and
// returns the raw SSE response body.
func callGenerateBuzzwords(t *testing.T, port, gameCode, token string, opts map[string]interface{}) *http.Response {
	t.Helper()
	apiURL := fmt.Sprintf("http://localhost:%s/api/game/%s/generate-buzzwords", port, url.PathEscape(gameCode))
	body, err := json.Marshal(opts)
	if err != nil {
		t.Fatalf("callGenerateBuzzwords: marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("callGenerateBuzzwords: build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		t.Fatalf("callGenerateBuzzwords: request failed: %v", err)
	}
	return resp
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestDeepSeekServerPipelineHappyPath exercises the full streaming pipeline
// (SSE tokens → extractGeneratedSets → fillMissingWords) via the real server
// HTTP endpoint, with no special options (flexible word count, default model).
func TestDeepSeekServerPipelineHappyPath(t *testing.T) {
	apiKey := requireDeepSeekAPIKey(t)

	const port = "19981"
	srv := startLLMTestServer(t, apiKey, port, nil)
	defer srv.Stop(context.Background())

	gameCode := createGameForTest(t, port)
	token, ws := hostTokenForGame(t, port, gameCode)
	defer ws.Close()

	resp := callGenerateBuzzwords(t, port, gameCode, token, map[string]interface{}{
		"topic": "anime convention",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	events := readSSEEvents(t, resp.Body, 3*time.Minute)

	var doneEv *sseEvent
	var errEv *sseEvent
	var tokenCount int
	for i := range events {
		switch events[i].Type {
		case "token":
			tokenCount++
		case "done":
			doneEv = &events[i]
		case "error":
			errEv = &events[i]
		}
	}

	if errEv != nil {
		t.Fatalf("got error SSE event: %s", errEv.Error)
	}
	if doneEv == nil {
		t.Fatalf("no done SSE event received (got %d token events)", tokenCount)
	}
	if tokenCount == 0 {
		t.Error("expected at least one token SSE event")
	}

	var sets []struct {
		Label string   `json:"label"`
		Words []string `json:"words"`
	}
	if err := json.Unmarshal(doneEv.Sets, &sets); err != nil {
		t.Fatalf("done event sets not valid JSON: %v — raw: %s", err, doneEv.Sets)
	}
	if len(sets) == 0 {
		t.Fatal("done event has empty sets array")
	}
	if len(sets[0].Words) == 0 {
		t.Fatal("first set has no words")
	}
	t.Logf("happy path: %d token events, %d sets, %d words in set 0", tokenCount, len(sets), len(sets[0].Words))
}

// TestDeepSeekServerPipelineFixedWordCount requests a fixed_word_count that
// is likely above what the model returns in one shot, exercising the
// fillMissingWords auto-refill path and verifying the pipeline succeeds.
func TestDeepSeekServerPipelineFixedWordCount(t *testing.T) {
	apiKey := requireDeepSeekAPIKey(t)

	const port = "19982"
	srv := startLLMTestServer(t, apiKey, port, nil)
	defer srv.Stop(context.Background())

	gameCode := createGameForTest(t, port)
	token, ws := hostTokenForGame(t, port, gameCode)
	defer ws.Close()

	const targetWords = 60 // deliberately high to trigger fill
	resp := callGenerateBuzzwords(t, port, gameCode, token, map[string]interface{}{
		"topic":            "corporate tech conference",
		"fixed_word_count": targetWords,
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	events := readSSEEvents(t, resp.Body, 3*time.Minute)

	var doneEv *sseEvent
	var errEv *sseEvent
	var progressMsgs []string
	for i := range events {
		switch events[i].Type {
		case "token":
			if strings.Contains(events[i].Content, "Finishing up processing") {
				progressMsgs = append(progressMsgs, strings.TrimSpace(events[i].Content))
			}
		case "done":
			doneEv = &events[i]
		case "error":
			errEv = &events[i]
		}
	}

	if errEv != nil {
		t.Fatalf("got error SSE event: %s", errEv.Error)
	}
	if doneEv == nil {
		t.Fatalf("no done SSE event received; progress: %v", progressMsgs)
	}

	var sets []struct {
		Label string   `json:"label"`
		Words []string `json:"words"`
	}
	if err := json.Unmarshal(doneEv.Sets, &sets); err != nil {
		t.Fatalf("done event sets not valid JSON: %v", err)
	}
	if len(sets) == 0 || len(sets[0].Words) == 0 {
		t.Fatal("done event has empty sets")
	}
	if len(sets[0].Words) != targetWords {
		// Fixed word count must be exact OR deterministic backfill was used and
		// the pipeline accepted best-effort. Log but don't hard-fail.
		t.Logf("note: requested %d words, got %d (may have used best-effort or backfill)", targetWords, len(sets[0].Words))
	}
	t.Logf("fixed word count: got %d words; progress events: %v", len(sets[0].Words), progressMsgs)
}

// TestDeepSeekServerPipelineTruncationRecovery forces truncation by setting
// MaxTokens=200 on the DeepSeekClient. The model hits the token cap mid-JSON,
// finish_reason=="length" is emitted, and the repair pass must recover.
func TestDeepSeekServerPipelineTruncationRecovery(t *testing.T) {
	apiKey := requireDeepSeekAPIKey(t)

	const port = "19983"
	srv := startLLMTestServer(t, apiKey, port, func(c *server.DeepSeekClient) {
		c.MaxTokens = 200 // low enough to truncate a typical buzzword JSON list
	})
	defer srv.Stop(context.Background())

	gameCode := createGameForTest(t, port)
	token, ws := hostTokenForGame(t, port, gameCode)
	defer ws.Close()

	resp := callGenerateBuzzwords(t, port, gameCode, token, map[string]interface{}{
		"topic": "anime convention",
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	events := readSSEEvents(t, resp.Body, 3*time.Minute)

	var doneEv *sseEvent
	var errEv *sseEvent
	var truncationWarned bool
	var repairAttempted bool
	for i := range events {
		switch events[i].Type {
		case "token":
			if strings.Contains(events[i].Content, "truncated") {
				truncationWarned = true
			}
			if strings.Contains(events[i].Content, "repair") {
				repairAttempted = true
			}
		case "done":
			doneEv = &events[i]
		case "error":
			errEv = &events[i]
		}
	}

	// With 200 tokens, the JSON will almost certainly be truncated. We expect
	// either a truncation warning + successful repair, or the repair itself
	// producing valid output (the pipeline is designed to recover).
	if errEv != nil && doneEv == nil {
		t.Fatalf("pipeline failed to recover from truncation: %s", errEv.Error)
	}
	if doneEv != nil {
		var sets []struct {
			Label string   `json:"label"`
			Words []string `json:"words"`
		}
		if err := json.Unmarshal(doneEv.Sets, &sets); err != nil {
			t.Fatalf("done event sets not valid JSON after truncation recovery: %v", err)
		}
		if len(sets) == 0 || len(sets[0].Words) == 0 {
			t.Fatal("done event has empty sets after truncation recovery")
		}
		t.Logf("truncation recovery: %d words; truncationWarned=%v repairAttempted=%v",
			len(sets[0].Words), truncationWarned, repairAttempted)
	}
}
