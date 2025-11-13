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

	"github.com/jkMLnop/binGO-CLI/shared"
)

// dont worry about it
func printWinScreen() {
	// Set terminal window title
	fmt.Print("\033]0;BINGO!!!\007")
	// TODO: can we make it wait for response to cut to the animation? and in meantime countdown indefinitely? 3..2..1..0..-1..-2..-3.. etc
	cmd := exec.Command("curl", "--max-time", "30", "parrot.live")
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func main() {
	// TODO: rewrite this manually to make sure you get it
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

	// Create board
	board := shared.NewBoard(matrix)

	// Display initial board
	shared.PrintBoard(board)

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
		board.MarkCell(num)

		// Clear screen (works on Unix-like systems)
		fmt.Print("\033[H\033[2J")

		// Check for win
		if board.HasWon() {
			printWinScreen()
			break
		}

		// Redraw the board
		shared.PrintBoard(board)
		fmt.Println("\nEnter a number (1-9) to mark a cell, or 'q' to quit:")
	}
}
