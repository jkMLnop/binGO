package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/jkMLnop/binGO-CLI/client"
	"github.com/jkMLnop/binGO-CLI/server"
	"github.com/jkMLnop/binGO-CLI/shared"
	"github.com/jkMLnop/binGO-CLI/standalone"
)

func main() {
	mode := flag.String("mode", "standalone", "standalone, server, or client")
	serverAddr := flag.String("server", "localhost:8080", "server address for client mode (e.g., localhost:8080, 192.168.1.100:8080)")
	code := flag.String("code", "", "game code for joining (Phase 7.3: required for remote connections)")
	port := flag.String("port", "8080", "port for server mode")
	dbPath := flag.String("db", "", "path to SQLite database file (Phase 7.5: optional, e.g., ./bingo.db)")
	flag.Parse()

	switch *mode {
	case "standalone":
		runStandalone()
	case "server":
		runServer(*port, *dbPath)
	case "client":
		runClient(*serverAddr, *code)
	default:
		log.Fatalf("Unknown mode: %s. Use 'standalone', 'server', or 'client'", *mode)
	}
}

func runStandalone() {
	// Load buzzwords from CSV (prefer buzzwords_full.csv if available)
	buzzwords, err := shared.LoadBuzzwordsWithFallback()
	if err != nil {
		log.Fatalf("Failed to load buzzwords: %v", err)
	}

	// Create and run a new standalone game
	game := standalone.NewGame(buzzwords)
	game.RunGame()
}

