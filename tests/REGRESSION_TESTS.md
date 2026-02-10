# ngrok-Based Multiplayer - Manual Regression Tests

## Test Setup

**Prerequisites:**
- ngrok installed and authenticated (`ngrok config add-authtoken YOUR_TOKEN_HERE`)
- binGO-CLI built (`go build -o binGO-CLI`)
- Multiple test machines or terminal windows ready
- Fresh buzzwords.csv or known dataset

**Test Process:**
1. Start server: `./binGO-CLI -mode server -port 8080`
2. In another terminal, expose: `ngrok http 8080`
3. Copy the ngrok URL and game code displayed on server
4. Connect clients: `./binGO-CLI -mode client -server <ngrok-url> -code <GAME-CODE>`

---

## 1. Server Initialization & Code Generation

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 1.1 | Server starts on correct port | Run `./binGO-CLI -mode server -port 8080` | Server prints "Listening on :8080" and displays game code | [X] |
| 1.2 | Game code format correct | Observe server output | Code matches format `BINGO-XXXXX` (11 chars) | [X] |
| 1.3 | Game code is unique | Start two servers on different ports, compare codes | Each server has different code | [X] |
| 1.4 | Game code per server run | Note first game code, restart server | Server generates new code (each run gets unique code - codes not yet persisted to disk) | [X] |
| 1.5 | Host is set | Connect first client, observe logs | Server tracks first client as HostID (immutable) | [X] |

---

## 2. Remote Connection, Code Validity & Security

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 2.1 | ngrok tunnel created | Run `ngrok http 8080` while server running | Shows "Forwarding http://abc123xyz.ngrok-free.dev → http://localhost:8080" | [X] |
| 2.2 | Client connects via ngrok URL | Run `./binGO-CLI -mode client -server kohen-gumlike-kellan.ngrok-free.dev -code BINGO-EVTGD` | Client connects and displays bingo board | [X] |
| 2.3 | Invalid code rejected | Connect with wrong code | Connection rejected or error message shown | [X] |
| 2.4 | Code required for all connections | Try to connect without `-code` flag | Connection rejected (code required for all connections - no localhost/LAN auto-join) | [X] |
| 2.5 | Multiple ngrok clients can connect | Connect 2-3 clients simultaneously | All clients connect and join same game | [X] |
| 2.6 | Code case sensitivity | Try code with lowercase letters (e.g., `bingo-25z26`) | Code must be uppercase (e.g., `BINGO-25Z26`), lowercase rejected with "invalid game code" error | [X] |
| 2.7 | Duplicate login rejected | Player already in game, another client tries to join as same player (with or without token) | Second client rejected with error "Username already in use in this game" | [X] |

---

## 3. Multiplayer Gameplay

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 3.1 | Player list displays on connect | Client connects to active game | Client sees "Players in game: [player-1, player-2, ...]" immediately | [X] |
| 3.2 | New player appears in everyone's list | 2 clients connected, third joins | All three clients update to show new player in their lists | [X] |
| 3.3 | Player disconnect updates lists | Client 2 of 3 disconnects | Remaining 2 clients see updated list without Client 1 | [X] |
| 3.4 | Board state does not persist for client | Client disconnects and reconnects with same player name and game code | Client receives fresh board with new buzzwords, all marks cleared | [X] |
| 3.5 | One client achieves bingo | One client marks winning pattern (3 in a row) | All connected clients see win announcement, game ends | [X] |

---

## 4. Win Detection

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 4.1 | Horizontal win detected | Mark cells 1, 2, 3 (bottom row) | Winner sees celebration animation; other players see trophy message "🏆 Game Ended! Winner: <player>" | [X] |
| 4.2 | Vertical win detected | Mark cells 1, 4, 7 (left column) | [X] |
| 4.3 | Diagonal win detected | Mark cells 1, 5, 9 (main diagonal) | [X] |
| 4.4 | Reverse diagonal win detected | Mark cells 3, 5, 7 (anti-diagonal) | [X] |
| 4.5 | Non-winner still connected | Winner announces win, other clients remain in game | Non-winners see trophy message with winner name below their prompt, game_ended message displays | [X] |

---

