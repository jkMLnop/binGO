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

func TestIPClassification(t *testing.T) {
	tests := []struct {
		name          string
		clientIP      string
		serverIP      string
		expectedType  IPType
		expectedLocal bool
	}{
		// Localhost tests
		{"localhost IPv4", "127.0.0.1", "127.0.0.1", Localhost, true},
		{"localhost IPv6", "::1", "::1", Localhost, true},

		// LAN tests (same /24 subnet)
		{"LAN IPv4 same subnet", "192.168.1.100", "192.168.1.1", LAN, true},
		{"LAN IPv4 same subnet 2", "10.0.0.50", "10.0.0.1", LAN, true},

		// Remote tests
		{"remote different subnet", "203.0.113.100", "192.168.1.1", Remote, false},
		{"remote different class A", "172.16.0.1", "192.168.1.1", Remote, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test ClassifyIP
			result := ClassifyIP(tt.clientIP, tt.serverIP)
			if result != tt.expectedType {
				t.Errorf("ClassifyIP(%s, %s) = %v, want %v", tt.clientIP, tt.serverIP, result, tt.expectedType)
			}

			// Test IsLocalConnection
			isLocal := IsLocalConnection(tt.clientIP, tt.serverIP)
			if isLocal != tt.expectedLocal {
				t.Errorf("IsLocalConnection(%s, %s) = %v, want %v", tt.clientIP, tt.serverIP, isLocal, tt.expectedLocal)
			}
		})
	}
}

func TestServerCodeBasedLookup(t *testing.T) {
	ResetMetrics() // Reset metrics before test
	
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// Check that current game has a code
	if srv.CurrentGame.Code == "" {
		t.Error("Expected current game to have a code")
	}

	code := srv.CurrentGame.Code

	// Check that game is registered by code
	if game, exists := srv.CodeToGame[code]; !exists {
		t.Error("Expected game to be registered by code in CodeToGame map")
	} else if game.ID != srv.CurrentGame.ID {
		t.Errorf("Expected CodeToGame[%s] to point to current game", code)
	}
}

func TestLocalConnectionCanJoinWithoutCode(t *testing.T) {
	ResetMetrics() // Reset metrics before test
	
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// Local connection (localhost) should be able to join without code
	code := ""
	clientIP := "127.0.0.1"
	serverIP := "127.0.0.1"

	game, err := srv.getOrCreateGame(code, clientIP, serverIP)
	if err != nil {
		t.Errorf("Expected local connection to join without code, got error: %v", err)
	}

	if game == nil || game.ID != srv.CurrentGame.ID {
		t.Error("Expected to get current game for local connection")
	}
}

func TestRemoteConnectionRequiresCode(t *testing.T) {
	ResetMetrics() // Reset metrics before test
	
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// Remote connection without code should fail
	code := ""
	clientIP := "203.0.113.100" // Remote IP
	serverIP := "192.168.1.1"

	_, err := srv.getOrCreateGame(code, clientIP, serverIP)
	if err == nil {
		t.Error("Expected remote connection without code to fail")
	}
}

func TestCodeBasedGameJoin(t *testing.T) {
	ResetMetrics() // Reset metrics before test
	
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// Get current game's code
	correctCode := srv.CurrentGame.Code

	// Remote connection with correct code should succeed
	clientIP := "203.0.113.100"
	serverIP := "192.168.1.1"

	game, err := srv.getOrCreateGame(correctCode, clientIP, serverIP)
	if err != nil {
		t.Errorf("Expected remote connection with correct code to succeed, got error: %v", err)
	}

	if game.ID != srv.CurrentGame.ID {
		t.Error("Expected to get current game when providing correct code")
	}
}

func TestInvalidCodeRejected(t *testing.T) {
	ResetMetrics() // Reset metrics before test
	
	buzzwords := testBuzzwords()
	srv := NewServer(buzzwords, 3, 3, "8080")

	// Invalid code should be rejected
	invalidCode := "BINGO-XXXXX"
	clientIP := "203.0.113.100"
	serverIP := "192.168.1.1"

	_, err := srv.getOrCreateGame(invalidCode, clientIP, serverIP)
	if err == nil {
		t.Error("Expected invalid code to be rejected")
	}
}
