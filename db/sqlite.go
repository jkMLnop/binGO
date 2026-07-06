package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStore implements GameStore using SQLite
type SQLiteStore struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewSQLiteStore creates a new SQLite-backed GameStore
func NewSQLiteStore(ctx context.Context, dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection (context-aware so callers can apply a deadline)
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Init creates the database schema and runs any needed column migrations.
func (s *SQLiteStore) Init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Phase 1: base schema.
	// CREATE TABLE IF NOT EXISTS is a no-op when the table already exists,
	// so columns added in later phases (e.g. room_code) won't appear on old
	// databases.  Those are handled in the migration step below.
	// Note: idx_wins_room_code is intentionally absent here; it is created
	// after the migration that adds the column.
	schema := `
	CREATE TABLE IF NOT EXISTS hosts (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		approved_buzzwords JSON,
		created_at INTEGER NOT NULL,
		last_modified_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS rooms (
		id TEXT PRIMARY KEY,
		code TEXT UNIQUE NOT NULL,
		host_id TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_rooms_code ON rooms(code);

	CREATE TABLE IF NOT EXISTS games (
		id TEXT PRIMARY KEY,
		code TEXT UNIQUE NOT NULL,
		host_id TEXT NOT NULL,
		room_code TEXT,
		status TEXT NOT NULL DEFAULT 'active',
		buzzwords JSON,
		winner_id TEXT,
		created_at INTEGER NOT NULL,
		ended_at INTEGER,
		expires_at INTEGER NOT NULL,
		FOREIGN KEY (host_id) REFERENCES hosts(id),
		FOREIGN KEY (room_code) REFERENCES rooms(code)
	);

	CREATE TABLE IF NOT EXISTS players (
		id TEXT PRIMARY KEY,
		game_id TEXT NOT NULL,
		username TEXT NOT NULL,
		ip_address TEXT NOT NULL,
		is_host INTEGER NOT NULL DEFAULT 0,
		joined_at INTEGER NOT NULL,
		left_at INTEGER,
		FOREIGN KEY (game_id) REFERENCES games(id)
	);

	CREATE TABLE IF NOT EXISTS wins_history (
		id TEXT PRIMARY KEY,
		player_username TEXT NOT NULL,
		game_code TEXT NOT NULL,
		room_code TEXT,
		counted_in_global INTEGER NOT NULL DEFAULT 1,
		won_at INTEGER NOT NULL,
		FOREIGN KEY (room_code) REFERENCES rooms(code)
	);

	CREATE TABLE IF NOT EXISTS room_buzzwords (
		room_code TEXT PRIMARY KEY,
		words JSON NOT NULL,
		uploaded_by TEXT NOT NULL,
		uploaded_at INTEGER NOT NULL,
		FOREIGN KEY (room_code) REFERENCES rooms(code)
	);

	CREATE TABLE IF NOT EXISTS llm_feedback (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		game_code TEXT,
		topic TEXT,
		source_url TEXT,
		set_label TEXT,
		generation_mode TEXT,
		total_words INTEGER NOT NULL DEFAULT 0,
		included_words JSON,
		excluded_words JSON,
		submitted_by TEXT,
		submitted_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_games_code ON games(code);
	CREATE INDEX IF NOT EXISTS idx_games_host_id ON games(host_id);
	CREATE INDEX IF NOT EXISTS idx_games_expires_at ON games(expires_at);
	CREATE INDEX IF NOT EXISTS idx_players_game_id ON players(game_id);
	CREATE INDEX IF NOT EXISTS idx_hosts_username ON hosts(username);
	CREATE INDEX IF NOT EXISTS idx_wins_player_username ON wins_history(player_username);
	CREATE INDEX IF NOT EXISTS idx_llm_feedback_game_code ON llm_feedback(game_code);
	CREATE INDEX IF NOT EXISTS idx_llm_feedback_topic ON llm_feedback(topic);
	CREATE INDEX IF NOT EXISTS idx_llm_feedback_submitted_at ON llm_feedback(submitted_at);

	CREATE TABLE IF NOT EXISTS game_archives (
		id TEXT PRIMARY KEY,
		game_id TEXT NOT NULL,
		code TEXT NOT NULL,
		host_id TEXT NOT NULL,
		winner_id TEXT NOT NULL,
		player_count INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		ended_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_game_archives_ended_at ON game_archives(ended_at);
	`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Phase 2: column migrations for databases created before Phase 11.
	// ALTER TABLE ... ADD COLUMN is idempotent here because addColumnIfNotExists
	// checks pragma_table_info first.
	if err := s.addColumnIfNotExists(ctx, "games", "room_code", "TEXT"); err != nil {
		return fmt.Errorf("failed to migrate games.room_code: %w", err)
	}
	if err := s.addColumnIfNotExists(ctx, "wins_history", "room_code", "TEXT"); err != nil {
		return fmt.Errorf("failed to migrate wins_history.room_code: %w", err)
	}
	if err := s.addColumnIfNotExists(ctx, "wins_history", "counted_in_global", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return fmt.Errorf("failed to migrate wins_history.counted_in_global: %w", err)
	}
	if err := s.addColumnIfNotExists(ctx, "llm_feedback", "generation_mode", "TEXT"); err != nil {
		return fmt.Errorf("failed to migrate llm_feedback.generation_mode: %w", err)
	}

	// Phase 13.1: linked room code for side-bet / linked rooms.
	if err := s.addColumnIfNotExists(ctx, "rooms", "linked_room_code", "TEXT"); err != nil {
		return fmt.Errorf("failed to migrate rooms.linked_room_code: %w", err)
	}

	// Phase 3: indexes that depend on migrated columns.
	if _, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_wins_room_code ON wins_history(room_code)`); err != nil {
		return fmt.Errorf("failed to create idx_wins_room_code: %w", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_rooms_linked_room_code ON rooms(linked_room_code)`); err != nil {
		return fmt.Errorf("failed to create idx_rooms_linked_room_code: %w", err)
	}

	return nil
}

