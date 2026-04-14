package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestServer creates a minimal Server for rate-limit tests.
func newTestServerRL(t *testing.T) *Server {
	t.Helper()
	ResetMetrics()
	buzzwords := [][]string{{"A", "B", "C"}, {"D", "E", "F"}, {"G", "H", "I"}}
	return NewServer(buzzwords, 3, 3, "0")
}

// --- extractClientIP ---

func TestExtractClientIP_SingleXFF(t *testing.T) {
	s := newTestServerRL(t)
	r := httptest.NewRequest("GET", "/ws", nil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := s.extractClientIP(r); got != "1.2.3.4" {
		t.Fatalf("expected 1.2.3.4, got %q", got)
	}
}

func TestExtractClientIP_MultiHopXFF(t *testing.T) {
	s := newTestServerRL(t)
	r := httptest.NewRequest("GET", "/ws", nil)
	// Proxy chain: real client → proxy1 → proxy2; leftmost = real client
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 172.16.0.1, 192.168.1.1")
	if got := s.extractClientIP(r); got != "10.0.0.1" {
		t.Fatalf("expected leftmost IP 10.0.0.1, got %q", got)
	}
}

func TestExtractClientIP_NoXFF_UsesRemoteAddr(t *testing.T) {
	s := newTestServerRL(t)
	r := httptest.NewRequest("GET", "/ws", nil)
	r.RemoteAddr = "203.0.113.5:54321"
	if got := s.extractClientIP(r); got != "203.0.113.5" {
		t.Fatalf("expected 203.0.113.5, got %q", got)
	}
}

// --- wsConnLimitMiddleware ---

func TestWsConnLimitMiddleware_AllowsUnderLimit(t *testing.T) {
	s := newTestServerRL(t)
	handler := s.wsConnLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < maxConnsPerIP; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/ws", nil)
		req.RemoteAddr = "10.0.0.1:1000"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("connection %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

func TestWsConnLimitMiddleware_Blocks6thConnection(t *testing.T) {
	s := newTestServerRL(t)

	// A handler that blocks until we signal it (simulates a live WS session).
	unblock := make(chan struct{})
	handler := s.wsConnLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		<-unblock // hold the connection open
	}))

	const ip = "10.0.0.2"
	done := make(chan struct{}, maxConnsPerIP)

	// Fire maxConnsPerIP goroutines that each hold a connection open.
	for i := 0; i < maxConnsPerIP; i++ {
		go func() {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/ws", nil)
			req.RemoteAddr = fmt.Sprintf("%s:%d", ip, 2000+i)
			handler.ServeHTTP(rec, req)
			done <- struct{}{}
		}()
	}

	// Give goroutines time to start and increment the counter.
	time.Sleep(20 * time.Millisecond)

	// The 6th connection should be rejected with 429.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.RemoteAddr = fmt.Sprintf("%s:3000", ip)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for 6th connection, got %d", rec.Code)
	}

	// Release the held connections and let goroutines finish.
	close(unblock)
	for i := 0; i < maxConnsPerIP; i++ {
		<-done
	}
}

func TestWsConnLimitMiddleware_DifferentIPsAreIndependent(t *testing.T) {
	s := newTestServerRL(t)
	handler := s.wsConnLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Fill up limit for ip A.
	for i := 0; i < maxConnsPerIP; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/ws", nil)
		req.RemoteAddr = fmt.Sprintf("10.0.0.3:%d", 4000+i)
		handler.ServeHTTP(rec, req)
	}

	// ip B should still be allowed.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.RemoteAddr = "10.0.0.4:5000"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("different IP should be unaffected; expected 200, got %d", rec.Code)
	}
}

