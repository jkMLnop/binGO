import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { Navigate, Route, Routes, useNavigate, useParams } from "react-router-dom";
import QRCode from "qrcode";
import { createGame, fetchGameByCode, fetchLeaderboard, fetchRoomLeaderboard, getRoomBuzzwords, setRoomBuzzwords, streamBuzzwords } from "./lib/api";
import { hasBingo, shuffleArray, toCellId } from "./lib/board";
import type { BoardCell, BoardState } from "./lib/board";
import type { Bet, ClientMessage, LeaderboardEntry, ServerMessage, Suggestion } from "./lib/types";
import type { WordSet } from "./lib/api";

type HelpEntry = { cmd: string; desc: string; hostOnly?: boolean };

const HELP_ENTRIES: HelpEntry[] = [
  { cmd: "Click a cell",             desc: "Mark / unmark that buzzword" },
  { cmd: "Auto-win detection",       desc: "Bingo is announced automatically when you complete a row, column, or diagonal" },
  { cmd: "Restart Game",             desc: "Start a new round with the same code (host only)", hostOnly: true },
  { cmd: "Leave Game",               desc: "Disconnect and return to the join screen" },
  { cmd: "Copy Share Link",          desc: "Copy the URL so others can join" },
  { cmd: "Suggest Buzzword",         desc: "Propose a new buzzword to be added to the pool" },
  { cmd: "Approve / Reject",         desc: "Accept or decline a pending buzzword suggestion (host only)", hostOnly: true },
  { cmd: "Buzzwords",                desc: "See all buzzwords in the current pool, and host-rejected phrases" },
  { cmd: "Place Bet",                desc: "Bet on who wins — format: player wins|loses (AND to chain)" },
  { cmd: "Leaderboard",              desc: "Top players by wins — visible in the Leaderboard panel" },
];

