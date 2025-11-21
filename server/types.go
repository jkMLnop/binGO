package server

// ClientMessage represents messages sent from client to server
type ClientMessage struct {
	Action string `json:"action"` // "win" announcement
}

// ServerMessage represents messages sent from server to client
type ServerMessage struct {
	Type      string     `json:"type"` // "welcome", "game_ended", "error"
	GameID    string     `json:"game_id"`
	PlayerID  string     `json:"player_id"`
	Rows      int        `json:"rows"` // board dimensions
	Cols      int        `json:"cols"`
	Buzzwords [][]string `json:"buzzwords"` // buzzword list for client board generation
	Players   []string   `json:"players"`   // list of connected player IDs
	Winner    string     `json:"winner"`    // player ID who won (in game_ended)
	Message   string     `json:"message"`   // error or info messages
}
