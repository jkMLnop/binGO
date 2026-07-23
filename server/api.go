package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// APIResponse is a standard API response format
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// GameInfo represents game metadata for API responses
type GameInfo struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	HostID      string `json:"host_id"`
	Status      string `json:"status"`
	Winner      string `json:"winner,omitempty"`
	PlayerCount int    `json:"player_count"`
	CreatedAt   int64  `json:"created_at"`
}

// LeaderboardEntry represents a player's leaderboard position
type LeaderboardEntryResponse struct {
	Username string `json:"username"`
	Wins     int    `json:"wins"`
	Rank     int    `json:"rank"`
}

// handleGameRoutes dispatches sub-routes under /api/game/:code/*.
func (s *Server) handleGameRoutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/game/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		writeAPIError(w, http.StatusBadRequest, "missing game code")
		return
	}

	code := strings.ToUpper(parts[0])
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}
	if sub == "" && r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch {
	case sub == "" && r.Method == http.MethodGet:
		s.handleGetGameByCode(w, r)
	case sub == "buzzwords" && r.Method == http.MethodPost:
		s.handleSetGameBuzzwords(w, r, code)
	case sub == "feedback" && r.Method == http.MethodPost:
		s.handleSubmitGameBuzzwordFeedback(w, r, code)
	case sub == "generate-buzzwords" && r.Method == http.MethodPost:
		s.handleGenerateBuzzwordsForGame(w, r, code)
	default:
		writeAPIError(w, http.StatusNotFound, "not found")
	}
}

// handleGetGameByCode retrieves game information by code
// GET /api/game/:code
func (s *Server) handleGetGameByCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract code from path: /api/game/CODE
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		writeAPIError(w, http.StatusBadRequest, "missing game code")
		return
	}

	code := parts[3]
	if code == "" {
		writeAPIError(w, http.StatusBadRequest, "game code cannot be empty")
		return
	}

	// First check in-memory games (backward compatibility)
	s.GamesMu.RLock()
	game, exists := s.CodeToGame[code]
	s.GamesMu.RUnlock()

	if exists && game != nil {
		// Return game info
		gameInfo := GameInfo{
			ID:          game.ID,
			Code:        game.Code,
			HostID:      game.HostID,
			Status:      getGameStatus(game),
			Winner:      game.Winner,
			PlayerCount: game.PlayerCount(),
			CreatedAt:   game.CreatedAt.Unix(),
		}
		writeAPISuccess(w, gameInfo)
		return
	}

	// If not in-memory, check database (Phase 7.5)
	if s.DB != nil {
		ctx := r.Context()
		dbGame, err := s.DB.GetGameByCode(ctx, code)
		if err == nil && dbGame != nil {
			gameInfo := GameInfo{
				ID:        dbGame.ID,
				Code:      dbGame.Code,
				HostID:    dbGame.HostID,
				Status:    dbGame.Status,
				CreatedAt: dbGame.CreatedAt,
			}
			writeAPISuccess(w, gameInfo)
			return
		}
	}

	// Game not found
	writeAPIError(w, http.StatusNotFound, fmt.Sprintf("game code %s not found", code))
}

// handlePublicCreateGame creates a new game without requiring an admin key.
// POST /api/games — intended for web client "Host a new game" button.
func (s *Server) handlePublicCreateGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	start := time.Now()

	s.GamesMu.Lock()
	gameID := fmt.Sprintf("game-%d", len(s.Games)+1)
	newGame := NewGame(gameID, s.Buzzwords, s.Rows, s.Cols)
	s.Games[gameID] = newGame
	s.CodeToGame[newGame.Code] = newGame
	s.GamesMu.Unlock()

	s.Metrics.GameCreationDuration.Observe(float64(time.Since(start).Milliseconds()))
	s.Metrics.GameCount.Set(float64(len(s.Games)))
	s.Metrics.GamesCreatedTotal.Inc()

	s.Logger.GameCreated(gameID, newGame.Code, newGame.HostID, map[string]interface{}{
		"admin_created": false,
	})

	writeAPISuccess(w, GameInfo{
		ID:          newGame.ID,
		Code:        newGame.Code,
		HostID:      newGame.HostID,
		Status:      getGameStatus(newGame),
		PlayerCount: newGame.PlayerCount(),
		CreatedAt:   newGame.CreatedAt.Unix(),
	})
}

