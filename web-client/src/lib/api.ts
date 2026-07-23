import type { ApiResponse, GameInfo, LeaderboardEntry } from "./types";

export type APIStatus = {
  status: string;
  port: string;
  active_games: number;
  db_enabled: boolean;
  llm_timeout_seconds?: number;
  room_code_ttl_seconds?: number;
};

export async function fetchGameByCode(code: string): Promise<GameInfo> {
  const response = await fetch(`/api/game/${encodeURIComponent(code)}`);
  const payload = (await response.json()) as ApiResponse<GameInfo>;

  if (!response.ok || !payload.success || !payload.data) {
    throw new Error(payload.error || `Unable to find game ${code}`);
  }

  return payload.data;
}

export async function fetchLeaderboard(limit = 10): Promise<LeaderboardEntry[]> {
  const response = await fetch(`/api/leaderboard?limit=${limit}&sort=wins`);
  const payload = (await response.json()) as ApiResponse<LeaderboardEntry[]>;

  if (!response.ok || !payload.success || !payload.data) {
    throw new Error(payload.error || "Unable to fetch leaderboard");
  }

  return payload.data;
}

export async function fetchAPIStatus(): Promise<APIStatus> {
  const response = await fetch("/api/status");
  const payload = (await response.json()) as ApiResponse<APIStatus>;

  if (!response.ok || !payload.success || !payload.data) {
    throw new Error(payload.error || "Unable to fetch API status");
  }

  return payload.data;
}

export async function createGame(): Promise<GameInfo> {
  const response = await fetch("/api/games", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({}),
  });

  const raw = await response.text();
  let payload: ApiResponse<GameInfo> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<GameInfo>;
    } catch {
      throw new Error("Create game failed: server returned non-JSON response");
    }
  }

  if (!response.ok || !payload?.success || !payload.data) {
    throw new Error(payload?.error || `Unable to create a new game (HTTP ${response.status})`);
  }

  return payload.data;
}

// ─── Phase 11.0: Room API functions ──────────────────────────────────────────

export type RoomInfo = {
  code: string;
  game_code: string;
  host_id: string;
  host_username: string;
  player_count: number;
  game_status: string;
  custom_board_used: boolean;
  linked_room_code?: string;
};

export async function createRoom(): Promise<RoomInfo> {
  const response = await fetch("/api/rooms", { method: "POST" });
  const raw = await response.text();
  let payload: ApiResponse<RoomInfo> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<RoomInfo>;
    } catch {
      throw new Error("Create room failed: non-JSON response");
    }
  }
  if (!response.ok || !payload?.success || !payload.data) {
    throw new Error(payload?.error || `Unable to create room (HTTP ${response.status})`);
  }
  return payload.data;
}

export async function fetchRoom(roomCode: string): Promise<RoomInfo> {
  const response = await fetch(`/api/room/${encodeURIComponent(roomCode)}`);
  const raw = await response.text();
  let payload: ApiResponse<RoomInfo> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<RoomInfo>;
    } catch {
      throw new Error("Fetch room failed: non-JSON response");
    }
  }
  if (!response.ok || !payload?.success || !payload.data) {
    throw new Error(payload?.error || `Room not found (HTTP ${response.status})`);
  }
  return payload.data;
}

export async function fetchRoomLeaderboard(roomCode: string, limit = 10): Promise<LeaderboardEntry[]> {
  const response = await fetch(`/api/room/${encodeURIComponent(roomCode)}/leaderboard?limit=${limit}`);
  const raw = await response.text();
  let payload: ApiResponse<LeaderboardEntry[]> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<LeaderboardEntry[]>;
    } catch {
      throw new Error("Fetch room leaderboard failed: non-JSON response");
    }
  }
  if (!response.ok || !payload?.success || !payload.data) {
    throw new Error(payload?.error || `Unable to fetch room leaderboard (HTTP ${response.status})`);
  }
  return payload.data;
}

