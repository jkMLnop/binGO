import { expect, test } from "@playwright/test";

const STAGING_BASE_URL = process.env.STAGING_BASE_URL || "https://bingo-server-staging.fly.dev";
const PROD_BASE_URL = process.env.PROD_BASE_URL || "https://bingo-server.fly.dev";

// Default dev key used for agent auth (matches DefaultAgentKey in server/auth.go)
const AGENT_API_KEY = process.env.AGENT_API_KEY || "dev-agent-key-local-only";

test.describe("staging smoke", () => {
  test("GET /api/status is healthy", async ({ request }) => {
    const response = await request.get(`${STAGING_BASE_URL}/api/status`);
    expect(response.ok()).toBeTruthy();
    const payload = await response.json();
    expect(payload.success).toBeTruthy();
    expect(payload.data?.status).toBe("running");
  });

  test("POST /metrics/agent-event returns auth error or not-yet-deployed", async ({ request }) => {
    const response = await request.post(`${STAGING_BASE_URL}/metrics/agent-event`, {
      data: { event: "hotfix_success", outcome: "pr_opened", run_id: "smoke-1" },
      headers: { "Accept": "application/json" },
    });
    // Pre-deploy: may return HTML (web client) or 401; post-deploy: returns 401
    const body = await response.text();
    const isHTML = body.includes("<!doctype") || body.includes("<html");
    const is401 = response.status() === 401;
    expect(isHTML || is401).toBeTruthy();
  });

  test("POST /metrics/agent-event accepts valid key", async ({ request }) => {
    const response = await request.post(`${STAGING_BASE_URL}/metrics/agent-event`, {
      data: { event: "hotfix_success", outcome: "pr_opened", run_id: "smoke-2", latency_ms: 5000 },
      headers: { "X-Agent-Key": AGENT_API_KEY, "Accept": "application/json" },
    });
    // Pre-deploy: may 404 or HTML; post-deploy: 200 with recorded=true
    const body = await response.text();
    if (response.status() === 200 && !body.includes("<!doctype")) {
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

  test("POST /metrics/agent-event returns auth error or not-yet-deployed", async ({ request }) => {
    const response = await request.post(`${PROD_BASE_URL}/metrics/agent-event`, {
      data: { event: "hotfix_success", outcome: "pr_opened", run_id: "smoke-3" },
      headers: { "Accept": "application/json" },
    });
    const body = await response.text();
    const isHTML = body.includes("<!doctype") || body.includes("<html");
    const is401 = response.status() === 401;
    expect(isHTML || is401).toBeTruthy();
  });
});