// addColumnIfNotExists adds a column to a table only when it is absent,
// avoiding "duplicate column name" errors on databases that are already
// up to date.
func (s *SQLiteStore) addColumnIfNotExists(ctx context.Context, table, column, typ string) error {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`,
		table, column,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("checking column %s.%s: %w", table, column, err)
	}
	if count == 0 {
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, typ),
		); err != nil {
			return fmt.Errorf("adding column %s.%s: %w", table, column, err)
		}
	}
	return nil
}

// Close closes the database connection
func (s *SQLiteStore) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// CreateGame creates a new game record
func (s *SQLiteStore) CreateGame(ctx context.Context, code string, hostID string, buzzwords json.RawMessage) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	gameID := generateID()
	now := time.Now().Unix()
	expiresAt := now + (4 * 24 * 3600) // 4 days

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO games (id, code, host_id, status, buzzwords, created_at, expires_at)
		 VALUES (?, ?, ?, 'active', ?, ?, ?)`,
		gameID, code, hostID, buzzwords, now, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create game: %w", err)
	}

	return gameID, nil
}

// GetGameByCode retrieves a game by its code
func (s *SQLiteStore) GetGameByCode(ctx context.Context, code string) (*Game, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	game := &Game{}
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, code, host_id, room_code, status, buzzwords, winner_id, created_at, ended_at, expires_at
		 FROM games WHERE code = ?`,
		code,
	).Scan(&game.ID, &game.Code, &game.HostID, &game.RoomCode, &game.Status, &game.Buzzwords, &game.WinnerID,
		&game.CreatedAt, &game.EndedAt, &game.ExpiresAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("game not found: %w", sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query game: %w", err)
	}

	return game, nil
}

// GetGameByID retrieves a game by its ID
func (s *SQLiteStore) GetGameByID(ctx context.Context, gameID string) (*Game, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	game := &Game{}
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, code, host_id, room_code, status, buzzwords, winner_id, created_at, ended_at, expires_at
		 FROM games WHERE id = ?`,
		gameID,
	).Scan(&game.ID, &game.Code, &game.HostID, &game.RoomCode, &game.Status, &game.Buzzwords, &game.WinnerID,
		&game.CreatedAt, &game.EndedAt, &game.ExpiresAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("game not found: %w", sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query game: %w", err)
	}

	return game, nil
}