export async function setRoomBuzzwords(roomCode: string, words: string[], uploadedBy: string): Promise<void> {
  const response = await fetch(`/api/room/${encodeURIComponent(roomCode)}/buzzwords`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ words, uploaded_by: uploadedBy }),
  });
  const raw = await response.text();
  let payload: ApiResponse<null> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<null>;
    } catch {
      throw new Error("Set buzzwords failed: non-JSON response");
    }
  }
  if (!response.ok || !payload?.success) {
    throw new Error(payload?.error || `Unable to set buzzwords (HTTP ${response.status})`);
  }
}

export async function getRoomBuzzwords(roomCode: string): Promise<string[]> {
  const response = await fetch(`/api/room/${encodeURIComponent(roomCode)}/buzzwords`);
  const raw = await response.text();
  let payload: ApiResponse<string[]> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<string[]>;
    } catch {
      throw new Error("Get buzzwords failed: non-JSON response");
    }
  }
  if (!response.ok || !payload?.success || !payload.data) {
    throw new Error(payload?.error || `Unable to get buzzwords (HTTP ${response.status})`);
  }
  return payload.data;
}

export async function setGameBuzzwords(gameCode: string, words: string[], authToken: string): Promise<void> {
  if (!authToken.trim()) {
    throw new Error("Missing session token. Reconnect to the game and try again.");
  }

  const response = await fetch(`/api/game/${encodeURIComponent(gameCode)}/buzzwords`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${authToken}`,
    },
    body: JSON.stringify({ words }),
  });

  const raw = await response.text();
  let payload: ApiResponse<null> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<null>;
    } catch {
      throw new Error("Set game buzzwords failed: non-JSON response");
    }
  }

  if (!response.ok || !payload?.success) {
    throw new Error(payload?.error || `Unable to set game buzzwords (HTTP ${response.status})`);
  }
}

// ─── Phase 12.2: AI Buzzword Generation (SSE streaming) ──────────────────────

/** One labelled set of buzzwords returned by the LLM. */
export type WordSet = {
  label: string;
  words: string[];
};

export type ExcludedWordFeedback = {
  word: string;
  reason: "" | "not_observable" | "too_generic" | "duplicate" | "not_relevant" | "too_hard" | "safety_accessibility" | "other";
  other_text?: string;
  duplicate_of?: string;
  specificity_note?: string;
  retrieval_url?: string;
};

/** A single SSE event from the generate-buzzwords endpoint. */
export type SSEEvent =
  | { type: "token"; content: string }
  | { type: "done"; sets: WordSet[] }
  | { type: "error"; error: string };

export function formatGenerationError(message: string): string {
  const trimmed = message.trim();

  // Match "fixed word count requested: got X, expected Y"
  const fixedMatch = trimmed.match(/fixed word count requested: got (\d+), expected (\d+)/i);
  if (fixedMatch) {
    const actual = Number(fixedMatch[1]);
    const expected = Number(fixedMatch[2]);
    if (Number.isFinite(expected) && Number.isFinite(actual)) {
      if (actual > expected) {
        return `Too many words: got ${actual}, expected ${expected}.`;
      }
      return `Too few words: got ${actual}, expected ${expected}.`;
    }
  }

  // Match "minimum word count not met: got X, expected at least Y"
  const minMatch = trimmed.match(/minimum word count not met: got (\d+), expected at least (\d+)/i);
  if (minMatch) {
    const actual = Number(minMatch[1]);
    const expected = Number(minMatch[2]);
    if (Number.isFinite(expected) && Number.isFinite(actual)) {
      return `Too few words: got ${actual}, expected at least ${expected}.`;
    }
  }

  return trimmed;
}

/**
 * Stream AI-generated buzzword sets for a room.
 *
 * Calls POST /api/room/:roomCode/generate-buzzwords with the given topic, optional
 * URL, and conversation history. Fires onToken for each streamed text chunk,
 * onDone when the LLM has finished and sets are validated, or onError on failure.
 */
export async function streamBuzzwords(
  roomCode: string,
  topic: string,
  url: string | undefined,
  messages: Array<{ role: string; content: string }>,
  authToken: string,
  onToken: (chunk: string) => void,
  onDone: (sets: WordSet[]) => void,
  onError: (err: string) => void,
): Promise<void> {
  if (!authToken.trim()) {
    onError("Missing session token. Reconnect to the room and try again.");
    return;
  }

  let response: Response;
  try {
    response = await fetch(`/api/room/${encodeURIComponent(roomCode)}/generate-buzzwords`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${authToken}`,
      },
      body: JSON.stringify({ topic, url, messages }),
    });
  } catch (err) {
    onError(err instanceof Error ? err.message : "Network error");
    return;
  }

  if (!response.ok) {
    const raw = await response.text();
    let errMsg = `HTTP ${response.status}`;
    try {
      const parsed = JSON.parse(raw) as ApiResponse<null>;
      if (parsed.error) errMsg = parsed.error;
    } catch {
      /* ignore */
    }
    onError(errMsg);
    return;
  }

  const reader = response.body?.getReader();
  if (!reader) {
    onError("No response body from server");
    return;
  }

  const decoder = new TextDecoder();
  let buffer = "";
  let finished = false;

  const handleEventData = (data: string) => {
    if (!data) return;
    try {
      const event = JSON.parse(data) as SSEEvent;
      if (event.type === "token") {
        if (!finished) onToken(event.content);
      } else if (event.type === "done") {
        if (!finished) {
          finished = true;
          onDone(event.sets);
        }
      } else if (event.type === "error") {
        if (!finished) {
          finished = true;
          onError(formatGenerationError(event.error));
        }
      }
    } catch {
      /* skip malformed SSE lines */
    }
  };

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });

      // Process all complete SSE lines in the buffer
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? ""; // hold the last (possibly incomplete) line

      for (const line of lines) {
        if (!line.startsWith("data: ")) continue;
        const data = line.slice(6).trim();
        handleEventData(data);
      }
    }
  } catch (err) {
    if (!finished) {
      finished = true;
      onError(err instanceof Error ? err.message : "Stream interrupted");
    }
    return;
  }

  // Handle a final frame that ended without a trailing newline.
  if (buffer.startsWith("data: ")) {
    handleEventData(buffer.slice(6).trim());
  }

  // Prevent the UI from hanging if the stream closes without terminal event.
  if (!finished) {
    onError("Generation stream ended before completion. Please try again.");
  }
}

