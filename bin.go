package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jkMLnop/binGO-CLI/client"
	"github.com/jkMLnop/binGO-CLI/server"
	"github.com/jkMLnop/binGO-CLI/shared"
	"github.com/jkMLnop/binGO-CLI/standalone"
)

// version is set at build time via -ldflags "-X main.version=<value>"
var version = "dev"

func main() {
	mode := flag.String("mode", "standalone", "standalone, server, or client")
	serverAddr := flag.String("server", "localhost:8080", "server address for client mode (e.g., localhost:8080, 192.168.1.100:8080)")
	code := flag.String("code", "", "game code for joining (Phase 7.3: required for remote connections)")
	port := flag.String("port", "8080", "port for server mode")
	dbPath := flag.String("db", "", "path to SQLite database file (Phase 7.5: optional, e.g., ./bingo.db)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

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

	// Initialize database if path provided
	var dbConfig *server.DBConfig
	if dbPath != "" {
		var dbErr error
		dbConfig, dbErr = server.NewDBConfig(dbPath)
		if dbErr != nil {
			log.Fatalf("Failed to initialize database: %v", dbErr)
		}
		srv.SetDB(dbConfig.Store)
		log.Printf("Database enabled: %s", dbPath)
	} else {
		log.Println("Running without database (use -db flag to enable)")
	}

	// Bootstrap OpenTelemetry tracing → Grafana Tempo (or OTEL_EXPORTER_OTLP_ENDPOINT)
	shutdownTracer, tracerErr := server.InitTracer(srv)
	if tracerErr != nil {
		log.Printf("Warning: failed to init tracer (traces disabled): %v", tracerErr)
	}

	// Handle graceful shutdown (SIGINT = Ctrl-C, SIGTERM = Docker/k8s stop)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for interrupt
	<-sigChan
	log.Println("\nShutting down server...")

	// Notify all connected players before closing
	srv.NotifyShutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}

	// Close DB with the same shutdown deadline
	if dbConfig != nil {
		if err := dbConfig.Close(ctx); err != nil {
			log.Printf("DB close error: %v", err)
		}
	}

	// Flush remaining spans before exit
	if shutdownTracer != nil {
		shutdownTracer(ctx)
	}

	log.Println("Server stopped")
}

