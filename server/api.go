package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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

	status := map[string]interface{}{
		"status":       "running",
		"port":         s.Port,
		"active_games": activeGames,
		"db_enabled":   s.DB != nil,
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
	Code        string `json:"code"`      // 5-char room code
	GameCode    string `json:"game_code"` // BINGO-XXXXX (empty if game not yet created)
	HostID      string `json:"host_id"`
	PlayerCount int    `json:"player_count"`
	GameStatus  string `json:"game_status"` // "pending", "active", "ended"
}

// handleCreateRoom creates a new room.
// POST /api/rooms
func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		HostID string `json:"host_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.HostID = "" // allow empty body → auto-generate host ID
	}
	hostID := body.HostID
	if hostID == "" {
		hostID = fmt.Sprintf("host-%d", len(s.Rooms)+1)
	}

	room, err := s.createRoom(r.Context(), hostID)
	if err != nil {
		log.Printf("Error creating room: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "failed to create room")
		return
	}

	info := RoomInfo{
		Code:        room.Code,
		GameCode:    "",
		HostID:      room.HostID,
		PlayerCount: 0,
		GameStatus:  "pending",
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
		Code:        room.Code,
		HostID:      room.HostID,
		PlayerCount: 0,
		GameStatus:  "pending",
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

	if words == nil {
		// Fall back to built-in list
		flat := make([]string, 0)
		for _, row := range s.Buzzwords {
			flat = append(flat, row...)
		}
		words = flat
	}

	writeAPISuccess(w, map[string]interface{}{"words": words, "custom": words != nil})
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

// generateBuzzwordsRequest is the body for POST /api/room/:code/generate-buzzwords.
type generateBuzzwordsRequest struct {
	HostID   string        `json:"host_id"`
	Topic    string        `json:"topic"`
	URL      string        `json:"url,omitempty"`
	Messages []ChatMessage `json:"messages,omitempty"`
}

func bearerTokenFromAuthHeader(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

// handleGenerateBuzzwords streams AI-generated buzzword sets as SSE.
// POST /api/room/:code/generate-buzzwords
// Requires a bearer token belonging to the room host.
// Returns HTTP 503 when Ollama is not configured or not reachable.
func (s *Server) handleGenerateBuzzwords(w http.ResponseWriter, r *http.Request, code string) {
	if s.LLMClient == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — Ollama is not reachable")
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

	// Build the user message content
	userContent := topic

	// Optionally scrape URL and append to context
	if body.URL != "" {
		excerpt, scrapeErr := ScrapeURL(body.URL)
		if scrapeErr != nil {
			log.Printf("URL scrape failed for room %s: %v", code, scrapeErr)
			// Non-fatal: proceed without URL content
		} else if excerpt != "" {
			if topic != "" {
				userContent = fmt.Sprintf("Topic: %s\n\nURL excerpt:\n%s", topic, excerpt)
			} else {
				userContent = fmt.Sprintf("URL excerpt:\n%s", excerpt)
			}
		}
	}

	// Merge conversation history with new user message
	messages := make([]ChatMessage, 0, len(body.Messages)+1)
	messages = append(messages, body.Messages...)
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
		writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — Ollama is not reachable")
		return
	}

	if err := s.LLMClient.StreamGenerate(r.Context(), messages, w); err != nil {
		log.Printf("LLM generation error for room %s: %v", code, err)
		if errors.Is(err, ErrLLMUnavailable) {
			writeAPIError(w, http.StatusServiceUnavailable, "AI generation not available — Ollama is not reachable")
			return
		}
		// If streaming has started (headers already sent) we cannot write a JSON error —
		// the SSE "error" event was already written inside StreamGenerate.
	}
}
