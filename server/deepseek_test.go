package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── DeepSeekClient.StreamGenerate ─────────────────────────────────────────────

// TestDeepSeekClientStreamGenerate tests the full streaming pipeline against a
// mock HTTP server that replays a valid OpenAI-style SSE response.
func TestDeepSeekClientStreamGenerate(t *testing.T) {
	validJSON := buildValidSetsJSON(3, 25)

	// Build an SSE fixture: emit tokens that together form the JSON,
	// then a final [DONE] marker.
	var sse strings.Builder
	tokens := splitTokens(validJSON, 10)
	for _, tok := range tokens {
		chunk := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"delta": map[string]string{"content": tok},
				},
			},
		}
		line, _ := json.Marshal(chunk)
		sse.WriteString("data: ")
		sse.Write(line)
		sse.WriteString("\n\n")
	}
	sse.WriteString("data: [DONE]\n\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/chat/completions" {
			// Verify request body has required fields
			var reqBody struct {
				Model          string `json:"model"`
				Stream         bool   `json:"stream"`
				ResponseFormat *struct {
					Type string `json:"type"`
				} `json:"response_format"`
				Thinking *struct {
					Type string `json:"type"`
				} `json:"thinking"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}
			if reqBody.Model == "" {
				t.Fatal("expected model to be set")
			}
			if !reqBody.Stream {
				t.Fatal("expected stream=true")
			}
			if reqBody.ResponseFormat == nil || reqBody.ResponseFormat.Type != "json_object" {
				t.Fatal("expected response_format with type=json_object")
			}
			if reqBody.Thinking == nil || reqBody.Thinking.Type != "disabled" {
				t.Fatal("expected thinking=disabled")
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(sse.String()))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()

	messages := []ChatMessage{{Role: "user", Content: "anime convention"}}
	err := client.StreamGenerate(context.Background(), messages, DefaultGenerationOptions(), rec)
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

// TestDeepSeekClientStreamGenerateError verifies that a non-200 response causes
// StreamGenerate to return an error without panicking.
func TestDeepSeekClientStreamGenerateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "test"}}, DefaultGenerationOptions(), rec)
	if err == nil {
		t.Error("expected error when DeepSeek returns 500, got nil")
	}
}

// TestDeepSeekClientStreamGenerateRepairsMalformedOutput tests the repair pass
// when the streaming output is not valid JSON.
func TestDeepSeekClientStreamGenerateRepairsMalformedOutput(t *testing.T) {
	malformed := `NOT_JSON_OUTPUT_WITHOUT_QUOTES_OR_BRACES`
	repaired := buildValidSetsJSON(1, 30)

	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		callCount++

		var reqBody struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if callCount == 1 {
			if !reqBody.Stream {
				t.Fatalf("expected initial call to be stream=true")
			}
			var sse strings.Builder
			for _, tok := range splitTokens(malformed, 10) {
				chunk := map[string]interface{}{
					"choices": []map[string]interface{}{
						{"delta": map[string]string{"content": tok}},
					},
				}
				line, _ := json.Marshal(chunk)
				sse.WriteString("data: ")
				sse.Write(line)
				sse.WriteString("\n\n")
			}
			sse.WriteString("data: [DONE]\n\n")

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(sse.String()))
			return
		}

		if reqBody.Stream {
			t.Fatalf("expected repair call to be stream=false")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, repaired)))
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "anime convention"}}, DefaultGenerationOptions(), rec)
	if err != nil {
		t.Fatalf("expected repair flow success, got error: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("expected done SSE event, got: %s", body)
	}
	if !strings.Contains(body, "attempting structured repair") {
		t.Fatalf("expected repair progress message, got: %s", body)
	}
	if callCount < 2 {
		t.Fatalf("expected at least 2 calls (stream + repair), got %d", callCount)
	}
}

// TestDeepSeekClientHealthy verifies the health probe.
func TestDeepSeekClientHealthy(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer healthy.Close()

	client := NewDeepSeekClient(healthy.URL, "test-api-key", "test-model")
	if !client.Healthy(context.Background()) {
		t.Error("expected Healthy to return true for running server")
	}
}

func TestDeepSeekClientNotHealthy(t *testing.T) {
	// Point at a port with nothing listening
	client := NewDeepSeekClient("http://127.0.0.1:19999", "test-api-key", "test-model")
	if client.Healthy(context.Background()) {
		t.Error("expected Healthy to return false for unreachable server")
	}
}

func TestDeepSeekClientEmptyAPIKeyNotHealthy(t *testing.T) {
	client := NewDeepSeekClient("http://127.0.0.1:19999", "", "test-model")
	if client.Healthy(context.Background()) {
		t.Error("expected Healthy to return false for empty API key")
	}
}

// TestDeepSeekClientStreamGenerateFillsMissingWordsWhenFixedCountEnabled tests
// the fill-missing-words flow when the initial output is below the target.
func TestDeepSeekClientStreamGenerateFillsMissingWordsWhenFixedCountEnabled(t *testing.T) {
	nearMissJSON := buildValidSetsJSON(1, 39)
	fillWords := make([]string, 0, 11)
	for i := 39; i < 50; i++ {
		fillWords = append(fillWords, fmt.Sprintf("word%d", i))
	}
	fillWordsJSON, _ := json.Marshal(fillWords)
	fillJSON := fmt.Sprintf(`{"sets":[{"label":"Set A","words":%s}]}`, string(fillWordsJSON))

	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/chat/completions" {
			callCount++
			var reqBody struct {
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}

			if callCount == 1 {
				if !reqBody.Stream {
					t.Fatalf("expected first call to be stream=true")
				}
				var sse strings.Builder
				for _, tok := range splitTokens(nearMissJSON, 12) {
					chunk := map[string]interface{}{
						"choices": []map[string]interface{}{
							{"delta": map[string]string{"content": tok}},
						},
					}
					line, _ := json.Marshal(chunk)
					sse.WriteString("data: ")
					sse.Write(line)
					sse.WriteString("\n\n")
				}
				sse.WriteString("data: [DONE]\n\n")

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(sse.String()))
				return
			}

			if reqBody.Stream {
				t.Fatalf("expected fill call to be stream=false")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, fillJSON)))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()
	opts := DefaultGenerationOptions()
	opts.FixedWordCount = 50
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "anime convention"}}, opts, rec)
	if err != nil {
		t.Fatalf("StreamGenerate returned error: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("expected done SSE event, got: %s", body)
	}
	if strings.Contains(body, `"type":"error"`) {
		t.Fatalf("did not expect error SSE event, got: %s", body)
	}
	if !strings.Contains(body, "word49") {
		t.Fatalf("expected merged output to include filled words, got: %s", body)
	}
	if callCount < 2 {
		t.Fatalf("expected fill call to deepseek, got %d call(s)", callCount)
	}
}

// TestDeepSeekClientStreamGenerateFillsToDefaultMinimum30 tests the default
// minimum word count fill flow.
func TestDeepSeekClientStreamGenerateFillsToDefaultMinimum30(t *testing.T) {
	initialJSON := buildValidSetsJSON(1, 12)
	fillWords := make([]string, 0, 18)
	for i := 12; i < 30; i++ {
		fillWords = append(fillWords, fmt.Sprintf("word%d", i))
	}
	fillWordsJSON, _ := json.Marshal(fillWords)
	fillJSON := fmt.Sprintf(`{"sets":[{"label":"Set A","words":%s}]}`, string(fillWordsJSON))

	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/chat/completions" {
			callCount++
			var reqBody struct {
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}

			if callCount == 1 {
				if !reqBody.Stream {
					t.Fatalf("expected first call to be stream=true")
				}
				var sse strings.Builder
				for _, tok := range splitTokens(initialJSON, 12) {
					chunk := map[string]interface{}{
						"choices": []map[string]interface{}{
							{"delta": map[string]string{"content": tok}},
						},
					}
					line, _ := json.Marshal(chunk)
					sse.WriteString("data: ")
					sse.Write(line)
					sse.WriteString("\n\n")
				}
				sse.WriteString("data: [DONE]\n\n")

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(sse.String()))
				return
			}

			if reqBody.Stream {
				t.Fatalf("expected refill call to be stream=false")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, fillJSON)))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "anime convention"}}, DefaultGenerationOptions(), rec)
	if err != nil {
		t.Fatalf("StreamGenerate returned error: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("expected done SSE event, got: %s", body)
	}
	if strings.Contains(body, `"type":"error"`) {
		t.Fatalf("did not expect error SSE event, got: %s", body)
	}
	if !strings.Contains(body, "word29") {
		t.Fatalf("expected output to include refilled minimum words, got: %s", body)
	}
	if callCount < 2 {
		t.Fatalf("expected refill call to deepseek, got %d call(s)", callCount)
	}
}

// TestDeepSeekClientStreamGenerateFlexibleBestEffortWhenRefillStalls tests that
// the client returns best-effort results when refill attempts stall.
func TestDeepSeekClientStreamGenerateFlexibleBestEffortWhenRefillStalls(t *testing.T) {
	initialJSON := buildValidSetsJSON(1, 19)
	stalledFillJSON := `{"sets":[{"label":"Set A","words":["word 1","word 2","word 3"]}]}`

	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/chat/completions" {
			callCount++
			var reqBody struct {
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}

			if callCount == 1 {
				if !reqBody.Stream {
					t.Fatalf("expected first call to be stream=true")
				}
				var sse strings.Builder
				for _, tok := range splitTokens(initialJSON, 12) {
					chunk := map[string]interface{}{
						"choices": []map[string]interface{}{
							{"delta": map[string]string{"content": tok}},
						},
					}
					line, _ := json.Marshal(chunk)
					sse.WriteString("data: ")
					sse.Write(line)
					sse.WriteString("\n\n")
				}
				sse.WriteString("data: [DONE]\n\n")

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(sse.String()))
				return
			}

			if reqBody.Stream {
				t.Fatalf("expected refill calls to be stream=false")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, stalledFillJSON)))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "anime convention"}}, DefaultGenerationOptions(), rec)
	if err != nil {
		t.Fatalf("expected flexible best-effort success, got error: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("expected done SSE event, got: %s", body)
	}
	if !strings.Contains(body, "returning best-effort list") {
		t.Fatalf("expected best-effort progress message, got: %s", body)
	}
}

// TestDeepSeekClientStreamGenerateFixedCountUsesDeterministicBackfill tests
// deterministic backfill when refill attempts produce too few words.
func TestDeepSeekClientStreamGenerateFixedCountUsesDeterministicBackfill(t *testing.T) {
	initialJSON := buildValidSetsJSON(1, 26)
	stalledFillJSON := `{"sets":[{"label":"Set A","words":["word 1","word 2","word 3"]}]}`

	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/chat/completions" {
			callCount++
			var reqBody struct {
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}

			if callCount == 1 {
				if !reqBody.Stream {
					t.Fatalf("expected first call to be stream=true")
				}
				var sse strings.Builder
				for _, tok := range splitTokens(initialJSON, 12) {
					chunk := map[string]interface{}{
						"choices": []map[string]interface{}{
							{"delta": map[string]string{"content": tok}},
						},
					}
					line, _ := json.Marshal(chunk)
					sse.WriteString("data: ")
					sse.Write(line)
					sse.WriteString("\n\n")
				}
				sse.WriteString("data: [DONE]\n\n")

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(sse.String()))
				return
			}

			if reqBody.Stream {
				t.Fatalf("expected refill calls to be stream=false")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, stalledFillJSON)))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()
	opts := DefaultGenerationOptions()
	opts.FixedWordCount = 30
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "anime convention"}}, opts, rec)
	if err != nil {
		t.Fatalf("expected deterministic backfill success, got error: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("expected done SSE event, got: %s", body)
	}
	if !strings.Contains(body, "deterministic backfill added") {
		t.Fatalf("expected deterministic backfill progress message, got: %s", body)
	}
}

// TestDeepSeekClientStreamGenerateUsesLowQualityBonusRetry tests the bonus
// retry mechanism when refill attempts produce low-quality placeholders.
func TestDeepSeekClientStreamGenerateUsesLowQualityBonusRetry(t *testing.T) {
	initialJSON := buildValidSetsJSON(1, 26)
	invalidFillJSON := `{"sets":[{"label":"Set A","words":["word 1","word 2","word 3","word 4"]}]}`
	validFillJSON := `{"sets":[{"label":"Set A","words":["artist alley table map","cosplay repair station","dealer hall queue marker","panel room overflow line"]}]}`

	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/chat/completions" {
			callCount++
			var reqBody struct {
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
				t.Fatalf("failed to decode request body: %v", err)
			}

			if callCount == 1 {
				if !reqBody.Stream {
					t.Fatalf("expected first call to be stream=true")
				}
				var sse strings.Builder
				for _, tok := range splitTokens(initialJSON, 12) {
					chunk := map[string]interface{}{
						"choices": []map[string]interface{}{
							{"delta": map[string]string{"content": tok}},
						},
					}
					line, _ := json.Marshal(chunk)
					sse.WriteString("data: ")
					sse.Write(line)
					sse.WriteString("\n\n")
				}
				sse.WriteString("data: [DONE]\n\n")

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(sse.String()))
				return
			}

			if reqBody.Stream {
				t.Fatalf("expected refill calls to be stream=false")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			payload := invalidFillJSON
			// First 5 refill attempts return low-quality placeholders.
			// The 6th refill succeeds and is only reachable via bonus retry.
			if callCount == 7 {
				payload = validFillJSON
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, payload)))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "anime convention"}}, DefaultGenerationOptions(), rec)
	if err != nil {
		t.Fatalf("StreamGenerate returned error: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("expected done SSE event, got: %s", body)
	}
	if strings.Contains(body, `"type":"error"`) {
		t.Fatalf("did not expect error SSE event, got: %s", body)
	}
	if !strings.Contains(body, "high low-quality drop count") {
		t.Fatalf("expected bonus retry progress message, got: %s", body)
	}
	if callCount != 7 {
		t.Fatalf("expected 7 total calls (1 stream + 6 refill), got %d", callCount)
	}
}

// TestDeepSeekClientStreamGenerateEmptyContentRetry tests that when the
// streaming endpoint returns zero content tokens, the client falls back to a
// single non-streaming generateOnce call and succeeds.
func TestDeepSeekClientStreamGenerateEmptyContentRetry(t *testing.T) {
	validJSON := buildValidSetsJSON(1, 30)

	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		callCount++

		var reqBody struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		if callCount == 1 {
			if !reqBody.Stream {
				t.Fatalf("expected first call to be stream=true")
			}
			// Emit an empty SSE stream: no content tokens, just [DONE].
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}

		if reqBody.Stream {
			t.Fatalf("expected retry call to be stream=false")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"content":%q}}]}`, validJSON)))
	}))
	defer srv.Close()

	client := NewDeepSeekClient(srv.URL, "test-api-key", "test-model")
	rec := httptest.NewRecorder()
	err := client.StreamGenerate(context.Background(), []ChatMessage{{Role: "user", Content: "test"}}, DefaultGenerationOptions(), rec)
	if err != nil {
		t.Fatalf("expected empty-content retry success, got error: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("expected done SSE event, got: %s", body)
	}
	if !strings.Contains(body, "empty response received, retrying") {
		t.Fatalf("expected empty-content retry progress message, got: %s", body)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 total calls (1 stream + 1 retry), got %d", callCount)
	}
}
