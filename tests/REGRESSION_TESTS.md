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
| 1.5 | Original host is set | Connect first client, observe logs | Server tracks first client as OriginalHostID | [X] |

---

## 2. ngrok Tunnel & Remote Connection

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 2.1 | ngrok tunnel created | Run `ngrok http 8080` while server running | Shows "Forwarding http://abc123xyz.ngrok-free.dev → http://localhost:8080" | [X] |
| 2.2 | Client connects via ngrok URL | Run `./binGO-CLI -mode client -server kohen-gumlike-kellan.ngrok-free.dev -code BINGO-EVTGD` | Client connects and displays bingo board | [X] |
| 2.3 | Invalid code rejected | Connect with wrong code | Connection rejected or error message shown | [X] |
| 2.4 | Missing code required for remote | Try to connect without `-code` flag | Connection rejected (code required for ngrok) | [X] |
| 2.5 | Multiple ngrok clients can connect | Connect 2-3 clients simultaneously | All clients connect and join same game | [X] |

---

## 3. Multiplayer Gameplay

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 3.1 | Player list displays on connect | Client connects to active game | Client sees "Players in game: [player-1, player-2, ...]" immediately | [X] |
| 3.2 | New player appears in everyone's list | 2 clients connected, third joins | All three clients update to show new player in their lists | [X] |
| 3.3 | Player disconnect updates lists | Client 2 of 3 disconnects | Remaining 2 clients see updated list without Client 1 | [X] |
| 3.4 | Each client has independent board marks | Multiple clients mark different cells locally | Client 1's marks don't appear on Client 2's display (marks are local) | [X] |
| 3.5 | Board state does not persist for client | Client disconnects and reconnects with same player name and game code | Client receives fresh board with new buzzwords, all marks cleared | [X] |
| 3.6 | One client achieves bingo | One client marks winning pattern (3 in a row) | All connected clients see win announcement, game ends | [X] |

---

## 4. Win Detection (Existing Behavior)

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 4.1 | Horizontal win detected | Mark cells 1, 2, 3 (bottom row) | Client announces win, celebration animation plays on all clients | [X] |
| 4.2 | Vertical win detected | Mark cells 1, 4, 7 (left column) | Win detected and celebrated | [X] |
| 4.3 | Diagonal win detected | Mark cells 1, 5, 9 (main diagonal) | Win detected and celebrated | [X] |
| 4.4 | Reverse diagonal win detected | Mark cells 3, 5, 7 (anti-diagonal) | Win detected and celebrated | [X] |
| 4.5 | Non-winner still connected | Winner announces win, other clients remain in game | Non-winners see win announcement, game_ended message displays | [X] |

---

## 5. Game Restart - NEW FUNCTIONALITY

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 5.1 | Host sees restart prompt | Game ends (someone wins), host observes message | Host sees: "Type 'restart' to start a new game or 'q' to quit" | [X] |
| 5.2 | Non-host sees waiting message | Game ends, non-host observes message | Non-host sees: "Waiting for host to restart..." | [X] |
| 5.3 | Host types restart | Host types `restart` after game ends | Board resets with new buzzwords, all clients receive game_restart message and display fresh board simultaneously | [X] |
| 5.4 | New buzzwords on restart | Host types `restart`, observe new board | New buzzwords appear in cells, no previous marks visible | [X] |
| 5.5 | Non-host cannot restart | Non-host tries typing `restart` after game ends | Client shows "🔄 Requesting game restart..." then receives error "❌ only the original host can restart the game" (game does not restart) | [X] |
| 5.6 | Game code persists across restart | Note code before game, type `restart`, check code | Same code still in use for next session | [X] |
| 5.7 | Multiple restarts work | Restart 2-3 times in sequence | Each restart resets board, loads new buzzwords, works seamlessly | [X] |

---