// UpdateGameStatus updates a game's status
func (s *SQLiteStore) UpdateGameStatus(ctx context.Context, gameID string, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(
		ctx,
		`UPDATE games SET status = ? WHERE id = ?`,
		status, gameID,
	)
	if err != nil {
		return fmt.Errorf("failed to update game status: %w", err)
	}
	return nil
}

// UpdateGameWinner updates the winner of a game
func (s *SQLiteStore) UpdateGameWinner(ctx context.Context, gameID string, winnerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE games SET winner_id = ?, status = 'ended', ended_at = ? WHERE id = ?`,
		winnerID, now, gameID,
	)
	if err != nil {
		return fmt.Errorf("failed to update game winner: %w", err)
	}
	return nil
}

// UpdateGameBuzzwords updates a game's buzzword list
func (s *SQLiteStore) UpdateGameBuzzwords(ctx context.Context, gameID string, buzzwords json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(
		ctx,
		`UPDATE games SET buzzwords = ? WHERE id = ?`,
		buzzwords, gameID,
	)
	if err != nil {
		return fmt.Errorf("failed to update game buzzwords: %w", err)
	}
	return nil
}

// DeleteGame deletes a game record
func (s *SQLiteStore) DeleteGame(ctx context.Context, gameID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `DELETE FROM games WHERE id = ?`, gameID)
	if err != nil {
		return fmt.Errorf("failed to delete game: %w", err)
	}
	return nil
}

// CleanupExpiredGames deletes games older than 4 days
func (s *SQLiteStore) CleanupExpiredGames(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	result, err := s.db.ExecContext(
		ctx,
		`DELETE FROM games WHERE expires_at < ?`,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup games: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(affected), nil
}

// AddPlayer adds a player to a game
func (s *SQLiteStore) AddPlayer(ctx context.Context, gameID string, username string, ipAddress string, isHost bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	playerID := generateID()
	now := time.Now().Unix()
	isHostInt := 0
	if isHost {
		isHostInt = 1
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO players (id, game_id, username, ip_address, is_host, joined_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		playerID, gameID, username, ipAddress, isHostInt, now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to add player: %w", err)
	}

	return playerID, nil
}

// GetPlayersInGame retrieves all players in a game
func (s *SQLiteStore) GetPlayersInGame(ctx context.Context, gameID string) ([]*Player, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, game_id, username, ip_address, is_host, joined_at, left_at FROM players WHERE game_id = ?`,
		gameID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query players: %w", err)
	}
	defer rows.Close()

	var players []*Player
	for rows.Next() {
		player := &Player{}
		var isHost int
		err := rows.Scan(&player.ID, &player.GameID, &player.Username, &player.IPAddress,
			&isHost, &player.JoinedAt, &player.LeftAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan player: %w", err)
		}
		player.IsHost = isHost != 0
		players = append(players, player)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating players: %w", err)
	}

	return players, nil
}

// GetPlayerByID retrieves a player by ID
func (s *SQLiteStore) GetPlayerByID(ctx context.Context, playerID string) (*Player, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	player := &Player{}
	var isHost int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, game_id, username, ip_address, is_host, joined_at, left_at FROM players WHERE id = ?`,
		playerID,
	).Scan(&player.ID, &player.GameID, &player.Username, &player.IPAddress,
		&isHost, &player.JoinedAt, &player.LeftAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("player not found: %w", sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query player: %w", err)
	}

	player.IsHost = isHost != 0
	return player, nil
}

// RemovePlayer removes a player from a game
func (s *SQLiteStore) RemovePlayer(ctx context.Context, playerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `DELETE FROM players WHERE id = ?`, playerID)
	if err != nil {
		return fmt.Errorf("failed to remove player: %w", err)
	}
	return nil
}

// UpdatePlayerLeftTime marks when a player left
func (s *SQLiteStore) UpdatePlayerLeftTime(ctx context.Context, playerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE players SET left_at = ? WHERE id = ?`,
		now, playerID,
	)
	if err != nil {
		return fmt.Errorf("failed to update player left time: %w", err)
	}
	return nil
}

