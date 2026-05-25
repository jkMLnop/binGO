package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// buzzwordSystemPrompt is the system instruction sent to the LLM for every generation request.
const buzzwordSystemPrompt = `You are a bingo card designer. Generate a themed buzzword list for a scavenger-hunt style bingo game. The words should be short observable things (2–5 words), specific to the topic, and fun to spot in person. Output 3 distinct sets of 25 words each. Return your final answer as JSON: {"sets": [{"label": "Set A", "words": [...]}, {"label": "Set B", "words": [...]}, {"label": "Set C", "words": [...]}]}`

// ChatMessage is a single turn in an LLM conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// WordSet is one labelled set of generated buzzwords.
type WordSet struct {
	Label string   `json:"label"`
	Words []string `json:"words"`
}

// GeneratedSets is the final structured output validated from LLM text.
type GeneratedSets struct {
	Sets []WordSet `json:"sets"`
}

// SSEEvent is the JSON payload for each SSE frame sent to the web client.
// type is one of: "token", "done", "error".
type SSEEvent struct {
	Type    string    `json:"type"`
	Content string    `json:"content,omitempty"`
	Sets    []WordSet `json:"sets,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// LLMClient is the interface for streaming LLM generation.
// StreamGenerate prepends the bingo-card system prompt, forwards user messages
// to the LLM, and writes SSE frames (token / done / error) to w.
type LLMClient interface {
	StreamGenerate(ctx context.Context, messages []ChatMessage, w http.ResponseWriter) error
	Healthy(ctx context.Context) bool
}

// ── Ollama implementation ─────────────────────────────────────────────────────

// OllamaClient calls the Ollama /api/chat endpoint.
type OllamaClient struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

// NewOllamaClient creates an OllamaClient with sensible timeouts.
func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Minute, // long timeout for streaming generation
		},
	}
}

// Healthy probes GET $OLLAMA_BASE_URL/api/tags and returns true if Ollama responds 200.
func (c *OllamaClient) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ollamaRequest is the JSON body for POST /api/chat.
type ollamaRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ollamaChunk is one NDJSON line in Ollama's streaming response.
type ollamaChunk struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// StreamGenerate sends messages to Ollama with the system prompt prepended,
// streams tokens to w as SSE data events, then sends a final "done" event
// containing the validated {sets:[...]} JSON extracted from the full response.
func (c *OllamaClient) StreamGenerate(ctx context.Context, messages []ChatMessage, w http.ResponseWriter) error {
	// Prepend system prompt
	fullMessages := make([]ChatMessage, 0, len(messages)+1)
	fullMessages = append(fullMessages, ChatMessage{Role: "system", Content: buzzwordSystemPrompt})
	fullMessages = append(fullMessages, messages...)

	body, err := json.Marshal(ollamaRequest{
		Model:    c.Model,
		Messages: fullMessages,
		Stream:   true,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}

	// Write SSE headers before the first byte
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, hasFlusher := w.(http.Flusher)

	var accumulated strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk ollamaChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue // skip malformed lines
		}
		if !chunk.Done && chunk.Message.Content != "" {
			accumulated.WriteString(chunk.Message.Content)
			writeSSE(w, SSEEvent{Type: "token", Content: chunk.Message.Content})
			if hasFlusher {
				flusher.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading ollama stream: %w", err)
	}

	// Parse and validate the final JSON from the accumulated text
	sets, err := extractGeneratedSets(accumulated.String())
	if err != nil {
		writeSSE(w, SSEEvent{Type: "error", Error: fmt.Sprintf("failed to parse LLM output: %v", err)})
		if hasFlusher {
			flusher.Flush()
		}
		return fmt.Errorf("failed to parse LLM output: %w", err)
	}

	writeSSE(w, SSEEvent{Type: "done", Sets: sets.Sets})
	if hasFlusher {
		flusher.Flush()
	}
	return nil
}

// writeSSE encodes event as JSON and writes an SSE data frame to w.
func writeSSE(w http.ResponseWriter, event SSEEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// ── Structured output extraction ──────────────────────────────────────────────

// extractGeneratedSets finds the first {...} JSON object in text and validates
// it conforms to GeneratedSets: ≥1 set, each set ≥24 words, each word ≤60 chars,
// and no word appears in more than one set.
func extractGeneratedSets(text string) (*GeneratedSets, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in LLM output")
	}
	jsonStr := text[start : end+1]

	var result GeneratedSets
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal generated sets: %w", err)
	}
	if len(result.Sets) == 0 {
		return nil, fmt.Errorf("LLM returned no word sets")
	}

	seen := make(map[string]bool)
	for i, set := range result.Sets {
		if len(set.Words) < 24 {
			return nil, fmt.Errorf("set %d (%q) has only %d words — need at least 24", i+1, set.Label, len(set.Words))
		}
		for _, word := range set.Words {
			if len(word) > 60 {
				return nil, fmt.Errorf("word too long (max 60 chars): %q", word)
			}
			key := strings.ToLower(strings.TrimSpace(word))
			if seen[key] {
				return nil, fmt.Errorf("duplicate word across sets: %q", word)
			}
			seen[key] = true
		}
	}
	return &result, nil
}
