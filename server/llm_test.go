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

func TestExtractGeneratedSetsSingleSet50Valid(t *testing.T) {
	sets := buildValidSetsJSON(1, 50)
	result, err := extractGeneratedSets(sets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Sets) != 1 {
		t.Errorf("expected 1 set, got %d", len(result.Sets))
	}
	if len(result.Sets[0].Words) != 50 {
		t.Errorf("expected 50 words, got %d", len(result.Sets[0].Words))
	}
}

func TestExtractGeneratedSetsFlexibleWordCount(t *testing.T) {
	sets := buildValidSetsJSON(1, 39)
	result, err := extractGeneratedSets(sets)
	if err != nil {
		t.Fatalf("unexpected error for flexible word count: %v", err)
	}
	if len(result.Sets) != 1 || len(result.Sets[0].Words) != 39 {
		t.Fatalf("expected 1 set with 39 words, got %+v", result.Sets)
	}
}

func TestExtractGeneratedSetsWrongSetCount(t *testing.T) {
	sets := buildValidSetsJSON(2, 25)
	_, err := extractGeneratedSets(sets)
	if err == nil {
		t.Error("expected error for wrong number of sets, got nil")
	}
}

func TestExtractGeneratedSetsDuplicateAcrossSetsAllowed(t *testing.T) {
	// Three sets where two share a word
	raw := `{"sets":[` +
		buildSetJSON("Set A", 25, "shared") + "," +
		buildSetJSON("Set B", 25, "shared") +
		"," +
		buildSetJSON("Set C", 25, "unique-c") +
		`]}`
	sets, err := extractGeneratedSets(raw)
	if err != nil {
		t.Fatalf("expected duplicates to pass parse stage, got error: %v", err)
	}
	if len(sets.Sets) != 3 {
		t.Fatalf("expected 3 sets, got %d", len(sets.Sets))
	}
}

func TestExtractGeneratedSetsWordTooLongAllowedAtParseStage(t *testing.T) {
	longWord := strings.Repeat("x", 61)
	words := make([]string, 25)
	for i := range words {
		words[i] = fmt.Sprintf("word%d", i)
	}
	words[0] = longWord
	wordsJSON, _ := json.Marshal(words)
	raw := fmt.Sprintf(`{"sets":[{"label":"Set A","words":%s},{"label":"Set B","words":%s},{"label":"Set C","words":%s}]}`, string(wordsJSON), string(wordsJSON), string(wordsJSON))
	sets, err := extractGeneratedSets(raw)
	if err != nil {
		t.Fatalf("expected parse success for long word (filtered later), got: %v", err)
	}
	if len(sets.Sets) != 3 {
		t.Fatalf("expected 3 sets, got %d", len(sets.Sets))
	}
}

func TestExtractGeneratedSetsNoJSON(t *testing.T) {
	_, err := extractGeneratedSets("Here are some words but no JSON object at all")
	if err == nil {
		t.Error("expected error when no JSON found, got nil")
	}
}

func TestExtractGeneratedSetsMalformedJSONFallback(t *testing.T) {
	raw := `{"sets":[{"label":"Set A","words":["Anime convention","Convention hall","Convention booth",` +
		`"Anime merchandise"` // intentionally truncated/malformed JSON
	sets, err := extractGeneratedSets(raw)
	if err != nil {
		t.Fatalf("expected fallback parse success, got error: %v", err)
	}
	if len(sets.Sets) != 1 {
		t.Fatalf("expected 1 set from fallback, got %d", len(sets.Sets))
	}
	if len(sets.Sets[0].Words) < 4 {
		t.Fatalf("expected fallback to recover words, got %v", sets.Sets[0].Words)
	}
}

