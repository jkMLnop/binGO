package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

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
		log.Fatal("Client mode not yet implemented (connect to server: " + *serverAddr + ")")
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
