package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	AdminKeyHeader  = "X-Admin-Key"
	AdminKeyEnvVar  = "ADMIN_API_KEY"
	DefaultAdminKey = "dev-admin-key-local-only" // Safe default for local development
)

// handleAdminGames routes requests to appropriate admin game handlers
// Handles POST (create), GET (list), and DELETE (close) operations
func (s *Server) handleAdminGames(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.Metrics.AdminAPIRequestsTotal.Inc()

	// Check if this is a request to a specific game (has path segment after /admin/api/games/)
	path := r.URL.Path
	// Remove the /admin/api/games prefix to check for ID
	suffix := strings.TrimPrefix(path, "/admin/api/games/")

	if suffix != "" && suffix != path { // There is content after /admin/api/games/
		// This is a request to /admin/api/games/{id}
		if r.Method == http.MethodGet {
			s.handleGetGameDetail(w, r)
		} else if r.Method == http.MethodDelete {
			s.handleDeleteGame(w, r)
		} else {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	} else {
		// This is a request to /admin/api/games (without ID)
		if r.Method == http.MethodPost {
			s.handleCreateGame(w, r)
		} else if r.Method == http.MethodGet {
			s.handleListGames(w, r)
		} else {
			writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}

	// Record API latency
	duration := time.Since(start).Milliseconds()
	s.Metrics.AdminAPILatency.Observe(float64(duration))
}

// adminKeyMiddleware validates the X-Admin-Key header against the configured ADMIN_API_KEY
// Returns true if the request is authorized, false otherwise
func (s *Server) adminKeyMiddleware(w http.ResponseWriter, r *http.Request) bool {
	adminKey := os.Getenv(AdminKeyEnvVar)
	if adminKey == "" {
		// Default to dev key for local development
		adminKey = DefaultAdminKey
		log.Printf("ADMIN_API_KEY not set, using default dev key")
	}

	providedKey := r.Header.Get(AdminKeyHeader)
	if providedKey == "" {
		writeAPIError(w, http.StatusUnauthorized, "missing X-Admin-Key header")
		return false
	}

	if providedKey != adminKey {
		writeAPIError(w, http.StatusForbidden, "invalid X-Admin-Key")
		s.Logger.Error("admin_auth_failed", "Admin authentication failed", nil, map[string]interface{}{
			"ip": r.RemoteAddr,
		})
		return false
	}

	return true
}

// handleCreateGame creates a new game with optional players
// POST /admin/api/games
// Body: optional {players: ["player1", "player2"]}
func (s *Server) handleCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !s.adminKeyMiddleware(w, r) {
		return
	}

	type CreateGameRequest struct {
		Players []string `json:"players"`
	}

	var req CreateGameRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Body is optional - ignore decode errors
			req.Players = []string{}
		}
	}

	// Record creation latency
	start := time.Now()

	s.GamesMu.Lock()
	gameID := fmt.Sprintf("game-%d", len(s.Games)+1)
	newGame := NewGame(gameID, s.Buzzwords, s.Rows, s.Cols)
	s.Games[gameID] = newGame
	s.CodeToGame[newGame.Code] = newGame
	s.GamesMu.Unlock()

	// Record creation duration
	duration := time.Since(start).Milliseconds()
	s.Metrics.GameCreationDuration.Observe(float64(duration))

	// Update metrics
	s.Metrics.GameCount.Set(float64(len(s.Games)))
	s.Metrics.GamesCreatedTotal.Inc()

	s.Logger.GameCreated(gameID, newGame.Code, newGame.HostID, map[string]interface{}{
		"admin_created": true,
		"players":       len(req.Players),
	})

	gameInfo := GameInfo{
		ID:          newGame.ID,
		Code:        newGame.Code,
		HostID:      newGame.HostID,
		Status:      getGameStatus(newGame),
		PlayerCount: newGame.PlayerCount(),
		CreatedAt:   newGame.CreatedAt.Unix(),
	}

	writeAPISuccess(w, gameInfo)
}

