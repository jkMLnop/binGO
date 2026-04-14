package server

import (
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

const (
	// maxConnsPerIP is the maximum number of simultaneous WebSocket connections
	// allowed from a single IP address.
	maxConnsPerIP = 5

	// codeGuessPerWindow is the number of failed game-code attempts an IP is
	// allowed before being rate-limited.
	codeGuessPerWindow = 5

	// codeGuessWindow is the sliding window over which the token bucket refills.
	codeGuessWindow = 60 * time.Second
)

// getCodeLimiter returns the per-IP rate.Limiter for game-code guesses,
// creating a new one if none exists yet for this IP.
func (s *Server) getCodeLimiter(ip string) *rate.Limiter {
	s.CodeLimitersMu.Lock()
	defer s.CodeLimitersMu.Unlock()
	if l, ok := s.CodeLimiters[ip]; ok {
		return l
	}
	l := rate.NewLimiter(rate.Every(codeGuessWindow/codeGuessPerWindow), codeGuessPerWindow)
	s.CodeLimiters[ip] = l
	return l
}

// decrementConnCount atomically decrements the WebSocket connection counter for
// an IP and removes the map entry when the count reaches zero.
func (s *Server) decrementConnCount(ip string) {
	s.ConnCountsMu.Lock()
	defer s.ConnCountsMu.Unlock()
	if s.ConnCounts[ip] > 0 {
		s.ConnCounts[ip]--
	}
	if s.ConnCounts[ip] == 0 {
		delete(s.ConnCounts, ip)
	}
}

// wsConnLimitMiddleware is an HTTP handler middleware that enforces a per-IP
// maximum WebSocket connection limit. Connections that would exceed the limit
// receive HTTP 429 before the WebSocket upgrade occurs. The counter is
// decremented automatically when the underlying handler returns (covering both
// normal teardown and failed upgrades).
func (s *Server) wsConnLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := s.extractClientIP(r)

		s.ConnCountsMu.Lock()
		count := s.ConnCounts[ip]
		if count >= maxConnsPerIP {
			s.ConnCountsMu.Unlock()
			s.Logger.RateLimitExceeded(ip, "ws", count+1)
			s.Metrics.RecordRateLimit("ws")
			http.Error(w, "Too many connections from your IP. Please disconnect an existing session first.", http.StatusTooManyRequests)
			return
		}
		s.ConnCounts[ip] = count + 1
		s.ConnCountsMu.Unlock()

		// Decrement when the inner handler exits — this covers the normal WS
		// session lifecycle as well as any upgrade failures.
		defer s.decrementConnCount(ip)

		next.ServeHTTP(w, r)
	})
}

// cleanupRateLimiters removes stale entries from ConnCounts and CodeLimiters
// to prevent unbounded map growth. Called periodically by startCleanupRoutine.
func (s *Server) cleanupRateLimiters() {
	// ConnCounts: remove any zero entries that weren't cleaned up on disconnect.
	s.ConnCountsMu.Lock()
	for ip, count := range s.ConnCounts {
		if count == 0 {
			delete(s.ConnCounts, ip)
		}
	}
	s.ConnCountsMu.Unlock()

	// CodeLimiters: remove limiters whose buckets are fully replenished —
	// they carry no useful state and will be recreated on demand.
	s.CodeLimitersMu.Lock()
	for ip, l := range s.CodeLimiters {
		if l.Tokens() >= float64(codeGuessPerWindow) {
			delete(s.CodeLimiters, ip)
		}
	}
	s.CodeLimitersMu.Unlock()
}
