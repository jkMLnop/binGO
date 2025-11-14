package standalone

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jkMLnop/binGO-CLI/shared"
)

// Game represents a standalone game session
type Game struct {
	session *shared.GameSession
}

// NewGame creates a new standalone game (3x3 speed bingo)
func NewGame(buzzwords [][]string) *Game {
	// Create a shared game session with 3x3 dimensions
	session := shared.NewGameSession(buzzwords, 3, 3)

	return &Game{
		session: session,
	}
}

// Start begins the standalone game loop
func (g *Game) Start() {
	// Display initial board
	shared.PrintBoard(g.session.Board)

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

		// Convert numeric input to cell ID for 3x3
		cellID := strconv.Itoa(num)

		// Mark the cell
		if err := g.session.Board.MarkCell(cellID); err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		// Clear screen (works on Unix-like systems)
		fmt.Print("\033[H\033[2J")

		// Check for win
		if g.session.CheckWin() {
			printWinScreen()
			break
		}

		// Redraw the board
		shared.PrintBoard(g.session.Board)
		fmt.Println("\nEnter a number (1-9) to mark a cell, or 'q' to quit:")
	}
}

// printWinScreen displays the win animation
func printWinScreen() {
	shared.DisplayWinScreen()
}
