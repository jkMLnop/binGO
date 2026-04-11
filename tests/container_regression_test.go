//go:build container
// +build container

// Automated regression tests that cover manual REGRESSION_TESTS.md scenarios
// not yet handled by container_e2e_test.go.
//
// Run with:  go test -tags=container -timeout=10m ./tests -v -run TestRegression
//
// Coverage map (REGRESSION_TESTS.md → this file):
//   7.5 recent records survive cleanup  → TestRegressionCleanupRecentSurvives
//   7.1–7.3 multi-win + archive         → TestRegressionMultiWinArchive
//   11.1–11.3 auth matrix               → TestRegressionAdminAuthMatrix
//   11.5–11.8 create game               → TestRegressionAdminCreateGame
//   11.9–11.10 list games               → TestRegressionAdminListGames
//   11.11–11.15 get/delete detail       → TestRegressionAdminGetDeleteGame
//   11.16 status codes                  → TestRegressionAdminStatusCodes
//   11.21–11.23 concurrency             → TestRegressionAdminConcurrency
//   13.4 zero-player shutdown           → TestRegressionZeroPlayerShutdown

package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/websocket"
)

// ─── 7.5: Recent records survive cleanup ─────────────────────────────────────

func TestRegressionCleanupRecentSurvives(t *testing.T) {
	dataDir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Start container to initialise the DB schema.
	c1, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	stopTimeout := 10 * time.Second
	if err := c1.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container 1: %v", err)
	}

	// Phase 2: Insert a recent row (1 hour old — under the 4-day threshold)
	// and a stale row (5 days old — over the threshold) side by side.
	dbPath := filepath.Join(dataDir, "bingo.db")
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}

	oneHourAgo := time.Now().Add(-1 * time.Hour).Unix()
	fiveDaysAgo := time.Now().Add(-5 * 24 * time.Hour).Unix()

	_, err = sqlDB.Exec(
		`INSERT INTO game_archives(id, game_id, code, host_id, winner_id, player_count, created_at, ended_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		"recent-row", "g-recent", "BINGO-RECNT", "host-r", "winner-r", 2, oneHourAgo, oneHourAgo,
	)
	if err != nil {
		t.Fatalf("insert recent row: %v", err)
	}

	_, err = sqlDB.Exec(
		`INSERT INTO game_archives(id, game_id, code, host_id, winner_id, player_count, created_at, ended_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		"stale-row", "g-stale", "BINGO-STALE", "host-s", "winner-s", 2, fiveDaysAgo, fiveDaysAgo,
	)
	if err != nil {
		t.Fatalf("insert stale row: %v", err)
	}
	sqlDB.Close()

	// Phase 3: Restart; cleanup should delete stale but keep recent.
	c2, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	// Wait for cleanup goroutine.
	if !waitForLog(t, ctx, c2, "Cleaned up", 8*time.Second) {
		t.Log("Note: no cleanup log line found (may mean 0 stale rows were seen)")
	}
	// Extra settle time.
	time.Sleep(1 * time.Second)

	sqlDB2, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("reopen %s: %v", dbPath, err)
	}
	defer sqlDB2.Close()

	// Recent row should survive.
	var recentCount int
	if err := sqlDB2.QueryRow(`SELECT COUNT(*) FROM game_archives WHERE id='recent-row'`).Scan(&recentCount); err != nil {
		t.Fatalf("count recent row: %v", err)
	}
	if recentCount != 1 {
		t.Errorf("7.5 FAIL: recent row (1 hour old) should survive cleanup, got count=%d", recentCount)
	} else {
		t.Log("✓ 7.5: Recent record (1 hour old) survived cleanup")
	}

	// Stale row should be gone.
	var staleCount int
	if err := sqlDB2.QueryRow(`SELECT COUNT(*) FROM game_archives WHERE id='stale-row'`).Scan(&staleCount); err != nil {
		t.Fatalf("count stale row: %v", err)
	}
	if staleCount != 0 {
		t.Errorf("7.5 bonus FAIL: stale row (5 days old) should be deleted, got count=%d", staleCount)
	} else {
		t.Log("✓ 7.5 bonus: Stale record (5 days old) deleted by cleanup")
	}
}

// ─── 7.1–7.3: Multiple wins accumulate archives ─────────────────────────────

