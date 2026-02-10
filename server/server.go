package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jkMLnop/binGO-CLI/db"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/websocket"
)

// Server manages the WebSocket server and game sessions
type Server struct {
	Games         map[string]*Game // gameID -> Game (for backward compatibility)
	CodeToGame    map[string]*Game // Code -> Game (Phase 7.3: code-based access)
	ArchivedGames []ArchivedGame   // Completed game sessions for history
	Buzzwords     [][]string
	Rows          int
	Cols          int
	GamesMu       sync.RWMutex
	Port          string
	Server        *http.Server
	Mux           *http.ServeMux            // Custom mux for this server
	TokenManager  *TokenManager             // JWT token manager
	Sessions      map[string]*ClientSession // IP -> ClientSession for tracking usernames
	SessionsMu    sync.RWMutex
	DB            db.GameStore // Database store (Phase 7.5)
	Metrics       *Metrics     // Prometheus metrics (Phase 8)
	Logger        *Logger      // Structured JSON logger (Phase 8)
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
		Games:         make(map[string]*Game),
		CodeToGame:    make(map[string]*Game),
		ArchivedGames: make([]ArchivedGame, 0),
		Buzzwords:     buzzwords,
		Rows:          rows,
		Cols:          cols,
		Port:          port,
		Mux:           mux,
		TokenManager:  NewTokenManager(""), // Will generate random secret
		Sessions:      make(map[string]*ClientSession),
		DB:            nil, // Optional - can be set later with SetDB()
		Metrics:       NewMetrics(),
		Logger:        NewLogger(),
	}
	return srv
}

// SetDB sets the database store for this server
func (s *Server) SetDB(store db.GameStore) {
	s.DB = store
}

// Start begins listening for connections
func (s *Server) Start() error {
	s.registerHandlers()
	// Create initial game and display code to users
	s.createNewGame()
	return s.startHTTPServer()
}

// registerHandlers registers all HTTP handlers
func (s *Server) registerHandlers() {
	s.Mux.Handle("/ws", websocket.Handler(s.wsHandler))
	s.Mux.HandleFunc("/status", s.handleStatus)

	// API handlers (Phase 7.5)
	s.Mux.HandleFunc("/api/status", s.handleAPIStatus)
	s.Mux.HandleFunc("/api/game/", s.handleGetGameByCode)
	s.Mux.HandleFunc("/api/leaderboard", s.handleGetLeaderboard)

	// Admin API handlers (Phase 8) - register both with and without trailing path
	s.Mux.HandleFunc("/admin/api/games", s.handleAdminGames)
	s.Mux.HandleFunc("/admin/api/games/", s.handleAdminGames)

	// Metrics endpoint (Phase 8)
	s.Mux.Handle("/metrics", promhttp.Handler())
}

