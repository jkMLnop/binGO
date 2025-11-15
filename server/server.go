package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/jkMLnop/binGO-CLI/shared"
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
}

// NewServer creates a new bingo server
func NewServer(buzzwords [][]string, rows, cols int, port string) *Server {
	srv := &Server{
		Games:     make(map[string]*Game),
		Buzzwords: buzzwords,
		Rows:      rows,
		Cols:      cols,
		Port:      port,
	}
	srv.createNewGame()
	return srv
}

// Start begins listening for connections
func (s *Server) Start() error {
	// Register WebSocket handler
	http.Handle("/ws", websocket.Handler(s.wsHandler))
	http.HandleFunc("/status", s.handleStatus)

	s.Server = &http.Server{
		Addr: ":" + s.Port,
	}

	log.Printf("Server starting on port %s", s.Port)
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

// wsHandler handles incoming WebSocket connections (Phase 2)
func (s *Server) wsHandler(ws *websocket.Conn) {
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

	if game == nil {
		errMsg := shared.ServerMessage{
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
	player := NewPlayer(playerID, s.Buzzwords, s.Rows, s.Cols)

	// Add player to game
	if err := game.AddPlayer(player); err != nil {
		errMsg := shared.ServerMessage{
			Type:    "error",
			Message: err.Error(),
		}
		websocket.JSON.Send(ws, errMsg)
		ws.Close()
		return
	}

	log.Printf("Player %s joined game %s via WebSocket", playerID, game.ID)

	// Send welcome message
	welcomeMsg := shared.ServerMessage{
		Type:     "welcome",
		GameID:   game.ID,
		PlayerID: playerID,
		CardID:   "0",
		Board:    player.GetFirstCard().Board.Matrix,
		Rows:     s.Rows,
		Cols:     s.Cols,
		Marked:   player.GetFirstCard().Board.Marked,
		Players:  game.GetPlayerList(),
		Message:  fmt.Sprintf("Welcome %s! Players in game: %d", playerID, game.PlayerCount()),
	}

	if err := websocket.JSON.Send(ws, welcomeMsg); err != nil {
		log.Printf("Error sending welcome message: %v", err)
		game.RemovePlayer(playerID)
		ws.Close()
		return
	}

	log.Printf("Sent welcome message to %s", playerID)

	// Listen for incoming messages from player (Phase 3+)
	for {
		var msg shared.ClientMessage
		if err := websocket.JSON.Receive(ws, &msg); err != nil {
			log.Printf("Player %s disconnected: %v", playerID, err)
			game.RemovePlayer(playerID)
			break
		}

		// Phase 3: Handle mark actions
		if msg.Action == "mark" {
			log.Printf("Received mark from %s: cell=%s, cardID=%s", playerID, msg.Cell, msg.CardID)

			// Get card ID (default to "0" if not specified)
			cardID := msg.CardID
			if cardID == "" {
				cardID = "0"
			}

			// Handle the mark
			if err := s.HandleMark(game.ID, playerID, cardID, msg.Cell); err != nil {
				// Send error response
				errResp := shared.ServerMessage{
					Type:     "mark_error",
					GameID:   game.ID,
					PlayerID: playerID,
					CardID:   cardID,
					Message:  err.Error(),
				}
				websocket.JSON.Send(ws, errResp)
				log.Printf("Mark error for %s: %v", playerID, err)
				continue
			}

			// Get updated player state
			player, exists := game.GetPlayer(playerID)
			if !exists || player == nil {
				errResp := shared.ServerMessage{
					Type:     "mark_error",
					GameID:   game.ID,
					PlayerID: playerID,
					Message:  "Player not found",
				}
				websocket.JSON.Send(ws, errResp)
				log.Printf("Player %s not found after mark", playerID)
				continue
			}

			card := player.GetCard(0) // TODO: support multiple cards
			if card == nil {
				errResp := shared.ServerMessage{
					Type:     "mark_error",
					GameID:   game.ID,
					PlayerID: playerID,
					Message:  "Card not found",
				}
				websocket.JSON.Send(ws, errResp)
				log.Printf("Card 0 not found for player %s", playerID)
				continue
			}

			// Send confirmation with updated board state
			confirmResp := shared.ServerMessage{
				Type:     "mark_confirmed",
				GameID:   game.ID,
				PlayerID: playerID,
				CardID:   cardID,
				Board:    card.Board.Matrix,
				Rows:     game.Rows,
				Cols:     game.Cols,
				Marked:   card.Board.Marked,
				Message:  fmt.Sprintf("Cell %s marked", msg.Cell),
			}
			websocket.JSON.Send(ws, confirmResp)
			log.Printf("Mark confirmed for %s on cell %s", playerID, msg.Cell)
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

// HandleMark processes a mark action from a player
func (s *Server) HandleMark(gameID, playerID, cardID, cellID string) error {
	s.GamesMu.RLock()
	game, exists := s.Games[gameID]
	s.GamesMu.RUnlock()

	if !exists {
		return fmt.Errorf("game %s not found", gameID)
	}

	player, exists := game.GetPlayer(playerID)
	if !exists {
		return fmt.Errorf("player %s not found in game", playerID)
	}

	cardIndex, err := strconv.Atoi(cardID)
	if err != nil {
		return fmt.Errorf("invalid card ID: %s", cardID)
	}

	card := player.GetCard(cardIndex)
	if card == nil {
		return fmt.Errorf("card %d not found for player %s", cardIndex, playerID)
	}

	// Mark the cell
	if err := card.Board.MarkCell(cellID); err != nil {
		return err
	}

	log.Printf("Player %s marked cell %s on card %d", playerID, cellID, cardIndex)

	return nil
}

// CheckWinnerInGame checks if any player has won
func (s *Server) CheckWinnerInGame(gameID string) (string, error) {
	s.GamesMu.RLock()
	game, exists := s.Games[gameID]
	s.GamesMu.RUnlock()

	if !exists {
		return "", fmt.Errorf("game %s not found", gameID)
	}

	for playerID, player := range game.Players {
		for cardIdx, card := range player.Cards {
			if card.CheckWin() {
				log.Printf("Player %s (card %d) won game %s!", playerID, cardIdx, gameID)
				game.Winner = playerID
				game.IsActive = false
				return playerID, nil
			}
		}
	}

	return "", nil
}

// BroadcastToGame sends a message to all players in a game
func (s *Server) BroadcastToGame(gameID string, msg interface{}) error {
	s.GamesMu.RLock()
	game, exists := s.Games[gameID]
	s.GamesMu.RUnlock()

	if !exists {
		return fmt.Errorf("game %s not found", gameID)
	}

	for _, player := range game.Players {
		_ = player.SendMessage(msg) // Non-blocking send
	}

	return nil
}