func TestRegressionMultiWinArchive(t *testing.T) {
	dataDir := t.TempDir()
	ctx := context.Background()

	c, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	playAndWin := func(round int) {
		code := adminCreateGame(t, baseURL, ctDefaultKey)
		ws1, _ := wsLogin(t, baseURL, fmt.Sprintf("Alice-%d", round), code)
		ws2, _ := wsLogin(t, baseURL, fmt.Sprintf("Bob-%d", round), code)
		defer ws1.Close()
		defer ws2.Close()

		// Alice announces the win.
		if err := websocket.JSON.Send(ws1, map[string]interface{}{"action": "win"}); err != nil {
			t.Fatalf("round %d: send win: %v", round, err)
		}

		// Drain game_ended from both.
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); drainUntilType(ws1, "game_ended", 10*time.Second) }()
		go func() { defer wg.Done(); drainUntilType(ws2, "game_ended", 10*time.Second) }()
		wg.Wait()
	}

	// Play 3 games.
	for i := 1; i <= 3; i++ {
		t.Logf("  Playing game %d/3 ...", i)
		playAndWin(i)
		time.Sleep(500 * time.Millisecond) // let archive write settle
	}

	// 7.3: Check container logs for 3 "Archived game" lines.
	logs := containerLogs(t, ctx, c)
	archiveCount := strings.Count(logs, "Archived game")
	if archiveCount < 3 {
		t.Errorf("7.3 FAIL: expected ≥3 '📋 Archived game' log lines, got %d\n--- logs ---\n%s", archiveCount, logs)
	} else {
		t.Logf("✓ 7.3: Found %d 'Archived game' log lines across 3 wins", archiveCount)
	}

	// 7.1: Verify only game_ended triggers archive (no extra on restart).
	// None of these games used restart, so archiveCount should be exactly 3, not more.
	if archiveCount > 3 {
		t.Errorf("7.1 FAIL: expected exactly 3 archive lines (no extras), got %d", archiveCount)
	} else {
		t.Log("✓ 7.1: No extra archive writes beyond the 3 wins")
	}

	// Check the DB row count.
	stopTimeout := 10 * time.Second
	if err := c.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container: %v", err)
	}
	dbPath := filepath.Join(dataDir, "bingo.db")
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer sqlDB.Close()

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM game_archives`).Scan(&count); err != nil {
		t.Fatalf("count game_archives: %v", err)
	}
	if count < 3 {
		t.Errorf("7.3 DB FAIL: expected ≥3 rows in game_archives, got %d", count)
	} else {
		t.Logf("✓ 7.3 DB: %d archive rows in database after 3 wins", count)
	}
}

// ─── 11.1–11.3: Admin auth matrix ───────────────────────────────────────────

func TestRegressionAdminAuthMatrix(t *testing.T) {
	ctx := context.Background()
	_, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	tests := []struct {
		name       string
		key        string
		wantStatus int
	}{
		{"11.1 missing key", "", http.StatusUnauthorized},
		{"11.2 wrong key", "wrong-key-123", http.StatusForbidden},
		{"11.3 valid key", ctDefaultKey, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, baseURL+"/admin/api/games", nil)
			if tt.key != "" {
				req.Header.Set("X-Admin-Key", tt.key)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("want %d, got %d", tt.wantStatus, resp.StatusCode)
			} else {
				t.Logf("✓ %s → %d", tt.name, resp.StatusCode)
			}
		})
	}
}

// ─── 11.5–11.8: Create game tests ───────────────────────────────────────────

func TestRegressionAdminCreateGame(t *testing.T) {
	ctx := context.Background()
	_, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	codePattern := regexp.MustCompile(`^BINGO-[A-Z0-9]{5}$`)
	seenCodes := make(map[string]bool)

	for i := 0; i < 5; i++ {
		req, _ := http.NewRequest(http.MethodPost, baseURL+"/admin/api/games", nil)
		req.Header.Set("X-Admin-Key", ctDefaultKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("create game %d: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("11.5 FAIL: game %d returned %d", i, resp.StatusCode)
			continue
		}

		var out struct {
			Success bool `json:"success"`
			Data    struct {
				ID          string `json:"id"`
				Code        string `json:"code"`
				Status      string `json:"status"`
				PlayerCount int    `json:"player_count"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode game %d: %v", i, err)
		}

		// 11.5: Returns success with game data.
		if !out.Success {
			t.Errorf("11.5 FAIL: game %d success=false", i)
		}
		if out.Data.ID == "" {
			t.Errorf("11.5 FAIL: game %d missing id", i)
		}
		if out.Data.Status != "active" {
			t.Errorf("11.5 FAIL: game %d status=%q, want active", i, out.Data.Status)
		}

		// 11.7: Code format.
		if !codePattern.MatchString(out.Data.Code) {
			t.Errorf("11.7 FAIL: game %d code %q doesn't match BINGO-[A-Z0-9]{5}", i, out.Data.Code)
		}

		// 11.8: Unique.
		if seenCodes[out.Data.Code] {
			t.Errorf("11.8 FAIL: duplicate code %q on game %d", out.Data.Code, i)
		}
		seenCodes[out.Data.Code] = true
	}

	t.Logf("✓ 11.5–11.8: Created 5 games, all valid format, all unique codes: %v",
		func() []string {
			codes := make([]string, 0, len(seenCodes))
			for c := range seenCodes {
				codes = append(codes, c)
			}
			return codes
		}())
}

