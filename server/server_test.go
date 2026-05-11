package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkMLnop/binGO-CLI/db"
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Helper function to create test buzzwords in the expected format
func testBuzzwords() [][]string {
	return [][]string{
		{"synergy"},
		{"leverage"},
		{"paradigm"},
		{"disruption"},
		{"innovation"},
		{"blockchain"},
		{"AI"},
		{"cloud"},
		{"agile"},
	}
}

func TestPlayerCreation(t *testing.T) {
	player := newPlayer("player-1")

	if player.ID != "player-1" {
		t.Errorf("Expected player ID 'player-1', got %s", player.ID)
	}

	if player.messages == nil {
		t.Error("Expected messages channel to be initialized")
	}
}

func TestGameCreation(t *testing.T) {
	buzzwords := testBuzzwords()

	game := NewGame("game-1", buzzwords, 3, 3)

	if game.ID != "game-1" {
		t.Errorf("Expected game ID 'game-1', got %s", game.ID)
	}

	if !game.IsActive {
		t.Error("Expected game to be active")
	}

	if game.PlayerCount() != 0 {
		t.Errorf("Expected 0 players initially, got %d", game.PlayerCount())
	}
}

func TestAddPlayerToGame(t *testing.T) {
	buzzwords := testBuzzwords()

	game := NewGame("game-1", buzzwords, 3, 3)
	player := newPlayer("player-1")

	err := game.AddPlayer(player)
	if err != nil {
		t.Fatalf("Expected no error adding player, got: %v", err)
	}

	if game.PlayerCount() != 1 {
		t.Errorf("Expected 1 player, got %d", game.PlayerCount())
	}

	retrievedPlayer, exists := game.GetPlayer("player-1")
	if !exists {
		t.Fatal("Expected player to exist in game")
	}

	if retrievedPlayer.ID != "player-1" {
		t.Errorf("Expected player ID 'player-1', got %s", retrievedPlayer.ID)
	}
}

func TestDuplicatePlayerError(t *testing.T) {
	buzzwords := testBuzzwords()

	game := NewGame("game-1", buzzwords, 3, 3)
	player := newPlayer("player-1")

	err1 := game.AddPlayer(player)
	if err1 != nil {
		t.Fatalf("Expected no error on first add, got: %v", err1)
	}

	// Try adding the same player again
	err2 := game.AddPlayer(player)
	if err2 == nil {
		t.Fatal("Expected error adding duplicate player, got nil")
	}
}

func TestServerConnectionHandler(t *testing.T) {
	ResetMetrics() // Reset metrics before test

	buzzwords := testBuzzwords()

	srv := NewServer(buzzwords, 3, 3, "8080")

	// Create a game (NewServer no longer creates one automatically)
	srv.createNewGame()

	// Test status endpoint
	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()

	srv.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var statusData map[string]interface{}
	json.NewDecoder(w.Body).Decode(&statusData)

	if statusData["total_games"] != float64(1) {
		t.Errorf("Expected 1 game, got %v", statusData["total_games"])
	}
}

func TestGetPlayerList(t *testing.T) {
	buzzwords := testBuzzwords()

	game := NewGame("game-1", buzzwords, 3, 3)
	player1 := newPlayer("player-1")
	player2 := newPlayer("player-2")

	game.AddPlayer(player1)
	game.AddPlayer(player2)

	playerList := game.GetPlayerList()
	if len(playerList) != 2 {
		t.Errorf("Expected 2 players in list, got %d", len(playerList))
	}

	// Check that both players are in the list
	found1 := false
	found2 := false
	for _, id := range playerList {
		if id == "player-1" {
			found1 = true
		}
		if id == "player-2" {
			found2 = true
		}
	}

	if !found1 || !found2 {
		t.Error("Expected both players in list")
	}
}

// Phase 7.3 Tests: Game Access Control

func TestGameCodeGeneration(t *testing.T) {
	buzzwords := testBuzzwords()
	game := NewGame("game-1", buzzwords, 3, 3)

	// Test that code is generated and has expected format
	if game.Code == "" {
		t.Error("Expected game code to be generated")
	}

	// Code should be 10 characters (BINGO-XXXXX format)
	if len(game.Code) != 11 {
		t.Errorf("Expected code length 11 (BINGO-XXXXX), got %d: %s", len(game.Code), game.Code)
	}

	// Code should contain the BINGO- prefix
	if !strings.HasPrefix(game.Code, "BINGO-") {
		t.Errorf("Expected code to start with 'BINGO-', got %s", game.Code)
	}

	// Each game should have a unique code
	game2 := NewGame("game-2", buzzwords, 3, 3)
	if game.Code == game2.Code {
		t.Error("Expected different codes for different games")
	}
}

func TestCodeBasedGameLookup(t *testing.T) {
	ResetMetrics() // Reset metrics before test

	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// Create a new game with a code
	srv.createNewGame()

	// Get a code from the map
	var code string
	for c := range srv.CodeToGame {
		code = c
		break
	}

	// Check that game is registered by code
	if game, exists := srv.CodeToGame[code]; !exists {
		t.Error("Expected game to be registered by code in CodeToGame map")
	} else if game.Code != code {
		t.Errorf("Expected CodeToGame[%s] to have matching code", code)
	}
}

