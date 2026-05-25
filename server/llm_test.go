package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── extractGeneratedSets ──────────────────────────────────────────────────────

func TestExtractGeneratedSetsValid(t *testing.T) {
	sets := buildValidSetsJSON(3, 25)
	result, err := extractGeneratedSets(sets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sets) != 3 {
		t.Errorf("expected 3 sets, got %d", len(result.Sets))
	}
}

func TestExtractGeneratedSetsTooFewWords(t *testing.T) {
	sets := buildValidSetsJSON(1, 10) // only 10 words per set
	_, err := extractGeneratedSets(sets)
	if err == nil {
		t.Error("expected error for <24 words, got nil")
	}
}

func TestExtractGeneratedSetsDuplicateAcrossSets(t *testing.T) {
	// Two sets that share a word
	raw := `{"sets":[` +
		buildSetJSON("Set A", 25, "shared") + "," +
		buildSetJSON("Set B", 25, "shared") +
		`]}`
	_, err := extractGeneratedSets(raw)
	if err == nil {
		t.Error("expected error for cross-set duplicate, got nil")
	}
}

func TestExtractGeneratedSetsWordTooLong(t *testing.T) {
	longWord := strings.Repeat("x", 61)
	words := make([]string, 25)
	for i := range words {
		words[i] = fmt.Sprintf("word%d", i)
	}
	words[0] = longWord
	wordsJSON, _ := json.Marshal(words)
	raw := fmt.Sprintf(`{"sets":[{"label":"Set A","words":%s}]}`, string(wordsJSON))
	_, err := extractGeneratedSets(raw)
	if err == nil {
		t.Error("expected error for word >60 chars, got nil")
	}
}

func TestExtractGeneratedSetsNoJSON(t *testing.T) {
	_, err := extractGeneratedSets("Here are some words but no JSON object at all")
	if err == nil {
		t.Error("expected error when no JSON found, got nil")
	}
}

func TestExtractGeneratedSetsProse(t *testing.T) {
	// LLM may output prose before the JSON — we extract the first {...}
	valid := buildValidSetsJSON(3, 25)
	text := "Here are your buzzword sets:\n\n" + valid + "\n\nEnjoy your game!"
	result, err := extractGeneratedSets(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sets) != 3 {
		t.Errorf("expected 3 sets, got %d", len(result.Sets))
	}
}

// ── OllamaClient.StreamGenerate ───────────────────────────────────────────────

// TestOllamaClientStreamGenerate tests the full streaming pipeline against a
// mock HTTP server that replays a valid Ollama NDJSON response.
func TestOllamaClientStreamGenerate(t *testing.T) {
	validJSON := buildValidSetsJSON(3, 25)

	// Build an Ollama NDJSON fixture: emit tokens that together form the JSON,
	// then a final done chunk.
	var ndjson strings.Builder
	tokens := splitTokens(validJSON, 10) // split into ~10-char chunks
	for _, tok := range tokens {
		chunk := ollamaChunk{Done: false}
		chunk.Message.Content = tok
		line, _ := json.Marshal(chunk)
		ndjson.Write(line)
		ndjson.WriteByte('\n')
	}
	doneChunk := ollamaChunk{Done: true}
	doneLine, _ := json.Marshal(doneChunk)
	ndjson.Write(doneLine)
	ndjson.WriteByte('\n')

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/chat" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(ndjson.String()))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewOllamaClient(srv.URL, "test-model")
	rec := httptest.NewRecorder()

	messages := []ChatMessage{{Role: "user", Content: "anime convention"}}
	err := client.StreamGenerate(context.Background(), messages, rec)
	if err != nil {
		t.Fatalf("StreamGenerate returned error: %v", err)
	}

	body := rec.Body.String()

	// Verify token events were emitted
	if !strings.Contains(body, `"type":"token"`) {
		t.Error("expected token SSE events in output")
	}
	// Verify done event with sets was emitted
	if !strings.Contains(body, `"type":"done"`) {
		t.Error("expected done SSE event in output")
	}
	if !strings.Contains(body, `"sets"`) {
		t.Error("expected sets in done event")
	}
}

// TestOllamaClientStreamGenerateOllamaError verifies that a non-200 Ollama
// response causes StreamGenerate to return an error without panicking.
func TestOllamaClientStreamGenerateOllamaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewOllamaClient(srv.URL, "test-model")
	rec := httptest.NewRecorder()
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "test"}}, rec)
	if err == nil {
		t.Error("expected error when Ollama returns 500, got nil")
	}
}

// TestOllamaClientHealthy verifies the health probe.
func TestOllamaClientHealthy(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer healthy.Close()

	client := NewOllamaClient(healthy.URL, "test-model")
	if !client.Healthy(context.Background()) {
		t.Error("expected Healthy to return true for running server")
	}
}