func TestIsLowQualityBuzzword(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "placeholder phrase", in: "some word here", want: true},
		{name: "numbered placeholder", in: "word 12", want: true},
		{name: "meta joke phrase", in: "Just kidding, this is a real set of words but with some extra stuff", want: true},
		{name: "metadata key value", in: "name: Anime North Convention", want: true},
		{name: "normal buzzword", in: "Artist alley sketchbook", want: false},
		{name: "specific sighting", in: "Cosplay armor prop", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isLowQualityBuzzword(tc.in)
			if got != tc.want {
				t.Fatalf("isLowQualityBuzzword(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeUniqueWordsWithStatsFiltersLowQuality(t *testing.T) {
	input := []string{"Cosplay armor prop", "placeholder", "word 2", "Cosplay armor prop", "Panel room line"}
	words, stats := normalizeUniqueWordsWithStats(input)

	if len(words) != 2 {
		t.Fatalf("expected 2 kept words, got %d (%v)", len(words), words)
	}
	if stats.DroppedLowQuality != 2 {
		t.Fatalf("expected 2 low-quality drops, got %d", stats.DroppedLowQuality)
	}
	if stats.DroppedDuplicates != 1 {
		t.Fatalf("expected 1 duplicate drop, got %d", stats.DroppedDuplicates)
	}
}

func TestExtractGeneratedSetsQuotedListFallback(t *testing.T) {
	raw := `"Anime North", "Convention booth", "Cosplay contest", "Convention booth"`
	sets, err := extractGeneratedSets(raw)
	if err != nil {
		t.Fatalf("expected fallback parse success, got error: %v", err)
	}
	if len(sets.Sets) != 1 {
		t.Fatalf("expected 1 set from fallback, got %d", len(sets.Sets))
	}
	if len(sets.Sets[0].Words) != 3 {
		t.Fatalf("expected deduped 3 words, got %d (%v)", len(sets.Sets[0].Words), sets.Sets[0].Words)
	}
}

func TestExtractGeneratedSetsQuotedListFallbackFiltersLowQuality(t *testing.T) {
	raw := `"Anime North", "some word here", "placeholder", "Cosplay contest"`
	sets, err := extractGeneratedSets(raw)
	if err != nil {
		t.Fatalf("expected fallback parse success, got error: %v", err)
	}
	if len(sets.Sets) != 1 {
		t.Fatalf("expected 1 set from fallback, got %d", len(sets.Sets))
	}
	if len(sets.Sets[0].Words) != 2 {
		t.Fatalf("expected low-quality phrases to be filtered, got %d (%v)", len(sets.Sets[0].Words), sets.Sets[0].Words)
	}
}

func TestExtractGeneratedSetsAllowsEmptyWordsAtParseStage(t *testing.T) {
	raw := `{"sets":[{"label":"Set A","words":["Anime North","", "Cosplay contest"]}]}`
	sets, err := extractGeneratedSets(raw)
	if err != nil {
		t.Fatalf("expected parse success with empty words (filtered later), got: %v", err)
	}
	if len(sets.Sets) != 1 {
		t.Fatalf("expected 1 set, got %d", len(sets.Sets))
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

func TestDetectLowDiversity(t *testing.T) {
	words := []string{
		"Convention booth", "Convention hall", "Convention panel", "Convention app", "Convention map",
		"Convention schedule", "Convention ticket", "Convention cosplay", "Convention stage", "Convention line",
	}
	low, reason := detectLowDiversity(words)
	if !low {
		t.Fatalf("expected low diversity, got false")
	}
	if reason == "" {
		t.Fatalf("expected low-diversity reason, got empty")
	}
}

func TestDetectLowDiversityVariedList(t *testing.T) {
	words := []string{
		"Cosplay armor prop", "Artist alley sketch", "Food truck lineup", "Live AMV screening", "Manga haul tote",
		"Panel room queue", "Convention badge lanyard", "Photo backdrop prop", "Voice actor autograph", "Arcade rhythm game",
	}
	low, _ := detectLowDiversity(words)
	if low {
		t.Fatalf("expected varied list to pass diversity check")
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
	if !strings.Contains(rec.Body.String(), "DeepSeek") {
		t.Errorf("expected DeepSeek error message, got: %s", rec.Body.String())
	}
}

// TestHandleGenerateBuzzwordsHostAuth verifies HTTP 403 for wrong host_id.
func TestHandleGenerateBuzzwordsHostAuth(t *testing.T) {
	ResetMetrics()
	s := NewServer([][]string{{"foo"}}, 5, 5, "9999")
	s.LLMClient = &stubLLMClient{healthy: true}

	room, _ := s.createRoom(context.Background(), "host-123")
	guestToken, err := s.TokenManager.IssueToken("guest-1", "203.0.113.1", 1)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	body := bytes.NewBufferString(`{"topic":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/room/"+room.Code+"/generate-buzzwords", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+guestToken)
	req.RemoteAddr = "203.0.113.1:12345"
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
	stub := &stubLLMClient{healthy: true}
	s.LLMClient = stub

	room, _ := s.createRoom(context.Background(), "host-42")
	hostToken, err := s.TokenManager.IssueToken(room.HostID, "203.0.113.7", 1)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	body := bytes.NewBufferString(`{"topic":"anime convention"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/room/"+room.Code+"/generate-buzzwords", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+hostToken)
	req.RemoteAddr = "203.0.113.7:54321"
	rec := httptest.NewRecorder()

	s.handleGenerateBuzzwords(rec, req, room.Code)

	if !stub.called {
		t.Error("expected LLMClient.StreamGenerate to be called")
	}
}

func TestHandleGenerateBuzzwordsMissingBearerToken(t *testing.T) {
	ResetMetrics()
	s := NewServer([][]string{{"foo"}}, 5, 5, "9999")
	s.LLMClient = &stubLLMClient{healthy: true}

	room, _ := s.createRoom(context.Background(), "host-1")
	body := bytes.NewBufferString(`{"topic":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/room/"+room.Code+"/generate-buzzwords", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleGenerateBuzzwords(rec, req, room.Code)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestHandleGenerateBuzzwordsLLMUnhealthy(t *testing.T) {
	ResetMetrics()
	s := NewServer([][]string{{"foo"}}, 5, 5, "9999")
	s.LLMClient = &stubLLMClient{healthy: false}

	room, _ := s.createRoom(context.Background(), "host-77")
	hostToken, err := s.TokenManager.IssueToken(room.HostID, "203.0.113.20", 1)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}
	body := bytes.NewBufferString(`{"topic":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/room/"+room.Code+"/generate-buzzwords", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+hostToken)
	req.RemoteAddr = "203.0.113.20:80"
	rec := httptest.NewRecorder()

	s.handleGenerateBuzzwords(rec, req, room.Code)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestHandleSubmitGameBuzzwordFeedbackAndUseInPrompt(t *testing.T) {
	ResetMetrics()
	s := NewServer([][]string{{"foo"}}, 5, 5, "9999")
	stub := &stubLLMClient{healthy: true}
	s.LLMClient = stub

	game := NewGame("game-1", s.Buzzwords, s.Rows, s.Cols)
	game.HostID = "host-42"
	s.GamesMu.Lock()
	s.Games[game.ID] = game
	s.CodeToGame[game.Code] = game
	s.GamesMu.Unlock()

	hostToken, err := s.TokenManager.IssueToken(game.HostID, "203.0.113.50", 1)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}

	feedbackBody := bytes.NewBufferString(`{
		"topic":"anime north",
		"set_label":"Set A",
		"total_words":50,
		"included_words":["Cosplay armor props","Dealer hall tote bag"],
		"excluded":[{"word":"Innovation","reason":"too_generic"}]
	}`)
	feedbackReq := httptest.NewRequest(http.MethodPost, "/api/game/"+game.Code+"/feedback", feedbackBody)
	feedbackReq.Header.Set("Content-Type", "application/json")
	feedbackReq.Header.Set("Authorization", "Bearer "+hostToken)
	feedbackReq.RemoteAddr = "203.0.113.50:1234"
	feedbackRec := httptest.NewRecorder()

	s.handleSubmitGameBuzzwordFeedback(feedbackRec, feedbackReq, game.Code)

	if feedbackRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from feedback submit, got %d: %s", feedbackRec.Code, feedbackRec.Body.String())
	}

	genBody := bytes.NewBufferString(`{"topic":"anime north"}`)
	genReq := httptest.NewRequest(http.MethodPost, "/api/game/"+game.Code+"/generate-buzzwords", genBody)
	genReq.Header.Set("Content-Type", "application/json")
	genReq.Header.Set("Authorization", "Bearer "+hostToken)
	genReq.RemoteAddr = "203.0.113.50:9876"
	genRec := httptest.NewRecorder()

	s.handleGenerateBuzzwordsForGame(genRec, genReq, game.Code)

	if !stub.called {
		t.Fatal("expected StreamGenerate to be called")
	}

	joined := ""
	for _, msg := range stub.messages {
		joined += msg.Content + "\n"
	}
	if !strings.Contains(joined, "User scoring guidance from prior rounds") {
		t.Fatalf("expected prompt to include scoring guidance, got: %s", joined)
	}
	if !strings.Contains(joined, "too_generic") {
		t.Fatalf("expected prompt guidance to include exclusion reason, got: %s", joined)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// stubLLMClient is a no-op LLMClient for testing.
type stubLLMClient struct {
	called   bool
	healthy  bool
	messages []ChatMessage
}

func (s *stubLLMClient) StreamGenerate(_ context.Context, messages []ChatMessage, _ GenerationOptions, _ http.ResponseWriter) error {
	s.called = true
	s.messages = append([]ChatMessage(nil), messages...)
	return nil
}

func (s *stubLLMClient) Healthy(_ context.Context) bool { return s.healthy }

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
