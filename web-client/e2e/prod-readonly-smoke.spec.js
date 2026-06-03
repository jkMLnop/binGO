import { expect, test } from "@playwright/test";

const PROD_BASE_URL = process.env.PROD_BASE_URL || "https://bingo-server.fly.dev";

test("prod status endpoint is healthy", async ({ request }) => {
  const response = await request.get(`${PROD_BASE_URL}/api/status`);
  expect(response.ok()).toBeTruthy();

  const payload = await response.json();
  expect(payload.success).toBeTruthy();
  expect(payload.data?.status).toBe("running");
});

test("prod home page renders host/join controls", async ({ page }) => {
  await page.goto(PROD_BASE_URL, { waitUntil: "domcontentloaded" });

  await expect(page.getByRole("button", { name: "1) Host a new game" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Join by Code" })).toBeVisible();
});
