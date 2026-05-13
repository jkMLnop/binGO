# ngrok-Based Multiplayer - Manual Regression Tests

> **Note:** Many former manual tests are now automated in
> `tests/container_regression_test.go` and `tests/container_e2e_test.go`.
> Run them with: `go test -tags=container -timeout=10m ./tests -v`

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

## 7. Game Archiving - Database Persistence (Phase 8.5)

### Setup
```bash
docker-compose up -d --build
export ADMIN_KEY="dev-admin-key-local-only"
export BASE_URL="http://localhost:8080"

# Create a game via Admin API to get a game code
curl -X POST http://localhost:8080/admin/api/games \
  -H "X-Admin-Key: dev-admin-key-local-only"

# sample server response
{"success":true,"data":{"id":"game-2","code":"BINGO-3Q93C","host_id":"","status":"active","player_count":0,"created_at":1772969328}}
```

**To connect clients:**
```bash
# In separate terminal windows, connect clients to localhost:8080 with the game code
./binGO-CLI -mode client -server localhost:8080 -code BINGO-3Q93C
```

### 7D — Archive doesn't affect gameplay continuity

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 7.6 | Archive doesn't affect gameplay | Play 3 complete game cycles (win → restart → win → restart → win) | No errors in `docker-compose logs bingo-server`; join/mark/win all work normally across all three cycles | [X] |

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
docker-compose up -d --build
export ADMIN_KEY="dev-admin-key-local-only"
export BASE_URL="http://localhost:8080"
```

**Note:** Tests use Docker container on localhost, not the binary server. Game codes are obtained via Admin API and used to connect clients to `localhost:8080`.


### Integration with Gameplay Tests

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 11.17 | Admin API creates playable game | Use POST /admin/api/games to create game, then connect client with returned code | Client successfully joins game with correct code | [X] |
| 11.18 | Admin API tracks active players | Create game with admin API, connect 2 clients, get game detail | player_count increases from 0 to 2 | [X] |
| 11.19 | Admin API reflects game status | Create game, play until win, get game detail | status changes from "active" to "ended" | [X] |
| 11.20 | Delete game removes from play | Create game, delete via admin API, try to connect client | Client cannot join (game not found or access denied) | [X] |

---

## 12. Production Credentials Setup (docker-compose + .env)

Validates that the full stack reads credentials from `.env` correctly and that defaults are properly isolated.

### Setup

```bash
# From repo root
cp .env.example .env
```

### 12A — Grafana login uses credentials from .env

Edit `.env`:
```
GRAFANA_USER=testadmin
GRAFANA_PASSWORD=testpass123
```

Restart the stack (use `-v` to remove the Grafana volume so credentials are re-initialized):
```bash
docker-compose down -v && docker-compose up -d
```

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 12.1 | Grafana accepts .env credentials | Open `http://localhost:3000`, log in with `testadmin` / `testpass123` | Login succeeds, Grafana dashboard loads | [X] |
| 12.2 | Default `admin`/`admin` rejected | Try to log in with `admin` / `admin` | Login fails (wrong credentials) | [X] |

### 12B — Fallback to defaults when no .env file present

```bash
docker-compose down
mv .env .env.bak  # remove .env so no file is present
docker-compose up -d
```

| Test # | Scenario | Command | Expected Result | Status |
|--------|----------|---------|-----------------|--------|
| 12.3 | Grafana accessible with default creds | Open `http://localhost:3000`, log in with `admin` / `admin` | Login succeeds | [X] |

### 12C — Full multiplayer game with custom credentials

Validates that credential changes don't break the WebSocket game path.

```bash
# Restore .env with a custom admin key
mv .env.bak .env
# Set ADMIN_API_KEY=test-regression-key-12a in .env
docker-compose down && docker-compose up -d --build
```

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 12.4 | Create game via admin API with custom key | `curl -X POST http://localhost:8080/admin/api/games -H "X-Admin-Key: test-regression-key-12a"` | Returns `{"success":true,...}` with a valid game code — note the `code` for 12.5 | [X] |
| 12.5 | Players can join and play | Connect 2 clients: `./binGO-CLI -mode client -server localhost:8080 -code <code>` | Both clients join, boards render, cells can be marked | [X] |
| 12.6 | Game plays to win with custom creds active | One player marks a winning row | Win announcement broadcast to both clients; no auth errors in `docker-compose logs bingo-server` | [X] |

### Teardown

```bash
docker-compose down
mv .env.bak .env  # or delete if you want a clean state
```

---

## 13. OpenTelemetry Tracing (Grafana Tempo Smoke Test)

