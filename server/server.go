package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jkMLnop/binGO/db"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/net/websocket"
	"golang.org/x/time/rate"
)

// Server manages the WebSocket server and game sessions
type Server struct {
	Games         map[string]*Game // gameID -> Game (for backward compatibility)
	CodeToGame    map[string]*Game // Code -> Game (Phase 7.3: code-based access)
	Rooms         map[string]*Room // roomCode -> Room (Phase 11.0)
	Buzzwords     [][]string
	Rows          int
	Cols          int
	GamesMu       sync.RWMutex
	RoomsMu       sync.RWMutex
	Port          string
	Server        *http.Server
	Mux           *http.ServeMux            // Custom mux for this server
	TokenManager  *TokenManager             // JWT token manager
	Sessions      map[string]*ClientSession // IP -> ClientSession for tracking usernames
	SessionsMu    sync.RWMutex
	DB            db.GameStore  // Database store (Phase 7.5)
	Metrics       *Metrics      // Prometheus metrics (Phase 8)
	Logger        *Logger       // Structured JSON logger (Phase 8)
	Tracer        trace.Tracer  // OTel tracer (Phase 8)
	cleanupStop   chan struct{} // signals startCleanupRoutine to exit
	StaticHandler http.Handler  // SPA web client handler (optional, Phase 7.6)
	// Rate-limiting state (Phase 8.8)
	ConnCounts     map[string]int           // active WS connections per IP
	ConnCountsMu   sync.Mutex               // protects ConnCounts
	CodeLimiters   map[string]*rate.Limiter // per-IP token-bucket for code guesses
	CodeLimitersMu sync.Mutex               // protects CodeLimiters
	// AI buzzword generation (Phase 12)
	LLMClient   LLMClient // nil when DeepSeek is unreachable or not configured
	FeedbackMu  sync.RWMutex
	LLMFeedback []LLMFeedbackEntry
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
		Rooms:        make(map[string]*Room),
		Buzzwords:    buzzwords,
		Rows:         rows,
		Cols:         cols,
		Port:         port,
		Mux:          mux,
		TokenManager: NewTokenManager(""), // Will generate random secret
		Sessions:     make(map[string]*ClientSession),
		ConnCounts:   make(map[string]int),
		CodeLimiters: make(map[string]*rate.Limiter),
		DB:           nil,
		Metrics:      NewMetrics(),
		Logger:       NewLogger(),
		Tracer:       trace.NewNoopTracerProvider().Tracer("bingo-server"),
		cleanupStop:  make(chan struct{}),
		LLMClient:    nil, // set by InitLLMClient after health probe
		LLMFeedback:  make([]LLMFeedbackEntry, 0, 128),
	}
	return srv
}

// SetDB sets the database store for this server
func (s *Server) SetDB(store db.GameStore) {
	s.DB = store
}

// SetTracer sets the OTel tracer for this server
func (s *Server) SetTracer(t trace.Tracer) {
	s.Tracer = t
}

// InitLLMClient probes the DeepSeek health endpoint and, if reachable, stores a
// configured DeepSeekClient with a background health check loop. When the API
// key is empty or DeepSeek is not reachable the LLMClient field stays nil and
// the generate-buzzwords endpoint returns HTTP 503.
func (s *Server) InitLLMClient(baseURL, apiKey, model string, enableThinking bool) {
	if strings.TrimSpace(apiKey) == "" {
		log.Printf("Warning: DEEPSEEK_API_KEY not set — AI buzzword generation disabled")
		return
	}
	client := NewDeepSeekClient(baseURL, apiKey, model)
	client.Thinking = enableThinking
	client.Metrics = s.Metrics
	// Start background health check loop (30s interval) so /api/status can
	// report llm_healthy without hitting the DeepSeek API on every request.
	client.StartHealthCheckLoop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if client.Healthy(ctx) {
		s.LLMClient = client
		log.Printf("DeepSeek LLM client ready (model: %s, thinking: %t, base: %s)", model, enableThinking, baseURL)
	} else {
		log.Printf("Warning: DeepSeek not reachable at %s — AI buzzword generation disabled", baseURL)
	}
}

// Start begins listening for connections
func (s *Server) Start() error {
	s.registerHandlers()
	// Create initial game and display code to users
	s.createNewGame()
	s.startCleanupRoutine()
	return s.startHTTPServer()
}

