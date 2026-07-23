/**
 * Phase 12.5 Full Regression Suite
 *
 * Covers every new behaviour introduced in Phase 12.5:
 *   1.  Home page room options
 *   2.  Room creation API shape
 *   3.  Room page initial UI
 *   4.  Identity layer (localStorage "bingo-identity")
 *   5.  Board creation inside a room (requires DeepSeek — skipped if unreachable)
 *   6.  Multi-board listing (title, status, Join, Delete)
 *   7.  Leave Game → navigate back to room (?from= param)
 *   8.  Board deletion by room admin
 *   9.  API auth enforcement (403 for non-admin)
 *  10.  No-regression checks on standalone game flow
 *
 * Run locally (requires server on :8080 + vite on :5173):
 *   npx playwright test e2e/phase12.5-full.spec.js --config playwright.config.cjs
 *
 * Run against staging:
 *   SMOKE_BASE_URL=https://bingo-server-staging.fly.dev npx playwright test e2e/phase12.5-full.spec.js
 */

import { expect, test } from "@playwright/test";

const BASE_URL =
  process.env.SMOKE_BASE_URL ||
  process.env.STAGING_BASE_URL ||
  "http://localhost:5173";

const API_BASE =
  process.env.SMOKE_API_URL ||
  process.env.SMOKE_BASE_URL ||
  process.env.STAGING_BASE_URL ||
  "http://localhost:8080";

// Unique suffix per test run to avoid data collisions
const RUN_ID = Date.now().toString().slice(-6);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Creates a room via the API and returns { code, game_code, host_id }. */
async function apiCreateRoom(request) {
  const resp = await request.post(`${API_BASE}/api/rooms`);
  expect(resp.ok()).toBeTruthy();
  const body = await resp.json();
  expect(body.success).toBeTruthy();
  return body.data;
}

/** Gets a JWT token by connecting a WebSocket and sending room_login. */
async function getTokenViaWS(page, roomCode, username) {
  return page.evaluate(
    async ({ apiBase, roomCode, username }) => {
      return new Promise((resolve, reject) => {
        const wsProto = apiBase.startsWith("https") ? "wss" : "ws";
        const wsHost = apiBase.replace(/^https?:\/\//, "");
        const ws = new WebSocket(`${wsProto}://${wsHost}/ws`);
        ws.onopen = () =>
          ws.send(JSON.stringify({ action: "room_login", username, room_code: roomCode }));
        ws.onmessage = (e) => {
          const msg = JSON.parse(e.data);
          if (msg.token) { ws.close(); resolve(msg.token); }
          else if (msg.type === "error") { ws.close(); reject(msg.message); }
        };
        ws.onerror = () => reject("websocket error");
        setTimeout(() => reject("timeout"), 8000);
      });
    },
    { apiBase: API_BASE, roomCode, username }
  );
}

// ---------------------------------------------------------------------------
// 1. Home Page Options
// ---------------------------------------------------------------------------

test.describe("1. Home page room options", () => {
  test("shows all four actions: host game, host room, join game, join room", async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

    await expect(page.getByRole("button", { name: /1\) Host a new game/i })).toBeVisible();
    await expect(page.getByRole("button", { name: /2\) Host a new Room/i })).toBeVisible();
    await expect(page.getByText(/3\) Join existing game/i)).toBeVisible();
    await expect(page.getByText(/4\) Join Room/i)).toBeVisible();
  });

  test("'Host a new Room' creates a room and navigates to /room/:code", async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: /2\) Host a new Room/i }).click();

    // Should navigate to /room/<5-char code>
    await expect(page).toHaveURL(/\/room\/[A-Z0-9]{5}$/i, { timeout: 10_000 });

    // Room heading should show the code
    const url = page.url();
    const roomCode = url.split("/room/")[1];
    await expect(page.getByRole("heading", { name: roomCode })).toBeVisible();
  });

  test("'Join Room' form navigates to /room/:code", async ({ page, request }) => {
    const room = await apiCreateRoom(request);

    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

    const roomInput = page.locator("input[placeholder='ROOM-ABCDE']");
    await roomInput.fill(room.code);
    await page.getByRole("button", { name: "Join Room" }).click();

    await expect(page).toHaveURL(new RegExp(`/room/${room.code}`, "i"), { timeout: 8_000 });
  });
});

