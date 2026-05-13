import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createGame, fetchGameByCode, fetchLeaderboard } from "./api";

// ─── helpers ─────────────────────────────────────────────────────────────────

function mockFetch(status: number, body: unknown): void {
  global.fetch = vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
  } as Response);
}

function mockFetchText(status: number, text: string): void {
  global.fetch = vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.reject(new SyntaxError("not json")),
    text: () => Promise.resolve(text),
  } as Response);
}

const gamePayload = {
  success: true,
  data: {
    id: "game-1",
    code: "BINGO-ABCDE",
    host_id: "alice",
    status: "active",
    player_count: 2,
    created_at: 1715000000,
  },
};

beforeEach(() => {
  vi.restoreAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

// ─── fetchGameByCode ──────────────────────────────────────────────────────────

describe("fetchGameByCode", () => {
  it("returns game data on success", async () => {
    mockFetch(200, gamePayload);
    const game = await fetchGameByCode("BINGO-ABCDE");
    expect(game.code).toBe("BINGO-ABCDE");
    expect(game.host_id).toBe("alice");
  });

  it("throws when success=false", async () => {
    mockFetch(404, { success: false, error: "game not found" });
    await expect(fetchGameByCode("BINGO-XXXXX")).rejects.toThrow("game not found");
  });

  it("throws when data is missing", async () => {
    mockFetch(200, { success: true });
    await expect(fetchGameByCode("BINGO-ABCDE")).rejects.toThrow();
  });

  it("URL-encodes the game code", async () => {
    mockFetch(200, gamePayload);
    await fetchGameByCode("BINGO-AB/CD");
    const calledUrl = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(calledUrl).toContain(encodeURIComponent("BINGO-AB/CD"));
  });
});

// ─── fetchLeaderboard ─────────────────────────────────────────────────────────

describe("fetchLeaderboard", () => {
  const leaderPayload = {
    success: true,
    data: [
      { username: "alice", wins: 5, rank: 1 },
      { username: "bob", wins: 3, rank: 2 },
    ],
  };

  it("returns entries on success", async () => {
    mockFetch(200, leaderPayload);
    const entries = await fetchLeaderboard();
    expect(entries).toHaveLength(2);
    expect(entries[0].username).toBe("alice");
  });

  it("passes limit param to URL", async () => {
    mockFetch(200, leaderPayload);
    await fetchLeaderboard(5);
    const calledUrl = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(calledUrl).toContain("limit=5");
  });

  it("throws on server error", async () => {
    mockFetch(500, { success: false, error: "internal error" });
    await expect(fetchLeaderboard()).rejects.toThrow("internal error");
  });

  it("throws when data is missing", async () => {
    mockFetch(200, { success: true });
    await expect(fetchLeaderboard()).rejects.toThrow();
  });
});

// ─── createGame ──────────────────────────────────────────────────────────────

describe("createGame", () => {
  it("returns game on success", async () => {
    mockFetch(200, gamePayload);
    const game = await createGame();
    expect(game.id).toBe("game-1");
  });

  it("sends X-Admin-Key header", async () => {
    mockFetch(200, gamePayload);
    await createGame();
    const calledInit = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit;
    expect((calledInit.headers as Record<string, string>)["X-Admin-Key"]).toBeTruthy();
  });

  it("throws on HTTP error with JSON payload", async () => {
    mockFetch(403, { success: false, error: "forbidden" });
    await expect(createGame()).rejects.toThrow("forbidden");
  });

  it("throws on non-JSON response", async () => {
    mockFetchText(500, "internal server error");
    await expect(createGame()).rejects.toThrow("non-JSON");
  });

  it("uses POST method", async () => {
    mockFetch(200, gamePayload);
    await createGame();
    const calledInit = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit;
    expect(calledInit.method).toBe("POST");
  });
});
