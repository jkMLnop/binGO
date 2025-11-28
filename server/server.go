package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"

	"golang.org/x/net/websocket"
)

// Server manages the WebSocket server and game sessions
type Server struct {
	Games         map[string]*Game // gameID -> Game
	CurrentGame   *Game            // The active game all new players join
	Buzzwords     [][]string
	Rows          int
	Cols          int
	GamesMu       sync.RWMutex
	PlayerCounter int64 // Atomic counter for unique player IDs
	Port          string
	Server        *http.Server
	Mux           *http.ServeMux // Custom mux for this server
}

// NewServer creates a new bingo server
func NewServer(buzzwords [][]string, rows, cols int, port string) *Server {
	mux := http.NewServeMux()
	srv := &Server{
		Games:     make(map[string]*Game),
		Buzzwords: buzzwords,
		Rows:      rows,
		Cols:      cols,
		Port:      port,
		Mux:       mux,
	}
	srv.createNewGame()
	return srv
}

// Start begins listening for connections with TLS
func (s *Server) Start() error {
	// Register WebSocket handler with custom mux
	s.Mux.Handle("/ws", websocket.Handler(s.wsHandler))
	s.Mux.HandleFunc("/status", s.handleStatus)

	s.Server = &http.Server{
		Addr:    ":" + s.Port,
		Handler: s.Mux,
	}

	log.Printf("Server starting on port %s", s.Port)

	// For now, use plain HTTP - TLS will be enabled when domain is ready
	log.Printf("Running without TLS (using plain HTTP for testing)")
	return s.Server.ListenAndServe()
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	if s.Server != nil {
		return s.Server.Shutdown(ctx)
	}
	return nil
}

// createNewGame creates a new game and sets it as current
func (s *Server) createNewGame() {
	s.GamesMu.Lock()
	defer s.GamesMu.Unlock()

	gameID := fmt.Sprintf("game-%d", len(s.Games)+1)
	newGame := NewGame(gameID, s.Buzzwords, s.Rows, s.Cols)
	s.Games[gameID] = newGame
	s.CurrentGame = newGame

	log.Printf("Created new game: %s", gameID)
}

// wsHandler handles incoming WebSocket connections
func (s *Server) wsHandler(ws *websocket.Conn) {
	log.Printf("New WebSocket connection from %s", ws.Request().RemoteAddr)
	r := ws.Request()

	// Extract gameId from query params, or use current game
	gameID := r.URL.Query().Get("gameId")

	s.GamesMu.RLock()
	var game *Game
	if gameID != "" {
		game = s.Games[gameID]
	}
	if game == nil {
		game = s.CurrentGame
	}
	s.GamesMu.RUnlock()

	// If current game is inactive, create a new game
	if game == s.CurrentGame && !game.IsActive {
		s.createNewGame()
		s.GamesMu.RLock()
		game = s.CurrentGame
		s.GamesMu.RUnlock()
		log.Printf("Current game was inactive, created new game: %s", game.ID)
	}

	if game == nil {
		errMsg := ServerMessage{
			Type:    "error",
			Message: "No active game",
		}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return
	}

	// Generate player ID
	playerID := r.URL.Query().Get("id")
	if playerID == "" {
		newID := atomic.AddInt64(&s.PlayerCounter, 1)
		playerID = fmt.Sprintf("player-%d", newID)
	}

	// Create player
	player := NewPlayer(playerID)

	// Add player to game
	if err := game.AddPlayer(player); err != nil {
		errMsg := ServerMessage{
			Type:    "error",
			Message: err.Error(),
		}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return
	}

	log.Printf("Player %s joined game %s via WebSocket", playerID, game.ID)

	// Send welcome message with buzzwords (client will generate board locally)
	welcomeMsg := ServerMessage{
		Type:      "welcome",
		GameID:    game.ID,
		PlayerID:  playerID,
		Buzzwords: s.Buzzwords,
		Rows:      s.Rows,
		Cols:      s.Cols,
		Players:   game.GetPlayerList(),
		Message:   fmt.Sprintf("Welcome %s! Players in game: %d", playerID, game.PlayerCount()),
	}

	if err := websocket.JSON.Send(ws, welcomeMsg); err != nil {
		log.Printf("Error sending welcome message: %v", err)
		game.RemovePlayer(playerID)
		ws.Close()
		return
	}

	log.Printf("Sent welcome message to %s", playerID)

	// Spawn goroutine to forward messages from each player's message channel to their WebSocket connection
	go func() {
		for msg := range player.Messages.Send {
			if err := websocket.JSON.Send(ws, msg); err != nil {
				log.Printf("Error sending message to %s: %v", playerID, err)
				return
			}
		}
	}()

	// Listen for incoming messages from player (win announcements)
	for {
		var msg ClientMessage
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			log.Printf("Player %s disconnected: %v", playerID, err)
			game.RemovePlayer(playerID)
			break
		}

		// Handle win announcement from player
		if msg.Action == "win" {
			log.Printf("Player %s announced a win!", playerID)

			// Verify player exists in game
			_, exists := game.GetPlayer(playerID)
			if !exists {
				log.Printf("Player %s not found in game", playerID)
				continue
			}

			// Update game state
			game.IsActive = false
			game.Winner = playerID
			log.Printf("🏆 Player %s WON game %s!", playerID, game.ID)

			// Create win announcement message
			winMsg := ServerMessage{
				Type:    "game_ended",
				GameID:  game.ID,
				Winner:  playerID,
				Message: fmt.Sprintf("Player %s has won!", playerID),
			}

			// Broadcast to all players in game
			s.BroadcastToGame(game.ID, winMsg)
			log.Printf("Broadcasted win for player %s to all players in game %s", playerID, game.ID)
		}
	}
}

// handleStatus returns server status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.GamesMu.RLock()
	defer s.GamesMu.RUnlock()

	statusData := map[string]interface{}{
		"total_games": len(s.Games),
		"current_game": map[string]interface{}{
			"id":          s.CurrentGame.ID,
			"players":     s.CurrentGame.PlayerCount(),
			"is_active":   s.CurrentGame.IsActive,
			"player_list": s.CurrentGame.GetPlayerList(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statusData)
}

// BroadcastToGame sends a message to all players in a game
func (s *Server) BroadcastToGame(gameID string, msg interface{}) error {
	s.GamesMu.RLock()
	game, exists := s.Games[gameID]
	s.GamesMu.RUnlock()

	if !exists {
		return fmt.Errorf("game %s not found", gameID)
	}

	game.PlayersMu.RLock()
	playersCopy := make([]*Player, 0, len(game.Players))
	for _, player := range game.Players {
		playersCopy = append(playersCopy, player)
	}
	game.PlayersMu.RUnlock()

	for _, player := range playersCopy {
		_ = player.SendMessage(msg) // Non-blocking send
	}

	return nil
}