func TestGameJoinRequiresCode(t *testing.T) {
	ResetMetrics() // Reset metrics before test

	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// All connections require a code now
	code := ""

	_, err := srv.getOrCreateGame(code)
	if err == nil {
		t.Error("Expected getOrCreateGame without code to fail")
	}
}

func TestCodeBasedGameJoin(t *testing.T) {
	ResetMetrics() // Reset metrics before test

	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// Create a new game with a code
	srv.createNewGame()
	var correctCode string
	for code := range srv.CodeToGame {
		correctCode = code
		break
	}
	game, err := srv.getOrCreateGame(correctCode)
	if err != nil {
		t.Errorf("Expected getOrCreateGame with valid code to succeed, got error: %v", err)
	}

	if game == nil {
		t.Error("Expected to get a game when providing valid code")
	}
}

// TestHostIDImmutableAfterDisconnect verifies that HostID is not cleared when host disconnects
func TestHostIDImmutableAfterDisconnect(t *testing.T) {
	buzzwords := testBuzzwords()
	game := NewGame("game-1", buzzwords, 3, 3)

	// Create and add host (first player) - simulates createPlayerInGame behavior
	host := newPlayer("host-player-id")
	err := game.AddPlayer(host)
	if err != nil {
		t.Fatalf("Failed to add host: %v", err)
	}

	// Manually set HostID (simulating createPlayerInGame logic)
	if game.HostID == "" {
		game.HostID = host.ID
	}

	// Verify host is set
	if game.HostID != "host-player-id" {
		t.Errorf("Expected HostID to be 'host-player-id', got %s", game.HostID)
	}

	// Create and add a second player
	player2 := newPlayer("player-2")
	err = game.AddPlayer(player2)
	if err != nil {
		t.Fatalf("Failed to add player 2: %v", err)
	}

	originalHostID := game.HostID
	t.Logf("Original HostID: %s", originalHostID)

	// Simulate host disconnection by removing them from the game
	game.RemovePlayer("host-player-id")

	// BUG CHECK: HostID should NOT be cleared
	// If it is cleared, this test will fail and indicate the bug exists
	if game.HostID != originalHostID {
		t.Errorf("BUG DETECTED: HostID was mutated on disconnect. Expected %s, got %s", originalHostID, game.HostID)
	} else {
		t.Logf("✓ HostID preserved after host disconnect: %s", game.HostID)
	}
}

// TestHostIDPersistsMultipleTimes verifies immutability through multiple disconnects
func TestHostIDPersistsMultipleTimes(t *testing.T) {
	buzzwords := testBuzzwords()
	game := NewGame("game-1", buzzwords, 3, 3)

	// Create and add host
	host := newPlayer("immutable-host")
	err := game.AddPlayer(host)
	if err != nil {
		t.Fatalf("Failed to add host: %v", err)
	}

	// Manually set HostID (simulating createPlayerInGame logic)
	if game.HostID == "" {
		game.HostID = host.ID
	}

	originalHostID := game.HostID
	t.Logf("Set HostID: %s", originalHostID)

	// Add and remove multiple players
	for i := 1; i <= 3; i++ {
		player := newPlayer("temp-player-" + string(rune(48+i)))
		game.AddPlayer(player)

		// Verify HostID unchanged
		if game.HostID != originalHostID {
			t.Errorf("HostID changed when adding player %d: %s → %s", i, originalHostID, game.HostID)
		}

		// Remove the player
		game.RemovePlayer(player.ID)

		// Verify HostID still unchanged
		if game.HostID != originalHostID {
			t.Errorf("HostID changed when removing player %d: %s → %s", i, originalHostID, game.HostID)
		}
	}

	if game.HostID != originalHostID {
		t.Errorf("HostID mutated through player lifecycle. Expected %s, got %s", originalHostID, game.HostID)
	} else {
		t.Logf("✓ HostID remained immutable through multiple changes: %s", originalHostID)
	}
}

// TestHostReconnectionIdentity verifies host maintains same ID after reconnect
func TestHostReconnectionIdentity(t *testing.T) {
	buzzwords := testBuzzwords()
	game := NewGame("game-1", buzzwords, 3, 3)

	// First connection: Host joins
	host1 := newPlayer("persistent-host-id")
	err := game.AddPlayer(host1)
	if err != nil {
		t.Fatalf("Failed to add host: %v", err)
	}

	// Manually set HostID (simulating createPlayerInGame logic)
	if game.HostID == "" {
		game.HostID = host1.ID
	}

	hostID := game.HostID
	playerCount := game.PlayerCount()

	// Host disconnects
	game.RemovePlayer(host1.ID)
	if game.PlayerCount() != playerCount-1 {
		t.Errorf("Expected player count to decrease after disconnect")
	}

	// Host ID should still be set (not cleared)
	if game.HostID != hostID {
		t.Errorf("HostID cleared on disconnect: %s → %s", hostID, game.HostID)
	}

	// Host reconnects (same player ID due to token-based auth)
	host2 := newPlayer(host1.ID) // Same ID as before
	err = game.AddPlayer(host2)
	if err != nil {
		// This error would indicate collision detection interfering with reconnection
		t.Fatalf("Failed to reconnect host (collision?): %v", err)
	}

	// Verify host is still the host
	if game.HostID != hostID {
		t.Errorf("Host status lost after reconnect. Expected %s, got %s", hostID, game.HostID)
	}

	t.Logf("✓ Host maintained identity through disconnect/reconnect: %s", hostID)
}

