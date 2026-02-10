# Admin API Documentation

The Admin API provides endpoints for testing and managing bingo games programmatically. All endpoints require authentication via the `X-Admin-Key` header.

## Authentication

All Admin API endpoints require the `X-Admin-Key` header:

```
X-Admin-Key: <admin-key>
```

### Admin Key Configuration

The admin key is configured via the `ADMIN_API_KEY` environment variable:

```bash
# Production
export ADMIN_API_KEY="your-secure-production-key"

# Local Development (default)
# If ADMIN_API_KEY is not set, the server uses: "dev-admin-key-local-only"
```

**Security Note:** Never commit production admin keys to version control. Use environment variables or secure secret management systems.

## Endpoints

### 1. Create Game

**Endpoint:** `POST /admin/api/games`

Creates a new bingo game.

#### Request

```bash
curl -X POST http://localhost:8080/admin/api/games \
  -H "X-Admin-Key: dev-admin-key-local-only" \
  -H "Content-Type: application/json" \
  -d '{}'
```

Optional request body:

```json
{
  "players": ["player1", "player2"]
}
```

#### Response

```json
{
  "success": true,
  "data": {
    "id": "game-1",
    "code": "ABC123",
    "host_id": "",
    "status": "active",
    "player_count": 0,
    "created_at": 1707345600
  }
}
```

#### Status Codes

- **200 OK** - Game created successfully
- **400 Bad Request** - Invalid request format
- **401 Unauthorized** - Missing X-Admin-Key header
- **403 Forbidden** - Invalid X-Admin-Key

---

### 2. List All Games

**Endpoint:** `GET /admin/api/games`

Retrieves a list of all active games with their current state.

#### Request

```bash
curl -X GET http://localhost:8080/admin/api/games \
  -H "X-Admin-Key: dev-admin-key-local-only"
```

#### Response

```json
{
  "success": true,
  "data": {
    "games": [
      {
        "id": "game-1",
        "code": "ABC123",
        "host_id": "host-1",
        "status": "active",
        "player_count": 3,
        "created_at": 1707345600
      },
      {
        "id": "game-2",
        "code": "XYZ789",
        "host_id": "host-2",
        "status": "ended",
        "player_count": 2,
        "created_at": 1707345700
      }
    ],
    "count": 2
  }
}
```

#### Status Codes

- **200 OK** - Games retrieved successfully
- **401 Unauthorized** - Missing X-Admin-Key header
- **403 Forbidden** - Invalid X-Admin-Key

---

### 3. Get Game Details

**Endpoint:** `GET /admin/api/games/{id}`

Retrieves detailed state information for a specific game.

#### Request

```bash
curl -X GET http://localhost:8080/admin/api/games/game-1 \
  -H "X-Admin-Key: dev-admin-key-local-only"
```

#### Response

```json
{
  "success": true,
  "data": {
    "id": "game-1",
    "code": "ABC123",
    "host_id": "host-1",
    "status": "active",
    "player_count": 3,
    "created_at": 1707345600,
    "players": [
      {
        "id": "player-1"
      },
      {
        "id": "player-2"
      },
      {
        "id": "player-3"
      }
    ],
    "is_active": true
  }
}
```

#### Status Codes

- **200 OK** - Game details retrieved successfully
- **400 Bad Request** - Missing game ID
- **401 Unauthorized** - Missing X-Admin-Key header
- **403 Forbidden** - Invalid X-Admin-Key
- **404 Not Found** - Game not found

---

### 4. Delete Game (Force Close)

**Endpoint:** `DELETE /admin/api/games/{id}`

Force closes a game, marking it as inactive. This removes the game from active play.

#### Request

```bash
curl -X DELETE http://localhost:8080/admin/api/games/game-1 \
  -H "X-Admin-Key: dev-admin-key-local-only"
```

#### Response

```json
{
  "success": true,
  "data": {
    "message": "Game game-1 closed successfully",
    "game_id": "game-1"
  }
}
```

