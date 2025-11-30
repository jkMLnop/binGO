package client

import (
	"fmt"
	"log"
	"strings"

	"github.com/jkMLnop/binGO-CLI/shared"
	"golang.org/x/net/websocket"
)

// Player represents a bingo player client
type Player struct {
	ServerURL   string
	WS          *websocket.Conn
	GameID      string
	PlayerID    string
	GameSession *shared.GameSession // Use shared game logic (includes Board with Rows/Cols)
}

// NewPlayer creates a new player client
func NewPlayer(serverURL string) *Player {
	return &Player{
		ServerURL: serverURL,
	}
}

// Connect establishes WebSocket connection to server and performs handshake
func (p *Player) Connect() (ServerMessage, error) {
	origin := "http://localhost"

	// Construct WS URL from server address
	wsURL := p.ServerURL

	// For all connections, use ws:// (plain WebSocket)
	// ngrok free tier doesn't support wss:// anyway
	if !strings.HasPrefix(wsURL, "ws://") {
		wsURL = "ws://" + wsURL + "/ws"
	}

	log.Printf("Attempting to connect to: %s", wsURL)

	// Dial WebSocket
	ws, err := websocket.Dial(wsURL, "", origin)
	if err != nil {
		return ServerMessage{}, fmt.Errorf("failed to connect: %w", err)
	}

	p.WS = ws
	log.Printf("Connected to server at %s", wsURL)

	// Receive welcome message
	var welcomeMsg ServerMessage
	if err := websocket.JSON.Receive(ws, &welcomeMsg); err != nil {
		return ServerMessage{}, fmt.Errorf("failed to receive welcome: %w", err)
	}

	if welcomeMsg.Type != "welcome" {
		return ServerMessage{}, fmt.Errorf("unexpected message type: %s", welcomeMsg.Type)
	}

	p.GameID = welcomeMsg.GameID
	p.PlayerID = welcomeMsg.PlayerID

	return welcomeMsg, nil
}

// HasWon checks if the player has a winning pattern using shared game logic
func (p *Player) HasWon() bool {
	return p.GameSession.CheckWin()
}

// AnnounceWin sends a win message to the server
func (p *Player) AnnounceWin() error {
	if !p.HasWon() {
		return fmt.Errorf("no winning pattern detected")
	}

	winMsg := ClientMessage{Action: "win"}
	return websocket.JSON.Send(p.WS, winMsg)
}

// ReceiveMessage receives a single message from the server
func (p *Player) ReceiveMessage() (ServerMessage, error) {
	var msg ServerMessage
	if err := websocket.JSON.Receive(p.WS, &msg); err != nil {
		return ServerMessage{}, err
	}
	return msg, nil
}

// Close closes the WebSocket connection
func (p *Player) Close() error {
	if p.WS != nil {
		return p.WS.Close()
	}
	return nil
}

// HandleMark processes a mark command: validate, mark cell, check win
// Returns true if player won, false otherwise
func (p *Player) HandleMark(cellID string, inputHandler *shared.InputHandler, maxCellNum int) (bool, error) {
	// Mark the cell
	if err := p.GameSession.Board.MarkCell(cellID); err != nil {
		return false, err
	}

	// Clear screen and redraw board
	fmt.Print("\033[H\033[2J")
	shared.PrintBoard(p.GameSession.Board)

	// Check for win
	if p.GameSession.CheckWin() {
		return true, nil
	}

	// Prompt for next move
	fmt.Println("\n" + inputHandler.PromptMessage())
	return false, nil
}

// HandleBoard redisplays the current board
func (p *Player) HandleBoard(inputHandler *shared.InputHandler) {
	fmt.Print("\033[H\033[2J")
	shared.PrintBoard(p.GameSession.Board)
	fmt.Println("\n" + inputHandler.PromptMessage())
}

// HandleInvalidInput displays an error message for invalid input
func (p *Player) HandleInvalidInput(inputHandler *shared.InputHandler, maxCellNum int) {
	fmt.Println(inputHandler.InvalidInputMessage(maxCellNum))
}
