package server

// ClientMessage represents messages sent from client to server
type ClientMessage struct {
	Action   string `json:"action"`             // "login", "win", etc.
	Username string `json:"username,omitempty"` // for login action
	Token    string `json:"token,omitempty"`    // JWT token for re-authentication
	Code     string `json:"code,omitempty"`     // Phase 7.3: Game join code (for remote players)
}

// ServerMessage represents messages sent from server to client
type ServerMessage struct {
	Type      string     `json:"type"` // "welcome", "game_ended", "error"
	GameID    string     `json:"game_id"`
	Code      string     `json:"code,omitempty"`    // Phase 7.3: Game code for joining
	HostID    string     `json:"host_id,omitempty"` // Host player ID (immutable)
	PlayerID  string     `json:"player_id"`
	Username  string     `json:"username,omitempty"` // authenticated username
	Token     string     `json:"token,omitempty"`    // JWT token issued on login
	Rows      int        `json:"rows"`               // board dimensions
	Cols      int        `json:"cols"`
	Buzzwords [][]string `json:"buzzwords"` // buzzword list for client board generation
	Players   []string   `json:"players"`   // list of connected player IDs
	Winner    string     `json:"winner"`    // player ID who won (in game_ended)
	Message   string     `json:"message"`   // error or info messages
}
