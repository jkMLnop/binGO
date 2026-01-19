package client

import (
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/jkMLnop/binGO-CLI/shared"
	"golang.org/x/net/websocket"
)

// Player represents a bingo player client
type Player struct {
	ServerURL    string
	ClientIP     string // Client's local IP (for session tracking)
	WS           *websocket.Conn
	GameID       string
	PlayerID     string
	Username     string
	Token        string              // JWT token for re-authentication
	GameSession  *shared.GameSession // Use shared game logic (includes Board with Rows/Cols)
	DisplayWidth int                 // Cached display width for consistent rendering
	WelcomeMsg   ServerMessage       // Store welcome message for later display
}

// NewPlayer creates a new player client
func NewPlayer(serverURL string) *Player {
	return &Player{
		ServerURL: serverURL,
	}
}

// getLocalIP retrieves the client's local IP address
func (p *Player) getLocalIP() string {
	if p.ClientIP != "" {
		return p.ClientIP
	}

	// Try to get local IP by connecting to a remote server (doesn't actually connect)
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		p.ClientIP = conn.LocalAddr().(*net.UDPAddr).IP.String()
		return p.ClientIP
	}

	// Fallback
	p.ClientIP = "localhost"
	return p.ClientIP
}

// Connect establishes WebSocket connection to server and performs authentication handshake
// If token is provided, uses it for reconnection. Otherwise requires username.
// code parameter is for Phase 7.3 remote player joining (optional for localhost/LAN)
func (p *Player) Connect(username string, token string, code string) (ServerMessage, error) {
	origin := "http://localhost"

	// Construct WS URL from server address
	wsURL := p.ServerURL

	// Strip any existing protocol and /ws path
	wsURL = strings.TrimPrefix(wsURL, "ws://")
	wsURL = strings.TrimPrefix(wsURL, "wss://")
	wsURL = strings.TrimSuffix(wsURL, "/ws")

	// Auto-detect protocol based on server URL
	if strings.Contains(wsURL, "ngrok") {
		// ngrok uses HTTPS, so use wss:// (WebSocket Secure)
		wsURL = "wss://" + wsURL + "/ws"
	} else {
		// Local/LAN connections use plain ws://
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

	// Send login message with username or token and optional code (Phase 7.3)
	loginMsg := ClientMessage{
		Action:   "login",
		Username: username,
		Token:    token,
		Code:     code,
	}
	if err := websocket.JSON.Send(ws, loginMsg); err != nil {
		ws.Close()
		return ServerMessage{}, fmt.Errorf("failed to send login: %w", err)
	}

	// Receive welcome message
	var welcomeMsg ServerMessage
	if err := websocket.JSON.Receive(ws, &welcomeMsg); err != nil {
		ws.Close()
		return ServerMessage{}, fmt.Errorf("failed to receive welcome: %w", err)
	}

	if welcomeMsg.Type != "welcome" {
		ws.Close()
		return ServerMessage{}, fmt.Errorf("unexpected message type: %s, message: %s", welcomeMsg.Type, welcomeMsg.Message)
	}

	p.GameID = welcomeMsg.GameID
	p.PlayerID = welcomeMsg.PlayerID
	p.Username = welcomeMsg.Username
	p.Token = welcomeMsg.Token

	log.Printf("Successfully authenticated as %s, received JWT token and game code: %s", p.Username, welcomeMsg.Code)

	return welcomeMsg, nil
}

// ConnectWithAuth handles the full authentication flow with token/username prompts
// This is the main entry point for client connections
// code parameter is for Phase 7.3 remote player joining (optional for localhost/LAN)
func (p *Player) ConnectWithAuth(code string) (ServerMessage, error) {
	// Initialize auth manager
	authMgr := NewAuthManager()

	// Get local IP for session tracking
	p.getLocalIP()

	// Try to load saved session
	lastUsername, lastToken, _ := authMgr.LoadSession(p.ClientIP)

	var username, token string

	if lastToken != "" && lastUsername != "" {
		// Ask if they want to reconnect
		reuse, err := authMgr.PromptForReconnect(lastUsername)
		if err != nil {
			return ServerMessage{}, err
		}

		if reuse {
			username = lastUsername
			token = lastToken
			log.Printf("Reconnecting as %s with saved token", username)
		} else {
			// User wants new username
			newUsername, err := authMgr.PromptForUsername("")
			if err != nil {
				return ServerMessage{}, err
			}
			username = newUsername
			token = "" // New login
		}
	} else {
		// No saved session, prompt for username
		newUsername, err := authMgr.PromptForUsername(lastUsername)
		if err != nil {
			return ServerMessage{}, err
		}
		username = newUsername
		token = ""
	}

	// Connect to server with code (Phase 7.3)
	welcomeMsg, err := p.Connect(username, token, code)
	if err != nil {
		return ServerMessage{}, err
	}

	// Save session for future use
	if err := authMgr.SaveSession(p.ClientIP, p.Username, p.Token); err != nil {
		log.Printf("Warning: failed to save session: %v", err)
	}

	return welcomeMsg, nil
}

// Close closes the WebSocket connection
func (p *Player) Close() error {
	if p.WS != nil {
		return p.WS.Close()
	}
	return nil
}

// AnnounceWin sends a win message to the server
func (p *Player) AnnounceWin() error {
	if !p.hasWon() {
		return fmt.Errorf("no winning pattern detected")
	}

	winMsg := ClientMessage{Action: "win"}
	return websocket.JSON.Send(p.WS, winMsg)
}

// AnnounceRestart sends a restart request to the server (host only)
func (p *Player) AnnounceRestart() error {
	restartMsg := ClientMessage{Action: "restart"}
	return websocket.JSON.Send(p.WS, restartMsg)
}

// ReceiveMessage receives a single message from the server
func (p *Player) ReceiveMessage() (ServerMessage, error) {
	var msg ServerMessage
	if err := websocket.JSON.Receive(p.WS, &msg); err != nil {
		return ServerMessage{}, err
	}
	return msg, nil
}

// hasWon checks if the player has a winning pattern using shared game logic
func (p *Player) hasWon() bool {
	return p.GameSession.CheckWin()
}

// HandleMark processes a mark command: validate, mark cell, check win
// Returns true if player won, false otherwise
func (p *Player) HandleMark(cellID string, maxCellNum int) (bool, error) {
	// Mark the cell
	if err := p.GameSession.Board.MarkCell(cellID); err != nil {
		return false, err
	}

	// Clear screen and redraw banner + board
	fmt.Print("\033[H\033[2J")
	shared.DisplayBannerWithWidth(p.DisplayWidth)
	shared.PrintBoard(p.GameSession.Board)

	// Display game info below board
	p.DisplayWelcome(p.WelcomeMsg)

	// Check for win
	if p.GameSession.CheckWin() {
		fmt.Println("\n🎉 YOU WIN! 🎉")
		return true, nil
	}

	// Don't print prompt here - let the event loop handle it via printPrompt()
	return false, nil
}

// HandleBoard redisplays the current board with game info
func (p *Player) HandleBoard() {
	fmt.Print("\033[H\033[2J")
	shared.DisplayBannerWithWidth(p.DisplayWidth)
	shared.PrintBoard(p.GameSession.Board)

	// Display game info below board
	p.DisplayWelcome(p.WelcomeMsg)

	// Don't print prompt here - let the event loop handle it via printPrompt()
}

// HandleInvalidInput displays an error message for invalid input
func (p *Player) HandleInvalidInput(inputHandler *shared.InputHandler, maxCellNum int) {
	fmt.Println(inputHandler.InvalidInputMessage(maxCellNum))
}