// startHTTPServer creates and starts the HTTP server
func (s *Server) startHTTPServer() error {
	s.Server = &http.Server{
		Addr:    ":" + s.Port,
		Handler: s.Mux,
	}

	log.Printf("Server listening on port %s", s.Port)
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

// createNewGame creates a new game and registers it by code
func (s *Server) createNewGame() {
	s.GamesMu.Lock()
	defer s.GamesMu.Unlock()

	gameID := fmt.Sprintf("game-%d", len(s.Games)+1)
	newGame := NewGame(gameID, s.Buzzwords, s.Rows, s.Cols)
	s.Games[gameID] = newGame
	s.CodeToGame[newGame.Code] = newGame // Phase 7.3: Register by code

	log.Printf("Created new game: %s with code: %s", gameID, newGame.Code)

	// Update metrics (Phase 8)
	s.Metrics.GameCount.Set(float64(len(s.Games)))
	s.Metrics.GamesCreatedTotal.Inc()

	// Save game to database (Phase 7.5)
	ctx := context.Background()
	if err := SaveGameToDB(ctx, s.DB, newGame, s.Buzzwords); err != nil {
		log.Printf("Warning: failed to save game to DB: %v", err)
	}
}

// wsHandler handles incoming WebSocket connections - orchestrates the connection lifecycle
func (s *Server) wsHandler(ws *websocket.Conn) {
	player, game, err := s.handlePlayerConnect(ws)
	if err != nil {
		log.Printf("Error handling player connect: %v", err)
		return
	}
	defer func() {
		if err := s.handlePlayerDisconnect(game, player, ws); err != nil {
			log.Printf("Error during disconnect: %v", err)
		}
	}()

	// Spawn goroutine to forward messages from player's message channel to WebSocket
	go s.forwardPlayerMessages(player, ws)

	// Listen for incoming messages from player
	for {
		msg, err := s.receivePlayerMessage(ws)
		if err != nil {
			log.Printf("Player %s disconnected: %v", player.ID, err)
			break
		}

		s.processPlayerMessage(game, player, msg)
	}
}

// handlePlayerConnect authenticates and welcomes a player, returns player and game or error
func (s *Server) handlePlayerConnect(ws *websocket.Conn) (*Player, *Game, error) {
	log.Printf("New WebSocket connection from %s", ws.Request().RemoteAddr)
	r := ws.Request()
	clientIP := s.extractClientIP(r)

	// Receive and authenticate login message
	var loginMsg ClientMessage
	if err := websocket.JSON.Receive(ws, &loginMsg); err != nil {
		errMsg := ServerMessage{Type: "error", Message: fmt.Sprintf("Failed to receive login: %v", err)}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, fmt.Errorf("failed to receive login: %w", err)
	}

	username, err := s.authenticatePlayer(ws, loginMsg)
	if err != nil {
		return nil, nil, err
	}

	// Store session and issue token
	s.storeSession(clientIP, username)
	token, err := s.issueAndStoreToken(ws, username, clientIP)
	if err != nil {
		return nil, nil, err
	}

	// Get or create game
	code := r.URL.Query().Get("code")
	if code == "" && loginMsg.Code != "" {
		code = loginMsg.Code
	}

	if code == "" {
		errMsg := ServerMessage{Type: "error", Message: "Game code required for all remote connections"}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, fmt.Errorf("game code required")
	}

	game, err := s.getOrCreateGame(code)
	if err != nil {
		errMsg := ServerMessage{Type: "error", Message: err.Error()}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, err
	}

	// Check if player is reconnecting (already in game)
	existingPlayer, exists := game.GetPlayer(username)
	var player *Player

	if exists && existingPlayer != nil {
		// Player already exists in the game - reject all attempts (token or not)
		// Only reconnection allowed AFTER player is removed from game
		errMsg := ServerMessage{Type: "error", Message: "Username already in use in this game"}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return nil, nil, fmt.Errorf("username %s already in game", username)
	} else {
		// New player - create and add to game
		player, err = s.createPlayerInGame(game, username)
		if err != nil {
			errMsg := ServerMessage{Type: "error", Message: err.Error()}
			websocket.JSON.Send(ws, errMsg)
			ws.Close()
			return nil, nil, err
		}
		log.Printf("Player %s JOINED game %s from IP %s via WebSocket", username, game.ID, clientIP)
	}

	// Update metrics (Phase 8)
	s.Metrics.PlayerCount.Set(float64(s.countTotalPlayers()))
	s.Metrics.PlayersConnectedTotal.Inc()

	// Send welcome and broadcast
	if err := s.welcomeAndBroadcast(ws, game, player, token); err != nil {
		log.Printf("Error in welcome/broadcast: %v", err)
		game.RemovePlayer(username)
		ws.Close()
		return nil, nil, err
	}

	return player, game, nil
}

// authenticatePlayer validates login via token or username
func (s *Server) authenticatePlayer(ws *websocket.Conn, loginMsg ClientMessage) (string, error) {
	var username string
	var err error

	if loginMsg.Token != "" {
		// Token-based authentication (reconnect)
		clientIP := ""
		if r := ws.Request(); r != nil {
			clientIP = s.extractClientIP(r)
		}
		username, err = s.TokenManager.VerifyToken(loginMsg.Token, clientIP)
		if err != nil {
			errMsg := ServerMessage{Type: "error", Message: fmt.Sprintf("Invalid token: %v", err)}
			websocket.JSON.Send(ws, errMsg)
			ws.Close()
			return "", fmt.Errorf("invalid token: %w", err)
		}
		log.Printf("Player %s re-authenticated with token", username)
	} else if loginMsg.Username != "" {
		// Username-based login
		username = loginMsg.Username
		log.Printf("Player logging in with username: %s", username)
	} else {
		errMsg := ServerMessage{Type: "error", Message: "Username or token required"}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return "", fmt.Errorf("no username or token provided")
	}

	return username, nil
}

// issueAndStoreToken issues a new JWT token
func (s *Server) issueAndStoreToken(ws *websocket.Conn, username, clientIP string) (string, error) {
	token, err := s.TokenManager.IssueToken(username, clientIP, 24) // 24 hour expiration
	if err != nil {
		errMsg := ServerMessage{Type: "error", Message: fmt.Sprintf("Failed to issue token: %v", err)}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return "", fmt.Errorf("failed to issue token: %w", err)
	}
	return token, nil
}

// welcomeAndBroadcast sends welcome message and announces player to others
func (s *Server) welcomeAndBroadcast(ws *websocket.Conn, game *Game, player *Player, token string) error {
	if err := s.sendWelcomeMessage(ws, game, player, token); err != nil {
		return err
	}

	// Broadcast player update to all players in game
	updateMsg := ServerMessage{
		Type:    "player_update",
		GameID:  game.ID,
		Code:    game.Code,
		HostID:  game.HostID,
		Players: game.GetPlayerList(),
		Message: fmt.Sprintf("Player %s joined the game", player.ID),
	}
	return s.broadcastToGame(game.ID, updateMsg)
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

// createPlayerInGame creates a new player and adds them to the game
func (s *Server) createPlayerInGame(game *Game, playerID string) (*Player, error) {
	player := newPlayer(playerID)
	if err := game.AddPlayer(player); err != nil {
		return nil, err
	}

	// Set HostID if this is the first player (immutable once set)
	isHost := false
	if game.HostID == "" {
		game.HostID = playerID
		isHost = true
		log.Printf("👑 Player %s set as HostID for game %s with code %s", playerID, game.ID, game.Code)
	}

	// Record player in database (Phase 7.5)
	ctx := context.Background()
	if s.DB != nil {
		dbPlayerID, err := RecordPlayerInDB(ctx, s.DB, game.ID, playerID, "", isHost)
		if err != nil {
			log.Printf("Warning: failed to record player in DB: %v", err)
		} else {
			// Store DB info for later win recording
			SetPlayerDBInfo(player.ID, &PlayerDBInfo{
				DBPlayerID: dbPlayerID,
				GameCode:   game.Code,
				Username:   playerID,
				IPAddress:  "",
			})
		}
	}

	return player, nil
}

// sendWelcomeMessage sends the welcome message to a newly connected player with JWT token
func (s *Server) sendWelcomeMessage(ws *websocket.Conn, game *Game, player *Player, token string) error {
	welcomeMsg := ServerMessage{
		Type:      "welcome",
		GameID:    game.ID,
		Code:      game.Code,   // Phase 7.3: Include game code
		HostID:    game.HostID, // Include host player ID (immutable)
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

// getOrCreateGame retrieves a game by code
func (s *Server) getOrCreateGame(code string) (*Game, error) {
	s.GamesMu.RLock()
	defer s.GamesMu.RUnlock()

	// Code is required - look up game by code
	if game, exists := s.CodeToGame[code]; exists {
		// Check if game is deleted (inactive)
		if !game.IsActive {
			return nil, fmt.Errorf("game has been deleted by admin and is no longer available")
		}
		return game, nil
	}

	// Game not found
	return nil, fmt.Errorf("invalid game code: %s", code)
}

// receivePlayerMessage reads and returns the next message from the WebSocket connection
func (s *Server) receivePlayerMessage(ws *websocket.Conn) (*ClientMessage, error) {
	var msg ClientMessage
	if err := websocket.JSON.Receive(ws, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// processPlayerMessage handles a message from a player and logs any errors internally
func (s *Server) processPlayerMessage(game *Game, player *Player, msg *ClientMessage) {
	switch msg.Action {
	case "win":
		if err := s.handlePlayerWin(game, player); err != nil {
			log.Printf("Error processing player message: %v", err)
			// Send error message back to the requesting player
			errMsg := ServerMessage{
				Type:    "error",
				Message: fmt.Sprintf("❌ %v", err),
			}
			if err := player.sendMessage(errMsg); err != nil {
				log.Printf("Failed to send error to player: %v", err)
			}
		}
	case "restart":
		if err := s.handleGameRestart(game, player); err != nil {
			log.Printf("Error restarting game: %v", err)
			// Send error message back to the requesting player
			errMsg := ServerMessage{
				Type:    "error",
				Message: fmt.Sprintf("❌ %v", err),
			}
			if err := player.sendMessage(errMsg); err != nil {
				log.Printf("Failed to send error to player: %v", err)
			}
		}
	}
}

// handlePlayerWin processes a win announcement from a player
func (s *Server) handlePlayerWin(game *Game, player *Player) error {
	log.Printf("Player %s announced a win!", player.ID)

	// Check if game is already ended
	if !game.IsActive {
		return fmt.Errorf("game has already ended with winner: %s", game.Winner)
	}

	// Verify player exists in game
	_, exists := game.GetPlayer(player.ID)
	if !exists {
		return fmt.Errorf("player %s not found in game", player.ID)
	}

	// Update game state
	game.IsActive = false
	game.Winner = player.ID
	game.EndedAt = time.Now()
	log.Printf("🏆 Player %s WON game %s!", player.ID, game.ID)

	// Record win in database (Phase 7.5)
	ctx := context.Background()
	if err := RecordWinInDB(ctx, s.DB, game, player.ID); err != nil {
		log.Printf("Warning: failed to record win in DB: %v", err)
	}

	// Archive the completed game
	s.archiveGame(game)

	// Create and broadcast win message
	winMsg := ServerMessage{
		Type:    "game_ended",
		GameID:  game.ID,
		Winner:  player.ID,
		HostID:  game.HostID,
		Message: fmt.Sprintf("Player %s has won!", player.ID),
	}

	// Check if host is still connected
	hostPlayer, hostExists := game.GetPlayer(game.HostID)
	if !hostExists || hostPlayer == nil {
		winMsg.Message += "\n❌ Host has disconnected. Game cannot be restarted."
		log.Printf("   ⚠️  Host is disconnected - game cannot be restarted")
	} else {
		log.Printf("   ✓ Host is connected - game can be restarted")
	}

	if err := s.broadcastToGame(game.ID, winMsg); err != nil {
		return err
	}

	log.Printf("Broadcasted win for player %s to all players in game %s", player.ID, game.ID)
	return nil
}

// handleGameRestart allows the host to restart a completed game with the same code and fresh board
func (s *Server) handleGameRestart(game *Game, player *Player) error {
	// Check if game is deleted (inactive)
	if !game.IsActive {
		return fmt.Errorf("Game has been deleted by admin and cannot be restarted")
	}

	// Check if player is the host
	if player.ID != game.HostID {
		// Non-host trying to restart - check if host is still connected
		hostPlayer, hostExists := game.GetPlayer(game.HostID)
		if !hostExists || hostPlayer == nil {
			return fmt.Errorf("❌ Host has disconnected. Game cannot be restarted.")
		}
		return fmt.Errorf("only the host can restart the game")
	}

	// Note: Game was already archived when it ended (in handlePlayerWin).
	// Restarting is a new session of the same game, don't archive again.

	// Reset the game for a fresh session
	game.ResetBoard(s.Buzzwords, s.Rows, s.Cols)

	log.Printf("🔄 Host %s restarted game %s with code: %s", player.ID, game.ID, game.Code)

	// Broadcast restart message to all players
	restartMsg := ServerMessage{
		Type:      "game_restart",
		GameID:    game.ID,
		Code:      game.Code,
		HostID:    game.HostID,
		Players:   game.GetPlayerList(),
		Buzzwords: s.Buzzwords,
		Rows:      s.Rows,
		Cols:      s.Cols,
		Message:   "Game restarted! New round begins.",
	}
	s.broadcastToGame(game.ID, restartMsg)

	return nil
}

// archiveGame saves a completed game session to the archive
func (s *Server) archiveGame(game *Game) {
	s.GamesMu.Lock()
	defer s.GamesMu.Unlock()

	archived := ArchivedGame{
		ID:        game.ID,
		Code:      game.Code,
		HostID:    game.HostID,
		Winner:    game.Winner,
		CreatedAt: game.CreatedAt,
		EndedAt:   time.Now(),
	}
	s.ArchivedGames = append(s.ArchivedGames, archived)
	log.Printf("📋 Archived game %s (code: %s)", game.ID, game.Code)
}

// forwardPlayerMessages forwards messages from player's channel to their WebSocket connection
func (s *Server) forwardPlayerMessages(player *Player, ws *websocket.Conn) {
	for msg := range player.messages.send {
		if err := websocket.JSON.Send(ws, msg); err != nil {
			log.Printf("Error sending message to %s: %v", player.ID, err)
			return
		}
	}
}

// handlePlayerDisconnect removes player from game and closes the connection
func (s *Server) handlePlayerDisconnect(game *Game, player *Player, ws *websocket.Conn) error {
	log.Printf("🔌 HandlePlayerDisconnect called for player %s, game %s (IsActive=%v, HostID=%s)",
		player.ID, game.ID, game.IsActive, game.HostID)

	game.RemovePlayer(player.ID)
	playerCount := game.PlayerCount()
	log.Printf("   After RemovePlayer: playerCount=%d", playerCount)

	// Update metrics (Phase 8)
	s.Metrics.PlayerCount.Set(float64(s.countTotalPlayers()))
	s.Metrics.PlayersDisconnectedTotal.Inc()

	// Broadcast disconnection messages if players remain
	if playerCount > 0 {
		s.broadcastDisconnectionMessages(game, player)
	}

	return s.closeConnection(ws, player)
}

// broadcastDisconnectionMessages notifies remaining players about the disconnection
func (s *Server) broadcastDisconnectionMessages(game *Game, player *Player) {
	playerCount := game.PlayerCount()

	// Host disconnection - notify everyone
	if player.ID == game.HostID {
		log.Printf("   ✓ Host disconnected, %d player(s) remaining", playerCount)

		// NOTE: HostID is immutable - DO NOT clear it. Host can reconnect and remains host.
		log.Printf("   ℹ️  Host ID preserved for potential reconnection: %s", game.HostID)

		// Send error message to keep non-hosts in postgame state if game is ended
		// or to warn them if game is still active
		errorMsg := ServerMessage{
			Type:    "error",
			GameID:  game.ID,
			Code:    game.Code,
			HostID:  game.HostID,
			Message: "❌ Host has disconnected. Game cannot be restarted.",
		}
		log.Printf("   📢 Broadcasting host disconnection error to remaining players")
		s.broadcastToGame(game.ID, errorMsg)
		return
	}

	// Non-host disconnection - send player_update
	log.Printf("   ℹ️ Non-host player disconnected, broadcasting player_update")
	updateMsg := ServerMessage{
		Type:    "player_update",
		GameID:  game.ID,
		Code:    game.Code,
		HostID:  game.HostID,
		Players: game.GetPlayerList(),
		Message: fmt.Sprintf("Player %s left the game", player.ID),
	}
	s.broadcastToGame(game.ID, updateMsg)
}

// closeConnection closes the WebSocket and logs
func (s *Server) closeConnection(ws *websocket.Conn, player *Player) error {
	if err := ws.Close(); err != nil {
		log.Printf("Error closing connection for player %s: %v", player.ID, err)
		return err
	}
	log.Printf("Player %s disconnected", player.ID)
	return nil
}

// handleStatus returns server status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.GamesMu.RLock()
	defer s.GamesMu.RUnlock()

	statusData := map[string]interface{}{
		"total_games": len(s.Games),
		"games":       len(s.CodeToGame),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(statusData)
}

// broadcastToGame sends a message to all players in a game
func (s *Server) broadcastToGame(gameID string, msg interface{}) error {
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

	log.Printf("   📡 BroadcastToGame %s: sending to %d player(s)", gameID, len(playersCopy))
	for _, player := range playersCopy {
		err := player.sendMessage(msg) // Non-blocking send
		if err != nil {
			log.Printf("     ⚠️ Failed to send to player %s: %v", player.ID, err)
		} else {
			log.Printf("     ✓ Message sent to player %s", player.ID)
		}
	}

	return nil
}

// countTotalPlayers counts all connected players across all games
func (s *Server) countTotalPlayers() int {
	s.GamesMu.RLock()
	defer s.GamesMu.RUnlock()
	total := 0
	for _, game := range s.Games {
		total += game.PlayerCount()
	}
	return total
}
