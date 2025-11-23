package client

// ServerMessage matches server/types.go
type ServerMessage struct {
	Type      string     `json:"type"`
	GameID    string     `json:"game_id"`
	PlayerID  string     `json:"player_id"`
	Buzzwords [][]string `json:"buzzwords"`
	Rows      int        `json:"rows"`
	Cols      int        `json:"cols"`
	Players   []string   `json:"players"`
	Winner    string     `json:"winner"`
	Message   string     `json:"message"`
}

// ClientMessage matches server/types.go
type ClientMessage struct {
	Action string `json:"action"`
	Cell   string `json:"cell,omitempty"`
	CardID string `json:"cardId,omitempty"`
}