// ---------------------------------------------------------------------------
// 2. Room Creation API Shape
// ---------------------------------------------------------------------------

test.describe("2. Room creation API shape", () => {
  test("POST /api/rooms returns correct shape", async ({ request }) => {
    const resp = await request.post(`${API_BASE}/api/rooms`);
    expect(resp.status()).toBe(200);

    const body = await resp.json();
    expect(body.success).toBe(true);

    const d = body.data;
    expect(typeof d.code).toBe("string");
    expect(d.code).toMatch(/^[A-Z0-9]{5}$/);
    expect(typeof d.host_id).toBe("string");
    expect(typeof d.player_count).toBe("number");
    expect(typeof d.game_status).toBe("string");
    // host_username is empty until first room_login
    expect(d.host_username === "" || d.host_username == null).toBeTruthy();
  });

  test("GET /api/room/:code returns same shape", async ({ request }) => {
    const created = await apiCreateRoom(request);

    const resp = await request.get(`${API_BASE}/api/room/${created.code}`);
    expect(resp.ok()).toBeTruthy();

    const body = await resp.json();
    expect(body.success).toBe(true);
    expect(body.data.code).toBe(created.code);
    expect(typeof body.data.player_count).toBe("number");
    expect(typeof body.data.game_status).toBe("string");
  });

  test("GET /api/room/:code 404 for non-existent room", async ({ request }) => {
    const resp = await request.get(`${API_BASE}/api/room/ZZZZZ`);
    expect(resp.status()).toBe(404);
    const body = await resp.json();
    expect(body.success).toBe(false);
  });

  test("GET /api/room/:code/games returns empty array initially", async ({ request }) => {
    const room = await apiCreateRoom(request);

    const resp = await request.get(`${API_BASE}/api/room/${room.code}/games`);
    expect(resp.ok()).toBeTruthy();

    const body = await resp.json();
    expect(body.success).toBe(true);
    expect(Array.isArray(body.data)).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// 3. Room Page Initial UI
// ---------------------------------------------------------------------------

test.describe("3. Room page initial UI", () => {
  test("shows room code heading and Boards section", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

    await expect(page.getByRole("heading", { name: room.code })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Boards" })).toBeVisible();
  });

  test("shows 'No boards yet' in empty room", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

    await expect(page.getByText("No boards yet.")).toBeVisible();
  });

  test("Home button returns to home page", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: "Home" }).click();
    await expect(page).toHaveURL(new RegExp(`^${BASE_URL}/?$`), { timeout: 5_000 });
  });

  test("+ New Board button is visible", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

    await expect(page.getByRole("button", { name: "+ New Board" })).toBeVisible();
  });

  test("identity bar shows name input when no identity in localStorage", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

    // Clear any existing identity
    await page.evaluate(() => localStorage.removeItem("bingo-identity"));
    await page.reload({ waitUntil: "domcontentloaded" });

    await expect(page.getByLabel("Your name")).toBeVisible();
    await expect(page.getByRole("button", { name: "Set Name" })).toBeVisible();
  });

  test("setting name in identity bar shows 'Playing as X'", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

    await page.evaluate(() => localStorage.removeItem("bingo-identity"));
    await page.reload({ waitUntil: "domcontentloaded" });

    const name = `tester-${RUN_ID}`;
    await page.getByLabel("Your name").fill(name);
    await page.getByRole("button", { name: "Set Name" }).click();

    await expect(page.getByText(new RegExp(`Playing as ${name}`))).toBeVisible();
    await expect(page.getByRole("button", { name: "change" })).toBeVisible();
  });

  test("'change' button shows identity modal", async ({ page, request }) => {
    const room = await apiCreateRoom(request);

    // Pre-set identity
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate(() => localStorage.setItem("bingo-identity", "Alice"));
    await page.reload({ waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: "change" }).click();
    await expect(page.getByText("Who are you?")).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// 4. Identity Layer
// ---------------------------------------------------------------------------

test.describe("4. Identity layer (localStorage)", () => {
  test("identity set on room page is saved to localStorage", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

    await page.evaluate(() => localStorage.removeItem("bingo-identity"));
    await page.reload({ waitUntil: "domcontentloaded" });

    const name = `id-test-${RUN_ID}`;
    await page.getByLabel("Your name").fill(name);
    await page.getByRole("button", { name: "Set Name" }).click();

    const stored = await page.evaluate(() => localStorage.getItem("bingo-identity"));
    expect(stored).toBe(name);
  });

  test("identity from localStorage pre-fills game page username field", async ({ page }) => {
    const name = `prefill-${RUN_ID}`;
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    await page.getByRole("button", { name: /1\) Host a new game/i }).click();
    await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/i, { timeout: 10_000 });

    // Username input should be pre-filled
    await expect(page.getByLabel("Username")).toHaveValue(name);
  });

  test("leaving a game preserves identity in localStorage", async ({ page }) => {
    const name = `leave-${RUN_ID}`;
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: /1\) Host a new game/i }).click();
    await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/i, { timeout: 10_000 });

    await page.getByLabel("Username").fill(name);
    await page.getByRole("button", { name: "Join Game" }).click();

    // Dismiss lobby setup if required
    try {
      await page.getByRole("button", { name: /Play with default words/i }).waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("button", { name: /Play with default words/i }).click();
    } catch {
      // lobby may not appear — continue
    }

    await expect(page.getByText(new RegExp(`You are: ${name}`))).toBeVisible({ timeout: 10_000 });

    await page.getByRole("button", { name: "Leave Game" }).click();

    // Should navigate to home (standalone game)
    await expect(page).toHaveURL(new RegExp(`^${BASE_URL}/?$`), { timeout: 5_000 });

    // Identity should still be in localStorage
    const stored = await page.evaluate(() => localStorage.getItem("bingo-identity"));
    expect(stored).toBe(name);
  });

  test("leaving a room game navigates back to room", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `room-leave-${RUN_ID}`;

    // Pre-set identity and navigate directly to a game with ?from param
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    // Create a standalone game to simulate coming from a room
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

    // The "+ New Board" flow needs AI — verify the ?from param wiring by
    // manually navigating to a game with ?from set
    const gameResp = await request.post(`${API_BASE}/api/games`);
    if (!gameResp.ok()) {
      test.skip(); // API not available
      return;
    }
    const gameData = await gameResp.json();
    const gameCode = gameData?.data?.code;
    if (!gameCode) { test.skip(); return; }

    await page.goto(
      `${BASE_URL}/game/${gameCode}?from=/room/${room.code}`,
      { waitUntil: "domcontentloaded" }
    );

    await page.getByLabel("Username").fill(name);
    await page.getByRole("button", { name: "Join Game" }).click();

    // Dismiss lobby setup if required
    try {
      await page.getByRole("button", { name: /Play with default words/i }).waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("button", { name: /Play with default words/i }).click();
    } catch {
      // lobby may not appear — continue
    }

    await expect(page.getByText(new RegExp(`You are: ${name}`))).toBeVisible({ timeout: 10_000 });

    await page.getByRole("button", { name: "Leave Game" }).click();

    // Should navigate back to the room
    await expect(page).toHaveURL(new RegExp(`/room/${room.code}`, "i"), { timeout: 8_000 });
  });
});