// CreateOrUpdateHost creates or updates a host profile
func (s *SQLiteStore) CreateOrUpdateHost(ctx context.Context, hostID string, username string, approvedBuzzwords json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()

	// Try to update first
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE hosts SET username = ?, approved_buzzwords = ?, last_modified_at = ? WHERE id = ?`,
		username, approvedBuzzwords, now, hostID,
	)
	if err != nil {
		return fmt.Errorf("failed to update host: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	// If no rows were updated, insert new host
	if rowsAffected == 0 {
		_, err = s.db.ExecContext(
			ctx,
			`INSERT INTO hosts (id, username, approved_buzzwords, created_at, last_modified_at)
			 VALUES (?, ?, ?, ?, ?)`,
			hostID, username, approvedBuzzwords, now, now,
		)
		if err != nil {
			return fmt.Errorf("failed to create host: %w", err)
		}
	}

	return nil
}

// GetHost retrieves a host by ID
func (s *SQLiteStore) GetHost(ctx context.Context, hostID string) (*Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	host := &Host{}
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, username, approved_buzzwords, created_at, last_modified_at FROM hosts WHERE id = ?`,
		hostID,
	).Scan(&host.ID, &host.Username, &host.ApprovedBuzzwords, &host.CreatedAt, &host.LastModifiedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("host not found: %w", sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query host: %w", err)
	}

	return host, nil
}

// GetHostByUsername retrieves a host by username
func (s *SQLiteStore) GetHostByUsername(ctx context.Context, username string) (*Host, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	host := &Host{}
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, username, approved_buzzwords, created_at, last_modified_at FROM hosts WHERE username = ?`,
		username,
	).Scan(&host.ID, &host.Username, &host.ApprovedBuzzwords, &host.CreatedAt, &host.LastModifiedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("host not found: %w", sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query host: %w", err)
	}

	return host, nil
}

// UpdateHostBuzzwords updates a host's approved buzzwords
func (s *SQLiteStore) UpdateHostBuzzwords(ctx context.Context, hostID string, approvedBuzzwords json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE hosts SET approved_buzzwords = ?, last_modified_at = ? WHERE id = ?`,
		approvedBuzzwords, now, hostID,
	)
	if err != nil {
		return fmt.Errorf("failed to update host buzzwords: %w", err)
	}
	return nil
}

// RecordWin records a win in the history table
// roomCode is empty string for standalone games (stored as NULL).
func (s *SQLiteStore) RecordWin(ctx context.Context, playerUsername string, gameCode string, roomCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	winID := generateID()
	now := time.Now().Unix()

	var roomCodeVal interface{}
	countedInGlobal := 1
	if roomCode != "" {
		roomCodeVal = roomCode
		var customCount int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM room_buzzwords WHERE room_code = ?`, roomCode,
		).Scan(&customCount); err != nil {
			return fmt.Errorf("failed to determine room board type: %w", err)
		}
		if customCount > 0 {
			countedInGlobal = 0
		}
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO wins_history (id, player_username, game_code, room_code, counted_in_global, won_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		winID, playerUsername, gameCode, roomCodeVal, countedInGlobal, now,
	)
	if err != nil {
		return fmt.Errorf("failed to record win: %w", err)
	}

	return nil
}

// GetPlayerWins retrieves win count for a player
func (s *SQLiteStore) GetPlayerWins(ctx context.Context, playerUsername string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM wins_history WHERE player_username = ?`,
		playerUsername,
	).Scan(&count)

	if err != nil {
		return 0, fmt.Errorf("failed to query win count: %w", err)
	}

	return count, nil
}