// handleGetLeaderboard retrieves top players
// GET /api/leaderboard?limit=10&sort=wins|win_rate|games_played
func (s *Server) handleGetLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Parse limit query parameter
	limit := 10 // default
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	sort := r.URL.Query().Get("sort") // "wins" (default), "win_rate", "games_played"

	// Only available with database
	if s.DB == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "leaderboard not available - database not enabled")
		return
	}

	ctx := r.Context()

	// For win_rate and games_played sorts we fetch stats per top player
	if sort == "win_rate" || sort == "games_played" {
		entries, err := s.DB.GetLeaderboard(ctx, 100) // fetch more to sort
		if err != nil {
			log.Printf("Error retrieving leaderboard: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "failed to retrieve leaderboard")
			return
		}

		type enriched struct {
			Username    string  `json:"username"`
			Wins        int     `json:"wins"`
			GamesPlayed int     `json:"games_played"`
			WinRate     float64 `json:"win_rate"`
			Rank        int     `json:"rank"`
		}
		enriched_entries := make([]enriched, 0, len(entries))
		for _, e := range entries {
			stats, err := s.DB.GetPlayerStats(ctx, e.Username)
			if err != nil || stats == nil {
				enriched_entries = append(enriched_entries, enriched{Username: e.Username, Wins: e.Wins})
				continue
			}
			enriched_entries = append(enriched_entries, enriched{
				Username:    stats.Username,
				Wins:        stats.Wins,
				GamesPlayed: stats.GamesPlayed,
				WinRate:     stats.WinRate,
			})
		}

		// Sort by requested metric
		for i := 1; i < len(enriched_entries); i++ {
			for j := i; j > 0; j-- {
				swap := false
				if sort == "win_rate" && enriched_entries[j].WinRate > enriched_entries[j-1].WinRate {
					swap = true
				} else if sort == "games_played" && enriched_entries[j].GamesPlayed > enriched_entries[j-1].GamesPlayed {
					swap = true
				}
				if swap {
					enriched_entries[j], enriched_entries[j-1] = enriched_entries[j-1], enriched_entries[j]
				} else {
					break
				}
			}
		}
		if len(enriched_entries) > limit {
			enriched_entries = enriched_entries[:limit]
		}
		for i := range enriched_entries {
			enriched_entries[i].Rank = i + 1
		}
		writeAPISuccess(w, enriched_entries)
		return
	}

	// Default: sort by wins
	entries, err := s.DB.GetLeaderboard(ctx, limit)
	if err != nil {
		log.Printf("Error retrieving leaderboard: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "failed to retrieve leaderboard")
		return
	}

	// Convert to response format with ranks
	response := make([]LeaderboardEntryResponse, 0, len(entries))
	for i, entry := range entries {
		response = append(response, LeaderboardEntryResponse{
			Username: entry.Username,
			Wins:     entry.Wins,
			Rank:     i + 1,
		})
	}

	writeAPISuccess(w, response)
}

// handleGetPlayerStats retrieves aggregated stats for a single player
// GET /api/player/{username}/stats
func (s *Server) handleGetPlayerStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract username from path: /api/player/USERNAME/stats
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/player/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeAPIError(w, http.StatusBadRequest, "missing username")
		return
	}
	username := parts[0]

	if s.DB == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "player stats not available - database not enabled")
		return
	}

	ctx := r.Context()
	stats, err := s.DB.GetPlayerStats(ctx, username)
	if err != nil {
		log.Printf("Error retrieving player stats for %s: %v", username, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to retrieve player stats")
		return
	}

	writeAPISuccess(w, stats)
}

// handleAPIStatus provides status endpoint for monitoring
// GET /api/status
func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.GamesMu.RLock()
	activeGames := len(s.Games)
	s.GamesMu.RUnlock()

	llmHealthy := false
	if s.LLMClient != nil {
		llmHealthy = s.LLMClient.Healthy(r.Context())
	}
	status := map[string]interface{}{
		"status":                "running",
		"port":                  s.Port,
		"active_games":          activeGames,
		"db_enabled":            s.DB != nil,
		"llm_healthy":           llmHealthy,
		"llm_timeout_seconds":   int(llmRequestTimeout.Seconds()),
		"room_code_ttl_seconds": int(roomCodeTTL().Seconds()),
	}

	writeAPISuccess(w, status)
}

// Helper functions

