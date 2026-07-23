import { expect, test } from "@playwright/test";

/**
 * Phase 12.5: Dev Smoke Tests
 *
 * Lightweight smoke tests for local development. Assumes the server is running
 * on http://localhost:8080 (default `go run .` port).
 *
 * Run: npx playwright test dev-smoke.spec.js
 */

const BASE_URL = process.env.DEV_BASE_URL || "http://localhost:8080";

test("local API status", async ({ request }) => {
  const response = await request.get(`${BASE_URL}/api/status`);
  expect(response.ok()).toBeTruthy();

  const payload = await response.json();
  expect(payload.success).toBeTruthy();
  expect(payload.data?.status).toBe("running");
});

test("local home page loads and shows all options", async ({ page }) => {
  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

  await expect(page.getByRole("button", { name: /1\) Host a new game/i })).toBeVisible();
  await expect(page.getByRole("button", { name: /2\) Host a new Room/i })).toBeVisible();
  await expect(page.getByPlaceholder("ROOM-ABCDE")).toBeVisible();
  await expect(page.getByRole("button", { name: "Join Room" })).toBeVisible();
});

test("local room creation and page load", async ({ page, request }) => {
  const createResp = await request.post(`${BASE_URL}/api/rooms`);
  expect(createResp.ok() || createResp.status() === 200).toBeTruthy();
  const { data: room } = await createResp.json();

  await page.goto(`${BASE_URL}/room/${room.code}`, { waitUntil: "domcontentloaded" });

  await expect(page.getByRole("heading", { name: room.code })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Boards" })).toBeVisible();
  await expect(page.getByText("No boards yet.")).toBeVisible();
});

test("local game join flow still works", async ({ page }) => {
  await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });

  await page.getByRole("button", { name: /1\) Host a new game/i }).click();
  await expect(page).toHaveURL(/\/game\/BINGO-[A-Z0-9]{5}$/);

  // Should not show validation errors
  await page.waitForTimeout(1000);
  await expect(page.getByText("Unable to validate this room code")).toHaveCount(0);
});
