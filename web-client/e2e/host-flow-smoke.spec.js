import { expect, test } from "@playwright/test";

const BASE_URL =
  process.env.SMOKE_BASE_URL ||
  process.env.STAGING_BASE_URL ||
  "https://bingo-server-staging.fly.dev";

test("status endpoint is healthy", async ({ request }) => {
  const response = await request.get(`${BASE_URL}/api/status`);
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

  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

  await expect(page.getByRole("button", { name: "1) Host a new game" })).toBeVisible();
  await page.getByRole("button", { name: "1) Host a new game" }).click();

  await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/);
  await page.waitForTimeout(2000);

  await expect(page.getByText("Unable to validate this room code")).toHaveCount(0);
  await expect(page.getByText(/game code BINGO-[A-Z0-9]{5} not found/)).toHaveCount(0);
  expect(badGameLookups).toEqual([]);
});

test("all-time leaderboard is refreshed after a room win", async ({ page }) => {
  const leaderboardRequests = [];

  page.on("request", (request) => {
    if (request.url().includes("/api/leaderboard?")) {
      leaderboardRequests.push(request.url());
    }
  });

  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "1) Host a new game" }).click();

  const username = `smoke-${Date.now().toString().slice(-6)}`;
  await page.getByLabel("Username").fill(username);
  await page.getByRole("button", { name: "Join Game" }).click();

  await expect(page.getByText(new RegExp(`You are: ${username}`))).toBeVisible();

  // Host must choose a word list before the board appears
  const defaultWordsBtn = page.getByRole("button", { name: /Play with default words/i });
  if (await defaultWordsBtn.isVisible()) {
    await defaultWordsBtn.click();
  }

  for (const cell of ["A1", "A2", "A3"]) {
    await page.getByRole("button", { name: new RegExp(`^${cell}\\b`) }).click();
  }

  await expect(page.getByText(new RegExp(`${username} won this round\\.`))).toBeVisible();
  const beforeWinRefresh = leaderboardRequests.length;

  // The app automatically refreshes the all-time leaderboard on game_ended.
  // Wait for the post-win refresh request to arrive.
  await expect.poll(() => leaderboardRequests.length).toBeGreaterThan(beforeWinRefresh);

  const apiEntries = await page.evaluate(async () => {
    const response = await fetch("/api/leaderboard?limit=10&sort=wins");
    const payload = await response.json();
    return payload.data ?? [];
  });

  const apiPlayerEntry = apiEntries.find((entry) => entry.username === username);

  expect(apiPlayerEntry).toBeTruthy();
  expect(apiPlayerEntry.wins).toBeGreaterThanOrEqual(1);

  const leaderboardItems = page.locator("article").filter({ has: page.getByRole("heading", { name: "Leaderboard" }) }).locator("li");
  const matchingItem = leaderboardItems.filter({ hasText: username }).first();
  await expect(matchingItem).toContainText(username);
  await expect(matchingItem).toContainText(`${apiPlayerEntry.wins} wins`);
});