## 5. Game Restart

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 5.1 | Host sees restart prompt | Game ends (someone wins), host observes message | Host sees: "Type 'restart' to start a new game or 'q' to quit" | [X] |
| 5.2 | Non-host sees waiting message | Game ends, non-host observes message | Non-host sees: "Waiting for host to restart..." | [X] |
| 5.3 | Non-host sees disconnect message | Game ends, host disconnects | Non-host sees: "⚠️  WARNING: ❌ Host has disconnected. Game cannot be restarted." displayed at top without board reset | [X] |
| 5.4 | Host types restart | Host types `restart` after game ends | Board resets with new buzzwords (all cells have new values, no previous marks), all clients receive game_restart message and display fresh board simultaneously | [X] |
| 5.5 | Non-host cannot restart | Non-host tries typing `restart` after game ends | Client shows "🔄 Requesting game restart..." then receives error "❌ only the host can restart the game" (game does not restart) | [X] |
| 5.6 | Game code persists across restart | Note code before game, type `restart`, check code | Same code still in use for next session | [X] |
| 5.7 | Multiple restarts work | Restart 2-3 times in sequence | Each restart resets board, loads new buzzwords, works seamlessly | [X] |

---

## 6. Host Disconnect & Reconnection - SIMPLIFIED BEHAVIOR

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 6.1 | Host can reconnect after disconnect | Host disconnects (Ctrl+C or network drop), then reconnect to same game code | Host reconnects as same player, retains host status (HostID is immutable) | [X] |
| 6.2 | Host remains host after disconnect | Host disconnects mid-game, non-host continues | Non-host still waits for host to restart (no host reassignment) | [X] |
| 6.3 | Host can restart after reconnect | Host reconnects post-game, types `restart` | Restart works normally | [X] |
| 6.4 | Host rejoins with same code | Host disconnects and reconnects with same game code | Host rejoins same game session, retains host status (immutable HostID), gets fresh board with new buzzwords | [X] |
| 6.5 | Non-host loses game access when host disconnects | Host disconnects after game ends, non-host marks cells then attempts win announcement | Non-host sees host disconnected message, can mark cells, receives error when announcing win: "❌ game has already ended with winner: player-X", prompt is restored | [X] |

---

## 7. Game Archiving - NEW FUNCTIONALITY (Server-Side)

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 7.1 | Game archived on end | Play until someone wins | Server stores game in archive (no client-visible change) | [X] |
| 7.2 | Archiving is logged | Play a game to completion and check server logs | Server logs "📋 Archived game <id> (code: <CODE>)" when game ends | [X] |
| 7.3 | Code still usable after archive | Game ends, restart happens, new game starts with same code | Code works for multiple sessions indefinitely | [X] |
| 7.4 | Archive doesn't affect gameplay | Win game, archive created, restart, play new game | No performance impact, no errors during restart | [X] |

---

## 8. Edge Cases & Robustness

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 8.1 | Client disconnect mid-game | Client disconnects while game active | Game continues for remaining players | [X] |
| 8.2 | All clients disconnect except one | Multiple clients disconnect, one remains | Server keeps game active for remaining client | [X] |
| 8.3 | Rapid mark input | Mark multiple cells quickly on one client | All marks sync without lag or loss | [X] |
| 8.4 | Mark same cell twice | Client marks cell 5, then marks cell 5 again | Cell remains marked (idempotent), error message shown: "Error: cell already marked: 5" | [X] |
| 8.5 | Invalid input handling | Type invalid input (e.g., 10, 'abc', special chars) | Input rejected with helpful error message, game continues | [X] |
| 8.6 | Help command works | Type `help` in client | Client displays available commands including 'restart' with description | [X] |
| 8.7 | Quit command works | Type `q` | Client exits cleanly | [X] |

---

## 9. Display & UX

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 9.1 | Board rendering clarity | Observe board display | Numbers 1-9 visible, buzzwords readable, marks clearly shown | [X] |
| 9.2 | Player list visible | Observe client display | Current player list and count shown at top/side | [X] |
| 9.3 | Win celebration visible | Someone wins | Celebration animation plays (dancing parrot ASCII art, "BINGO!" message) | [X] |
| 9.4 | Game state messages clear | After each major event (join, mark, win, restart) | Status messages are clear and informative | [X] |

---

## 10. Backwards Compatibility

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 10.1 | Standalone mode unaffected | Run `./binGO-CLI -mode standalone` | Standalone game works normally, no changes observed | [X] |
| 10.2 | Local LAN mode unaffected | Run client on local network (no ngrok) | Local network functionality works as before | [ ] |

---

## 11. Admin API Regression Tests

### Setup
```bash
# Start server with default dev admin key
./binGO-CLI -mode server -port 8080

# In another terminal, test endpoints
export ADMIN_KEY="dev-admin-key-local-only"
export BASE_URL="http://localhost:8080"
```

### Authentication Tests

