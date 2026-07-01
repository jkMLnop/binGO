package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// ── DeepSeek implementation ───────────────────────────────────────────────────
//
// DeepSeekClient talks to the DeepSeek hosted API (OpenAI-compatible
// /chat/completions). It replaces the former local OllamaClient as the sole
// LLMClient implementation. Sampling and thinking are CLIENT-level configuration:
// production bakes in the guided-mode winners (thinking disabled, default
// sampling), while the experiment harness varies them per run.

const (
	defaultDeepSeekBaseURL = "https://api.deepseek.com"
	defaultDeepSeekModel   = "deepseek-v4-pro"
	// deepSeekMaxTokens caps output to avoid json_object truncation. Buzzword
	// lists are small; this is a generous ceiling.
	deepSeekMaxTokens = 4096
	// deepSeekStreamTimeout bounds a single streaming generation. DeepSeek closes
	// the connection if inference has not started within 10 minutes; buzzword
	// generation completes well inside this window.
	deepSeekStreamTimeout = 10 * time.Minute
)

// DeepSeekClient implements LLMClient against DeepSeek's /chat/completions.
type DeepSeekClient struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client

	// Sampling / thinking configuration. Production leaves these at guided-mode
	// defaults; the experiment harness sets them per sweep config.
	Thinking    bool     // true => thinking enabled; false (default) => disabled
	Temperature *float64 // nil => omit (API default 1)
	TopP        *float64 // nil => omit (API default 1)

	// MaxTokens overrides deepSeekMaxTokens when > 0. Useful for tests that
	// need to force truncation (finish_reason=="length") with a low cap.
	MaxTokens int

	// Metrics is optional; when set, provider error paths are recorded.
	Metrics *Metrics

	// healthy is an atomically-cached health probe result updated by a
	// background goroutine started via StartHealthCheckLoop(). Read with
	// Healthy(), which avoids a live HTTP call on every request.
	healthy atomic.Bool
}

// StartHealthCheckLoop launches a background goroutine that probes the DeepSeek
// API health every 30 seconds and caches the result. The loop stops when ctx is
// cancelled. Call this after NewDeepSeekClient if you want cached health
// (recommended for production). Without it, Healthy() makes a live HTTP call.
func (c *DeepSeekClient) StartHealthCheckLoop(ctx context.Context) {
	// Probe immediately on start so the cache is populated quickly.
	c.probeAndCache(ctx)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.probeAndCache(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// probeAndCache performs a single health probe and stores the result.
func (c *DeepSeekClient) probeAndCache(ctx context.Context) {
	// Use a 10s timeout for health probes so they never hang.
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	healthy := c.probeHealth(probeCtx)
	c.healthy.Store(healthy)
	if !healthy {
		log.Printf("DeepSeek health check: unhealthy")
	}
}

// probeHealth does the actual GET /models call without touching the cached value.
func (c *DeepSeekClient) probeHealth(ctx context.Context) bool {
	if strings.TrimSpace(c.APIKey) == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// NewDeepSeekClient builds a DeepSeekClient with guided-mode defaults.
func NewDeepSeekClient(baseURL, apiKey, model string) *DeepSeekClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultDeepSeekBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = defaultDeepSeekModel
	}
	return &DeepSeekClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		// No overall client timeout: streaming relies on per-request context
		// deadlines so long generations are not cut off mid-stream.
		HTTPClient: &http.Client{Timeout: 0},
	}
}

// deepSeekRequest is the JSON body for POST /chat/completions.
type deepSeekRequest struct {
	Model          string                  `json:"model"`
	Messages       []ChatMessage           `json:"messages"`
	Stream         bool                    `json:"stream"`
	ResponseFormat *deepSeekResponseFormat `json:"response_format,omitempty"`
	Thinking       *deepSeekThinking       `json:"thinking,omitempty"`
	Temperature    *float64                `json:"temperature,omitempty"`
	TopP           *float64                `json:"top_p,omitempty"`
	MaxTokens      int                     `json:"max_tokens,omitempty"`
}

type deepSeekResponseFormat struct {
	Type string `json:"type"` // "json_object"
}

type deepSeekThinking struct {
	Type string `json:"type"` // "enabled" | "disabled"
}

