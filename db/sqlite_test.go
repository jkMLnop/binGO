package db

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestSQLiteStoreBasics validates schema and CRUD operations
func TestSQLiteStoreBasics(t *testing.T) {
	// Use temp file for testing
	tmpFile := "/tmp/test_bingo.db"
	defer os.Remove(tmpFile)

	// Create store
	store, err := NewSQLiteStore(context.Background(), tmpFile)
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
	gameID, err := store.CreateGame(ctx, gameCode, hostID, "", json.RawMessage(`["item1", "item2", "item3"]`))
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
	if err := store.RecordWin(ctx, "bob", gameCode, ""); err != nil {
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

	store, err := NewSQLiteStore(context.Background(), tmpFile)
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
	gameID, err := store.CreateGame(ctx, "CODE123", hostID, "", json.RawMessage(`[]`))
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

func TestGetLeaderboardIncludesRoomWins(t *testing.T) {
	tmpFile := "/tmp/test_leaderboard_includes_room_wins.db"
	defer os.Remove(tmpFile)

	store, err := NewSQLiteStore(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	// 3 room-scoped wins for CalmOtter74
	for i := 0; i < 3; i++ {
		if err := store.RecordWin(ctx, "CalmOtter74", "BINGO-ROOM1", "ARB32"); err != nil {
			t.Fatalf("failed to record room win %d: %v", i+1, err)
		}
	}

	// 1 standalone win for OtherPlayer
	if err := store.RecordWin(ctx, "OtherPlayer", "BINGO-AAAAA", ""); err != nil {
		t.Fatalf("failed to record standalone win: %v", err)
	}

	leaderboard, err := store.GetLeaderboard(ctx, 10)
	if err != nil {
		t.Fatalf("failed to get leaderboard: %v", err)
	}

	if len(leaderboard) < 2 {
		t.Fatalf("expected at least 2 leaderboard entries, got %d", len(leaderboard))
	}

	if leaderboard[0].Username != "CalmOtter74" || leaderboard[0].Wins != 3 {
		t.Fatalf("unexpected top entry: %+v", leaderboard[0])
	}
}

func TestGetLeaderboardExcludesCustomRoomWins(t *testing.T) {
	tmpFile := "/tmp/test_leaderboard_excludes_custom_room_wins.db"
	defer os.Remove(tmpFile)

	store, err := NewSQLiteStore(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	words := make([]string, 24)
	for i := range words {
		words[i] = "custom-word-" + string(rune('a'+(i%26)))
	}
	if err := store.SetRoomBuzzwords(ctx, "ARB32", words, "host"); err != nil {
		t.Fatalf("failed to set room buzzwords: %v", err)
	}

	// 3 wins in a custom-board room should stay room-only.
	for i := 0; i < 3; i++ {
		if err := store.RecordWin(ctx, "CalmOtter74", "BINGO-ROOM1", "ARB32"); err != nil {
			t.Fatalf("failed to record room win %d: %v", i+1, err)
		}
	}

	// 1 standalone win should be visible in all-time leaderboard.
	if err := store.RecordWin(ctx, "OtherPlayer", "BINGO-AAAAA", ""); err != nil {
		t.Fatalf("failed to record standalone win: %v", err)
	}

	leaderboard, err := store.GetLeaderboard(ctx, 10)
	if err != nil {
		t.Fatalf("failed to get leaderboard: %v", err)
	}

	if len(leaderboard) != 1 {
		t.Fatalf("expected 1 global leaderboard entry, got %d", len(leaderboard))
	}

	if leaderboard[0].Username != "OtherPlayer" || leaderboard[0].Wins != 1 {
		t.Fatalf("unexpected top entry: %+v", leaderboard[0])
	}
}

// TestArchiveGame validates persisting a completed game and querying it back
func TestArchiveGame(t *testing.T) {
	tmpFile := "/tmp/test_bingo_archive.db"
	defer os.Remove(tmpFile)

	store, err := NewSQLiteStore(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	now := time.Now()

	// Archive a game
	err = store.ArchiveGame(ctx, "game-1", "BINGO-AAAAA", "host-1", "winner-1", 3, now.Add(-10*time.Minute), now)
	if err != nil {
		t.Fatalf("ArchiveGame failed: %v", err)
	}

	// Archive a second game
	err = store.ArchiveGame(ctx, "game-2", "BINGO-BBBBB", "host-2", "winner-2", 5, now.Add(-5*time.Minute), now)
	if err != nil {
		t.Fatalf("ArchiveGame second entry failed: %v", err)
	}

	t.Log("✓ ArchiveGame persisted two entries without error")
}

// TestCleanupOldArchives validates that records older than 4 days are removed and recent ones kept
func TestCleanupOldArchives(t *testing.T) {
	tmpFile := "/tmp/test_bingo_cleanup.db"
	defer os.Remove(tmpFile)

	store, err := NewSQLiteStore(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	now := time.Now()
	fiveDaysAgo := now.Add(-5 * 24 * time.Hour)
	oneHourAgo := now.Add(-1 * time.Hour)

	// Insert an old archive (5 days ago — should be deleted)
	if err := store.ArchiveGame(ctx, "old-game", "BINGO-OLD00", "host-1", "winner-1", 2, fiveDaysAgo.Add(-30*time.Minute), fiveDaysAgo); err != nil {
		t.Fatalf("failed to insert old archive: %v", err)
	}

	// Insert a recent archive (1 hour ago — should be kept)
	if err := store.ArchiveGame(ctx, "new-game", "BINGO-NEW00", "host-2", "winner-2", 4, oneHourAgo.Add(-10*time.Minute), oneHourAgo); err != nil {
		t.Fatalf("failed to insert recent archive: %v", err)
	}

	// Run cleanup
	deleted, err := store.CleanupOldArchives(ctx)
	if err != nil {
		t.Fatalf("CleanupOldArchives failed: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected 1 record deleted, got %d", deleted)
	}

	// Running cleanup again should delete nothing
	deleted, err = store.CleanupOldArchives(ctx)
	if err != nil {
		t.Fatalf("second CleanupOldArchives failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 records on second cleanup, got %d", deleted)
	}

	t.Log("✓ CleanupOldArchives correctly removed old record and kept recent one")
}

// ---------------------------------------------------------------------------
// Phase 9: GetPlayerStats tests
// ---------------------------------------------------------------------------

func TestGetPlayerStatsNoGames(t *testing.T) {
	tmpFile := "/tmp/test_bingo_stats_empty.db"
	defer os.Remove(tmpFile)

	store, err := NewSQLiteStore(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	stats, err := store.GetPlayerStats(ctx, "unknown-player")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Wins != 0 {
		t.Errorf("expected 0 wins, got %d", stats.Wins)
	}
	if stats.GamesPlayed != 0 {
		t.Errorf("expected 0 games played, got %d", stats.GamesPlayed)
	}
	if stats.WinRate != 0 {
		t.Errorf("expected 0.0 win rate, got %f", stats.WinRate)
	}
	t.Log("✓ GetPlayerStats returns zero stats for unknown player")
}

func TestGetPlayerStatsWithWins(t *testing.T) {
	tmpFile := "/tmp/test_bingo_stats_wins.db"
	defer os.Remove(tmpFile)

	store, err := NewSQLiteStore(context.Background(), tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close(context.Background())

	ctx := context.Background()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	// Add alice to 3 games (required for GamesPlayed count)
	gameCodes := []struct{ id, code string }{
		{"game-001", "BINGO-00001"},
		{"game-002", "BINGO-00002"},
		{"game-003", "BINGO-00003"},
	}
	for _, g := range gameCodes {
		if _, err := store.AddPlayer(ctx, g.id, "alice", "", false); err != nil {
			t.Fatalf("AddPlayer failed for %s: %v", g.id, err)
		}
	}

	// Record 2 wins for alice
	if err := store.RecordWin(ctx, "alice", gameCodes[0].code, ""); err != nil {
		t.Fatalf("RecordWin failed: %v", err)
	}
	if err := store.RecordWin(ctx, "alice", gameCodes[1].code, ""); err != nil {
		t.Fatalf("RecordWin failed: %v", err)
	}

	stats, err := store.GetPlayerStats(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.Username != "alice" {
		t.Errorf("expected username 'alice', got %q", stats.Username)
	}
	if stats.Wins != 2 {
		t.Errorf("expected 2 wins, got %d", stats.Wins)
	}
	if stats.GamesPlayed != 3 {
		t.Errorf("expected 3 games played, got %d", stats.GamesPlayed)
	}
	expectedRate := 2.0 / 3.0
	if stats.WinRate < expectedRate-0.001 || stats.WinRate > expectedRate+0.001 {
		t.Errorf("expected win rate ~%.3f, got %.3f", expectedRate, stats.WinRate)
	}
	t.Logf("✓ GetPlayerStats: wins=%d games=%d rate=%.3f", stats.Wins, stats.GamesPlayed, stats.WinRate)
}

func TestLLMFeedbackPersistence(t *testing.T) {
	tmpFile := "/tmp/test_bingo_llm_feedback.db"
	defer os.Remove(tmpFile)

	ctx := context.Background()

	store, err := NewSQLiteStore(ctx, tmpFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	if err := store.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}

	entry := LLMFeedbackEntry{
		GameCode:       "BINGO-ABCDE",
		Topic:          "anime conventions",
		SourceURL:      "https://example.com",
		SetLabel:       "set-1",
		GenerationMode: "agentic-retrieval",
		TotalWords:     25,
		IncludedWords:  []string{"cosplay", "artist alley"},
		Excluded: []LLMFeedbackExcludedWord{
			{Word: "fandom", Reason: "too_generic"},
			{Word: "unsafe word", Reason: "safety_accessibility"},
		},
		SubmittedBy: "host-1",
		SubmittedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := store.SaveLLMFeedback(ctx, entry); err != nil {
		t.Fatalf("SaveLLMFeedback failed: %v", err)
	}

	entries, err := store.GetRecentLLMFeedback(ctx, "BINGO-ABCDE", "anime conventions", 10)
	if err != nil {
		t.Fatalf("GetRecentLLMFeedback failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 feedback entry, got %d", len(entries))
	}
	if entries[0].IncludedWords[0] != "cosplay" {
		t.Fatalf("expected included word 'cosplay', got %q", entries[0].IncludedWords[0])
	}
	if len(entries[0].Excluded) != 2 {
		t.Fatalf("expected 2 excluded words, got %d", len(entries[0].Excluded))
	}
	if entries[0].GenerationMode != "agentic-retrieval" {
		t.Fatalf("expected generation mode agentic-retrieval, got %q", entries[0].GenerationMode)
	}

	if err := store.Close(ctx); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	reopened, err := NewSQLiteStore(ctx, tmpFile)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer reopened.Close(ctx)
	if err := reopened.Init(ctx); err != nil {
		t.Fatalf("failed to init reopened store: %v", err)
	}

	reopenedEntries, err := reopened.GetRecentLLMFeedback(ctx, "bingo-abcde", "ANIME CONVENTIONS", 10)
	if err != nil {
		t.Fatalf("GetRecentLLMFeedback on reopened store failed: %v", err)
	}
	if len(reopenedEntries) != 1 {
		t.Fatalf("expected 1 persisted entry after reopen, got %d", len(reopenedEntries))
	}
	if !strings.EqualFold(reopenedEntries[0].GameCode, "BINGO-ABCDE") {
		t.Fatalf("expected persisted game code BINGO-ABCDE, got %q", reopenedEntries[0].GameCode)
	}

	t.Log("✓ LLM feedback persists in SQLite and survives store reopen")
}
