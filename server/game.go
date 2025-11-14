package server

import (
	"fmt"
	"sync"

	"github.com/jkMLnop/binGO-CLI/shared"
)

// MessageChannel represents a communication channel to a player
type MessageChannel struct {
	Send chan interface{}
}

// Player represents a connected player with one or more bingo cards
type Player struct {
	ID       string
	Messages *MessageChannel
	Cards    []*shared.GameSession // Support multiple cards per player
	Mu       sync.Mutex            // Protect concurrent access to Cards
}

// NewPlayer creates a new player with an initial card
func NewPlayer(id string, buzzwords [][]string, rows, cols int) *Player {
	initialCard := shared.NewGameSession(buzzwords, rows, cols)
	return &Player{
		ID: id,
		Messages: &MessageChannel{
			Send: make(chan interface{}, 10),
		},
		Cards: []*shared.GameSession{initialCard},
	}
}

// SendMessage safely sends a message to the player
func (p *Player) SendMessage(msg interface{}) error {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	select {
	case p.Messages.Send <- msg:
		return nil
	default:
		return fmt.Errorf("player %s message channel full", p.ID)
	}
}

// GetCard returns the player's card by index
func (p *Player) GetCard(cardIndex int) *shared.GameSession {
	p.Mu.Lock()
	defer p.Mu.Unlock()

	if cardIndex < 0 || cardIndex >= len(p.Cards) {
		return nil
	}
	return p.Cards[cardIndex]
}

// GetFirstCard returns the player's first (primary) card
func (p *Player) GetFirstCard() *shared.GameSession {
	return p.GetCard(0)
}

// Game represents a bingo game session
type Game struct {
	ID            string
	Players       map[string]*Player // playerID -> Player
	Buzzwords     [][]string         // Shared buzzwords for board generation
	Rows          int                // Board dimensions
	Cols          int
	IsActive      bool             // Game is in progress
	Winner        string           // Player ID of winner (empty if no winner yet)
	PlayersMu     sync.RWMutex     // Protect Players map
	BroadcastChan chan interface{} // Channel for broadcasting messages
}

// NewGame creates a new game session
func NewGame(id string, buzzwords [][]string, rows, cols int) *Game {
	return &Game{
		ID:            id,
		Players:       make(map[string]*Player),
		Buzzwords:     buzzwords,
		Rows:          rows,
		Cols:          cols,
		IsActive:      true,
		BroadcastChan: make(chan interface{}, 100), // Buffered channel for broadcasts
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
