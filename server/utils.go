package server

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
)

// GenerateGameCode generates a random alphanumeric code for a game session
func GenerateGameCode() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const codeLength = 5 // 5 chars after BINGO-
	b := make([]byte, codeLength)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			panic(err)
		}
		b[i] = charset[num.Int64()]
	}
	// Format as BINGO-XXXXX
	return fmt.Sprintf("BINGO-%s", string(b))
}

// metricsAuthMiddleware protects /metrics with a bearer token when
// METRICS_AUTH_TOKEN is set. If the env var is empty, the handler is
// unprotected (local dev / docker-compose behaviour is unchanged).
func metricsAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("METRICS_AUTH_TOKEN")
		if token == "" {
			// No token configured — allow unauthenticated access.
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		provided := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