func TestWsConnLimitMiddleware_CounterDecrementsAfterHandler(t *testing.T) {
	s := newTestServerRL(t)
	handler := s.wsConnLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const ip = "10.0.0.5"
	for i := 0; i < maxConnsPerIP; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/ws", nil)
		req.RemoteAddr = fmt.Sprintf("%s:%d", ip, 6000+i)
		handler.ServeHTTP(rec, req)
	}

	// All handlers have returned → counter should be back to 0.
	s.ConnCountsMu.Lock()
	count := s.ConnCounts[ip]
	s.ConnCountsMu.Unlock()
	if count != 0 {
		t.Fatalf("expected ConnCounts[%s]=0 after all handlers returned, got %d", ip, count)
	}

	// Next connection should be accepted again.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ws", nil)
	req.RemoteAddr = fmt.Sprintf("%s:7000", ip)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after counter reset, got %d", rec.Code)
	}
}

// --- getCodeLimiter / code-guess rate limiting ---

func TestGetCodeLimiter_AllowsUpToLimit(t *testing.T) {
	s := newTestServerRL(t)
	const ip = "10.0.0.6"

	for i := 0; i < codeGuessPerWindow; i++ {
		if !s.getCodeLimiter(ip).Allow() {
			t.Fatalf("attempt %d should be allowed, was rejected", i+1)
		}
	}
}

func TestGetCodeLimiter_BlocksAfterExhausted(t *testing.T) {
	s := newTestServerRL(t)
	const ip = "10.0.0.7"

	// Drain all tokens.
	for i := 0; i < codeGuessPerWindow; i++ {
		s.getCodeLimiter(ip).Allow()
	}

	// The next call should be blocked.
	if s.getCodeLimiter(ip).Allow() {
		t.Fatal("expected rate limiter to block after token exhaustion")
	}
}

func TestGetCodeLimiter_SameIPReturnsSameLimiter(t *testing.T) {
	s := newTestServerRL(t)
	const ip = "10.0.0.8"
	l1 := s.getCodeLimiter(ip)
	l2 := s.getCodeLimiter(ip)
	if l1 != l2 {
		t.Fatal("expected the same *rate.Limiter pointer for the same IP")
	}
}

// --- cleanupRateLimiters ---

func TestCleanupRateLimiters_RemovesFullyReplenishedLimiters(t *testing.T) {
	s := newTestServerRL(t)
	const ip = "10.0.0.9"

	// A freshly created limiter starts with Tokens() == burst (fully loaded),
	// so cleanup should remove it immediately — it carries no useful state.
	_ = s.getCodeLimiter(ip)

	s.cleanupRateLimiters()

	s.CodeLimitersMu.Lock()
	_, exists := s.CodeLimiters[ip]
	s.CodeLimitersMu.Unlock()
	if exists {
		t.Fatal("cleanup should remove fully-replenished limiter entries")
	}
}

func TestCleanupRateLimiters_KeepsActiveCodeLimiters(t *testing.T) {
	s := newTestServerRL(t)
	const ip = "10.0.0.10"

	// Exhaust half the tokens so the limiter is still needed.
	l := s.getCodeLimiter(ip)
	for i := 0; i < codeGuessPerWindow/2; i++ {
		l.Allow()
	}

	s.cleanupRateLimiters()

	s.CodeLimitersMu.Lock()
	_, exists := s.CodeLimiters[ip]
	s.CodeLimitersMu.Unlock()
	if !exists {
		t.Fatal("cleanup should NOT remove a partially-depleted limiter")
	}
}

func TestCleanupRateLimiters_RemovesZeroConnCounts(t *testing.T) {
	s := newTestServerRL(t)
	const ip = "10.0.0.11"
	s.ConnCountsMu.Lock()
	s.ConnCounts[ip] = 0
	s.ConnCountsMu.Unlock()

	s.cleanupRateLimiters()

	s.ConnCountsMu.Lock()
	_, exists := s.ConnCounts[ip]
	s.ConnCountsMu.Unlock()
	if exists {
		t.Fatal("cleanup should remove zero-count ConnCounts entries")
	}
}

// --- RateLimitedTotal metric ---

func TestMetrics_RecordRateLimit(t *testing.T) {
	ResetMetrics()
	m := NewMetrics()
	// Should not panic for both valid labels.
	m.RecordRateLimit("ws")
	m.RecordRateLimit("code_guess")
}

// Ensure strings is used to silence import warnings in test file (it's used in
// the helper below and across multiple sub-tests via the production code).
var _ = strings.TrimSpace
