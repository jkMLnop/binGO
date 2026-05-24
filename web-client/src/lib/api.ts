import type { ApiResponse, GameInfo, LeaderboardEntry } from "./types";

const DEFAULT_ADMIN_KEY = "dev-admin-key-local-only";

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

export async function createGame(): Promise<GameInfo> {
  const adminKey =
    ((import.meta as { env?: { VITE_ADMIN_API_KEY?: string } }).env?.VITE_ADMIN_API_KEY as string | undefined) ||
    DEFAULT_ADMIN_KEY;

  const response = await fetch("/admin/api/games", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "X-Admin-Key": adminKey,
    },
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
  player_count: number;
  game_status: string;
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
