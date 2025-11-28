package shared

import (
	"fmt"
)

// NewBoard creates a new Board with the given matrix and dimensions
// rows/cols: 3 for speed bingo, 5 for classic bingo
func NewBoard(matrix [][]string, rows, cols int) *Board {
	board := &Board{
		Rows:      rows,
		Cols:      cols,
		Matrix:    matrix,
		Marked:    make(map[string]bool),
		ColWidths: make([]int, cols),
	}

	// Calculate column widths
	for j := range cols {
		maxWidth := 0
		for i := range rows {
			if len(matrix[i][j]) > maxWidth {
				maxWidth = len(matrix[i][j])
			}
		}
		board.ColWidths[j] = maxWidth
	}

	return board
}

// CellID generates a cell ID based on board dimensions
// For 3x3 (speed bingo): uses numeric "1"-"9" in numpad layout:
//
//	[[7,8,9],
//	 [4,5,6],
//	 [1,2,3]]
//
// For 5x5 (classic bingo): uses BINGO letters "B1"-"O5"
// NOTE: 5x5 support exists but is not currently used or tested
func (b *Board) CellID(row, col int) string {
	if b.Rows == 3 && b.Cols == 3 {
		// For 3x3: map to numpad layout (1-9)
		// row 0 maps to numpad rows 7,8,9
		// row 1 maps to numpad rows 4,5,6
		// row 2 maps to numpad rows 1,2,3
		numpadRow := 2 - row // Reverse row order
		cellNum := numpadRow*3 + col + 1
		return fmt.Sprintf("%d", cellNum)
	}

	// For 5x5 use BINGO letters: B=col0, I=col1, N=col2, G=col3, O=col4
	letters := []string{"B", "I", "N", "G", "O"}
	if col < len(letters) {
		return fmt.Sprintf("%s%d", letters[col], row+1)
	}
	// Fallback for non-standard sizes
	return fmt.Sprintf("%d-%d", row, col)
}

// ParseCellID converts a cell ID like "B1" or "3" back to row/col
// Supports both 3x3 numeric format ("1"-"9") and 5x5 BINGO format ("B1"-"O5")
// NOTE: 5x5 support exists but is not currently used or tested
func ParseCellID(cellID string, cols int) (row, col int, err error) {
	// Try parsing as numeric (3x3)
	if len(cellID) == 1 {
		cell := cellID[0] - '0'
		if cell >= 1 && cell <= 9 {
			row = int((cell - 1) / 3)
			col = int((cell - 1) % 3)
			return row, col, nil
		}
	}

	// Try parsing as BINGO letter format (5x5)
	if len(cellID) >= 2 {
		letters := "BINGO"
		letter := cellID[0]
		colIdx := -1
		for i, l := range letters {
			if rune(letter) == l {
				colIdx = i
				break
			}
		}
		if colIdx >= 0 {
			var rowNum int
			_, err := fmt.Sscanf(cellID[1:], "%d", &rowNum)
			if err == nil && rowNum >= 1 && rowNum <= 5 {
				row = rowNum - 1
				col = colIdx
				return row, col, nil
			}
		}
	}

	return 0, 0, fmt.Errorf("invalid cell ID: %s", cellID)
}

// MarkCell marks a cell on the board by its ID
func (b *Board) MarkCell(cellID string) error {
	row, col, err := ParseCellID(cellID, b.Cols)
	if err != nil {
		return err
	}
	if row < 0 || row >= b.Rows || col < 0 || col >= b.Cols {
		return fmt.Errorf("cell out of bounds: %s", cellID)
	}
	// Check if already marked
	if b.Marked[cellID] {
		return fmt.Errorf("cell already marked: %s", cellID)
	}
	b.Marked[cellID] = true
	return nil
}

// IsMarked checks if a cell is marked
func (b *Board) IsMarked(cellID string) bool {
	return b.Marked[cellID]
}