// god method but also controller orchestrating model operations pattern
func runClient(serverAddr string, code string) {
	// === Setup Client Connection ===
	// Pass raw server address; player.Connect() will add ws:// or wss:// protocol and /ws path
	player := client.NewPlayer(serverAddr)

	// Show menu when no code was provided on the command line
	if code == "" {
		choice, menuCode, buzzwords, menuErr := client.ShowMainMenu(serverAddr)
		if menuErr != nil {
			log.Fatalf("Menu error: %v", menuErr)
		}
		code = menuCode
		if choice == "host" {
			player.IsHostMode = true
			player.PendingBuzzwords = buzzwords
		}
	}

	// Use the full auth flow with token/username prompts and local storage
	welcomeMsg, err := player.ConnectWithAuth(code)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer player.Close()

	// Announce game code after host creates a game
	if player.IsHostMode && welcomeMsg.Code != "" {
		fmt.Printf("\n✓ Game created! Code: %s\n   Share this code with players to join.\n", welcomeMsg.Code)
	}

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
	promptMsg := "\nEnter a number (1-" + strconv.Itoa(maxCellNum) + ") to mark a cell, 'help' for all commands, or 'q' to quit:"
	gameEndedPrompt := "\nGame has ended. Type 'q' to quit."

	// Track game state
	gameEnded := false

	// Phase 9/9.5: local display state
	var activeSuggestions []client.Suggestion
	var activeBets []client.Bet

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
			lc := strings.ToLower(input)
			switch {
			case lc == "q" || lc == "quit":
				inputChan <- "quit"
			case lc == "restart":
				inputChan <- "restart"
			case lc == "help":
				inputChan <- "help"
			case lc == "leaderboard":
				inputChan <- "leaderboard"
			case lc == "stats":
				inputChan <- "stats"
			case lc == "list_buzzwords":
				inputChan <- "list_buzzwords"
			case strings.HasPrefix(lc, "add_new_phrase "):
				phrase := strings.TrimSpace(input[len("add_new_phrase "):])
				inputChan <- "suggest:" + phrase
			case strings.HasPrefix(lc, "approve "):
				phrase := strings.TrimSpace(input[len("approve "):])
				inputChan <- "approve:" + phrase
			case strings.HasPrefix(lc, "reject "):
				phrase := strings.TrimSpace(input[len("reject "):])
				inputChan <- "reject:" + phrase
			case strings.HasPrefix(lc, "bet:"):
				betText := strings.TrimSpace(input[len("bet:"):])
				inputChan <- "bet:" + betText
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
				player.DisplaySuggestions(activeSuggestions)
				player.DisplayActiveBets(activeBets)
				printPrompt(promptMsg)
			case "player_update":
				// Update welcome message with new player list and redraw
				player.WelcomeMsg = msg
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession)
				player.DisplayWelcome(player.WelcomeMsg)
				player.DisplaySuggestions(activeSuggestions)
				player.DisplayActiveBets(activeBets)
				printPrompt(promptMsg)
			case "suggestion_broadcast":
				activeSuggestions = msg.Suggestions
				if msg.Message != "" {
					fmt.Println(msg.Message)
				}
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession)
				player.DisplayWelcome(player.WelcomeMsg)
				player.DisplaySuggestions(activeSuggestions)
				player.DisplayActiveBets(activeBets)
				printPrompt(promptMsg)
			case "bets_update":
				activeBets = msg.ActiveBets
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession)
				player.DisplayWelcome(player.WelcomeMsg)
				player.DisplaySuggestions(activeSuggestions)
				player.DisplayActiveBets(activeBets)
				printPrompt(promptMsg)
			case "buzzword_list":
				player.DisplayBuzzwordList(msg)
				printPrompt(promptMsg)
			case "game_ended":
				gameEnded = true
				player.DisplayGameEnd(msg)
				player.DisplayBetResults(activeBets)
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
				gameEnded = false
				activeSuggestions = nil
				activeBets = nil
				// Redisplay board (same as player_update to maintain consistency)
				log.Println("DEBUG: Clearing screen and displaying board")
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession)
				player.DisplayWelcome(player.WelcomeMsg)
				player.DisplaySuggestions(activeSuggestions)
				player.DisplayActiveBets(activeBets)
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

			case "leaderboard":
				printLeaderboard(serverAddr)
				printPrompt(promptMsg)
				continue

			case "stats":
				printPlayerStats(serverAddr, player.PlayerID)
				printPrompt(promptMsg)
				continue

			case "suggest":
				if err := player.SendMessage(client.ClientMessage{Action: "suggest", Phrase: cellID}); err != nil {
					fmt.Printf("❌ Error: %v\n", err)
				}
				continue

			case "approve":
				if err := player.SendMessage(client.ClientMessage{Action: "approve", Phrase: cellID}); err != nil {
					fmt.Printf("❌ Error: %v\n", err)
				}
				continue

			case "reject":
				if err := player.SendMessage(client.ClientMessage{Action: "reject", Phrase: cellID}); err != nil {
					fmt.Printf("❌ Error: %v\n", err)
				}
				continue

			case "bet":
				if err := player.SendMessage(client.ClientMessage{Action: "bet", Phrase: cellID}); err != nil {
					fmt.Printf("❌ Error: %v\n", err)
				}
				continue

			case "list_buzzwords":
				if err := player.SendMessage(client.ClientMessage{Action: "list_buzzwords"}); err != nil {
					fmt.Printf("❌ Error: %v\n", err)
				}
				continue

			case "restart":
				if err := player.AnnounceRestart(); err != nil {
					fmt.Printf("❌ Error: %v\n", err)
					if gameEnded {
						printPrompt(gameEndedPrompt)
					} else {
						printPrompt("")
					}
					continue
				}
				fmt.Println("🔄 Requesting game restart...")
				// Server will validate that player is original host
				continue

			case "help":
				printClientHelp()
				if gameEnded {
					printPrompt(gameEndedPrompt)
				} else {
					printPrompt("")
				}
				continue

			case "invalid":
				fmt.Printf("Invalid input. Please enter a number between 1-%d, or type 'help' for commands.\n", maxCellNum)
				if gameEnded {
					printPrompt(gameEndedPrompt)
				} else {
					printPrompt("")
				}
				continue

			case "mark":
				won, err := player.HandleMark(cellID, maxCellNum)
				if err != nil {
					fmt.Printf("Error: %v\n", err)
					if gameEnded {
						printPrompt(gameEndedPrompt)
					} else {
						printPrompt("")
					}
					continue
				}

				// Full redraw including suggestions and bets
				fmt.Print("\033[H\033[2J")
				shared.DisplayBannerWithWidth(player.DisplayWidth)
				shared.PrintBoard(player.GameSession)
				player.DisplayWelcome(player.WelcomeMsg)
				player.DisplaySuggestions(activeSuggestions)
				player.DisplayActiveBets(activeBets)

				// Check if player won
				if won {
					fmt.Println("\n🎉 YOU WIN! 🎉")
					// Announce win to server immediately (broadcasts game_ended to all players)
					if err := player.AnnounceWin(); err != nil {
						fmt.Printf("Error announcing win: %v\n", err)
						if gameEnded {
							printPrompt(gameEndedPrompt)
						} else {
							printPrompt("")
						}
						continue
					}

					fmt.Println("🎉 Announcing win to server...")
					// Server will broadcast game_ended to all players
					continue
				}

				// Small delay to allow messages from server to be printed
				time.Sleep(100 * time.Millisecond)
				if gameEnded {
					printPrompt(gameEndedPrompt)
				} else {
					printPrompt(promptMsg)
				}
			}
		}
	}
}