| Test # | Scenario | Command | Expected Result | Status |
|--------|----------|---------|-----------------|--------|
| 11.1 | Missing admin key header | `curl -X GET $BASE_URL/admin/api/games` | Returns 401 Unauthorized with "missing X-Admin-Key header" | [X] |
| 11.2 | Invalid admin key | `curl -X GET $BASE_URL/admin/api/games -H "X-Admin-Key: wrong-key"` | Returns 403 Forbidden with "invalid X-Admin-Key" | [X] |
| 11.3 | Valid admin key | `curl -X GET $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY"` | Returns 200 OK with games list | [X] |
| 11.4 | Custom admin key via env | `ADMIN_API_KEY=my-custom-key ./binGO-CLI -mode server -port 8080` then test with custom key | Returns 200 with custom key, 403 with default key | [X] |

### POST /admin/api/games Tests

| Test # | Scenario | Command | Expected Result | Status |
|--------|----------|---------|-----------------|--------|
| 11.5 | Create game without body | `curl -X POST $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY"` | Returns 200 with game data (id, code, status, player_count) | [X] |
| 11.6 | Create game with player list | `curl -X POST $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY" -d '{"players":["alice","bob"]}'` | Returns 200 with game data, player_count=0 (players added via WebSocket only) | [X] |
| 11.7 | Game code format | Create game and check code | Code matches pattern `BINGO-[A-Z0-9]{5}` (11 chars total) | [X] |
| 11.8 | Unique codes | Create 5 games | All codes are unique | [X] |

### GET /admin/api/games Tests

| Test # | Scenario | Command | Expected Result | Status |
|--------|----------|---------|-----------------|--------|
| 11.9 | List games returns array | `curl -X GET $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY"` | Returns 200 with data.games array and data.count | [X] |
| 11.10 | Correct count value | Create 5 games then list | data.count equals 5 and games array length equals 5 | [X] |

### GET /admin/api/games/{id} Tests

| Test # | Scenario | Command | Expected Result | Status |
|--------|----------|---------|-----------------|--------|
| 11.11 | Get existing game | Create game, then `curl -X GET $BASE_URL/admin/api/games/game-1 -H "X-Admin-Key: $ADMIN_KEY"` | Returns 200 with all fields (id, code, host_id, status, player_count, created_at, players, is_active) | [X] |
| 11.12 | Get non-existent game | `curl -X GET $BASE_URL/admin/api/games/nonexistent -H "X-Admin-Key: $ADMIN_KEY"` | Returns 404 with "game nonexistent not found" | [X] |
| 11.13 | Player IDs in detail | Create game: `curl -X POST $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY"`, connect players via client, then get detail: `curl -X GET $BASE_URL/admin/api/games/{id} -H "X-Admin-Key: $ADMIN_KEY"` | Returns 200 with players array containing player IDs | [X] |

### DELETE /admin/api/games/{id} Tests

| Test # | Scenario | Command | Expected Result | Status |
|--------|----------|---------|-----------------|--------|
| 11.14 | Delete existing game | Create game: `curl -X POST $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY"`, note the returned `id`, then delete: `curl -X DELETE $BASE_URL/admin/api/games/{id} -H "X-Admin-Key: $ADMIN_KEY"` | Returns 200, is_active=false, status="ended" | [X] |
| 11.15 | Delete non-existent game | `curl -X DELETE $BASE_URL/admin/api/games/nonexistent -H "X-Admin-Key: $ADMIN_KEY"` | Returns 404 with "game nonexistent not found" | [X] |

### Integration with Gameplay Tests

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 11.16 | HTTP status codes & response structure | Test various scenarios | All responses use correct HTTP status (200/400/401/403/404) with success/error fields | [ ] |
| 11.17 | Admin API creates playable game | Use POST /admin/api/games to create game, then connect client with returned code | Client successfully joins game with correct code | [X] |
| 11.18 | Admin API tracks active players | Create game with admin API, connect 2 clients, get game detail | player_count increases from 0 to 2 | [X] |
| 11.19 | Admin API reflects game status | Create game, play until win, get game detail | status changes from "active" to "ended" | [X] |
| 11.20 | Delete game removes from play | Create game, delete via admin API, try to connect client | Client cannot join (game not found or access denied) | [X] |

### Load & Concurrency Tests

| Test # | Scenario | Command | Expected Result | Status |
|--------|----------|---------|-----------------|--------|
| 11.21 | Create 50 games rapidly | Loop 50x: `curl -X POST $BASE_URL/admin/api/games -H "X-Admin-Key: $ADMIN_KEY"` | All requests return 200, all games created successfully | [X] |
| 11.22 | Concurrent requests | 5 parallel processes each creating 10 games | All 50 games created, no conflicts or errors | [X] |
| 11.23 | Query performance | Create 100 games, GET /admin/api/games | Response time < 1 second, all games returned | [X] |