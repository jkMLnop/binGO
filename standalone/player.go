package standalone

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jkMLnop/binGO-CLI/shared"
)

// Game represents a standalone game session
type Game struct {
	session      *shared.Board // Bingo board with game state
	displayWidth int           // Cached display width for consistent banner/board rendering
}

// NewGame creates a new standalone game (3x3 speed bingo)
func NewGame(buzzwords [][]string) *Game {
	// Create a shared game session with 3x3 dimensions
	board := shared.NewGameSession(buzzwords, 3, 3)

	// Calculate display width based on board size
	boardWidth := shared.CalculateBoardWidth(board.Cols, board.ColWidths)

	return &Game{
		session:      board,
		displayWidth: boardWidth,
	}
}

// playRound handles a single player's turn: mark cell, check win, redraw board
func (g *Game) playRound(scanner *bufio.Scanner, maxCells int, promptMsg string) bool {
	fmt.Print("> ")

	if !scanner.Scan() {
		return false // Exit on error
	}

	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return true
	}

	// Parse input
	var command string
	var cellID string

	switch input {
	case "q", "quit":
		fmt.Println("Thanks for playing!")
		return false
	default:
		// Try to parse as numeric cell ID
		cellNum, err := strconv.Atoi(input)
		if err != nil || cellNum < 1 || cellNum > maxCells {
			fmt.Printf("Invalid input. Please enter a number between 1-%d, or 'q' to quit.\n", maxCells)
			return true // Continue to next round
		}
		command = "mark"
		cellID = strconv.Itoa(cellNum)
	}

	// Mark the cell
	if command == "mark" {
		if err := g.session.MarkCell(cellID); err != nil {
			fmt.Printf("Error: %v\n", err)
			return true // Continue to next round
		}

		// Clear screen and redraw banner + board
		fmt.Print("\033[H\033[2J")
		shared.DisplayBannerWithWidth(g.displayWidth)

		// Check for win
		if g.session.CheckWin() {
			// Banner + board already displayed together above
			shared.PrintBoard(g.session)
			fmt.Println("\n🎉 YOU WIN! 🎉")
			time.Sleep(2 * time.Second) // Let player see winning board + banner
			shared.DisplayWinScreen()
			return false // Exit after win
		}

		// Redraw the board
		shared.PrintBoard(g.session)
		fmt.Println("\n" + promptMsg)
	}

	return true // Continue to next round
}

// RunGame orchestrates the game loop
func (g *Game) RunGame() {
	// Clear screen and display banner and initial board at top of terminal
	fmt.Print("\033[H\033[2J")
	shared.DisplayBannerWithWidth(g.displayWidth)
	shared.PrintBoard(g.session)

	// Setup input scanner for 3x3 board (cells 1-9)
	maxCells := 9
	promptMsg := "Enter a number (1-9) to mark a cell, or 'q' to quit:"
	fmt.Println("\n" + promptMsg)

	scanner := bufio.NewScanner(os.Stdin)

	// Game loop
	for {
		if !g.playRound(scanner, maxCells, promptMsg) {
			break
		}
	}
}