// registerHandlers registers all HTTP handlers
func (s *Server) registerHandlers() {
	s.Mux.Handle("/ws", s.wsConnLimitMiddleware(websocket.Handler(s.wsHandler)))
	s.Mux.HandleFunc("/status", s.handleStatus)

	// API handlers (Phase 7.5)
	s.Mux.HandleFunc("/api/status", s.handleAPIStatus)
	s.Mux.HandleFunc("/api/game/", s.handleGameRoutes)
	s.Mux.HandleFunc("/api/games", s.handlePublicCreateGame) // public game creation for web client
	s.Mux.HandleFunc("/api/leaderboard", s.handleGetLeaderboard)
	s.Mux.HandleFunc("/api/player/", s.handleGetPlayerStats) // Phase 9: player stats

	// Room API handlers (Phase 11.0)
	s.Mux.HandleFunc("/api/rooms", s.handleCreateRoom)
	s.Mux.HandleFunc("/api/room/", s.handleRoomRoutes)

	// Admin API handlers (Phase 8) - register both with and without trailing path
	s.Mux.HandleFunc("/admin/api/games", s.handleAdminGames)
	s.Mux.HandleFunc("/admin/api/games/", s.handleAdminGames)

	// Metrics endpoint (Phase 8)
	// If METRICS_AUTH_TOKEN is set, require "Authorization: Bearer <token>".
	// If unset (local dev / docker-compose), the endpoint is open.
	s.Mux.Handle("/metrics", metricsAuthMiddleware(promhttp.Handler()))

	// Agent observability endpoint (Phase 15.2)
	s.Mux.HandleFunc("/metrics/agent-event", s.handleAgentEvent)

	// Serve embedded web client at / (Phase 7.6 — set by bin.go when built with embed)
	if s.StaticHandler != nil {
		s.Mux.Handle("/", s.StaticHandler)
	}
}

// startHTTPServer creates and starts the HTTP server
func (s *Server) startHTTPServer() error {
	s.Server = &http.Server{
		Addr:    ":" + s.Port,
		Handler: otelhttp.NewHandler(s.Mux, "bingo-server"),
	}

	log.Printf("Server listening on port %s", s.Port)
	log.Printf("Running without TLS (using plain HTTP for testing)")
	return s.Server.ListenAndServe()
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	// Signal the cleanup goroutine to stop (idempotent via select)
	if s.cleanupStop != nil {
		select {
		case <-s.cleanupStop:
			// Already closed
		default:
			close(s.cleanupStop)
		}
	}
	if s.Server != nil {
		return s.Server.Shutdown(ctx)
	}
	return nil
}

// createNewGame creates a new game and registers it by code
func (s *Server) createNewGame() {
	s.GamesMu.Lock()
	defer s.GamesMu.Unlock()
	s.createNewGameLocked()
}

// createNewGameLocked creates a new game while GamesMu is already held.
// Caller must hold s.GamesMu.Lock().
func (s *Server) createNewGameLocked() {
	gameID := fmt.Sprintf("game-%d", len(s.Games)+1)
	newGame := NewGame(gameID, s.Buzzwords, s.Rows, s.Cols)
	s.Games[gameID] = newGame
	s.CodeToGame[newGame.Code] = newGame

	log.Printf("Created new game: %s with code: %s", gameID, newGame.Code)

	// Update metrics
	s.Metrics.GameCount.Set(float64(len(s.Games)))
	s.Metrics.GamesCreatedTotal.Inc()

	// Save game to database
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer dbCancel()
	spanCtx, span := s.Tracer.Start(dbCtx, "bingo.game.create")
	defer span.End()
	if err := SaveGameToDB(spanCtx, s.DB, newGame, s.Buzzwords); err != nil {
		log.Printf("Warning: failed to save game to DB: %v", err)
		s.Metrics.RecordError("db")
	}
}

// ensureActiveGameAvailable makes sure there is always at least one joinable game code.
// If all games are inactive (e.g., only orphaned/admin-deleted remain), it auto-creates one.
func (s *Server) ensureActiveGameAvailable() {
	s.GamesMu.Lock()
	defer s.GamesMu.Unlock()

	for _, g := range s.Games {
		if g != nil && g.IsActive {
			return
		}
	}

	s.createNewGameLocked()
	log.Printf("♻️ Auto-created replacement game because no active games remained")
}

