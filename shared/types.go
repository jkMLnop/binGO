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
