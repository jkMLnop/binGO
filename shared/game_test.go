package shared

import (
	"os"
	"path/filepath"
	"testing"
)

// loadTestBuzzwords loads buzzwords.csv for testing
func loadTestBuzzwords(t *testing.T) [][]string {
	buzzwordPaths := []string{
		"../buzzwords.csv",
		"buzzwords.csv",
		filepath.Join(os.Getenv("PWD"), "buzzwords.csv"),
	}

	for _, path := range buzzwordPaths {
		if _, err := os.Stat(path); err == nil {
			buzzwords, err := LoadBuzzwords(path)
			if err == nil && len(buzzwords) > 0 {
				return buzzwords
			}
		}
	}

	t.Fatalf("Could not find buzzwords.csv")
	return nil
}

// TestGameSessionCreation tests initialization with buzzwords
func TestGameSessionCreation(t *testing.T) {
	buzzwords := loadTestBuzzwords(t)
	game := NewGameSession(buzzwords, 3, 3)

	if game.Board == nil {
		t.Error("Board should not be nil")
	}
	if game.Board.Rows != 3 {
		t.Errorf("Expected 3 rows, got %d", game.Board.Rows)
	}
	if game.Board.Cols != 3 {
		t.Errorf("Expected 3 columns, got %d", game.Board.Cols)
	}
	if len(game.Board.Matrix) == 0 {
		t.Error("Board matrix should not be empty")
	}
	if len(game.Board.Marked) != 0 {
		t.Error("Marked map should be empty initially")
	}

	// Verify all cells have content
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if game.Board.Matrix[i][j] == "" {
				t.Errorf("Cell [%d][%d] is empty", i, j)
			}
		}
	}
}

// TestWinDetection tests all winning patterns for a 3x3 board
func TestWinDetection(t *testing.T) {
	testCases := []struct {
		name      string
		cells     []string
		shouldWin bool
	}{
		// Rows
		{"bottom row (1,2,3)", []string{"1", "2", "3"}, true},
		{"middle row (4,5,6)", []string{"4", "5", "6"}, true},
		{"top row (7,8,9)", []string{"7", "8", "9"}, true},
		// Columns
		{"left column (1,4,7)", []string{"1", "4", "7"}, true},
		{"middle column (2,5,8)", []string{"2", "5", "8"}, true},
		{"right column (3,6,9)", []string{"3", "6", "9"}, true},
		// Diagonals
		{"diagonal (1,5,9)", []string{"1", "5", "9"}, true},
		{"anti-diagonal (3,5,7)", []string{"3", "5", "7"}, true},
		// Non-wins
		{"no win (1,2,4)", []string{"1", "2", "4"}, false},
		{"incomplete row (1,2)", []string{"1", "2"}, false},
		{"two in row (7,8)", []string{"7", "8"}, false},
		{"partial column (1,4)", []string{"1", "4"}, false},
		{"partial diagonal (1,5)", []string{"1", "5"}, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buzzwords := loadTestBuzzwords(t)
			game := NewGameSession(buzzwords, 3, 3)

			for _, cellID := range tc.cells {
				err := game.Board.MarkCell(cellID)
				if err != nil {
					t.Fatalf("Failed to mark cell %s: %v", cellID, err)
				}
			}

			hasWon := game.CheckWin()
			if hasWon != tc.shouldWin {
				t.Errorf("CheckWin() = %v, want %v", hasWon, tc.shouldWin)
			}
		})
	}
}