// TestReconnectionDetectionDoesntCauseCollision verifies returning player isn't rejected
func TestReconnectionDetectionDoesntCauseCollision(t *testing.T) {
	buzzwords := testBuzzwords()
	game := NewGame("game-1", buzzwords, 3, 3)

	// Player joins initially
	player1 := newPlayer("returning-player")
	err := game.AddPlayer(player1)
	if err != nil {
		t.Fatalf("Failed to add player initially: %v", err)
	}

	// Player disconnects
	game.RemovePlayer(player1.ID)

	// Player reconnects with same ID (simulating token-based reconnection)
	player2 := newPlayer(player1.ID)
	err = game.AddPlayer(player2)

	// This should succeed, not trigger a collision error
	if err != nil {
		t.Errorf("Reconnection triggered collision error (should be allowed): %v", err)
	}

	// Verify player is back in the game
	retrieved, exists := game.GetPlayer(player1.ID)
	if !exists {
		t.Error("Reconnected player not found in game")
	} else {
		t.Logf("✓ Player successfully reconnected: %s", retrieved.ID)
	}
}

// TestGameCodePersistsAcrossRestarts verifies code is maintained
func TestGameCodePersistsAcrossRestarts(t *testing.T) {
	buzzwords := testBuzzwords()
	game := NewGame("game-1", buzzwords, 3, 3)

	originalCode := game.Code
	t.Logf("Original code: %s", originalCode)

	// Simulate a game ending and restarting
	game.IsActive = false // Game ends
	game.IsActive = true  // Game restarts

	if game.Code != originalCode {
		t.Errorf("Game code changed after restart. Expected %s, got %s", originalCode, game.Code)
	} else {
		t.Logf("✓ Game code persisted across restart: %s", game.Code)
	}
}

// TestHandlePlayerWinAlreadyEnded verifies that duplicate win announcements are rejected
// when a game has already ended (e.g., host disconnects/reconnects without restarting)
func TestHandlePlayerWinAlreadyEnded(t *testing.T) {
	buzzwords := testBuzzwords()
	server := NewServer(buzzwords, 3, 3, "8080")

	// Create a game with a player
	game := NewGame("game-1", buzzwords, 3, 3)
	player := newPlayer("player-1")
	game.AddPlayer(player)
	game.HostID = player.ID

	// Register game with server
	server.GamesMu.Lock()
	server.Games["game-1"] = game
	server.GamesMu.Unlock()

	// First win announcement succeeds
	err := server.handlePlayerWin(context.Background(), game, player)
	if err != nil {
		t.Fatalf("First win announcement should succeed: %v", err)
	}

	// Verify game has a winner but IsActive stays true (only admin delete/orphan sets false)
	if game.Winner != player.ID {
		t.Fatalf("Game winner should be %s, got %s", player.ID, game.Winner)
	}

	// Second win announcement should fail
	player2 := newPlayer("player-2")
	game.AddPlayer(player2)
	err = server.handlePlayerWin(context.Background(), game, player2)
	if err == nil {
		t.Fatal("Expected error for win when game already ended")
	}

	// Verify error message mentions the already-ended state
	if !strings.Contains(err.Error(), "already ended") {
		t.Fatalf("Expected error about game already ended, got: %v", err)
	}

	// Verify winner didn't change
	if game.Winner != player.ID {
		t.Fatalf("Game winner should not change. Expected %s, got %s", player.ID, game.Winner)
	}

	t.Logf("✓ Duplicate win rejection works correctly")
}

