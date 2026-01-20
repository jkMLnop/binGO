package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helper to create a mock board for testing without buzzwords file
func createMockBoard(rows, cols int) *Board {
	// Create dummy matrix with simple content
	matrix := make([][]string, rows)
	for i := range rows {
		matrix[i] = make([]string, cols)
		for j := range cols {
			matrix[i][j] = strings.ToUpper(string(rune('A'+j))) + string(rune('1'+i))
		}
	}
	return NewBoard(matrix, rows, cols)
}

// TestBoardCreation verifies board initialization with correct dimensions
func TestBoardCreation(t *testing.T) {
	testCases := []struct {
		name string
		rows int
		cols int
	}{
		{"3x3 board (speed bingo)", 3, 3},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			board := createMockBoard(tc.rows, tc.cols)

			if board.Rows != tc.rows {
				t.Errorf("Expected %d rows, got %d", tc.rows, board.Rows)
			}
			if board.Cols != tc.cols {
				t.Errorf("Expected %d cols, got %d", tc.cols, board.Cols)
			}
			if len(board.Matrix) != tc.rows {
				t.Errorf("Expected %d matrix rows, got %d", tc.rows, len(board.Matrix))
			}
			if len(board.ColWidths) != tc.cols {
				t.Errorf("Expected %d column widths, got %d", tc.cols, len(board.ColWidths))
			}
			if len(board.Marked) != 0 {
				t.Errorf("Marked map should be empty initially, got %d", len(board.Marked))
			}
		})
	}
}

// TestCellIDGeneration verifies correct cell ID generation for 3x3 board
func TestCellIDGeneration(t *testing.T) {
	// Test 3x3 board (numpad layout: 7,8,9 / 4,5,6 / 1,2,3)
	board3x3 := createMockBoard(3, 3)
	testCases3x3 := []struct {
		row      int
		col      int
		expected string
	}{
		{0, 0, "7"}, // top-left
		{0, 1, "8"}, // top-middle
		{0, 2, "9"}, // top-right
		{1, 0, "4"}, // middle-left
		{1, 1, "5"}, // center
		{1, 2, "6"}, // middle-right
		{2, 0, "1"}, // bottom-left
		{2, 1, "2"}, // bottom-middle
		{2, 2, "3"}, // bottom-right
	}

	for _, tc := range testCases3x3 {
		t.Run("3x3 cell ID", func(t *testing.T) {
			cellID := board3x3.CellID(tc.row, tc.col)
			if cellID != tc.expected {
				t.Errorf("CellID(%d, %d) = %s, want %s", tc.row, tc.col, cellID, tc.expected)
			}
		})
	}
}

// TestCellIDParsing verifies correct parsing of cell IDs back to row/col for 3x3
func TestCellIDParsing(t *testing.T) {
	// Test 3x3 parsing (numeric format)
	// Note: ParseCellID uses simple math, not numpad layout
	// 1-3 = row 0, 4-6 = row 1, 7-9 = row 2
	// Within row: 0,1,2 for columns
	testCases3x3 := []struct {
		cellID string
		expRow int
		expCol int
		valid  bool
	}{
		{"1", 0, 0, true},
		{"2", 0, 1, true},
		{"3", 0, 2, true},
		{"4", 1, 0, true},
		{"5", 1, 1, true},
		{"6", 1, 2, true},
		{"7", 2, 0, true},
		{"8", 2, 1, true},
		{"9", 2, 2, true},
		{"0", 0, 0, false},  // invalid
		{"10", 0, 0, false}, // invalid
	}

	for _, tc := range testCases3x3 {
		t.Run("3x3 parse "+tc.cellID, func(t *testing.T) {
			row, col, err := ParseCellID(tc.cellID, 3)
			if tc.valid && err != nil {
				t.Errorf("ParseCellID(%s, 3) returned error: %v", tc.cellID, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("ParseCellID(%s, 3) should have returned error", tc.cellID)
			}
			if tc.valid && (row != tc.expRow || col != tc.expCol) {
				t.Errorf("ParseCellID(%s, 3) = (%d, %d), want (%d, %d)",
					tc.cellID, row, col, tc.expRow, tc.expCol)
			}
		})
	}
}

// TestBoardMarking tests cell marking and error handling
func TestBoardMarking(t *testing.T) {
	board := createMockBoard(3, 3)

	// Test marking a valid cell
	err := board.MarkCell("5")
	if err != nil {
		t.Fatalf("Failed to mark valid cell: %v", err)
	}

	if !board.IsMarked("5") {
		t.Error("Cell 5 should be marked")
	}

	// Test re-marking prevention
	err = board.MarkCell("5")
	if err == nil {
		t.Error("Re-marking cell should fail but didn't")
	}
	if err != nil && err.Error() != "cell already marked: 5" {
		t.Errorf("Wrong error message: %v", err)
	}

	// Test marking multiple different cells
	for _, cellID := range []string{"1", "2", "3", "4"} {
		err := board.MarkCell(cellID)
		if err != nil {
			t.Errorf("Failed to mark cell %s: %v", cellID, err)
		}
		if !board.IsMarked(cellID) {
			t.Errorf("Cell %s should be marked", cellID)
		}
	}

	// Test unmarked cells
	for _, cellID := range []string{"6", "7", "8", "9"} {
		if board.IsMarked(cellID) {
			t.Errorf("Cell %s should not be marked", cellID)
		}
	}

	// Test invalid cell ID
	err = board.MarkCell("99")
	if err == nil {
		t.Error("Marking invalid cell should fail")
	}
}

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
	board := NewGameSession(buzzwords, 3, 3)

	if board == nil {
		t.Error("Board should not be nil")
	}
	if board.Rows != 3 {
		t.Errorf("Expected 3 rows, got %d", board.Rows)
	}
	if board.Cols != 3 {
		t.Errorf("Expected 3 columns, got %d", board.Cols)
	}
	if len(board.Matrix) == 0 {
		t.Error("Board matrix should not be empty")
	}
	if len(board.Marked) != 0 {
		t.Error("Marked map should be empty initially")
	}

	// Verify all cells have content
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if board.Matrix[i][j] == "" {
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
			board := NewGameSession(buzzwords, 3, 3)

			for _, cellID := range tc.cells {
				err := board.MarkCell(cellID)
				if err != nil {
					t.Fatalf("Failed to mark cell %s: %v", cellID, err)
				}
			}

			hasWon := board.CheckWin()
			if hasWon != tc.shouldWin {
				t.Errorf("CheckWin() = %v, want %v", hasWon, tc.shouldWin)
			}
		})
	}
}
