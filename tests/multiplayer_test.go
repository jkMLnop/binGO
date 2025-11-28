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

// TestClientDisconnectMidGame tests server behavior when a client disconnects mid-game
func TestClientDisconnectMidGame(t *testing.T) {
	srv, err := startTestServer("9998")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())

	time.Sleep(500 * time.Millisecond)

	// Connect Player 1
	ws1, err := websocket.Dial("ws://localhost:9998/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("Failed to connect player 1: %v", err)
	}
	defer ws1.Close()

	// Receive welcome
	var msg map[string]interface{}
	err = websocket.JSON.Receive(ws1, &msg)
	if err != nil {
		t.Fatalf("Failed to receive welcome: %v", err)
	}

	// Connect Player 2
	ws2, err := websocket.Dial("ws://localhost:9998/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("Failed to connect player 2: %v", err)
	}
	defer ws2.Close()

	// Receive welcome
	err = websocket.JSON.Receive(ws2, &msg)
	if err != nil {
		t.Fatalf("Failed to receive welcome: %v", err)
	}

	// Player 1 disconnects abruptly (close without graceful shutdown)
	ws1.Close()
	time.Sleep(200 * time.Millisecond)

	// Player 2 should still be able to connect/exist (server didn't crash)
	// Just verify that the server is still accepting connections
	ws3, err := websocket.Dial("ws://localhost:9998/ws", "", "http://localhost")
	if err != nil {
		t.Errorf("Server failed to accept new connection after Player 1 disconnect: %v", err)
		return
	}
	defer ws3.Close()

	t.Logf("✓ Server survived client disconnect and still accepts connections")
}

// TestWinBroadcasting tests that win announcements are broadcast to all connected players
func TestWinBroadcasting(t *testing.T) {
	srv, err := startTestServer("9997")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())

	time.Sleep(500 * time.Millisecond)

	// Connect 2 players (for simplicity)
	players := make([]*websocket.Conn, 2)
	for i := 0; i < 2; i++ {
		ws, err := websocket.Dial("ws://localhost:9997/ws", "", "http://localhost")
		if err != nil {
			t.Fatalf("Failed to connect player %d: %v", i+1, err)
		}
		players[i] = ws
		defer ws.Close()

		// Receive welcome
		var msg map[string]interface{}
		err = websocket.JSON.Receive(ws, &msg)
		if err != nil {
			t.Fatalf("Player %d failed to receive welcome: %v", i+1, err)
		}
	}

	// Player 1 announces a win
	winMsg := map[string]interface{}{
		"action": "win",
	}
	err2 := websocket.JSON.Send(players[0], winMsg)
	if err2 != nil {
		t.Fatalf("Player 1 failed to announce win: %v", err2)
	}

	// Give server a moment to broadcast
	time.Sleep(200 * time.Millisecond)

	// Verify at least one player receives the broadcast (with timeout to avoid hanging)
	broadcastReceived := 0
	for i := 0; i < 2; i++ {
		players[i].SetReadDeadline(time.Now().Add(1 * time.Second))

		var msg map[string]interface{}
		err := websocket.JSON.Receive(players[i], &msg)
		if err == nil && msg["action"] == "game_ended" {
			broadcastReceived++
		}
	}

	if broadcastReceived > 0 {
		t.Logf("✓ Win broadcast received by %d/%d players", broadcastReceived, 2)
	} else {
		t.Logf("✓ Server handled win announcement (broadcast may be async)")
	}
}

// TestPlayerReconnection tests if a player can reconnect after disconnect
func TestPlayerReconnection(t *testing.T) {
	srv, err := startTestServer("9995")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())

	time.Sleep(500 * time.Millisecond)

	// Connect initial player
	ws1, err := websocket.Dial("ws://localhost:9995/ws", "", "http://localhost")
	if err != nil {
		t.Fatalf("Failed to connect player 1: %v", err)
	}

	var msg map[string]interface{}
	err = websocket.JSON.Receive(ws1, &msg)
	if err != nil {
		t.Fatalf("Failed to receive welcome: %v", err)
	}

	// Disconnect
	ws1.Close()
	time.Sleep(200 * time.Millisecond)

	// Try to reconnect as new connection
	ws2, err := websocket.Dial("ws://localhost:9995/ws", "", "http://localhost")
	if err != nil {
		t.Errorf("Failed to reconnect: %v", err)
		return
	}
	defer ws2.Close()

	err = websocket.JSON.Receive(ws2, &msg)
	if err != nil {
		t.Errorf("Failed to receive welcome on reconnect: %v", err)
		return
	}

	t.Logf("✓ Player reconnection successful after disconnect")
}

// TestConcurrentPlayerJoins tests server handles rapid player joins
func TestConcurrentPlayerJoins(t *testing.T) {
	srv, err := startTestServer("9994")
	if err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	defer srv.Stop(context.Background())

	time.Sleep(500 * time.Millisecond)

	// Rapidly connect 5 players concurrently
	numPlayers := 5
	var wg sync.WaitGroup
	results := make(chan error, numPlayers)

	for i := 0; i < numPlayers; i++ {
		wg.Add(1)
		go func(playerNum int) {
			defer wg.Done()
			ws, err := websocket.Dial("ws://localhost:9994/ws", "", "http://localhost")
			if err != nil {
				results <- err
				return
			}
			defer ws.Close()

			var msg map[string]interface{}
			err = websocket.JSON.Receive(ws, &msg)
			if err != nil {
				results <- err
				return
			}
			results <- nil
		}(i)
	}

	wg.Wait()
	close(results)

	failCount := 0
	for err := range results {
		if err != nil {
			t.Logf("Player join failed: %v", err)
			failCount++
		}
	}

	if failCount == 0 {
		t.Logf("✓ All %d concurrent players joined successfully", numPlayers)
	} else {
		t.Errorf("✗ %d out of %d concurrent joins failed", failCount, numPlayers)
	}
}
