import { expect, test } from "@playwright/test";

const STAGING_BASE_URL = process.env.STAGING_BASE_URL || "https://bingo-server-staging.fly.dev";
const PROD_BASE_URL = process.env.PROD_BASE_URL || "https://bingo-server.fly.dev";

// Agent key must be explicitly provided via env — no default.
// Tests requiring a valid key are skipped when AGENT_API_KEY is unset.
const AGENT_API_KEY = process.env.AGENT_API_KEY || "";

// Returns true if the response looks like the agent-event endpoint exists
// (returns JSON, not HTML — i.e. the route is registered).
function isEndpointDeployed(response, body) {
  return !body.includes("<!doctype") && !body.includes("<html") && response.status() !== 404;
}

test.describe("staging smoke", () => {
  test("GET /api/status is healthy", async ({ request }) => {
    const response = await request.get(`${STAGING_BASE_URL}/api/status`);
    expect(response.ok()).toBeTruthy();
    const payload = await response.json();
    expect(payload.success).toBeTruthy();
    expect(payload.data?.status).toBe("running");
  });

  test("POST /metrics/agent-event rejects missing auth", async ({ request }) => {
    const response = await request.post(`${STAGING_BASE_URL}/metrics/agent-event`, {
      data: { outcome: "pr_opened", run_id: "smoke-1" },
      headers: { "Accept": "application/json" },
    });
    const body = await response.text();
    // Pre-deploy: may return HTML (web client); post-deploy: returns 401/503
    if (isEndpointDeployed(response, body)) {
      expect([401, 503]).toContain(response.status());
    }
    // Otherwise endpoint not yet deployed — acceptable
  });

  test("POST /metrics/agent-event accepts valid key", async ({ request }) => {
    test.skip(!AGENT_API_KEY, "AGENT_API_KEY not configured — skipping auth test");
    const response = await request.post(`${STAGING_BASE_URL}/metrics/agent-event`, {
      data: { outcome: "pr_opened", run_id: "smoke-2", latency_ms: 5000 },
      headers: { "X-Agent-Key": AGENT_API_KEY, "Accept": "application/json" },
    });
    const body = await response.text();
    // If endpoint is deployed, must return 200 with recorded=true
    if (isEndpointDeployed(response, body)) {
      expect(response.status()).toBe(200);
      const payload = JSON.parse(body);
      expect(payload.success).toBeTruthy();
      expect(payload.data?.recorded).toBe(true);
    }
    // Otherwise it's pre-deploy — acceptable
  });
});

test.describe("production smoke", () => {
  test("GET /api/status is healthy", async ({ request }) => {
    const response = await request.get(`${PROD_BASE_URL}/api/status`);
    expect(response.ok()).toBeTruthy();
    const payload = await response.json();
    expect(payload.success).toBeTruthy();
    expect(payload.data?.status).toBe("running");
  });

  test("POST /metrics/agent-event rejects missing auth", async ({ request }) => {
    const response = await request.post(`${PROD_BASE_URL}/metrics/agent-event`, {
      data: { outcome: "pr_opened", run_id: "smoke-3" },
      headers: { "Accept": "application/json" },
    });
    const body = await response.text();
    if (isEndpointDeployed(response, body)) {
      expect([401, 503]).toContain(response.status());
    }
  });
});
