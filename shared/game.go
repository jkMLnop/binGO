package shared

import (
	"math/rand"
)

// GameSession represents a game session with a board
type GameSession struct {
	Board *Board
}

// NewGameSession creates a new game session from buzzwords
// This logic is shared between standalone and server modes
func NewGameSession(buzzwords [][]string) *GameSession {
	// Shuffle and select 9 random rows
	rand.Shuffle(len(buzzwords), func(i, j int) {
		buzzwords[i], buzzwords[j] = buzzwords[j], buzzwords[i]
	})
	selectedRows := buzzwords[:9]

	// Populate the 3x3 matrix
	matrix := make([][]string, 3)
	for i := range 3 {
		matrix[i] = make([]string, 3)
		for j := range 3 {
			matrix[i][j] = selectedRows[i*3+j][0]
		}
	}

	// Create board
	board := NewBoard(matrix)

	return &GameSession{
		Board: board,
	}
}

// CheckWin checks if there's a winning combination (3 in a row)
func (gs *GameSession) CheckWin() bool {
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
		if gs.Board.IsMarked(pattern[0]) && gs.Board.IsMarked(pattern[1]) && gs.Board.IsMarked(pattern[2]) {
			return true
		}
	}
	return false
}
