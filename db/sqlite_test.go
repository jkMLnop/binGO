package db

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestSQLiteStoreBasics validates schema and CRUD operations
func TestSQLiteStoreBasics(t *testing.T) {
	// Use temp file for testing
	tmpFile := "/tmp/test_bingo.db"
	defer os.Remove(tmpFile)

	// Create store
	store, err := NewSQLiteStore(tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()

	// Initialize schema
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	// Test host creation
	hostID := "host-123"
	username := "alice"
	buzzwords := json.RawMessage(`["synergy", "move the needle"]`)

	if err := store.CreateOrUpdateHost(ctx, hostID, username, buzzwords); err != nil {
		t.Fatalf("failed to create host: %v", err)
	}

	// Test host retrieval
	host, err := store.GetHost(ctx, hostID)
	if err != nil {
		t.Fatalf("failed to get host: %v", err)
	}
	if host.Username != username {
		t.Errorf("expected username %q, got %q", username, host.Username)
	}

	// Test game creation
	gameCode := "ABC123"
	gameID, err := store.CreateGame(ctx, gameCode, hostID, json.RawMessage(`["item1", "item2", "item3"]`))
	if err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	// Test game retrieval
	game, err := store.GetGameByCode(ctx, gameCode)
	if err != nil {
		t.Fatalf("failed to get game: %v", err)
	}
	if game.ID != gameID {
		t.Errorf("expected game ID %q, got %q", gameID, game.ID)
	}
	if game.Status != "active" {
		t.Errorf("expected status 'active', got %q", game.Status)
	}

	// Test player add
	playerID, err := store.AddPlayer(ctx, gameID, "bob", "192.168.1.1", false)
	if err != nil {
		t.Fatalf("failed to add player: %v", err)
	}
	_ = playerID // playerID is added for future use in tests

	// Test players retrieval
	players, err := store.GetPlayersInGame(ctx, gameID)
	if err != nil {
		t.Fatalf("failed to get players: %v", err)
	}
	if len(players) != 1 {
		t.Errorf("expected 1 player, got %d", len(players))
	}

	// Test game update
	if err := store.UpdateGameStatus(ctx, gameID, "ended"); err != nil {
		t.Fatalf("failed to update game status: %v", err)
	}

	game, err = store.GetGameByID(ctx, gameID)
	if err != nil {
		t.Fatalf("failed to get game: %v", err)
	}
	if game.Status != "ended" {
		t.Errorf("expected status 'ended', got %q", game.Status)
	}

	// Test win recording
	if err := store.RecordWin(ctx, "bob", gameCode); err != nil {
		t.Fatalf("failed to record win: %v", err)
	}

	// Test leaderboard
	leaderboard, err := store.GetLeaderboard(ctx, 10)
	if err != nil {
		t.Fatalf("failed to get leaderboard: %v", err)
	}
	if len(leaderboard) != 1 || leaderboard[0].Username != "bob" || leaderboard[0].Wins != 1 {
		t.Errorf("unexpected leaderboard: %v", leaderboard)
	}

	t.Log("✓ All basic store operations passed")
}

// TestGameExpiration validates 4-day expiration logic
func TestGameExpiration(t *testing.T) {
	tmpFile := "/tmp/test_expiration.db"
	defer os.Remove(tmpFile)

	store, err := NewSQLiteStore(tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	// Create a host
	hostID := "host-123"
	if err := store.CreateOrUpdateHost(ctx, hostID, "alice", json.RawMessage(`[]`)); err != nil {
		t.Fatalf("failed to create host: %v", err)
	}

	// Create a game
	gameID, err := store.CreateGame(ctx, "CODE123", hostID, json.RawMessage(`[]`))
	if err != nil {
		t.Fatalf("failed to create game: %v", err)
	}

	game, err := store.GetGameByID(ctx, gameID)
	if err != nil {
		t.Fatalf("failed to get game: %v", err)
	}

	// Check expiration is 4 days in future
	now := time.Now().Unix()
	expectedExpiration := now + (4 * 24 * 3600)
	tolerance := 5 // 5 second tolerance
	if game.ExpiresAt < expectedExpiration-int64(tolerance) || game.ExpiresAt > expectedExpiration+int64(tolerance) {
		t.Errorf("expected expiration ~%d, got %d (diff: %d seconds)",
			expectedExpiration, game.ExpiresAt, game.ExpiresAt-expectedExpiration)
	}

	t.Log("✓ Game expiration validation passed")
}