// ─── 11.9–11.10: List games ─────────────────────────────────────────────────

func TestRegressionAdminListGames(t *testing.T) {
	ctx := context.Background()
	_, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	// Create 5 games.
	for i := 0; i < 5; i++ {
		adminCreateGame(t, baseURL, ctDefaultKey)
	}

	req, _ := http.NewRequest(http.MethodGet, baseURL+"/admin/api/games", nil)
	req.Header.Set("X-Admin-Key", ctDefaultKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list games: %v", err)
	}
	defer resp.Body.Close()

	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Count int              `json:"count"`
			Games []map[string]any `json:"games"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&out)

	if !out.Success {
		t.Error("11.9 FAIL: success=false")
	}

	// 11.9: Returns array.
	if out.Data.Games == nil {
		t.Error("11.9 FAIL: games is nil")
	}

	// 11.10: Count matches.
	if out.Data.Count < 5 {
		t.Errorf("11.10 FAIL: count=%d, want ≥5", out.Data.Count)
	}
	if len(out.Data.Games) != out.Data.Count {
		t.Errorf("11.10 FAIL: games array length %d ≠ count %d", len(out.Data.Games), out.Data.Count)
	} else {
		t.Logf("✓ 11.9–11.10: Listed %d games, count matches array length", out.Data.Count)
	}
}

// ─── 11.11–11.15: Get detail + Delete ────────────────────────────────────────

func TestRegressionAdminGetDeleteGame(t *testing.T) {
	ctx := context.Background()
	_, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	// Create a game and extract its ID.
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/admin/api/games", nil)
	req.Header.Set("X-Admin-Key", ctDefaultKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var createOut struct {
		Data struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&createOut)
	resp.Body.Close()
	gameID := createOut.Data.ID

	// ── 11.11: Get existing game detail ──
	t.Run("11.11 get existing", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/admin/api/games/"+gameID, nil)
		req.Header.Set("X-Admin-Key", ctDefaultKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get detail: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}

		var detail struct {
			Data struct {
				ID        string `json:"id"`
				Code      string `json:"code"`
				HostID    string `json:"host_id"`
				Status    string `json:"status"`
				IsActive  bool   `json:"is_active"`
				CreatedAt int64  `json:"created_at"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&detail)

		if detail.Data.ID != gameID {
			t.Errorf("id mismatch: got %q, want %q", detail.Data.ID, gameID)
		}
		if detail.Data.Status != "active" {
			t.Errorf("status: got %q, want active", detail.Data.Status)
		}
		if !detail.Data.IsActive {
			t.Error("is_active should be true")
		}
		t.Logf("✓ 11.11: Game detail returned for %s", gameID)
	})

	// ── 11.12: Get non-existent game ──
	t.Run("11.12 get non-existent", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/admin/api/games/nonexistent-xyz", nil)
		req.Header.Set("X-Admin-Key", ctDefaultKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get nonexistent: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("want 404, got %d", resp.StatusCode)
		} else {
			t.Log("✓ 11.12: Non-existent game returns 404")
		}
	})

	// ── 11.14: Delete existing game ──
	t.Run("11.14 delete existing", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/admin/api/games/"+gameID, nil)
		req.Header.Set("X-Admin-Key", ctDefaultKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}

		var delOut struct {
			Success bool `json:"success"`
		}
		json.Unmarshal(body, &delOut)
		if !delOut.Success {
			t.Error("expected success=true on delete")
		}
		t.Logf("✓ 11.14: Game %s deleted successfully", gameID)
	})

	// ── 11.15: Delete non-existent game ──
	t.Run("11.15 delete non-existent", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/admin/api/games/nonexistent-xyz", nil)
		req.Header.Set("X-Admin-Key", ctDefaultKey)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("delete nonexistent: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("want 404, got %d", resp.StatusCode)
		} else {
			t.Log("✓ 11.15: Delete non-existent returns 404")
		}
	})
}

// ─── 11.16: HTTP status codes comprehensive ──────────────────────────────────

