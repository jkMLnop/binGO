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

// Start begins the standalone game loop
func (g *Game) Start() {
	// Display initial board
	shared.PrintBoard(g.session.Board)

	// Create input handler for 3x3 board (cells 1-9)
	inputHandler := shared.NewInputHandler(9, "Enter a number (1-9) to mark a cell, or 'q' to quit:")
	fmt.Println("\n" + inputHandler.PromptMessage())

	// Interactive loop
	for {
		cellID, command, err := inputHandler.ProcessInput()
		if err != nil {
			break
		}

		switch command {
		case "quit":
			fmt.Println("Thanks for playing!")
			return

		case "mark":
			// Mark the cell
			if err := g.session.Board.MarkCell(cellID); err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			// Clear screen and redraw board
			fmt.Print("\033[H\033[2J")

			// Check for win
			if g.session.CheckWin() {
				printWinScreen()
				return
			}

			// Redraw the board
			shared.PrintBoard(g.session.Board)
			fmt.Println("\n" + inputHandler.PromptMessage())

		case "invalid":
			fmt.Println(inputHandler.InvalidInputMessage(9))

		default:
			// Ignore other commands in standalone mode
		}
	}
}

// printWinScreen displays the win animation
func printWinScreen() {
	shared.DisplayWinScreen()
}
