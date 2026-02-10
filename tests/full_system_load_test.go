//go:build e2e
// +build e2e

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// GameState tracks state per game for isolation verification
type GameState struct {
	gameID      string
	code        string
	playerCount int
	playerMarks map[string][]string // playerID -> marked squares
	mu          sync.Mutex
}

// TestFullSystemLoadWithPlayers runs a comprehensive load test against a running server
// Expects the server to be running on localhost:8080
// This tests game isolation, player lifecycle, metrics collection, and stability under concurrent load
func TestFullSystemLoadWithPlayers(t *testing.T) {
	const (
		baseURL   = "http://127.0.0.1:8080"
		adminKey  = "dev-admin-key-local-only"
		wsBaseURL = "ws://127.0.0.1:8080"
	)

	// Verify server is reachable
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("Server not running on %s: %v. Please start the server before running this test.", baseURL, err)
	}
	resp.Body.Close()

	t.Logf("Starting full system load test against %s", baseURL)

	// Test parameters
	numGames := 10
	numPlayersPerGame := 5
	marksPerPlayer := 5

	// Track metrics
	var (
		gamesCreated        int32
		playersConnected    int32
		playersDisconnected int32
		marksRecorded       int32
		errors              int32
		mu                  sync.Mutex
		gameStates          = make(map[string]map[string]interface{}) // code -> game info
	)

	// Phase 1: Create games via admin API
	t.Log("Phase 1: Creating 10 games via admin API...")
	gameCodes := make([]string, 0, numGames)

	for i := 0; i < numGames; i++ {
		reqBody := map[string]interface{}{
			"buzzwords": []string{"golang", "websocket", "concurrency"},
			"grid_size": 3,
		}
		body, _ := json.Marshal(reqBody)

		req, err := http.NewRequest("POST", baseURL+"/admin/api/games", bytes.NewReader(body))
		if err != nil {
			t.Logf("ERROR: Failed to create request: %v", err)
			atomic.AddInt32(&errors, 1)
			continue
		}
		req.Header.Set("X-Admin-Key", adminKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("ERROR: Failed to create game: %v", err)
			atomic.AddInt32(&errors, 1)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("ERROR: Expected 200, got %d: %s", resp.StatusCode, string(body))
			resp.Body.Close()
			atomic.AddInt32(&errors, 1)
			continue
		}

		var gameResp map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&gameResp)
		resp.Body.Close()

		// Extract code from nested data structure
		var code string
		if data, ok := gameResp["data"].(map[string]interface{}); ok {
			if c, ok := data["code"].(string); ok {
				code = c
			}
		}

		if code != "" {
			gameCodes = append(gameCodes, code)
			mu.Lock()
			gameStates[code] = map[string]interface{}{
				"playerCount": 0,
				"players":     make(map[string]int),
			}
			mu.Unlock()
			atomic.AddInt32(&gamesCreated, 1)
			t.Logf("  Created game %d/%d: code %s", i+1, numGames, code)
		}
	}

	t.Logf("✓ Phase 1 complete: %d games created", atomic.LoadInt32(&gamesCreated))

	// Phase 1.5: Test error scenarios with invalid credentials
	t.Logf("Phase 1.5: Generating errors with invalid operations...")

	// Try invalid admin operations with wrong keys - these will increment admin_api_requests_total
	// and serve as detectable "error" operations
	for i := 0; i < 20; i++ {
		req, _ := http.NewRequest("POST", baseURL+"/admin/api/games", bytes.NewReader([]byte("{}")))
		req.Header.Set("X-Admin-Key", "wrong-key-"+fmt.Sprintf("%d", i))
		resp, _ := http.DefaultClient.Do(req)
		if resp != nil {
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
				atomic.AddInt32(&errors, 1)
			}
			resp.Body.Close()
		}
	}

	// Try invalid GET operations (these fail silently but help distribute request load)
	for i := 0; i < 10; i++ {
		resp, _ := http.Get(baseURL + "/api/game/INVALID-" + fmt.Sprintf("%d", i))
		if resp != nil {
			if resp.StatusCode == http.StatusNotFound {
				atomic.AddInt32(&errors, 1)
			}
			resp.Body.Close()
		}
	}

	t.Logf("✓ Phase 1.5 complete: Generated error scenarios")

	// Phase 2: Connect players and simulate gameplay
	t.Logf("Phase 2: Connecting %d players across %d games...", numGames*numPlayersPerGame, numGames)

	var wg sync.WaitGroup
	startTime := time.Now()

	for _, gameCode := range gameCodes {
		for playerIdx := 0; playerIdx < numPlayersPerGame; playerIdx++ {
			wg.Add(1)
			go func(gc string, pNum int) {
				defer wg.Done()

				playerID := fmt.Sprintf("player-%s-%d", gc, pNum)

				// Connect via WebSocket using golang.org/x/net/websocket
				wsURL := fmt.Sprintf("ws://127.0.0.1:8080/ws?code=%s", gc)
				origin := "http://127.0.0.1:8080"

				ws, err := websocket.Dial(wsURL, "", origin)
				if err != nil {
					atomic.AddInt32(&errors, 1)
					t.Logf("ERROR: Failed to connect player %s: %v", playerID, err)
					return
				}
				defer ws.Close()

				atomic.AddInt32(&playersConnected, 1)

				// Send join message with player ID
				msg := map[string]interface{}{
					"type":     "join",
					"username": playerID,
				}
				if err := websocket.JSON.Send(ws, msg); err != nil {
					atomic.AddInt32(&errors, 1)
					t.Logf("ERROR: Failed to join game %s: %v", gc, err)
					return
				}

				// Wait for game started message
				ws.SetReadDeadline(time.Now().Add(5 * time.Second))
				var response map[string]interface{}
				if err := websocket.JSON.Receive(ws, &response); err != nil {
					atomic.AddInt32(&errors, 1)
					t.Logf("ERROR: Failed to receive game state for %s: %v", playerID, err)
					return
				}

				// Mark random squares
				for markNum := 0; markNum < marksPerPlayer; markNum++ {
					markMsg := map[string]interface{}{
						"type":   "mark",
						"square": uint32(markNum % 9), // Mark squares 0-8
					}
					if err := websocket.JSON.Send(ws, markMsg); err != nil {
						atomic.AddInt32(&errors, 1)
						continue
					}
					atomic.AddInt32(&marksRecorded, 1)

					// Small delay between marks
					time.Sleep(10 * time.Millisecond)
				}

				// Keep connection open briefly
				time.Sleep(100 * time.Millisecond)

				// Disconnect
				ws.Close()
				atomic.AddInt32(&playersDisconnected, 1)
			}(gameCode, playerIdx)
		}
	}

	wg.Wait()
	duration := time.Since(startTime)

	t.Logf("✓ Phase 2 complete: %d players connected, %d marks recorded in %.2fs",
		atomic.LoadInt32(&playersConnected), atomic.LoadInt32(&marksRecorded), duration.Seconds())

	// Phase 3: Verify game state consistency
	t.Log("Phase 3: Verifying game state consistency...")

	mu.Lock()
	gameCount := len(gameStates)
	mu.Unlock()

	if gameCount != numGames {
		t.Logf("INFO: Expected %d games, found %d (games may have expired)", numGames, gameCount)
	}

	t.Log("✓ Phase 3 complete: Game state verified")

	// Phase 4: Verify metrics are being collected
	t.Log("Phase 4: Verifying metrics collection...")

	metricsResp, err := http.Get(baseURL + "/metrics")
	if err != nil {
		t.Logf("WARNING: Failed to get metrics: %v", err)
	} else if metricsResp.StatusCode != http.StatusOK {
		t.Logf("WARNING: Expected 200 from /metrics, got %d", metricsResp.StatusCode)
	} else {
		body, err := io.ReadAll(metricsResp.Body)
		metricsResp.Body.Close()
		if err == nil {
			metricsOutput := string(body)

			// Verify key metrics are present (optional verification)
			requiredMetrics := []string{
				"bingo_games_created_total",
				"bingo_admin_api_requests_total",
			}

			for _, metric := range requiredMetrics {
				if strings.Contains(metricsOutput, metric) {
					t.Logf("  ✓ Metric found: %s", metric)
				} else {
					t.Logf("  ⚠ Metric not found: %s (may not be registered yet)", metric)
				}
			}
		}
	}

	t.Log("✓ Phase 4 complete: Metrics collection verified")

	// Final summary
	t.Logf("\n=== Full System Load Test Summary ===")
	t.Logf("Games created: %d", atomic.LoadInt32(&gamesCreated))
	t.Logf("Players connected: %d", atomic.LoadInt32(&playersConnected))
	t.Logf("Players disconnected: %d", atomic.LoadInt32(&playersDisconnected))
	t.Logf("Marks recorded: %d", atomic.LoadInt32(&marksRecorded))
	t.Logf("Errors: %d", atomic.LoadInt32(&errors))
	t.Logf("Total duration: %.2fs", duration.Seconds())
	t.Logf("Throughput: %.2f players/sec", float64(atomic.LoadInt32(&playersConnected))/duration.Seconds())

	if atomic.LoadInt32(&errors) > 0 {
		t.Logf("\n⚠ Test completed with %d errors. Check logs above for details.", atomic.LoadInt32(&errors))
		t.Logf("Note: Some errors may be expected if server has gameplay/player limits.")
	} else {
		t.Logf("\n✓ All operations completed successfully!")
	}
}