#### Status Codes

- **200 OK** - Game closed successfully
- **400 Bad Request** - Missing game ID
- **401 Unauthorized** - Missing X-Admin-Key header
- **403 Forbidden** - Invalid X-Admin-Key
- **404 Not Found** - Game not found

---

## Example Workflows

### Testing Game Creation and Listing

```bash
#!/bin/bash

ADMIN_KEY="dev-admin-key-local-only"
BASE_URL="http://localhost:8080"

# Create 5 games
echo "Creating 5 games..."
for i in {1..5}; do
  curl -X POST "$BASE_URL/admin/api/games" \
    -H "X-Admin-Key: $ADMIN_KEY" \
    -H "Content-Type: application/json" \
    -d '{}' | jq '.data.code'
done

# List all games
echo "Listing all games..."
curl -X GET "$BASE_URL/admin/api/games" \
  -H "X-Admin-Key: $ADMIN_KEY" | jq '.data | {count, games}'

# Get details of first game
echo "Getting game details for game-1..."
curl -X GET "$BASE_URL/admin/api/games/game-1" \
  -H "X-Admin-Key: $ADMIN_KEY" | jq '.data'

# Close a game
echo "Closing game-1..."
curl -X DELETE "$BASE_URL/admin/api/games/game-1" \
  -H "X-Admin-Key: $ADMIN_KEY" | jq '.data'
```

### Load Testing with Admin API

```bash
#!/bin/bash

ADMIN_KEY="dev-admin-key-local-only"
BASE_URL="http://localhost:8080"
NUM_GAMES=50

echo "Creating $NUM_GAMES games for load testing..."
start_time=$(date +%s%N | cut -b1-13)

for i in $(seq 1 $NUM_GAMES); do
  curl -s -X POST "$BASE_URL/admin/api/games" \
    -H "X-Admin-Key: $ADMIN_KEY" \
    -H "Content-Type: application/json" \
    -d '{}' > /dev/null &
done

wait
end_time=$(date +%s%N | cut -b1-13)
elapsed=$((end_time - start_time))

echo "Created $NUM_GAMES games in ${elapsed}ms"

# Check final count
final_count=$(curl -s -X GET "$BASE_URL/admin/api/games" \
  -H "X-Admin-Key: $ADMIN_KEY" | jq '.data.count')

echo "Total games now: $final_count"
```

---

## Error Responses

All error responses follow this format:

```json
{
  "success": false,
  "error": "error message"
}
```

### Common Errors

| Status | Error | Cause |
|--------|-------|-------|
| 401 | `missing X-Admin-Key header` | Request is missing the authentication header |
| 403 | `invalid X-Admin-Key` | Provided key doesn't match the configured admin key |
| 400 | `missing game id` | Game ID not provided in URL path |
| 404 | `game {id} not found` | Game with specified ID doesn't exist |

---

## Integration with Monitoring

The Admin API integrates with the existing monitoring stack:

- **Metrics:** Admin operations update the `games_created_total` and `games_active` metrics
- **Logging:** All admin operations are logged with structured JSON logging
- **Security Events:** Failed authentication attempts are logged with the client IP

Monitor these metrics in Grafana to track admin API usage and identify potential abuse patterns.

---

## Best Practices

1. **Rotate Admin Keys:** Change `ADMIN_API_KEY` regularly in production
2. **Use HTTPS:** Always use HTTPS in production to protect the admin key in transit
3. **Rate Limiting:** Consider adding rate limiting for admin endpoints in production
4. **Audit Logging:** Log all admin operations for compliance and debugging
5. **Separate Env Variables:** Use different admin keys for staging and production
6. **Test Locally First:** Always test workflows locally with the default dev key before deploying

---

## Related Documentation

- [Server Architecture](./DEPLOYMENT.md)
- [Monitoring Setup](./MONITORING_SETUP.md)
- [Roadmap](./ROADMAP.md)
