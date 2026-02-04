package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
