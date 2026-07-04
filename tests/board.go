package tests

import (
	"encoding/csv"
	"fmt"
	"math/rand"
	"os"
)

// Board represents a bingo board with configurable dimensions.
// 3x3 = speed bingo (cells 1-9).
type Board struct {
	Rows      int
	Cols      int
	Matrix    [][]string
	ColWidths []int
	Marked    map[string]bool
}

// CellID generates a cell ID based on board dimensions.
// For 3x3 (speed bingo): uses numeric "1"-"9" in numpad layout:
//
//	[[7,8,9],
//	 [4,5,6],
//	 [1,2,3]]
func (b *Board) CellID(row, col int) string {
	if b.Rows == 3 && b.Cols == 3 {
		numpadRow := 2 - row
		cellNum := numpadRow*3 + col + 1
		return fmt.Sprintf("%d", cellNum)
	}
	return fmt.Sprintf("%d-%d", row, col)
}

// MarkCell marks a cell on the board by its ID.
func (b *Board) MarkCell(cellID string) error {
	row, col, err := ParseCellID(cellID, b.Cols)
	if err != nil {
		return err
	}
	if row < 0 || row >= b.Rows || col < 0 || col >= b.Cols {
		return fmt.Errorf("cell out of bounds: %s", cellID)
	}
	if b.Marked[cellID] {
		return fmt.Errorf("cell already marked: %s", cellID)
	}
	b.Marked[cellID] = true
	return nil
}

// IsMarked checks if a cell is marked.
func (b *Board) IsMarked(cellID string) bool {
	return b.Marked[cellID]
}

// CheckWin checks if there's a winning combination (3+ in a row).
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

	// Check diagonals
	for i := 0; i <= rows-3; i++ {
		for j := 0; j <= cols-3; j++ {
			if b.checkLineWin(i, j, 1, 1) {
				return true
			}
		}
	}
	for i := 0; i <= rows-3; i++ {
		for j := 2; j < cols; j++ {
			if b.checkLineWin(i, j, 1, -1) {
				return true
			}
		}
	}

	return false
}

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

// ParseCellID converts a cell ID like "3" back to row/col.
func ParseCellID(cellID string, cols int) (row, col int, err error) {
	if len(cellID) == 1 {
		cell := cellID[0] - '0'
		if cell >= 1 && cell <= 9 {
			row = int((cell - 1) / 3)
			col = int((cell - 1) % 3)
			return row, col, nil
		}
	}
	return 0, 0, fmt.Errorf("invalid cell ID: %s", cellID)
}

// NewBoard creates a new Board with the given matrix and dimensions.
func NewBoard(matrix [][]string, rows, cols int) *Board {
	board := &Board{
		Rows:      rows,
		Cols:      cols,
		Matrix:    matrix,
		Marked:    make(map[string]bool),
		ColWidths: make([]int, cols),
	}
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

// NewGameSession creates a new game session from buzzwords.
func NewGameSession(buzzwords [][]string, rows, cols int) *Board {
	totalCells := rows * cols
	rand.Shuffle(len(buzzwords), func(i, j int) {
		buzzwords[i], buzzwords[j] = buzzwords[j], buzzwords[i]
	})
	selectedRows := buzzwords[:totalCells]
	matrix := make([][]string, rows)
	for i := range rows {
		matrix[i] = make([]string, cols)
		for j := range cols {
			matrix[i][j] = selectedRows[i*cols+j][0]
		}
	}
	return NewBoard(matrix, rows, cols)
}

// LoadBuzzwords reads buzzwords from a CSV file and returns them as rows.
func LoadBuzzwords(filename string) ([][]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 9 {
		return nil, fmt.Errorf("not enough rows in CSV file: have %d, need at least 9", len(rows))
	}
	return rows, nil
}
