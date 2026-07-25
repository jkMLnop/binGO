//go:build container
// +build container

package tests

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestRegressionCleanupRecentSurvives(t *testing.T) {
	dataDir := t.TempDir()
	ctx := context.Background()

	// Pre-create the DB file owned by the test process so that after the
	// container (which runs as root) stops, we retain write access to insert
	// test rows without hitting "attempt to write a readonly database".
	dbPath := filepath.Join(dataDir, "bingo.db")
	if f, err := os.Create(dbPath); err != nil {
		t.Fatalf("pre-create bingo.db: %v", err)
	} else {
		f.Close()
	}

	c1, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	stopTimeout := 10 * time.Second
	if err := c1.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container 1: %v", err)
	}

	sqlDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}

	// Use a timestamp well inside the safe window: 2 hours old is comfortably
	// within the 4-day TTL and far from any boundary even with clock skew.
	twoHoursAgo := time.Now().Add(-2 * time.Hour).Unix()
	fiveDaysAgo := time.Now().Add(-5 * 24 * time.Hour).Unix()

	_, err = sqlDB.Exec(
		`INSERT INTO game_archives(id, game_id, code, host_id, winner_id, player_count, created_at, ended_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		"recent-row", "g-recent", "BINGO-RECNT", "host-r", "winner-r", 2, twoHoursAgo, twoHoursAgo,
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

	c2, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	_ = waitForLog(t, ctx, c2, "Cleaned up", 15*time.Second)
	waitForArchiveRowCount(t, dbPath, "stale-row", 0, 20*time.Second)
	waitForArchiveRowCount(t, dbPath, "recent-row", 1, 20*time.Second)

	t.Log("✓ 7.5: Recent record (2 hours old) survived cleanup")
	t.Log("✓ 7.5 bonus: Stale record (5 days old) deleted by cleanup")
}

func TestRegressionMultiWinArchive(t *testing.T)    { /* unchanged */ }
func TestRegressionAdminAuthMatrix(t *testing.T)    { /* unchanged */ }
func TestRegressionAdminCreateGame(t *testing.T)    { /* unchanged */ }
func TestRegressionAdminListGames(t *testing.T)     { /* unchanged */ }
func TestRegressionAdminGetDeleteGame(t *testing.T) { /* unchanged */ }
func TestRegressionAdminStatusCodes(t *testing.T)   { /* unchanged */ }
func TestRegressionAdminConcurrency(t *testing.T)   { /* unchanged */ }
func TestRegressionZeroPlayerShutdown(t *testing.T) { /* unchanged */ }
func TestRegressionWSConnLimit(t *testing.T)        { /* unchanged */ }
func TestRegressionCodeGuessRateLimit(t *testing.T) { /* unchanged */ }
func TestRegressionWebClientEmbedded(t *testing.T)  { /* unchanged */ }
