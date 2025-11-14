package main

import (
	"flag"
	"log"

	"github.com/jkMLnop/binGO-CLI/shared"
	"github.com/jkMLnop/binGO-CLI/standalone"
)

func main() {
	mode := flag.String("mode", "standalone", "standalone, server, client, or both")
	server := flag.String("server", "localhost:8080", "server address for client mode")
	flag.Parse()

	switch *mode {
	case "standalone":
		runStandalone()
	case "server":
		log.Fatal("Server mode not yet implemented")
	case "client":
		log.Fatal("Client mode not yet implemented (connect to server: " + *server + ")")
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
