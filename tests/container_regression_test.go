//go:build container
// +build container

package tests

import (
	"context"
	"fmt"
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

	// Phase 2: Insert a recent row (2 hours old — under the 4-day threshold)
	// and a stale row (5 days old — over the threshold) via sqlite3 inside the
	// running container. We exec into the container rather than writing to the
	// SQLite file from the host because bind-mounted files may have different
	// ownership on CI runners (container runs as root; host is non-root).

	// Use a timestamp well inside the safe window: 2 hours old is comfortably
	// within the 4-day TTL and far from any boundary even with clock skew.
	twoHoursAgo := time.Now().Add(-2 * time.Hour).Unix()
	fiveDaysAgo := time.Now().Add(-5 * 24 * time.Hour).Unix()

	execSQLInContainer(t, ctx, c1, "/app/data/bingo.db",
		fmt.Sprintf(`INSERT INTO game_archives(id, game_id, code, host_id, winner_id, player_count, created_at, ended_at)
		 VALUES ('recent-row','g-recent','BINGO-RECNT','host-r','winner-r',2,%d,%d)`, twoHoursAgo, twoHoursAgo))

	execSQLInContainer(t, ctx, c1, "/app/data/bingo.db",
		fmt.Sprintf(`INSERT INTO game_archives(id, game_id, code, host_id, winner_id, player_count, created_at, ended_at)
		 VALUES ('stale-row','g-stale','BINGO-STALE','host-s','winner-s',2,%d,%d)`, fiveDaysAgo, fiveDaysAgo))

	stopTimeout := 10 * time.Second
	if err := c1.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container 1: %v", err)
	}

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