// GetLeaderboard retrieves top players by win count across all games.
func (s *SQLiteStore) GetLeaderboard(ctx context.Context, limit int) ([]*LeaderboardEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT player_username, COUNT(*) as wins FROM wins_history
		 WHERE COALESCE(counted_in_global, 1) = 1
		 GROUP BY player_username ORDER BY wins DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query leaderboard: %w", err)
	}
	defer rows.Close()

	var entries []*LeaderboardEntry
	for rows.Next() {
		entry := &LeaderboardEntry{}
		err := rows.Scan(&entry.Username, &entry.Wins)
		if err != nil {
			return nil, fmt.Errorf("failed to scan leaderboard entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating leaderboard: %w", err)
	}

	return entries, nil
}

// GetRoomLeaderboard retrieves top players by win count for a specific room (Phase 11.4).
func (s *SQLiteStore) GetRoomLeaderboard(ctx context.Context, roomCode string, limit int) ([]*LeaderboardEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT player_username, COUNT(*) as wins FROM wins_history
		 WHERE room_code = ?
		 GROUP BY player_username ORDER BY wins DESC LIMIT ?`,
		roomCode, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query room leaderboard: %w", err)
	}
	defer rows.Close()

	var entries []*LeaderboardEntry
	for rows.Next() {
		entry := &LeaderboardEntry{}
		if err := rows.Scan(&entry.Username, &entry.Wins); err != nil {
			return nil, fmt.Errorf("failed to scan room leaderboard entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating room leaderboard: %w", err)
	}

	return entries, nil
}

// GetPlayerStats retrieves aggregated win/game statistics for a player
func (s *SQLiteStore) GetPlayerStats(ctx context.Context, username string) (*PlayerStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var wins, gamesPlayed int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT
			COALESCE((SELECT COUNT(*) FROM wins_history WHERE player_username = ?), 0),
			COALESCE((SELECT COUNT(DISTINCT game_id) FROM players WHERE username = ?), 0)`,
		username, username,
	).Scan(&wins, &gamesPlayed)
	if err != nil {
		return nil, fmt.Errorf("failed to query player stats: %w", err)
	}

	var winRate float64
	if gamesPlayed > 0 {
		winRate = float64(wins) / float64(gamesPlayed)
	}

	return &PlayerStats{
		Username:    username,
		Wins:        wins,
		GamesPlayed: gamesPlayed,
		WinRate:     winRate,
	}, nil
}

// ArchiveGame persists a completed game session to the game_archives table
func (s *SQLiteStore) ArchiveGame(ctx context.Context, gameID, code, hostID, winnerID string, playerCount int, createdAt, endedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	archiveID := generateID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO game_archives (id, game_id, code, host_id, winner_id, player_count, created_at, ended_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		archiveID, gameID, code, hostID, winnerID, playerCount,
		createdAt.Unix(), endedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to archive game: %w", err)
	}
	return nil
}

// CleanupOldArchives deletes game_archives records older than 4 days
func (s *SQLiteStore) CleanupOldArchives(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-4 * 24 * time.Hour).Unix()
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM game_archives WHERE ended_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old archives: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}
	return int(n), nil
}

// ── Room operations (Phase 11.0) ─────────────────────────────────────────────

// CreateRoom creates a new room record.
// The caller must pass a collision-checked 5-char alphanumeric code.
func (s *SQLiteStore) CreateRoom(ctx context.Context, hostID string) (*Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	roomID := generateID()
	code := generateRoomCode()
	now := time.Now().Unix()

	// Retry on code collision (extremely rare but possible)
	for {
		var exists int
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rooms WHERE code = ?`, code).Scan(&exists)
		if err != nil {
			return nil, fmt.Errorf("failed to check room code uniqueness: %w", err)
		}
		if exists == 0 {
			break
		}
		code = generateRoomCode()
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO rooms (id, code, host_id, created_at) VALUES (?, ?, ?, ?)`,
		roomID, code, hostID, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create room: %w", err)
	}

	return &Room{ID: roomID, Code: code, HostID: hostID, CreatedAt: now}, nil
}

