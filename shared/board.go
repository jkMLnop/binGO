package shared

import (
	"fmt"
	"math/rand"
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

// CheckWin checks if there's a winning combination (3+ in a row)
func (b *Board) CheckWin() bool {
	rows := b.Rows
	cols := b.Cols

	// Check horizontal
	for i := range rows {
		for j := 0; j <= cols-3; j++ {
			if b.checkLineWin(i, j, 0, 1) {
				return true
			}
		}
	}

	// Check vertical
	for i := 0; i <= rows-3; i++ {
		for j := range cols {
			if b.checkLineWin(i, j, 1, 0) {
				return true
			}
		}
	}

	// Check diagonals (top-left to bottom-right)
	for i := 0; i <= rows-3; i++ {
		for j := 0; j <= cols-3; j++ {
			if b.checkLineWin(i, j, 1, 1) {
				return true
			}
		}
	}

	// Check diagonals (top-right to bottom-left)
	for i := 0; i <= rows-3; i++ {
		for j := 2; j < cols; j++ {
			if b.checkLineWin(i, j, 1, -1) {
				return true
			}
		}
	}

	return false
}

// checkLineWin checks if 3 cells in a line are marked
// startRow, startCol: starting position
// dRow, dCol: direction (e.g., 0,1 for horizontal, 1,0 for vertical, 1,1 for diagonal)
func (b *Board) checkLineWin(startRow, startCol, dRow, dCol int) bool {
	for k := 0; k < 3; k++ {
		row := startRow + k*dRow
		col := startCol + k*dCol
		cellID := b.CellID(row, col)
		if !b.IsMarked(cellID) {
			return false
		}
	}
	return true
}

// NewGameSession creates a new game session from buzzwords
// rows/cols: 3 for speed bingo, 5 for classic bingo
// This logic is shared between standalone and server modes
// NOTE: 5x5 support exists but is not currently used or tested
func NewGameSession(buzzwords [][]string, rows, cols int) *Board {
	// Calculate how many buzzwords we need
	totalCells := rows * cols

	// Shuffle and select random rows
	rand.Shuffle(len(buzzwords), func(i, j int) {
		buzzwords[i], buzzwords[j] = buzzwords[j], buzzwords[i]
	})
	selectedRows := buzzwords[:totalCells]

	// Populate the matrix in row-major order
	// For 3x3 (speed bingo): matrix[0] = top row (cells 7,8,9), matrix[2] = bottom row (cells 1,2,3)
	// For 5x5 (classic bingo): matrix[0] = top row (B1-O1), matrix[4] = bottom row (B5-O5)
	// The CellID function maps these matrix positions to the appropriate cell numbering scheme
	matrix := make([][]string, rows)
	for i := range rows {
		matrix[i] = make([]string, cols)
		for j := range cols {
			matrix[i][j] = selectedRows[i*cols+j][0]
		}
	}

	// Create and return board
	return NewBoard(matrix, rows, cols)
}