// writeAPISuccess writes a successful JSON response
func writeAPISuccess(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := APIResponse{
		Success: true,
		Data:    data,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// writeAPIError writes an error JSON response
func writeAPIError(w http.ResponseWriter, statusCode int, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	response := APIResponse{
		Success: false,
		Error:   errMsg,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding error response: %v", err)
	}
}

// getGameStatus returns a human-readable status for a game
func getGameStatus(game *Game) string {
	if !game.IsActive {
		return "ended"
	}
	return "active"
}

// ── Room API handlers (Phase 11.0) ────────────────────────────────────────────

// RoomInfo is the API response shape for room endpoints.
type RoomInfo struct {
	Code           string  `json:"code"`      // 5-char room code
	GameCode       string  `json:"game_code"` // BINGO-XXXXX (empty if game not yet created)
	HostID         string  `json:"host_id"`
	HostUsername   string  `json:"host_username"` // Phase 12.5: host's username (from hosts table or empty)
	PlayerCount    int     `json:"player_count"`
	GameStatus     string  `json:"game_status"` // "pending", "active", "ended"
	CustomBoard    bool    `json:"custom_board_used"`
	LinkedRoomCode *string `json:"linked_room_code"` // Phase 13.1: nullable — set when room is a side-bet room
}

// handleCreateRoom creates a new room.
// POST /api/rooms
func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		HostID         string  `json:"host_id"`
		LinkedRoomCode *string `json:"linked_room_code"` // Phase 13.1: optional side-bet room link
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.HostID = "" // allow empty body → auto-generate host ID
	}
	var hostID string
	if body.HostID != "" {
		hostID = body.HostID
	}
	// When no host_id is provided, leave HostID empty — the first player to
	// connect via room_login becomes the room host.

	room, err := s.createRoom(r.Context(), hostID)
	if err != nil {
		log.Printf("Error creating room: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "failed to create room")
		return
	}

	// Phase 13.1: set linked room code if requested, after room creation.
	if body.LinkedRoomCode != nil && *body.LinkedRoomCode != "" {
		if s.DB != nil {
			lc := strings.ToUpper(*body.LinkedRoomCode)
			if setErr := s.DB.SetRoomLinkedCode(r.Context(), room.Code, lc); setErr != nil {
				log.Printf("Warning: failed to set linked_room_code for room %s: %v", room.Code, setErr)
			} else {
				room.LinkedRoomCode = &lc
			}
		} else {
			// No DB — set in memory only.
			lc := strings.ToUpper(*body.LinkedRoomCode)
			room.LinkedRoomCode = &lc
		}
	}

	info := RoomInfo{
		Code:           room.Code,
		GameCode:       "",
		HostID:         room.HostID,
		HostUsername:   s.hostUsernameForID(r.Context(), room.HostID),
		PlayerCount:    0,
		GameStatus:     "pending",
		CustomBoard:    false,
		LinkedRoomCode: room.LinkedRoomCode,
	}
	if s.DB != nil {
		if words, getErr := s.DB.GetRoomBuzzwords(r.Context(), room.Code); getErr == nil && len(words) > 0 {
			info.CustomBoard = true
		}
	}
	if g := room.GetGame(); g != nil {
		info.GameCode = g.Code
		info.PlayerCount = g.PlayerCount()
		info.GameStatus = getGameStatus(g)
	}

	writeAPISuccess(w, info)
}

// handleRoomRoutes dispatches sub-routes under /api/room/:code/*.
func (s *Server) handleRoomRoutes(w http.ResponseWriter, r *http.Request) {
	// Path: /api/room/<code>[/<sub>]
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/room/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		writeAPIError(w, http.StatusBadRequest, "missing room code")
		return
	}
	code := strings.ToUpper(parts[0])
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}

	switch {
	case sub == "" && r.Method == http.MethodGet:
		s.handleGetRoom(w, r, code)
	case sub == "buzzwords" && r.Method == http.MethodGet:
		s.handleGetRoomBuzzwords(w, r, code)
	case sub == "buzzwords" && r.Method == http.MethodPost:
		s.handleSetRoomBuzzwords(w, r, code)
	case sub == "leaderboard" && r.Method == http.MethodGet:
		s.handleGetRoomLeaderboard(w, r, code)
	case sub == "generate-buzzwords" && r.Method == http.MethodPost:
		s.handleGenerateBuzzwords(w, r, code)
	case sub == "games" && r.Method == http.MethodGet:
		s.handleGetRoomGames(w, r, code)
	case sub == "games" && r.Method == http.MethodPost:
		s.handleCreateRoomGame(w, r, code)
	case strings.HasPrefix(sub, "games/") && r.Method == http.MethodDelete:
		gameCode := strings.TrimPrefix(sub, "games/")
		if gameCode == "" {
			writeAPIError(w, http.StatusBadRequest, "missing game code")
			return
		}
		s.handleDeleteRoomGame(w, r, code, gameCode)
	default:
		writeAPIError(w, http.StatusNotFound, "not found")
	}
}

// handleGetRoom returns a lobby snapshot for the given room.
// GET /api/room/:code
func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request, code string) {
	room, err := s.getOrCreateRoom(code)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("room %s not found", code))
		return
	}

	info := RoomInfo{
		Code:           room.Code,
		HostID:         room.HostID,
		HostUsername:   s.hostUsernameForID(r.Context(), room.HostID),
		PlayerCount:    0,
		GameStatus:     "pending",
		CustomBoard:    false,
		LinkedRoomCode: room.LinkedRoomCode,
	}
	if s.DB != nil {
		if words, getErr := s.DB.GetRoomBuzzwords(r.Context(), room.Code); getErr == nil && len(words) > 0 {
			info.CustomBoard = true
		}
	}
	if g := room.GetGame(); g != nil {
		info.GameCode = g.Code
		info.PlayerCount = g.PlayerCount()
		info.GameStatus = getGameStatus(g)
	}

	writeAPISuccess(w, info)
}

// handleGetRoomBuzzwords returns the active buzzword list for a room.
// GET /api/room/:code/buzzwords
func (s *Server) handleGetRoomBuzzwords(w http.ResponseWriter, r *http.Request, code string) {
	if s.DB == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "buzzwords not available - database not enabled")
		return
	}

	words, err := s.DB.GetRoomBuzzwords(r.Context(), code)
	if err != nil {
		log.Printf("Error fetching room buzzwords for %s: %v", code, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to fetch buzzwords")
		return
	}
	custom := len(words) > 0

	if words == nil {
		// Fall back to built-in list
		flat := make([]string, 0)
		for _, row := range s.Buzzwords {
			flat = append(flat, row...)
		}
		words = flat
	}

	writeAPISuccess(w, map[string]interface{}{"words": words, "custom": custom})
}

// BuzzwordUploadRequest is the body for POST /api/room/:code/buzzwords.
type BuzzwordUploadRequest struct {
	Words      []string `json:"words"`
	UploadedBy string   `json:"uploaded_by"`
}

