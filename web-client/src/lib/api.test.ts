import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createGame, fetchGameByCode, fetchLeaderboard, formatGenerationError, streamBuzzwords } from "./api";
import type { WordSet } from "./api";

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

  it("calls /api/games", async () => {
    mockFetch(200, gamePayload);
    await createGame();
    const calledUrl = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(calledUrl).toBe("/api/games");
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

// ─── streamBuzzwords ──────────────────────────────────────────────────────────

/** Build a ReadableStream that emits the given SSE lines, then closes. */
function makeSSEStream(lines: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  const text = lines.join("\n") + "\n";
  return new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode(text));
      controller.close();
    },
  });
}

function mockSSEFetch(status: number, sseLines: string[]): void {
  global.fetch = vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    body: makeSSEStream(sseLines),
    text: async () => JSON.stringify({ success: false, error: "err" }),
  } as unknown as Response);
}

describe("streamBuzzwords", () => {
  const validSets: WordSet[] = [
    { label: "Set A", words: Array.from({ length: 25 }, (_, i) => `word-a-${i}`) },
    { label: "Set B", words: Array.from({ length: 25 }, (_, i) => `word-b-${i}`) },
    { label: "Set C", words: Array.from({ length: 25 }, (_, i) => `word-c-${i}`) },
  ];

  it("fires onToken for each token event", async () => {
    const lines = [
      `data: ${JSON.stringify({ type: "token", content: "Hello" })}`,
      `data: ${JSON.stringify({ type: "token", content: " world" })}`,
      `data: ${JSON.stringify({ type: "done", sets: validSets })}`,
    ];
    mockSSEFetch(200, lines);

    const tokens: string[] = [];
    let doneSets: WordSet[] | null = null;
    await streamBuzzwords("AB3K7", "anime", undefined, [], "session-token",
      (c) => tokens.push(c),
      (s) => { doneSets = s; },
      () => {},
    );

    expect(tokens).toEqual(["Hello", " world"]);
    expect(doneSets).toHaveLength(3);
  });

  it("fires onDone with sets when done event arrives", async () => {
    const lines = [
      `data: ${JSON.stringify({ type: "done", sets: validSets })}`,
    ];
    mockSSEFetch(200, lines);

    let doneSets: WordSet[] | null = null;
    await streamBuzzwords("AB3K7", "topic", undefined, [], "session-token",
      () => {},
      (s) => { doneSets = s; },
      () => {},
    );

    expect(doneSets).not.toBeNull();
    expect(doneSets![0].label).toBe("Set A");
  });

  it("fires onError when server returns 503", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      body: null,
      text: async () => JSON.stringify({ success: false, error: "DeepSeek is not reachable" }),
    } as unknown as Response);

    let errorMsg = "";
    await streamBuzzwords("AB3K7", "topic", undefined, [], "session-token",
      () => {},
      () => {},
      (e) => { errorMsg = e; },
    );

    expect(errorMsg).toContain("DeepSeek");
  });

  it("fires onError when an error SSE event arrives", async () => {
    const lines = [
      `data: ${JSON.stringify({ type: "error", error: "LLM parse failed" })}`,
    ];
    mockSSEFetch(200, lines);

    let errorMsg = "";
    await streamBuzzwords("AB3K7", "topic", undefined, [], "session-token",
      () => {},
      () => {},
      (e) => { errorMsg = e; },
    );

    expect(errorMsg).toBe("LLM parse failed");
  });

  it("formats oversized generation errors", () => {
    expect(formatGenerationError('fixed word count requested: got 54, expected 50')).toBe(
      "Too many words: got 54, expected 50.",
    );
  });

  it("formats undersized generation errors", () => {
    expect(formatGenerationError('fixed word count requested: got 44, expected 50')).toBe(
      "Too few words: got 44, expected 50.",
    );
  });

  it("formats minimum word count errors", () => {
    expect(formatGenerationError('minimum word count not met: got 12, expected at least 30')).toBe(
      "Too few words: got 12, expected at least 30.",
    );
  });

  it("URL-encodes the room code", async () => {
    mockSSEFetch(200, [`data: ${JSON.stringify({ type: "done", sets: validSets })}`]);
    await streamBuzzwords("AB 3K7", "topic", undefined, [], "session-token", () => {}, () => {}, () => {});
    const calledUrl = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(calledUrl).toContain(encodeURIComponent("AB 3K7"));
  });

  it("sends Authorization bearer token header", async () => {
    mockSSEFetch(200, [`data: ${JSON.stringify({ type: "done", sets: validSets })}`]);
    await streamBuzzwords("AB3K7", "topic", undefined, [], "the-token", () => {}, () => {}, () => {});
    const calledInit = (global.fetch as ReturnType<typeof vi.fn>).mock.calls[0][1] as RequestInit;
    const headers = calledInit.headers as Record<string, string>;
    expect(headers.Authorization).toBe("Bearer the-token");
  });

  it("returns a clear error when auth token is missing", async () => {
    global.fetch = vi.fn();
    let errorMsg = "";
    await streamBuzzwords("AB3K7", "topic", undefined, [], "", () => {}, () => {}, (e) => {
      errorMsg = e;
    });
    expect(errorMsg).toContain("Missing session token");
    expect(global.fetch).not.toHaveBeenCalled();
  });

  it("skips malformed SSE lines without throwing", async () => {
    const lines = [
      "data: not-json",
      `data: ${JSON.stringify({ type: "done", sets: validSets })}`,
    ];
    mockSSEFetch(200, lines);

    let doneSets: WordSet[] | null = null;
    await expect(
      streamBuzzwords("AB3K7", "topic", undefined, [], "session-token",
        () => {},
        (s) => { doneSets = s; },
        () => {},
      )
    ).resolves.not.toThrow();
    expect(doneSets).not.toBeNull();
  });

  it("fires onError when stream closes without done/error event", async () => {
    const lines = [
      `data: ${JSON.stringify({ type: "token", content: "partial output" })}`,
    ];
    mockSSEFetch(200, lines);

    let errorMsg = "";
    await streamBuzzwords("AB3K7", "topic", undefined, [], "session-token",
      () => {},
      () => {},
      (e) => { errorMsg = e; },
    );

    expect(errorMsg).toContain("ended before completion");
  });
});
