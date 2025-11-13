package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// centerText returns the text centered within the given width using the specified padding character
// If no padding character is provided, defaults to space
func centerText(text string, width int, paddingChar ...string) string {
	// Default to space if no padding character provided
	padChar := " "
	if len(paddingChar) > 0 {
		padChar = paddingChar[0]
	}

	totalPadding := width - len(text)
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

// applyStrikethrough adds strikethrough formatting to text using Unicode combining character
func applyStrikethrough(text string) string {
	result := ""
	for _, char := range text {
		result += string(char) + "\u0336"
	}
	return result
}

// visualLength returns the visual length of text (ignoring combining characters)
func visualLength(text string) int {
	count := 0
	for _, char := range text {
		// Skip combining characters (U+0300 to U+036F range) matters for strikethrough
		if char < 0x0300 || char > 0x036F {
			count++
		}
	}
	return count
}

// centerTextVisual centers text within the given width, accounting for visual length
func centerTextVisual(text string, width int, paddingChar ...string) string {
	// Default to space if no padding character provided
	padChar := " "
	if len(paddingChar) > 0 {
		padChar = paddingChar[0]
	}

	visLen := visualLength(text)
	totalPadding := width - visLen
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

// printBoard displays the bingo board with optional strikethrough for marked cells
func printBoard(matrix [][]string, colWidths []int, marked map[int]bool) {
	var output []string
	var rowLength int

	for i := range 3 {
		// Build the row string with | separators and centered padding
		rowStr := "| "
		for j := range 3 {
			// Get the cell number based on the mapping: [[7,8,9],[4,5,6],[1,2,3]]
			cellNum := (2-i)*3 + j + 1

			cellText := matrix[i][j]
			// Apply strikethrough if marked
			if marked[cellNum] {
				cellText = applyStrikethrough(cellText)
			}

			// Center the text within the column width, accounting for visual length
			rowStr += centerTextVisual(cellText, colWidths[j])
			if j < 2 {
				rowStr += " | "
			}
		}
		rowStr += " |"

		// Store row length from first row
		if i == 0 {
			rowLength = len(rowStr)

			// Add "BINGO!" centered above top separator with ~ padding
			bingoLine := centerText(" BINGO! ", rowLength, "~")
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

// checkWin checks if there's a winning combination (3 in a row)
func checkWin(marked map[int]bool) bool {
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
		if marked[pattern[0]] && marked[pattern[1]] && marked[pattern[2]] {
			return true
		}
	}
	return false
}

// dont worry about it
func printWinScreen() {
	// Set terminal window title
	fmt.Print("\033]0;BINGO!!!\007")

	cmd := exec.Command("curl", "--max-time", "30", "parrot.live")
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func main() {
	// Open the CSV file
	file, err := os.Open("buzzwords.csv")
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	// Read all rows from the CSV file
	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		log.Fatalf("Failed to read CSV file: %v", err)
	}

	// Check if there are enough rows
	if len(rows) < 9 {
		log.Fatalf("Not enough rows in the CSV file to populate a 3x3 matrix")
	}

	// Shuffle and select 9 random rows
	// Use math/rand's Shuffle which is deterministic unless seeded.
	// We intentionally do not seed here so behavior is reproducible across runs.
	rand.Shuffle(len(rows), func(i, j int) {
		rows[i], rows[j] = rows[j], rows[i]
	})
	selectedRows := rows[:9]

	// Populate the 3x3 matrix
	matrix := make([][]string, 3)
	for i := range 3 {
		matrix[i] = make([]string, 3)
		for j := range 3 {
			matrix[i][j] = selectedRows[i*3+j][0]
		}
	}

	// Calculate the maximum width for each column
	colWidths := make([]int, 3)
	for j := range 3 {
		maxWidth := 0
		for i := range 3 {
			if len(matrix[i][j]) > maxWidth {
				maxWidth = len(matrix[i][j])
			}
		}
		colWidths[j] = maxWidth
	}

	// Track marked cells
	marked := make(map[int]bool)

	// Display initial board
	printBoard(matrix, colWidths, marked)

	// Interactive loop
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("\nEnter a number (1-9) to mark a cell, or 'q' to quit:")

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())

		// Check for quit command
		if input == "q" || input == "quit" {
			fmt.Println("Thanks for playing!")
			break
		}

		// Parse the input
		num, err := strconv.Atoi(input)
		if err != nil || num < 1 || num > 9 {
			fmt.Println("Invalid input. Please enter a number between 1-9, or 'q' to quit.")
			continue
		}

		// Mark the cell
		marked[num] = true

		// Clear screen (works on Unix-like systems)
		fmt.Print("\033[H\033[2J")

		// Check for win
		if checkWin(marked) {
			printWinScreen()
			break
		}

		// Redraw the board
		printBoard(matrix, colWidths, marked)
		fmt.Println("\nEnter a number (1-9) to mark a cell, or 'q' to quit:")
	}
}
