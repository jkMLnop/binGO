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

// Initialize sets up the game session from the welcome message
func (p *Player) Initialize(welcomeMsg ServerMessage) error {
	p.GameSession = shared.NewGameSession(welcomeMsg.Buzzwords, welcomeMsg.Rows, welcomeMsg.Cols)
	return nil
}

// DisplayWelcome displays the welcome message and board to the player
func (p *Player) DisplayWelcome(welcomeMsg ServerMessage) {
	fmt.Printf("\n🎲 Welcome %s!\n", p.PlayerID)
	fmt.Printf("   Game: %s\n", p.GameID)
	fmt.Printf("   Players in game: %v\n", welcomeMsg.Players)

	fmt.Println("\n📋 Your Bingo Board:")
	shared.PrintBoard(p.GameSession.Board)
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

// ListenForMessages listens for messages from the server (blocking)
func (p *Player) ListenForMessages() error {
	for {
		var msg ServerMessage
		if err := websocket.JSON.Receive(p.WS, &msg); err != nil {
			return err
		}

		switch msg.Type {
		case "game_ended":
			fmt.Printf("\n\n🏆 Game Ended! Winner: %s\n", msg.Winner)
			fmt.Printf("   %s\n\n", msg.Message)
			if msg.Winner == p.PlayerID {
				fmt.Println("🎊 You won!")
				// Show win animation for the winner
				shared.DisplayWinScreen()
			}
			return nil
		}
	}
}

// Close closes the WebSocket connection
func (p *Player) Close() error {
	if p.WS != nil {
		return p.WS.Close()
	}
	return nil
}