// handleSetRoomBuzzwords validates and stores a custom buzzword list for a room.
// POST /api/room/:code/buzzwords
func (s *Server) handleSetRoomBuzzwords(w http.ResponseWriter, r *http.Request, code string) {
	if s.DB == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "buzzwords not available - database not enabled")
		return
	}

	var body BuzzwordUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate
	if len(body.Words) < 24 {
		writeAPIError(w, http.StatusBadRequest, "word list must contain at least 24 words")
		return
	}
	if len(body.Words) > 500 {
		writeAPIError(w, http.StatusBadRequest, "word list must not exceed 500 words")
		return
	}
	cleaned := make([]string, 0, len(body.Words))
	for _, word := range body.Words {
		// Strip control characters
		word = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, word)
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		if len(word) > 60 {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("word too long (max 60 chars): %q", word))
			return
		}
		cleaned = append(cleaned, word)
	}
	if len(cleaned) < 24 {
		writeAPIError(w, http.StatusBadRequest, "word list must contain at least 24 non-empty words")
		return
	}

	uploadedBy := body.UploadedBy
	if uploadedBy == "" {
		uploadedBy = "host"
	}

	if err := s.DB.SetRoomBuzzwords(r.Context(), code, cleaned, uploadedBy); err != nil {
		log.Printf("Error setting room buzzwords for %s: %v", code, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to save buzzwords")
		return
	}

	writeAPISuccess(w, map[string]interface{}{"words_saved": len(cleaned)})
}

// handleSetGameBuzzwords validates and stores a custom buzzword list for a game.
// POST /api/game/:code/buzzwords
func (s *Server) handleSetGameBuzzwords(w http.ResponseWriter, r *http.Request, code string) {
	game, err := s.getOrCreateGame(code)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("game code %s not found", code))
		return
	}

	// Host-only auth: require a valid bearer token bound to the game host.
	token := bearerTokenFromAuthHeader(r.Header.Get("Authorization"))
	if token == "" {
		writeAPIError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	clientIP := s.extractClientIP(r)
	tokenUsername, verifyErr := s.TokenManager.VerifyToken(token, clientIP)
	if verifyErr != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid or expired bearer token")
		return
	}
	if tokenUsername != game.HostID {
		writeAPIError(w, http.StatusForbidden, "only the game host can upload buzzwords")
		return
	}

	var body BuzzwordUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}

	if len(body.Words) < 24 {
		writeAPIError(w, http.StatusBadRequest, "word list must contain at least 24 words")
		return
	}
	if len(body.Words) > 500 {
		writeAPIError(w, http.StatusBadRequest, "word list must not exceed 500 words")
		return
	}

	cleaned := make([]string, 0, len(body.Words))
	for _, word := range body.Words {
		word = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, word)
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		if len(word) > 60 {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("word too long (max 60 chars): %q", word))
			return
		}
		cleaned = append(cleaned, word)
	}
	if len(cleaned) < 24 {
		writeAPIError(w, http.StatusBadRequest, "word list must contain at least 24 non-empty words")
		return
	}

	rows := make([][]string, 0, len(cleaned))
	for _, w := range cleaned {
		rows = append(rows, []string{w})
	}
	game.Buzzwords = rows

	writeAPISuccess(w, map[string]interface{}{"words_saved": len(cleaned)})
}

// handleGetRoomLeaderboard returns per-room top players by win count.
// GET /api/room/:code/leaderboard?limit=10
func (s *Server) handleGetRoomLeaderboard(w http.ResponseWriter, r *http.Request, code string) {
	if s.DB == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "leaderboard not available - database not enabled")
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	entries, err := s.DB.GetRoomLeaderboard(r.Context(), code, limit)
	if err != nil {
		log.Printf("Error fetching room leaderboard for %s: %v", code, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to retrieve leaderboard")
		return
	}

	result := make([]LeaderboardEntryResponse, 0, len(entries))
	for i, e := range entries {
		result = append(result, LeaderboardEntryResponse{
			Username: e.Username,
			Wins:     e.Wins,
			Rank:     i + 1,
		})
	}
	writeAPISuccess(w, result)
}

// ── Phase 12.1: AI Buzzword Generation ────────────────────────────────────────

// generateBuzzwordsRequest is the body for POST /api/room/:code/generate-buzzwords
// and POST /api/game/:code/generate-buzzwords.
type generateBuzzwordsRequest struct {
	HostID   string        `json:"host_id"`
	Topic    string        `json:"topic"`
	URL      string        `json:"url,omitempty"`
	Messages []ChatMessage `json:"messages,omitempty"`
	// Generation options — populated from UI controls.
	// Zero/nil values fall back to experimentally-validated defaults.
	GenerationMode string `json:"generation_mode,omitempty"` // "guided-prompt"|"agentic-retrieval"
	FixedWordCount int    `json:"fixed_word_count,omitempty"`
}

type submitFeedbackRequest struct {
	Topic          string                    `json:"topic"`
	URL            string                    `json:"url,omitempty"`
	SetLabel       string                    `json:"set_label,omitempty"`
	GenerationMode string                    `json:"generation_mode,omitempty"`
	TotalWords     int                       `json:"total_words"`
	IncludedWords  []string                  `json:"included_words"`
	Excluded       []LLMFeedbackExcludedWord `json:"excluded"`
}

