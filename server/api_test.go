package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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
