package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// buzzwordSystemPrompt is the system instruction sent to the LLM for every generation request.
// Validated by experiments: direct JSON-only instruction (no "final answer" phrasing) reduces
// thinking-mode leakage on qwen3 models and improves schema compliance.
const buzzwordSystemPrompt = `You are a bingo card designer. Generate a themed buzzword list for a scavenger-hunt style bingo game. The words should be short observable things (2-5 words), specific to the topic, and fun to spot in person. Output 1 set of buzzwords. Return JSON only in the form {"sets":[{"label":"Set A","words":[...]}]}`

const agenticRetrievalSystemPrompt = `When URL excerpts are available, prioritize concrete entities from those excerpts (people, places, booths, events, props, schedules, signage). Avoid abstract categories unless they map to a visible, verifiable in-person sighting. Keep wording short and observable.`

const (
	GenerationModeGuidedPrompt     = "guided-prompt"
	GenerationModeAgenticRetrieval = "agentic-retrieval"
	DefaultMinimumWordCount        = 30
	FlexibleHardMinimumWordCount   = 15
	maxRefillAttempts              = 5
	lowQualityBonusRetryThreshold  = 3
	maxDiversityRetries            = 1
	maxParseRepairAttempts         = 1
)

var quotedPhrasePattern = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

var lowQualityBuzzwordPattern = regexp.MustCompile(`^(word|phrase|item|thing)\s+\d+$`)
var metadataBuzzwordPattern = regexp.MustCompile(`^(name|value|group|type|id|url|description)\s*:`)

var deterministicFallbackWordPool = []string{
	"Event entrance banner",
	"Staff information desk",
	"Printed floor map",
	"Photo backdrop wall",
	"Queue line marker",
	"Directional wayfinding sign",
	"Schedule board listing",
	"Vendor checkout counter",
	"Cosplay repair station",
	"Lanyard badge holder",
	"Autograph signing table",
	"Panel room doorway",
	"Merchandise display rack",
	"Concessions drink station",
	"Security checkpoint table",
	"Community meetup board",
	"Contest stage lighting",
	"Volunteer help kiosk",
	"Flyer handout stack",
	"Raffle prize display",
}

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

// GenerationOptions controls per-request LLM parameters surfaced to the UI.
// Zero values mean "use the server default" for each field.
type GenerationOptions struct {
	// GenerationMode controls prompt strategy.
	// "guided-prompt" (default) | "agentic-retrieval".
	GenerationMode string
	// FixedWordCount enforces an exact count when >0. Default 0 (flexible length).
	FixedWordCount int
}

// DefaultGenerationOptions returns the experimentally-validated defaults.
func DefaultGenerationOptions() GenerationOptions {
	return GenerationOptions{
		GenerationMode: GenerationModeGuidedPrompt,
		FixedWordCount: 0,
	}
}

func normalizeGenerationMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case GenerationModeAgenticRetrieval:
		return GenerationModeAgenticRetrieval
	default:
		return GenerationModeGuidedPrompt
	}
}

// LLMClient is the interface for streaming LLM generation.
// StreamGenerate prepends the bingo-card system prompt, forwards user messages
// to the LLM, and writes SSE frames (token / done / error) to w.
type LLMClient interface {
	StreamGenerate(ctx context.Context, messages []ChatMessage, opts GenerationOptions, w http.ResponseWriter) error
	Healthy(ctx context.Context) bool
}

var (
	ErrLLMUnavailable = errors.New("llm unavailable")
)

const (
	// llmRequestTimeout controls the max room-validity window derived for clients.
	llmRequestTimeout = 4 * time.Hour
	// roomExpiryBuffer gives users extra time after generation for review/feedback.
	roomExpiryBuffer = 2 * time.Hour
)

// roomCodeTTL returns the recommended room validity window for clients.
func roomCodeTTL() time.Duration {
	return llmRequestTimeout + roomExpiryBuffer
}

type wordNormalizationStats struct {
	InputCount        int
	KeptCount         int
	DroppedEmpty      int
	DroppedTooLong    int
	DroppedLowQuality int
	DroppedDuplicates int
}

func isLowQualityBuzzword(word string) bool {
	normalized := strings.ToLower(strings.TrimSpace(word))
	if normalized == "" {
		return false
	}
	normalized = strings.Join(strings.Fields(normalized), " ")

	lowQualityPhrases := []string{
		"some word here",
		"insert word here",
		"placeholder",
		"placeholder text",
		"not a real word",
		"example word",
		"example phrase",
		"sample word",
		"sample phrase",
		"lorem ipsum",
		"just kidding",
		"this is a real set of words",
		"with some extra stuff",
		"dummy text",
	}
	for _, phrase := range lowQualityPhrases {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}

	if metadataBuzzwordPattern.MatchString(normalized) {
		return true
	}

	return lowQualityBuzzwordPattern.MatchString(normalized)
}

func systemPromptForMode(opts GenerationOptions) string {
	systemPrompt := buzzwordSystemPrompt
	if normalizeGenerationMode(opts.GenerationMode) == GenerationModeAgenticRetrieval {
		systemPrompt += "\n\n" + agenticRetrievalSystemPrompt
	}
	return systemPrompt
}