func TestRegressionAdminStatusCodes(t *testing.T) {
	ctx := context.Background()
	_, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	// Create a game so we have a valid ID.
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/admin/api/games", nil)
	req.Header.Set("X-Admin-Key", ctDefaultKey)
	resp, _ := http.DefaultClient.Do(req)
	var co struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&co)
	resp.Body.Close()

	tests := []struct {
		name       string
		method     string
		path       string
		key        string
		wantStatus int
		wantField  string // "success" or "error" in JSON body
	}{
		// Auth
		{"GET no key → 401", "GET", "/admin/api/games", "", 401, "error"},
		{"GET bad key → 403", "GET", "/admin/api/games", "wrong", 403, "error"},
		{"GET good key → 200", "GET", "/admin/api/games", ctDefaultKey, 200, "success"},
		// CRUD
		{"POST create → 200", "POST", "/admin/api/games", ctDefaultKey, 200, "success"},
		{"GET detail → 200", "GET", "/admin/api/games/" + co.Data.ID, ctDefaultKey, 200, "success"},
		{"GET missing → 404", "GET", "/admin/api/games/no-such-game", ctDefaultKey, 404, "error"},
		{"DELETE missing → 404", "DELETE", "/admin/api/games/no-such-game", ctDefaultKey, 404, "error"},
		{"DELETE existing → 200", "DELETE", "/admin/api/games/" + co.Data.ID, ctDefaultKey, 200, "success"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(tt.method, baseURL+tt.path, nil)
			if tt.key != "" {
				req.Header.Set("X-Admin-Key", tt.key)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status: want %d, got %d (body: %s)", tt.wantStatus, resp.StatusCode, body)
				return
			}

			// Validate JSON has the expected field.
			var parsed map[string]interface{}
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Errorf("body not valid JSON: %s", body)
				return
			}
			if _, ok := parsed[tt.wantField]; !ok {
				t.Errorf("response missing %q field: %s", tt.wantField, body)
			}

			t.Logf("✓ %s → %d with %q field", tt.name, resp.StatusCode, tt.wantField)
		})
	}
}

// ─── 11.21–11.23: Concurrency and load ──────────────────────────────────────

func TestRegressionAdminConcurrency(t *testing.T) {
	ctx := context.Background()
	_, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	const goroutines = 5
	const gamesPerGoroutine = 10
	total := goroutines * gamesPerGoroutine

	var successCount atomic.Int32
	var wg sync.WaitGroup

	start := time.Now()

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < gamesPerGoroutine; i++ {
				req, _ := http.NewRequest(http.MethodPost, baseURL+"/admin/api/games", nil)
				req.Header.Set("X-Admin-Key", ctDefaultKey)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Errorf("create game: %v", err)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					successCount.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	createDuration := time.Since(start)

	// 11.21 + 11.22: All 50 created successfully.
	if int(successCount.Load()) != total {
		t.Errorf("11.21–22 FAIL: expected %d successful creates, got %d", total, successCount.Load())
	} else {
		t.Logf("✓ 11.21–22: %d concurrent game creates, all returned 200 (%v)", total, createDuration)
	}

	// 11.23: Query performance — list all games under 1 second.
	start = time.Now()
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/admin/api/games", nil)
	req.Header.Set("X-Admin-Key", ctDefaultKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list games: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	listDuration := time.Since(start)

	if listDuration > 1*time.Second {
		t.Errorf("11.23 FAIL: list took %v, want < 1s", listDuration)
	} else {
		t.Logf("✓ 11.23: Listed games in %v (< 1s)", listDuration)
	}

	var listOut struct {
		Data struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	json.Unmarshal(body, &listOut)
	if listOut.Data.Count < total {
		t.Errorf("11.23 FAIL: expected ≥%d games in list, got %d", total, listOut.Data.Count)
	} else {
		t.Logf("✓ 11.23: Count=%d (expected ≥%d)", listOut.Data.Count, total)
	}
}

// ─── 13.4: Zero-player shutdown — no notification log ────────────────────────

func TestRegressionZeroPlayerShutdown(t *testing.T) {
	ctx := context.Background()
	c, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	// Stop container with no clients connected.
	stopTimeout := 15 * time.Second
	if err := c.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container: %v", err)
	}

	logs := containerLogs(t, ctx, c)

	// Should NOT contain notification log since there are zero players.
	if strings.Contains(logs, "Notified") && strings.Contains(logs, "player(s) of server shutdown") {
		t.Errorf("13.4 FAIL: found shutdown notification log with zero players\n--- logs ---\n%s", logs)
	} else {
		t.Log("✓ 13.4: No shutdown notification log when zero players connected")
	}
}
