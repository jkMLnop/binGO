package shared

// Board represents the 3x3 bingo board state
type Board struct {
	Matrix    [][]string
	ColWidths []int
	Marked    map[int]bool
}

// Message types for client-server communication (future use)
type GameMessage struct {
	Action string      `json:"action"`
	Data   interface{} `json:"data"`
}

type MarkCellMessage struct {
	Cell int `json:"cell"`
}

type BoardStateMessage struct {
	Matrix    [][]string   `json:"matrix"`
	ColWidths []int        `json:"colWidths"`
	Marked    map[int]bool `json:"marked"`
	Winner    bool         `json:"winner"`
}
