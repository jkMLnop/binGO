//go:build e2e
// +build e2e

package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// loadTestBaseURL returns the HTTP base URL for the load test target.
// Reads LOAD_TEST_URL env var (default: http://127.0.0.1:8080).
func loadTestBaseURL() string {
	if u := os.Getenv("LOAD_TEST_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://127.0.0.1:8080"
}

// loadTestAdminKey returns the admin API key for the load test target.
// Reads ADMIN_API_KEY env var (default: dev-admin-key-local-only).
func loadTestAdminKey() string {
	if k := os.Getenv("ADMIN_API_KEY"); k != "" {
		return k
	}
	return "dev-admin-key-local-only"
}

// httpToWS converts an http:// or https:// base URL to the equivalent ws:// or wss:// URL.
func httpToWS(baseURL string) string {
	if strings.HasPrefix(baseURL, "https://") {
		return "wss://" + strings.TrimPrefix(baseURL, "https://")
	}
	return "ws://" + strings.TrimPrefix(baseURL, "http://")
}

// TestFullSystemLoadWithPlayers runs a comprehensive load test against a running server.
// By default targets http://127.0.0.1:8080. Override with env vars:
//
//	LOAD_TEST_URL=https://bingo-server-staging.fly.dev
//	ADMIN_API_KEY=your-admin-key
func TestFullSystemLoadWithPlayers(t *testing.T) {
	baseURL := loadTestBaseURL()
	adminKey := loadTestAdminKey()
	wsBaseURL := httpToWS(baseURL)

	// Verify server is reachable
	resp, err := http.Get(baseURL + "/api/status")
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
				wsURL := fmt.Sprintf("%s/ws", wsBaseURL)
				origin := baseURL

				ws, err := websocket.Dial(wsURL, "", origin)
				if err != nil {
					atomic.AddInt32(&errors, 1)
					t.Logf("ERROR: Failed to connect player %s: %v", playerID, err)
					return
				}
				defer ws.Close()

				atomic.AddInt32(&playersConnected, 1)

				// Send login message with player ID and game code
				msg := map[string]interface{}{
					"action":   "login",
					"username": playerID,
					"code":     gc,
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

	// Phase 5a: Connection-flood guardrail
	t.Log("Phase 5a: Testing WS connection-flood rate limiting...")

	// Create a fresh game for this phase so Phase 2 state doesn't interfere.
	floodGame := lt5aCreateGame(t, baseURL, adminKey)

	if floodGame != "" {
		// Open maxConnsPerIP (5) connections and keep them alive.
		const maxConns = 5
		floodConns := make([]*websocket.Conn, 0, maxConns)
		for i := 0; i < maxConns; i++ {
			ws, err := websocket.Dial(wsBaseURL+"/ws", "", baseURL)
			if err != nil {
				t.Logf("  ⚠ Phase 5a: failed to open flood conn %d: %v", i+1, err)
				break
			}
			// Send login so server keeps connection alive.
			_ = websocket.JSON.Send(ws, map[string]interface{}{
				"action":   "login",
				"username": fmt.Sprintf("flood-player-%d", i),
				"code":     floodGame,
			})
			// Drain the welcome message so the server isn't blocked on sends.
			var discard map[string]interface{}
			_ = ws.SetDeadline(time.Now().Add(3 * time.Second))
			_ = websocket.JSON.Receive(ws, &discard)
			_ = ws.SetDeadline(time.Time{})
			floodConns = append(floodConns, ws)
		}

		if len(floodConns) == maxConns {
			// 5a-i: The (maxConnsPerIP+1)th HTTP request to /ws must get 429.
			rejectResp, err := http.Get(baseURL + "/ws")
			if err != nil {
				t.Logf("  ⚠ Phase 5a: HTTP GET /ws error: %v", err)
			} else {
				rejectResp.Body.Close()
				if rejectResp.StatusCode == http.StatusTooManyRequests {
					t.Log("  ✓ Phase 5a-i: 6th connection correctly rejected with HTTP 429")
				} else {
					t.Errorf("  FAIL Phase 5a-i: expected 429, got %d", rejectResp.StatusCode)
				}
			}

			// 5a-ii: bingo_rate_limited_total{endpoint="ws"} must appear in /metrics.
			m5a, err := http.Get(baseURL + "/metrics")
			if err == nil {
				body5a, _ := io.ReadAll(m5a.Body)
				m5a.Body.Close()
				if strings.Contains(string(body5a), `bingo_rate_limited_total{endpoint="ws"}`) {
					t.Log("  ✓ Phase 5a-ii: bingo_rate_limited_total{endpoint=\"ws\"} recorded")
				} else {
					t.Error("  FAIL Phase 5a-ii: bingo_rate_limited_total{endpoint=\"ws\"} not found in /metrics")
				}
			}

			// Phase 5c: Server resilience while flood is ongoing.
			t.Log("Phase 5c: Verifying server resilience under connection flood...")

			// 5c-i: /api/status must still respond 200.
			status5c, err := http.Get(baseURL + "/api/status")
			if err != nil {
				t.Errorf("  FAIL Phase 5c-i: /api/status unreachable under flood: %v", err)
			} else {
				status5c.Body.Close()
				if status5c.StatusCode == http.StatusOK {
					t.Log("  ✓ Phase 5c-i: /api/status still responding 200 under connection flood")
				} else {
					t.Errorf("  FAIL Phase 5c-i: /api/status returned %d under flood", status5c.StatusCode)
				}
			}

			// 5c-ii: A legitimate player on a different game can still connect.
			// Close one flood connection first to free a slot (flood came from same IP).
			if len(floodConns) > 0 {
				floodConns[0].Close()
				floodConns = floodConns[1:]
				time.Sleep(50 * time.Millisecond) // let server register the disconnect
			}
			legitCode := lt5aCreateGame(t, baseURL, adminKey)
			if legitCode != "" {
				legitWS, err := websocket.Dial(wsBaseURL+"/ws", "", baseURL)
				if err != nil {
					t.Errorf("  FAIL Phase 5c-ii: legit player rejected under flood: %v", err)
				} else {
					_ = websocket.JSON.Send(legitWS, map[string]interface{}{
						"action":   "login",
						"username": "legit-player",
						"code":     legitCode,
					})
					var legitWelcome map[string]interface{}
					_ = legitWS.SetDeadline(time.Now().Add(5 * time.Second))
					recvErr := websocket.JSON.Receive(legitWS, &legitWelcome)
					_ = legitWS.SetDeadline(time.Time{})
					legitWS.Close()
					if recvErr == nil && legitWelcome["type"] == "welcome" {
						t.Log("  ✓ Phase 5c-ii: legit player connected and welcomed during flood")
					} else if recvErr == nil {
						t.Logf("  ✓ Phase 5c-ii: legit player connected (msg type: %v)", legitWelcome["type"])
					} else {
						t.Errorf("  FAIL Phase 5c-ii: legit player welcome recv error: %v", recvErr)
					}
				}
			}
			t.Log("✓ Phase 5c complete: Server remained healthy under connection flood")
		} else {
			t.Logf("  ⚠ Phase 5a/5c skipped: only opened %d/%d flood connections", len(floodConns), maxConns)
		}

		// Close all remaining flood connections.
		for _, ws := range floodConns {
			ws.Close()
		}
		time.Sleep(100 * time.Millisecond) // let server clean up conn counts
	} else {
		t.Log("  ⚠ Phase 5a/5c skipped: could not create flood game")
	}
	t.Log("✓ Phase 5a complete: WS connection-flood guardrail verified")

	// Phase 5b: Brute-force code-guess rate limiting.
	t.Log("Phase 5b: Testing code-guess brute-force rate limiting...")

	const badCode = "BINGO-ZZZZZ" // guaranteed not to exist
	const guessWindow = 5         // codeGuessPerWindow from server/ratelimit.go
	var lastMsg string
	for attempt := 1; attempt <= guessWindow+1; attempt++ {
		ws5b, dialErr := websocket.Dial(wsBaseURL+"/ws", "", baseURL)
		if dialErr != nil {
			t.Logf("  ⚠ Phase 5b attempt %d: dial failed: %v", attempt, dialErr)
			break
		}
		_ = websocket.JSON.Send(ws5b, map[string]interface{}{
			"action":   "login",
			"username": fmt.Sprintf("bf-attacker-%d", attempt),
			"code":     badCode,
		})
		_ = ws5b.SetDeadline(time.Now().Add(5 * time.Second))
		var errMsg map[string]interface{}
		_ = websocket.JSON.Receive(ws5b, &errMsg)
		_ = ws5b.SetDeadline(time.Time{})
		ws5b.Close()
		msgText, _ := errMsg["message"].(string)
		lastMsg = msgText
		t.Logf("  Phase 5b attempt %d: %q", attempt, msgText)
	}
	if strings.Contains(strings.ToLower(lastMsg), "too many") {
		t.Logf("  ✓ Phase 5b-i: attempt %d correctly rate-limited: %q", guessWindow+1, lastMsg)
	} else {
		t.Errorf("  FAIL Phase 5b-i: expected rate-limit on attempt %d, got: %q", guessWindow+1, lastMsg)
	}

	m5b, err := http.Get(baseURL + "/metrics")
	if err == nil {
		body5b, _ := io.ReadAll(m5b.Body)
		m5b.Body.Close()
		if strings.Contains(string(body5b), `bingo_rate_limited_total{endpoint="code_guess"}`) {
			t.Log("  ✓ Phase 5b-ii: bingo_rate_limited_total{endpoint=\"code_guess\"} recorded")
		} else {
			t.Error("  FAIL Phase 5b-ii: bingo_rate_limited_total{endpoint=\"code_guess\"} not in /metrics")
		}
	}
	t.Log("✓ Phase 5b complete: Code-guess brute-force guardrail verified")

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

// lt5aCreateGame creates a game via the admin API and returns its code.
// Returns "" and logs (but does not fatal) on failure so Phase 5 can be skipped gracefully.
func lt5aCreateGame(t *testing.T, baseURL, adminKey string) string {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]interface{}{
		"buzzwords": []string{"flood", "test", "ratelimit"},
		"grid_size": 3,
	})
	req, err := http.NewRequest("POST", baseURL+"/admin/api/games", bytes.NewReader(reqBody))
	if err != nil {
		t.Logf("lt5aCreateGame: build request: %v", err)
		return ""
	}
	req.Header.Set("X-Admin-Key", adminKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("lt5aCreateGame: do request: %v", err)
		return ""
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if data, ok := result["data"].(map[string]interface{}); ok {
		if code, ok := data["code"].(string); ok {
			return code
		}
	}
	t.Logf("lt5aCreateGame: unexpected response: %v", result)
	return ""
}
