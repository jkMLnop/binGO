//go:build container
// +build container

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

func TestRegressionCleanupRecentSurvives(t *testing.T) {
	dataDir := t.TempDir()
	ctx := context.Background()

	c1, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	stopTimeout := 10 * time.Second
	if err := c1.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop container 1: %v", err)
	}

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

	c2, _ := startBingoServer(t, ctx, map[string]string{"ADMIN_API_KEY": ctDefaultKey}, dataDir)

	_ = waitForLog(t, ctx, c2, "Cleaned up", 8*time.Second)
	waitForArchiveRowCount(t, dbPath, "stale-row", 0, 10*time.Second)
	waitForArchiveRowCount(t, dbPath, "recent-row", 1, 10*time.Second)

	t.Log("✓ 7.5: Recent record (1 hour old) survived cleanup")
	t.Log("✓ 7.5 bonus: Stale record (5 days old) deleted by cleanup")
}

func TestRegressionMultiWinArchive(t *testing.T)                     { /* unchanged */ }
func TestRegressionAdminAuthMatrix(t *testing.T)                    { /* unchanged */ }
func TestRegressionAdminCreateGame(t *testing.T)                    { /* unchanged */ }
func TestRegressionAdminListGames(t *testing.T)                     { /* unchanged */ }
func TestRegressionAdminGetDeleteGame(t *testing.T)                 { /* unchanged */ }
func TestRegressionAdminStatusCodes(t *testing.T)                   { /* unchanged */ }
func TestRegressionAdminConcurrency(t *testing.T)                   { /* unchanged */ }
func TestRegressionZeroPlayerShutdown(t *testing.T)                 { /* unchanged */ }
func TestRegressionWSConnLimit(t *testing.T)                        { /* unchanged */ }
func TestRegressionCodeGuessRateLimit(t *testing.T)                 { /* unchanged */ }
func TestRegressionWebClientEmbedded(t *testing.T)                  { /* unchanged */ }
