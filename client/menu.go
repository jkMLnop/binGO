package client

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jkMLnop/binGO-CLI/shared"
)

// ShowMainMenu presents the host/join choice to the user and returns the selected
// mode, an optional game code (for join), and an optional custom buzzword list (for host).
// It reads from stdin, so it is called before the WebSocket connection is established.
func ShowMainMenu(serverAddr string) (choice string, code string, buzzwords [][]string, err error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Printf("\n🎲 Connect to %s\n", serverAddr)
	fmt.Println("   1) Host a new game")
	fmt.Println("   2) Join existing game (with code)")
	fmt.Print("> ")

	line, readErr := reader.ReadString('\n')
	if readErr != nil {
		return "", "", nil, fmt.Errorf("failed to read menu choice: %w", readErr)
	}
	selection := strings.TrimSpace(line)

	switch selection {
	case "1":
		bw, loadErr := promptForBuzzwords(reader)
		if loadErr != nil {
			return "", "", nil, loadErr
		}
		return "host", "", bw, nil

	case "2":
		fmt.Print("Enter game code: ")
		codeLine, codeErr := reader.ReadString('\n')
		if codeErr != nil {
			return "", "", nil, fmt.Errorf("failed to read game code: %w", codeErr)
		}
		gameCode := strings.TrimSpace(codeLine)
		if gameCode == "" {
			return "", "", nil, fmt.Errorf("game code cannot be empty")
		}
		return "join", gameCode, nil, nil

	default:
		return "", "", nil, fmt.Errorf("invalid selection %q — enter 1 or 2", selection)
	}
}

// promptForBuzzwords asks for an optional CSV buzzword file path.
// Returns nil buzzwords when the user skips (uses server defaults).
func promptForBuzzwords(reader *bufio.Reader) ([][]string, error) {
	fmt.Print("Enter path to buzzword CSV file (or press Enter to skip): ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read file path: %w", err)
	}
	path := strings.TrimSpace(line)
	if path == "" {
		return nil, nil // use server defaults
	}

	buzzwords, err := shared.LoadBuzzwords(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load buzzword CSV %q: %w", path, err)
	}

	fmt.Printf("✓ Loaded %d buzzword rows from %s\n", len(buzzwords), path)
	return buzzwords, nil
}
