package standalone

import (
	"fmt"

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

// playRound handles a single player's turn: mark cell, check win, redraw board
func (g *Game) playRound(inputHandler *shared.InputHandler, maxCells int) bool {
	fmt.Print("> ")
	cellID, command, err := inputHandler.ProcessInput()
	if err != nil {
		return false // Exit on error
	}

	switch command {
	case "quit":
		fmt.Println("Thanks for playing!")
		return false

	case "mark":
		// Mark the cell
		if err := g.session.Board.MarkCell(cellID); err != nil {
			fmt.Printf("Error: %v\n", err)
			return true // Continue to next round
		}

		// Clear screen and redraw board
		fmt.Print("\033[H\033[2J")

		// Check for win
		if g.session.CheckWin() {
			shared.DisplayWinScreen()
			return false // Exit after win
		}

		// Redraw the board
		shared.PrintBoard(g.session.Board)
		fmt.Println("\n" + inputHandler.PromptMessage())

	case "invalid":
		fmt.Println(inputHandler.InvalidInputMessage(maxCells))

	default:
		// Ignore other commands in standalone mode
	}

	return true // Continue to next round
}

// RunGame orchestrates the game loop
func (g *Game) RunGame() {
	// Display initial board
	shared.PrintBoard(g.session.Board)

	// Create input handler for 3x3 board (cells 1-9)
	maxCells := 9
	inputHandler := shared.NewInputHandler(maxCells, "Enter a number (1-9) to mark a cell, or 'q' to quit:")
	fmt.Println("\n" + inputHandler.PromptMessage())

	// Game loop
	for {
		if !g.playRound(inputHandler, maxCells) {
			break
		}
	}
}