// createGameForHost creates a new on-demand game for a host player (Phase 9).
// Custom buzzwords may be provided; if nil the host's DB profile is checked, then server defaults.
func (s *Server) createGameForHost(ctx context.Context, hostUsername string, customBuzzwords [][]string) (*Game, error) {
	buzzwords := s.Buzzwords

	if len(customBuzzwords) > 0 {
		// Caller provided an explicit buzzword list — use it
		buzzwords = customBuzzwords
	} else if s.DB != nil {
		// Try to load the host's approved buzzword list from their profile
		dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		host, err := s.DB.GetHostByUsername(dbCtx, hostUsername)
		if err == nil && host != nil && len(host.ApprovedBuzzwords) > 0 {
			var profileWords []string
			if jsonErr := json.Unmarshal(host.ApprovedBuzzwords, &profileWords); jsonErr == nil && len(profileWords) > 0 {
				// Wrap flat list into rows-of-one for the board generator
				rows := make([][]string, len(profileWords))
				for i, w := range profileWords {
					rows[i] = []string{w}
				}
				buzzwords = append(s.Buzzwords, rows...)
			}
		}
	}

	s.GamesMu.Lock()
	defer s.GamesMu.Unlock()

	gameID := fmt.Sprintf("game-%d-%d", len(s.Games)+1, time.Now().UnixNano())
	newGame := NewGame(gameID, buzzwords, s.Rows, s.Cols)
	s.Games[gameID] = newGame
	s.CodeToGame[newGame.Code] = newGame

	log.Printf("Host %s created game: %s with code: %s", hostUsername, gameID, newGame.Code)

	s.Metrics.GameCount.Set(float64(len(s.Games)))
	s.Metrics.GamesCreatedTotal.Inc()

	dbCtx, dbCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dbCancel()
	spanCtx, span := s.Tracer.Start(dbCtx, "bingo.game.create")
	defer span.End()
	if err := SaveGameToDB(spanCtx, s.DB, newGame, buzzwords); err != nil {
		log.Printf("Warning: failed to save host game to DB: %v", err)
		s.Metrics.RecordError("db")
	}

	return newGame, nil
}