func bearerTokenFromAuthHeader(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

// handleSubmitGameBuzzwordFeedback accepts curation feedback for generated words.
// POST /api/game/:code/feedback
// Requires a bearer token belonging to the game host.
func (s *Server) handleSubmitGameBuzzwordFeedback(w http.ResponseWriter, r *http.Request, code string) {
	game, err := s.getOrCreateGame(code)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("game code %s not found", code))
		return
	}

	token := bearerTokenFromAuthHeader(r.Header.Get("Authorization"))
	if token == "" {
		writeAPIError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	clientIP := s.extractClientIP(r)
	tokenUsername, verifyErr := s.TokenManager.VerifyToken(token, clientIP)
	if verifyErr != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid or expired bearer token")
		return
	}
	if tokenUsername != game.HostID {
		writeAPIError(w, http.StatusForbidden, "only the game host can submit AI feedback")
		return
	}

	var body submitFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(body.IncludedWords) == 0 {
		writeAPIError(w, http.StatusBadRequest, "at least one included word is required")
		return
	}
	if len(body.IncludedWords) > 500 || len(body.Excluded) > 500 {
		writeAPIError(w, http.StatusBadRequest, "feedback payload exceeds limits")
		return
	}

	included := make([]string, 0, len(body.IncludedWords))
	for _, word := range body.IncludedWords {
		clean := normalizeFeedbackWord(word)
		if clean == "" {
			continue
		}
		if len(clean) > 60 {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("word too long (max 60 chars): %q", clean))
			return
		}
		included = append(included, clean)
	}
	if len(included) == 0 {
		writeAPIError(w, http.StatusBadRequest, "at least one valid included word is required")
		return
	}

	excluded := make([]LLMFeedbackExcludedWord, 0, len(body.Excluded))
	for _, item := range body.Excluded {
		cleanWord := normalizeFeedbackWord(item.Word)
		reason := strings.TrimSpace(item.Reason)
		if cleanWord == "" {
			continue
		}
		if len(cleanWord) > 60 {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("word too long (max 60 chars): %q", cleanWord))
			return
		}
		if !isAllowedFeedbackReason(reason) {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("invalid exclusion reason: %q", reason))
			return
		}
		otherText := strings.TrimSpace(item.OtherText)
		if reason == "other" && otherText == "" {
			writeAPIError(w, http.StatusBadRequest, "other reason requires explanation text")
			return
		}
		if len(otherText) > 180 {
			writeAPIError(w, http.StatusBadRequest, "other reason text must be 180 characters or fewer")
			return
		}
		duplicateOf := normalizeFeedbackWord(item.DuplicateOf)
		if reason == "duplicate" && duplicateOf == "" {
			writeAPIError(w, http.StatusBadRequest, "duplicate reason requires duplicate_of target")
			return
		}
		if len(duplicateOf) > 60 {
			writeAPIError(w, http.StatusBadRequest, fmt.Sprintf("duplicate_of too long (max 60 chars): %q", duplicateOf))
			return
		}
		specificityNote := strings.TrimSpace(item.SpecificityNote)
		if len(specificityNote) > 180 {
			writeAPIError(w, http.StatusBadRequest, "specificity_note must be 180 characters or fewer")
			return
		}
		retrievalURL := strings.TrimSpace(item.RetrievalURL)
		if len(retrievalURL) > 300 {
			writeAPIError(w, http.StatusBadRequest, "retrieval_url must be 300 characters or fewer")
			return
		}
		if retrievalURL != "" {
			parsedURL, parseErr := url.ParseRequestURI(retrievalURL)
			if parseErr != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
				writeAPIError(w, http.StatusBadRequest, "retrieval_url must be a valid absolute URL")
				return
			}
		}
		if reason != "too_generic" {
			specificityNote = ""
			retrievalURL = ""
		}
		excluded = append(excluded, LLMFeedbackExcludedWord{
			Word:            cleanWord,
			Reason:          reason,
			OtherText:       otherText,
			DuplicateOf:     duplicateOf,
			SpecificityNote: specificityNote,
			RetrievalURL:    retrievalURL,
		})
	}

	entry := LLMFeedbackEntry{
		GameCode:       game.Code,
		Topic:          strings.TrimSpace(body.Topic),
		SourceURL:      strings.TrimSpace(body.URL),
		SetLabel:       strings.TrimSpace(body.SetLabel),
		GenerationMode: normalizeGenerationMode(body.GenerationMode),
		TotalWords:     body.TotalWords,
		IncludedWords:  included,
		Excluded:       excluded,
		SubmittedBy:    tokenUsername,
		SubmittedAt:    time.Now().UTC(),
	}
	s.storeLLMFeedback(entry)

	writeAPISuccess(w, map[string]interface{}{
		"stored": true,
	})
}

