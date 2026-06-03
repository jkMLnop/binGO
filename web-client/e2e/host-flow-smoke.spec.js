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
