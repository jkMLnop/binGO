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

  // Poll the leaderboard API until the winner's entry appears (server records the
  // win asynchronously after game_ended, so allow a few seconds).
  let apiPlayerEntry = null;
  await expect.poll(async () => {
    apiPlayerEntry = await page.evaluate(async (u) => {
      const r = await fetch(`/api/leaderboard?limit=50&sort=wins`);
      const p = await r.json();
      return (p.data ?? []).find((e) => e.username === u) ?? null;
    }, username);
    return apiPlayerEntry;
  }, { timeout: 15000, intervals: [500, 1000, 2000] }).toBeTruthy();

  expect(apiPlayerEntry.wins).toBeGreaterThanOrEqual(1);

  // Verify the leaderboard panel renders and has at least one entry.
  // (The new winner may not appear in the visible top-N if other players have
  //  more wins; the API poll above already confirmed the win was recorded.)
  const leaderboardPanel = page.locator("article").filter({ has: page.getByRole("heading", { name: "Leaderboard" }) });
  await expect(leaderboardPanel.locator("li").first()).toBeVisible();
});
