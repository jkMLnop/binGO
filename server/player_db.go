package server

// PlayerDBInfo tracks database information for a player
type PlayerDBInfo struct {
	DBPlayerID string // ID of player in database (for win recording)
	GameCode   string // Game code for this player (for win recording)
	Username   string // Player's username (for win recording)
	IPAddress  string // Player's IP address
}

// playerDBMap stores database info for connected players
var playerDBMap = make(map[string]*PlayerDBInfo) // playerID -> PlayerDBInfo

// SetPlayerDBInfo stores database information for a player
func SetPlayerDBInfo(playerID string, dbInfo *PlayerDBInfo) {
	playerDBMap[playerID] = dbInfo
}

// GetPlayerDBInfo retrieves database information for a player
func GetPlayerDBInfo(playerID string) *PlayerDBInfo {
	return playerDBMap[playerID]
}

// ClearPlayerDBInfo removes database information for a player
func ClearPlayerDBInfo(playerID string) {
	delete(playerDBMap, playerID)
}
