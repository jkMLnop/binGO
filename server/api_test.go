package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jkMLnop/binGO-CLI/db"
)

// TestAPIGameByCode tests the GET /api/game/:code endpoint
func TestAPIGameByCode(t *testing.T) {
	ResetMetrics() // Reset metrics before test

	// Create a test server with buzzwords
	testBuzzwords := [][]string{
		{"synergy", "moving the needle", "low-hanging fruit"},
		{"circle back", "touch base", "deep dive"},
		{"take offline", "at the end of the day", "move forward"},
	}

	server := NewServer(testBuzzwords, 3, 3, "9999")

	// Create a game manually
	server.createNewGame()

	// Get the game code
	server.GamesMu.RLock()
	var gameCode string
	for code := range server.CodeToGame {
		gameCode = code
		break
	}
	server.GamesMu.RUnlock()

	if gameCode == "" {
		t.Fatal("no game code found")
	}

	// Register handlers
	server.registerHandlers()

	// Test: GET /api/game/:code - Valid code
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/game/%s", gameCode), nil)
	w := httptest.NewRecorder()
	server.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response APIResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got false: %s", response.Error)
	}

	// Check game info
	gameData, ok := response.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected game data as map")
	}

	if code, ok := gameData["code"].(string); !ok || code != gameCode {
		t.Errorf("expected code %s, got %v", gameCode, gameData["code"])
	}

	t.Log("✓ GET /api/game/:code (valid) passed")

	// Test: GET /api/game/:code - Invalid code
	req = httptest.NewRequest("GET", "/api/game/INVALID", nil)
	w = httptest.NewRecorder()
	server.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}

	t.Log("✓ GET /api/game/:code (invalid) passed")
}

// TestAPIStatus tests the GET /api/status endpoint
func TestAPIStatus(t *testing.T) {
	ResetMetrics() // Reset metrics before test

	testBuzzwords := [][]string{
		{"synergy", "moving the needle", "low-hanging fruit"},
		{"circle back", "touch base", "deep dive"},
		{"take offline", "at the end of the day", "move forward"},
	}

	server := NewServer(testBuzzwords, 3, 3, "9999")
	server.registerHandlers()

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()
	server.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var response APIResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !response.Success {
		t.Errorf("expected success=true, got false: %s", response.Error)
	}

	statusData, ok := response.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected status data as map")
	}

	if status, ok := statusData["status"].(string); !ok || status != "running" {
		t.Errorf("expected status=running, got %v", statusData["status"])
	}

	llmTimeout, ok := statusData["llm_timeout_seconds"].(float64)
	if !ok {
		t.Fatalf("expected llm_timeout_seconds in status payload")
	}

	roomTTL, ok := statusData["room_code_ttl_seconds"].(float64)
	if !ok {
		t.Fatalf("expected room_code_ttl_seconds in status payload")
	}

	if llmTimeout != float64((4 * 60 * 60)) {
		t.Errorf("expected llm_timeout_seconds=14400, got %v", llmTimeout)
	}

	if roomTTL != llmTimeout+float64((2*60*60)) {
		t.Errorf("expected room_code_ttl_seconds to equal llm_timeout_seconds+7200, got room=%v llm=%v", roomTTL, llmTimeout)
	}

	t.Log("✓ GET /api/status passed")
}

// TestAPILeaderboardWithoutDB tests leaderboard endpoint without database
func TestAPILeaderboardWithoutDB(t *testing.T) {
	ResetMetrics() // Reset metrics before test

	testBuzzwords := [][]string{
		{"synergy", "moving the needle", "low-hanging fruit"},
		{"circle back", "touch base", "deep dive"},
		{"take offline", "at the end of the day", "move forward"},
	}

	server := NewServer(testBuzzwords, 3, 3, "9999")
	server.registerHandlers()

	req := httptest.NewRequest("GET", "/api/leaderboard", nil)
	w := httptest.NewRecorder()
	server.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 (no DB), got %d", w.Code)
	}

	t.Log("✓ GET /api/leaderboard (no DB) returns 503 passed")
}

// TestAPIMethodNotAllowed tests that only GET is allowed for API endpoints
func TestAPIMethodNotAllowed(t *testing.T) {
	ResetMetrics() // Reset metrics before test

	testBuzzwords := [][]string{
		{"synergy", "moving the needle", "low-hanging fruit"},
		{"circle back", "touch base", "deep dive"},
		{"take offline", "at the end of the day", "move forward"},
	}

	server := NewServer(testBuzzwords, 3, 3, "9999")
	server.registerHandlers()

	tests := []string{
		"/api/status",
		"/api/leaderboard",
		"/api/game/CODE",
	}

	for _, endpoint := range tests {
		req := httptest.NewRequest("POST", endpoint, nil)
		w := httptest.NewRecorder()
		server.Mux.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405 for %s, got %d", endpoint, w.Code)
		}
	}

	t.Log("✓ API method validation passed")
}

