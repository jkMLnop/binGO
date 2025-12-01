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

// Start begins listening for connections
func (s *Server) Start() error {
	s.registerHandlers()
	return s.startHTTPServer()
}

// registerHandlers registers all HTTP handlers
func (s *Server) registerHandlers() {
	s.Mux.Handle("/ws", websocket.Handler(s.wsHandler))
	s.Mux.HandleFunc("/status", s.handleStatus)
}

// startHTTPServer creates and starts the HTTP server
func (s *Server) startHTTPServer() error {
	s.Server = &http.Server{
		Addr:    ":" + s.Port,
		Handler: s.Mux,
	}

	log.Printf("Server starting on port %s", s.Port)
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

// wsHandler handles incoming WebSocket connections - orchestrates the connection lifecycle
func (s *Server) wsHandler(ws *websocket.Conn) {
	player, game, err := s.HandlePlayerConnect(ws)
	if err != nil {
		log.Printf("Error handling player connect: %v", err)
		return
	}
	defer func() {
		if err := s.HandlePlayerDisconnect(game, player, ws); err != nil {
			log.Printf("Error during disconnect: %v", err)
		}
	}()

	// Spawn goroutine to forward messages from player's message channel to WebSocket
	go s.forwardPlayerMessages(player, ws)

	// Listen for incoming messages from player
	for {
		msg, err := s.ReceivePlayerMessage(ws)
		if err != nil {
			log.Printf("Player %s disconnected: %v", player.ID, err)
			break
		}

		s.ProcessPlayerMessage(game, player, msg)
	}
}

// HandlePlayerConnect authenticates and welcomes a player, returns player and game or error
func (s *Server) HandlePlayerConnect(ws *websocket.Conn) (*Player, *Game, error) {
	log.Printf("New WebSocket connection from %s", ws.Request().RemoteAddr)
	r := ws.Request()

	// Get or create game
	game, err := s.getOrCreateGame(r.URL.Query().Get("gameId"))
	if err != nil {
		errMsg := ServerMessage{Type: "error", Message: err.Error()}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, err
	}

	// Extract or generate player ID
	playerID := s.extractPlayerID(r)

	// Create and add player to game
	player, err := s.createPlayerInGame(game, playerID)
	if err != nil {
		errMsg := ServerMessage{Type: "error", Message: err.Error()}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, err
	}

	log.Printf("Player %s joined game %s via WebSocket", playerID, game.ID)

	// Send welcome message
	if err := s.sendWelcomeMessage(ws, game, player); err != nil {
		log.Printf("Error sending welcome message: %v", err)
		game.RemovePlayer(playerID)
		ws.Close()
		return nil, nil, err
	}

	return player, game, nil
}

// extractPlayerID gets player ID from request or generates a new one
func (s *Server) extractPlayerID(r *http.Request) string {
	playerID := r.URL.Query().Get("id")
	if playerID == "" {
		newID := atomic.AddInt64(&s.PlayerCounter, 1)
		playerID = fmt.Sprintf("player-%d", newID)
	}
	return playerID
}

// createPlayerInGame creates a new player and adds them to the game
func (s *Server) createPlayerInGame(game *Game, playerID string) (*Player, error) {
	player := NewPlayer(playerID)
	if err := game.AddPlayer(player); err != nil {
		return nil, err
	}
	return player, nil
}

// sendWelcomeMessage sends the welcome message to a newly connected player
func (s *Server) sendWelcomeMessage(ws *websocket.Conn, game *Game, player *Player) error {
	welcomeMsg := ServerMessage{
		Type:      "welcome",
		GameID:    game.ID,
		PlayerID:  player.ID,
		Buzzwords: s.Buzzwords,
		Rows:      s.Rows,
		Cols:      s.Cols,
		Players:   game.GetPlayerList(),
		Message:   fmt.Sprintf("Welcome %s! Players in game: %d", player.ID, game.PlayerCount()),
	}

	if err := websocket.JSON.Send(ws, welcomeMsg); err != nil {
		return err
	}

	log.Printf("Sent welcome message to %s", player.ID)
	return nil
}

// getOrCreateGame retrieves or creates a game
func (s *Server) getOrCreateGame(gameID string) (*Game, error) {
	s.GamesMu.RLock()
	var game *Game
	if gameID != "" {
		game = s.Games[gameID]
	}
	if game == nil {
		game = s.CurrentGame
	}
	s.GamesMu.RUnlock()

	// If current game is inactive, create a new one
	if game == s.CurrentGame && !game.IsActive {
		s.createNewGame()
		s.GamesMu.RLock()
		game = s.CurrentGame
		s.GamesMu.RUnlock()
		log.Printf("Current game was inactive, created new game: %s", game.ID)
	}

	if game == nil {
		return nil, fmt.Errorf("no active game available")
	}

	return game, nil
}

// ReceivePlayerMessage reads and returns the next message from the WebSocket connection
func (s *Server) ReceivePlayerMessage(ws *websocket.Conn) (*ClientMessage, error) {
	var msg ClientMessage
	if err := websocket.JSON.Receive(ws, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ProcessPlayerMessage handles a message from a player and logs any errors internally
func (s *Server) ProcessPlayerMessage(game *Game, player *Player, msg *ClientMessage) {
	if msg.Action != "win" {
		return
	}

	if err := s.HandlePlayerWin(game, player); err != nil {
		log.Printf("Error processing player message: %v", err)
	}
}

// HandlePlayerWin processes a win announcement from a player
func (s *Server) HandlePlayerWin(game *Game, player *Player) error {
	log.Printf("Player %s announced a win!", player.ID)

	// Verify player exists in game
	_, exists := game.GetPlayer(player.ID)
	if !exists {
		return fmt.Errorf("player %s not found in game", player.ID)
	}

	// Update game state
	game.IsActive = false
	game.Winner = player.ID
	log.Printf("🏆 Player %s WON game %s!", player.ID, game.ID)

	// Create and broadcast win message
	winMsg := ServerMessage{
		Type:    "game_ended",
		GameID:  game.ID,
		Winner:  player.ID,
		Message: fmt.Sprintf("Player %s has won!", player.ID),
	}

	if err := s.BroadcastToGame(game.ID, winMsg); err != nil {
		return err
	}

	log.Printf("Broadcasted win for player %s to all players in game %s", player.ID, game.ID)
	return nil
}

// forwardPlayerMessages forwards messages from player's channel to their WebSocket connection
func (s *Server) forwardPlayerMessages(player *Player, ws *websocket.Conn) {
	for msg := range player.Messages.Send {
		if err := websocket.JSON.Send(ws, msg); err != nil {
			log.Printf("Error sending message to %s: %v", player.ID, err)
			return
		}
	}
}

// HandlePlayerDisconnect removes player from game and closes the connection
func (s *Server) HandlePlayerDisconnect(game *Game, player *Player, ws *websocket.Conn) error {
	game.RemovePlayer(player.ID)
	if err := ws.Close(); err != nil {
		log.Printf("Error closing connection for player %s: %v", player.ID, err)
		return err
	}
	log.Printf("Player %s disconnected from game %s", player.ID, game.ID)
	return nil
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
