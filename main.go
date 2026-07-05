package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jkMLnop/binGO/server"
)

//go:embed all:web-client/dist
var webClientDist embed.FS

// spaFileServer serves the embedded web client, falling back to index.html for
// unknown paths so that React Router's client-side routing works correctly.
func spaFileServer(dist fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := dist.Open(p); err != nil {
			// Path is a SPA route, not a file — serve index.html
			http.ServeFileFS(w, r, dist, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// loadBuzzwordsWithFallback loads buzzwords from buzzwords_full.csv if it
// exists, falling back to buzzwords.csv.
func loadBuzzwordsWithFallback() ([][]string, error) {
	buzzwords, err := loadBuzzwords("buzzwords_full.csv")
	if err == nil {
		return buzzwords, nil
	}
	buzzwords, err = loadBuzzwords("buzzwords.csv")
	if err != nil {
		return nil, fmt.Errorf("could not load buzzwords: %w", err)
	}
	return buzzwords, nil
}

// loadBuzzwords reads a CSV file and returns all rows.
func loadBuzzwords(filename string) ([][]string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var rows [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		rows = append(rows, strings.Split(line, ","))
	}
	if len(rows) < 9 {
		return nil, fmt.Errorf("not enough rows in %s: have %d, need at least 9", filename, len(rows))
	}
	return rows, nil
}

// version is set at build time via -ldflags "-X main.version=<value>"
var version = "dev"

func main() {
	port := flag.String("port", "8080", "port for server")
	dbPath := flag.String("db", "", "path to SQLite database file (optional, e.g., ./bingo.db)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	// Load buzzwords from CSV (prefer buzzwords_full.csv if available)
	buzzwords, err := loadBuzzwordsWithFallback()
	if err != nil {
		log.Fatalf("Failed to load buzzwords: %v", err)
	}

	// Create server (3x3 for speed bingo mode)
	srv := server.NewServer(buzzwords, 3, 3, *port)

	// Initialize database if path provided
	var dbConfig *server.DBConfig
	if *dbPath != "" {
		var dbErr error
		dbConfig, dbErr = server.NewDBConfig(*dbPath)
		if dbErr != nil {
			log.Fatalf("Failed to initialize database: %v", dbErr)
		}
		srv.SetDB(dbConfig.Store)
		log.Printf("Database enabled: %s", *dbPath)
	} else {
		log.Println("Running without database (use -db flag to enable)")
	}

	// Initialise DeepSeek LLM client for AI buzzword generation
	deepSeekBaseURL := os.Getenv("DEEPSEEK_BASE_URL")
	if deepSeekBaseURL == "" {
		deepSeekBaseURL = "https://api.deepseek.com"
	}
	deepSeekAPIKey := os.Getenv("DEEPSEEK_API_KEY")
	deepSeekModel := os.Getenv("DEEPSEEK_MODEL")
	if deepSeekModel == "" {
		deepSeekModel = "deepseek-v4-pro"
	}
	deepSeekThinking := false
	if rawThinking := os.Getenv("DEEPSEEK_THINKING"); rawThinking != "" {
		parsedThinking, parseErr := strconv.ParseBool(rawThinking)
		if parseErr != nil {
			log.Printf("Warning: invalid DEEPSEEK_THINKING value %q; defaulting to false", rawThinking)
		} else {
			deepSeekThinking = parsedThinking
		}
	}
	srv.InitLLMClient(deepSeekBaseURL, deepSeekAPIKey, deepSeekModel, deepSeekThinking)

	// Serve embedded web client
	if distFS, fsErr := fs.Sub(webClientDist, "web-client/dist"); fsErr == nil {
		srv.StaticHandler = spaFileServer(distFS)
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
