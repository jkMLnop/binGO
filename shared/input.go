package shared

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// InputHandler provides reusable command loop logic for bingo games
// It handles parsing numeric cell inputs (1-9) and common commands
type InputHandler struct {
	scanner       *bufio.Scanner
	maxCellNum    int // typically 9 for 3x3, could be 25 for 5x5
	promptMessage string
}

// NewInputHandler creates a new input handler with default settings
func NewInputHandler(maxCellNum int, promptMessage string) *InputHandler {
	return &InputHandler{
		scanner:       bufio.NewScanner(os.Stdin),
		maxCellNum:    maxCellNum,
		promptMessage: promptMessage,
	}
}

// ProcessInput reads a line from input and returns the parsed command
// Returns: cellID (or empty), commandName, error
func (h *InputHandler) ProcessInput() (string, string, error) {
	if !h.scanner.Scan() {
		return "", "", h.scanner.Err()
	}

	input := strings.TrimSpace(h.scanner.Text())
	if input == "" {
		return "", "", nil
	}

	// Check for text commands first
	switch input {
	case "q", "quit":
		return "", "quit", nil
	case "board":
		return "", "board", nil
	case "win":
		return "", "win", nil
	case "help":
		return "", "help", nil
	}

	// Try to parse as numeric cell ID
	cellNum, err := strconv.Atoi(input)
	if err != nil || cellNum < 1 || cellNum > h.maxCellNum {
		return "", "invalid", nil
	}

	cellID := strconv.Itoa(cellNum)
	return cellID, "mark", nil
}

// PromptMessage returns the current instruction prompt
func (h *InputHandler) PromptMessage() string {
	return h.promptMessage
}

// InvalidInputMessage returns a formatted error message for invalid input
func (h *InputHandler) InvalidInputMessage(maxNum int) string {
	return fmt.Sprintf("Invalid input. Please enter a number between 1-%d, or type 'help' for commands.", maxNum)
}