// handleListGames lists all active games
// GET /admin/api/games
func (s *Server) handleListGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !s.adminKeyMiddleware(w, r) {
		return
	}

	s.GamesMu.RLock()
	games := make([]GameInfo, 0, len(s.Games))
	for _, game := range s.Games {
		games = append(games, GameInfo{
			ID:          game.ID,
			Code:        game.Code,
			HostID:      game.HostID,
			Status:      getGameStatus(game),
			PlayerCount: game.PlayerCount(),
			CreatedAt:   game.CreatedAt.Unix(),
		})
	}
	s.GamesMu.RUnlock()

	// Log the action (using Error with nil error for info-level logging)
	s.Logger.Error("admin_list_games", "Admin listed games", nil, map[string]interface{}{
		"game_count": len(games),
	})

	response := map[string]interface{}{
		"games": games,
		"count": len(games),
	}
	writeAPISuccess(w, response)
}

// handleGetGameDetail retrieves detailed state of a specific game
// GET /admin/api/games/{id}
func (s *Server) handleGetGameDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !s.adminKeyMiddleware(w, r) {
		return
	}

	// Extract game ID from path: /admin/api/games/GAME_ID
	parts := r.URL.Path[len("/admin/api/games/"):]
	if parts == "" {
		writeAPIError(w, http.StatusBadRequest, "missing game id")
		return
	}

	s.GamesMu.RLock()
	game, exists := s.Games[parts]
	s.GamesMu.RUnlock()

	if !exists || game == nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("game %s not found", parts))
		return
	}

	// Build detailed game state
	players := make([]map[string]interface{}, 0)
	for _, player := range game.Players {
		players = append(players, map[string]interface{}{
			"id": player.ID,
		})
	}

	gameDetail := map[string]interface{}{
		"id":           game.ID,
		"code":         game.Code,
		"host_id":      game.HostID,
		"status":       getGameStatus(game),
		"player_count": game.PlayerCount(),
		"created_at":   game.CreatedAt.Unix(),
		"players":      players,
		"is_active":    game.IsActive,
	}

	s.Logger.Error("admin_get_detail", "Admin retrieved game detail", nil, map[string]interface{}{
		"game_id": parts,
	})

	writeAPISuccess(w, gameDetail)
}

// handleDeleteGame force closes a game
// DELETE /admin/api/games/{id}
func (s *Server) handleDeleteGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !s.adminKeyMiddleware(w, r) {
		return
	}

	// Extract game ID from path: /admin/api/games/GAME_ID
	parts := r.URL.Path[len("/admin/api/games/"):]
	if parts == "" {
		writeAPIError(w, http.StatusBadRequest, "missing game id")
		return
	}

	s.GamesMu.Lock()
	game, exists := s.Games[parts]
	if !exists || game == nil {
		s.GamesMu.Unlock()
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("game %s not found", parts))
		return
	}

	// Mark game as inactive
	game.IsActive = false

	// Remove from code mapping
	delete(s.CodeToGame, game.Code)

	s.GamesMu.Unlock()

	// Broadcast deletion notice to all connected players (same as host disconnect pattern)
	deletionMsg := ServerMessage{
		Type:    "error",
		GameID:  game.ID,
		Code:    game.Code,
		HostID:  game.HostID,
		Message: "⚠️  Game has been closed by admin. Play can continue but the game cannot be won or restarted.",
	}
	log.Printf("📢 Broadcasting game deletion notice to players in %s", parts)
	s.broadcastToGame(game.ID, deletionMsg)

	s.Logger.Error("admin_delete_game", "Admin deleted game", nil, map[string]interface{}{
		"game_id": parts,
	})

	response := map[string]interface{}{
		"message": fmt.Sprintf("Game %s closed successfully", parts),
		"game_id": parts,
	}
	writeAPISuccess(w, response)
}
