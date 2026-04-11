package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminKeyMiddleware(t *testing.T) {
	// Create test server
	buzzwords := [][]string{
		{"test1", "test2", "test3"},
		{"test4", "test5", "test6"},
		{"test7", "test8", "test9"},
	}
	server := NewServer(buzzwords, 3, 3, "8080")

	tests := []struct {
		name       string
		adminKey   string
		expectCode int
		expectBody string
	}{
		{
			name:       "missing admin key header",
			adminKey:   "",
			expectCode: http.StatusUnauthorized,
			expectBody: "missing X-Admin-Key header",
		},
		{
			name:       "invalid admin key",
			adminKey:   "wrong-key",
			expectCode: http.StatusForbidden,
			expectBody: "invalid X-Admin-Key",
		},
		{
			name:       "valid admin key (default)",
			adminKey:   "dev-admin-key-local-only",
			expectCode: http.StatusOK,
			expectBody: "success",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/api/games", nil)
			if tt.adminKey != "" {
				req.Header.Set("X-Admin-Key", tt.adminKey)
			}
			w := httptest.NewRecorder()

			server.handleListGames(w, req)

			if w.Code != tt.expectCode {
				t.Errorf("got status %d, want %d", w.Code, tt.expectCode)
			}

			if !bytes.Contains(w.Body.Bytes(), []byte(tt.expectBody)) {
				t.Errorf("got body %s, expected to contain %s", w.Body.String(), tt.expectBody)
			}
		})
	}
}

// TestAdminKeyMiddlewareEnvVar validates that adminKeyMiddleware reads ADMIN_API_KEY
// from the environment rather than always falling back to the hardcoded default.
// This serves as the integration test for the production credentials setup.
func TestAdminKeyMiddlewareEnvVar(t *testing.T) {
	buzzwords := [][]string{
		{"test1", "test2", "test3"},
		{"test4", "test5", "test6"},
		{"test7", "test8", "test9"},
	}

	tests := []struct {
		name       string
		envKey     string // value to set in ADMIN_API_KEY env var ("" = not set)
		headerKey  string // value to send in X-Admin-Key header
		expectCode int
	}{
		{
			name:       "custom env key accepted",
			envKey:     "my-production-secret-key",
			headerKey:  "my-production-secret-key",
			expectCode: http.StatusOK,
		},
		{
			name:       "default key rejected when custom env key is set",
			envKey:     "my-production-secret-key",
			headerKey:  DefaultAdminKey,
			expectCode: http.StatusForbidden,
		},
		{
			name:       "default key works when env var is not set",
			envKey:     "", // unset — middleware falls back to DefaultAdminKey
			headerKey:  DefaultAdminKey,
			expectCode: http.StatusOK,
		},
		{
			name:       "wrong key rejected regardless of env var",
			envKey:     "my-production-secret-key",
			headerKey:  "completely-wrong-key",
			expectCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envKey != "" {
				t.Setenv(AdminKeyEnvVar, tt.envKey)
			} else {
				t.Setenv(AdminKeyEnvVar, "") // ensure clean state; t.Setenv restores on cleanup
			}

			server := NewServer(buzzwords, 3, 3, "8080")
			ResetMetrics()

			req := httptest.NewRequest(http.MethodGet, "/admin/api/games", nil)
			req.Header.Set("X-Admin-Key", tt.headerKey)
			w := httptest.NewRecorder()

			server.handleListGames(w, req)

			if w.Code != tt.expectCode {
				t.Errorf("got status %d, want %d (env=%q header=%q)",
					w.Code, tt.expectCode, tt.envKey, tt.headerKey)
			}
		})
	}
}

func TestCreateGame(t *testing.T) {
	buzzwords := [][]string{
		{"test1", "test2", "test3"},
		{"test4", "test5", "test6"},
		{"test7", "test8", "test9"},
	}
	server := NewServer(buzzwords, 3, 3, "8080")

	tests := []struct {
		name       string
		body       string
		adminKey   string
		expectCode int
	}{
		{
			name:       "create game with valid key",
			body:       `{"players": ["player1", "player2"]}`,
			adminKey:   "dev-admin-key-local-only",
			expectCode: http.StatusOK,
		},
		{
			name:       "create game without admin key",
			body:       `{"players": ["player1"]}`,
			adminKey:   "",
			expectCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/admin/api/games", bytes.NewBufferString(tt.body))
			if tt.adminKey != "" {
				req.Header.Set("X-Admin-Key", tt.adminKey)
			}
			w := httptest.NewRecorder()

			server.handleCreateGame(w, req)

			if w.Code != tt.expectCode {
				t.Errorf("got status %d, want %d", w.Code, tt.expectCode)
			}

			if tt.expectCode == http.StatusOK {
				var resp APIResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Errorf("failed to unmarshal response: %v", err)
				}
				if !resp.Success {
					t.Errorf("expected success=true, got %v", resp.Success)
				}

				// Verify game was created
				if len(server.Games) == 0 {
					t.Errorf("expected at least 1 game to be created")
				}
			}
		})
	}
}

