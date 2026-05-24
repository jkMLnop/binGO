package client

// ServerMessage matches server/types.go
type ServerMessage struct {
	Type                string       `json:"type"`
	GameID              string       `json:"game_id"`
	Code                string       `json:"code,omitempty"`
	RoomCode            string       `json:"room_code,omitempty"` // Phase 11.0: 5-char room code
	HostID              string       `json:"host_id,omitempty"`
	PlayerID            string       `json:"player_id"`
	Username            string       `json:"username,omitempty"`
	Token               string       `json:"token,omitempty"`
	Buzzwords           [][]string   `json:"buzzwords"`
	Rows                int          `json:"rows"`
	Cols                int          `json:"cols"`
	Players             []string     `json:"players"`
	Winner              string       `json:"winner"`
	Message             string       `json:"message"`
	Suggestions         []Suggestion `json:"suggestions,omitempty"`          // Phase 9: pending buzzword suggestions
	ActiveBets          []Bet        `json:"active_bets,omitempty"`          // Phase 9.5: active player bets
	FlatBuzzwords       []string     `json:"flat_buzzwords,omitempty"`       // Phase 9.6: flat buzzword pool
	RejectedSuggestions []string     `json:"rejected_suggestions,omitempty"` // Phase 9.6: host-rejected phrases
}

// ClientMessage matches server/types.go
type ClientMessage struct {
	Action    string     `json:"action"`
	Username  string     `json:"username,omitempty"`
	Token     string     `json:"token,omitempty"`
	Code      string     `json:"code,omitempty"`      // Phase 7.3: Game join code
	RoomCode  string     `json:"room_code,omitempty"` // Phase 11.0: 5-char room code
	Cell      string     `json:"cell,omitempty"`
	CardID    string     `json:"cardId,omitempty"`
	Phrase    string     `json:"phrase,omitempty"`    // Phase 9: buzzword suggestion or bet text
	Buzzwords [][]string `json:"buzzwords,omitempty"` // Phase 9: custom buzzwords for host game creation
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
