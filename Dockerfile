# Build stage
ARG GO_VERSION=1.25.3
FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev sqlite-dev

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary with SQLite support and version injection
ARG VERSION=dev
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo \
    -ldflags "-X main.version=${VERSION}" -o binGO .

# Final stage
FROM alpine:3.19

# Install SQLite runtime dependencies + CLI for debugging/testing
RUN apk add --no-cache ca-certificates sqlite-libs sqlite

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/binGO .

# Copy default buzzwords
COPY buzzwords.csv .

# Create directory for persistent data
RUN mkdir -p /app/data

# Expose WebSocket port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/status || exit 1

# Run server with database persistence to mounted volume
ENTRYPOINT ["./binGO", "-mode", "server", "-port", "8080", "-db", "/app/data/bingo.db"]
