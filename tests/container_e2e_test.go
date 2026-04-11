//go:build container
// +build container

// Package tests contains Testcontainers-based tests that replace the manual
// Docker-stack regression checks from REGRESSION_TESTS.md.
//
// Run with:
//
//	go test -tags=container -timeout=10m ./tests -v
//
// Requirements: Docker Desktop (or Docker Engine) running on the host.
// On macOS, Docker Desktop must share the OS temp directory (default: /private).
// On Linux no extra configuration is needed.
package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	_ "github.com/mattn/go-sqlite3"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/net/websocket"
)

// ─── constants ───────────────────────────────────────────────────────────────

const (
	ctDefaultKey = "dev-admin-key-local-only"
	ctCustomKey  = "ct-custom-key-xyz789"
	ctPort       = "8080/tcp"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// repoRootAbs returns the absolute path of the repository root.
// Tests live in tests/, so "../" resolves correctly.
func repoRootAbs(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../")
	if err != nil {
		t.Fatalf("repoRootAbs: %v", err)
	}
	return abs
}

// startBingoServer builds the Dockerfile and starts the bingo-server container.
//
// env is merged with a minimal default set (ADMIN_API_KEY from ctDefaultKey).
// If dataDir is non-empty it is bind-mounted to /app/data inside the container
// so the SQLite database is accessible from the host after the container stops.
//
// The container is automatically terminated when the test ends.
// Returns the container handle and the base HTTP URL, e.g. "http://localhost:NNNNN".
func startBingoServer(t *testing.T, ctx context.Context, env map[string]string, dataDir string) (tc.Container, string) {
	t.Helper()

	req := tc.ContainerRequest{
		FromDockerfile: tc.FromDockerfile{
			Context:    repoRootAbs(t),
			Dockerfile: "Dockerfile",
			// Cache the image across test runs; testcontainers rebuilds
			// automatically when the build-context hash changes.
			KeepImage: true,
		},
		ExposedPorts: []string{ctPort},
		Env:          env,
		WaitingFor: wait.ForHTTP("/api/status").
			WithPort(ctPort).
			WithStatusCodeMatcher(func(status int) bool { return status < 300 }).
			WithStartupTimeout(90 * time.Second),
	}

	if dataDir != "" {
		req.HostConfigModifier = func(hc *dockercontainer.HostConfig) {
			hc.Binds = append(hc.Binds, dataDir+":/app/data")
		}
	}

	c, err := tc.GenericContainer(ctx, tc.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start bingo-server container: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := c.MappedPort(ctx, ctPort)
	if err != nil {
		t.Fatalf("container mapped port: %v", err)
	}

	return c, fmt.Sprintf("http://%s:%s", host, port.Port())
}

// adminCreateGame calls POST /admin/api/games and returns the game code.
func adminCreateGame(t *testing.T, baseURL, adminKey string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/admin/api/games", nil)
	req.Header.Set("X-Admin-Key", adminKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /admin/api/games: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /admin/api/games returned %d", resp.StatusCode)
	}

	var out struct {
		Success bool `json:"success"`
		Data    struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode create-game response: %v", err)
	}
	if out.Data.Code == "" {
		t.Fatal("create game returned empty code")
	}
	return out.Data.Code
}

// wsLogin dials the container WebSocket, sends the login message, and returns
// the connection and the server-assigned playerID.
func wsLogin(t *testing.T, baseURL, username, code string) (*websocket.Conn, string) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws"

	ws, err := websocket.Dial(wsURL, "", "http://localhost")
	if err != nil {
		t.Fatalf("[%s] dial: %v", username, err)
	}

	loginMsg := map[string]interface{}{
		"action":   "login",
		"username": username,
		"code":     code,
	}
	if err := websocket.JSON.Send(ws, loginMsg); err != nil {
		t.Fatalf("[%s] login send: %v", username, err)
	}

	var welcome map[string]interface{}
	_ = ws.SetDeadline(time.Now().Add(10 * time.Second))
	if err := websocket.JSON.Receive(ws, &welcome); err != nil {
		t.Fatalf("[%s] welcome recv: %v", username, err)
	}
	_ = ws.SetDeadline(time.Time{})

	playerID, _ := welcome["player_id"].(string)
	if playerID == "" {
		t.Fatalf("[%s] welcome missing player_id: %v", username, welcome)
	}
	t.Logf("[%s] logged in: playerID=%s", username, playerID)
	return ws, playerID
}

// containerLogs returns all container log output as a single string.
func containerLogs(t *testing.T, ctx context.Context, c tc.Container) string {
	t.Helper()
	r, err := c.Logs(ctx)
	if err != nil {
		t.Fatalf("container logs: %v", err)
	}
	defer r.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

// waitForLog polls container logs until substr is present or timeout expires.
// Returns true when the substring is found.
func waitForLog(t *testing.T, ctx context.Context, c tc.Container, substr string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(containerLogs(t, ctx, c), substr) {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// drainUntilType reads WebSocket messages until one with wantType arrives,
// or until deadline/EOF.  Returns (true, nil) on success.
func drainUntilType(ws *websocket.Conn, wantType string, timeout time.Duration) (bool, error) {
	_ = ws.SetDeadline(time.Now().Add(timeout))
	defer func() { _ = ws.SetDeadline(time.Time{}) }()
	for {
		var msg map[string]interface{}
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			return false, err
		}
		if t, _ := msg["type"].(string); t == wantType {
			return true, nil
		}
	}
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestContainerAdminKeyCustom verifies that setting ADMIN_API_KEY env var
// replaces the hardcoded default key → default key → 403, custom key → 200.
// Automates manual regression test 12.1.
func TestContainerAdminKeyCustom(t *testing.T) {
	ctx := context.Background()
	_, baseURL := startBingoServer(t, ctx, map[string]string{
		"ADMIN_API_KEY": ctCustomKey,
	}, "")

	doGet := func(key string) int {
		req, _ := http.NewRequest(http.MethodGet, baseURL+"/admin/api/games", nil)
		req.Header.Set("X-Admin-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /admin/api/games (key=%s): %v", key, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := doGet(ctDefaultKey); got != http.StatusForbidden {
		t.Errorf("default key: want 403, got %d", got)
	} else {
		t.Logf("✓ Default key rejected (403) when custom ADMIN_API_KEY is set")
	}

	if got := doGet(ctCustomKey); got != http.StatusOK {
		t.Errorf("custom key: want 200, got %d", got)
	} else {
		t.Logf("✓ Custom ADMIN_API_KEY accepted (200)")
	}
}

// TestContainerAdminKeyFallback verifies that when ADMIN_API_KEY is absent
// the server falls back to the hardcoded dev key.
// Automates manual regression test 12.4.
func TestContainerAdminKeyFallback(t *testing.T) {
	ctx := context.Background()
	_, baseURL := startBingoServer(t, ctx, map[string]string{}, "")

	req, _ := http.NewRequest(http.MethodGet, baseURL+"/admin/api/games", nil)
	req.Header.Set("X-Admin-Key", ctDefaultKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("fallback default key: want 200, got %d", resp.StatusCode)
	} else {
		t.Logf("✓ Default admin key fallback works when ADMIN_API_KEY not set (200)")
	}
}

// TestContainerSIGTERMNotifiesClients verifies that sending SIGTERM via
// docker stop causes the server to broadcast {"type":"server_shutdown"} to all
// connected players before the process exits, and logs the notification count.
// Automates manual regression tests 13.5 and 13.6.
func TestContainerSIGTERMNotifiesClients(t *testing.T) {
	ctx := context.Background()
	c, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	code := adminCreateGame(t, baseURL, ctDefaultKey)

	ws1, _ := wsLogin(t, baseURL, "Alice", code)
	ws2, _ := wsLogin(t, baseURL, "Bob", code)
	defer ws1.Close()
	defer ws2.Close()

	// Both goroutines drain messages until server_shutdown arrives.
	type listenResult struct {
		received bool
		err      error
	}
	ch1, ch2 := make(chan listenResult, 1), make(chan listenResult, 1)
	listen := func(ws *websocket.Conn, ch chan<- listenResult) {
		got, err := drainUntilType(ws, "server_shutdown", 20*time.Second)
		ch <- listenResult{got, err}
	}
	go listen(ws1, ch1)
	go listen(ws2, ch2)

	// Small pause so both connections are fully established server-side.
	time.Sleep(300 * time.Millisecond)

	// docker stop sends SIGTERM then waits for the graceful timeout.
	stopTimeout := 15 * time.Second
	if err := c.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container: %v", err)
	}

	// Collect listener results (goroutines finish once the WS is closed).
	collect := func(name string, ch <-chan listenResult) {
		select {
		case r := <-ch:
			if !r.received {
				t.Errorf("%s: server_shutdown not received (err=%v)", name, r.err)
			} else {
				t.Logf("✓ %s received server_shutdown", name)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s: timed out waiting for shutdown message", name)
		}
	}
	collect("Alice", ch1)
	collect("Bob", ch2)

	// Container logs must contain the notification count line.
	logs := containerLogs(t, ctx, c)
	if !strings.Contains(logs, "Notified 2 player(s) of server shutdown") {
		t.Errorf("expected 'Notified 2 player(s) of server shutdown' in logs\n--- logs ---\n%s", logs)
	} else {
		t.Logf("✓ Container log confirms 2 players notified on shutdown")
	}
}

// TestContainerOrphanedGame verifies that when all players disconnect without a
// winner the server logs an orphan event, and subsequent join attempts receive
// the "all players disconnected" error.
// Automates manual regression tests 13.1 and 13.2.
func TestContainerOrphanedGame(t *testing.T) {
	ctx := context.Background()
	c, baseURL := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, "")

	code := adminCreateGame(t, baseURL, ctDefaultKey)

	ws1, _ := wsLogin(t, baseURL, "Alice", code)
	ws2, _ := wsLogin(t, baseURL, "Bob", code)

	// Disconnect both clients without anyone winning.
	ws1.Close()
	ws2.Close()

	// Server detects the last disconnect asynchronously and logs the orphan event.
	if !waitForLog(t, ctx, c, "orphaned", 5*time.Second) {
		t.Errorf("orphan log line never appeared\n--- logs ---\n%s\n", containerLogs(t, ctx, c))
	} else {
		t.Logf("✓ Orphan log line appeared in container logs")
	}

	// Reconnecting with the orphaned code should produce an error message
	// containing "all players disconnected".
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws"
	wsNew, err := websocket.Dial(wsURL, "", "http://localhost")
	if err != nil {
		t.Fatalf("reconnect dial: %v", err)
	}
	defer wsNew.Close()

	loginMsg := map[string]interface{}{
		"action":   "login",
		"username": "Charlie",
		"code":     code,
	}
	if err := websocket.JSON.Send(wsNew, loginMsg); err != nil {
		t.Fatalf("reconnect login send: %v", err)
	}

	var errResp map[string]interface{}
	_ = wsNew.SetDeadline(time.Now().Add(5 * time.Second))
	if err := websocket.JSON.Receive(wsNew, &errResp); err != nil {
		t.Fatalf("reconnect response recv: %v", err)
	}

	msgType, _ := errResp["type"].(string)
	message, _ := errResp["message"].(string)
	if msgType != "error" || !strings.Contains(strings.ToLower(message), "all players disconnected") {
		t.Errorf("expected error 'all players disconnected', got type=%q message=%q", msgType, message)
	} else {
		t.Logf("✓ Reconnect to orphaned code returns correct error: %q", message)
	}
}

// TestContainerVolumeArchivePersistence verifies that a completed game is
// written to the SQLite database, the archive row survives a container restart
// (same bind-mounted data directory), and a second container starts
// successfully against the pre-existing database volume.
// Automates manual regression tests 7.1, 7.5, and the container-restart case
// of 7.6.
//
// NOTE: requires Docker Desktop to allow bind-mounts of the OS temp directory.
// On macOS this works out of the box (/private is shared by default).
func TestContainerVolumeArchivePersistence(t *testing.T) {
	dataDir := t.TempDir() // bind-mounted to /app/data
	ctx := context.Background()

	// ── Phase 1: Play a game to completion ───────────────────────────────────

	c1, baseURL1 := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	code := adminCreateGame(t, baseURL1, ctDefaultKey)
	ws1, _ := wsLogin(t, baseURL1, "Alice", code)
	ws2, _ := wsLogin(t, baseURL1, "Bob", code)

	// Alice announces the win.
	if err := websocket.JSON.Send(ws1, map[string]interface{}{"action": "win"}); err != nil {
		t.Fatalf("send win: %v", err)
	}

	// Both clients should receive game_ended.
	var wg sync.WaitGroup
	wg.Add(2)
	endedAlice := make(chan bool, 1)
	endedBob := make(chan bool, 1)
	waitEnd := func(ws *websocket.Conn, ch chan<- bool) {
		defer wg.Done()
		got, _ := drainUntilType(ws, "game_ended", 12*time.Second)
		ch <- got
	}
	go waitEnd(ws1, endedAlice)
	go waitEnd(ws2, endedBob)
	wg.Wait()

	if !<-endedAlice {
		t.Error("Alice did not receive game_ended")
	}
	if !<-endedBob {
		t.Error("Bob did not receive game_ended")
	}
	ws1.Close()
	ws2.Close()

	// Wait for the archive log line before stopping.
	if !waitForLog(t, ctx, c1, "Archived game", 5*time.Second) {
		t.Log("Warning: archive log line not seen before container stop, continuing")
	}

	stopTimeout := 10 * time.Second
	if err := c1.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container 1: %v", err)
	}

	// ── Phase 2: Verify archive row via direct SQLite read ───────────────────
	// Container is stopped; the DB file is on the host at dataDir/bingo.db.

	dbPath := filepath.Join(dataDir, "bingo.db")
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer sqlDB.Close()

	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM game_archives`).Scan(&count); err != nil {
		t.Fatalf("query game_archives count: %v", err)
	}
	if count == 0 {
		t.Error("expected ≥1 row in game_archives after game completion")
	}

	var winnerID string
	if err := sqlDB.QueryRow(`SELECT winner_id FROM game_archives LIMIT 1`).Scan(&winnerID); err != nil {
		t.Fatalf("query winner_id: %v", err)
	}
	if winnerID == "" {
		t.Error("winner_id in game_archives should not be empty")
	}
	sqlDB.Close()
	t.Logf("✓ Archive row written: winner_id=%s (%d row(s))", winnerID, count)

	// ── Phase 3: Start a second container on the same volume ─────────────────

	_, baseURL2 := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	statusResp, err := http.Get(baseURL2 + "/api/status")
	if err != nil || statusResp.StatusCode != http.StatusOK {
		t.Errorf("second container /api/status: err=%v status code=%v", err, statusResp)
	} else {
		statusResp.Body.Close()
		t.Logf("✓ Second container started healthy against pre-existing DB volume")
	}
}

// TestContainerCleanupGoroutine verifies that archive records older than 4 days
// are deleted on server startup (the cleanup goroutine runs immediately at boot).
// Automates manual regression test 7.7.
func TestContainerCleanupGoroutine(t *testing.T) {
	dataDir := t.TempDir()
	ctx := context.Background()

	// ── Phase 1: Start container to initialise the DB schema ─────────────────

	c1, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	stopTimeout := 10 * time.Second
	if err := c1.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container 1: %v", err)
	}

	// ── Phase 2: Insert a 5-day-old archive row directly from the test ───────

	dbPath := filepath.Join(dataDir, "bingo.db")
	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	fiveDaysAgo := time.Now().Add(-5 * 24 * time.Hour).Unix()
	_, err = sqlDB.Exec(
		`INSERT INTO game_archives(id, game_id, code, host_id, winner_id, player_count, created_at, ended_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		"stale-row-1", "g-stale", "BINGO-STALE", "host-st", "winner-st", 2, fiveDaysAgo, fiveDaysAgo,
	)
	sqlDB.Close()
	if err != nil {
		t.Fatalf("insert stale row: %v", err)
	}

	// ── Phase 3: Fresh container on same volume; cleanup runs at startup ──────

	c2, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	if !waitForLog(t, ctx, c2, "Cleaned up 1 old game archive", 5*time.Second) {
		t.Errorf("cleanup log line not found\n--- logs ---\n%s", containerLogs(t, ctx, c2))
	} else {
		t.Logf("✓ Startup cleanup goroutine removed the stale archive row")
	}

	// Verify the stale row is gone by reading the DB from the test side.
	// The container is still running, so open read-only to avoid lock contention.
	sqlDB2, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("reopen %s: %v", dbPath, err)
	}
	defer sqlDB2.Close()

	var remaining int
	if err := sqlDB2.QueryRow(`SELECT COUNT(*) FROM game_archives WHERE id='stale-row-1'`).Scan(&remaining); err != nil {
		t.Fatalf("count stale row: %v", err)
	}
	if remaining != 0 {
		t.Errorf("stale row still present after cleanup (count=%d)", remaining)
	} else {
		t.Logf("✓ Stale archive row confirmed deleted from game_archives")
	}
}