// wsHandler handles incoming WebSocket connections - orchestrates the connection lifecycle
func (s *Server) wsHandler(ws *websocket.Conn) {
	ctx := ws.Request().Context()
	ctx, wsSpan := s.Tracer.Start(ctx, "bingo.ws.session", trace.WithSpanKind(trace.SpanKindServer))
	defer wsSpan.End()

	player, game, err := s.handlePlayerConnect(ctx, ws)
	if err != nil {
		log.Printf("Error handling player connect: %v", err)
		return
	}
	defer func() {
		if err := s.handlePlayerDisconnect(ctx, game, player, ws); err != nil {
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

		s.processPlayerMessage(ctx, game, player, msg)
	}
}

// handlePlayerConnect authenticates and welcomes a player, returns player and game or error
func (s *Server) handlePlayerConnect(ctx context.Context, ws *websocket.Conn) (*Player, *Game, error) {
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

	var game *Game
	if loginMsg.Action == "room_login" {
		// Phase 11.0: room_login — resolve room → game (lazy creation on first login)
		roomCode := loginMsg.RoomCode
		if roomCode == "" {
			errMsg := ServerMessage{Type: "error", Message: "room_code required for room_login"}
			websocket.JSON.Send(ws, errMsg)
			ws.Close()
			s.Metrics.RecordError("input")
			return nil, nil, fmt.Errorf("room_code required for room_login")
		}
		room, roomErr := s.getOrCreateRoom(strings.ToUpper(roomCode))
		if roomErr != nil {
			if !s.getCodeLimiter(clientIP).Allow() {
				s.Logger.RateLimitExceeded(clientIP, "code_guess", 0)
				s.Metrics.RecordRateLimit("code_guess")
				errMsg := ServerMessage{Type: "error", Message: "Too many failed attempts. Please wait before trying again."}
				websocket.JSON.Send(ws, errMsg)
				ws.Close()
				return nil, nil, fmt.Errorf("rate limited: too many invalid code attempts from %s", clientIP)
			}
			errMsg := ServerMessage{Type: "error", Message: roomErr.Error()}
			websocket.JSON.Send(ws, errMsg)
			ws.Close()
			return nil, nil, roomErr
		}
		// Lazily create the game the first time someone joins this room
		game = room.GetGame()
		if game == nil {
			var createErr error
			game, createErr = s.createGameForHost(ctx, username, nil)
			if createErr != nil {
				errMsg := ServerMessage{Type: "error", Message: createErr.Error()}
				websocket.JSON.Send(ws, errMsg)
				ws.Close()
				return nil, nil, createErr
			}
			room.SetGame(game)
		}
	} else if code == "" && loginMsg.Action == "host" {
		// Host flow: create a fresh game for this player (Phase 9)
		var createErr error
		game, createErr = s.createGameForHost(ctx, username, loginMsg.Buzzwords)
		if createErr != nil {
			errMsg := ServerMessage{Type: "error", Message: createErr.Error()}
			websocket.JSON.Send(ws, errMsg)
			ws.Close()
			return nil, nil, createErr
		}
	} else if code == "" {
		errMsg := ServerMessage{Type: "error", Message: "Game code required for all remote connections"}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		s.Metrics.RecordError("input")
		return nil, nil, fmt.Errorf("game code required")
	} else {
		var gameErr error
		game, gameErr = s.getOrCreateGame(code)
		if gameErr != nil {
			// Consume a token from the per-IP limiter; reject immediately when exhausted.
			if !s.getCodeLimiter(clientIP).Allow() {
				s.Logger.RateLimitExceeded(clientIP, "code_guess", 0)
				s.Metrics.RecordRateLimit("code_guess")
				errMsg := ServerMessage{Type: "error", Message: "Too many failed attempts. Please wait before trying again."}
				websocket.JSON.Send(ws, errMsg)
				ws.Close()
				return nil, nil, fmt.Errorf("rate limited: too many invalid code attempts from %s", clientIP)
			}
			errMsg := ServerMessage{Type: "error", Message: gameErr.Error()}
			websocket.JSON.Send(ws, errMsg)
			ws.Close()
			return nil, nil, gameErr
		}
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
		var playerErr error
		player, playerErr = s.createPlayerInGame(ctx, game, username)
		if playerErr != nil {
			errMsg := ServerMessage{Type: "error", Message: playerErr.Error()}
			websocket.JSON.Send(ws, errMsg)
			ws.Close()
			return nil, nil, playerErr
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

	// Store the WebSocket connection on the player for graceful shutdown
	player.SetWS(ws)

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
			s.Metrics.RecordError("auth")
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
		s.Metrics.RecordError("input")
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

// extractClientIP extracts the real client IP from the request.
// The server always runs behind a trusted proxy (Fly.io / ngrok), so the
// leftmost value in X-Forwarded-For is the original client IP.
func (s *Server) extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// XFF may be a comma-separated list; leftmost entry is the real client.
		if idx := strings.IndexByte(xff, ','); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
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
func (s *Server) createPlayerInGame(ctx context.Context, game *Game, playerID string) (*Player, error) {
	ctx, span := s.Tracer.Start(ctx, "bingo.player.create")
	defer span.End()

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

	// Record player in database with a 3s deadline
	dbCtx, dbCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dbCancel()
	if s.DB != nil {
		dbPlayerID, err := RecordPlayerInDB(dbCtx, s.DB, game.ID, playerID, "", isHost)
		if err != nil {
			log.Printf("Warning: failed to record player in DB: %v", err)
			s.Metrics.RecordError("db")
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
	// Use game-specific buzzwords if set (e.g. custom host upload), else server defaults
	buzzwords := game.Buzzwords
	if len(buzzwords) == 0 {
		buzzwords = s.Buzzwords
	}

	// Include room code if this game is attached to a room (Phase 11.0)
	roomCode := s.roomCodeForGame(game)

	welcomeMsg := ServerMessage{
		Type:      "welcome",
		GameID:    game.ID,
		Code:      game.Code,   // Phase 7.3: Include game code
		RoomCode:  roomCode,    // Phase 11.0: 5-char room code (empty for standalone)
		HostID:    game.HostID, // Include host player ID (immutable)
		PlayerID:  player.ID,
		Username:  player.ID, // username is the player ID
		Token:     token,     // Include JWT token
		Buzzwords: buzzwords,
		Rows:      s.Rows,
		Cols:      s.Cols,
		Players:   game.GetPlayerList(),
		Winner:    game.Winner,
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
		if !game.IsActive {
			s.Metrics.RecordError("game")
			if game.Orphaned {
				return nil, fmt.Errorf("game %s has ended: all players disconnected", code)
			}
			return nil, fmt.Errorf("game has been deleted by admin and is no longer available")
		}
		return game, nil
	}

	// Game not found
	s.Metrics.RecordError("game")
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
func (s *Server) processPlayerMessage(ctx context.Context, game *Game, player *Player, msg *ClientMessage) {
	sendErr := func(err error) {
		errMsg := ServerMessage{Type: "error", Message: fmt.Sprintf("❌ %v", err)}
		if sendE := player.sendMessage(errMsg); sendE != nil {
			log.Printf("Failed to send error to player: %v", sendE)
		}
	}

	switch msg.Action {
	case "win":
		if err := s.handlePlayerWin(ctx, game, player); err != nil {
			log.Printf("Error processing player message: %v", err)
			sendErr(err)
		}
	case "restart":
		if err := s.handleGameRestart(ctx, game, player); err != nil {
			log.Printf("Error restarting game: %v", err)
			sendErr(err)
		}
	case "suggest":
		if err := s.handlePlayerSuggest(game, player, msg.Phrase); err != nil {
			log.Printf("Error handling suggest: %v", err)
			sendErr(err)
		}
	case "approve":
		if err := s.handleHostApprove(ctx, game, player, msg.Phrase); err != nil {
			log.Printf("Error handling approve: %v", err)
			sendErr(err)
		}
	case "reject":
		if err := s.handleHostReject(game, player, msg.Phrase); err != nil {
			log.Printf("Error handling reject: %v", err)
			sendErr(err)
		}
	case "bet":
		if err := s.handlePlayerBet(game, player, msg.Phrase); err != nil {
			log.Printf("Error handling bet: %v", err)
			sendErr(err)
		}
	case "list_buzzwords":
		s.handleListBuzzwords(game, player)
	}
}

// handlePlayerWin processes a win announcement from a player
func (s *Server) handlePlayerWin(ctx context.Context, game *Game, player *Player) error {
	ctx, span := s.Tracer.Start(ctx, "bingo.game.win")
	defer span.End()
	log.Printf("Player %s announced a win!", player.ID)

	// Check if game is already ended (admin-deleted/orphaned OR already has a winner)
	if !game.IsActive || game.Winner != "" {
		s.Metrics.RecordError("game")
		return fmt.Errorf("game has already ended with winner: %s", game.Winner)
	}

	// Verify player exists in game
	_, exists := game.GetPlayer(player.ID)
	if !exists {
		s.Metrics.RecordError("game")
		return fmt.Errorf("player %s not found in game", player.ID)
	}

	// Update game state (don't set IsActive=false; keep it true for restart)
	// IsActive=false only for admin-deleted or orphaned games
	game.Winner = player.ID
	game.EndedAt = time.Now()
	log.Printf("🏆 Player %s WON game %s!", player.ID, game.ID)

	// Record win in database with a 3s deadline.
	// Resolve room code for scoped leaderboard (nil when no room).
	dbCtx, dbCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dbCancel()
	roomCode := s.roomCodeForGame(game)
	if err := RecordWinInDB(dbCtx, s.DB, game, player.ID, roomCode); err != nil {
		log.Printf("Warning: failed to record win in DB: %v", err)
		s.Metrics.RecordError("db")
	}

	// Archive the completed game
	s.archiveGame(ctx, game)

	// Evaluate any active bets now that we know the winner (Phase 9.5)
	s.evaluateBets(game, player.ID)

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
func (s *Server) handleGameRestart(ctx context.Context, game *Game, player *Player) error {
	ctx, span := s.Tracer.Start(ctx, "bingo.game.restart")
	defer span.End()
	// Check if game is deleted (inactive) or orphaned
	if !game.IsActive || game.Orphaned {
		s.Metrics.RecordError("game")
		return fmt.Errorf("Game has been deleted by admin and cannot be restarted")
	}

	// Check if player is the host
	if player.ID != game.HostID {
		// Non-host trying to restart - check if host is still connected
		hostPlayer, hostExists := game.GetPlayer(game.HostID)
		if !hostExists || hostPlayer == nil {
			s.Metrics.RecordError("game")
			return fmt.Errorf("❌ Host has disconnected. Game cannot be restarted.")
		}
		s.Metrics.RecordError("game")
		return fmt.Errorf("only the host can restart the game")
	}

	// Note: Game was already archived when it ended (in handlePlayerWin).
	// Restarting is a new session of the same game, don't archive again.

	// Reset the game for a fresh session
	game.ResetBoard(game.Buzzwords, s.Rows, s.Cols)

	log.Printf("🔄 Host %s restarted game %s with code: %s", player.ID, game.ID, game.Code)

	// Use game-specific buzzwords if set, else server defaults
	restartBuzzwords := game.Buzzwords
	if len(restartBuzzwords) == 0 {
		restartBuzzwords = s.Buzzwords
	}

	// Broadcast restart message to all players
	restartMsg := ServerMessage{
		Type:      "game_restart",
		GameID:    game.ID,
		Code:      game.Code,
		HostID:    game.HostID,
		Players:   game.GetPlayerList(),
		Buzzwords: restartBuzzwords,
		Rows:      s.Rows,
		Cols:      s.Cols,
		Message:   "Game restarted! New round begins.",
	}
	s.broadcastToGame(game.ID, restartMsg)

	return nil
}

// handlePlayerSuggest adds a buzzword suggestion to the game's pending queue and broadcasts it (Phase 9)
func (s *Server) handlePlayerSuggest(game *Game, player *Player, phrase string) error {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return fmt.Errorf("suggestion phrase cannot be empty")
	}
	if len(phrase) > 100 {
		return fmt.Errorf("suggestion phrase too long (max 100 characters)")
	}

	game.SuggestionsMu.Lock()
	// Check for duplicate
	for _, sug := range game.Suggestions {
		if strings.EqualFold(sug.Phrase, phrase) {
			game.SuggestionsMu.Unlock()
			return fmt.Errorf("phrase %q is already pending suggestion", phrase)
		}
	}
	game.Suggestions = append(game.Suggestions, Suggestion{
		PlayerID: player.ID,
		Phrase:   phrase,
	})
	snapshot := make([]Suggestion, len(game.Suggestions))
	copy(snapshot, game.Suggestions)
	game.SuggestionsMu.Unlock()

	s.broadcastSuggestionsUpdate(game, snapshot)
	return nil
}

// handleHostApprove approves a suggestion, appends it to host's DB profile, and broadcasts (Phase 9)
func (s *Server) handleHostApprove(ctx context.Context, game *Game, player *Player, phrase string) error {
	phrase = strings.TrimSpace(phrase)
	if player.ID != game.HostID {
		return fmt.Errorf("only the host can approve suggestions")
	}

	game.SuggestionsMu.Lock()
	found := false
	remaining := game.Suggestions[:0]
	for _, sug := range game.Suggestions {
		if strings.EqualFold(sug.Phrase, phrase) {
			found = true
		} else {
			remaining = append(remaining, sug)
		}
	}
	if !found {
		game.SuggestionsMu.Unlock()
		return fmt.Errorf("no pending suggestion matches %q", phrase)
	}
	game.Suggestions = remaining
	snapshot := make([]Suggestion, len(remaining))
	copy(snapshot, remaining)
	game.SuggestionsMu.Unlock()

	// Persist approved phrase to host profile in DB (nil-safe, append semantics)
	dbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if s.DB != nil {
		existingHost, loadErr := HostProfileFromDB(dbCtx, s.DB, game.HostID)
		var existingWords []string
		if loadErr == nil && existingHost != nil && len(existingHost.ApprovedBuzzwords) > 0 {
			// Unmarshal existing list to append to it
			_ = json.Unmarshal(existingHost.ApprovedBuzzwords, &existingWords)
		}
		updatedWords := append(existingWords, phrase)
		if saveErr := SaveHostProfileToDB(dbCtx, s.DB, game.HostID, game.HostID, updatedWords); saveErr != nil {
			log.Printf("Warning: failed to save approved buzzword to host profile: %v", saveErr)
		}
	}

	// Also append to the in-memory game buzzword pool so it shows up immediately
	// in list_buzzwords and is available on the next restart of this game.
	game.SuggestionsMu.Lock()
	game.Buzzwords = append(game.Buzzwords, []string{phrase})
	game.SuggestionsMu.Unlock()

	s.broadcastSuggestionsUpdate(game, snapshot)
	approvedMsg := ServerMessage{
		Type:    "suggestion_broadcast",
		GameID:  game.ID,
		Message: fmt.Sprintf("✅ Host approved buzzword: \"%s\"", phrase),
	}
	_ = s.broadcastToGame(game.ID, approvedMsg)
	return nil
}

// handleHostReject removes a suggestion without saving it (Phase 9)
func (s *Server) handleHostReject(game *Game, player *Player, phrase string) error {
	phrase = strings.TrimSpace(phrase)
	if player.ID != game.HostID {
		return fmt.Errorf("only the host can reject suggestions")
	}

	game.SuggestionsMu.Lock()
	found := false
	remaining := game.Suggestions[:0]
	for _, sug := range game.Suggestions {
		if strings.EqualFold(sug.Phrase, phrase) {
			found = true
		} else {
			remaining = append(remaining, sug)
		}
	}
	if !found {
		game.SuggestionsMu.Unlock()
		return fmt.Errorf("no pending suggestion matches %q", phrase)
	}
	game.Suggestions = remaining
	snapshotSugs := make([]Suggestion, len(remaining))
	copy(snapshotSugs, remaining)
	game.RejectedSuggestions = append(game.RejectedSuggestions, phrase)
	game.SuggestionsMu.Unlock()

	s.broadcastSuggestionsUpdate(game, snapshotSugs)
	rejectedMsg := ServerMessage{
		Type:    "suggestion_broadcast",
		GameID:  game.ID,
		Message: fmt.Sprintf("❌ Host rejected suggestion: \"%s\"", phrase),
	}
	_ = s.broadcastToGame(game.ID, rejectedMsg)
	return nil
}

// broadcastSuggestionsUpdate sends the current suggestions list to all players (Phase 9)
func (s *Server) broadcastSuggestionsUpdate(game *Game, suggestions []Suggestion) {
	msg := ServerMessage{
		Type:        "suggestion_broadcast",
		GameID:      game.ID,
		Suggestions: suggestions,
	}
	_ = s.broadcastToGame(game.ID, msg)
}

// handleListBuzzwords responds to the requesting player with the full buzzword pool
// and the list of phrases rejected by the host this round (Phase 9.6).
func (s *Server) handleListBuzzwords(game *Game, player *Player) {
	// Flatten [][]string pool into a single []string for readability
	flat := make([]string, 0)
	for _, group := range game.Buzzwords {
		flat = append(flat, group...)
	}

	game.SuggestionsMu.Lock()
	rejected := make([]string, len(game.RejectedSuggestions))
	copy(rejected, game.RejectedSuggestions)
	game.SuggestionsMu.Unlock()

	msg := ServerMessage{
		Type:                "buzzword_list",
		GameID:              game.ID,
		FlatBuzzwords:       flat,
		RejectedSuggestions: rejected,
	}
	_ = player.sendMessage(msg)
}

// handlePlayerBet parses and registers a bet for the round (Phase 9.5)
func (s *Server) handlePlayerBet(game *Game, player *Player, rawText string) error {
	rawText = strings.TrimSpace(rawText)
	if rawText == "" {
		return fmt.Errorf("bet text cannot be empty — usage: bet: <player> wins|loses [AND ...]")
	}
	if game.Winner != "" {
		return fmt.Errorf("bets are closed — game has already ended")
	}

	conditions, err := parseBetConditions(rawText, game)
	if err != nil {
		return err
	}

	game.BetsMu.Lock()
	// One active bet per player per round
	for _, b := range game.Bets {
		if b.BetterID == player.ID && b.Status == "active" {
			game.BetsMu.Unlock()
			return fmt.Errorf("you already have an active bet — wait for results before placing another")
		}
	}
	betID := fmt.Sprintf("bet-%s-%d", player.ID, time.Now().UnixNano())
	game.Bets = append(game.Bets, GameBet{
		ID:             betID,
		BetterID:       player.ID,
		BetterUsername: player.ID,
		RawText:        rawText,
		Conditions:     conditions,
		Status:         "active",
	})
	snapshot := make([]GameBet, len(game.Bets))
	copy(snapshot, game.Bets)
	game.BetsMu.Unlock()

	s.broadcastBetsUpdate(game, snapshot)
	return nil
}

// parseBetConditions parses an AND-joined bet string into GameBetCondition slice (Phase 9.5)
// Format: "<player> wins|loses [AND <player> wins|loses]"
func parseBetConditions(rawText string, game *Game) ([]GameBetCondition, error) {
	parts := strings.Split(strings.ToLower(rawText), " and ")
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid bet format — usage: <player> wins|loses [AND ...]")
	}

	// Snapshot player names for validation (IDs are usernames)
	game.PlayersMu.RLock()
	playerNames := make(map[string]bool, len(game.Players))
	for id := range game.Players {
		playerNames[strings.ToLower(id)] = true
	}
	game.PlayersMu.RUnlock()

	conditions := make([]GameBetCondition, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		tokens := strings.Fields(part)
		if len(tokens) != 2 {
			return nil, fmt.Errorf("invalid condition %q — expected \"<player> wins\" or \"<player> loses\"", part)
		}
		playerName := tokens[0]
		outcome := tokens[1]
		if outcome != "wins" && outcome != "loses" {
			return nil, fmt.Errorf("invalid outcome %q — must be \"wins\" or \"loses\"", outcome)
		}
		if !playerNames[playerName] {
			return nil, fmt.Errorf("player %q not found in this game", playerName)
		}
		conditions = append(conditions, GameBetCondition{
			PlayerUsername: playerName,
			Outcome:        outcome,
		})
	}
	return conditions, nil
}

// evaluateBets resolves all active bets against the winner and broadcasts results (Phase 9.5)
func (s *Server) evaluateBets(game *Game, winnerID string) {
	winnerLower := strings.ToLower(winnerID)

	game.BetsMu.Lock()
	for i := range game.Bets {
		if game.Bets[i].Status != "active" {
			continue
		}
		allMet := true
		for _, cond := range game.Bets[i].Conditions {
			condMet := false
			switch cond.Outcome {
			case "wins":
				condMet = strings.ToLower(cond.PlayerUsername) == winnerLower
			case "loses":
				condMet = strings.ToLower(cond.PlayerUsername) != winnerLower
			}
			if !condMet {
				allMet = false
				break
			}
		}
		if allMet {
			game.Bets[i].Status = "won"
		} else {
			game.Bets[i].Status = "lost"
		}
	}
	snapshot := make([]GameBet, len(game.Bets))
	copy(snapshot, game.Bets)
	game.BetsMu.Unlock()

	s.broadcastBetsUpdate(game, snapshot)
}

// broadcastBetsUpdate sends the current bets list to all players (Phase 9.5)
func (s *Server) broadcastBetsUpdate(game *Game, bets []GameBet) {
	msg := ServerMessage{
		Type:       "bets_update",
		GameID:     game.ID,
		ActiveBets: bets,
	}
	_ = s.broadcastToGame(game.ID, msg)
}

// archiveGame persists a completed game session to the database
func (s *Server) archiveGame(ctx context.Context, game *Game) {
	ctx, span := s.Tracer.Start(ctx, "bingo.game.archive")
	defer span.End()
	dbCtx, dbCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dbCancel()
	if err := ArchiveGameInDB(dbCtx, s.DB, game); err != nil {
		log.Printf("Warning: failed to archive game in DB: %v", err)
		s.Metrics.RecordError("db")
	}
	s.Metrics.GameArchived.Inc()
	log.Printf("📋 Archived game %s (code: %s)", game.ID, game.Code)
}

// markGameOrphaned marks a game as orphaned (all players left before a winner was declared),
// ends it, and archives it so the code can be cleanly reused.
func (s *Server) markGameOrphaned(ctx context.Context, game *Game) {
	game.IsActive = false
	game.Orphaned = true
	game.EndedAt = time.Now()
	log.Printf("🕳️  Game %s (code: %s) orphaned — all players disconnected without a winner", game.ID, game.Code)
	s.archiveGame(ctx, game)
	s.ensureActiveGameAvailable()
}

// NotifyShutdown broadcasts a server_shutdown message to every connected player and
// closes their WebSocket connections so the HTTP server can drain cleanly.
func (s *Server) NotifyShutdown() {
	shutdownMsg := ServerMessage{
		Type:    "server_shutdown",
		Message: "⚠️ Server is shutting down. Please reconnect later.",
	}

	s.GamesMu.RLock()
	defer s.GamesMu.RUnlock()

	// First pass: deliver the shutdown message to every player.
	// For players with a live WebSocket we write directly (bypassing the async
	// message channel) so the frame is guaranteed to be on the wire before we
	// close the connection. For players without a live WebSocket (e.g. created
	// in unit tests) we fall back to the channel so callers can still verify
	// that the message was produced.
	var wsConns []*websocket.Conn

	notified := 0
	for _, game := range s.Games {
		game.PlayersMu.RLock()
		for _, p := range game.Players {
			p.wsMu.Lock()
			if p.ws != nil {
				_ = websocket.JSON.Send(p.ws, shutdownMsg)
				wsConns = append(wsConns, p.ws)
			} else {
				_ = p.sendMessage(shutdownMsg)
			}
			p.wsMu.Unlock()
			notified++
		}
		game.PlayersMu.RUnlock()
	}

	if notified > 0 {
		log.Printf("🛑 Notified %d player(s) of server shutdown", notified)
		// Give clients time to receive and process the message before we close.
		time.Sleep(500 * time.Millisecond)
		for _, ws := range wsConns {
			ws.Close()
		}
	}
}

// startCleanupRoutine starts a background goroutine that periodically removes
// game archive records older than 4 days.
func (s *Server) startCleanupRoutine() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		s.runArchiveCleanup() // Run once immediately on startup
		for {
			select {
			case <-ticker.C:
				s.runArchiveCleanup()
				s.cleanupRateLimiters()
			case <-s.cleanupStop:
				return
			}
		}
	}()
}

// runArchiveCleanup deletes old entries from the game_archives table
func (s *Server) runArchiveCleanup() {
	if s.DB == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := s.DB.CleanupOldArchives(ctx)
	if err != nil {
		log.Printf("Warning: archive cleanup failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("🧹 Cleaned up %d old game archive(s) (older than 4 days)", n)
	}
}

// forwardPlayerMessages forwards messages from player's channel to their WebSocket connection
func (s *Server) forwardPlayerMessages(player *Player, ws *websocket.Conn) {
	for msg := range player.messages.send {
		if err := websocket.JSON.Send(ws, msg); err != nil {
			log.Printf("Error sending message to %s: %v", player.ID, err)
			s.Metrics.RecordError("ws")
			return
		}
	}
}

// handlePlayerDisconnect removes player from game and closes the connection
func (s *Server) handlePlayerDisconnect(ctx context.Context, game *Game, player *Player, ws *websocket.Conn) error {
	log.Printf("🔌 HandlePlayerDisconnect called for player %s, game %s (IsActive=%v, HostID=%s)",
		player.ID, game.ID, game.IsActive, game.HostID)

	game.RemovePlayer(player.ID)
	playerCount := game.PlayerCount()
	log.Printf("   After RemovePlayer: playerCount=%d", playerCount)

	// Update metrics
	s.Metrics.PlayerCount.Set(float64(s.countTotalPlayers()))
	s.Metrics.PlayersDisconnectedTotal.Inc()

	// Orphaned game detection: all players left an active game with no winner.
	// Note: IsActive stays true after a win (to allow restart), so we must also
	// check that no winner was declared before marking the game orphaned.
	if playerCount == 0 && game.IsActive && game.Winner == "" {
		s.markGameOrphaned(ctx, game)
	}

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
