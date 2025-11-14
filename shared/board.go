package shared

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
