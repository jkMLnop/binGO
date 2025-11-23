package client

import (
	"fmt"
	"log"

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

// Connect establishes WebSocket connection to server
func (p *Player) Connect() error {
	origin := "http://localhost:8080"
	ws, err := websocket.Dial(p.ServerURL, "", origin)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	p.WS = ws
	log.Printf("Connected to server")

	// Receive welcome message
	var welcomeMsg ServerMessage
	if err := websocket.JSON.Receive(ws, &welcomeMsg); err != nil {
		return fmt.Errorf("failed to receive welcome: %w", err)
	}

	if welcomeMsg.Type != "welcome" {
		return fmt.Errorf("unexpected message type: %s", welcomeMsg.Type)
	}

	p.GameID = welcomeMsg.GameID
	p.PlayerID = welcomeMsg.PlayerID

	fmt.Printf("\n🎲 Welcome %s!\n", p.PlayerID)
	fmt.Printf("   Game: %s\n", p.GameID)
	fmt.Printf("   Players in game: %v\n", welcomeMsg.Players)

	// Generate board from buzzwords using shared game logic
	p.GameSession = shared.NewGameSession(welcomeMsg.Buzzwords, welcomeMsg.Rows, welcomeMsg.Cols)

	fmt.Println("\n📋 Your Bingo Board:")
	shared.PrintBoard(p.GameSession.Board)

	return nil
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
