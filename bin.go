package main

import (
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
	port := flag.String("port", "8080", "port for server mode")
	flag.Parse()

	switch *mode {
	case "standalone":
		runStandalone()
	case "server":
		runServer(*port)
	case "client":
		runClient(*serverAddr)
	default:
		log.Fatalf("Unknown mode: %s. Use 'standalone', 'server', or 'client'", *mode)
	}
}

func runStandalone() {
	// Load buzzwords from CSV
	buzzwords, err := shared.LoadBuzzwords("buzzwords.csv")
	if err != nil {
		log.Fatalf("Failed to load buzzwords: %v", err)
	}

	// Create and run a new standalone game
	game := standalone.NewGame(buzzwords)
	game.RunGame()
}

func runServer(port string) {
	// Load buzzwords from CSV
	buzzwords, err := shared.LoadBuzzwords("buzzwords.csv")
	if err != nil {
		log.Fatalf("Failed to load buzzwords: %v", err)
	}

	// Create server (3x3 for speed bingo mode)
	srv := server.NewServer(buzzwords, 3, 3, port)

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
func runClient(serverAddr string) {
	// === Setup Client Connection ===
	// Connect to server via WebSocket
	wsURL := "ws://" + serverAddr + "/ws"
	player := client.NewPlayer(wsURL)

	// Use the full auth flow with token/username prompts and local storage
	welcomeMsg, err := player.ConnectWithAuth()
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer player.Close()

	// Initialize game session from welcome message
	player.GameSession = shared.NewGameSession(welcomeMsg.Buzzwords, welcomeMsg.Rows, welcomeMsg.Cols)

	// Calculate display width based on board size
	player.DisplayWidth = shared.CalculateBoardWidth(player.GameSession.Board.Cols, player.GameSession.Board.ColWidths)
	
	// Store welcome message for later display
	player.WelcomeMsg = welcomeMsg

	// Display banner and board first
	shared.DisplayBannerWithWidth(player.DisplayWidth)
	shared.PrintBoard(player.GameSession.Board)
	
	// Display initial game info
	player.DisplayWelcome(welcomeMsg)

	// === Setup Communication Channels ===
	// Channel to signal when game ends (from server message listener)
	gameDone := make(chan bool, 1)

	// Channel for user input (so we can select on it)
	inputChan := make(chan string, 1)

	// Create input handler for the board dimensions
	maxCellNum := welcomeMsg.Rows * welcomeMsg.Cols
	inputHandler := shared.NewInputHandler(maxCellNum, "\nEnter a number (1-"+strconv.Itoa(maxCellNum)+") to mark a cell, 'board' to redisplay, 'win' to announce, or 'q' to quit:")

	// === Setup Server Listener ===
	// Spawn goroutine to listen for server messages
	go func() {
		for {
			msg, err := player.ReceiveMessage()
			if err != nil {
				log.Printf("Server disconnected: %v", err)
				gameDone <- true
				return
			}

			switch msg.Type {
			case "player_update":
				// Update welcome message with new player list and redraw
				player.WelcomeMsg = msg
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession.Board)
				player.DisplayWelcome(player.WelcomeMsg)
				fmt.Println("\n" + inputHandler.PromptMessage())
				fmt.Print("> ")
			case "game_ended":
				player.DisplayGameEnd(msg)
				gameDone <- true
				return
			}
		}
	}()

	// === Setup Input Listener ===
	// Spawn goroutine to read user input (non-blocking)
	go func() {
		for {
			cellID, command, _ := inputHandler.ProcessInput()
			// Encode the result as "command:cellID" or just "command"
			if command == "mark" {
				inputChan <- "mark:" + cellID
			} else {
				inputChan <- command
			}
		}
	}()

	// === Command Loop ===
	// Command loop using select for non-blocking I/O
	fmt.Println(inputHandler.PromptMessage())

	for {
		fmt.Print("> ")

		// Wait for either user input or game end
		select {
		case <-gameDone:
			// Game ended (winner announced)
			os.Exit(0)

		case input := <-inputChan:
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
				player.HandleBoard(inputHandler)
				continue

			case "win":
				if err := player.AnnounceWin(); err != nil {
					fmt.Printf("❌ %v\n", err)
					continue
				}
				fmt.Println("🎉 Announcing win to server...")
				// Wait for game_ended message from server
				<-gameDone
				os.Exit(0)

			case "help":
				printClientHelp()
				continue

			case "invalid":
				player.HandleInvalidInput(inputHandler, maxCellNum)
				continue

			case "mark":
				won, err := player.HandleMark(cellID, inputHandler, maxCellNum)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
					continue
				}

				// Check if player won
				if won {
					// Announce win to server immediately (broadcasts game_ended to all players)
					if err := player.AnnounceWin(); err != nil {
						fmt.Printf("Error announcing win: %v\n", err)
						os.Exit(0)
					}

					fmt.Println("🎉 Announcing win to server...")
					// Wait for game_ended message from server (all players get kicked)
					<-gameDone

					// Now show the win animation for the winner
					shared.DisplayWinScreen()
					os.Exit(0)
				}

				// Small delay to allow messages from server to be printed
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

func printClientHelp() {
	fmt.Println("\n📝 Commands:")
	fmt.Println("  'mark <row> <col>' - Mark a cell (e.g., mark 0 1)")
	fmt.Println("  'board' - Redisplay the board")
	fmt.Println("  'win' - Announce you've won (must have winning pattern)")
	fmt.Println("  'help' - Show this help")
	fmt.Println("  'quit' - Exit game")
}