> **Automated coverage:** span instrumentation is exercised on every `go test ./...` run via the no-op tracer.
> This section only needs to be run once after the docker-compose stack is (re)built, or after changes to `server/tracing.go`, `tempo.yml`, or the Grafana datasource provisioning.

**Prerequisites:**
- `docker-compose up -d --build` (brings up bingo-server, Prometheus, Grafana, and Tempo)
- Grafana accessible at http://localhost:3000 (default user/password from `.env`)

### Setup

```bash
docker-compose up -d --build
# Verify Tempo is healthy
curl -s http://localhost:3200/ready   # should return "ready"
# Verify bingo-server is exporting spans to Tempo
docker-compose logs bingo-server | grep -i "otel\|trace\|tempo"
```

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 13.1 | Tempo service healthy | `curl -s http://localhost:3200/ready` | Returns `ready` | [ ] |
| 13.2 | bingo-server exports spans | `docker-compose logs bingo-server` | No OTLP exporter errors (connection refused / export failed) | [ ] |
| 13.3 | Tempo datasource provisioned in Grafana | Grafana → Connections → Data sources | A datasource named `Tempo` (uid `bingo-tempo`) is listed and shows "Data source connected" | [ ] |
| 13.4 | WebSocket session span visible | Connect a client (`./binGO-CLI -mode client -server localhost:8080 -code <code>`), then in Grafana → Explore → Tempo, search by Service Name `bingo-server` | At least one trace appears; root span is `bingo.ws.session` | [ ] |
| 13.5 | Full win trace chain visible | Play a game to a win, then search Tempo | Trace contains span chain: `bingo.ws.session` → `bingo.game.win` → `bingo.game.archive` | [ ] |
| 13.6 | Admin span visible | `curl -X POST http://localhost:8080/admin/api/games -H "X-Admin-Key: dev-admin-key-local-only"`, then search Tempo | A trace with root span `bingo.admin.createGame` is present | [ ] |
| 13.7 | trace_id in structured logs | `docker-compose logs bingo-server \| grep trace_id` | Log lines for win/archive events include a `trace_id` field matching the Tempo trace | [ ] |

### Teardown

```bash
docker-compose down
```

---

## 14. Phase 9 — Client Menu, Host Flow & Custom Buzzwords

### Setup

```bash
go build -o binGO-CLI .
./binGO-CLI -mode server -port 8080 -db ./bingo.db
```

### 14A — Main Menu

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 14.1 | Menu appears when no -code flag | `./binGO-CLI -mode client -server localhost:8080` | Prints `1) Host a new game` / `2) Join existing game` prompt | [ ] |
| 14.2 | No menu when -code flag provided | `./binGO-CLI -mode client -server localhost:8080 -code BINGO-XXXXX` | Skips menu, goes straight to username prompt | [ ] |
| 14.3 | Invalid menu selection rejected | At prompt enter `3` then Enter | Error `invalid selection "3" — enter 1 or 2`, process exits | [ ] |

### 14B — Host Flow

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 14.4 | Host creates game (no custom buzzwords) | Select `1`, press Enter to skip buzzword file | Prompted for username; connects; server prints ✓ Game created! Code: BINGO-XXXXX | [ ] |
| 14.5 | Game code shown after host creates game | Complete 14.4 | CLI prints `Share this code with players to join.` alongside the code | [ ] |
| 14.6 | Second client joins with printed code | Copy code from 14.4, run `./binGO-CLI -mode client -server localhost:8080 -code <code>` | Second client joins; host's player list updates | [ ] |
| 14.7 | Host flow with valid buzzword JSON | Create file `bw.json` with `[["foo"],["bar"],["baz"],["qux"],["quux"],["corge"],["grault"],["garply"],["waldo"]]`, select `1`, enter `bw.json` path | Prints `✓ Loaded 9 buzzword rows`; board cells contain words from that file | [ ] |
| 14.8 | Invalid JSON buzzword file rejected | Point to a non-JSON file; select `1` | Error `invalid buzzword JSON in ...`; process exits | [ ] |
| 14.9 | Empty buzzword JSON file rejected | Point to `[]`; select `1` | Error `buzzword file ... is empty`; process exits | [ ] |
| 14.10 | Missing buzzword file rejected | Point to nonexistent path; select `1` | Error `failed to read buzzword file ...`; process exits | [ ] |

### 14C — Join Flow

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 14.11 | Join prompts for code | Select `2` | Prints `Enter game code:` | [ ] |
| 14.12 | Empty code rejected | Select `2`, press Enter with no code | Error `game code cannot be empty`; process exits | [ ] |
| 14.13 | Invalid code rejected by server | Select `2`, enter `BINGO-ZZZZZ` (nonexistent game) | Server rejects; client shows connection error | [ ] |

