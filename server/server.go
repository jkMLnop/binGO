package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"
)

// Server manages the WebSocket server and game sessions
type Server struct {
	Games         map[string]*Game // gameID -> Game (for backward compatibility)
	CodeToGame    map[string]*Game // Code -> Game (Phase 7.3: code-based access)
	CurrentGame   *Game            // The active game all new players join (localhost/LAN auto-join)
	Buzzwords     [][]string
	Rows          int
	Cols          int
	GamesMu       sync.RWMutex
	PlayerCounter int64 // Atomic counter for unique player IDs
	Port          string
	Server        *http.Server
	Mux           *http.ServeMux            // Custom mux for this server
	TokenManager  *TokenManager             // JWT token manager
	Sessions      map[string]*ClientSession // IP -> ClientSession for tracking usernames
	SessionsMu    sync.RWMutex
}

// ClientSession tracks an authenticated client by IP
type ClientSession struct {
	IP       string
	Username string
	IssuedAt time.Time
}

// NewServer creates a new bingo server
func NewServer(buzzwords [][]string, rows, cols int, port string) *Server {
	mux := http.NewServeMux()
	srv := &Server{
		Games:        make(map[string]*Game),
		CodeToGame:   make(map[string]*Game),
		Buzzwords:    buzzwords,
		Rows:         rows,
		Cols:         cols,
		Port:         port,
		Mux:          mux,
		TokenManager: NewTokenManager(""), // Will generate random secret
		Sessions:     make(map[string]*ClientSession),
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
	s.CodeToGame[newGame.Code] = newGame // Phase 7.3: Register by code
	s.CurrentGame = newGame

	log.Printf("Created new game: %s with code: %s", gameID, newGame.Code)
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

	// Extract client IP
	clientIP := s.extractClientIP(r)

	// Receive login message with username or token
	var loginMsg ClientMessage
	if err := websocket.JSON.Receive(ws, &loginMsg); err != nil {
		errMsg := ServerMessage{Type: "error", Message: fmt.Sprintf("Failed to receive login: %v", err)}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, fmt.Errorf("failed to receive login: %w", err)
	}

	// Authenticate player and get username
	var username string
	var err error

	if loginMsg.Token != "" {
		// Token-based authentication (reconnect)
		username, err = s.TokenManager.VerifyToken(loginMsg.Token, clientIP)
		if err != nil {
			errMsg := ServerMessage{Type: "error", Message: fmt.Sprintf("Invalid token: %v", err)}
			websocket.JSON.Send(ws, errMsg)
			ws.Close()
			return nil, nil, fmt.Errorf("invalid token: %w", err)
		}
		log.Printf("Player %s re-authenticated with token from IP %s", username, clientIP)
	} else if loginMsg.Username != "" {
		// Username-based login
		username = loginMsg.Username
		log.Printf("Player logging in with username: %s from IP %s", username, clientIP)
	} else {
		errMsg := ServerMessage{Type: "error", Message: "Username or token required"}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, fmt.Errorf("no username or token provided")
	}

	// Update session for this IP
	s.storeSession(clientIP, username)

	// Issue new token
	token, err := s.TokenManager.IssueToken(username, clientIP, 24) // 24 hour expiration
	if err != nil {
		errMsg := ServerMessage{Type: "error", Message: fmt.Sprintf("Failed to issue token: %v", err)}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, fmt.Errorf("failed to issue token: %w", err)
	}

	// Phase 7.3: Get game code from query parameter or login message
	code := r.URL.Query().Get("code")
	if code == "" && loginMsg.Code != "" {
		code = loginMsg.Code
	}

	// Get server IP for classification (use localhost as default for IP comparison)
	serverIP := "127.0.0.1"
	if addr, err := net.ResolveTCPAddr("tcp", r.Host); err == nil {
		serverIP = addr.IP.String()
	}

	// Get or create game with code-based lookup (Phase 7.3)
	game, err := s.getOrCreateGame(code, clientIP, serverIP)
	if err != nil {
		errMsg := ServerMessage{Type: "error", Message: err.Error()}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, err
	}

	// Create and add player to game (use username as playerID)
	player, err := s.createPlayerInGame(game, username)
	if err != nil {
		errMsg := ServerMessage{Type: "error", Message: err.Error()}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, err
	}

	log.Printf("Player %s joined game %s from IP %s via WebSocket", username, game.ID, clientIP)

	// Send welcome message with token
	if err := s.sendWelcomeMessage(ws, game, player, token); err != nil {
		log.Printf("Error sending welcome message: %v", err)
		game.RemovePlayer(username)
		ws.Close()
		return nil, nil, err
	}

	// Broadcast player update to all players in game (excluding the new player who just got welcome)
	updateMsg := ServerMessage{
		Type:    "player_update",
		GameID:  game.ID,
		Code:    game.Code,
		HostID:  game.HostID,
		Players: game.GetPlayerList(),
		Message: fmt.Sprintf("Player %s joined the game", player.ID),
	}
	s.BroadcastToGame(game.ID, updateMsg)

	return player, game, nil
}

// extractClientIP extracts the client IP from the request
func (s *Server) extractClientIP(r *http.Request) string {
	// Try X-Forwarded-For header first (for proxied connections like ngrok)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	// Fall back to RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// storeSession stores or updates a client session for the given IP and username
func (s *Server) storeSession(clientIP, username string) {
	s.SessionsMu.Lock()
	defer s.SessionsMu.Unlock()
	s.Sessions[clientIP] = &ClientSession{
		IP:       clientIP,
		Username: username,
		IssuedAt: time.Now(),
	}
}

// getStoredUsername retrieves the stored username for an IP, or empty string if not found
func (s *Server) getStoredUsername(clientIP string) string {
	s.SessionsMu.RLock()
	defer s.SessionsMu.RUnlock()
	if session, exists := s.Sessions[clientIP]; exists {
		return session.Username
	}
	return ""
}

// extractPlayerID gets player ID from request or generates a new one (deprecated, kept for compatibility)
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

	// Set HostID if this is the first player (no host yet)
	if game.HostID == "" {
		game.HostID = playerID
	}

	return player, nil
}

// sendWelcomeMessage sends the welcome message to a newly connected player with JWT token
func (s *Server) sendWelcomeMessage(ws *websocket.Conn, game *Game, player *Player, token string) error {
	welcomeMsg := ServerMessage{
		Type:      "welcome",
		GameID:    game.ID,
		Code:      game.Code,   // Phase 7.3: Include game code
		HostID:    game.HostID, // Include host player ID
		PlayerID:  player.ID,
		Username:  player.ID, // username is the player ID
		Token:     token,     // Include JWT token
		Buzzwords: s.Buzzwords,
		Rows:      s.Rows,
		Cols:      s.Cols,
		Players:   game.GetPlayerList(),
		Message:   fmt.Sprintf("Welcome %s! Players in game: %d", player.ID, game.PlayerCount()),
	}

	if err := websocket.JSON.Send(ws, welcomeMsg); err != nil {
		return err
	}

	log.Printf("Sent welcome message to %s with JWT token and game code: %s", player.ID, game.Code)
	return nil
}

// getOrCreateGame retrieves a game by code (Phase 7.3) or falls back to CurrentGame for localhost/LAN
func (s *Server) getOrCreateGame(code string, clientIP, serverIP string) (*Game, error) {
	s.GamesMu.RLock()
	defer s.GamesMu.RUnlock()

	// Phase 7.3: If code provided, look up game by code
	if code != "" {
		if game, exists := s.CodeToGame[code]; exists {
			return game, nil
		}
		return nil, fmt.Errorf("invalid game code: %s", code)
	}

	// No code provided - check if connection is local/LAN
	ipType := ClassifyIP(clientIP, serverIP)
	if ipType != Remote {
		// Localhost or LAN - can auto-join current game
		var game *Game
		game = s.CurrentGame

		// If current game is inactive, create a new one
		if game != nil && !game.IsActive {
			// Need to unlock before calling createNewGame which will lock
			s.GamesMu.RUnlock()
			s.createNewGame()
			s.GamesMu.RLock()
			game = s.CurrentGame
			log.Printf("Current game was inactive, created new game: %s", game.ID)
		}

		if game == nil {
			return nil, fmt.Errorf("no active game available")
		}
		return game, nil
	}

	// Remote connection without code - reject
	return nil, fmt.Errorf("remote connections require a game code")
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

	// Broadcast player update to remaining players
	if game.IsActive && game.PlayerCount() > 0 {
		updateMsg := ServerMessage{
			Type:    "player_update",
			GameID:  game.ID,
			Players: game.GetPlayerList(),
			Message: fmt.Sprintf("Player %s left the game", player.ID),
		}
		s.BroadcastToGame(game.ID, updateMsg)
	}

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