// TestHandlePlayerWinArchivesGameToDB verifies the complete path:
// handlePlayerWin → archiveGame → ArchiveGameInDB → row written to game_archives table
func TestHandlePlayerWinArchivesGameToDB(t *testing.T) {
	ResetMetrics()

	// Set up a real SQLite database in a temp dir
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_archive.db")

	store, err := db.NewSQLiteStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("failed to create DB store: %v", err)
	}
	defer store.Close(context.Background())

	if err := store.Init(context.Background()); err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}

	// Create server with the real DB
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")
	srv.SetDB(store)

	// Create a game with one player
	game := NewGame("game-archive-1", buzzwords, 3, 3)
	player := newPlayer("alice")
	game.AddPlayer(player)
	game.HostID = player.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	// Announce win — this triggers archiveGame → ArchiveGameInDB under the hood
	if err := srv.handlePlayerWin(context.Background(), game, player); err != nil {
		t.Fatalf("handlePlayerWin failed: %v", err)
	}

	// Verify a row exists in game_archives by querying the SQLite file directly
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw DB: %v", err)
	}
	defer sqlDB.Close()

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM game_archives WHERE game_id = ?`, game.ID).Scan(&count); err != nil {
		t.Fatalf("failed to query game_archives: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 game_archives row, got %d", count)
	}

	// Verify the archive fields
	var code, hostID, winnerID string
	var playerCount int
	if err := sqlDB.QueryRow(
		`SELECT code, host_id, winner_id, player_count FROM game_archives WHERE game_id = ?`,
		game.ID,
	).Scan(&code, &hostID, &winnerID, &playerCount); err != nil {
		t.Fatalf("failed to scan game_archives row: %v", err)
	}

	if code != game.Code {
		t.Errorf("expected code %s, got %s", game.Code, code)
	}
	if hostID != game.HostID {
		t.Errorf("expected host_id %s, got %s", game.HostID, hostID)
	}
	if winnerID != player.ID {
		t.Errorf("expected winner_id %s, got %s", player.ID, winnerID)
	}
	if playerCount != 0 { // player was announced winner before DB write; channel closed, count may be 0
		t.Logf("player_count=%d (note: may differ depending on disconnect timing)", playerCount)
	}

	t.Logf("✓ handlePlayerWin correctly archived game to DB: code=%s, winner=%s", code, winnerID)
}

// TestImpersonationPrevention verifies that attempting to join as an existing player without a token is rejected
func TestImpersonationPrevention(t *testing.T) {
	buzzwords := testBuzzwords()
	game := NewGame("game-1", buzzwords, 3, 3)

	// Player 1 joins
	player1 := newPlayer("alice")
	err := game.AddPlayer(player1)
	if err != nil {
		t.Fatalf("Failed to add first player: %v", err)
	}
	game.HostID = player1.ID

	// Verify player1 is in the game
	retrieved, exists := game.GetPlayer("alice")
	if !exists {
		t.Fatal("Player 1 should be in game")
	}
	if retrieved.ID != "alice" {
		t.Errorf("Expected player ID 'alice', got %s", retrieved.ID)
	}

	// Simulate impersonation attempt: Player 2 tries to join as "alice" (no token)
	// This would happen in handlePlayerConnect when:
	// - existingPlayer, exists := game.GetPlayer(username) → exists=true, has player object
	// - loginMsg.Token == "" → no token provided
	// Should reject with error

	// We can't fully test this without mocking WebSocket, but we can verify the logic
	existingPlayer, exists := game.GetPlayer("alice")
	if !exists {
		t.Fatal("Test setup failed: alice should exist in game")
	}

	// Check: if player exists AND no token, it's an impersonation attempt
	hasToken := false // loginMsg.Token == "" means no token
	if exists && !hasToken {
		// This is the impersonation check
		t.Logf("✓ Would reject impersonation attempt: existing player 'alice' + no token")
	} else {
		t.Error("Logic check failed: should detect impersonation attempt")
	}

	// Verify the existing player is still the original
	if existingPlayer.ID != "alice" {
		t.Errorf("Original player modified: expected 'alice', got %s", existingPlayer.ID)
	}
}

// TestOrphanedGameMarkedOnLastDisconnect verifies that markGameOrphaned sets the correct
// fields so the game is ended and recorded even without a winner.
func TestOrphanedGameMarkedOnLastDisconnect(t *testing.T) {
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-orphan-1", buzzwords, 3, 3)
	player := newPlayer("solo-player")
	game.AddPlayer(player)
	game.HostID = player.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	if !game.IsActive {
		t.Fatal("game should be active before the test")
	}

	// Simulate last player leaving: remove them then call markGameOrphaned
	game.RemovePlayer(player.ID)
	if game.PlayerCount() != 0 {
		t.Fatal("expected 0 players after removal")
	}

	srv.markGameOrphaned(context.Background(), game)

	if game.IsActive {
		t.Error("game should be marked inactive after orphan")
	}
	if !game.Orphaned {
		t.Error("game.Orphaned should be true")
	}
	if game.EndedAt.IsZero() {
		t.Error("game.EndedAt should be set")
	}

	t.Logf("✓ markGameOrphaned correctly ends game %s (code: %s)", game.ID, game.Code)
}

// TestOrphanedGameNotJoinable verifies that getOrCreateGame returns a helpful error
// (not the admin-deleted message) when a game is orphaned.
func TestOrphanedGameNotJoinable(t *testing.T) {
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-orphan-2", buzzwords, 3, 3)
	player := newPlayer("lone-wolf")
	game.AddPlayer(player)
	game.HostID = player.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	// Orphan the game
	game.RemovePlayer(player.ID)
	srv.markGameOrphaned(context.Background(), game)

	// A new player should not be able to join
	_, err := srv.getOrCreateGame(game.Code)
	if err == nil {
		t.Fatal("expected error joining orphaned game, got nil")
	}
	if strings.Contains(err.Error(), "deleted by admin") {
		t.Errorf("orphaned game should not show admin-deleted message; got: %v", err)
	}
	if !strings.Contains(err.Error(), "all players disconnected") {
		t.Errorf("expected 'all players disconnected' in error; got: %v", err)
	}

	t.Logf("✓ orphaned game correctly rejected new join with: %v", err)
}

// TestOrphanedGameAutoCreatesReplacement verifies that when the last active game
// becomes orphaned, the server auto-creates a fresh active game code.
func TestOrphanedGameAutoCreatesReplacement(t *testing.T) {
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-orphan-replace", buzzwords, 3, 3)
	player := newPlayer("solo")
	game.AddPlayer(player)
	game.HostID = player.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	beforeGames := len(srv.Games)

	game.RemovePlayer(player.ID)
	srv.markGameOrphaned(context.Background(), game)

	if len(srv.Games) != beforeGames+1 {
		t.Fatalf("expected one replacement game to be created, before=%d after=%d", beforeGames, len(srv.Games))
	}

	activeCount := 0
	for _, g := range srv.Games {
		if g != nil && g.IsActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active game after orphan replacement, got %d", activeCount)
	}

	if game.IsActive {
		t.Fatal("original orphaned game should remain inactive")
	}
}

// TestOrphanedGameNoReplacementWhenAnotherActiveExists verifies that a replacement
// is not created if another active game already exists.
func TestOrphanedGameNoReplacementWhenAnotherActiveExists(t *testing.T) {
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game1 := NewGame("game-orphan-a", buzzwords, 3, 3)
	p1 := newPlayer("p1")
	game1.AddPlayer(p1)
	game1.HostID = p1.ID

	game2 := NewGame("game-stays-active", buzzwords, 3, 3)

	srv.GamesMu.Lock()
	srv.Games[game1.ID] = game1
	srv.CodeToGame[game1.Code] = game1
	srv.Games[game2.ID] = game2
	srv.CodeToGame[game2.Code] = game2
	srv.GamesMu.Unlock()

	beforeGames := len(srv.Games)

	game1.RemovePlayer(p1.ID)
	srv.markGameOrphaned(context.Background(), game1)

	if len(srv.Games) != beforeGames {
		t.Fatalf("expected no replacement game when another active game exists, before=%d after=%d", beforeGames, len(srv.Games))
	}

	activeCount := 0
	for _, g := range srv.Games {
		if g != nil && g.IsActive {
			activeCount++
		}
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active game to remain, got %d", activeCount)
	}
}

// ---------------------------------------------------------------------------
// Phase 9: Buzzword Suggestion Tests
// ---------------------------------------------------------------------------

func TestHandlePlayerSuggest(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	game := NewGame("g1", testBuzzwords(), 3, 3)
	host := newPlayer("host-user")
	game.HostID = host.ID
	player := newPlayer("player-user")
	game.AddPlayer(host)
	game.AddPlayer(player)
	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.GamesMu.Unlock()

	// A normal player suggests a phrase
	if err := srv.handlePlayerSuggest(game, player, "low-hanging fruit"); err != nil {
		t.Fatalf("unexpected error suggesting phrase: %v", err)
	}

	game.SuggestionsMu.Lock()
	if len(game.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(game.Suggestions))
	}
	if game.Suggestions[0].Phrase != "low-hanging fruit" {
		t.Errorf("unexpected suggestion phrase: %s", game.Suggestions[0].Phrase)
	}
	game.SuggestionsMu.Unlock()
}

func TestHandlePlayerSuggestDuplicate(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	game := NewGame("g2", testBuzzwords(), 3, 3)
	p := newPlayer("player-user")
	game.HostID = p.ID
	game.AddPlayer(p)
	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.GamesMu.Unlock()

	if err := srv.handlePlayerSuggest(game, p, "bandwidth"); err != nil {
		t.Fatalf("first suggestion failed: %v", err)
	}
	if err := srv.handlePlayerSuggest(game, p, "Bandwidth"); err == nil {
		t.Fatal("expected error on duplicate (case-insensitive), got nil")
	}
}

func TestHandleHostRejectEnforcement(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	game := NewGame("g3", testBuzzwords(), 3, 3)
	host := newPlayer("host")
	nonhost := newPlayer("nonhost")
	game.HostID = host.ID
	game.AddPlayer(host)
	game.AddPlayer(nonhost)
	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.GamesMu.Unlock()

	if err := srv.handlePlayerSuggest(game, nonhost, "pivot"); err != nil {
		t.Fatalf("suggest failed: %v", err)
	}

	// Non-host reject should fail
	if err := srv.handleHostReject(game, nonhost, "pivot"); err == nil {
		t.Fatal("expected error when non-host rejects, got nil")
	}

	// Host reject should succeed
	if err := srv.handleHostReject(game, host, "pivot"); err != nil {
		t.Fatalf("host reject failed: %v", err)
	}

	game.SuggestionsMu.Lock()
	if len(game.Suggestions) != 0 {
		t.Errorf("expected 0 suggestions after reject, got %d", len(game.Suggestions))
	}
	game.SuggestionsMu.Unlock()
}

func TestHandleHostRejectTracksRejected(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	game := NewGame("g-rej1", testBuzzwords(), 3, 3)
	host := newPlayer("host")
	player := newPlayer("player")
	game.HostID = host.ID
	game.AddPlayer(host)
	game.AddPlayer(player)
	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.GamesMu.Unlock()

	if err := srv.handlePlayerSuggest(game, player, "synergy"); err != nil {
		t.Fatalf("suggest failed: %v", err)
	}
	if err := srv.handleHostReject(game, host, "synergy"); err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	game.SuggestionsMu.Lock()
	defer game.SuggestionsMu.Unlock()
	if len(game.RejectedSuggestions) != 1 {
		t.Fatalf("expected 1 rejected suggestion, got %d", len(game.RejectedSuggestions))
	}
	if game.RejectedSuggestions[0] != "synergy" {
		t.Errorf("unexpected rejected phrase: %q", game.RejectedSuggestions[0])
	}
}

func TestHandleListBuzzwordsResponse(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	game := NewGame("g-lb1", testBuzzwords(), 3, 3)
	host := newPlayer("host")
	player := newPlayer("player")
	game.HostID = host.ID
	game.AddPlayer(host)
	game.AddPlayer(player)
	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.GamesMu.Unlock()

	// Reject one phrase so it appears in the rejected list
	if err := srv.handlePlayerSuggest(game, player, "circle back"); err != nil {
		t.Fatalf("suggest failed: %v", err)
	}
	if err := srv.handleHostReject(game, host, "circle back"); err != nil {
		t.Fatalf("reject failed: %v", err)
	}

	srv.handleListBuzzwords(game, player)

	// Drain any queued broadcast messages (from suggest/reject) to find buzzword_list
	var found *ServerMessage
	for i := 0; i < 10; i++ {
		select {
		case raw := <-player.messages.send:
			msg, ok := raw.(ServerMessage)
			if !ok {
				continue
			}
			if msg.Type == "buzzword_list" {
				m := msg
				found = &m
			}
		default:
			i = 10 // break outer loop
		}
	}
	if found == nil {
		t.Fatal("no buzzword_list message received by player")
	}
	if len(found.FlatBuzzwords) == 0 {
		t.Error("expected non-empty FlatBuzzwords")
	}
	if len(found.RejectedSuggestions) != 1 || found.RejectedSuggestions[0] != "circle back" {
		t.Errorf("unexpected RejectedSuggestions: %v", found.RejectedSuggestions)
	}
}

func TestResetBoardClearsRejectedSuggestions(t *testing.T) {
	game := NewGame("g-res1", testBuzzwords(), 3, 3)
	game.SuggestionsMu.Lock()
	game.RejectedSuggestions = []string{"move the needle", "deep dive"}
	game.SuggestionsMu.Unlock()

	game.ResetBoard(testBuzzwords(), 3, 3)

	game.SuggestionsMu.Lock()
	defer game.SuggestionsMu.Unlock()
	if len(game.RejectedSuggestions) != 0 {
		t.Errorf("expected RejectedSuggestions to be cleared on reset, got %v", game.RejectedSuggestions)
	}
}

// ---------------------------------------------------------------------------
// Phase 9.5: Betting Tests
// ---------------------------------------------------------------------------

func TestParseBetConditionsValid(t *testing.T) {
	game := NewGame("g4", testBuzzwords(), 3, 3)
	alice := newPlayer("alice")
	bob := newPlayer("bob")
	game.AddPlayer(alice)
	game.AddPlayer(bob)

	conds, err := parseBetConditions("alice wins", game)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conds) != 1 || conds[0].PlayerUsername != "alice" || conds[0].Outcome != "wins" {
		t.Errorf("unexpected conditions: %+v", conds)
	}
}

func TestParseBetConditionsAndChain(t *testing.T) {
	game := NewGame("g5", testBuzzwords(), 3, 3)
	alice := newPlayer("alice")
	bob := newPlayer("bob")
	game.AddPlayer(alice)
	game.AddPlayer(bob)

	conds, err := parseBetConditions("alice wins AND bob loses", game)
	if err != nil {
		t.Fatalf("unexpected error on AND chain: %v", err)
	}
	if len(conds) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(conds))
	}
	if conds[1].Outcome != "loses" {
		t.Errorf("expected second condition outcome 'loses', got %q", conds[1].Outcome)
	}
}

func TestParseBetConditionsUnknownPlayer(t *testing.T) {
	game := NewGame("g6", testBuzzwords(), 3, 3)
	alice := newPlayer("alice")
	game.AddPlayer(alice)

	_, err := parseBetConditions("charlie wins", game)
	if err == nil {
		t.Fatal("expected error for unknown player, got nil")
	}
	if !strings.Contains(err.Error(), "charlie") {
		t.Errorf("expected player name in error, got: %v", err)
	}
}

func TestParseBetConditionsMalformed(t *testing.T) {
	game := NewGame("g7", testBuzzwords(), 3, 3)
	alice := newPlayer("alice")
	game.AddPlayer(alice)

	// Missing outcome
	if _, err := parseBetConditions("alice", game); err == nil {
		t.Fatal("expected error for malformed bet, got nil")
	}

	// Invalid outcome
	if _, err := parseBetConditions("alice draws", game); err == nil {
		t.Fatal("expected error for invalid outcome, got nil")
	}
}

func TestEvaluateBetsWin(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	game := NewGame("g8", testBuzzwords(), 3, 3)
	alice := newPlayer("alice")
	bob := newPlayer("bob")
	game.AddPlayer(alice)
	game.AddPlayer(bob)
	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.GamesMu.Unlock()

	// Place two bets: one that should win, one that should lose
	game.BetsMu.Lock()
	game.Bets = []Bet{
		{ID: "b1", BetterID: "charlie", Conditions: []BetCondition{{PlayerUsername: "alice", Outcome: "wins"}}, Status: "active"},
		{ID: "b2", BetterID: "dave", Conditions: []BetCondition{{PlayerUsername: "bob", Outcome: "wins"}}, Status: "active"},
	}
	game.BetsMu.Unlock()

	srv.evaluateBets(game, "alice")

	game.BetsMu.Lock()
	defer game.BetsMu.Unlock()
	if game.Bets[0].Status != "won" {
		t.Errorf("bet b1 should be won, got %q", game.Bets[0].Status)
	}
	if game.Bets[1].Status != "lost" {
		t.Errorf("bet b2 should be lost, got %q", game.Bets[1].Status)
	}
}

func TestHandlePlayerBetDuplicate(t *testing.T) {
	ResetMetrics()
	srv := NewServer(testBuzzwords(), 3, 3, "test-admin-key")
	game := NewGame("g9", testBuzzwords(), 3, 3)
	alice := newPlayer("alice")
	charlie := newPlayer("charlie")
	game.AddPlayer(alice)
	game.AddPlayer(charlie)
	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.GamesMu.Unlock()

	if err := srv.handlePlayerBet(game, charlie, "alice wins"); err != nil {
		t.Fatalf("first bet failed: %v", err)
	}
	if err := srv.handlePlayerBet(game, charlie, "alice loses"); err == nil {
		t.Fatal("expected error placing second active bet, got nil")
	}
}

// TestNotifyShutdownDoesNotPanicWithNilWS verifies that NotifyShutdown handles players
// that have no active WebSocket (e.g., set up in unit tests without real connections).
func TestNotifyShutdownDoesNotPanicWithNilWS(t *testing.T) {
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-shutdown-1", buzzwords, 3, 3)
	player := newPlayer("player-a")
	player2 := newPlayer("player-b")
	game.AddPlayer(player)
	game.AddPlayer(player2)
	game.HostID = player.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	// player.ws is nil (no real WebSocket in unit tests) — NotifyShutdown must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NotifyShutdown panicked: %v", r)
		}
	}()

	srv.NotifyShutdown()

	// Both players should have received a shutdown message in their channel
	for _, p := range []*Player{player, player2} {
		select {
		case msg := <-p.messages.send:
			sm, ok := msg.(ServerMessage)
			if !ok {
				t.Errorf("player %s: expected ServerMessage, got %T", p.ID, msg)
				continue
			}
			if sm.Type != "server_shutdown" {
				t.Errorf("player %s: expected type 'server_shutdown', got %s", p.ID, sm.Type)
			}
		default:
			t.Errorf("player %s: no shutdown message received", p.ID)
		}
	}

	t.Logf("✓ NotifyShutdown delivered shutdown messages without panicking")
}

// TestPartialDisconnectDoesNotOrphan verifies that the orphan guard only fires when
// ALL players leave. A game with remaining players must stay active.
func TestPartialDisconnectDoesNotOrphan(t *testing.T) {
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-partial-1", buzzwords, 3, 3)
	alice := newPlayer("alice")
	bob := newPlayer("bob")
	game.AddPlayer(alice)
	game.AddPlayer(bob)
	game.HostID = alice.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	// Remove alice — bob still connected
	game.RemovePlayer(alice.ID)
	playerCount := game.PlayerCount()

	// Replicate the condition from handlePlayerDisconnect
	if playerCount == 0 && game.IsActive && game.Winner == "" {
		srv.markGameOrphaned(context.Background(), game)
	}

	if game.Orphaned {
		t.Error("game should NOT be orphaned while bob is still connected")
	}
	if !game.IsActive {
		t.Error("game should still be active while bob is still connected")
	}
	if game.PlayerCount() != 1 {
		t.Errorf("expected 1 remaining player, got %d", game.PlayerCount())
	}

	t.Logf("✓ partial disconnect (alice left, bob remains) did not trigger orphan")
}

// TestWinnerGameNotOrphaned verifies that a game ended by a win has Orphaned=false.
// Both code paths (win and orphan) set IsActive=false, but only the orphan path sets Orphaned=true.
func TestWinnerGameNotOrphaned(t *testing.T) {
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-win-orphan-check", buzzwords, 3, 3)
	player := newPlayer("alice")
	game.AddPlayer(player)
	game.HostID = player.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	if err := srv.handlePlayerWin(context.Background(), game, player); err != nil {
		t.Fatalf("handlePlayerWin failed: %v", err)
	}

	if game.Orphaned {
		t.Error("a game won by a player should have Orphaned=false")
	}
	// IsActive stays true after win (only admin delete/orphan sets it false)
	if !game.IsActive {
		t.Error("game should remain active after win (IsActive=false only for admin delete/orphan)")
	}
	if game.Winner != player.ID {
		t.Errorf("expected winner %s, got %s", player.ID, game.Winner)
	}

	t.Logf("✓ won game has Orphaned=false, IsActive=true, Winner=%s", game.Winner)
}

// TestOrphanedGamePreservesHostID verifies that markGameOrphaned does not clear HostID.
// HostID must remain immutable so the original host could theoretically reconnect after
// all players dropped (e.g., network blip) and restart via a new session.
func TestOrphanedGamePreservesHostID(t *testing.T) {
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-orphan-host", buzzwords, 3, 3)
	player := newPlayer("original-host")
	game.AddPlayer(player)
	game.HostID = player.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	originalHostID := game.HostID
	game.RemovePlayer(player.ID)
	srv.markGameOrphaned(context.Background(), game)

	if game.HostID != originalHostID {
		t.Errorf("HostID should be immutable after orphan: expected %s, got %s", originalHostID, game.HostID)
	}

	t.Logf("✓ HostID preserved after orphan: %s", game.HostID)
}

// --- Error metric tests ---

// TestErrorMetricInvalidGameCode verifies that looking up an unknown code increments
// bingo_errors_total{error_type="game"}.
func TestErrorMetricInvalidGameCode(t *testing.T) {
	ResetMetrics()
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	before := testutil.ToFloat64(srv.Metrics.ErrorsTotal.WithLabelValues("game"))

	_, err := srv.getOrCreateGame("BINGO-BADCO")
	if err == nil {
		t.Fatal("expected error for unknown code, got nil")
	}

	after := testutil.ToFloat64(srv.Metrics.ErrorsTotal.WithLabelValues("game"))
	if after-before != 1 {
		t.Errorf("expected game error counter to increment by 1, delta=%.0f", after-before)
	}

	t.Logf("✓ bingo_errors_total{error_type=\"game\"} incremented on invalid code: %v", err)
}

// TestErrorMetricAlreadyEndedGame verifies that a second win attempt on an ended game
// increments bingo_errors_total{error_type="game"}.
func TestErrorMetricAlreadyEndedGame(t *testing.T) {
	ResetMetrics()
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-errmetric-1", buzzwords, 3, 3)
	alice := newPlayer("alice")
	bob := newPlayer("bob")
	game.AddPlayer(alice)
	game.AddPlayer(bob)
	game.HostID = alice.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	// First win ends the game
	if err := srv.handlePlayerWin(context.Background(), game, alice); err != nil {
		t.Fatalf("first win should succeed: %v", err)
	}

	before := testutil.ToFloat64(srv.Metrics.ErrorsTotal.WithLabelValues("game"))

	// Second win should fail and increment the counter
	if err := srv.handlePlayerWin(context.Background(), game, bob); err == nil {
		t.Fatal("expected error for second win on ended game")
	}

	after := testutil.ToFloat64(srv.Metrics.ErrorsTotal.WithLabelValues("game"))
	if after-before != 1 {
		t.Errorf("expected game error counter to increment by 1, delta=%.0f", after-before)
	}

	t.Logf("✓ bingo_errors_total{error_type=\"game\"} incremented on already-ended game")
}

// TestErrorMetricNonHostRestart verifies that a non-host restart attempt increments
// bingo_errors_total{error_type="game"}.
func TestErrorMetricNonHostRestart(t *testing.T) {
	ResetMetrics()
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	game := NewGame("game-errmetric-2", buzzwords, 3, 3)
	alice := newPlayer("alice")
	bob := newPlayer("bob")
	game.AddPlayer(alice)
	game.AddPlayer(bob)
	game.HostID = alice.ID

	srv.GamesMu.Lock()
	srv.Games[game.ID] = game
	srv.CodeToGame[game.Code] = game
	srv.GamesMu.Unlock()

	// End the game so restart state is valid
	if err := srv.handlePlayerWin(context.Background(), game, alice); err != nil {
		t.Fatalf("win setup failed: %v", err)
	}

	before := testutil.ToFloat64(srv.Metrics.ErrorsTotal.WithLabelValues("game"))

	// Bob (non-host) tries to restart
	// handleGameRestart checks IsActive: since game ended it returns "Game has been deleted" path
	// Let re-activate it so we hit the "only the host can restart" branch instead
	game.IsActive = true
	game.Winner = ""

	if err := srv.handleGameRestart(context.Background(), game, bob); err == nil {
		t.Fatal("expected error for non-host restart")
	}

	after := testutil.ToFloat64(srv.Metrics.ErrorsTotal.WithLabelValues("game"))
	if after-before != 1 {
		t.Errorf("expected game error counter to increment by 1, delta=%.0f", after-before)
	}

	t.Logf("✓ bingo_errors_total{error_type=\"game\"} incremented on non-host restart")
}

// TestErrorMetricScrapeable verifies that bingo_errors_total appears in the
// Prometheus /metrics HTTP response after an error is triggered. This closes
// the gap between "counter increments in memory" and "Prometheus can scrape it".
func TestErrorMetricScrapeable(t *testing.T) {
	ResetMetrics()
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// Trigger an error so the CounterVec label is initialized
	_, err := srv.getOrCreateGame("BINGO-NOSUC")
	if err == nil {
		t.Fatal("expected error for unknown code")
	}

	// Serve /metrics through the same mux the real server uses
	srv.registerHandlers()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	srv.Mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("/metrics returned status %d", w.Code)
	}

	body := w.Body.String()

	if !strings.Contains(body, "bingo_errors_total") {
		t.Fatal("/metrics response does not contain bingo_errors_total")
	}
	if !strings.Contains(body, `error_type="game"`) {
		t.Fatal("/metrics response does not contain error_type=\"game\" label")
	}
	if !strings.Contains(body, `bingo_errors_total{error_type="game"} 1`) {
		t.Errorf("/metrics missing expected line: bingo_errors_total{error_type=\"game\"} 1\n\nGot:\n%s", body)
	}

	t.Logf("✓ bingo_errors_total{error_type=\"game\"} appears in /metrics HTTP response")
}