---

## 15. Phase 9 — Buzzword Suggestion System

### Setup

Open three terminals:
```bash
# Terminal A — server
./binGO-CLI -mode server -port 8080 -db ./bingo.db

# Terminal B — host client
./binGO-CLI -mode client -server localhost:8080
# (select 1 to host, skip buzzwords, enter username "Host")
# note the printed game code

# Terminal C — second player
./binGO-CLI -mode client -server localhost:8080 -code <code>
# (enter username "Alice")
```

### 15A — Suggesting Phrases

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 15.1 | Player suggests a phrase | In Terminal C type `add_new_phrase synergy overload` | Both clients show suggestion panel `📝 Pending Buzzword Suggestions` containing `Alice  suggested: "synergy overload"` | [ ] |
| 15.2 | Empty suggestion rejected | Type `add_new_phrase ` (no phrase) | Error `suggestion phrase cannot be empty` displayed on Alice's terminal | [ ] |
| 15.3 | Duplicate suggestion rejected | Type `add_new_phrase synergy overload` again | Error `phrase "synergy overload" is already pending suggestion` | [ ] |
| 15.4 | Multiple suggestions accumulate | Alice: `add_new_phrase circle back`; then `add_new_phrase deep dive` | Suggestion panel lists all three pending phrases | [ ] |
| 15.5 | Phrase too long rejected | Type `add_new_phrase ` followed by 101 characters | Error `suggestion phrase too long (max 100 characters)` | [ ] |

### 15B — Host Approve / Reject

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 15.6 | Host approves suggestion | In Terminal B (host) type `approve synergy overload` | Suggestion removed from panel; both clients see `✅ Host approved buzzword: "synergy overload"` notification | [ ] |
| 15.7 | Host rejects suggestion | In Terminal B type `reject circle back` | Suggestion removed from panel; both clients see `❌ Host rejected suggestion: "circle back"` notification | [ ] |
| 15.8 | Non-host cannot approve | In Terminal C (Alice) type `approve deep dive` | Error `only the host can approve suggestions` on Alice's terminal | [ ] |
| 15.9 | Non-host cannot reject | In Terminal C type `reject deep dive` | Error `only the host can reject suggestions` on Alice's terminal | [ ] |
| 15.10 | Approve nonexistent phrase | Host: `approve totally made up` | Error `no pending suggestion matches "totally made up"` | [ ] |
| 15.11 | Approve is case-insensitive | After suggestion "Leverage" exists, host: `approve leverage` | Suggestion removed successfully | [ ] |
| 15.12 | Approved phrase persisted to DB | Approve a phrase, stop server, restart with same -db file, host creates new game with same username | Server logs show approved buzzword loaded into game from host profile (check server logs for "Loading host profile from DB") | [ ] |
| 15.13 | Approved phrases accumulate across approvals | Approve two separate phrases in sequence, check DB | Both phrases present in host profile (not overwritten by second approval) | [ ] |
| 15.14 | Suggestions cleared on restart | Alice suggests "pivot"; Host approves then restarts game | Suggestion panel is empty after restart; ⏳ no leftover suggestions from previous round | [ ] |

---

## 16. Phase 9 — Leaderboard & Player Stats

### Setup

```bash
./binGO-CLI -mode server -port 8080 -db ./bingo.db
# Play 2-3 complete games to populate wins_history
```

### 16A — In-game commands

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 16.1 | `leaderboard` command works | During active game type `leaderboard` | Prints `🏆 Leaderboard:` with ranked list `#1 <username>  N wins` | [ ] |
| 16.2 | `stats` command works | During active game type `stats` | Prints `📊 Stats for <username>:` with Wins, Games Played, Win Rate % | [ ] |
| 16.3 | Win rate calculated correctly | Play 2 wins out of 4 games, type `stats` | Win Rate shows 50.0% | [ ] |
| 16.4 | Commands work before and after win | Type `leaderboard` before win; then again after win | Works both times; post-win counts are updated | [ ] |

### 16B — HTTP endpoints