// deepSeekStreamChunk is one streamed SSE data frame (OpenAI delta shape).
type deepSeekStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// deepSeekResponse is a non-streamed completion (used for refill/repair).
type deepSeekResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// buildRequest assembles the request body with client-level sampling/thinking.
func (c *DeepSeekClient) buildRequest(messages []ChatMessage, stream bool) deepSeekRequest {
	maxTok := deepSeekMaxTokens
	if c.MaxTokens > 0 {
		maxTok = c.MaxTokens
	}
	req := deepSeekRequest{
		Model:          c.Model,
		Messages:       messages,
		Stream:         stream,
		ResponseFormat: &deepSeekResponseFormat{Type: "json_object"},
		MaxTokens:      maxTok,
	}
	if c.Thinking {
		req.Thinking = &deepSeekThinking{Type: "enabled"}
	} else {
		req.Thinking = &deepSeekThinking{Type: "disabled"}
	}
	// Sampling: at most one of temperature/top_p should be set per the DeepSeek
	// guidance. The caller (harness) is responsible for not setting both.
	if c.Temperature != nil {
		req.Temperature = c.Temperature
	}
	if c.TopP != nil {
		req.TopP = c.TopP
	}
	return req
}

// newHTTPRequest builds an authenticated POST /chat/completions request.
func (c *DeepSeekClient) newHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	return req, nil
}

// statusError maps a DeepSeek HTTP status to a wrapped error and records a
// metric when available. 400/422 indicate a request bug (not wrapped as
// unavailable); the rest are treated as transient provider unavailability.
func (c *DeepSeekClient) statusError(code int) error {
	if c.Metrics != nil {
		switch code {
		case http.StatusUnauthorized, http.StatusPaymentRequired:
			c.Metrics.RecordError("auth")
		default:
			c.Metrics.RecordError("llm")
		}
	}
	switch code {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return fmt.Errorf("deepseek rejected request (HTTP %d)", code)
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: deepseek authentication failed (HTTP 401)", ErrLLMUnavailable)
	case http.StatusPaymentRequired:
		return fmt.Errorf("%w: deepseek insufficient balance (HTTP 402)", ErrLLMUnavailable)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%w: deepseek rate/concurrency limit (HTTP 429)", ErrLLMUnavailable)
	case http.StatusInternalServerError:
		return fmt.Errorf("%w: deepseek server error (HTTP 500)", ErrLLMUnavailable)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("%w: deepseek overloaded (HTTP 503)", ErrLLMUnavailable)
	default:
		return fmt.Errorf("%w: deepseek returned HTTP %d", ErrLLMUnavailable, code)
	}
}

// Healthy returns the cached health probe result. If the background health
// check loop was started (via StartHealthCheckLoop), this returns the value from
// the most recent probe, so it is safe to call on every /api/status request.
// If the loop was not started, Healthy() performs a live probe.
func (c *DeepSeekClient) Healthy(ctx context.Context) bool {
	// Fast path: use the cached value when the background loop is running
	// (the atomic will be non-default). Always falls back to live probe for
	// tests that never call StartHealthCheckLoop.
	if c.healthy.Load() {
		return true
	}
	// If the cache is false, it could mean (a) the background loop hasn't
	// populated yet, (b) the provider is unhealthy, or (c) no loop was started.
	// Perform a live probe to be sure.
	return c.probeHealth(ctx)
}

// generateOnce performs a single non-streaming completion and returns the raw
// content. Used by the refill and repair passes. The target word count is
// conveyed via the prompt (json_object mode cannot enforce array bounds like a
// schema).
func (c *DeepSeekClient) generateOnce(ctx context.Context, messages []ChatMessage, opts GenerationOptions) (string, error) {
	fullMessages := make([]ChatMessage, 0, len(messages)+1)
	fullMessages = append(fullMessages, ChatMessage{Role: "system", Content: systemPromptForMode(opts)})
	fullMessages = append(fullMessages, messages...)

	body, err := json.Marshal(c.buildRequest(fullMessages, false))
	if err != nil {
		return "", fmt.Errorf("failed to marshal deepseek request: %w", err)
	}

	req, err := c.newHTTPRequest(ctx, body)
	if err != nil {
		return "", fmt.Errorf("failed to build deepseek request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: deepseek request failed: %v", ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", c.statusError(resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read deepseek response: %w", err)
	}

	var parsed deepSeekResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse deepseek response: %w", err)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("deepseek returned empty content")
	}

	return parsed.Choices[0].Message.Content, nil
}

