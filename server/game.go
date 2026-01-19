package server

import (
	"fmt"
	"sync"
	"time"
)

// messageChannel represents a communication channel to a player
type messageChannel struct {
	send chan interface{}
}

// Player represents a connected player in a game
type Player struct {
	ID       string
	messages *messageChannel
}

// newPlayer creates a new player
func newPlayer(id string) *Player {
	return &Player{
		ID: id,
		messages: &messageChannel{
			send: make(chan interface{}, 25),
		},
	}
}

// sendMessage safely sends a message to the player
func (p *Player) sendMessage(msg interface{}) error {
	select {
	case p.messages.send <- msg:
		return nil
	default:
		return fmt.Errorf("player %s message channel full", p.ID)
	}
}

// Game represents a bingo game session
type Game struct {
	ID             string
	Code           string             // Join code for this game
	OriginalHostID string             // ID of the original host who owns the code (permanent)
	HostID         string             // ID of the current host player (can be empty if host disconnected)
	HostIP         string             // IP of the host player
	Players        map[string]*Player // playerID -> Player
	IsActive       bool               // Game is in progress
	Winner         string             // Player ID of winner (empty if no winner yet)
	CreatedAt      time.Time          // When this game session started
	EndedAt        time.Time          // When this game session ended (zero if still active)
	PlayersMu      sync.RWMutex       // Protect Players map
}

// ArchivedGame stores completed game sessions for history
type ArchivedGame struct {
	ID             string
	Code           string
	OriginalHostID string
	Winner         string
	CreatedAt      time.Time
	EndedAt        time.Time
}

// NewGame creates a new game session
func NewGame(id string, buzzwords [][]string, rows, cols int) *Game {
	return &Game{
		ID:        id,
		Code:      GenerateGameCode(),
		Players:   make(map[string]*Player),
		IsActive:  true,
		CreatedAt: time.Now(),
	}
}

// AddPlayer adds a new player to the game
func (g *Game) AddPlayer(player *Player) error {
	g.PlayersMu.Lock()
	defer g.PlayersMu.Unlock()

	if _, exists := g.Players[player.ID]; exists {
		return fmt.Errorf("player %s already in game", player.ID)
	}

	g.Players[player.ID] = player
	return nil
}

// GetPlayer retrieves a player by ID
func (g *Game) GetPlayer(playerID string) (*Player, bool) {
	g.PlayersMu.RLock()
	defer g.PlayersMu.RUnlock()

	player, exists := g.Players[playerID]
	return player, exists
}

// GetPlayerList returns list of player IDs
func (g *Game) GetPlayerList() []string {
	g.PlayersMu.RLock()
	defer g.PlayersMu.RUnlock()

	playerList := make([]string, 0, len(g.Players))
	for id := range g.Players {
		playerList = append(playerList, id)
	}
	return playerList
}

// RemovePlayer removes a player from the game
func (g *Game) RemovePlayer(playerID string) {
	g.PlayersMu.Lock()
	defer g.PlayersMu.Unlock()

	delete(g.Players, playerID)
}

// PlayerCount returns the number of connected players
func (g *Game) PlayerCount() int {
	g.PlayersMu.RLock()
	defer g.PlayersMu.RUnlock()

	return len(g.Players)
}

// ResetBoard clears all players and resets game state for the next round
func (g *Game) ResetBoard(buzzwords [][]string, rows, cols int) {
	g.PlayersMu.Lock()
	defer g.PlayersMu.Unlock()

	// Reset game state but keep current players (they will clear their own marks)
	// Just reset the game metadata for the new session
	g.IsActive = true
	g.Winner = ""
	g.CreatedAt = time.Now() // Fresh session start
	g.EndedAt = time.Time{}  // Clear end time
}
