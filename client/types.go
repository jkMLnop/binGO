package client

// ServerMessage matches server/types.go
type ServerMessage struct {
	Type           string     `json:"type"`
	GameID         string     `json:"game_id"`
	Code           string     `json:"code,omitempty"`    // Phase 7.3: Game code
	HostID         string     `json:"host_id,omitempty"` // Current host player ID
	OriginalHostID string     `json:"original_host_id,omitempty"` // Original host (never changes)
	PlayerID       string     `json:"player_id"`
	Username  string     `json:"username,omitempty"`
	Token     string     `json:"token,omitempty"`
	Buzzwords [][]string `json:"buzzwords"`
	Rows      int        `json:"rows"`
	Cols      int        `json:"cols"`
	Players   []string   `json:"players"`
	Winner    string     `json:"winner"`
	Message   string     `json:"message"`
}

// ClientMessage matches server/types.go
type ClientMessage struct {
	Action   string `json:"action"`
	Username string `json:"username,omitempty"`
	Token    string `json:"token,omitempty"`
	Code     string `json:"code,omitempty"` // Phase 7.3: Game join code
	Cell     string `json:"cell,omitempty"`
	CardID   string `json:"cardId,omitempty"`
}
