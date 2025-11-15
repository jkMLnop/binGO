package shared

// Board represents a bingo board with configurable dimensions
// 3x3 = speed bingo (cells 1-9)
// 5x5 = classic bingo (cells B1-O5)
type Board struct {
	Rows      int             // 3 or 5
	Cols      int             // 3 or 5
	Matrix    [][]string      // board phrases
	ColWidths []int           // width of each column for display
	Marked    map[string]bool // marked cells (e.g., "B1", "I2", "N3" for 5x5 or "1"-"9" for 3x3)
}

// ClientMessage represents messages sent from client to server
type ClientMessage struct {
	Action string `json:"action"`  // "mark" or "quit"
	Cell   string `json:"cell"`    // "1"-"9" for 3x3, "B1"-"O5" for 5x5
	CardID string `json:"card_id"` // which card to mark (for multi-card support)
}

// ServerMessage represents messages sent from server to client
type ServerMessage struct {
	Type     string          `json:"type"` // "welcome", "board_update", "player_joined", "game_ended", "error"
	GameID   string          `json:"game_id"`
	PlayerID string          `json:"player_id"`
	CardID   string          `json:"card_id"` // Which card this message relates to (for multi-card support)
	Board    [][]string      `json:"board"`   // player's random board (only sent on join/welcome)
	Rows     int             `json:"rows"`    // board dimensions
	Cols     int             `json:"cols"`
	Marked   map[string]bool `json:"marked"`  // player's marked cells
	Players  []string        `json:"players"` // list of connected player IDs
	Winner   string          `json:"winner"`  // player ID who won (only in game_ended)
	Message  string          `json:"message"` // error or info messages
}
