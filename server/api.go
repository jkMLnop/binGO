package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
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
