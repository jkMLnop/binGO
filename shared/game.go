package shared

import (
	"math/rand"
)

// GameSession represents a game session with a board
type GameSession struct {
	Board *Board
}

// NewGameSession creates a new game session from buzzwords
// rows/cols: 3 for speed bingo, 5 for classic bingo
// This logic is shared between standalone and server modes
// NOTE: 5x5 support exists but is not currently used or tested
func NewGameSession(buzzwords [][]string, rows, cols int) *GameSession {
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

	// Create board
	board := NewBoard(matrix, rows, cols)

	return &GameSession{
		Board: board,
	}
}

// CheckWin checks if there's a winning combination (3+ in a row)
func (gs *GameSession) CheckWin() bool {
	rows := gs.Board.Rows
	cols := gs.Board.Cols

	// Check horizontal
	for i := range rows {
		for j := 0; j <= cols-3; j++ {
			if gs.checkLineWin(i, j, 0, 1) {
				return true
			}
		}
	}

	// Check vertical
	for i := 0; i <= rows-3; i++ {
		for j := range cols {
			if gs.checkLineWin(i, j, 1, 0) {
				return true
			}
		}
	}

	// Check diagonals (top-left to bottom-right)
	for i := 0; i <= rows-3; i++ {
		for j := 0; j <= cols-3; j++ {
			if gs.checkLineWin(i, j, 1, 1) {
				return true
			}
		}
	}

	// Check diagonals (top-right to bottom-left)
	for i := 0; i <= rows-3; i++ {
		for j := 2; j < cols; j++ {
			if gs.checkLineWin(i, j, 1, -1) {
				return true
			}
		}
	}

	return false
}

// ClearMarks clears all marked cells on the board for a fresh game
func (gs *GameSession) ClearMarks() {
	gs.Board.Marked = make(map[string]bool)
}

// checkLineWin checks if 3 cells in a line are marked
// startRow, startCol: starting position
// dRow, dCol: direction (e.g., 0,1 for horizontal, 1,0 for vertical, 1,1 for diagonal)
func (gs *GameSession) checkLineWin(startRow, startCol, dRow, dCol int) bool {
	for k := 0; k < 3; k++ {
		row := startRow + k*dRow
		col := startCol + k*dCol
		cellID := gs.Board.CellID(row, col)
		if !gs.Board.IsMarked(cellID) {
			return false
		}
	}
	return true
}