/** Generation settings surfaced as UI controls in the Generate modal. */
export type GenerationOptions = {
  /** Guided prompt = current deterministic flow. Agentic retrieval = retrieval-prioritized guidance. */
  generationMode: "guided-prompt" | "agentic-retrieval";
  /** 0 = flexible length (server ensures at least 30), >0 = exact size (minimum 30). */
  fixedWordCount: number;
};

export const DEFAULT_GENERATION_OPTIONS: GenerationOptions = {
  generationMode: "guided-prompt",
  fixedWordCount: 0,
};

export async function streamGameBuzzwords(
  gameCode: string,
  topic: string,
  url: string | undefined,
  messages: Array<{ role: string; content: string }>,
  authToken: string,
  genOpts: GenerationOptions,
  onToken: (chunk: string) => void,
  onDone: (sets: WordSet[]) => void,
  onError: (err: string) => void,
): Promise<void> {
  if (!authToken.trim()) {
    onError("Missing session token. Reconnect to the game and try again.");
    return;
  }

  let response: Response;
  try {
    response = await fetch(`/api/game/${encodeURIComponent(gameCode)}/generate-buzzwords`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${authToken}`,
      },
      body: JSON.stringify({
        topic, url, messages,
        generation_mode: genOpts.generationMode,
        fixed_word_count: genOpts.fixedWordCount,
      }),
    });
  } catch (err) {
    onError(err instanceof Error ? err.message : "Network error");
    return;
  }

  if (!response.ok) {
    const raw = await response.text();
    let errMsg = `HTTP ${response.status}`;
    try {
      const parsed = JSON.parse(raw) as ApiResponse<null>;
      if (parsed.error) errMsg = parsed.error;
    } catch {
      /* ignore */
    }
    onError(errMsg);
    return;
  }

  const reader = response.body?.getReader();
  if (!reader) {
    onError("No response body from server");
    return;
  }

  const decoder = new TextDecoder();
  let buffer = "";
  let finished = false;

  const handleEventData = (data: string) => {
    if (!data) return;
    try {
      const event = JSON.parse(data) as SSEEvent;
      if (event.type === "token") {
        if (!finished) onToken(event.content);
      } else if (event.type === "done") {
        if (!finished) {
          finished = true;
          onDone(event.sets);
        }
      } else if (event.type === "error") {
        if (!finished) {
          finished = true;
          onError(event.error);
        }
      }
    } catch {
      /* skip malformed SSE lines */
    }
  };

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });

      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";

      for (const line of lines) {
        if (!line.startsWith("data: ")) continue;
        const data = line.slice(6).trim();
        handleEventData(data);
      }
    }
  } catch (err) {
    if (!finished) {
      finished = true;
      onError(err instanceof Error ? err.message : "Stream interrupted");
    }
    return;
  }

  if (buffer.startsWith("data: ")) {
    handleEventData(buffer.slice(6).trim());
  }

  if (!finished) {
    onError("Generation stream ended before completion. Please try again.");
  }
}

export async function submitGameBuzzwordFeedback(
  gameCode: string,
  authToken: string,
  payload: {
    topic: string;
    url?: string;
    generation_mode?: "guided-prompt" | "agentic-retrieval";
    set_label: string;
    total_words: number;
    included_words: string[];
    excluded: ExcludedWordFeedback[];
  },
): Promise<void> {
  if (!authToken.trim()) {
    throw new Error("Missing session token. Reconnect to the game and try again.");
  }

  const response = await fetch(`/api/game/${encodeURIComponent(gameCode)}/feedback`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${authToken}`,
    },
    body: JSON.stringify(payload),
  });

  const raw = await response.text();
  let parsed: ApiResponse<null> | null = null;
  if (raw.trim()) {
    try {
      parsed = JSON.parse(raw) as ApiResponse<null>;
    } catch {
      throw new Error("Feedback submit failed: non-JSON response");
    }
  }

  if (!response.ok || !parsed?.success) {
    throw new Error(parsed?.error || `Unable to submit feedback (HTTP ${response.status})`);
  }
}