// StreamGenerate sends messages to DeepSeek with the system prompt prepended,
// streams tokens to w as SSE data events, then sends a final "done" event
// containing the validated {sets:[...]} JSON extracted from the full response.
func (c *DeepSeekClient) StreamGenerate(ctx context.Context, messages []ChatMessage, opts GenerationOptions, w http.ResponseWriter) error {
	fullMessages := make([]ChatMessage, 0, len(messages)+1)
	fullMessages = append(fullMessages, ChatMessage{Role: "system", Content: systemPromptForMode(opts)})
	fullMessages = append(fullMessages, messages...)

	body, err := json.Marshal(c.buildRequest(fullMessages, true))
	if err != nil {
		return fmt.Errorf("failed to marshal deepseek request: %w", err)
	}

	streamCtx, cancel := context.WithTimeout(ctx, deepSeekStreamTimeout)
	defer cancel()

	req, err := c.newHTTPRequest(streamCtx, body)
	if err != nil {
		return fmt.Errorf("failed to build deepseek request: %w", err)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: deepseek request failed: %v", ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.statusError(resp.StatusCode)
	}

	// Write SSE headers before the first byte.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, hasFlusher := w.(http.Flusher)

	var accumulated strings.Builder
	var finishReason string
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		// Skip SSE comment / keep-alive lines (e.g. ": keep-alive").
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk deepSeekStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // skip malformed frames
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
			if choice.Delta.Content == "" {
				continue
			}
			accumulated.WriteString(choice.Delta.Content)
			writeSSE(w, SSEEvent{Type: "token", Content: choice.Delta.Content})
			if hasFlusher {
				flusher.Flush()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if accumulated.Len() == 0 {
			writeSSE(w, SSEEvent{Type: "error", Error: fmt.Sprintf("error reading LLM stream: %v", err)})
			if hasFlusher {
				flusher.Flush()
			}
			return fmt.Errorf("error reading deepseek stream: %w", err)
		}
		writeSSE(w, SSEEvent{Type: "token", Content: "\nFinishing up processing: stream interrupted, attempting recovery from partial output...\n"})
		if hasFlusher {
			flusher.Flush()
		}
	}

	// Detect truncation: if the API hit max_tokens mid-output, the accumulated
	// JSON is likely incomplete. Emit a warning so the repair/refill logic below
	// can recover gracefully.
	if finishReason == "length" && accumulated.Len() > 0 {
		truncMsg := "\nFinishing up processing: model output was truncated (max_tokens reached), attempting to recover from partial output...\n"
		writeSSE(w, SSEEvent{Type: "token", Content: truncMsg})
		if hasFlusher {
			flusher.Flush()
		}
	}

	// Known DeepSeek quirk: json_object mode may occasionally return empty
	// content. Do one non-streaming retry before giving up.
	if strings.TrimSpace(accumulated.String()) == "" {
		writeSSE(w, SSEEvent{Type: "token", Content: "\nFinishing up processing: empty response received, retrying...\n"})
		if hasFlusher {
			flusher.Flush()
		}
		retryCtx, retryCancel := context.WithTimeout(context.Background(), deepSeekStreamTimeout)
		retried, retryErr := c.generateOnce(retryCtx, messages, opts)
		retryCancel()
		if retryErr == nil {
			accumulated.WriteString(retried)
		}
	}

	// Parse and validate the final JSON; one guarded repair pass on failure.
	sets, err := extractGeneratedSets(accumulated.String())
	if err != nil {
		lastErr := err
		repairMessages := append([]ChatMessage(nil), messages...)
		for attempt := 0; attempt < maxParseRepairAttempts; attempt++ {
			writeSSE(w, SSEEvent{Type: "token", Content: "\nFinishing up processing: malformed model output detected, attempting structured repair...\n"})
			if hasFlusher {
				flusher.Flush()
			}
			repairMessages = append(repairMessages, ChatMessage{
				Role:    "system",
				Content: "Repair pass: return strict JSON only matching the schema with one set and a words array. No prose, no markdown, no commentary.",
			})
			repairCtx, repairCancel := context.WithTimeout(context.Background(), deepSeekStreamTimeout)
			repairedContent, repairErr := c.generateOnce(repairCtx, repairMessages, opts)
			repairCancel()
			if repairErr != nil {
				lastErr = repairErr
				continue
			}
			repairedSets, parseErr := extractGeneratedSets(repairedContent)
			if parseErr == nil {
				sets = repairedSets
				err = nil
				break
			}
			lastErr = parseErr
		}
		if err != nil {
			writeSSE(w, SSEEvent{Type: "error", Error: fmt.Sprintf("failed to parse LLM output: %v", lastErr)})
			if hasFlusher {
				flusher.Flush()
			}
			return fmt.Errorf("failed to parse LLM output: %w", lastErr)
		}
	}

	// fillMissingWords may run after the HTTP stream context closes (browser
	// already received all tokens), so use a fresh context with ample timeout.
	fillCtx, fillCancel := context.WithTimeout(context.Background(), deepSeekStreamTimeout)
	defer fillCancel()
	sets, err = c.fillMissingWords(fillCtx, messages, opts, sets, func(msg string) {
		writeSSE(w, SSEEvent{Type: "token", Content: "\n" + msg + "\n"})
		if hasFlusher {
			flusher.Flush()
		}
	})
	if err != nil {
		writeSSE(w, SSEEvent{Type: "error", Error: fmt.Sprintf("failed to satisfy fixed list size: %v", err)})
		if hasFlusher {
			flusher.Flush()
		}
		return fmt.Errorf("failed fixed list size handling: %w", err)
	}

	writeSSE(w, SSEEvent{Type: "done", Sets: sets.Sets})
	if hasFlusher {
		flusher.Flush()
	}
	return nil
}

