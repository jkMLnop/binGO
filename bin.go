package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
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
	mode := flag.String("mode", "standalone", "standalone, server, client, or both")
	serverAddr := flag.String("server", "localhost:8080", "server address for client mode")
	port := flag.String("port", "8080", "port for server mode")
	flag.Parse()

	switch *mode {
	case "standalone":
		runStandalone()
	case "server":
		runServer(*port)
	case "client":
		runClient(*serverAddr)
	case "both":
		log.Fatal("Both mode not yet implemented")
	default:
		log.Fatalf("Unknown mode: %s. Use 'standalone', 'server', 'client', or 'both'", *mode)
	}
}

func runStandalone() {
	// Load buzzwords from CSV
	buzzwords := shared.LoadBuzzwords("buzzwords.csv")

	// Create and start a new standalone game
	game := standalone.NewGame(buzzwords)
	game.Start()
}

func runServer(port string) {
	// Load buzzwords from CSV
	buzzwords := shared.LoadBuzzwords("buzzwords.csv")

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

func runClient(serverAddr string) {
	rand.Seed(time.Now().UnixNano())

	// Connect to server via WebSocket
	wsURL := "ws://" + serverAddr + "/ws"
	player := client.NewPlayer(wsURL)
	if err := player.Connect(); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer player.Close()

	// Channel to signal when game ends (from server message listener)
	gameDone := make(chan bool, 1)

	// Channel for user input (so we can select on it)
	inputChan := make(chan string, 1)

	// Spawn goroutine to listen for server messages
	go func() {
		if err := player.ListenForMessages(); err != nil {
			log.Printf("Server disconnected: %v", err)
		}
		gameDone <- true
	}()

	// Spawn goroutine to read user input (non-blocking)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			input, _ := reader.ReadString('\n')
			inputChan <- strings.TrimSpace(input)
		}
	}()

	// Command loop using select for non-blocking I/O
	fmt.Println("\nEnter a number (1-9) to mark a cell, 'board' to redisplay, 'win' to announce, or 'q' to quit:")

	for {
		fmt.Print("> ")

		// Wait for either user input or game end
		select {
		case <-gameDone:
			// Game ended (winner announced)
			os.Exit(0)

		case input := <-inputChan:
			if input == "" {
				continue
			}

			// Check for text commands first
			switch input {
			case "q", "quit":
				fmt.Println("Goodbye!")
				os.Exit(0)

			case "board":
				fmt.Print("\033[H\033[2J") // Clear screen
				shared.PrintBoard(player.GameSession.Board)
				fmt.Println("\nEnter a number (1-9) to mark a cell, 'board' to redisplay, 'win' to announce, or 'q' to quit:")
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
			}

			// Try to parse as numeric cell ID (1-9 for 3x3)
			cellNum, err := strconv.Atoi(input)
			if err != nil || cellNum < 1 || cellNum > 9 {
				fmt.Println("Invalid input. Please enter a number between 1-9, 'board', 'win', or 'q'.")
				continue
			}

			// Convert numeric input to cell ID for 3x3 board
			cellID := strconv.Itoa(cellNum)

			// Mark the cell using shared board logic
			if err := player.GameSession.Board.MarkCell(cellID); err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			// Clear screen and redraw board
			fmt.Print("\033[H\033[2J")
			shared.PrintBoard(player.GameSession.Board)

			// Check for win
			if player.GameSession.CheckWin() {
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

			fmt.Println("\nEnter a number (1-9) to mark a cell, 'board' to redisplay, 'win' to announce, or 'q' to quit:")

			// Small delay to allow messages from server to be printed
			time.Sleep(100 * time.Millisecond)
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
