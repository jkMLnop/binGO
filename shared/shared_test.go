package shared

import (
	"os"
	"path/filepath"
	"strings"
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
			buzzwords := LoadBuzzwords(path)
			if len(buzzwords) > 0 {
				return buzzwords
			}
		}
	}

	t.Fatalf("Could not find buzzwords.csv")
	return nil
}

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

// ============================================================================
// BOARD TESTS
// ============================================================================

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

// ============================================================================
// GAME SESSION TESTS
// ============================================================================

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

// ============================================================================
// DISPLAY UTILITY TESTS
// ============================================================================

// TestCenterText tests text centering functionality
func TestCenterText(t *testing.T) {
	testCases := []struct {
		name     string
		text     string
		width    int
		padding  string
		expected string
	}{
		{"Center short text", "Hi", 5, " ", " Hi  "},
		{"Center exact fit", "Hello", 5, " ", "Hello"},
		{"Center with dashes", "Hi", 5, "-", "-Hi--"},
		{"Large padding needed", "A", 7, " ", "   A   "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := CenterText(tc.text, tc.width, tc.padding)
			if result != tc.expected {
				t.Errorf("CenterText(%q, %d, %q) = %q, want %q",
					tc.text, tc.width, tc.padding, result, tc.expected)
			}
		})
	}
}

// TestStrikethrough tests strikethrough formatting
func TestStrikethrough(t *testing.T) {
	text := "test"
	result := ApplyStrikethrough(text)

	// Each character gets a combining strikethrough character after it
	// So "test" (4 chars) becomes 8 characters (4 chars + 4 combining chars)
	runes := []rune(result)
	if len(runes) != len(text)*2 {
		t.Errorf("Expected %d runes (with combining), got %d", len(text)*2, len(runes))
	}

	// Verify it contains the strikethrough combining character (U+0336)
	if !strings.Contains(result, "\u0336") {
		t.Error("Strikethrough result should contain combining character")
	}
}

// TestVisualLength tests visual length calculation (excluding combining characters)
func TestVisualLength(t *testing.T) {
	testCases := []struct {
		name     string
		text     string
		expected int
	}{
		{"Plain text", "hello", 5},
		{"Empty string", "", 0},
		{"Single char", "x", 1},
		{"Text with strikethrough", ApplyStrikethrough("hi"), 2}, // visual length should ignore combining chars
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := VisualLength(tc.text)
			if result != tc.expected {
				t.Errorf("VisualLength(%q) = %d, want %d", tc.text, result, tc.expected)
			}
		})
	}
}