// fillMissingWords parses, de-duplicates, refills, and diversity-checks the
// generated word list. Provider-agnostic logic preserved from the prior
// implementation; only the generation transport (generateOnce) is DeepSeek.
func (c *DeepSeekClient) fillMissingWords(ctx context.Context, messages []ChatMessage, opts GenerationOptions, sets *GeneratedSets, onProgress func(string)) (*GeneratedSets, error) {
	target := opts.FixedWordCount
	if target <= 0 {
		target = DefaultMinimumWordCount
	}
	// Auto-refill currently targets the primary one-set flow.
	if len(sets.Sets) != 1 {
		return sets, nil
	}

	current, initialStats := normalizeUniqueWordsWithStats(sets.Sets[0].Words)
	if onProgress != nil {
		onProgress(fmt.Sprintf(
			"Post-processing: parsed %d items, kept %d unique, removed %d duplicates, %d too-long, %d low-quality, %d empty.",
			initialStats.InputCount,
			initialStats.KeptCount,
			initialStats.DroppedDuplicates,
			initialStats.DroppedTooLong,
			initialStats.DroppedLowQuality,
			initialStats.DroppedEmpty,
		))
	}
	if len(current) >= target {
		if opts.FixedWordCount > 0 {
			sets.Sets[0].Words = current[:target]
		} else {
			sets.Sets[0].Words = current
		}
		return sets, nil
	}

	if onProgress != nil {
		onProgress(fmt.Sprintf("Finishing up processing: %d/%d words ready. Auto-filling missing items...", len(current), target))
	}

	refillAttemptLimit := maxRefillAttempts
	usedLowQualityBonusRetry := false
	for attempt := 0; attempt < refillAttemptLimit && len(current) < target; attempt++ {
		missing := target - len(current)
		beforeCount := len(current)
		retryMessages := make([]ChatMessage, 0, len(messages)+1)
		retryMessages = append(retryMessages, messages...)
		nonce := time.Now().UnixNano()
		retryMessages = append(retryMessages, ChatMessage{
			Role: "system",
			Content: fmt.Sprintf(
				"Auto-refill pass %d/%d (nonce=%d). Add exactly %d NEW unique buzzwords not already listed. Do not repeat, paraphrase, or slightly reword any listed item. Do not output placeholders or template text (examples forbidden: 'word 1', 'some word here', 'placeholder', 'example phrase'). Existing words (forbidden): %s. Return JSON only in the same schema with one set containing only the missing words.",
				attempt+1,
				refillAttemptLimit,
				nonce,
				missing,
				strings.Join(current, ", "),
			),
		})
		if onProgress != nil {
			onProgress(fmt.Sprintf("Finishing up processing: refill attempt %d/%d for %d missing words...", attempt+1, refillAttemptLimit, missing))
		}
		content, err := c.generateOnce(ctx, retryMessages, opts)
		if err != nil {
			return nil, err
		}
		extraSets, err := extractGeneratedSets(content)
		if err != nil {
			continue
		}
		if len(extraSets.Sets) == 0 {
			continue
		}
		merged := append(append([]string(nil), current...), extraSets.Sets[0].Words...)
		normalized, mergeStats := normalizeUniqueWordsWithStats(merged)
		current = normalized
		if onProgress != nil {
			onProgress(fmt.Sprintf(
				"Finishing up processing: merge stats -> now %d unique, removed %d duplicates, %d too-long, %d low-quality, %d empty.",
				len(current),
				mergeStats.DroppedDuplicates,
				mergeStats.DroppedTooLong,
				mergeStats.DroppedLowQuality,
				mergeStats.DroppedEmpty,
			))
		}
		if mergeStats.DroppedLowQuality >= lowQualityBonusRetryThreshold && !usedLowQualityBonusRetry {
			usedLowQualityBonusRetry = true
			refillAttemptLimit++
			if onProgress != nil {
				onProgress(fmt.Sprintf("Finishing up processing: high low-quality drop count (%d) detected, granting one extra guarded refill attempt.", mergeStats.DroppedLowQuality))
			}
		}
		if len(current) == beforeCount && onProgress != nil {
			onProgress("Finishing up processing: refill yielded no new unique words, retrying with stronger constraints...")
		}
	}

	if len(current) < target {
		if opts.FixedWordCount > 0 {
			missing := target - len(current)
			fallbackWords := deterministicBackfillWords(current, missing)
			if len(fallbackWords) > 0 {
				current = append(current, fallbackWords...)
				if onProgress != nil {
					onProgress(fmt.Sprintf("Finishing up processing: deterministic backfill added %d words to satisfy fixed size.", len(fallbackWords)))
				}
			}
			if len(current) >= target {
				sets.Sets[0].Words = current[:target]
				return sets, nil
			}
			return nil, fmt.Errorf("fixed word count requested: got %d, expected %d", len(current), target)
		}
		if len(current) >= FlexibleHardMinimumWordCount {
			if onProgress != nil {
				onProgress(fmt.Sprintf("Finishing up processing: returning best-effort list (%d words). Preferred minimum is %d.", len(current), target))
			}
			sets.Sets[0].Words = current
			return sets, nil
		}
		return nil, fmt.Errorf("minimum word count not met: got %d, expected at least %d", len(current), target)
	}
	if opts.FixedWordCount > 0 {
		sets.Sets[0].Words = current[:target]
	} else {
		sets.Sets[0].Words = current
	}

	if low, reason := detectLowDiversity(sets.Sets[0].Words); low {
		if onProgress != nil {
			onProgress("Finishing up processing: detected repetitive output (" + reason + "), running one diversity retry...")
		}
		for retry := 0; retry < maxDiversityRetries; retry++ {
			retryMessages := make([]ChatMessage, 0, len(messages)+1)
			retryMessages = append(retryMessages, messages...)
			retryMessages = append(retryMessages, ChatMessage{
				Role:    "system",
				Content: fmt.Sprintf("Diversity retry: produce highly varied, non-repetitive phrases. Avoid reusing the same leading word across many entries. Generate distinct observable sightings. Return exactly %d words in one set.", target),
			})
			content, err := c.generateOnce(ctx, retryMessages, opts)
			if err != nil {
				continue
			}
			retrySets, err := extractGeneratedSets(content)
			if err != nil || len(retrySets.Sets) == 0 {
				continue
			}
			candidate := normalizeUniqueWords(retrySets.Sets[0].Words)
			if len(candidate) < target {
				continue
			}
			candidate = candidate[:target]
			if lowRetry, _ := detectLowDiversity(candidate); !lowRetry {
				sets.Sets[0].Words = candidate
				break
			}
		}
	}
	return sets, nil
}
