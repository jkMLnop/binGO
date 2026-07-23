import { expect, test } from "@playwright/test";

const STAGING_BASE_URL = process.env.STAGING_BASE_URL || "https://bingo-server-staging.fly.dev";

test("staging API status endpoint is healthy", async ({ request }) => {
  const response = await request.get(`${STAGING_BASE_URL}/api/status`);
  expect(response.ok()).toBeTruthy();

  const payload = await response.json();

  expect(payload.success).toBeTruthy();
  expect(payload.data?.status).toBe("running");
});

test("host flow does not produce room validation error", async ({ page }) => {
  const badGameLookups = [];

  page.on("response", (response) => {
    const url = response.url();
    if (url.includes("/api/game/BINGO-") && response.status() === 404) {
      badGameLookups.push(url);
    }
  });

  await page.goto(STAGING_BASE_URL, { waitUntil: "domcontentloaded" });

  await expect(page.getByRole("button", { name: "1) Host a new game" })).toBeVisible();
  await page.getByRole("button", { name: "1) Host a new game" }).click();

  await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/);

  // Give the app a short window to finish route validation requests.
  await page.waitForTimeout(2000);

  await expect(page.getByText("Unable to validate this room code")).toHaveCount(0);
  await expect(page.getByText(/game code BINGO-[A-Z0-9]{5} not found/)).toHaveCount(0);

  expect(badGameLookups).toEqual([]);
});

// ─── Phase 12.5: Room Smoke Tests ────────────────────────────────────────────

test("staging home page shows create room and join room options", async ({ page }) => {
  await page.goto(STAGING_BASE_URL, { waitUntil: "domcontentloaded" });

  await expect(page.getByRole("button", { name: /2\\) Host a new Room/i })).toBeVisible();
  await expect(page.getByLabel("Room code")).toBeVisible();
  await expect(page.getByRole("button", { name: "Join Room" })).toBeVisible();
});

test("staging room creation and page load", async ({ page, request }) => {
  const createResp = await request.post(`${STAGING_BASE_URL}/api/rooms`);
  expect(createResp.ok() || createResp.status() === 200).toBeTruthy();
  const { data: room } = await createResp.json();

  await page.goto(`${STAGING_BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

  await expect(page.getByRole("heading", { name: room.code })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Boards" })).toBeVisible();
  await expect(page.getByText("No boards yet.")).toBeVisible();
});

test("staging room API returns valid structure", async ({ request }) => {
  const createResp = await request.post(`${STAGING_BASE_URL}/api/rooms`);
  expect(createResp.ok() || createResp.status() === 200).toBeTruthy();
  const { data: room } = await createResp.json();

  // Verify room info structure
  expect(room.code).toMatch(/^[A-Z0-9]{5}$/);
  expect(room).toHaveProperty("host_id");
  expect(room).toHaveProperty("host_username");
  expect(room).toHaveProperty("game_code");
  expect(room).toHaveProperty("player_count");

  // Verify room games endpoint
  const gamesResp = await request.get(`${STAGING_BASE_URL}/api/room/${room.code}/games`);
  expect(gamesResp.ok()).toBeTruthy();
  const gamesPayload = await gamesResp.json();
  expect(gamesPayload.success).toBeTruthy();
  expect(Array.isArray(gamesPayload.data)).toBeTruthy();
});
