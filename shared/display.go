package shared

import (
	"fmt"
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

	for i := range 3 {
		// Build the row string with | separators and centered padding
		rowStr := "| "
		for j := range 3 {
			// Get the cell number based on the mapping: [[7,8,9],[4,5,6],[1,2,3]]
			cellNum := (2-i)*3 + j + 1

			cellText := matrix[i][j]
			// Apply strikethrough if marked
			if marked[cellNum] {
				cellText = ApplyStrikethrough(cellText)
			}

			// Center the text within the column width, accounting for visual length
			rowStr += CenterText(cellText, colWidths[j])
			if j < 2 {
				rowStr += " | "
			}
		}
		rowStr += " |"

		// Store row length from first row
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
