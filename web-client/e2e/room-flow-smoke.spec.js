import { expect, test } from "@playwright/test";

/**
 * Phase 12.5: Room Flow Smoke Tests
 *
 * Tests the multi-board room system: room page loads, boards list renders,
 * board CRUD operations, and identity persistence.
 *
 * Target: staging (default) or SMOKE_BASE_URL env var.
 */

const BASE_URL =
  process.env.SMOKE_BASE_URL ||
  process.env.STAGING_BASE_URL ||
  "https://bingo-server-staging.fly.dev";

// Generate a unique room code suffix to avoid collisions
const SUFFIX = Date.now().toString().slice(-6);

test("room API endpoint is healthy", async ({ request }) => {
  // Create a room first to verify the endpoint works
  const createResp = await request.post(`${BASE_URL}/api/rooms`);
  expect(createResp.ok() || createResp.status() === 200).toBeTruthy();

  const createPayload = await createResp.json();
  expect(createPayload.success).toBeTruthy();
  expect(createPayload.data?.code).toMatch(/^[A-Z0-9]{5}$/);
  expect(createPayload.data?.host_username).toBeFalsy(); // No host yet
});

test("room page loads and shows boards", async ({ page, request }) => {
  // Create a room
  const createResp = await request.post(`${BASE_URL}/api/rooms`);
  expect(createResp.ok() || createResp.status() === 200).toBeTruthy();
  const { data: room } = await createResp.json();

  // Navigate to the room page
  await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

  // Verify room page header
  await expect(page.getByRole("heading", { name: room.code })).toBeVisible();
  await expect(page.getByText(/room/i)).toBeVisible();

  // Verify boards section exists
  await expect(page.getByRole("heading", { name: "Boards" })).toBeVisible();

  // Empty state: no boards yet
  await expect(page.getByText("No boards yet.")).toBeVisible();

  // Home button should be visible
  await expect(page.getByRole("button", { name: "Home" })).toBeVisible();
});

test("home page shows create room and join room options", async ({ page }) => {
  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

  // Verify "Host a new Room" button exists
  await expect(page.getByRole("button", { name: /2\) Host a new Room/i })).toBeVisible();

  // Verify "Join Room" form exists
  await expect(page.getByPlaceholder("ROOM-ABCDE")).toBeVisible();
  await expect(page.getByRole("button", { name: "Join Room" })).toBeVisible();

  // Verify existing game flow still works
  await expect(page.getByRole("button", { name: /1\) Host a new game/i })).toBeVisible();
  await expect(page.getByLabel("Room code").first()).toBeVisible();
});

test("identity layer persists username in localStorage", async ({ page }) => {
  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

  // Navigate to a game page to set identity
  await page.getByRole("button", { name: /1\) Host a new game/i }).click();
  await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/);

  const username = `smoke-${SUFFIX}`;
  await page.getByLabel("Username").fill(username);
  await page.getByRole("button", { name: "Join Game" }).click();

  // Wait for the WS connection to complete (welcome message received) before
  // checking localStorage, since identity is persisted asynchronously.
  await expect(page.getByText(new RegExp(`You are: ${username}`))).toBeVisible({ timeout: 10_000 });

  // Verify identity saved to localStorage
  const stored = await page.evaluate(() => localStorage.getItem("bingo-identity"));
  expect(stored).toBe(username);

  // Host sees a lobby panel first — exit it to reach the board/toolbar view
  const playDefaultBtn = page.getByRole("button", { name: "Play with default words" });
  if (await playDefaultBtn.isVisible({ timeout: 2_000 }).catch(() => false)) {
    await playDefaultBtn.click();
  }

  // Leave the game
  await page.getByRole("button", { name: "Leave Game" }).click();

  // localStorage should still have the identity after leave
  const storedAfterLeave = await page.evaluate(() => localStorage.getItem("bingo-identity"));
  expect(storedAfterLeave).toBe(username);

  // Navigate to room page — username input should be pre-filled
  const createResp = await page.request.post(`${BASE_URL}/api/rooms`);
  const { data: room } = await createResp.json();
  await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

  // The identity should be visible (used for board creation/auth)
  // Verify "Who are you?" modal is NOT shown (identity already set)
  await expect(page.getByText("Who are you?")).toHaveCount(0);
});