// ─── Phase 12.5: Room Games API ──────────────────────────────────────────────

export type RoomGameInfo = {
  id: string;
  code: string;
  title: string;
  host_id: string;
  status: string;
  winner: string;
  player_count: number;
  created_at: number;
  ended_at: number;
};

export async function fetchRoomGames(roomCode: string): Promise<RoomGameInfo[]> {
  const response = await fetch(`/api/room/${encodeURIComponent(roomCode)}/games`);
  const raw = await response.text();
  let payload: ApiResponse<RoomGameInfo[]> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<RoomGameInfo[]>;
    } catch {
      throw new Error("Fetch room games failed: non-JSON response");
    }
  }
  if (!response.ok || !payload?.success || !payload.data) {
    throw new Error(payload?.error || `Unable to fetch room games (HTTP ${response.status})`);
  }
  return payload.data;
}

export async function createRoomGame(
  roomCode: string,
  title: string,
  buzzwords: string[],
  authToken: string,
): Promise<RoomGameInfo> {
  const response = await fetch(`/api/room/${encodeURIComponent(roomCode)}/games`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${authToken}`,
    },
    body: JSON.stringify({ title, buzzwords }),
  });
  const raw = await response.text();
  let payload: ApiResponse<RoomGameInfo> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<RoomGameInfo>;
    } catch {
      throw new Error("Create room game failed: non-JSON response");
    }
  }
  if (!response.ok || !payload?.success || !payload.data) {
    throw new Error(payload?.error || `Unable to create room game (HTTP ${response.status})`);
  }
  return payload.data;
}

export async function deleteRoomGame(
  roomCode: string,
  gameCode: string,
  authToken: string,
): Promise<void> {
  const response = await fetch(
    `/api/room/${encodeURIComponent(roomCode)}/games/${encodeURIComponent(gameCode)}`,
    {
      method: "DELETE",
      headers: { Authorization: `Bearer ${authToken}` },
    },
  );
  const raw = await response.text();
  let payload: ApiResponse<null> | null = null;
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as ApiResponse<null>;
    } catch {
      throw new Error("Delete room game failed: non-JSON response");
    }
  }
  if (!response.ok || !payload?.success) {
    throw new Error(payload?.error || `Unable to delete room game (HTTP ${response.status})`);
  }
}

