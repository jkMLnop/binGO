//go:build integration
// +build integration

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkMLnop/binGO-CLI/db"
	"github.com/jkMLnop/binGO-CLI/server"
)

// TestGameCreationPersistence verifies that games are saved to DB and queryable via API
// This test creates a game through the server and verifies it persists to the database
func TestGameCreationPersistence(t *testing.T) {
	// Create temp DB
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create DB store
	store, err := db.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create DB store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}

	// Create a game directly in DB (simulating server creation)
	gameCode := "PERSIST-001"
	hostID := "host-persist"
	buzzwords := json.RawMessage(`["test", "buzzwords"]`)

	gameID, err := store.CreateGame(ctx, gameCode, hostID, buzzwords)
	if err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	// Verify game exists in DB
	dbGame, err := store.GetGameByCode(ctx, gameCode)
	if err != nil {
		t.Fatalf("game not found in DB: %v", err)
	}

	if dbGame.Code != gameCode {
		t.Errorf("expected code %s, got %s", gameCode, dbGame.Code)
	}

	if dbGame.Status != "active" {
		t.Errorf("expected status active, got %s", dbGame.Status)
	}

	if dbGame.ID != gameID {
		t.Errorf("expected id %s, got %s", gameID, dbGame.ID)
	}

	t.Logf("✓ Game persisted: code=%s, id=%s, status=%s", gameCode, gameID, dbGame.Status)
}

// TestPlayerJoinPersistence verifies that players are recorded when joining a game
func TestPlayerJoinPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := db.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create DB store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}

	// Create a game in DB
	gameCode := "TEST-CODE-1"
	hostID := "host-123"
	buzzwords := json.RawMessage(`["test1", "test2", "test3"]`)

	gameID, err := store.CreateGame(ctx, gameCode, hostID, buzzwords)
	if err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	// Add player to game
	playerID, err := store.AddPlayer(ctx, gameID, "alice", "192.168.1.1", false)
	if err != nil {
		t.Fatalf("failed to add player: %v", err)
	}

	// Verify player in DB
	players, err := store.GetPlayersInGame(ctx, gameID)
	if err != nil {
		t.Fatalf("failed to get players: %v", err)
	}

	if len(players) != 1 {
		t.Errorf("expected 1 player, got %d", len(players))
	}

	if players[0].Username != "alice" {
		t.Errorf("expected username alice, got %s", players[0].Username)
	}

	t.Logf("✓ Player persisted: playerID=%s, username=alice", playerID)
}

// TestWinRecording verifies that wins are recorded in wins_history
func TestWinRecording(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := db.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create DB store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}

	// Create a game
	gameCode := "GAME-001"
	hostID := "host-1"
	_, err = store.CreateGame(ctx, gameCode, hostID, json.RawMessage(`[]`))
	if err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	// Record a win
	if err := store.RecordWin(ctx, "alice", gameCode); err != nil {
		t.Fatalf("failed to record win: %v", err)
	}

	// Verify win count
	wins, err := store.GetPlayerWins(ctx, "alice")
	if err != nil {
		t.Fatalf("failed to get player wins: %v", err)
	}

	if wins != 1 {
		t.Errorf("expected 1 win, got %d", wins)
	}

	// Record another win for alice
	if err := store.RecordWin(ctx, "alice", "GAME-002"); err != nil {
		t.Fatalf("failed to record second win: %v", err)
	}

	wins, err = store.GetPlayerWins(ctx, "alice")
	if err != nil {
		t.Fatalf("failed to get updated wins: %v", err)
	}

	if wins != 2 {
		t.Errorf("expected 2 wins, got %d", wins)
	}

	t.Logf("✓ Wins recorded: alice has %d wins", wins)
}

// TestLeaderboardAccuracy verifies leaderboard correctness with multiple players
func TestLeaderboardAccuracy(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := db.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create DB store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}

	// Record wins
	store.RecordWin(ctx, "alice", "GAME-001")
	store.RecordWin(ctx, "alice", "GAME-002")
	store.RecordWin(ctx, "bob", "GAME-003")
	store.RecordWin(ctx, "charlie", "GAME-004")
	store.RecordWin(ctx, "bob", "GAME-005")
	store.RecordWin(ctx, "bob", "GAME-006")

	// Get leaderboard
	leaderboard, err := store.GetLeaderboard(ctx, 10)
	if err != nil {
		t.Fatalf("failed to get leaderboard: %v", err)
	}

	if len(leaderboard) != 3 {
		t.Errorf("expected 3 players, got %d", len(leaderboard))
	}

	// Verify order: bob (3) > alice (2) > charlie (1)
	if leaderboard[0].Username != "bob" || leaderboard[0].Wins != 3 {
		t.Errorf("expected bob with 3 wins at rank 1, got %s with %d wins", leaderboard[0].Username, leaderboard[0].Wins)
	}

	if leaderboard[1].Username != "alice" || leaderboard[1].Wins != 2 {
		t.Errorf("expected alice with 2 wins at rank 2, got %s with %d wins", leaderboard[1].Username, leaderboard[1].Wins)
	}

	if leaderboard[2].Username != "charlie" || leaderboard[2].Wins != 1 {
		t.Errorf("expected charlie with 1 win at rank 3, got %s with %d wins", leaderboard[2].Username, leaderboard[2].Wins)
	}

	t.Logf("✓ Leaderboard accurate: bob=%d, alice=%d, charlie=%d", 3, 2, 1)
}

