package shared

import (
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
