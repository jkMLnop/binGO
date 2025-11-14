package shared

import (
	"fmt"
	"os"
	"os/exec"
)

// CenterText returns the text centered within the given width using the specified padding character
// If no padding character is provided, defaults to space
func CenterText(text string, width int, paddingChar ...string) string {
	// Default to space if no padding character provided
	padChar := " "
	if len(paddingChar) > 0 {
		padChar = paddingChar[0]
	}

	totalPadding := width - VisualLength(text)
	leftPadding := totalPadding / 2
	rightPadding := totalPadding - leftPadding

	result := ""
	for range leftPadding {
		result += padChar
	}
	result += text
	for range rightPadding {
		result += padChar
	}
	return result
}

// ApplyStrikethrough adds strikethrough formatting to text using Unicode combining character
func ApplyStrikethrough(text string) string {
	result := ""
	for _, char := range text {
		result += string(char) + "\u0336"
	}
	return result
}

// VisualLength returns the visual length of text (ignoring combining characters)
func VisualLength(text string) int {
	count := 0
	for _, char := range text {
		// Skip combining characters (U+0300 to U+036F range) matters for strikethrough
		if char < 0x0300 || char > 0x036F {
			count++
		}
	}
	return count
}

// PrintBoard displays the bingo board with optional strikethrough for marked cells
func PrintBoard(board *Board) {
	var output []string
	var rowLength int

	matrix := board.Matrix
	colWidths := board.ColWidths
	marked := board.Marked
	rows := board.Rows
	cols := board.Cols
	isSpeedBingo := rows == 3 && cols == 3

	for i := range rows {
		// For speed bingo, show 3 lines per cell (number, phrase, padding)
		if isSpeedBingo {
			// Line 1: Cell numbers
			numberLine := "| "
			for j := range cols {
				cellID := board.CellID(i, j)
				numberLine += cellID
				// Pad to column width
				for k := len(cellID); k < colWidths[j]; k++ {
					numberLine += " "
				}
				if j < cols-1 {
					numberLine += " | "
				}
			}
			numberLine += " |"

			if i == 0 {
				rowLength = len(numberLine)
				// Add "BINGO!" centered above top separator with ~ padding
				bingoLine := CenterText(" BINGO! ", rowLength, "~")
				output = append(output, bingoLine)

				// Add top separator
				separator := ""
				for k := 0; k < rowLength; k++ {
					separator += "_"
				}
				output = append(output, separator)
			}

			output = append(output, numberLine)

			// Line 2: Phrases (centered)
			phraseStr := "| "
			for j := range cols {
				cellID := board.CellID(i, j)
				cellText := matrix[i][j]
				// Apply strikethrough if marked
				if marked[cellID] {
					cellText = ApplyStrikethrough(cellText)
				}
				phraseStr += CenterText(cellText, colWidths[j])
				if j < cols-1 {
					phraseStr += " | "
				}
			}
			phraseStr += " |"
			output = append(output, phraseStr)

			// Line 3: Padding
			paddingStr := "| "
			for j := range cols {
				for k := 0; k < colWidths[j]; k++ {
					paddingStr += " "
				}
				if j < cols-1 {
					paddingStr += " | "
				}
			}
			paddingStr += " |"
			output = append(output, paddingStr)
		} else {
			// For non-speed bingo, use regular layout
			rowStr := "| "
			for j := range cols {
				cellID := board.CellID(i, j)
				cellText := matrix[i][j]
				// Apply strikethrough if marked
				if marked[cellID] {
					cellText = ApplyStrikethrough(cellText)
				}
				rowStr += CenterText(cellText, colWidths[j])
				if j < cols-1 {
					rowStr += " | "
				}
			}
			rowStr += " |"

			if i == 0 {
				rowLength = len(rowStr)
				// Add "BINGO!" centered above top separator with ~ padding
				bingoLine := CenterText(" BINGO! ", rowLength, "~")
				output = append(output, bingoLine)

				// Add top separator
				separator := ""
				for k := 0; k < rowLength; k++ {
					separator += "_"
				}
				output = append(output, separator)
			}
			output = append(output, rowStr)
		}

		// Add underscores separator after each row
		separator := ""
		for k := 0; k < rowLength; k++ {
			separator += "_"
		}
		output = append(output, separator)
	}

	// Print the formatted matrix
	for _, line := range output {
		fmt.Println(line)
	}
}

// DisplayWinScreen displays the win animation with terminal title
func DisplayWinScreen() {
	// Set terminal window title
	fmt.Print("\033]0;BINGO!!!\007")

	// Display the parrot animation
	cmd := exec.Command("curl", "--max-time", "30", "parrot.live")
	cmd.Stdout = os.Stdout
	cmd.Run()
}
