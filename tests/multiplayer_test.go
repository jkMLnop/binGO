//go:build integration
// +build integration

package tests

import (
	"context"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jkMLnop/binGO-CLI/server"
	"github.com/jkMLnop/binGO-CLI/shared"
	"golang.org/x/net/websocket"
)

// findBuzzwordsFile locates and loads buzzwords.csv from multiple possible paths
func findBuzzwordsFile() ([][]string, error) {
	buzzwordPaths := []string{
		"buzzwords.csv",
		"../buzzwords.csv",
		filepath.Join(os.Getenv("PWD"), "buzzwords.csv"),
	}

	for _, path := range buzzwordPaths {
		if _, err := os.Stat(path); err == nil {
			buzzwords := shared.LoadBuzzwords(path)
			if len(buzzwords) > 0 {
				return buzzwords, nil
			}
		}
	}

	return nil, &os.PathError{Op: "open", Path: "buzzwords.csv", Err: os.ErrNotExist}
}

// createTestGame creates a new game session for testing (3x3 speed bingo)
func createTestGame(t *testing.T) *shared.GameSession {
	buzzwords, err := findBuzzwordsFile()
	if err != nil {
		t.Fatalf("Failed to load buzzwords: %v", err)
	}
	return shared.NewGameSession(buzzwords, 3, 3)
}

// runServerForTest starts a server for testing purposes
func runServerForTest(port string) {
	// Load buzzwords from CSV
	buzzwords, err := findBuzzwordsFile()
	if err != nil {
		log.Fatalf("Could not find buzzwords.csv: %v", err)
	}

	// Create server (3x3 for speed bingo mode)
	srv := server.NewServer(buzzwords, 3, 3, port)

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-sigChan
	log.Println("Shutdown signal received")

	// Graceful shutdown with 5 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Stop(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	log.Println("Server stopped")
}

// startTestServer starts a server for testing and returns it (non-blocking)
func startTestServer(port string) (*server.Server, error) {
	buzzwords, err := findBuzzwordsFile()
	if err != nil {
		return nil, err
	}

	srv := server.NewServer(buzzwords, 3, 3, port)
	go func() {
		if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Printf("Server error on port %s: %v", port, err)
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)
	return srv, nil
}

// TestMultiplayerGameFlow tests server with 2 connected clients
// Player 1 marks cells to win, Player 2 participates
// Run with: go test ./tests -tags=integration -run TestMultiplayerGameFlow -v
func TestMultiplayerGameFlow(t *testing.T) {
	serverPort := "9999"
	serverAddr := "localhost:" + serverPort

	// Start server in background
	srv, err := startTestServer(serverPort)
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())

	// Give server time to start
	time.Sleep(500 * time.Millisecond)

	// Player 1 marks: 7, 8, 9 (top row = win)
	player1Moves := []string{"7", "8", "9"}
	// Player 2 marks: 1, 2 (no win)
	player2Moves := []string{"1", "2"}

	var wg sync.WaitGroup
	var p1GameEnded, p2GameEnded bool
	var p1Won, p2Won bool

	// Player 1 connects and plays
	wg.Add(1)
	go func() {
		defer wg.Done()
		p1GameEnded, p1Won = playMultiplayerGame(t, "Player1", serverAddr, player1Moves)
	}()

	time.Sleep(100 * time.Millisecond)

	// Player 2 connects and plays
	wg.Add(1)
	go func() {
		defer wg.Done()
		p2GameEnded, p2Won = playMultiplayerGame(t, "Player2", serverAddr, player2Moves)
	}()

	wg.Wait()

	// Assertions
	if !p1GameEnded {
		t.Error("Player 1 did not receive game_ended message")
	}
	if !p2GameEnded {
		t.Error("Player 2 did not receive game_ended message")
	}
	if !p1Won {
		t.Error("Player 1 should have won (marked 7, 8, 9 = top row)")
	}
	if p2Won {
		t.Error("Player 2 should not have won (only marked 1, 2)")
	}

	t.Log("✓ Multiplayer test passed: server + 2 clients, Player 1 won")
}

// playMultiplayerGame connects a client to server and plays with given moves
func playMultiplayerGame(t *testing.T, playerName string, serverAddr string, moves []string) (bool, bool) {
	wsURL := url.URL{Scheme: "ws", Host: serverAddr, Path: "/ws"}

	ws, err := websocket.Dial(wsURL.String(), "", "http://localhost")
	if err != nil {
		t.Errorf("[%s] Failed to connect: %v", playerName, err)
		return false, false
	}
	defer ws.Close()

	t.Logf("[%s] Connected to server", playerName)

	gameEnded := false
	playerWon := false
	done := make(chan bool, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Receive welcome message from server
	var welcomeMsg map[string]interface{}
	err = websocket.JSON.Receive(ws, &welcomeMsg)
	if err != nil {
		t.Errorf("[%s] Failed to receive welcome: %v", playerName, err)
		return false, false
	}

	var playerID string
	if pid, ok := welcomeMsg["player_id"].(string); ok {
		playerID = pid
	}
	t.Logf("[%s] Received welcome as %s", playerName, playerID)

	// Create a local game session (same as what client does)
	localGame := createTestGame(t)

	// Listen for game_ended messages from server
	go func() {
		for {
			select {
			case <-ctx.Done():
				done <- false
				return
			default:
			}

			var msg map[string]interface{}
			err := websocket.JSON.Receive(ws, &msg)
			if err != nil {
				if strings.Contains(err.Error(), "EOF") {
					done <- gameEnded
					return
				}
				t.Logf("[%s] Receive error: %v", playerName, err)
				done <- false
				return
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "game_ended":
				gameEnded = true
				winner, _ := msg["winner"].(string)
				if winner == playerID {
					playerWon = true
					t.Logf("[%s] ✓ WINNER!", playerName)
				} else {
					t.Logf("[%s] Game ended (winner: %v)", playerName, winner)
				}
				done <- true
				return
			}
		}
	}()

	// Mark cells and check for win
	for _, move := range moves {
		err := localGame.Board.MarkCell(move)
		if err != nil {
			t.Logf("[%s] Cell already marked: %v", playerName, err)
		} else {
			t.Logf("[%s] Marked cell %s", playerName, move)
		}

		// Check if we've won
		if localGame.CheckWin() {
			t.Logf("[%s] Detected local win, announcing to server", playerName)
			// Send win announcement to server
			winMsg := map[string]interface{}{
				"action": "win",
			}
			err := websocket.JSON.Send(ws, winMsg)
			if err != nil {
				t.Errorf("[%s] Failed to announce win: %v", playerName, err)
				return false, false
			}
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Wait for game end
	select {
	case result := <-done:
		return result, playerWon
	case <-ctx.Done():
		t.Logf("[%s] Timeout", playerName)
		return false, false
	}
}