func normalizeUniqueWordsWithStats(words []string) ([]string, wordNormalizationStats) {
	stats := wordNormalizationStats{InputCount: len(words)}
	result := make([]string, 0, len(words))
	seen := make(map[string]bool, len(words))
	for _, word := range words {
		trimmed := strings.TrimSpace(word)
		if trimmed == "" {
			stats.DroppedEmpty++
			continue
		}
		if len(trimmed) > 60 {
			stats.DroppedTooLong++
			continue
		}
		if isLowQualityBuzzword(trimmed) {
			stats.DroppedLowQuality++
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			stats.DroppedDuplicates++
			continue
		}
		seen[key] = true
		result = append(result, trimmed)
	}
	stats.KeptCount = len(result)
	return result, stats
}

func normalizeUniqueWords(words []string) []string {
	result, _ := normalizeUniqueWordsWithStats(words)
	return result
}

func deterministicBackfillWords(existing []string, needed int) []string {
	if needed <= 0 {
		return nil
	}

	seen := make(map[string]bool, len(existing))
	for _, word := range existing {
		seen[strings.ToLower(strings.TrimSpace(word))] = true
	}

	result := make([]string, 0, needed)
	for _, candidate := range deterministicFallbackWordPool {
		key := strings.ToLower(candidate)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, candidate)
		if len(result) >= needed {
			return result
		}
	}

	for i := 1; len(result) < needed; i++ {
		candidate := fmt.Sprintf("Event sighting %d", i)
		key := strings.ToLower(candidate)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, candidate)
	}

	return result
}

func detectLowDiversity(words []string) (bool, string) {
	if len(words) < 10 {
		return false, ""
	}

	firstTokenCounts := map[string]int{}
	prefixCounts := map[string]int{}
	for _, word := range words {
		trimmed := strings.ToLower(strings.TrimSpace(word))
		if trimmed == "" {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) > 0 {
			firstTokenCounts[parts[0]]++
		}
		if len(trimmed) >= 12 {
			prefixCounts[trimmed[:12]]++
		}
	}

	maxFirstToken := 0
	maxPrefix := 0
	for _, n := range firstTokenCounts {
		if n > maxFirstToken {
			maxFirstToken = n
		}
	}
	for _, n := range prefixCounts {
		if n > maxPrefix {
			maxPrefix = n
		}
	}

	ratioFirstToken := float64(maxFirstToken) / float64(len(words))
	ratioPrefix := float64(maxPrefix) / float64(len(words))
	if ratioFirstToken >= 0.55 {
		return true, fmt.Sprintf("dominant first-token repetition %.0f%%", ratioFirstToken*100)
	}
	if ratioPrefix >= 0.35 {
		return true, fmt.Sprintf("dominant phrase-prefix repetition %.0f%%", ratioPrefix*100)
	}
	return false, ""
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
// it conforms to GeneratedSets in one of two accepted formats:
// - exactly 1 set (preferred) or exactly 3 sets (legacy)
// Each word must be non-empty and <=60 chars.
// Note: per-set word counts are flexible by default; fixed size is enforced
// separately via GenerationOptions.FixedWordCount when requested by the user.
func extractGeneratedSets(text string) (*GeneratedSets, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		fallback := extractQuotedWordFallback(text)
		if fallback != nil {
			return fallback, nil
		}
		return nil, fmt.Errorf("no JSON object found in LLM output")
	}
	jsonStr := text[start : end+1]

	var result GeneratedSets
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		fallback := extractQuotedWordFallback(text)
		if fallback != nil {
			return fallback, nil
		}
		return nil, fmt.Errorf("failed to unmarshal generated sets: %w", err)
	}
	setCount := len(result.Sets)
	if setCount != 1 && setCount != 3 {
		return nil, fmt.Errorf("LLM must return either 1 set (preferred) or 3 sets (legacy), got %d sets", setCount)
	}

	for i, set := range result.Sets {
		if strings.TrimSpace(set.Label) == "" {
			return nil, fmt.Errorf("set %d has an empty label", i+1)
		}
		if len(set.Words) == 0 {
			return nil, fmt.Errorf("set %d (%q) must contain at least 1 word", i+1, set.Label)
		}
	}
	return &result, nil
}

func extractQuotedWordFallback(text string) *GeneratedSets {
	matches := quotedPhrasePattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	words := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		decoded, err := strconv.Unquote("\"" + match[1] + "\"")
		if err != nil {
			decoded = match[1]
		}
		trimmed := strings.TrimSpace(decoded)
		if trimmed == "" || len(trimmed) > 60 {
			continue
		}
		if isLowQualityBuzzword(trimmed) {
			continue
		}
		lower := strings.ToLower(trimmed)
		if lower == "sets" || lower == "set" || lower == "label" || lower == "words" || strings.HasPrefix(lower, "set ") {
			continue
		}
		if seen[lower] {
			continue
		}
		seen[lower] = true
		words = append(words, trimmed)
	}

	if len(words) == 0 {
		return nil
	}

	return &GeneratedSets{Sets: []WordSet{{Label: "Set A", Words: words}}}
}
