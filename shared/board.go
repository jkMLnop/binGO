package shared

// CheckWin checks if there's a winning combination (3 in a row)
func CheckWin(marked map[int]bool) bool {
	// Winning combinations
	winPatterns := [][]int{
		// Horizontal
		{1, 2, 3}, // Bottom row
		{4, 5, 6}, // Middle row
		{7, 8, 9}, // Top row
		// Vertical
		{1, 4, 7}, // Left column
		{2, 5, 8}, // Middle column
		{3, 6, 9}, // Right column
		// Diagonal
		{1, 5, 9}, // Top-left to bottom-right
		{3, 5, 7}, // Top-right to bottom-left
	}

	for _, pattern := range winPatterns {
		if marked[pattern[0]] && marked[pattern[1]] && marked[pattern[2]] {
			return true
		}
	}
	return false
}

// NewBoard creates a new Board with the given matrix
func NewBoard(matrix [][]string) *Board {
	board := &Board{
		Matrix:    matrix,
		Marked:    make(map[int]bool),
		ColWidths: make([]int, 3),
	}

	// Calculate column widths
	for j := range 3 {
		maxWidth := 0
		for i := range 3 {
			if len(matrix[i][j]) > maxWidth {
				maxWidth = len(matrix[i][j])
			}
		}
		board.ColWidths[j] = maxWidth
	}

	return board
}

// MarkCell marks a cell on the board
func (b *Board) MarkCell(cellNum int) bool {
	if cellNum < 1 || cellNum > 9 {
		return false
	}
	b.Marked[cellNum] = true
	return true
}

// IsMarked checks if a cell is marked
func (b *Board) IsMarked(cellNum int) bool {
	return b.Marked[cellNum]
}

// HasWon checks if the current board state is a winning state
func (b *Board) HasWon() bool {
	return CheckWin(b.Marked)
}