func runServer(port string, dbPath string) {
	// Load buzzwords from CSV (prefer buzzwords_full.csv if available)
	buzzwords, err := shared.LoadBuzzwordsWithFallback()
	if err != nil {
		log.Fatalf("Failed to load buzzwords: %v", err)
	}

	// Create server (3x3 for speed bingo mode)
	srv := server.NewServer(buzzwords, 3, 3, port)

	// Initialize database if path provided (Phase 7.5)
	if dbPath != "" {
		dbConfig, err := server.NewDBConfig(dbPath)
		if err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		srv.SetDB(dbConfig.Store)
		defer dbConfig.Close()
		log.Printf("Database enabled: %s", dbPath)
	} else {
		log.Println("Running without database (use -db flag to enable)")
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for interrupt
	<-sigChan
	log.Println("\nShutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}
	log.Println("Server stopped")
}

// god method but also controller orchestrating model operations pattern
func runClient(serverAddr string, code string) {
	// === Setup Client Connection ===
	// Pass raw server address; player.Connect() will add ws:// or wss:// protocol and /ws path
	player := client.NewPlayer(serverAddr)

	// Use the full auth flow with token/username prompts and local storage
	welcomeMsg, err := player.ConnectWithAuth(code)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer player.Close()

	// Initialize game session from welcome message
	player.GameSession = shared.NewGameSession(welcomeMsg.Buzzwords, welcomeMsg.Rows, welcomeMsg.Cols)

	// Calculate display width based on board size
	player.DisplayWidth = shared.CalculateBoardWidth(player.GameSession.Cols, player.GameSession.ColWidths)

	// Store welcome message for later display
	player.WelcomeMsg = welcomeMsg

	// === Setup Communication Channels ===
	// Channel for user input (so we can select on it)
	inputChan := make(chan string, 1)
	// Channel for server messages (so we can select on it)
	serverMsgChan := make(chan client.ServerMessage, 10)

	// Calculate max cell number for the board dimensions
	maxCellNum := welcomeMsg.Rows * welcomeMsg.Cols
	promptMsg := "\nEnter a number (1-" + strconv.Itoa(maxCellNum) + ") to mark a cell, 'board' to redisplay, or 'q' to quit:"

	// === Setup Server Listener ===
	// Spawn goroutine to listen for server messages
	go func() {
		for {
			msg, err := player.ReceiveMessage()
			if err != nil {
				log.Printf("Server disconnected: %v", err)
				os.Exit(0)
				return
			}
			// Send message through channel so main loop can handle it
			serverMsgChan <- msg
		}
	}()

	// === Setup Input Listener ===
	// Spawn goroutine to read user input (non-blocking)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				continue
			}

			// Check for text commands first
			switch input {
			case "q", "quit":
				inputChan <- "quit"
			case "board":
				inputChan <- "board"
			case "restart":
				inputChan <- "restart"
			case "help":
				inputChan <- "help"
			default:
				// Try to parse as numeric cell ID
				cellNum, err := strconv.Atoi(input)
				if err != nil || cellNum < 1 || cellNum > maxCellNum {
					inputChan <- "invalid"
				} else {
					inputChan <- "mark:" + strconv.Itoa(cellNum)
				}
			}
		}
	}()

	// === Command Loop ===
	// Wait for the first player_update from server to display the board
	// Then start the command loop
	firstUpdate := true

	// Track warning message to display as header
	var warningMessage string

	// Helper function to consistently format warning and prompt
	printPrompt := func(promptText string) {
		if warningMessage != "" {
			fmt.Println()
			fmt.Println("⚠️  WARNING: " + warningMessage)
			fmt.Println()
		}
		if promptText != "" {
			fmt.Println(promptText)
		}
		fmt.Print("> ")
	}

	for {
		// Wait for either user input or server messages
		select {
		case msg := <-serverMsgChan:
			// Handle server message
			switch msg.Type {
			case "error":
				warningMessage = msg.Message
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession)
				player.DisplayWelcome(player.WelcomeMsg)
				printPrompt(promptMsg)
			case "player_update":
				// Update welcome message with new player list and redraw
				player.WelcomeMsg = msg
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession)
				player.DisplayWelcome(player.WelcomeMsg)
				printPrompt(promptMsg)
			case "game_ended":
				player.DisplayGameEnd(msg)
				// Show restart option if user is host
				if msg.HostID == player.PlayerID {
					printPrompt("\nType 'restart' to start a new game or 'q' to quit.")
				} else {
					// Non-host players wait for host
					printPrompt("\nWaiting for host to restart or type 'q' to quit.")
				}
			case "game_restart":
				// Game restarted - reset board and continue playing
				log.Printf("DEBUG: Received game_restart message from server")
				log.Printf("DEBUG: Message: Type=%s, Code=%s, Buzzwords len=%d, Rows=%d, Cols=%d",
					msg.Type, msg.Code, len(msg.Buzzwords), msg.Rows, msg.Cols)
				// Reset the game session with new buzzwords
				if len(msg.Buzzwords) > 0 {
					log.Println("DEBUG: Creating new game session with buzzwords")
					player.GameSession = shared.NewGameSession(msg.Buzzwords, msg.Rows, msg.Cols)
					log.Println("DEBUG: New game session created successfully")
				} else {
					log.Printf("DEBUG: Buzzwords is empty! len=%d", len(msg.Buzzwords))
				}
				// Update welcome message and clear warning on restart
				player.WelcomeMsg = msg
				warningMessage = ""
				// Redisplay board (same as player_update to maintain consistency)
				log.Println("DEBUG: Clearing screen and displaying board")
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession)
				player.DisplayWelcome(player.WelcomeMsg)
				fmt.Println("\n🔄 New round started! " + promptMsg)
				fmt.Print("> ")
			}

		case input := <-inputChan:
			// Mark that we've had our first update
			if firstUpdate {
				firstUpdate = false
			}

			// Parse command:cellID format
			parts := strings.SplitN(input, ":", 2)
			command := parts[0]
			var cellID string
			if len(parts) > 1 {
				cellID = parts[1]
			}

			switch command {
			case "q", "quit":
				fmt.Println("Goodbye!")
				os.Exit(0)

			case "board":
				player.HandleBoard()
				printPrompt("")
				continue

			case "restart":
				if err := player.AnnounceRestart(); err != nil {
					fmt.Printf("❌ Error: %v\n", err)
					printPrompt("")
					continue
				}
				fmt.Println("🔄 Requesting game restart...")
				// Server will validate that player is original host
				continue

			case "help":
				printClientHelp()
				printPrompt("")
				continue

			case "invalid":
				fmt.Printf("Invalid input. Please enter a number between 1-%d, or type 'help' for commands.\n", maxCellNum)
				printPrompt("")
				continue

			case "mark":
				won, err := player.HandleMark(cellID, maxCellNum)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
					printPrompt("")
					continue
				}

				// Check if player won
				if won {
					// Announce win to server immediately (broadcasts game_ended to all players)
					if err := player.AnnounceWin(); err != nil {
						fmt.Printf("Error announcing win: %v\n", err)
						printPrompt("")
						continue
					}

					fmt.Println("🎉 Announcing win to server...")
					// Server will broadcast game_ended to all players
					// Non-hosts will see the message and can quit
					// Host can send restart
					continue
				}

				// Small delay to allow messages from server to be printed
				time.Sleep(100 * time.Millisecond)
				printPrompt(promptMsg)
			}
		}
	}
}

func printClientHelp() {
	fmt.Println("\n📝 Commands:")
	fmt.Println("  1-9              - Mark a cell")
	fmt.Println("  'board'          - Redisplay the board")
	fmt.Println("  'restart'        - Restart game (host only)")
	fmt.Println("  'help'           - Show this help")
	fmt.Println("  'quit' or 'q'    - Exit game")
}