## 6. Host Disconnect & Reconnection - NEW BEHAVIOR

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 6.1 | Host can reconnect after disconnect | Host disconnects (Ctrl+C or network drop), then reconnect to same local server | Host reconnects as same player, retains original host status | [X] |
| 6.2 | Host remains original after disconnect | Host disconnects mid-game, non-host continues | Non-host still waits for original host to restart | [X] |
| 6.3 | Host can restart after reconnect | Host reconnects post-game, types `restart` | Restart works normally | [X] |
| 6.4 | Host rejoins after game ends | Host disconnects during active game, game ends, host reconnects | Host joins a new game with a new code (old game becomes orphaned) | [X] |
| 6.5 | Non-host orphaned when host disconnects | Host disconnects after game ends, non-host tries to continue | Non-host receives persistent warning on all prompts, can continue marking cells but cannot restart game. Must quit and contact host for new code to play again | [X] |

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
| 8.2 | Reconnect mid-game | Client disconnects and reconnects with same code | Client receives fresh board with new buzzwords, no previous marks (board state not persisted) | [X] |
| 8.3 | All clients disconnect except one | Multiple clients disconnect, one remains | Server keeps game active for remaining client | [X] |
| 8.4 | Rapid mark input | Mark multiple cells quickly on one client | All marks sync without lag or loss | [X] |
| 8.5 | Mark same cell twice | Client marks cell 5, then marks cell 5 again | Cell remains marked (idempotent), error message shown: "Error: cell already marked: 5" | [X] |
| 8.6 | Invalid input handling | Type invalid input (e.g., 10, 'abc', special chars) | Input rejected with helpful error message, game continues | [X] |
| 8.7 | Help command works | Type `help` or similar | Client displays available commands including 'restart' | [X] |
| 8.8 | Quit command works | Type `q` | Client exits cleanly | [X] |

---

## 9. Code Validity & Security

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 9.1 | Code format validated | Try connecting with malformed code (e.g., 'BINGO-', 'INVALID') | Connection rejected | [X] |
| 9.2 | Code case sensitivity | Try code with lowercase letters (e.g., `bingo-25z26`) | Code must be uppercase (e.g., `BINGO-25Z26`), lowercase rejected with "invalid game code" error | [X] |
| 9.3 | Expired code behavior | Play game, archive it, verify code still works | Code remains valid for new sessions (never expires) | [X] |
| 9.4 | Original host only can restart | Multiple clients in same game, non-host attempts restart | Only first local client (original host) can trigger restart | [X] |

---

## 10. Display & UX

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 10.1 | Help text updated | Type `help` in client | Help text includes "restart" command with description | [X] |
| 10.2 | Board rendering clarity | Observe board display | Numbers 1-9 visible, buzzwords readable, marks clearly shown | [X] |
| 10.3 | Player list visible | Observe client display | Current player list and count shown at top/side | [X] |
| 10.4 | Win celebration visible | Someone wins | Celebration animation plays (dancing parrot ASCII art, "BINGO!" message) | [X] |
| 10.5 | Game state messages clear | After each major event (join, mark, win, restart) | Status messages are clear and informative | [X] |

---

## 11. Backwards Compatibility

| Test # | Scenario | Steps | Expected Result | Status |
|--------|----------|-------|-----------------|--------|
| 11.1 | Standalone mode unaffected | Run `./binGO-CLI -mode standalone` | Standalone game works normally, no changes observed | [X] |
| 11.2 | Local LAN mode unaffected | Run client on local network (no ngrok) | Local network functionality works as before | [X] |
| 11.3 | Old test suite passes | Run `go test ./...` | All 70 tests pass (8 new tests added, all existing tests still pass) | [X] |

---

## Summary Checklist

- [X] All 11 test categories completed
- [X] No critical failures found
- [X] New restart functionality works as expected
- [X] Game code works for same server session and across game restarts
- [X] Original host ownership maintained
- [X] All clients stay synchronized
- [X] ngrok tunnel setup instructions clear in README
- [X] Help text updated with new commands
- [X] Aware of code persistence limitation (Phase 7.5+ feature)
- [X] Ready for release

---

## Notes

**Test Environment Variables (Optional):**
```bash
# To test on different ports
export BINGO_PORT=8080

# To test with specific buzzwords file
export BINGO_BUZZWORDS=buzzwords.csv
```

**Debugging Tips:**
- Check server logs for game creation/archiving messages
- Use `ps aux | grep ngrok` to verify tunnel is active
- Monitor WebSocket traffic with browser DevTools if needed
- Verify code format in server output matches expected pattern

**Known Limitations (For Documentation):**
- **Game codes are not persisted to disk** - Each server run generates a new code. Codes work for the duration of that server session and across game restarts, but are lost on server restart. This is a Phase 7.5+ feature (persistent code storage) to enable reusable personal links like Zoom URLs.
- Archives are in-memory only (lost on server restart) - Metadata about completed games is not persisted
- No UI for viewing archived games
- No database persistence yet (Phase 8+ feature)
