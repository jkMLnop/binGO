package server

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all Prometheus metrics for the bingo server
type Metrics struct {
	GameCount                prometheus.Gauge
	PlayerCount              prometheus.Gauge
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
		Registry: prometheus.DefaultRegisterer,
	}

	// Register all metrics
	prometheus.MustRegister(
		globalMetrics.GameCount,
		globalMetrics.PlayerCount,
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
	)

	return globalMetrics
}

// ResetMetrics resets the global metrics (for testing)
func ResetMetrics() {
	if globalMetrics != nil {
		// Unregister all metrics from Prometheus registry
		prometheus.Unregister(globalMetrics.GameCount)
		prometheus.Unregister(globalMetrics.PlayerCount)
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
	}
	globalMetrics = nil
}

// RecordError increments the bingo_errors_total counter for the given error type.
// Valid types: "auth", "game", "db", "ws", "input"
func (m *Metrics) RecordError(errorType string) {
	m.ErrorsTotal.WithLabelValues(errorType).Inc()
}
