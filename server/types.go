package server

// ClientMessage represents messages sent from client to server
type ClientMessage struct {
	Action    string     `json:"action"`              // "login", "host", "win", "restart", "suggest", "approve", "reject", "bet"
	Username  string     `json:"username,omitempty"`  // for login action
	Token     string     `json:"token,omitempty"`     // JWT token for re-authentication
	Code      string     `json:"code,omitempty"`      // Phase 7.3: Game join code (for remote players)
	Phrase    string     `json:"phrase,omitempty"`    // Phase 9: buzzword suggestion or bet text
	Buzzwords [][]string `json:"buzzwords,omitempty"` // Phase 9: custom buzzwords for host game creation
}

// ServerMessage represents messages sent from server to client
type ServerMessage struct {
	Type        string       `json:"type"` // "welcome", "game_ended", "error", "suggestion_broadcast", "bets_update"
	GameID      string       `json:"game_id"`
	Code        string       `json:"code,omitempty"`    // Phase 7.3: Game code for joining
	HostID      string       `json:"host_id,omitempty"` // Host player ID (immutable)
	PlayerID    string       `json:"player_id"`
	Username    string       `json:"username,omitempty"` // authenticated username
	Token       string       `json:"token,omitempty"`    // JWT token issued on login
	Rows        int          `json:"rows"`               // board dimensions
	Cols        int          `json:"cols"`
	Buzzwords   [][]string   `json:"buzzwords"`             // buzzword list for client board generation
	Players     []string     `json:"players"`               // list of connected player IDs
	Winner      string       `json:"winner"`                // player ID who won (in game_ended)
	Message     string       `json:"message"`               // error or info messages
	Suggestions []Suggestion `json:"suggestions,omitempty"` // Phase 9: pending buzzword suggestions
	ActiveBets  []Bet        `json:"active_bets,omitempty"` // Phase 9.5: active player bets
}

// Suggestion represents a pending buzzword suggestion from a player
type Suggestion struct {
	PlayerID string `json:"player_id"`
	Phrase   string `json:"phrase"`
}

// BetCondition represents one condition within a compound bet
type BetCondition struct {
	PlayerUsername string `json:"player_username"`
	Outcome        string `json:"outcome"` // "wins" or "loses"
}

// Bet represents a player's in-game bet on the outcome of the round
type Bet struct {
	ID             string         `json:"id"`
	BetterID       string         `json:"better_id"`
	BetterUsername string         `json:"better_username"`
	RawText        string         `json:"raw_text"`
	Conditions     []BetCondition `json:"conditions"`
	Status         string         `json:"status"` // "active", "won", "lost"
}
