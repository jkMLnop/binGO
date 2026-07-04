# Deployment Guide for binGO Server

This guide covers deploying the binGO server with persistent database to cloud platforms.

## Local Testing with Docker

### Prerequisites
- Docker & Docker Compose installed
- Go 1.25.3 (for building locally without Docker)

### Quick Start

1. **Build and run locally:**
   ```bash
   docker-compose up --build
   ```

2. **Server starts on:** `http://localhost:8080`

3. **Health check:**
   ```bash
   curl http://localhost:8080/api/status
   ```

4. **View logs:**
   ```bash
   docker-compose logs -f bingo-server
   ```

5. **Stop:**
   ```bash
   docker-compose down
   ```

### Persistent Data
- Database persists in Docker volume `bingo-data`
- Data survives container restarts
- To reset: `docker-compose down -v`

## Fly.io Deployment

### Prerequisites
- Fly.io CLI installed: https://fly.io/docs/getting-started/installing-flyctl/
- Fly.io account: https://fly.io/
- Domain pointing to your Fly.io app (optional, or use Fly.io's default domain)

### Deploy Steps

1. **Initialize Fly.io app** (first time only):
   ```bash
   flyctl launch
   ```
   - App name: `bingo-server` (or your choice)
   - Region: `sjc` (or your preferred region)
   - Use the existing `fly.toml` configuration

2. **Create persistent volume** for SQLite:
   ```bash
   flyctl volumes create bingo_data --region sjc
   ```

3. **Deploy:**
   ```bash
   flyctl deploy
   ```

4. **Get your app URL:**
   ```bash
   flyctl info
   ```
   Output will show: `https://bingo-server.fly.dev`

5. **Health check:**
   ```bash
   curl https://bingo-server.fly.dev/api/status
   ```

### View Logs
   ```bash
   flyctl logs
   ```

### Scale Configuration
   ```bash
   # Set number of instances
   flyctl scale count 2

   # Set instance resources
   flyctl scale memory 512
   ```

### Update DNS
To use a custom domain like `yubetcha.com`:

1. **Add domain in Fly.io:**
   ```bash
   flyctl certs create yubetcha.com
   ```

2. **Get CNAME records from:**
   ```bash
   flyctl certs list
   ```

3. **Update your DNS provider to point to Fly.io's CNAME**

4. **Verify:**
   ```bash
   curl https://yubetcha.com/api/status
   ```

## Database Management

### View Database
The SQLite database is stored in the persistent volume at `/app/data/bingo.db`

To access it:
```bash
# SSH into the app
flyctl ssh console

# Access SQLite
sqlite3 /app/data/bingo.db

# Example queries
SELECT COUNT(*) as active_games FROM games WHERE status = 'active';
SELECT username, wins FROM (
  SELECT player_username as username, COUNT(*) as wins 
  FROM wins_history GROUP BY player_username 
  ORDER BY wins DESC LIMIT 10
) AS leaderboard;
```

### Backup Database
```bash
# Download database locally
flyctl ssh console -C "cat /app/data/bingo.db" > bingo-backup.db
```

### Cleanup Old Games
Games automatically expire after 4 days (configurable in code).

Manual cleanup (in SSH console):
```sql
DELETE FROM games WHERE expires_at < datetime('now');
DELETE FROM players WHERE game_id NOT IN (SELECT id FROM games);
```

## Monitoring

### Health Checks
- Fly.io automatically monitors `/api/status` endpoint
- App restarts if health check fails

### API Endpoints for Monitoring
- `GET /api/status` - Server health, active games, database status
- `GET /api/leaderboard?limit=10` - Top players
- `GET /api/game/:code` - Game info lookup

### Logs
```bash
flyctl logs --follow
```

## Performance Tips

1. **Regional proximity:** Choose Fly.io region closest to your players
2. **Database optimization:** SQLite is suitable for 1-2 instances. For 3+, migrate to PostgreSQL (Phase 10)
3. **Connection pooling:** Fly.io handles automatic connection limits (1000 hard limit set in fly.toml)
4. **Auto-scaling:** Currently set to minimum 1 machine. Can increase with `flyctl scale count N`

## Troubleshooting

### App won't start
```bash
flyctl logs
```
Check logs for database or build errors.

### Slow leaderboard queries
```bash
# SSH in and check indexes
sqlite3 /app/data/bingo.db ".indices"
```

### Database locked errors
Increase SQLite timeouts in `db/sqlite.go` or migrate to PostgreSQL for concurrent writes.

### High memory usage
Monitor with:
```bash
flyctl status
```
Consider reducing busy-wait polling or increasing machine resources.