// ---------------------------------------------------------------------------
// Phase 9: /api/player/{username}/stats endpoint tests
// ---------------------------------------------------------------------------

func TestAPIPlayerStatsWithoutDB(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	srv.registerHandlers()

	req := httptest.NewRequest("GET", "/api/player/alice/stats", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 without DB, got %d", w.Code)
	}
	t.Log("✓ /api/player/stats returns 503 without DB")
}

func TestAPIPlayerStatsMissingUsername(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	srv.registerHandlers()

	// The path /api/player//stats will match the prefix handler but have empty segment
	req := httptest.NewRequest("GET", "/api/player//stats", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Errorf("expected non-200 for missing username, got %d", w.Code)
	}
	t.Log("✓ /api/player//stats returns error for missing username")
}

func TestAPILeaderboardSortParameter(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	srv.registerHandlers()

	// Without DB it should return 503 for all sort values
	sortValues := []string{"wins", "win_rate", "games_played", ""}
	for _, s := range sortValues {
		url := "/api/leaderboard"
		if s != "" {
			url += "?sort=" + s
		}
		req := httptest.NewRequest("GET", url, nil)
		w := httptest.NewRecorder()
		srv.Mux.ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("sort=%q: expected 503 without DB, got %d", s, w.Code)
		}
	}
	t.Log("✓ /api/leaderboard?sort= returns 503 without DB for all sort values")
}

func TestAPIRoomIncludesCustomBoardFlagDefaultFalse(t *testing.T) {
	ResetMetrics()
	tmpDir := t.TempDir()
	store, err := db.NewSQLiteStore(context.Background(), tmpDir+"/room-api.db")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	srv := NewServer(testBuzzwords(), 3, 3, "test-api")
	srv.SetDB(store)
	srv.registerHandlers()

	createReq := httptest.NewRequest("POST", "/api/rooms", bytes.NewBufferString(`{"host_id":"host-api-1"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	srv.Mux.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusOK {
		t.Fatalf("create room expected 200, got %d", createW.Code)
	}

	var createResp APIResponse
	if err := json.NewDecoder(createW.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	roomData, ok := createResp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected create response room data map")
	}
	roomCode, _ := roomData["code"].(string)
	if roomCode == "" {
		t.Fatalf("missing room code in create response")
	}

	getReq := httptest.NewRequest("GET", "/api/room/"+roomCode, nil)
	getW := httptest.NewRecorder()
	srv.Mux.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("get room expected 200, got %d", getW.Code)
	}

	var getResp APIResponse
	if err := json.NewDecoder(getW.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	getData, ok := getResp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected get response room data map")
	}
	if custom, ok := getData["custom_board_used"].(bool); !ok || custom {
		t.Fatalf("expected custom_board_used=false, got %v", getData["custom_board_used"])
	}
}

func TestAPIRoomIncludesCustomBoardFlagTrueAfterUpload(t *testing.T) {
	ResetMetrics()
	tmpDir := t.TempDir()
	store, err := db.NewSQLiteStore(context.Background(), tmpDir+"/room-api-custom.db")
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())
	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	srv := NewServer(testBuzzwords(), 3, 3, "test-api")
	srv.SetDB(store)
	srv.registerHandlers()

	createReq := httptest.NewRequest("POST", "/api/rooms", bytes.NewBufferString(`{"host_id":"host-api-2"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	srv.Mux.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusOK {
		t.Fatalf("create room expected 200, got %d", createW.Code)
	}

	var createResp APIResponse
	if err := json.NewDecoder(createW.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	roomData, ok := createResp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected create response room data map")
	}
	roomCode, _ := roomData["code"].(string)
	if roomCode == "" {
		t.Fatalf("missing room code in create response")
	}

	words := make([]string, 24)
	for i := range words {
		words[i] = fmt.Sprintf("word-%02d", i+1)
	}
	body := map[string]interface{}{"words": words, "uploaded_by": "host-api-2"}
	bodyBytes, _ := json.Marshal(body)
	uploadReq := httptest.NewRequest("POST", "/api/room/"+roomCode+"/buzzwords", bytes.NewBuffer(bodyBytes))
	uploadReq.Header.Set("Content-Type", "application/json")
	uploadW := httptest.NewRecorder()
	srv.Mux.ServeHTTP(uploadW, uploadReq)
	if uploadW.Code != http.StatusOK {
		t.Fatalf("upload buzzwords expected 200, got %d", uploadW.Code)
	}

	getReq := httptest.NewRequest("GET", "/api/room/"+roomCode, nil)
	getW := httptest.NewRecorder()
	srv.Mux.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("get room expected 200, got %d", getW.Code)
	}

	var getResp APIResponse
	if err := json.NewDecoder(getW.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	getData, ok := getResp.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected get response room data map")
	}
	if custom, ok := getData["custom_board_used"].(bool); !ok || !custom {
		t.Fatalf("expected custom_board_used=true, got %v", getData["custom_board_used"])
	}
}
