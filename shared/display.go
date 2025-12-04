package shared

import (
	"fmt"
	"os"
	"os/exec"
)

// CalculateBoardWidth calculates the total width needed for the board given column widths
func CalculateBoardWidth(cols int, colWidths []int) int {
	// Format: | cell | cell | cell |
	// That's: | + (content + " | ") * cols, then replace last " | " with " |"
	width := 1 // opening "|"
	for j := range cols {
		width += colWidths[j] // cell content
		width += 3            // " | " separator
	}
	// Last " | " has 3 chars but we only need " |" at end (2 chars), so subtract 1
	return width
}

// generatePatternLine generates a full pattern line of the specified type and length
func generatePatternLine(width int, patternType string) string {
	result := ""

	if patternType == "dash" {
		// Generate dash pattern: ---+---+---+... repeating
		for i := 0; i < width; i++ {
			cyclePos := i % 4
			if cyclePos == 3 {
				result += "+"
			} else {
				result += "-"
			}
		}
	} else {
		// Generate letter pattern for given letter sequence
		// Pattern is " X | X | X |..." repeating (4 chars per cycle)
		letters := patternType
		for i := 0; i < width; i++ {
			cyclePos := i % 4
			letterIdx := (i / 4) % len(letters)

			if cyclePos == 0 {
				result += " "
			} else if cyclePos == 1 {
				result += letters[letterIdx : letterIdx+1]
			} else if cyclePos == 2 {
				result += " "
			} else {
				result += "|"
			}
		}
	}

	return result
}

// DisplayBanner prints the binGO-CLI ASCII art banner centered with alternating BINGO borders
// Uses provided totalWidth to match the board width
func DisplayBannerWithWidth(totalWidth int) {
	bannerLines := []string{
		" /$$       /$$            /$$$$$$   /$$$$$$ ",
		"| $$      |__/           /$$__  $$ /$$__  $$",
		"| $$$$$$$  /$$ /$$$$$$$ | $$  \\__/| $$  \\ $$",
		"| $$__  $$| $$| $$__  $$| $$ /$$$$| $$  | $$",
		"| $$  \\ $$| $$| $$  \\ $$| $$|_  $$| $$  | $$",
		"| $$  | $$| $$| $$  | $$| $$  \\ $$| $$  | $$",
		"| $$$$$$$/| $$| $$  | $$|  $$$$$$/|  $$$$$$/",
		"|_______/ |__/|__/  |__/ \\______/  \\______/ ",
	}

	// Get the width of the banner (all lines are same width)
	bannerWidth := VisualLength(bannerLines[0])

	// Pattern definitions for each row
	patterns := []string{
		"dash",  // row 0: ---+---+---+---+...
		"BINGO", // row 1:  B | I | N | G | O | (cycles: B,I,N,G,O)
		"dash",  // row 2: ---+---+---+---+...
		"INGOB", // row 3:  I | N | G | O | B | (offset by 1: I,N,G,O,B)
		"dash",  // row 4: ---+---+---+---+...
		"NGOBI", // row 5:  N | G | O | B | I | (offset by 2: N,G,O,B,I)
		"dash",  // row 6: ---+---+---+---+...
		"GOBIN", // row 7:  G | O | B | I | N | (offset by 3: G,O,B,I,N)
	}

	// Print each banner line with alternating borders
	for rowIdx, line := range bannerLines {
		pattern := patterns[rowIdx]

		// Generate full-width pattern
		fullPattern := generatePatternLine(totalWidth, pattern)

		// Get banner center area
		totalPadding := totalWidth - bannerWidth
		startPos := totalPadding / 2

		// Build output by overlaying banner text onto pattern
		output := ""
		for i := 0; i < totalWidth; i++ {
			if i >= startPos && i < startPos+bannerWidth {
				// Use banner text
				output += string(line[i-startPos])
			} else {
				// Use pattern
				output += string(fullPattern[i])
			}
		}

		fmt.Println(output)
	}
}

// DisplayBanner prints the binGO-CLI ASCII art banner (uses default width)
func DisplayBanner() {
	// Default width - will be overridden by DisplayBannerWithWidth when called from game
	DisplayBannerWithWidth(116)
}

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

// FormatBoard returns formatted board lines without printing
// This allows for testable board rendering and reuse (logging, file output, etc.)
func FormatBoard(board *Board) []string {
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

	return output
}

// PrintBoardWithWidth displays the bingo board and banner at a consistent width
func PrintBoardWithWidth(board *Board) {
	// Calculate the board's natural width
	boardWidth := CalculateBoardWidth(board.Cols, board.ColWidths)

	// Banner width is always 42 + padding
	bannerWidth := 42

	// Use whichever is wider
	totalWidth := boardWidth
	if bannerWidth+10 > boardWidth {
		totalWidth = boardWidth
	}

	// Display banner and board at the calculated width
	DisplayBannerWithWidth(totalWidth)
	PrintBoard(board)
}

// PrintBoard displays the bingo board with optional strikethrough for marked cells
func PrintBoard(board *Board) {
	for _, line := range FormatBoard(board) {
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