// handleGenerateBuzzwords streams AI-generated buzzword sets as SSE.
// POST /api/room/:code/generate-buzzwords
// Requires a bearer token belonging to the room host.
// Returns HTTP 503 when DeepSeek is not configured or not reachable.
func (s *Server) handleGenerateBuzzwords(w http.ResponseWriter, r *http.Request, code string) {
	if s.LLMClient == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — DeepSeek is not reachable")
		return
	}

	room, err := s.getOrCreateRoom(code)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("room %s not found", code))
		return
	}

	var body generateBuzzwordsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Host-only auth: require a valid bearer token bound to the room host.
	token := bearerTokenFromAuthHeader(r.Header.Get("Authorization"))
	if token == "" {
		writeAPIError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	clientIP := s.extractClientIP(r)
	tokenUsername, verifyErr := s.TokenManager.VerifyToken(token, clientIP)
	if verifyErr != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid or expired bearer token")
		return
	}

	if tokenUsername != room.HostID {
		writeAPIError(w, http.StatusForbidden, "only the room host can generate buzzwords")
		return
	}

	// Keep host_id as an optional backward-compatible field; if provided, enforce consistency.
	if body.HostID != "" && body.HostID != room.HostID {
		writeAPIError(w, http.StatusForbidden, "host_id does not match this room")
		return
	}

	// Validate topic length
	topic := strings.TrimSpace(body.Topic)
	if len(topic) > 500 {
		writeAPIError(w, http.StatusBadRequest, "topic must be 500 characters or fewer")
		return
	}

	// Build generation options from request, falling back to validated defaults.
	genOpts := DefaultGenerationOptions()
	genOpts.GenerationMode = normalizeGenerationMode(body.GenerationMode)
	if body.FixedWordCount < 0 || body.FixedWordCount > 200 {
		writeAPIError(w, http.StatusBadRequest, "fixed_word_count must be between 0 and 200")
		return
	}
	if body.FixedWordCount > 0 && body.FixedWordCount < 30 {
		writeAPIError(w, http.StatusBadRequest, "fixed_word_count must be 0 or at least 30")
		return
	}
	genOpts.FixedWordCount = body.FixedWordCount

	// Build user content — scrape-first ordering is always used.
	var urlExcerpt string
	if body.URL != "" {
		excerpt, scrapeErr := ScrapeURL(body.URL)
		if scrapeErr != nil {
			log.Printf("URL scrape failed for room %s: %v", code, scrapeErr)
		} else {
			urlExcerpt = excerpt
		}
	}

	var userContent string
	switch {
	case urlExcerpt != "" && topic != "":
		userContent = fmt.Sprintf("URL excerpt:\n%s\n\nTopic: %s", urlExcerpt, topic)
	case urlExcerpt != "":
		userContent = fmt.Sprintf("URL excerpt:\n%s", urlExcerpt)
	default:
		userContent = topic
	}

	// Merge conversation history with new user message
	messages := make([]ChatMessage, 0, len(body.Messages)+1)
	messages = append(messages, body.Messages...)
	if guidance := s.llmFeedbackGuidance("", topic, genOpts.GenerationMode); guidance != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: guidance})
	}
	if userContent != "" {
		messages = append(messages, ChatMessage{Role: "user", Content: userContent})
	}

	if len(messages) == 0 {
		writeAPIError(w, http.StatusBadRequest, "topic or messages required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if !s.LLMClient.Healthy(ctx) {
		writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — DeepSeek is not reachable")
		return
	}

	if err := s.LLMClient.StreamGenerate(r.Context(), messages, genOpts, w); err != nil {
		log.Printf("LLM generation error for room %s: %v", code, err)
		if errors.Is(err, ErrLLMUnavailable) {
			writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — DeepSeek is not reachable")
			return
		}
		// If streaming has started (headers already sent) we cannot write a JSON error —
		// the SSE "error" event was already written inside StreamGenerate.
	}
}

// handleGenerateBuzzwordsForGame streams AI-generated buzzword sets as SSE.
// POST /api/game/:code/generate-buzzwords
// Requires a bearer token belonging to the game host.
func (s *Server) handleGenerateBuzzwordsForGame(w http.ResponseWriter, r *http.Request, code string) {
	if s.LLMClient == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — DeepSeek is not reachable")
		return
	}

	game, err := s.getOrCreateGame(code)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("game code %s not found", code))
		return
	}

	var body generateBuzzwordsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token := bearerTokenFromAuthHeader(r.Header.Get("Authorization"))
	if token == "" {
		writeAPIError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	clientIP := s.extractClientIP(r)
	tokenUsername, verifyErr := s.TokenManager.VerifyToken(token, clientIP)
	if verifyErr != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid or expired bearer token")
		return
	}
	if tokenUsername != game.HostID {
		writeAPIError(w, http.StatusForbidden, "only the game host can generate buzzwords")
		return
	}

	if body.HostID != "" && body.HostID != game.HostID {
		writeAPIError(w, http.StatusForbidden, "host_id does not match this game")
		return
	}

	topic := strings.TrimSpace(body.Topic)
	if len(topic) > 500 {
		writeAPIError(w, http.StatusBadRequest, "topic must be 500 characters or fewer")
		return
	}

	// Build generation options from request, falling back to validated defaults.
	gameGenOpts := DefaultGenerationOptions()
	gameGenOpts.GenerationMode = normalizeGenerationMode(body.GenerationMode)
	if body.FixedWordCount < 0 || body.FixedWordCount > 200 {
		writeAPIError(w, http.StatusBadRequest, "fixed_word_count must be between 0 and 200")
		return
	}
	if body.FixedWordCount > 0 && body.FixedWordCount < 30 {
		writeAPIError(w, http.StatusBadRequest, "fixed_word_count must be 0 or at least 30")
		return
	}
	gameGenOpts.FixedWordCount = body.FixedWordCount

	// Build user content: scrape-first ordering is always used.
	var urlExcerptGame string
	if body.URL != "" {
		excerpt, scrapeErr := ScrapeURL(body.URL)
		if scrapeErr != nil {
			log.Printf("URL scrape failed for game %s: %v", code, scrapeErr)
		} else {
			urlExcerptGame = excerpt
		}
	}

	var userContent string
	switch {
	case urlExcerptGame != "" && topic != "":
		userContent = fmt.Sprintf("URL excerpt:\n%s\n\nTopic: %s", urlExcerptGame, topic)
	case urlExcerptGame != "":
		userContent = fmt.Sprintf("URL excerpt:\n%s", urlExcerptGame)
	default:
		userContent = topic
	}

	messages := make([]ChatMessage, 0, len(body.Messages)+1)
	messages = append(messages, body.Messages...)
	if guidance := s.llmFeedbackGuidance(game.Code, topic, gameGenOpts.GenerationMode); guidance != "" {
		messages = append(messages, ChatMessage{Role: "system", Content: guidance})
	}
	if userContent != "" {
		messages = append(messages, ChatMessage{Role: "user", Content: userContent})
	}
	if len(messages) == 0 {
		writeAPIError(w, http.StatusBadRequest, "topic or messages required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if !s.LLMClient.Healthy(ctx) {
		writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — DeepSeek is not reachable")
		return
	}

	if err := s.LLMClient.StreamGenerate(r.Context(), messages, gameGenOpts, w); err != nil {
		log.Printf("LLM generation error for game %s: %v", code, err)
		if errors.Is(err, ErrLLMUnavailable) {
			writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — DeepSeek is not reachable")
			return
		}
	}
}