function HelpPanel({ isHost, onClose }: { isHost: boolean; onClose: () => void }) {
  return (
    <section className="panel help-panel" role="dialog" aria-label="Help">
      <div className="help-header">
        <h2>Commands &amp; Controls</h2>
        <button type="button" className="help-close" onClick={onClose} aria-label="Close help">
          ✕
        </button>
      </div>
      <table className="help-table">
        <thead>
          <tr>
            <th>Action</th>
            <th>Description</th>
          </tr>
        </thead>
        <tbody>
          {HELP_ENTRIES.filter((e) => !e.hostOnly || isHost).map((e) => (
            <tr key={e.cmd} className={e.hostOnly ? "host-row" : ""}>
              <td><code>{e.cmd}</code>{e.hostOnly ? " 👑" : ""}</td>
              <td>{e.desc}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {isHost && <p className="help-note">👑 = host-only actions</p>}
    </section>
  );
}

// ── Modal shell ─────────────────────────────────────────────────────────────

function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  return (
    <div className="modal-backdrop" role="dialog" aria-modal="true" aria-label={title}>
      <div className="modal">
        <div className="modal-header">
          <h3>{title}</h3>
          <button type="button" className="help-close" onClick={onClose} aria-label="Close">✕</button>
        </div>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  );
}

// ── Suggest modal ────────────────────────────────────────────────────────────

function SuggestModal({ onSubmit, onClose }: {
  onSubmit: (phrase: string) => void;
  onClose: () => void;
}) {
  const [phrase, setPhrase] = useState("");
  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = phrase.trim();
    if (!trimmed) return;
    onSubmit(trimmed);
    onClose();
  }
  return (
    <Modal title="Suggest a Buzzword" onClose={onClose}>
      <form onSubmit={handleSubmit} className="modal-form">
        <p className="modal-hint">Propose a new phrase to add to the buzzword pool. The host can approve or reject it.</p>
        <input
          autoFocus
          value={phrase}
          onChange={(e) => setPhrase(e.target.value)}
          placeholder="e.g. Let's circle back on that"
          maxLength={100}
        />
        <button type="submit" className="action-btn restart-btn" disabled={!phrase.trim()}>Submit</button>
      </form>
    </Modal>
  );
}

// ── Bet modal ────────────────────────────────────────────────────────────────

function BetModal({ players, currentUser, onSubmit, onClose }: {
  players: string[];
  currentUser: string;
  onSubmit: (phrase: string) => void;
  onClose: () => void;
}) {
  const selectablePlayers = players;
  const [conditions, setConditions] = useState([{ player: selectablePlayers[0] ?? "", outcome: "wins" }]);

  function addCondition() {
    setConditions((c) => [...c, { player: selectablePlayers[0] ?? "", outcome: "wins" }]);
  }
  function removeCondition(i: number) {
    setConditions((c) => c.filter((_, idx) => idx !== i));
  }
  function update(i: number, field: "player" | "outcome", val: string) {
    setConditions((c) => c.map((item, idx) => idx === i ? { ...item, [field]: val } : item));
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (conditions.some((c) => !c.player)) return;
    const phrase = conditions.map((c) => `${c.player} ${c.outcome}`).join(" AND ");
    onSubmit(phrase);
    onClose();
  }

  return (
    <Modal title="Place a Bet" onClose={onClose}>
      <form onSubmit={handleSubmit} className="modal-form">
        <p className="modal-hint">Bet on who will win or lose this round. Chain multiple conditions with AND.</p>
        {conditions.map((cond, i) => (
          <div key={i} className="bet-row">
            {i > 0 && <span className="bet-and">AND</span>}
            <select value={cond.player} onChange={(e) => update(i, "player", e.target.value)}>
              {selectablePlayers.length === 0
                ? <option value="">No players</option>
                : selectablePlayers.map((p) => (
                  <option key={p} value={p}>
                    {p}{p === currentUser ? " (you)" : ""}
                  </option>
                ))
              }
            </select>
            <select value={cond.outcome} onChange={(e) => update(i, "outcome", e.target.value)}>
              <option value="wins">wins</option>
              <option value="loses">loses</option>
            </select>
            {conditions.length > 1 && (
              <button type="button" className="bet-remove" onClick={() => removeCondition(i)}>✕</button>
            )}
          </div>
        ))}
        <button type="button" className="bet-add-btn" onClick={addCondition}>+ Add condition</button>
        <button type="submit" className="action-btn restart-btn" disabled={conditions.some((c) => !c.player)}>
          Place Bet
        </button>
      </form>
    </Modal>
  );
}

// ── Suggestions panel ────────────────────────────────────────────────────────

function SuggestionsPanel({ suggestions, isHost, onApprove, onReject }: {
  suggestions: Suggestion[];
  isHost: boolean;
  onApprove: (phrase: string) => void;
  onReject: (phrase: string) => void;
}) {
  if (suggestions.length === 0) return null;
  return (
    <section className="panel suggestions-panel">
      <h3>📝 Pending Suggestions</h3>
      <ul className="suggestions-list">
        {suggestions.map((s) => (
          <li key={s.phrase} className="suggestion-item">
            <span className="suggestion-who">{s.player_id}</span>
            <span className="suggestion-phrase">"{s.phrase}"</span>
            {isHost && (
              <span className="suggestion-actions">
                <button type="button" className="sug-approve" onClick={() => onApprove(s.phrase)}>✓ Approve</button>
                <button type="button" className="sug-reject" onClick={() => onReject(s.phrase)}>✕ Reject</button>
              </span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

// ── Bets panel ───────────────────────────────────────────────────────────────

function BetsPanel({ bets }: { bets: Bet[] }) {
  if (bets.length === 0) return null;
  const statusIcon = (s: string) => s === "won" ? "✅" : s === "lost" ? "❌" : "⏳";
  return (
    <section className="panel bets-panel">
      <h3>🎲 Active Bets</h3>
      <ul className="bets-list">
        {bets.map((b) => (
          <li key={b.id} className={`bet-item bet-${b.status}`}>
            <span className="bet-who">{b.better_username}</span>
            <span className="bet-text">"{b.raw_text}"</span>
            <span className="bet-status">{statusIcon(b.status)}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}

// ── Buzzwords panel ──────────────────────────────────────────────────────────

function BuzzwordsPanel({ buzzwords, rejected, onClose }: {
  buzzwords: string[];
  rejected: string[];
  onClose: () => void;
}) {
  return (
    <section className="panel buzzwords-panel">
      <div className="help-header">
        <h3>Buzzword Pool ({buzzwords.length})</h3>
        <button type="button" className="help-close" onClick={onClose} aria-label="Close">✕</button>
      </div>
      <div className="buzzwords-grid">
        {buzzwords.map((w) => <span key={w} className="buzzword-chip">{w}</span>)}
      </div>
      {rejected.length > 0 && (
        <>
          <h4 className="rejected-heading">Rejected this round ({rejected.length})</h4>
          <div className="buzzwords-grid">
            {rejected.map((w) => <span key={w} className="buzzword-chip rejected">{w}</span>)}
          </div>
        </>
      )}
    </section>
  );
}

function HomePage() {
  const [code, setCode] = useState("");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState("");
  const navigate = useNavigate();

  function handleJoin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!code.trim()) {
      return;
    }
    navigate(`/game/${code.trim().toUpperCase()}`);
  }

  async function handleCreateGame() {
    setCreateError("");
    setCreating(true);
    try {
      const game = await createGame();
      navigate(`/game/${game.code}`);
    } catch (err) {
      setCreateError(
        err instanceof Error
          ? err.message
          : "Could not create a new game"
      );
    } finally {
      setCreating(false);
    }
  }

  return (
    <main className="shell">
      <section className="panel hero">
        <p className="eyebrow">binGO web client</p>
        <h1>Connect to {window.location.host}</h1>
        <p>Choose exactly like the CLI client:</p>

        <div className="cli-flow">
          <button
            type="button"
            className="cli-option"
            onClick={handleCreateGame}
            disabled={creating}
          >
            {creating ? "Creating..." : "1) Host a new game"}
          </button>

          <form className="join-form cli-join" onSubmit={handleJoin}>
            <label htmlFor="join-code">2) Join existing game (with room code)</label>
            <input
              id="join-code"
              value={code}
              onChange={(event) => setCode(event.target.value)}
              placeholder="BINGO-ABCDE"
              maxLength={11}
              aria-label="Room code"
            />
            <button type="submit" className="ghost-btn">Join by Code</button>
          </form>
        </div>

        {createError ? (
          <p className="inline-error">
            {createError} (set VITE_ADMIN_API_KEY if your server uses a custom admin key)
          </p>
        ) : null}
      </section>
    </main>
  );
}

function GamePage() {
  const { code = "" } = useParams();
  const normalizedCode = code.toUpperCase();
  const [username, setUsername] = useState("");
  const [currentUser, setCurrentUser] = useState("");
  const [gameStatus, setGameStatus] = useState("Checking room code...");
  const [error, setError] = useState("");
  const [players, setPlayers] = useState<string[]>([]);
  const [leaderboard, setLeaderboard] = useState<LeaderboardEntry[]>([]);
  const [connected, setConnected] = useState(false);
  const [isHost, setIsHost] = useState(false);
  const [hostConnected, setHostConnected] = useState(true);
  const [showHelp, setShowHelp] = useState(false);
  const [winner, setWinner] = useState("");
  const [winPending, setWinPending] = useState(false);
  const [board, setBoard] = useState<BoardState | null>(null);
  const [suggestions, setSuggestions] = useState<Suggestion[]>([]);
  const [bets, setBets] = useState<Bet[]>([]);
  const [buzzwordPool, setBuzzwordPool] = useState<string[]>([]);
  const [rejectedSuggestions, setRejectedSuggestions] = useState<string[]>([]);
  const [showBuzzwords, setShowBuzzwords] = useState(false);
  const [showSuggestModal, setShowSuggestModal] = useState(false);
  const [showBetModal, setShowBetModal] = useState(false);
  const [gameDead, setGameDead] = useState(false);
  const [gameDeadReason, setGameDeadReason] = useState("");
  const [showQR, setShowQR] = useState(false);
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [roomCode, setRoomCode] = useState("");
  const [roomLeaderboard, setRoomLeaderboard] = useState<LeaderboardEntry[]>([]);
  const [leaderboardTab, setLeaderboardTab] = useState<"all" | "room">("all");
  const [showBuzzwordUpload, setShowBuzzwordUpload] = useState(false);
  const [buzzwordUploadError, setBuzzwordUploadError] = useState("");
  const [showGenerate, setShowGenerate] = useState(false);
  const socketRef = useRef<WebSocket | null>(null);
  const tokenRef = useRef<string>("");
  const hostIdRef = useRef<string>("");
  const winSentRef = useRef(false);
  // Sync guard: set immediately (before React re-renders) whenever the game ends.
  // Checked in toggleCell and the hasBingo effect to prevent any interaction window.
  const gameEndedRef = useRef(false);

  useEffect(() => {
    let mounted = true;

    async function validateCode() {
      try {
        const gameInfo = await fetchGameByCode(normalizedCode);
        if (mounted) {
          const GAME_TTL_SECONDS = 60 * 60; // 60 minutes
          const ageSeconds = Math.floor(Date.now() / 1000) - gameInfo.created_at;
          if (ageSeconds > GAME_TTL_SECONDS) {
            setGameDead(true);
            setGameDeadReason("This room code has expired. Room codes are valid for 60 minutes.");
            setGameStatus("Room code expired");
          } else if (gameInfo.status === "ended" && !gameInfo.winner) {
            // Orphaned — host left before anyone won
            setGameDead(true);
            setGameDeadReason("The host has left and this game is no longer active.");
            setGameStatus("Game unavailable");
          } else if (gameInfo.winner) {
            // Game already has a winner — pre-set the ended state immediately from
            // the HTTP API so the UI is correct before the WebSocket even connects.
            gameEndedRef.current = true;
            winSentRef.current = true;
            setWinner(gameInfo.winner);
            setGameStatus(`Game already ended — winner: ${gameInfo.winner}`);
          } else {
            setGameStatus(`Game ${normalizedCode} is active`);
          }
        }
      } catch (err) {
        if (mounted) {
          setError(err instanceof Error ? err.message : "Game not available");
          setGameStatus("Unable to validate this room code");
        }
      }
    }

    async function loadLeaderboard() {
      try {
        const rows = await fetchLeaderboard();
        if (mounted) {
          setLeaderboard(rows);
        }
      } catch {
        // Leaderboard is optional for gameplay; keep UI usable even if DB is disabled.
      }
    }

    validateCode();
    loadLeaderboard();

    return () => {
      mounted = false;
      socketRef.current?.close();
    };
  }, [normalizedCode]);

  const gameLink = useMemo(() => `${window.location.origin}/game/${normalizedCode}`, [normalizedCode]);

  function sendMessage(msg: ClientMessage) {
    if (!socketRef.current || socketRef.current.readyState !== WebSocket.OPEN) {
      return;
    }
    socketRef.current.send(JSON.stringify(msg));
  }

  function randomUsername(): string {
    const adjectives = ["Bold", "Swift", "Bright", "Lucky", "Eager", "Calm", "Wild", "Sharp"];
    const nouns = ["Panda", "Falcon", "Tiger", "Otter", "Gecko", "Raven", "Fox", "Lynx"];
    const adj = adjectives[Math.floor(Math.random() * adjectives.length)];
    const noun = nouns[Math.floor(Math.random() * nouns.length)];
    const num = Math.floor(Math.random() * 100);
    return `${adj}${noun}${num}`;
  }

  function connectToGame(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");

    const trimmed = username.trim();
    const name = trimmed || randomUsername();
    if (!trimmed) {
      setUsername(name);
    }

    const protocol = window.location.protocol === "https:" ? "wss" : "ws";
    const socket = new WebSocket(`${protocol}://${window.location.host}/ws?code=${encodeURIComponent(normalizedCode)}`);
    socketRef.current = socket;

    socket.onopen = () => {
      const authMessage: ClientMessage = tokenRef.current
        ? { action: "login", token: tokenRef.current, code: normalizedCode }
        : { action: "login", username: name, code: normalizedCode };
      socket.send(JSON.stringify(authMessage));
    };

    socket.onmessage = (eventMessage) => {
      const message = JSON.parse(eventMessage.data) as ServerMessage;

      if (message.type === "error") {
        // Suppress the "game has already ended" error — we show this state via the
        // game-ended banner instead, not as a red error panel.
        if (winPending || gameEndedRef.current) {
          if ((message.message || "").includes("game has already ended with winner")) {
            setWinPending(false);
            return;
          }
        }
        setError(message.message || "Server error");
        return;
      }

      if (message.type === "welcome") {
        tokenRef.current = message.token || "";
        setConnected(true);
        const resolvedUser = message.username || name;
        setCurrentUser(resolvedUser);
        const hostMatch = !!message.host_id && message.player_id === message.host_id;
        setIsHost(hostMatch);
        if (hostMatch) hostIdRef.current = message.host_id ?? "";
        setHostConnected(true);
        setPlayers(message.players || []);
        const existingWinner = message.winner || "";
        gameEndedRef.current = Boolean(existingWinner);
        winSentRef.current = Boolean(existingWinner);
        setWinner(existingWinner);
        setWinPending(false);
        setError("");

        const cells: BoardCell[] = [];
        const words = shuffleArray(message.buzzwords.flat());
        for (let i = 0; i < message.rows * message.cols; i += 1) {
          const row = Math.floor(i / message.cols);
          const col = i % message.cols;
          cells.push({ id: toCellId(row, col), text: words[i] || "", marked: false });
        }

        setBoard({ rows: message.rows, cols: message.cols, cells });
        if (!existingWinner) {
          setGameStatus(`Connected to ${message.game_id}`);
        }
        // Phase 11.0: capture room code from welcome message
        if (message.room_code) {
          setRoomCode(message.room_code);
          fetchRoomLeaderboard(message.room_code).then(setRoomLeaderboard).catch(() => {});
        }
        // If existingWinner is set, keep the status from validateCode (already informative).
      }

      if (message.type === "player_update") {
        setPlayers(message.players || []);
      }

      if (message.type === "game_ended") {
        const announcedWinner = message.winner || "";
        gameEndedRef.current = true;
        winSentRef.current = true;
        setWinner(announcedWinner);
        setWinPending(false);
        const hostGone = (message.message || "").includes("Host has disconnected");
        setHostConnected(!hostGone);
        setGameStatus(
          announcedWinner === currentUser
            ? `You won this round!`
            : announcedWinner
              ? `${announcedWinner} won this round.`
              : "Round ended."
        );
      }

      if (message.type === "game_restart") {
        gameEndedRef.current = false;
        winSentRef.current = false;
        setWinner("");
        setWinPending(false);
        setPlayers(message.players || []);
        setHostConnected(true);
        setSuggestions([]);
        setBets([]);
        setBuzzwordPool([]);
        setRejectedSuggestions([]);
        setShowBuzzwords(false);
        const cells: BoardCell[] = [];
        const words = shuffleArray(message.buzzwords.flat());
        for (let i = 0; i < message.rows * message.cols; i += 1) {
          const row = Math.floor(i / message.cols);
          const col = i % message.cols;
          cells.push({ id: toCellId(row, col), text: words[i] || "", marked: false });
        }
        setBoard({ rows: message.rows, cols: message.cols, cells });
        setGameStatus("New round started — good luck!");
      }

      if (message.type === "suggestion_broadcast") {
        setSuggestions(message.suggestions || []);
      }

      if (message.type === "bets_update") {
        setBets(message.active_bets || []);
      }

      if (message.type === "buzzword_list") {
        setBuzzwordPool(message.flat_buzzwords || []);
        setRejectedSuggestions(message.rejected_suggestions || []);
        setShowBuzzwords(true);
      }

      if (message.type === "server_shutdown") {
        setConnected(false);
        setGameStatus("Server is shutting down.");
      }
    };

    socket.onclose = () => {
      setConnected(false);
      setIsHost(false);
      gameEndedRef.current = false;
    };
  }

  function toggleCell(cellId: string) {
    // gameEndedRef is a sync guard; winner state is the async guard.
    // Both are checked so interaction is blocked as soon as the game ends,
    // even before React re-renders with the new winner state.
    if (gameEndedRef.current) {
      return;
    }
    setBoard((current) => {
      if (!current || winner) {
        return current;
      }

      return {
        ...current,
        cells: current.cells.map((cell) =>
          cell.id === cellId ? { ...cell, marked: !cell.marked } : cell
        ),
      };
    });
  }

  useEffect(() => {
    if (!board || winner || winPending || winSentRef.current) {
      return;
    }

    if (hasBingo(board)) {
      winSentRef.current = true;
      setWinPending(true);
      setGameStatus("Bingo detected. Waiting for server confirmation...");
      sendMessage({ action: "win" });
    }
  }, [board, winner, winPending]);

  function handleRestart() {
    sendMessage({ action: "restart" });
  }

  function handleLeave() {
    socketRef.current?.close();
    setConnected(false);
    setBoard(null);
    setWinner("");
    gameEndedRef.current = false;
    setWinPending(false);
    winSentRef.current = false;
    setPlayers([]);
    setCurrentUser("");
    setIsHost(false);
    setGameStatus(`Game ${normalizedCode} is active`);
  }

  function handleSuggest(phrase: string) {
    sendMessage({ action: "suggest", phrase });
  }

  function handleApprove(phrase: string) {
    sendMessage({ action: "approve", phrase });
  }

  function handleReject(phrase: string) {
    sendMessage({ action: "reject", phrase });
  }

  function handleBet(phrase: string) {
    sendMessage({ action: "bet", phrase });
  }

  function handleListBuzzwords() {
    sendMessage({ action: "list_buzzwords" });
  }

  async function copyShareLink() {
    try {
      await navigator.clipboard.writeText(gameLink);
      setGameStatus("Share link copied to clipboard");
    } catch {
      setGameStatus("Copy failed. Select and copy the URL manually.");
    }
  }

  async function handleShowQR() {
    try {
      const url = await QRCode.toDataURL(gameLink, { width: 256, margin: 2 });
      setQrDataUrl(url);
      setShowQR(true);
    } catch {
      setGameStatus("QR code generation failed.");
    }
  }

  return (
    <main className="shell">
      <section className="panel game-header">
        <div>
          <p className="eyebrow">room code</p>
          <h1>{normalizedCode}</h1>
          <p>{gameStatus}</p>
          {connected && currentUser ? (
            <p className="identity">
              You are: {currentUser}{isHost ? " 👑 (host)" : ""}
            </p>
          ) : null}
        </div>
        <div className="header-actions">
          {connected && (
            <button onClick={() => setShowHelp((v) => !v)} className="ghost-btn" type="button">
              {showHelp ? "Hide Help" : "? Help"}
            </button>
          )}
          <button onClick={copyShareLink} className="ghost-btn" type="button">
            Copy Share Link
          </button>
          <button onClick={handleShowQR} className="ghost-btn" type="button">
            QR Code
          </button>
        </div>
      </section>

      {showHelp && connected && (
        <HelpPanel isHost={isHost} onClose={() => setShowHelp(false)} />
      )}

      {showQR && (
        <div className="modal-overlay" role="dialog" aria-modal="true" aria-label="QR Code to share game">
          <div className="modal-panel qr-panel">
            <h2>Share this game</h2>
            <p className="qr-link">{gameLink}</p>
            {qrDataUrl && <img src={qrDataUrl} alt="QR code for game link" width={256} height={256} />}
            <div className="modal-actions">
              <button onClick={copyShareLink} className="ghost-btn" type="button">Copy Link</button>
              <button onClick={() => setShowQR(false)} className="ghost-btn" type="button">Close</button>
            </div>
          </div>
        </div>
      )}

      {showBuzzwords && (
        <BuzzwordsPanel
          buzzwords={buzzwordPool}
          rejected={rejectedSuggestions}
          onClose={() => setShowBuzzwords(false)}
        />
      )}

      {suggestions.length > 0 && connected && (
        <SuggestionsPanel
          suggestions={suggestions}
          isHost={isHost}
          onApprove={handleApprove}
          onReject={handleReject}
        />
      )}

      {bets.length > 0 && connected && <BetsPanel bets={bets} />}

      {error ? <section className="panel error">{error}</section> : null}

      {!connected && gameDead ? (
        <section className="panel dead-game-panel">
          <p className="dead-game-reason">{gameDeadReason}</p>
        </section>
      ) : !connected ? (
        <section className="panel">
          <form className="join-form" onSubmit={connectToGame}>
            <label htmlFor="username">Username</label>
            <input
              id="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              placeholder="Your name (optional — we'll pick one)"
              maxLength={32}
            />
            <button type="submit">Join Game</button>
          </form>
        </section>
      ) : null}

      <section className="layout-grid">
        <article className="panel">
          <h2>Board</h2>
          {winner !== "" && (
            <div className="game-ended-banner">
              <strong>Round over</strong>
              {winner ? (
                <span> — Winner: <em>{winner}</em></span>
              ) : null}
              {isHost ? (
                <span className="banner-hint"> — you can restart below</span>
              ) : hostConnected ? (
                <span className="banner-hint"> — waiting for host to restart</span>
              ) : (
                <span className="banner-hint"> — host disconnected, game over</span>
              )}
            </div>
          )}
          {winner === "" && (
            <div className="board" role="grid" aria-label="Bingo board">
              {board?.cells.map((cell) => (
                <button
                  key={cell.id}
                  type="button"
                  className={`board-cell ${cell.marked ? "marked" : ""}`}
                  onClick={() => toggleCell(cell.id)}
                  disabled={winPending}
                >
                  <span className="cell-id">{cell.id}</span>
                  <span>{cell.text}</span>
                </button>
              ))}
            </div>
          )}
          {winner !== "" && (
            <div className="post-game-actions">
              {isHost && hostConnected && (
                <button type="button" className="action-btn restart-btn" onClick={handleRestart}>
                  Restart Game
                </button>
              )}
              {!isHost && !hostConnected && (
                <p className="host-gone">Host has disconnected — game cannot be restarted.</p>
              )}
              {!isHost && hostConnected && (
                <p className="waiting-restart">Waiting for host to restart…</p>
              )}
              <button type="button" className="action-btn leave-btn" onClick={handleLeave}>
                Leave Game
              </button>
            </div>
          )}
            {connected && !winner && (
              <div className="action-toolbar">
                <button type="button" className="toolbar-btn" onClick={() => setShowSuggestModal(true)}>
                  + Suggest Buzzword
                </button>
                <button type="button" className="toolbar-btn" onClick={() => setShowBetModal(true)}>
                  🎲 Place Bet
                </button>
                <button type="button" className="toolbar-btn" onClick={handleListBuzzwords}>
                  📋 Buzzwords
                </button>
                {isHost && roomCode && (
                  <button type="button" className="toolbar-btn" onClick={() => { setBuzzwordUploadError(""); setShowBuzzwordUpload(true); }}>
                    📤 Upload Word List
                  </button>
                )}
                {isHost && roomCode && (
                  <button type="button" className="toolbar-btn" onClick={() => setShowGenerate(true)}>
                    ✨ Generate with AI
                  </button>
                )}
              </div>
            )}
          </article>

        <article className="panel">
          <h2>Players</h2>
          <ul className="list">
            {players.map((player) => (
              <li key={player}>{player}</li>
            ))}
          </ul>
        </article>

        <article className="panel">
          <h2>Leaderboard</h2>
          {roomCode && (
            <div className="tab-row">
              <button
                type="button"
                className={`tab-btn${leaderboardTab === "all" ? " active" : ""}`}
                onClick={() => setLeaderboardTab("all")}
              >All Time</button>
              <button
                type="button"
                className={`tab-btn${leaderboardTab === "room" ? " active" : ""}`}
                onClick={() => { setLeaderboardTab("room"); fetchRoomLeaderboard(roomCode).then(setRoomLeaderboard).catch(() => {}); }}
              >This Room</button>
            </div>
          )}
          <ul className="list">
            {(leaderboardTab === "room" && roomCode ? roomLeaderboard : leaderboard).map((entry) => (
              <li key={entry.username}>
                <span>#{entry.rank}</span>
                <span>{entry.username}</span>
                <span>{entry.wins} wins</span>
              </li>
            ))}
          </ul>
        </article>
      </section>

        {showSuggestModal && (
          <SuggestModal onSubmit={handleSuggest} onClose={() => setShowSuggestModal(false)} />
        )}
        {showBetModal && (
          <BetModal
            players={players}
            currentUser={currentUser}
            onSubmit={handleBet}
            onClose={() => setShowBetModal(false)}
          />
        )}
        {showBuzzwordUpload && roomCode && (
          <BuzzwordUploadModal
            roomCode={roomCode}
            uploadedBy={currentUser}
            error={buzzwordUploadError}
            onClose={() => setShowBuzzwordUpload(false)}
            onSubmit={async (words) => {
              try {
                await setRoomBuzzwords(roomCode, words, currentUser);
                setShowBuzzwordUpload(false);
                setGameStatus("Custom word list uploaded for next round.");
              } catch (e) {
                setBuzzwordUploadError(e instanceof Error ? e.message : "Upload failed");
              }
            }}
          />
        )}
        {showGenerate && roomCode && (
          <GenerateModal
            roomCode={roomCode}
            hostId={hostIdRef.current}
            onApply={async (words) => {
              await setRoomBuzzwords(roomCode, words, currentUser);
              setShowGenerate(false);
              setGameStatus("AI-generated word list saved for next round.");
            }}
            onClose={() => setShowGenerate(false)}
          />
        )}
    </main>
  );
}

// ─── Phase 12.2: AI Generate Modal ──────────────────────────────────────────

function FollowUpInput({ onSubmit, disabled }: { onSubmit: (msg: string) => void; disabled: boolean }) {
  const [value, setValue] = useState("");
  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = value.trim();
    if (!trimmed) return;
    onSubmit(trimmed);
    setValue("");
  }
  return (
    <form onSubmit={handleSubmit} className="follow-up-form">
      <input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="Refine: 'make them funnier', 'add cosplay items'…"
        disabled={disabled}
      />
      <button type="submit" className="ghost-btn" disabled={disabled || !value.trim()}>
        Send
      </button>
    </form>
  );
}

function GenerateModal({
  roomCode,
  hostId,
  onApply,
  onClose,
}: {
  roomCode: string;
  hostId: string;
  onApply: (words: string[]) => Promise<void>;
  onClose: () => void;
}) {
  const [topic, setTopic] = useState("");
  const [url, setUrl] = useState("");
  const [messages, setMessages] = useState<Array<{ role: string; content: string }>>([]);
  const [streaming, setStreaming] = useState(false);
  const [streamText, setStreamText] = useState("");
  const [sets, setSets] = useState<WordSet[] | null>(null);
  const [genError, setGenError] = useState("");
  const [applying, setApplying] = useState(false);
  const outputRef = useRef<HTMLDivElement>(null);

  async function generate(msgs: Array<{ role: string; content: string }>) {
    setStreaming(true);
    setStreamText("");
    setSets(null);
    setGenError("");
    await streamBuzzwords(
      roomCode,
      topic,
      url || undefined,
      msgs,
      hostId,
      (chunk) => {
        setStreamText((t) => t + chunk);
        if (outputRef.current) outputRef.current.scrollTop = outputRef.current.scrollHeight;
      },
      (newSets) => {
        setSets(newSets);
        setStreaming(false);
      },
      (err) => {
        setGenError(err);
        setStreaming(false);
      },
    );
  }

  function handleGenerate(e: FormEvent) {
    e.preventDefault();
    if (!topic.trim() && !url.trim()) return;
    const userMsg = { role: "user", content: topic.trim() || url.trim() };
    const newMessages = [...messages, userMsg];
    setMessages(newMessages);
    generate(newMessages);
  }

  async function handleApply(words: string[]) {
    setApplying(true);
    try {
      await onApply(words);
    } catch (err) {
      setGenError(err instanceof Error ? err.message : "Apply failed");
    } finally {
      setApplying(false);
    }
  }

  function handleStartOver() {
    setTopic("");
    setUrl("");
    setMessages([]);
    setStreamText("");
    setSets(null);
    setGenError("");
  }

  const hasOutput = streamText || sets || genError;

  return (
    <div className="modal-overlay" role="dialog" aria-modal="true" aria-label="Generate word list with AI">
      <div className="modal-panel generate-modal">
        <div className="generate-modal-header">
          <h2>✨ Generate Word List with AI</h2>
          <button type="button" className="help-close" onClick={onClose} aria-label="Close">✕</button>
        </div>

        {!hasOutput && (
          <form onSubmit={handleGenerate} className="modal-form">
            <label>
              Describe your event or topic
              <input
                autoFocus
                value={topic}
                onChange={(e) => setTopic(e.target.value)}
                placeholder="e.g. Anime North convention"
                maxLength={500}
              />
            </label>
            <label>
              URL (optional — we'll scrape it for context)
              <input
                type="url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://example.com/event-page"
              />
            </label>
            <div className="modal-actions">
              <button
                type="submit"
                className="action-btn"
                disabled={streaming || (!topic.trim() && !url.trim())}
              >
                {streaming ? "Generating…" : "Generate"}
              </button>
              <button type="button" className="ghost-btn" onClick={onClose}>Cancel</button>
            </div>
          </form>
        )}

        {hasOutput && (
          <>
            <div className="generate-output" ref={outputRef}>
              <pre className="stream-text">{streamText || (streaming ? "Thinking…" : "")}</pre>
            </div>

            {sets && (
              <div className="word-sets">
                {sets.map((set) => (
                  <div key={set.label} className="word-set-card">
                    <div className="word-set-header">
                      <strong>{set.label}</strong>
                      <span className="word-count">{set.words.length} words</span>
                    </div>
                    <p className="word-set-preview">{set.words.slice(0, 5).join(", ")}…</p>
                    <button
                      type="button"
                      className="action-btn"
                      onClick={() => handleApply(set.words)}
                      disabled={applying}
                    >
                      {applying ? "Saving…" : "Use this set"}
                    </button>
                  </div>
                ))}
              </div>
            )}

            {genError && <p className="inline-error">{genError}</p>}

            {sets && !streaming && (
              <FollowUpInput
                onSubmit={(msg) => {
                  const userMsg = { role: "user", content: msg };
                  const newMessages = [...messages, userMsg];
                  setMessages(newMessages);
                  generate(newMessages);
                }}
                disabled={streaming}
              />
            )}

            <div className="modal-actions">
              <button type="button" className="ghost-btn" onClick={handleStartOver}>Start Over</button>
              <button type="button" className="ghost-btn" onClick={onClose}>Close</button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// ─── Phase 11.3: Buzzword Upload Modal ──────────────────────────────────────
function BuzzwordUploadModal({
  roomCode,
  uploadedBy,
  error,
  onClose,
  onSubmit,
}: {
  roomCode: string;
  uploadedBy: string;
  error: string;
  onClose: () => void;
  onSubmit: (words: string[]) => Promise<void>;
}) {
  const [raw, setRaw] = useState("");
  const [parseError, setParseError] = useState("");

  function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (evt) => {
      setRaw((evt.target?.result as string) || "");
      setParseError("");
    };
    reader.readAsText(file);
  }

  async function handleSubmit(evt: FormEvent) {
    evt.preventDefault();
    let words: string[] = [];
    const trimmed = raw.trim();
    if (trimmed.startsWith("[")) {
      try {
        const parsed = JSON.parse(trimmed);
        if (!Array.isArray(parsed)) throw new Error("Expected a JSON array");
        words = (parsed as unknown[]).map(String);
      } catch (err) {
        setParseError(err instanceof Error ? err.message : "Invalid JSON");
        return;
      }
    } else {
      // One word/phrase per line
      words = trimmed.split("\n").map((w) => w.trim()).filter(Boolean);
    }
    if (words.length < 24) {
      setParseError(`Need at least 24 words — got ${words.length}`);
      return;
    }
    setParseError("");
    await onSubmit(words);
  }

  return (
    <div className="modal-overlay" role="dialog" aria-modal="true" aria-label="Upload custom word list">
      <div className="modal-panel">
        <h2>Upload Word List</h2>
        <p>For room <strong>{roomCode}</strong>. Needs ≥ 24 words. Takes effect on next restart.</p>
        <form onSubmit={handleSubmit}>
          <label>
            Upload JSON file or paste words below
            <input type="file" accept=".json,.txt,.csv" onChange={handleFile} />
          </label>
          <textarea
            rows={8}
            placeholder={"[\"Synergy\", \"Circle back\", ...]\n— or one phrase per line —"}
            value={raw}
            onChange={(e) => { setRaw(e.target.value); setParseError(""); }}
          />
          {(parseError || error) && (
            <p className="inline-error">{parseError || error}</p>
          )}
          <div className="modal-actions">
            <button type="submit" className="action-btn">Upload</button>
            <button type="button" className="ghost-btn" onClick={onClose}>Cancel</button>
          </div>
        </form>
      </div>
    </div>
  );
}

function OfflineBanner() {
  const [isOnline, setIsOnline] = useState(() => navigator.onLine);

  useEffect(() => {
    const goOnline = () => setIsOnline(true);
    const goOffline = () => setIsOnline(false);
    window.addEventListener("online", goOnline);
    window.addEventListener("offline", goOffline);
    return () => {
      window.removeEventListener("online", goOnline);
      window.removeEventListener("offline", goOffline);
    };
  }, []);

  if (isOnline) return null;
  return (
    <div className="offline-banner" role="alert">
      You're offline — reconnect to keep playing
    </div>
  );
}

export default function App() {
  return (
    <>
      <OfflineBanner />
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/game/:code" element={<GamePage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </>
  );
}
