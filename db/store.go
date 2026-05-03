package db

import (
	"context"
	"encoding/json"
	"time"
)

// GameStore defines the interface for persisting game state, players, and buzzword lists.
// Implementations can be SQLite, PostgreSQL, or other backends.
type GameStore interface {
	// Database lifecycle
	Init(ctx context.Context) error
	Close(ctx context.Context) error

	// Game operations
	CreateGame(ctx context.Context, code string, hostID string, buzzwords json.RawMessage) (gameID string, err error)
	GetGameByCode(ctx context.Context, code string) (*Game, error)
	GetGameByID(ctx context.Context, gameID string) (*Game, error)
	UpdateGameStatus(ctx context.Context, gameID string, status string) error
	UpdateGameWinner(ctx context.Context, gameID string, winnerID string) error
	UpdateGameBuzzwords(ctx context.Context, gameID string, buzzwords json.RawMessage) error
	DeleteGame(ctx context.Context, gameID string) error
	CleanupExpiredGames(ctx context.Context) (deletedCount int, err error)

	// Player operations
	AddPlayer(ctx context.Context, gameID string, username string, ipAddress string, isHost bool) (playerID string, err error)
	GetPlayersInGame(ctx context.Context, gameID string) ([]*Player, error)
	GetPlayerByID(ctx context.Context, playerID string) (*Player, error)
	RemovePlayer(ctx context.Context, playerID string) error
	UpdatePlayerLeftTime(ctx context.Context, playerID string) error

	// Host profile operations
	CreateOrUpdateHost(ctx context.Context, hostID string, username string, approvedBuzzwords json.RawMessage) error
	GetHost(ctx context.Context, hostID string) (*Host, error)
	GetHostByUsername(ctx context.Context, username string) (*Host, error)
	UpdateHostBuzzwords(ctx context.Context, hostID string, approvedBuzzwords json.RawMessage) error

	// Win history operations
	RecordWin(ctx context.Context, playerUsername string, gameCode string) error
	GetPlayerWins(ctx context.Context, playerUsername string) (int, error)
	GetLeaderboard(ctx context.Context, limit int) ([]*LeaderboardEntry, error)
	GetPlayerStats(ctx context.Context, username string) (*PlayerStats, error)

	// Game archive operations
	ArchiveGame(ctx context.Context, gameID, code, hostID, winnerID string, playerCount int, createdAt, endedAt time.Time) error
	CleanupOldArchives(ctx context.Context) (deletedCount int, err error)
}

// Game represents a game session
type Game struct {
	ID        string
	Code      string
	HostID    string
	Status    string // "active", "ended"
	Buzzwords json.RawMessage
	WinnerID  *string // nullable - nil if no winner yet
	CreatedAt int64   // Unix timestamp
	EndedAt   *int64  // Unix timestamp (nullable)
	ExpiresAt int64   // Unix timestamp (4 days from creation)
}

// Player represents a player in a game
type Player struct {
	ID        string
	GameID    string
	Username  string
	IPAddress string
	IsHost    bool
	JoinedAt  int64  // Unix timestamp
	LeftAt    *int64 // Unix timestamp (nullable - nil if still in game)
}

// Host represents a host profile with their approved buzzwords
type Host struct {
	ID                string
	Username          string
	ApprovedBuzzwords json.RawMessage
	CreatedAt         int64 // Unix timestamp
	LastModifiedAt    int64 // Unix timestamp
}

// LeaderboardEntry represents a player's win statistics
type LeaderboardEntry struct {
	Username string
	Wins     int
}

// PlayerStats holds aggregated statistics for a single player
type PlayerStats struct {
	Username    string  `json:"username"`
	Wins        int     `json:"wins"`
	GamesPlayed int     `json:"games_played"`
	WinRate     float64 `json:"win_rate"`
}

// GameArchive represents a completed game session persisted for history
type GameArchive struct {
	ID          string
	GameID      string
	Code        string
	HostID      string
	WinnerID    string
	PlayerCount int
	CreatedAt   int64 // Unix timestamp
	EndedAt     int64 // Unix timestamp
}
