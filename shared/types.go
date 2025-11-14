package shared

// Board represents the 3x3 bingo board state
type Board struct {
	Matrix    [][]string
	ColWidths []int
	Marked    map[int]bool
}

// ClientMessage represents messages sent from client to server
type ClientMessage struct {
	Action string `json:"action"` // "mark" or "quit"
	Cell   int    `json:"cell"`   // 1-9 (only for "mark")
}

// ServerMessage represents messages sent from server to client
type ServerMessage struct {
	Type     string       `json:"type"` // "welcome", "board_update", "player_joined", "game_ended", "error"
	GameID   string       `json:"game_id"`
	PlayerID string       `json:"player_id"`
	Board    [][]string   `json:"board"`   // player's random board (only sent on join/welcome)
	Marked   map[int]bool `json:"marked"`  // player's marked cells
	Players  []string     `json:"players"` // list of connected player IDs
	Winner   string       `json:"winner"`  // player ID who won (only in game_ended)
	Message  string       `json:"message"` // error or info messages
}
