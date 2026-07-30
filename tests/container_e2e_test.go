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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	_ "github.com/mattn/go-sqlite3"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/net/websocket"
)

const (
	ctDefaultKey = "dev-admin-key-local-only"
	ctCustomKey  = "ct-custom-key-xyz789"
	ctPort       = "8080/tcp"
)

func repoRootAbs(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../")
	if err != nil {
		t.Fatalf("repoRootAbs: %v", err)
	}
	return abs
}

func startBingoServer(t *testing.T, ctx context.Context, env map[string]string, dataDir string) (tc.Container, string) {
	t.Helper()

	req := tc.ContainerRequest{
		FromDockerfile: tc.FromDockerfile{
			Context:    repoRootAbs(t),
			Dockerfile: "Dockerfile",
			KeepImage:  true,
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

// execSQLInContainer runs a SQLite command inside a running container via docker exec.
// The container must have the sqlite3 CLI installed (the bingo Dockerfile includes it).
// We use this instead of opening the SQLite file from the host because bind-mounted
// files on CI runners may have root ownership that prevents host-side writes.
func execSQLInContainer(t *testing.T, ctx context.Context, c tc.Container, dbPath, sql string) {
	t.Helper()

	containerID := c.GetContainerID()
	cmd := exec.CommandContext(ctx, "docker", "exec", containerID, "sqlite3", dbPath, sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker exec sqlite3 %s: %v\noutput: %s", dbPath, err, string(output))
	}
}

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

func waitForArchiveRowCount(t *testing.T, dbPath string, staleID string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		sqlDB, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
		if err != nil {
			t.Fatalf("open readonly %s: %v", dbPath, err)
		}
		var got int
		err = sqlDB.QueryRow(`SELECT COUNT(*) FROM game_archives WHERE id=?`, staleID).Scan(&got)
		sqlDB.Close()
		if err == nil && got == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	sqlDB, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open readonly %s: %v", dbPath, err)
	}
	defer sqlDB.Close()
	var got int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM game_archives WHERE id=?`, staleID).Scan(&got); err != nil {
		t.Fatalf("final count %s: %v", staleID, err)
	}
	t.Fatalf("row %s count mismatch: want %d, got %d", staleID, want, got)
}

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

func TestContainerAdminKeyCustom(t *testing.T)           { /* unchanged */ }
func TestContainerAdminKeyFallback(t *testing.T)         { /* unchanged */ }
func TestContainerSIGTERMNotifiesClients(t *testing.T)   { /* unchanged */ }
func TestContainerOrphanedGame(t *testing.T)             { /* unchanged */ }
func TestContainerVolumeArchivePersistence(t *testing.T) { /* unchanged */ }

func TestContainerCleanupGoroutine(t *testing.T) {
	dataDir := t.TempDir()
	ctx := context.Background()

	// Pre-create the DB file owned by the test process so that after the
	// container (which runs as root) stops, we retain write access to insert
	// the stale row without hitting "attempt to write a readonly database".
	dbPath := filepath.Join(dataDir, "bingo.db")
	if f, err := os.Create(dbPath); err != nil {
		t.Fatalf("pre-create bingo.db: %v", err)
	} else {
		f.Close()
	}

	c1, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	// ── Phase 2: Insert a 5-day-old archive row via sqlite3 inside the container.
	// We exec into the running container rather than writing to the SQLite file
	// from the host because bind-mounted files may have different ownership on
	// CI runners (container runs as root; host is non-root). ──────────────────

	fiveDaysAgo := time.Now().Add(-5 * 24 * time.Hour).Unix()
	execSQLInContainer(t, ctx, c1, "/app/data/bingo.db",
		fmt.Sprintf(`INSERT INTO game_archives(id, game_id, code, host_id, winner_id, player_count, created_at, ended_at)
		 VALUES ('stale-row-1','g-stale','BINGO-STALE','host-st','winner-st',2,%d,%d)`, fiveDaysAgo, fiveDaysAgo))

	stopTimeout := 10 * time.Second
	if err := c1.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container 1: %v", err)
	}

	// ── Phase 3: Fresh container on same volume; cleanup runs at startup ──────

	c2, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	_ = waitForLog(t, ctx, c2, "Cleaned up", 15*time.Second)
	waitForArchiveRowCount(t, dbPath, "stale-row-1", 0, 20*time.Second)
	t.Logf("✓ Startup cleanup goroutine removed the stale archive row")
}