func TestListGames(t *testing.T) {
	buzzwords := [][]string{
		{"test1", "test2", "test3"},
		{"test4", "test5", "test6"},
		{"test7", "test8", "test9"},
	}
	server := NewServer(buzzwords, 3, 3, "8080")

	// Create a few games
	for i := 0; i < 3; i++ {
		server.createNewGame()
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/api/games", nil)
	req.Header.Set("X-Admin-Key", "dev-admin-key-local-only")
	w := httptest.NewRecorder()

	server.handleListGames(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success=true")
	}

	// Check response data contains games
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Errorf("expected map data, got %T", resp.Data)
	}

	count, ok := data["count"].(float64)
	if !ok {
		t.Errorf("expected numeric count")
	}
	if count != 3 { // 3 created
		t.Errorf("got %d games, want 3", int(count))
	}
}

func TestGetGameDetail(t *testing.T) {
	buzzwords := [][]string{
		{"test1", "test2", "test3"},
		{"test4", "test5", "test6"},
		{"test7", "test8", "test9"},
	}
	server := NewServer(buzzwords, 3, 3, "8080")
	server.createNewGame()

	// Get a game ID
	server.GamesMu.RLock()
	var gameID string
	for id := range server.Games {
		gameID = id
		break
	}
	server.GamesMu.RUnlock()

	if gameID == "" {
		t.Fatal("no game created")
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/api/games/%s", gameID), nil)
	req.Header.Set("X-Admin-Key", "dev-admin-key-local-only")
	w := httptest.NewRecorder()

	server.handleGetGameDetail(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	var resp APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to unmarshal response: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success=true")
	}

	// Check game detail structure
	detail, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Errorf("expected map data, got %T", resp.Data)
	}

	if detail["id"] == nil {
		t.Errorf("expected game id in response")
	}
	if detail["code"] == nil {
		t.Errorf("expected game code in response")
	}
}

func TestDeleteGame(t *testing.T) {
	buzzwords := [][]string{
		{"test1", "test2", "test3"},
		{"test4", "test5", "test6"},
		{"test7", "test8", "test9"},
	}
	server := NewServer(buzzwords, 3, 3, "8080")
	server.createNewGame()

	// Get a game ID
	server.GamesMu.RLock()
	var gameID string
	for id := range server.Games {
		gameID = id
		break
	}
	server.GamesMu.RUnlock()

	if gameID == "" {
		t.Fatal("no game created")
	}

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/api/games/%s", gameID), nil)
	req.Header.Set("X-Admin-Key", "dev-admin-key-local-only")
	w := httptest.NewRecorder()

	server.handleDeleteGame(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", w.Code)
	}

	// Verify game is no longer active
	server.GamesMu.RLock()
	game := server.Games[gameID]
	server.GamesMu.RUnlock()

	if game.IsActive {
		t.Errorf("expected game to be inactive after delete")
	}
}

func TestAdminGameRouter(t *testing.T) {
	buzzwords := [][]string{
		{"test1", "test2", "test3"},
		{"test4", "test5", "test6"},
		{"test7", "test8", "test9"},
	}
	server := NewServer(buzzwords, 3, 3, "8080")

	tests := []struct {
		name       string
		method     string
		path       string
		expectCode int
	}{
		{
			name:       "POST to /admin/api/games (create)",
			method:     http.MethodPost,
			path:       "/admin/api/games",
			expectCode: http.StatusOK,
		},
		{
			name:       "GET to /admin/api/games (list)",
			method:     http.MethodGet,
			path:       "/admin/api/games",
			expectCode: http.StatusOK,
		},
		{
			name:       "PUT to /admin/api/games (not allowed)",
			method:     http.MethodPut,
			path:       "/admin/api/games",
			expectCode: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString("{}"))
			req.Header.Set("X-Admin-Key", "dev-admin-key-local-only")
			w := httptest.NewRecorder()

			server.handleAdminGames(w, req)

			if w.Code != tt.expectCode {
				t.Errorf("got status %d, want %d", w.Code, tt.expectCode)
			}
		})
	}
}
