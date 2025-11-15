package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jkMLnop/binGO-CLI/shared"
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
	buzzwords := testBuzzwords()

	player := NewPlayer("player-1", buzzwords, 3, 3)

	if player.ID != "player-1" {
		t.Errorf("Expected player ID 'player-1', got %s", player.ID)
	}

	if len(player.Cards) != 1 {
		t.Errorf("Expected 1 card, got %d", len(player.Cards))
	}

	card := player.GetFirstCard()
	if card == nil {
		t.Fatal("Expected first card to be non-nil")
	}

	if card.Board.Rows != 3 || card.Board.Cols != 3 {
		t.Errorf("Expected 3x3 board, got %dx%d", card.Board.Rows, card.Board.Cols)
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
	player := NewPlayer("player-1", buzzwords, 3, 3)

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
	player := NewPlayer("player-1", buzzwords, 3, 3)

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

func TestServerHandleMark(t *testing.T) {
	buzzwords := testBuzzwords()

	srv := NewServer(buzzwords, 3, 3, "8080")
	srv.createNewGame()

	game := srv.CurrentGame
	player := NewPlayer("player-1", buzzwords, 3, 3)
	game.AddPlayer(player)

	// Mark cell "5" (center cell)
	err := srv.HandleMark(game.ID, "player-1", "0", "5")
	if err != nil {
		t.Fatalf("Expected no error marking cell, got: %v", err)
	}

	// Verify cell is marked
	card := player.GetFirstCard()
	if !card.Board.Marked["5"] {
		t.Error("Expected cell '5' to be marked")
	}
}

func TestServerCheckWinner(t *testing.T) {
	buzzwords := testBuzzwords()

	srv := NewServer(buzzwords, 3, 3, "8080")
	srv.createNewGame()

	game := srv.CurrentGame
	player := NewPlayer("player-1", buzzwords, 3, 3)
	game.AddPlayer(player)

	// Mark the three center cells vertically (4, 5, 6) - should win
	card := player.GetFirstCard()
	card.Board.MarkCell("4")
	card.Board.MarkCell("5")
	card.Board.MarkCell("6")

	winner, err := srv.CheckWinnerInGame(game.ID)
	if err != nil {
		t.Fatalf("Expected no error checking winner, got: %v", err)
	}

	if winner != "player-1" {
		t.Errorf("Expected winner 'player-1', got '%s'", winner)
	}

	if game.IsActive {
		t.Error("Expected game to be inactive after win")
	}
}

func TestServerBroadcast(t *testing.T) {
	buzzwords := testBuzzwords()

	srv := NewServer(buzzwords, 3, 3, "8080")
	srv.createNewGame()

	game := srv.CurrentGame
	player1 := NewPlayer("player-1", buzzwords, 3, 3)
	player2 := NewPlayer("player-2", buzzwords, 3, 3)
	game.AddPlayer(player1)
	game.AddPlayer(player2)

	// Broadcast message
	msg := shared.ServerMessage{
		Type:    "test",
		Message: "test message",
	}

	err := srv.BroadcastToGame(game.ID, msg)
	if err != nil {
		t.Fatalf("Expected no error broadcasting, got: %v", err)
	}
}

func TestGetPlayerList(t *testing.T) {
	buzzwords := testBuzzwords()

	game := NewGame("game-1", buzzwords, 3, 3)
	player1 := NewPlayer("player-1", buzzwords, 3, 3)
	player2 := NewPlayer("player-2", buzzwords, 3, 3)

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
