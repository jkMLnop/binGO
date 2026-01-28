package server

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
	ErrorsTotal              prometheus.CounterVec
	GameArchived             prometheus.Counter
	GameRestarted            prometheus.Counter
}

// NewMetrics creates and registers all Prometheus metrics
func NewMetrics() *Metrics {
	return &Metrics{
		GameCount: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bingo_game_count",
			Help: "Total number of active games",
		}),
		PlayerCount: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "bingo_player_count",
			Help: "Total number of connected players",
		}),
		GameCreationDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "bingo_game_creation_duration_ms",
			Help:    "Time taken to create a game in milliseconds",
			Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500, 5000},
		}),
		DatabaseQueryDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "bingo_database_query_duration_ms",
			Help:    "Database query execution time in milliseconds",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500},
		}),
		GamesCreatedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bingo_games_created_total",
			Help: "Total number of games created",
		}),
		PlayersConnectedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bingo_players_connected_total",
			Help: "Total number of players who have connected",
		}),
		PlayersDisconnectedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bingo_players_disconnected_total",
			Help: "Total number of players who have disconnected",
		}),
		ErrorsTotal: *promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "bingo_errors_total",
			Help: "Total number of errors by type",
		}, []string{"error_type"}),
		GameArchived: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bingo_game_archived_total",
			Help: "Total number of games archived",
		}),
		GameRestarted: promauto.NewCounter(prometheus.CounterOpts{
			Name: "bingo_game_restarted_total",
			Help: "Total number of games restarted",
		}),
	}
}