// GetRoom retrieves a room by its 5-char code.
func (s *SQLiteStore) GetRoom(ctx context.Context, code string) (*Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room := &Room{}
	var linkedRoomCode sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, code, host_id, linked_room_code, created_at FROM rooms WHERE code = ?`,
		code,
	).Scan(&room.ID, &room.Code, &room.HostID, &linkedRoomCode, &room.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("room not found: %w", sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query room: %w", err)
	}
	if linkedRoomCode.Valid {
		room.LinkedRoomCode = &linkedRoomCode.String
	}
	return room, nil
}

// GetRoomByGameCode retrieves a room via the associated game code (BINGO-XXXXX).
func (s *SQLiteStore) GetRoomByGameCode(ctx context.Context, gameCode string) (*Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room := &Room{}
	var linkedRoomCode sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT r.id, r.code, r.host_id, r.linked_room_code, r.created_at
		 FROM rooms r
		 JOIN games g ON g.room_code = r.code
		 WHERE g.code = ?`,
		gameCode,
	).Scan(&room.ID, &room.Code, &room.HostID, &linkedRoomCode, &room.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("room not found for game code %s: %w", gameCode, sql.ErrNoRows)
		}
		return nil, fmt.Errorf("failed to query room by game code: %w", err)
	}
	if linkedRoomCode.Valid {
		room.LinkedRoomCode = &linkedRoomCode.String
	}
	return room, nil
}

// SetRoomLinkedCode sets or updates the linked_room_code for a room.
func (s *SQLiteStore) SetRoomLinkedCode(ctx context.Context, code string, linkedRoomCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`UPDATE rooms SET linked_room_code = ? WHERE code = ?`,
		linkedRoomCode, code,
	)
	if err != nil {
		return fmt.Errorf("failed to set linked_room_code for room %s: %w", code, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected for room %s linked_room_code update: %w", code, err)
	}
	if n == 0 {
		return fmt.Errorf("room not found: %w", sql.ErrNoRows)
	}
	return nil
}

// GetLinkedRooms returns all rooms linked to the given parent room code.
func (s *SQLiteStore) GetLinkedRooms(ctx context.Context, roomCode string) ([]*Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, code, host_id, linked_room_code, created_at FROM rooms WHERE linked_room_code = ?`,
		roomCode,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query linked rooms for %s: %w", roomCode, err)
	}
	defer rows.Close()

	var rooms []*Room
	for rows.Next() {
		r := &Room{}
		var lrc sql.NullString
		if err := rows.Scan(&r.ID, &r.Code, &r.HostID, &lrc, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan linked room: %w", err)
		}
		if lrc.Valid {
			r.LinkedRoomCode = &lrc.String
		}
		rooms = append(rooms, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating linked rooms: %w", err)
	}
	return rooms, nil
}

// ── Buzzword operations (Phase 11.3) ─────────────────────────────────────────

// SetRoomBuzzwords stores or replaces the custom buzzword list for a room.
func (s *SQLiteStore) SetRoomBuzzwords(ctx context.Context, roomCode string, words []string, uploadedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(words)
	if err != nil {
		return fmt.Errorf("failed to marshal buzzwords: %w", err)
	}

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO room_buzzwords (room_code, words, uploaded_by, uploaded_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(room_code) DO UPDATE SET words = excluded.words,
		   uploaded_by = excluded.uploaded_by, uploaded_at = excluded.uploaded_at`,
		roomCode, data, uploadedBy, now,
	)
	if err != nil {
		return fmt.Errorf("failed to set room buzzwords: %w", err)
	}
	return nil
}