func TestOllamaClientNotHealthy(t *testing.T) {
	// Point at a port with nothing listening
	client := NewOllamaClient("http://127.0.0.1:19999", "test-model")
	if client.Healthy(context.Background()) {
		t.Error("expected Healthy to return false for unreachable server")
	}
}

// ── handleGenerateBuzzwords ───────────────────────────────────────────────────

// TestHandleGenerateBuzzwordsNoLLM verifies HTTP 503 when LLMClient is nil.
func TestHandleGenerateBuzzwordsNoLLM(t *testing.T) {
	ResetMetrics()
	s := NewServer([][]string{{"foo"}}, 5, 5, "9999")
	// s.LLMClient is nil

	// Create a room first
	room, _ := s.createRoom(context.Background(), "host-1")

	body := bytes.NewBufferString(fmt.Sprintf(`{"host_id":%q,"topic":"test"}`, room.HostID))
	req := httptest.NewRequest(http.MethodPost, "/api/room/"+room.Code+"/generate-buzzwords", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleGenerateBuzzwords(rec, req, room.Code)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Ollama") {
		t.Errorf("expected Ollama error message, got: %s", rec.Body.String())
	}
}

// TestHandleGenerateBuzzwordsHostAuth verifies HTTP 403 for wrong host_id.
func TestHandleGenerateBuzzwordsHostAuth(t *testing.T) {
	ResetMetrics()
	s := NewServer([][]string{{"foo"}}, 5, 5, "9999")
	s.LLMClient = &stubLLMClient{}

	room, _ := s.createRoom(context.Background(), "real-host")

	body := bytes.NewBufferString(`{"host_id":"wrong-host","topic":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/room/"+room.Code+"/generate-buzzwords", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleGenerateBuzzwords(rec, req, room.Code)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

// TestHandleGenerateBuzzwordsSuccess verifies the happy path with a stub LLMClient.
func TestHandleGenerateBuzzwordsSuccess(t *testing.T) {
	ResetMetrics()
	s := NewServer([][]string{{"foo"}}, 5, 5, "9999")
	stub := &stubLLMClient{}
	s.LLMClient = stub

	room, _ := s.createRoom(context.Background(), "host-42")

	body := bytes.NewBufferString(fmt.Sprintf(`{"host_id":%q,"topic":"anime convention"}`, room.HostID))
	req := httptest.NewRequest(http.MethodPost, "/api/room/"+room.Code+"/generate-buzzwords", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleGenerateBuzzwords(rec, req, room.Code)

	if !stub.called {
		t.Error("expected LLMClient.StreamGenerate to be called")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// stubLLMClient is a no-op LLMClient for testing.
type stubLLMClient struct {
	called bool
}

func (s *stubLLMClient) StreamGenerate(_ context.Context, _ []ChatMessage, _ http.ResponseWriter) error {
	s.called = true
	return nil
}

func (s *stubLLMClient) Healthy(_ context.Context) bool { return true }

// buildValidSetsJSON builds a GeneratedSets JSON string with n sets of wordsPerSet words.
func buildValidSetsJSON(n, wordsPerSet int) string {
	sets := make([]string, n)
	usedWords := make(map[string]bool)
	wordIdx := 0
	for i := range sets {
		label := fmt.Sprintf("Set %c", 'A'+rune(i))
		sets[i] = buildSetJSONUnique(label, wordsPerSet, usedWords, &wordIdx)
	}
	return fmt.Sprintf(`{"sets":[%s]}`, strings.Join(sets, ","))
}

// buildSetJSON builds a set JSON where words[0] is a specific "duplicate" word.
func buildSetJSON(label string, count int, firstWord string) string {
	words := make([]string, count)
	words[0] = firstWord
	for i := 1; i < count; i++ {
		words[i] = fmt.Sprintf("%s-word%d", label, i)
	}
	wordsJSON, _ := json.Marshal(words)
	return fmt.Sprintf(`{"label":%q,"words":%s}`, label, string(wordsJSON))
}

// buildSetJSONUnique builds a set with globally unique word names.
func buildSetJSONUnique(label string, count int, used map[string]bool, idx *int) string {
	words := make([]string, count)
	for i := range words {
		for {
			w := fmt.Sprintf("word%d", *idx)
			*idx++
			if !used[w] {
				used[w] = true
				words[i] = w
				break
			}
		}
	}
	wordsJSON, _ := json.Marshal(words)
	return fmt.Sprintf(`{"label":%q,"words":%s}`, label, string(wordsJSON))
}

// splitTokens splits s into chunks of roughly chunkSize characters.
func splitTokens(s string, chunkSize int) []string {
	var chunks []string
	for len(s) > 0 {
		end := chunkSize
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[:end])
		s = s[end:]
	}
	return chunks
}
