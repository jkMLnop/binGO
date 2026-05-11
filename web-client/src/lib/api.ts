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