// ── Agent observability endpoint (Phase 15.2) ────────────────────────────────

// AgentEventRequest is the body for POST /metrics/agent-event.
type AgentEventRequest struct {
	Outcome   string  `json:"outcome"`    // see validAgentOutcomes
	RunID     string  `json:"run_id"`     // GitHub Actions run ID
	LatencyMs float64 `json:"latency_ms"` // time from CI failure to PR opened
}

// validAgentOutcomes is the allowlist of outcomes accepted by /metrics/agent-event.
// Unbounded label cardinality can harm Prometheus performance, so we reject
// any value not in this set.
var validAgentOutcomes = map[string]bool{
	"pr_opened":        true,
	"tests_failed":     true,
	"no_fix_generated": true,
}

// handleAgentEvent records an agent observability event.
// POST /metrics/agent-event — requires X-Agent-Key header.
func (s *Server) handleAgentEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !agentKeyMiddleware(w, r) {
		return
	}

	var body AgentEventRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !validAgentOutcomes[body.Outcome] {
		writeAPIError(w, http.StatusBadRequest, "invalid outcome: must be one of pr_opened, tests_failed, no_fix_generated")
		return
	}

	// Record the metrics
	s.Metrics.HotfixTotal.WithLabelValues(body.Outcome).Inc()
	if body.LatencyMs > 0 {
		s.Metrics.HotfixLatency.Observe(body.LatencyMs)
	}

	log.Printf("agent event: run=%s outcome=%s latency_ms=%.0f", body.RunID, body.Outcome, body.LatencyMs)
	writeAPISuccess(w, map[string]interface{}{"recorded": true})
}

// ── Phase 12.5: Multi-Board Room Games ────────────────────────────────────────

// RoomGameInfo is the API response shape for a board in the room's Games/Bets panel.
type RoomGameInfo struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	HostID      string `json:"host_id"`
	Status      string `json:"status"`
	Winner      string `json:"winner,omitempty"`
	PlayerCount int    `json:"player_count"`
	CreatedAt   int64  `json:"created_at"`
	EndedAt     *int64 `json:"ended_at,omitempty"`
}

// hostUsernameForID resolves a host ID to a username via the hosts DB table.
// Returns empty string if DB is nil or the host is not found.
func (s *Server) hostUsernameForID(ctx context.Context, hostID string) string {
	if s.DB == nil {
		return ""
	}
	host, err := s.DB.GetHostByUsername(ctx, hostID)
	if err != nil || host == nil {
		return ""
	}
	return host.Username
}

// handleGetRoomGames returns all boards in a room.
// GET /api/room/:code/games
func (s *Server) handleGetRoomGames(w http.ResponseWriter, r *http.Request, code string) {
	if s.DB == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "room games not available - database not enabled")
		return
	}

	room, err := s.getOrCreateRoom(code)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("room %s not found", code))
		return
	}

	games, err := s.DB.GetGamesByRoom(r.Context(), room.Code)
	if err != nil {
		log.Printf("Error fetching games for room %s: %v", code, err)
		writeAPIError(w, http.StatusInternalServerError, "failed to fetch room games")
		return
	}

	result := make([]RoomGameInfo, 0, len(games))
	for _, g := range games {
		info := RoomGameInfo{
			ID:        g.ID,
			Code:      g.Code,
			Title:     g.Title,
			HostID:    g.HostID,
			Status:    g.Status,
			CreatedAt: g.CreatedAt,
			EndedAt:   g.EndedAt,
		}
		if g.WinnerID != nil {
			info.Winner = *g.WinnerID
		}
		// Check in-memory for player count (falls back to 0 for archived games)
		s.GamesMu.RLock()
		if inMem, ok := s.Games[g.ID]; ok {
			info.PlayerCount = inMem.PlayerCount()
		}
		s.GamesMu.RUnlock()
		result = append(result, info)
	}

	writeAPISuccess(w, result)
}

// CreateRoomGameRequest is the body for POST /api/room/:code/games.
type CreateRoomGameRequest struct {
	Title     string   `json:"title"`     // Board name — AI generation topic
	Buzzwords []string `json:"buzzwords"` // Flat word list for the board
}

