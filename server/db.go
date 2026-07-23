package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jkMLnop/binGO/db"
)

// DBConfig wraps database configuration
type DBConfig struct {
	Enabled bool   // Enable database persistence
	Path    string // Path to SQLite database file
	Store   db.GameStore
}

// NewDBConfig creates a new database configuration
func NewDBConfig(path string) (*DBConfig, error) {
	initCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := db.NewSQLiteStore(initCtx, path)
	if err != nil {
		return nil, fmt.Errorf("failed to create database store: %w", err)
	}

	if err := store.Init(initCtx); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return &DBConfig{
		Enabled: true,
		Path:    path,
		Store:   store,
	}, nil
}

// Close closes the database connection
func (cfg *DBConfig) Close(ctx context.Context) error {
	if cfg == nil || cfg.Store == nil {
		return nil
	}
	return cfg.Store.Close(ctx)
}

// SaveGameToDB persists a game session to the database
func SaveGameToDB(ctx context.Context, store db.GameStore, game *Game, buzzwords [][]string, title string) error {
	if store == nil {
		return nil // DB not enabled
	}

	// Convert buzzwords to JSON
	buzzwordData, err := json.Marshal(buzzwords)
	if err != nil {
		return fmt.Errorf("failed to marshal buzzwords: %w", err)
	}

	// If game already exists in DB, update it; otherwise create it
	existingGame, err := store.GetGameByCode(ctx, game.Code)
	if err == nil && existingGame != nil {
		// Game exists, update status
		return store.UpdateGameStatus(ctx, existingGame.ID, "active")
	}

	// Create new game in DB
	gameID, err := store.CreateGame(ctx, game.Code, game.HostID, title, json.RawMessage(buzzwordData))
	if err != nil {
		return fmt.Errorf("failed to save game to DB: %w", err)
	}

	log.Printf("Game created in DB: code=%s, id=%s", game.Code, gameID)
	return nil
}

// RecordPlayerInDB tracks a player joining a game
func RecordPlayerInDB(ctx context.Context, store db.GameStore, gameID string, username string, ipAddress string, isHost bool) (string, error) {
	if store == nil {
		return "", nil // DB not enabled
	}

	playerID, err := store.AddPlayer(ctx, gameID, username, ipAddress, isHost)
	if err != nil {
		return "", fmt.Errorf("failed to record player in DB: %w", err)
	}

	log.Printf("Player recorded in DB: username=%s, gameID=%s, playerID=%s", username, gameID, playerID)
	return playerID, nil
}

// RecordWinInDB records a game win in the database.
// roomCode is empty for standalone games (not in a room).
func RecordWinInDB(ctx context.Context, store db.GameStore, game *Game, winnerUsername string, roomCode string) error {
	if store == nil {
		return nil // DB not enabled
	}

	// Record in wins_history
	if err := store.RecordWin(ctx, winnerUsername, game.Code, roomCode); err != nil {
		return fmt.Errorf("failed to record win in DB: %w", err)
	}

	// Update game winner
	if err := store.UpdateGameWinner(ctx, game.ID, game.Winner); err != nil {
		log.Printf("Warning: failed to update game winner in DB: %v", err)
	}

	log.Printf("Win recorded in DB: username=%s, gameCode=%s, roomCode=%s", winnerUsername, game.Code, roomCode)
	return nil
}

// ArchiveGameInDB persists a completed game session to the game_archives table
func ArchiveGameInDB(ctx context.Context, store db.GameStore, game *Game) error {
	if store == nil {
		return nil // DB not enabled
	}

	endedAt := game.EndedAt
	if endedAt.IsZero() {
		endedAt = time.Now()
	}

	if err := store.ArchiveGame(ctx, game.ID, game.Code, game.HostID, game.Winner, game.PlayerCount(), game.CreatedAt, endedAt); err != nil {
		return fmt.Errorf("failed to archive game in DB: %w", err)
	}

	log.Printf("Game archived in DB: id=%s, code=%s, winner=%s", game.ID, game.Code, game.Winner)
	return nil
}

// HostProfileFromDB retrieves a host's approved buzzwords
func HostProfileFromDB(ctx context.Context, store db.GameStore, hostID string) (*db.Host, error) {
	if store == nil {
		return nil, nil // DB not enabled
	}

	host, err := store.GetHost(ctx, hostID)
	if err != nil {
		// Not found is OK - first time this host is creating a game
		return nil, nil
	}

	return host, nil
}

// SaveHostProfileToDB creates or updates a host profile with approved buzzwords
func SaveHostProfileToDB(ctx context.Context, store db.GameStore, hostID string, username string, buzzwords []string) error {
	if store == nil {
		return nil // DB not enabled
	}

	buzzwordData, err := json.Marshal(buzzwords)
	if err != nil {
		return fmt.Errorf("failed to marshal buzzwords: %w", err)
	}

	if err := store.CreateOrUpdateHost(ctx, hostID, username, json.RawMessage(buzzwordData)); err != nil {
		return fmt.Errorf("failed to save host profile to DB: %w", err)
	}

	log.Printf("Host profile saved in DB: hostID=%s, username=%s", hostID, username)
	return nil
}