| Test # | Scenario | Command | Expected Result | Status |
|--------|----------|---------|-----------------|--------|
| 16.5 | Default leaderboard (sort by wins) | `curl "localhost:8080/api/leaderboard"` | Returns JSON array sorted descending by `wins`, includes `rank` | [ ] |
| 16.6 | Leaderboard sort by win_rate | `curl "localhost:8080/api/leaderboard?sort=win_rate"` | Returns array sorted descending by `win_rate` field | [ ] |
| 16.7 | Leaderboard sort by games_played | `curl "localhost:8080/api/leaderboard?sort=games_played"` | Returns array sorted descending by `games_played` field | [ ] |
| 16.8 | Player stats endpoint | `curl "localhost:8080/api/player/alice/stats"` | Returns `{"success":true,"data":{"username":"alice","wins":N,"games_played":M,"win_rate":X.XX}}` | [ ] |
| 16.9 | Stats for unknown player returns zeros | `curl "localhost:8080/api/player/nobody/stats"` | Returns success with all zero fields | [ ] |
| 16.10 | Stats/leaderboard without DB return 503 | Start server without `-db` flag, then curl both endpoints | Both return `{"success":false,"error":"... not available - database not enabled"}` with HTTP 503 | [ ] |

---

## 17. Phase 9.5 — Player Betting System

### Setup

Three terminals, same as Section 15 setup. Add a third player:

```bash
# Terminal D — third player
./binGO-CLI -mode client -server localhost:8080 -code <code>
# (enter username "Bob")
```

### 17A — Placing Bets

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 17.1 | Simple win bet | Alice: `bet: bob wins` | Both clients show `🎲 Active Bets` panel with `⏳ Alice  bob wins` entry | [ ] |
| 17.2 | Simple loses bet | Bob: `bet: alice loses` | Panel shows Bob's bet added below Alice's | [ ] |
| 17.3 | AND compound bet | Host: `bet: alice wins AND bob loses` | Panel shows Host's compound bet; both conditions visible | [ ] |
| 17.4 | Case-insensitive player names | Alice: `bet: BOB wins` | Accepted (Bob exists); bet appears in panel | [ ] |
| 17.5 | Unknown player rejected | Alice: `bet: charlie wins` | Error `player "charlie" not found in this game` | [ ] |
| 17.6 | Invalid outcome rejected | Alice: `bet: bob draws` | Error `invalid outcome "draws" — must be "wins" or "loses"` | [ ] |
| 17.7 | Malformed bet rejected | Alice: `bet: bob` | Error about invalid condition format | [ ] |
| 17.8 | Empty bet rejected | Alice: `bet: ` (empty) | Error `bet text cannot be empty` | [ ] |
| 17.9 | Duplicate active bet blocked | Alice places a bet, then immediately tries another | Error `you already have an active bet — wait for results before placing another` | [ ] |
| 17.10 | Bet after game ended rejected | After a win, Alice: `bet: bob wins` | Error `bets are closed — game has already ended` | [ ] |

### 17B — Bet Evaluation

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 17.11 | Winning bet resolves ✓ | Alice bets `bob wins`; Bob marks a winning row and announces win | Alice's bet shows `✓` status in bets panel; all clients see updated panel | [ ] |
| 17.12 | Losing bet resolves ✗ | Alice bets `bob wins`; Alice (not Bob) wins | Alice's bet shows `✗` status | [ ] |
| 17.13 | AND bet wins when all conditions met | Host bets `alice wins AND bob loses`; Alice wins | Host bet shows `✓` (both conditions true) | [ ] |
| 17.14 | AND bet loses when one condition fails | Host bets `alice wins AND bob loses`; Bob wins | Host bet shows `✗` (first condition false) | [ ] |
| 17.15 | All bets resolved on win | Multiple players have active bets; one player wins | All bets in panel update to ✓ or ✗ simultaneously in a single broadcast | [ ] |
| 17.16 | Bets panel broadcast to all players | Alice places bet, Bob is in game | Bob's terminal updates to show Alice's bet without any action from Bob | [ ] |

### 17C — Bets Cleared on Restart

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 17.17 | Bets cleared after restart | Place bets; game ends (win); host restarts | After restart, bets panel is empty on all clients | [ ] |
| 17.18 | New bets accepted after restart | After 17.17, place a new bet | Accepted without "duplicate bet" error; panel shows new bet | [ ] |

### 17D — Help Text

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 17.19 | Help shows betting command | Type `help` | Output includes `bet: <player> wins\|loses` with AND description | [ ] |
| 17.20 | Help shows suggestion commands | Type `help` | Output includes `add_new_phrase`, `approve`, `reject` commands | [ ] |
| 17.21 | Help shows leaderboard/stats | Type `help` | Output includes `leaderboard` and `stats` commands | [ ] |

---

## Section 15 — Web Client Embedded Assets (automated: `TestRegressionWebClientEmbedded`)

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 15.1 | Root serves SPA | `GET /` on running container | 200, Content-Type: text/html | ✅ automated |
| 15.2 | manifest.json is embedded | `GET /manifest.json` on running container | 200, valid JSON, `name = "binGO"` | ✅ automated |
| 15.3 | icon.svg is embedded | `GET /icon.svg` on running container | 200 | ✅ automated |
