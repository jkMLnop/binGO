package server

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// messageChannel represents a communication channel to a player
type messageChannel struct {
	send chan interface{}
}

// Player represents a connected player in a game
type Player struct {
	ID       string
	messages *messageChannel
	ws       *websocket.Conn // Current WebSocket connection (for reconnection handling)
	wsMu     sync.Mutex      // Protect concurrent access to ws
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

// SetWS stores the active WebSocket connection on the player (used for graceful shutdown)
func (p *Player) SetWS(ws *websocket.Conn) {
	p.wsMu.Lock()
	defer p.wsMu.Unlock()
	p.ws = ws
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
	ID                  string
	Code                string             // Join code for this game
	HostID              string             // ID of the host player (immutable once set on first player connect)
	Title               string             // Board name (Phase 12.5) — set from AI generation topic
	Players             map[string]*Player // playerID -> Player
	IsActive            bool               // Game is in progress
	Orphaned            bool               // True when all players disconnected without a winner
	Winner              string             // Player ID of winner (empty if no winner yet)
	CreatedAt           time.Time          // When this game session started
	EndedAt             time.Time          // When this game session ended (zero if still active)
	Buzzwords           [][]string         // Buzzword pool for this game (may differ from server defaults)
	Suggestions         []Suggestion       // Phase 9: pending buzzword suggestions (in-memory only)
	SuggestionsMu       sync.Mutex         // Protect Suggestions slice
	RejectedSuggestions []string           // Phase 9.6: phrases rejected by host this round (in-memory)
	Bets                []GameBet          // Phase 9.5: active player bets (in-memory only)
	BetsMu              sync.Mutex         // Protect Bets slice
	PlayersMu           sync.RWMutex       // Protect Players map
}

// NewGame creates a new game session
func NewGame(id string, buzzwords [][]string, rows, cols int) *Game {
	return &Game{
		ID:        id,
		Code:      GenerateGameCode(),
		Players:   make(map[string]*Player),
		IsActive:  true,
		CreatedAt: time.Now(),
		Buzzwords: buzzwords,
	}
}

// AddPlayer adds a new player to the game
func (g *Game) AddPlayer(player *Player) error {
	g.PlayersMu.Lock()
	defer g.PlayersMu.Unlock()

	if _, exists := g.Players[player.ID]; exists {
		return fmt.Errorf("player ID %s is already in use (collision detected). Please use a different username", player.ID)
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
	g.IsActive = true
	g.Winner = ""
	g.CreatedAt = time.Now() // Fresh session start
	g.EndedAt = time.Time{}  // Clear end time
	if len(buzzwords) > 0 {
		g.Buzzwords = buzzwords
	}

	// Clear ephemeral in-game state for the new round
	g.SuggestionsMu.Lock()
	g.Suggestions = nil
	g.RejectedSuggestions = nil
	g.SuggestionsMu.Unlock()

	g.BetsMu.Lock()
	g.Bets = nil
	g.BetsMu.Unlock()
}
