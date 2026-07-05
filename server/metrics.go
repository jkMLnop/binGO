package server

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for the bingo server
type Metrics struct {
	GameCount                prometheus.Gauge
	PlayerCount              prometheus.Gauge
	RoomsActive              prometheus.Gauge // Phase 11.0: active rooms
	GameCreationDuration     prometheus.Histogram
	DatabaseQueryDuration    prometheus.Histogram
	GamesCreatedTotal        prometheus.Counter
	PlayersConnectedTotal    prometheus.Counter
	PlayersDisconnectedTotal prometheus.Counter

	GameArchived          prometheus.Counter
	GameRestarted         prometheus.Counter
	AdminAPIRequestsTotal prometheus.Counter
	AdminAPILatency       prometheus.Histogram
	ErrorsTotal           *prometheus.CounterVec // labeled by error_type: auth, game, db, ws, input
	RateLimitedTotal      *prometheus.CounterVec // labeled by endpoint: ws, code_guess
	HotfixTotal           *prometheus.CounterVec // Phase 15.2: labeled by outcome: pr_opened, tests_failed, no_fix_generated
	HotfixLatency         prometheus.Histogram   // Phase 15.2: time from CI failure to PR opened
	Registry              prometheus.Registerer  // Store the registry for Prometheus scraping
}

var globalMetrics *Metrics

// NewMetrics creates and registers all Prometheus metrics (singleton pattern for test safety)
func NewMetrics() *Metrics {
	if globalMetrics != nil {
		return globalMetrics
	}

	// Register metrics with the default Prometheus registry
	globalMetrics = &Metrics{
		GameCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "bingo_game_count",
			Help: "Total number of active games",
		}),
		PlayerCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "bingo_player_count",
			Help: "Total number of connected players",
		}),
		RoomsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "bingo_rooms_active",
			Help: "Total number of active rooms (Phase 11.0)",
		}),
		GameCreationDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "bingo_game_creation_duration_ms",
			Help:    "Time taken to create a game in milliseconds",
			Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500, 5000},
		}),
		DatabaseQueryDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "bingo_database_query_duration_ms",
			Help:    "Database query execution time in milliseconds",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500},
		}),
		GamesCreatedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bingo_games_created_total",
			Help: "Total number of games created",
		}),
		PlayersConnectedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bingo_players_connected_total",
			Help: "Total number of players who have connected",
		}),
		PlayersDisconnectedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bingo_players_disconnected_total",
			Help: "Total number of players who have disconnected",
		}),

		GameArchived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bingo_game_archived_total",
			Help: "Total number of games archived",
		}),
		GameRestarted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bingo_game_restarted_total",
			Help: "Total number of games restarted",
		}),
		AdminAPIRequestsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "bingo_admin_api_requests_total",
			Help: "Total number of admin API requests",
		}),
		AdminAPILatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "bingo_admin_api_latency_ms",
			Help:    "Admin API request latency in milliseconds",
			Buckets: []float64{10, 25, 50, 100, 250, 500, 1000},
		}),
		ErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bingo_errors_total",
			Help: "Total number of errors by type (auth, game, db, ws, input)",
		}, []string{"error_type"}),
		RateLimitedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bingo_rate_limited_total",
			Help: "Total number of requests rejected by rate limiting, by endpoint (ws, code_guess)",
		}, []string{"endpoint"}),
		HotfixTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bingo_agent_hotfix_total",
			Help: "Total number of hotfix agent activations by outcome (pr_opened, tests_failed, no_fix_generated)",
		}, []string{"outcome"}),
		HotfixLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "bingo_agent_hotfix_latency_ms",
			Help:    "Hotfix agent latency from CI failure to PR opened in milliseconds",
			Buckets: []float64{1000, 5000, 15000, 30000, 60000, 120000, 300000, 600000},
		}),
		Registry: prometheus.DefaultRegisterer,
	}

	// Register all metrics
	prometheus.MustRegister(
		globalMetrics.GameCount,
		globalMetrics.PlayerCount,
		globalMetrics.RoomsActive,
		globalMetrics.GameCreationDuration,
		globalMetrics.DatabaseQueryDuration,
		globalMetrics.GamesCreatedTotal,
		globalMetrics.PlayersConnectedTotal,
		globalMetrics.PlayersDisconnectedTotal,
		globalMetrics.GameArchived,
		globalMetrics.GameRestarted,
		globalMetrics.AdminAPIRequestsTotal,
		globalMetrics.AdminAPILatency,
		globalMetrics.ErrorsTotal,
		globalMetrics.RateLimitedTotal,
		globalMetrics.HotfixTotal,
		globalMetrics.HotfixLatency,
	)

	return globalMetrics
}

// ResetMetrics resets the global metrics (for testing)
func ResetMetrics() {
	if globalMetrics != nil {
		// Unregister all metrics from Prometheus registry
		prometheus.Unregister(globalMetrics.GameCount)
		prometheus.Unregister(globalMetrics.PlayerCount)
		prometheus.Unregister(globalMetrics.RoomsActive)
		prometheus.Unregister(globalMetrics.GameCreationDuration)
		prometheus.Unregister(globalMetrics.DatabaseQueryDuration)
		prometheus.Unregister(globalMetrics.GamesCreatedTotal)
		prometheus.Unregister(globalMetrics.PlayersConnectedTotal)
		prometheus.Unregister(globalMetrics.PlayersDisconnectedTotal)
		prometheus.Unregister(globalMetrics.GameArchived)
		prometheus.Unregister(globalMetrics.GameRestarted)
		prometheus.Unregister(globalMetrics.AdminAPIRequestsTotal)
		prometheus.Unregister(globalMetrics.AdminAPILatency)
		prometheus.Unregister(globalMetrics.ErrorsTotal)
		prometheus.Unregister(globalMetrics.RateLimitedTotal)
		prometheus.Unregister(globalMetrics.HotfixTotal)
		prometheus.Unregister(globalMetrics.HotfixLatency)
	}
	globalMetrics = nil
}

// RecordError increments the bingo_errors_total counter for the given error type.
// Valid types: "auth", "game", "db", "ws", "input", "llm"
func (m *Metrics) RecordError(errorType string) {
	m.ErrorsTotal.WithLabelValues(errorType).Inc()
}

// RecordRateLimit increments the bingo_rate_limited_total counter for the given endpoint.
// Valid endpoints: "ws", "code_guess"
func (m *Metrics) RecordRateLimit(endpoint string) {
	m.RateLimitedTotal.WithLabelValues(endpoint).Inc()
}
