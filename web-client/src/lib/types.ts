export type ApiResponse<T> = {
  success: boolean;
  data?: T;
  error?: string;
};

export type GameInfo = {
  id: string;
  code: string;
  host_id: string;
  status: string;
  winner?: string;
  player_count: number;
  created_at: number;
};

export type LeaderboardEntry = {
  username: string;
  wins: number;
  rank: number;
};

export type ClientMessage = {
  action: string;
  username?: string;
  token?: string;
  code?: string;
  room_code?: string;
  phrase?: string;
};

export type Suggestion = {
  player_id: string;
  phrase: string;
};

export type BetCondition = {
  player_username: string;
  outcome: string;
};

export type Bet = {
  id: string;
  better_id: string;
  better_username: string;
  raw_text: string;
  conditions: BetCondition[];
  status: string;
};

export type ServerMessage = {
  type: string;
  game_id: string;
  code?: string;
  room_code?: string; // Phase 11.0: 5-char room code
  host_id?: string;
  player_id: string;
  username?: string;
  token?: string;
  rows: number;
  cols: number;
  buzzwords: string[][];
  players: string[];
  winner: string;
  message: string;
  suggestions?: Suggestion[];
  active_bets?: Bet[];
  flat_buzzwords?: string[];
  rejected_suggestions?: string[];
};