// handleCreateRoomGame creates a new board in a room.
// POST /api/room/:code/games
// Requires bearer token auth; only the room admin can create boards.
func (s *Server) handleCreateRoomGame(w http.ResponseWriter, r *http.Request, code string) {
	room, err := s.getOrCreateRoom(code)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("room %s not found", code))
		return
	}

	// Auth: only room admin (host) can create boards
	tokenUsername, authErr := s.verifyBearerToken(r, room.HostID)
	if authErr != "" {
		writeAPIError(w, http.StatusForbidden, authErr)
		return
	}

	var body CreateRoomGameRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Title == "" {
		body.Title = "Untitled Board"
	}
	if len(body.Buzzwords) < 1 {
		writeAPIError(w, http.StatusBadRequest, "buzzwords are required")
		return
	}

	// Wrap the flat word list into the [][]string shape Game.Buzzwords expects.
	rows := make([][]string, 0, len(body.Buzzwords))
	for _, word := range body.Buzzwords {
		rows = append(rows, []string{word})
	}

	// Create the game in-memory
	s.GamesMu.Lock()
	gameID := fmt.Sprintf("game-%d-%d", len(s.Games)+1, time.Now().UnixNano())
	newGame := NewGame(gameID, rows, s.Rows, s.Cols)
	newGame.Title = body.Title
	s.Games[gameID] = newGame
	s.CodeToGame[newGame.Code] = newGame
	s.GamesMu.Unlock()

	// Persist to DB with room code and title
	if s.DB != nil {
		buzzwordJSON, _ := json.Marshal(rows)
		dbGameID, dbErr := s.DB.CreateGame(r.Context(), newGame.Code, room.HostID, body.Title, buzzwordJSON)
		if dbErr != nil {
			log.Printf("Warning: failed to persist new room game: %v", dbErr)
			s.Metrics.RecordError("db")
		} else {
			// Link the game to the room in DB
			_ = s.DB.SetGameRoomCode(r.Context(), dbGameID, room.Code)
		}
	}

	s.Metrics.GameCount.Set(float64(len(s.Games)))
	s.Metrics.GamesCreatedTotal.Inc()

	log.Printf("Room %s: new board created: %s (%s) by %s", room.Code, newGame.Code, body.Title, tokenUsername)

	writeAPISuccess(w, RoomGameInfo{
		ID:          gameID,
		Code:        newGame.Code,
		Title:       body.Title,
		HostID:      room.HostID,
		Status:      "active",
		PlayerCount: 0,
		CreatedAt:   newGame.CreatedAt.Unix(),
	})
}

// handleDeleteRoomGame deletes (soft-deletes) a board from a room.
// DELETE /api/room/:code/games/:gameCode
// Room admin or board creator can delete.
func (s *Server) handleDeleteRoomGame(w http.ResponseWriter, r *http.Request, code string, gameCode string) {
	room, err := s.getOrCreateRoom(code)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("room %s not found", code))
		return
	}

	// Find the game
	s.GamesMu.RLock()
	game, exists := s.CodeToGame[strings.ToUpper(gameCode)]
	s.GamesMu.RUnlock()
	if !exists || game == nil {
		writeAPIError(w, http.StatusNotFound, fmt.Sprintf("game %s not found", gameCode))
		return
	}

	// Auth: room admin or board creator
	tokenUsername, authErr := s.verifyBearerToken(r, "")
	if authErr != "" {
		writeAPIError(w, http.StatusUnauthorized, authErr)
		return
	}
	if tokenUsername != room.HostID && tokenUsername != game.HostID {
		writeAPIError(w, http.StatusForbidden, "only the room admin or board creator can delete this board")
		return
	}

	// Soft-delete in DB by game code (DB IDs differ from in-memory IDs)
	if s.DB != nil {
		if err := s.DB.UpdateGameStatusByCode(r.Context(), game.Code, "deleted"); err != nil {
			log.Printf("Warning: failed to soft-delete game %s: %v", game.Code, err)
		}
	}

	// Mark game inactive and notify players
	game.IsActive = false
	s.broadcastToGame(game.ID, ServerMessage{
		Type:    "game_ended",
		GameID:  game.ID,
		Message: "This board has been removed by the room admin.",
	})

	// Remove from in-memory maps
	s.GamesMu.Lock()
	delete(s.Games, game.ID)
	delete(s.CodeToGame, strings.ToUpper(gameCode))
	s.GamesMu.Unlock()

	s.Metrics.GameCount.Set(float64(len(s.Games)))

	log.Printf("Room %s: board %s (%s) deleted by %s", room.Code, gameCode, game.Title, tokenUsername)

	writeAPISuccess(w, map[string]interface{}{"deleted": true})
}

// verifyBearerToken extracts and verifies the JWT from Authorization header.
// If requiredUsername is non-empty, also checks that the token belongs to that user.
// Returns (username, "") on success or ("", errorMessage) on failure.
func (s *Server) verifyBearerToken(r *http.Request, requiredUsername string) (string, string) {
	token := bearerTokenFromAuthHeader(r.Header.Get("Authorization"))
	if token == "" {
		return "", "missing bearer token"
	}

	clientIP := s.extractClientIP(r)
	username, err := s.TokenManager.VerifyToken(token, clientIP)
	if err != nil {
		return "", "invalid or expired bearer token"
	}

	if requiredUsername != "" && username != requiredUsername {
		return "", "only the room admin can perform this action"
	}

	return username, ""
}