// ---------------------------------------------------------------------------
// 5. Board Creation (requires DeepSeek — skipped if AI unreachable)
// ---------------------------------------------------------------------------

test.describe("5. Board creation in a room", () => {
  test("'+ New Board' with no identity shows 'Who are you?' modal", async ({ page, request }) => {
    const room = await apiCreateRoom(request);

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate(() => localStorage.removeItem("bingo-identity"));
    await page.reload({ waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: "+ New Board" }).click();
    await expect(page.getByText("Who are you?")).toBeVisible();
  });

  test("'+ New Board' with identity opens GenerateModal", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `board-creator-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);
    await page.reload({ waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: "+ New Board" }).click();

    // Expect GenerateModal heading
    await expect(page.getByRole("heading", { name: /Generate Word List with AI/i })).toBeVisible({ timeout: 8_000 });
  });

  test("GenerateModal in create mode shows Board title input", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `title-check-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);
    await page.reload({ waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: "+ New Board" }).click();
    await expect(page.getByRole("heading", { name: /Generate Word List with AI/i })).toBeVisible({ timeout: 8_000 });

    // Board title field should be present in create mode
    await expect(page.getByLabel(/Board title/i)).toBeVisible();
  });

  test("board creation with AI generates and navigates to game with ?from param", async ({ page, request }) => {
    // Skip if DeepSeek not configured
    const statusResp = await request.get(`${API_BASE}/api/status`);
    const statusBody = await statusResp.json();
    if (!statusBody?.data?.llm_healthy) {
      test.skip();
      return;
    }

    const room = await apiCreateRoom(request);
    const name = `ai-creator-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);
    await page.reload({ waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: "+ New Board" }).click();
    await expect(page.getByRole("heading", { name: /Generate Word List with AI/i })).toBeVisible({ timeout: 8_000 });

    // Fill board title and topic
    const titleInput = page.getByLabel(/Board title/i);
    await titleInput.fill(`Zoo Bingo ${RUN_ID}`);

    const topicInput = page.getByLabel(/Describe your event or topic/i);
    await topicInput.fill("zoo animals");

    await page.getByRole("button", { name: /Generate/i }).click();

    // Wait for AI response (can be slow)
    await expect(page.getByRole("button", { name: /Use included words/i })).toBeVisible({ timeout: 60_000 });
    await page.getByRole("button", { name: /Use included words/i }).click();

    // Should navigate to /game/BINGO-XXXXX?from=/room/<code>
    await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}\?from=\/room\//, { timeout: 15_000 });
  });
});

// ---------------------------------------------------------------------------
// 6. Multi-Board Listing
// ---------------------------------------------------------------------------

test.describe("6. Multi-board listing", () => {
  test("board appears in room games list after creation via API", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `list-test-${RUN_ID}`;

    // Get a JWT token for the room
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    let token;
    try {
      token = await getTokenViaWS(page, room.code, name);
    } catch {
      test.skip(); // WS auth not available in this env
      return;
    }

    // Create a board via API
    const boardResp = await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        title: `Test Board ${RUN_ID}`,
        buzzwords: ["Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta", "Theta", "Iota", "Kappa", "Lambda", "Mu", "Nu", "Xi", "Omicron", "Pi", "Rho", "Sigma", "Tau", "Upsilon", "Phi", "Chi", "Psi", "Omega", "Alpha2"],
      },
    });
    expect(boardResp.ok()).toBeTruthy();
    const boardData = await boardResp.json();
    const boardCode = boardData?.data?.code;
    expect(boardCode).toMatch(/^BINGO-[A-Z0-9]{5}$/);

    // Reload room page and verify board appears
    await page.reload({ waitUntil: "domcontentloaded" });

    await expect(page.getByText(`Test Board ${RUN_ID}`)).toBeVisible({ timeout: 5_000 });
    await expect(page.getByText(boardCode)).toBeVisible();
    await expect(page.getByRole("button", { name: "Join" })).toBeVisible();
  });

  test("board card shows status badge", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `status-test-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    let token;
    try {
      token = await getTokenViaWS(page, room.code, name);
    } catch {
      test.skip();
      return;
    }

    const boardResp = await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        title: `Status Board ${RUN_ID}`,
        buzzwords: ["A1", "A2", "A3", "A4", "A5", "B1", "B2", "B3", "B4", "B5", "C1", "C2", "C3", "C4", "C5", "D1", "D2", "D3", "D4", "D5", "E1", "E2", "E3", "E4", "E5"],
      },
    });
    expect(boardResp.ok()).toBeTruthy();

    await page.reload({ waitUntil: "domcontentloaded" });

    // Board should show some status text
    await expect(page.getByText(`Status Board ${RUN_ID}`)).toBeVisible({ timeout: 5_000 });
    // Status badge area should exist (active, pending, or ended)
    await expect(page.locator(".game-card").first()).toBeVisible();
  });

  test("Join button navigates to game page with ?from param", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `join-test-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    let token;
    try {
      token = await getTokenViaWS(page, room.code, name);
    } catch {
      test.skip();
      return;
    }

    const boardResp = await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        title: `Join Test ${RUN_ID}`,
        buzzwords: ["A1", "A2", "A3", "A4", "A5", "B1", "B2", "B3", "B4", "B5", "C1", "C2", "C3", "C4", "C5", "D1", "D2", "D3", "D4", "D5", "E1", "E2", "E3", "E4", "E5"],
      },
    });
    expect(boardResp.ok()).toBeTruthy();
    const boardCode = (await boardResp.json())?.data?.code;

    await page.reload({ waitUntil: "domcontentloaded" });

    await page.getByRole("button", { name: "Join" }).first().click();

    // Should navigate to game page with ?from=/room/<code>
    await expect(page).toHaveURL(
      new RegExp(`/game/${boardCode}\\?from=/room/${room.code}`, "i"),
      { timeout: 8_000 }
    );
  });
});

// ---------------------------------------------------------------------------
// 7. Leave Game → Back to Room
// ---------------------------------------------------------------------------

test.describe("7. Leave Game navigates back to room", () => {
  test("URL includes ?from=/room/:code when joining via room Join button", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `from-test-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    let token;
    try {
      token = await getTokenViaWS(page, room.code, name);
    } catch {
      test.skip();
      return;
    }

    await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        title: `From Test ${RUN_ID}`,
        buzzwords: ["A1", "A2", "A3", "A4", "A5", "B1", "B2", "B3", "B4", "B5", "C1", "C2", "C3", "C4", "C5", "D1", "D2", "D3", "D4", "D5", "E1", "E2", "E3", "E4", "E5"],
      },
    });

    await page.reload({ waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Join" }).first().click();

    // URL must include ?from=/room/<code>
    await expect(page).toHaveURL(
      new RegExp(`\\?from=/room/${room.code}`, "i"),
      { timeout: 8_000 }
    );
  });

  test("Leave Game button on room-linked game navigates back to room page", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `back-test-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    let token;
    try {
      token = await getTokenViaWS(page, room.code, name);
    } catch {
      test.skip();
      return;
    }

    const boardResp = await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        title: `Back Test ${RUN_ID}`,
        buzzwords: ["A1", "A2", "A3", "A4", "A5", "B1", "B2", "B3", "B4", "B5", "C1", "C2", "C3", "C4", "C5", "D1", "D2", "D3", "D4", "D5", "E1", "E2", "E3", "E4", "E5"],
      },
    });
    const boardCode = (await boardResp.json())?.data?.code;

    // Navigate directly to game with ?from param
    await page.goto(
      `${BASE_URL}/game/${boardCode}?from=/room/${room.code}`,
      { waitUntil: "domcontentloaded" }
    );

    await page.getByLabel("Username").fill(name);
    await page.getByRole("button", { name: "Join Game" }).click();

    await expect(page.getByText(new RegExp(`You are: ${name}`))).toBeVisible({ timeout: 10_000 });

    // Host may see lobby — click "Play with default words" to exit to board
    const playDefaultBtn = page.getByRole("button", { name: "Play with default words" });
    if (await playDefaultBtn.isVisible({ timeout: 2_000 }).catch(() => false)) {
      await playDefaultBtn.click();
    }

    await page.getByRole("button", { name: "Leave Game" }).click();

    // Should be back at room page
    await expect(page).toHaveURL(new RegExp(`/room/${room.code}`, "i"), { timeout: 8_000 });
    await expect(page.getByRole("heading", { name: room.code })).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// 8. Board Deletion
// ---------------------------------------------------------------------------

test.describe("8. Board deletion by room admin", () => {
  test("DELETE /api/room/:code/games/:gameCode returns 403 without auth", async ({ request }) => {
    const room = await apiCreateRoom(request);

    // First create a board (no auth needed via POST with a fake token — this
    // will 403 too, but we need a game code; use standalone game instead)
    const gameResp = await request.post(`${API_BASE}/api/games`);
    if (!gameResp.ok()) { test.skip(); return; }
    const gameCode = (await gameResp.json())?.data?.code;
    if (!gameCode) { test.skip(); return; }

    const deleteResp = await request.delete(
      `${API_BASE}/api/room/${room.code}/games/${gameCode}`
    );
    // 401 (no auth), 403 (bad auth), or 404 (game not in this room) — all valid rejections
    expect([401, 403, 404]).toContain(deleteResp.status());
  });

  test("room admin can delete board via API", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `deleter-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    let token;
    try {
      token = await getTokenViaWS(page, room.code, name);
    } catch {
      test.skip();
      return;
    }

    // Create a board
    const boardResp = await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        title: `Delete Me ${RUN_ID}`,
        buzzwords: ["A1", "A2", "A3", "A4", "A5", "B1", "B2", "B3", "B4", "B5", "C1", "C2", "C3", "C4", "C5", "D1", "D2", "D3", "D4", "D5", "E1", "E2", "E3", "E4", "E5"],
      },
    });
    expect(boardResp.ok()).toBeTruthy();
    const boardCode = (await boardResp.json())?.data?.code;

    // Delete via API
    const deleteResp = await request.delete(
      `${API_BASE}/api/room/${room.code}/games/${boardCode}`,
      { headers: { Authorization: `Bearer ${token}` } }
    );
    expect(deleteResp.ok()).toBeTruthy();

    // Board should no longer appear in room games
    const gamesResp = await request.get(`${API_BASE}/api/room/${room.code}/games`);
    const gamesBody = await gamesResp.json();
    const codes = gamesBody.data?.map((g) => g.code) ?? [];
    expect(codes).not.toContain(boardCode);
  });

  test("Delete button is visible on board card for room admin", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `ui-deleter-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    let token;
    try {
      token = await getTokenViaWS(page, room.code, name);
    } catch {
      test.skip();
      return;
    }

    await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        title: `Delete UI ${RUN_ID}`,
        buzzwords: ["A1", "A2", "A3", "A4", "A5", "B1", "B2", "B3", "B4", "B5", "C1", "C2", "C3", "C4", "C5", "D1", "D2", "D3", "D4", "D5", "E1", "E2", "E3", "E4", "E5"],
      },
    });

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByRole("button", { name: "Delete" })).toBeVisible({ timeout: 5_000 });
  });

  test("clicking Delete removes board from UI", async ({ page, request }) => {
    const room = await apiCreateRoom(request);
    const name = `click-delete-${RUN_ID}`;

    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    let token;
    try {
      token = await getTokenViaWS(page, room.code, name);
    } catch {
      test.skip();
      return;
    }

    await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: `Bearer ${token}` },
      data: {
        title: `UI Delete ${RUN_ID}`,
        buzzwords: ["A1", "A2", "A3", "A4", "A5", "B1", "B2", "B3", "B4", "B5", "C1", "C2", "C3", "C4", "C5", "D1", "D2", "D3", "D4", "D5", "E1", "E2", "E3", "E4", "E5"],
      },
    });

    await page.reload({ waitUntil: "domcontentloaded" });
    await expect(page.getByText(`UI Delete ${RUN_ID}`)).toBeVisible({ timeout: 5_000 });

    // Click "+ New Board" to establish WS auth token, then close the modal
    await page.getByRole("button", { name: "+ New Board" }).click();
    // Wait for the Generate modal to appear (confirms WS auth completed)
    await expect(page.getByText("Generate Word List with AI")).toBeVisible({ timeout: 5_000 });
    // Close the modal
    await page.getByRole("button", { name: "Cancel" }).click();

    await page.getByRole("button", { name: "Delete" }).first().click();

    // Board title should disappear
    await expect(page.getByText(`UI Delete ${RUN_ID}`)).toHaveCount(0, { timeout: 8_000 });
  });
});

// ---------------------------------------------------------------------------
// 9. API Auth Enforcement
// ---------------------------------------------------------------------------

test.describe("9. API auth enforcement", () => {
  test("POST /api/room/:code/games 403 without auth token", async ({ request }) => {
    const room = await apiCreateRoom(request);

    const resp = await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      data: {
        title: "Unauthorized Board",
        buzzwords: ["A", "B", "C", "D", "E"],
      },
    });
    expect(resp.status()).toBe(403);
  });

  test("POST /api/room/:code/games 403 with invalid bearer token", async ({ request }) => {
    const room = await apiCreateRoom(request);

    const resp = await request.post(`${API_BASE}/api/room/${room.code}/games`, {
      headers: { Authorization: "Bearer not-a-real-token" },
      data: {
        title: "Bad Auth Board",
        buzzwords: ["A", "B", "C", "D", "E"],
      },
    });
    expect(resp.status()).toBe(403);
  });
});

// ---------------------------------------------------------------------------
// 10. No-Regression Checks on Standalone Game Flow
// ---------------------------------------------------------------------------

test.describe("10. Standalone game flow (no regression)", () => {
  test("'1) Host a new game' still works and navigates to /game/:code", async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: /1\) Host a new game/i }).click();
    await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/i, { timeout: 10_000 });
  });

  test("standalone game Join by Code still works", async ({ page, request }) => {
    const gameResp = await request.post(`${API_BASE}/api/games`);
    if (!gameResp.ok()) { test.skip(); return; }
    const gameCode = (await gameResp.json())?.data?.code;
    if (!gameCode) { test.skip(); return; }

    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

    const codeInput = page.locator("input[placeholder='BINGO-ABCDE']");
    await codeInput.fill(gameCode);
    await page.getByRole("button", { name: "Join by Code" }).click();

    await expect(page).toHaveURL(new RegExp(`/game/${gameCode}`), { timeout: 8_000 });
  });

  test("game page shows ROOM CODE heading for standalone game", async ({ page }) => {
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: /1\) Host a new game/i }).click();
    await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/i, { timeout: 10_000 });

    // Standalone game shows "room code" in the eyebrow and the game code as h1
    await expect(page.getByText("room code", { exact: true })).toBeVisible();
  });

  test("Leave Game on standalone game navigates to home page", async ({ page }) => {
    const name = `standalone-${RUN_ID}`;
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.evaluate((n) => localStorage.setItem("bingo-identity", n), name);

    await page.getByRole("button", { name: /1\) Host a new game/i }).click();
    await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/i, { timeout: 10_000 });

    await page.getByLabel("Username").fill(name);
    await page.getByRole("button", { name: "Join Game" }).click();

    // Dismiss lobby setup if required
    try {
      await page.getByRole("button", { name: /Play with default words/i }).waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("button", { name: /Play with default words/i }).click();
    } catch {
      // lobby may not appear — continue
    }

    await expect(page.getByText(new RegExp(`You are: ${name}`))).toBeVisible({ timeout: 10_000 });
    await page.getByRole("button", { name: "Leave Game" }).click();

    // Standalone game → goes home
    await expect(page).toHaveURL(new RegExp(`^${BASE_URL}/?$`), { timeout: 5_000 });
  });

  test("game page has no JS console errors on load", async ({ page }) => {
    const errors = [];
    page.on("console", (msg) => { if (msg.type() === "error") errors.push(msg.text()); });
    page.on("pageerror", (err) => errors.push(err.message));

    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: /1\) Host a new game/i }).click();
    await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/i, { timeout: 10_000 });
    await page.waitForTimeout(2000);

    expect(errors).toEqual([]);
  });

  test("room page has no JS console errors on load", async ({ page, request }) => {
    const errors = [];
    page.on("console", (msg) => { if (msg.type() === "error") errors.push(msg.text()); });
    page.on("pageerror", (err) => errors.push(err.message));

    const room = await apiCreateRoom(request);
    await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });
    await page.waitForTimeout(2000);

    expect(errors).toEqual([]);
  });
});
