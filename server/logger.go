package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// Logger provides structured JSON logging for the bingo server
type Logger struct {
	logger *log.Logger
}

// LogEvent represents a structured log event
type LogEvent struct {
	Timestamp string      `json:"timestamp"`
	Level     string      `json:"level"`
	EventType string      `json:"event_type"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
}

// NewLogger creates a new structured logger
func NewLogger() *Logger {
	return &Logger{
		logger: log.New(os.Stdout, "", 0),
	}
}

// logEvent writes a structured log event as JSON
func (l *Logger) logEvent(level, eventType, message string, details interface{}) {
	event := LogEvent{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		EventType: eventType,
		Message:   message,
		Details:   details,
	}
	data, _ := json.Marshal(event)
	l.logger.Println(string(data))
}

// GameCreated logs when a game is created
func (l *Logger) GameCreated(gameID, code, hostID string, details map[string]interface{}) {
	d := map[string]interface{}{
		"game_id": gameID,
		"code":    code,
		"host_id": hostID,
	}
	if details != nil {
		for k, v := range details {
			d[k] = v
		}
	}
	l.logEvent("INFO", "game_created", "Game created", d)
}

// GameEnded logs when a game ends
func (l *Logger) GameEnded(gameID, code string, details map[string]interface{}) {
	d := map[string]interface{}{
		"game_id": gameID,
		"code":    code,
	}
	if details != nil {
		for k, v := range details {
			d[k] = v
		}
	}
	l.logEvent("INFO", "game_ended", "Game ended", d)
}

// GameArchived logs when a game is archived
func (l *Logger) GameArchived(gameID, code string, winner string) {
	l.logEvent("INFO", "game_archived", "Game archived", map[string]interface{}{
		"game_id": gameID,
		"code":    code,
		"winner":  winner,
	})
}

// GameRestarted logs when a game is restarted
func (l *Logger) GameRestarted(gameID, code string) {
	l.logEvent("INFO", "game_restarted", "Game restarted", map[string]interface{}{
		"game_id": gameID,
		"code":    code,
	})
}

// PlayerJoined logs when a player joins
func (l *Logger) PlayerJoined(gameID, playerID, username string) {
	l.logEvent("INFO", "player_joined", "Player joined", map[string]interface{}{
		"game_id":   gameID,
		"player_id": playerID,
		"username":  username,
		"timestamp": time.Now().Unix(),
	})
}

// PlayerDisconnected logs when a player disconnects
func (l *Logger) PlayerDisconnected(gameID, playerID, username string, details map[string]interface{}) {
	d := map[string]interface{}{
		"game_id":   gameID,
		"player_id": playerID,
		"username":  username,
	}
	if details != nil {
		for k, v := range details {
			d[k] = v
		}
	}
	l.logEvent("INFO", "player_disconnected", "Player disconnected", d)
}

// DatabaseQuery logs database query performance
func (l *Logger) DatabaseQuery(operation string, duration float64, success bool, details map[string]interface{}) {
	level := "INFO"
	if !success {
		level = "ERROR"
	}
	d := map[string]interface{}{
		"operation":   operation,
		"duration_ms": duration,
		"success":     success,
	}
	if details != nil {
		for k, v := range details {
			d[k] = v
		}
	}
	l.logEvent(level, "database_query", "Database operation", d)
}

// Error logs an error event
func (l *Logger) Error(eventType, message string, err error, details map[string]interface{}) {
	d := map[string]interface{}{}
	if err != nil {
		d["error"] = err.Error()
	}
	if details != nil {
		for k, v := range details {
			d[k] = v
		}
	}
	l.logEvent("ERROR", eventType, message, d)
}

// RateLimitExceeded logs a structured WARN event when a request is rejected by
// the rate limiter. attemptCount is the connection or attempt number that was
// blocked (informational; pass 0 when not applicable).
func (l *Logger) RateLimitExceeded(ip, endpoint string, attemptCount int) {
	d := map[string]interface{}{
		"ip":       ip,
		"endpoint": endpoint,
	}
	if attemptCount > 0 {
		d["attempt_count"] = attemptCount
	}
	l.logEvent("WARN", "rate_limit_exceeded", "Rate limit exceeded", d)
}

// mergeMaps merges extra into base (extra keys take precedence) and returns base.
// Both arguments may be nil.
func mergeMaps(base, extra map[string]interface{}) map[string]interface{} {
	if base == nil {
		base = map[string]interface{}{}
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

// SpanDetails extracts the active trace_id and span_id from ctx and returns them
// as a map suitable for merging into a Logger details argument. Returns an empty
// map when there is no active recording span (e.g. tests using the noop tracer).
func SpanDetails(ctx context.Context) map[string]interface{} {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return map[string]interface{}{}
	}
	sc := span.SpanContext()
	return map[string]interface{}{
		"trace_id": sc.TraceID().String(),
		"span_id":  sc.SpanID().String(),
	}
}
