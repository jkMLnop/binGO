package server

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// InitTracer sets up the OTel TracerProvider with an OTLP HTTP exporter
// targeting Grafana Tempo (or any OTLP-compatible backend).
//
// Endpoint resolution order:
//  1. OTEL_EXPORTER_OTLP_ENDPOINT env var (e.g. http://localhost:4318 or https://cloud.example.com:4318)
//  2. Default: http://localhost:4318 (local Grafana Tempo in docker-compose)
//
// TLS: Using http:// → insecure (plain TCP). Using https:// → TLS.
// For Grafana Cloud Tempo, set the env var to the HTTPS OTLP endpoint.
//
// Returns a shutdown function that must be called before process exit to flush
// buffered spans. Pairs with the 5s shutdown context in bin.go.
func InitTracer(srv *Server) (func(context.Context), error) {
	ctx := context.Background()

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// No endpoint configured — skip tracing to avoid noisy connection errors.
		// Set OTEL_EXPORTER_OTLP_ENDPOINT to enable (e.g. Grafana Cloud Tempo).
		return func(context.Context) {}, nil
	}

	// The SDK's WithEndpoint option takes host:port only (no scheme).
	// We strip the scheme and apply WithInsecure for http://.
	insecure := strings.HasPrefix(endpoint, "http://")
	hostPort := strings.TrimPrefix(endpoint, "https://")
	hostPort = strings.TrimPrefix(hostPort, "http://")

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(hostPort),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP HTTP exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	// Set as the global provider so library code that uses otel.GetTracerProvider() picks it up
	otel.SetTracerProvider(tp)
	srv.SetTracer(tp.Tracer("bingo-server"))

	return func(ctx context.Context) {
		if err := tp.Shutdown(ctx); err != nil {
			// Best-effort flush — already shutting down, nothing to do
			_ = err
		}
	}, nil
}