// GetRoomBuzzwords retrieves the custom buzzword list for a room.
// Returns (nil, nil) when no custom list has been set.
func (s *SQLiteStore) GetRoomBuzzwords(ctx context.Context, roomCode string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var raw []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT words FROM room_buzzwords WHERE room_code = ?`, roomCode,
	).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // no custom list set
		}
		return nil, fmt.Errorf("failed to query room buzzwords: %w", err)
	}

	var words []string
	if err := json.Unmarshal(raw, &words); err != nil {
		return nil, fmt.Errorf("failed to unmarshal room buzzwords: %w", err)
	}
	return words, nil
}

// SaveLLMFeedback persists one feedback submission.
func (s *SQLiteStore) SaveLLMFeedback(ctx context.Context, entry LLMFeedbackEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	includedData, err := json.Marshal(entry.IncludedWords)
	if err != nil {
		return fmt.Errorf("failed to marshal included words: %w", err)
	}

	excludedData, err := json.Marshal(entry.Excluded)
	if err != nil {
		return fmt.Errorf("failed to marshal excluded words: %w", err)
	}

	submittedAt := entry.SubmittedAt.Unix()
	if submittedAt <= 0 {
		submittedAt = time.Now().Unix()
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO llm_feedback
		 (game_code, topic, source_url, set_label, generation_mode, total_words, included_words, excluded_words, submitted_by, submitted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.GameCode,
		entry.Topic,
		entry.SourceURL,
		entry.SetLabel,
		entry.GenerationMode,
		entry.TotalWords,
		includedData,
		excludedData,
		entry.SubmittedBy,
		submittedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to save llm feedback: %w", err)
	}

	return nil
}

// GetRecentLLMFeedback retrieves recent feedback entries filtered by game code and/or topic.
func (s *SQLiteStore) GetRecentLLMFeedback(ctx context.Context, gameCode, topic string, limit int) ([]LLMFeedbackEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 120
	}

	query := `SELECT game_code, topic, source_url, set_label, generation_mode, total_words, included_words, excluded_words, submitted_by, submitted_at
		FROM llm_feedback`
	args := make([]interface{}, 0, 3)
	conditions := make([]string, 0, 2)

	if gameCode != "" {
		conditions = append(conditions, "LOWER(game_code) = LOWER(?)")
		args = append(args, gameCode)
	}
	if topic != "" {
		conditions = append(conditions, "LOWER(topic) = LOWER(?)")
		args = append(args, topic)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY submitted_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query llm feedback: %w", err)
	}
	defer rows.Close()

	entries := make([]LLMFeedbackEntry, 0, limit)
	for rows.Next() {
		var entry LLMFeedbackEntry
		var includedData []byte
		var excludedData []byte
		var submittedAt int64

		err := rows.Scan(
			&entry.GameCode,
			&entry.Topic,
			&entry.SourceURL,
			&entry.SetLabel,
			&entry.GenerationMode,
			&entry.TotalWords,
			&includedData,
			&excludedData,
			&entry.SubmittedBy,
			&submittedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan llm feedback row: %w", err)
		}

		entry.SubmittedAt = time.Unix(submittedAt, 0)

		if len(includedData) > 0 {
			if err := json.Unmarshal(includedData, &entry.IncludedWords); err != nil {
				return nil, fmt.Errorf("failed to unmarshal included words: %w", err)
			}
		}

		if len(excludedData) > 0 {
			if err := json.Unmarshal(excludedData, &entry.Excluded); err != nil {
				return nil, fmt.Errorf("failed to unmarshal excluded words: %w", err)
			}
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating llm feedback rows: %w", err)
	}

	return entries, nil
}

// generateID generates a unique ID (simple UUID-like string)
func generateID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Nanosecond())
}

// generateRoomCode generates a random 5-character alphanumeric room code (uppercase).
func generateRoomCode() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	var sb strings.Builder
	for range 5 {
		sb.WriteByte(chars[rand.Intn(len(chars))])
	}
	return sb.String()
}