func printClientHelp() {
	fmt.Println("\n📝 Commands:")
	fmt.Println("  1-9                         - Mark a cell by number")
	fmt.Println("  restart                     - Restart game (host only)")
	fmt.Println("  add_new_phrase <phrase>     - Suggest a buzzword")
	fmt.Println("  approve <phrase>            - Approve suggestion (host only)")
	fmt.Println("  reject <phrase>             - Reject suggestion (host only)")
	fmt.Println("  list_buzzwords              - Show current buzzword pool + rejected")
	fmt.Println("  bet: <player> wins|loses    - Place a bet (AND to chain)")
	fmt.Println("  leaderboard                 - Show top players")
	fmt.Println("  stats                       - Show your stats")
	fmt.Println("  help                        - Show this help")
	fmt.Println("  quit / q                    - Exit game")
}

// getHTTPBaseURL converts a server address into an HTTP base URL.
func getHTTPBaseURL(serverAddr string) string {
	addr := strings.TrimPrefix(serverAddr, "ws://")
	addr = strings.TrimPrefix(addr, "wss://")
	addr = strings.TrimSuffix(addr, "/ws")
	if strings.Contains(addr, "fly.dev") || strings.Contains(addr, "ngrok") {
		return "https://" + addr
	}
	return "http://" + addr
}

// printLeaderboard fetches and displays the top-10 leaderboard.
func printLeaderboard(serverAddr string) {
	baseURL := getHTTPBaseURL(serverAddr)
	resp, err := http.Get(baseURL + "/api/leaderboard?limit=10") //nolint:noctx
	if err != nil {
		fmt.Printf("❌ Failed to fetch leaderboard: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Println("❌ Invalid leaderboard response")
		return
	}
	fmt.Println("\n🏆 Leaderboard:")
	if data, ok := result["data"]; ok {
		if entries, ok := data.([]interface{}); ok {
			for _, e := range entries {
				if entry, ok := e.(map[string]interface{}); ok {
					rank := entry["rank"]
					uname := entry["username"]
					wins := entry["wins"]
					fmt.Printf("  #%.0f %-20s %v wins\n", rank, uname, wins)
				}
			}
		}
	}
}

// printPlayerStats fetches and displays stats for a given username.
func printPlayerStats(serverAddr, username string) {
	baseURL := getHTTPBaseURL(serverAddr)
	resp, err := http.Get(baseURL + "/api/player/" + username + "/stats") //nolint:noctx
	if err != nil {
		fmt.Printf("❌ Failed to fetch stats: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		fmt.Println("❌ Invalid stats response")
		return
	}
	if data, ok := result["data"]; ok {
		if stats, ok := data.(map[string]interface{}); ok {
			fmt.Printf("\n📊 Stats for %s:\n", username)
			fmt.Printf("   Wins:         %v\n", stats["wins"])
			fmt.Printf("   Games Played: %v\n", stats["games_played"])
			if wr, ok := stats["win_rate"].(float64); ok {
				fmt.Printf("   Win Rate:     %.1f%%\n", wr*100)
			}
		}
	}
}