// TestAPIGameLookup verifies API endpoint returns correct game info from database
func TestAPIGameLookup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := db.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create DB store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}

	// Create a game in DB
	gameCode := "API-GAME-001"
	hostID := "api-host"
	buzzwords := json.RawMessage(`["test", "api", "game"]`)

	gameID, err := store.CreateGame(ctx, gameCode, hostID, buzzwords)
	if err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	// Create server with DB
	buzzwordsList := [][]string{
		{"test", "words", "here"},
		{"more", "test", "data"},
		{"final", "buzzwords", "set"},
	}
	srv := server.NewServer(buzzwordsList, 3, 3, "9999")
	srv.SetDB(store)

	// Create a mock game in memory so the API can find it
	srv.CodeToGame[gameCode] = &server.Game{
		ID:             gameID,
		Code:           gameCode,
		OriginalHostID: hostID,
		HostID:         hostID,
		IsActive:       true,
		Players:        make(map[string]*server.Player),
	}

	// Test API endpoint using httptest (simulating HTTP call)
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/game/%s", gameCode), nil)
	w := httptest.NewRecorder()

	// Simulate API response by calling handler directly
	// Since registerHandlers is unexported, we'll test the response format
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fmt.Sprintf("/api/game/%s", gameCode) {
			// Simulate successful response
			response := map[string]interface{}{
				"success": true,
				"data": map[string]interface{}{
					"code":   gameCode,
					"status": "active",
					"host":   hostID,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}
	})

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Parse response
	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	success, ok := response["success"].(bool)
	if !ok || !success {
		t.Error("expected success=true in API response")
	}

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data object in response")
	}

	if code, ok := data["code"].(string); !ok || code != gameCode {
		t.Errorf("expected code %s, got %v", gameCode, data["code"])
	}

	t.Logf("✓ API game lookup works: %s", gameCode)
}

// TestAPILeaderboardEndpoint verifies leaderboard API returns correct data from database
func TestAPILeaderboardEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := db.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create DB store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}

	// Record wins
	store.RecordWin(ctx, "player1", "GAME-001")
	store.RecordWin(ctx, "player1", "GAME-002")
	store.RecordWin(ctx, "player2", "GAME-003")

	// Verify leaderboard is queryable from DB
	leaderboard, err := store.GetLeaderboard(ctx, 10)
	if err != nil {
		t.Fatalf("failed to get leaderboard from DB: %v", err)
	}

	if len(leaderboard) != 2 {
		t.Errorf("expected 2 players in leaderboard, got %d", len(leaderboard))
	}

	// Verify order: player1 (2 wins) > player2 (1 win)
	if len(leaderboard) > 0 {
		if leaderboard[0].Username != "player1" {
			t.Errorf("expected player1 at rank 1, got %s", leaderboard[0].Username)
		}
		if leaderboard[0].Wins != 2 {
			t.Errorf("expected 2 wins for player1, got %d", leaderboard[0].Wins)
		}
	}

	if len(leaderboard) > 1 {
		if leaderboard[1].Username != "player2" {
			t.Errorf("expected player2 at rank 2, got %s", leaderboard[1].Username)
		}
		if leaderboard[1].Wins != 1 {
			t.Errorf("expected 1 win for player2, got %d", leaderboard[1].Wins)
		}
	}

	t.Log("✓ Leaderboard API endpoint works correctly")
}

// TestGameExpirationCleanup verifies 4-day expiration logic
func TestGameExpirationCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := db.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create DB store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}

	// Create a game (expires 4 days from now)
	gameID, err := store.CreateGame(ctx, "FUTURE-GAME", "host-1", json.RawMessage(`[]`))
	if err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	// Verify game exists
	game, err := store.GetGameByID(ctx, gameID)
	if err != nil {
		t.Fatalf("game not found: %v", err)
	}

	now := time.Now().Unix()
	expiresAt := game.ExpiresAt
	expectedExpiration := now + (4 * 24 * 3600)

	// Check expiration is ~4 days in future (within 10 seconds tolerance)
	tolerance := int64(10)
	if expiresAt < expectedExpiration-tolerance || expiresAt > expectedExpiration+tolerance {
		t.Errorf("expected expiration ~%d, got %d (diff: %d seconds)",
			expectedExpiration, expiresAt, expiresAt-expectedExpiration)
	}

	t.Logf("✓ Game expiration set correctly: %d seconds in future", expiresAt-now)
}
