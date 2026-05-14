import { describe, expect, it } from "vitest";
import { hasBingo, shuffleArray, toCellId } from "./board";
import type { BoardState } from "./board";

// ─── helpers ─────────────────────────────────────────────────────────────────

function makeBoard(rows: number, cols: number, markedIds: string[] = []): BoardState {
  const marked = new Set(markedIds);
  const cells = [];
  for (let r = 0; r < rows; r++) {
    for (let c = 0; c < cols; c++) {
      const id = toCellId(r, c);
      cells.push({ id, text: id, marked: marked.has(id) });
    }
  }
  return { rows, cols, cells };
}

// ─── toCellId ────────────────────────────────────────────────────────────────

describe("toCellId", () => {
  it("maps row 0 col 0 to A1", () => {
    expect(toCellId(0, 0)).toBe("A1");
  });
  it("maps row 0 col 4 to A5", () => {
    expect(toCellId(0, 4)).toBe("A5");
  });
  it("maps row 4 col 0 to E1", () => {
    expect(toCellId(4, 0)).toBe("E1");
  });
  it("maps row 4 col 4 to E5", () => {
    expect(toCellId(4, 4)).toBe("E5");
  });
  it("maps row 1 col 2 to B3", () => {
    expect(toCellId(1, 2)).toBe("B3");
  });
});

// ─── hasBingo — no win ───────────────────────────────────────────────────────

describe("hasBingo — no win", () => {
  it("returns false for empty board", () => {
    expect(hasBingo(makeBoard(5, 5))).toBe(false);
  });

  it("returns false with scattered marks (no line)", () => {
    expect(hasBingo(makeBoard(5, 5, ["A1", "A3", "B2", "C4", "D1", "E3"]))).toBe(false);
  });

  it("returns false with 4 of 5 in a row", () => {
    expect(hasBingo(makeBoard(5, 5, ["A1", "A2", "A3", "A4"]))).toBe(false);
  });

  it("returns false with 4 of 5 in a column", () => {
    expect(hasBingo(makeBoard(5, 5, ["A1", "B1", "C1", "D1"]))).toBe(false);
  });

  it("returns false with 4 of 5 on main diagonal", () => {
    expect(hasBingo(makeBoard(5, 5, ["A1", "B2", "C3", "D4"]))).toBe(false);
  });
});

// ─── hasBingo — row wins ──────────────────────────────────────────────────────

describe("hasBingo — row wins", () => {
  it("detects full first row", () => {
    expect(hasBingo(makeBoard(5, 5, ["A1", "A2", "A3", "A4", "A5"]))).toBe(true);
  });

  it("detects full last row", () => {
    expect(hasBingo(makeBoard(5, 5, ["E1", "E2", "E3", "E4", "E5"]))).toBe(true);
  });

  it("detects full middle row", () => {
    expect(hasBingo(makeBoard(5, 5, ["C1", "C2", "C3", "C4", "C5"]))).toBe(true);
  });

  it("detects row win on 3x3 board", () => {
    expect(hasBingo(makeBoard(3, 3, ["B1", "B2", "B3"]))).toBe(true);
  });
});

// ─── hasBingo — column wins ───────────────────────────────────────────────────

describe("hasBingo — column wins", () => {
  it("detects full first column", () => {
    expect(hasBingo(makeBoard(5, 5, ["A1", "B1", "C1", "D1", "E1"]))).toBe(true);
  });

  it("detects full last column", () => {
    expect(hasBingo(makeBoard(5, 5, ["A5", "B5", "C5", "D5", "E5"]))).toBe(true);
  });

  it("detects full middle column", () => {
    expect(hasBingo(makeBoard(5, 5, ["A3", "B3", "C3", "D3", "E3"]))).toBe(true);
  });
});

// ─── hasBingo — diagonal wins ─────────────────────────────────────────────────

describe("hasBingo — diagonal wins", () => {
  it("detects main diagonal (top-left to bottom-right)", () => {
    expect(hasBingo(makeBoard(5, 5, ["A1", "B2", "C3", "D4", "E5"]))).toBe(true);
  });

  it("detects reverse diagonal (top-right to bottom-left)", () => {
    expect(hasBingo(makeBoard(5, 5, ["A5", "B4", "C3", "D2", "E1"]))).toBe(true);
  });

  it("detects main diagonal on 3x3 board", () => {
    expect(hasBingo(makeBoard(3, 3, ["A1", "B2", "C3"]))).toBe(true);
  });

  it("detects reverse diagonal on 3x3 board", () => {
    expect(hasBingo(makeBoard(3, 3, ["A3", "B2", "C1"]))).toBe(true);
  });
});

// ─── hasBingo — edge cases ────────────────────────────────────────────────────

describe("hasBingo — edge cases", () => {
  it("detects win on 1x1 board with one marked cell", () => {
    expect(hasBingo(makeBoard(1, 1, ["A1"]))).toBe(true);
  });

  it("returns false on 1x1 board with no marked cell", () => {
    expect(hasBingo(makeBoard(1, 1))).toBe(false);
  });

  it("ignores extra marks — still detects win", () => {
    // Full row A plus scattered noise
    expect(
      hasBingo(makeBoard(5, 5, ["A1", "A2", "A3", "A4", "A5", "B2", "C4"]))
    ).toBe(true);
  });
});

// ─── shuffleArray ─────────────────────────────────────────────────────────────

describe("shuffleArray", () => {
  it("returns an array with the same elements", () => {
    const input = ["a", "b", "c", "d", "e"];
    expect(shuffleArray(input).sort()).toEqual([...input].sort());
  });

  it("does not mutate the original array", () => {
    const input = ["x", "y", "z"];
    const copy = [...input];
    shuffleArray(input);
    expect(input).toEqual(copy);
  });

  it("returns an array of the same length", () => {
    const input = Array.from({ length: 25 }, (_, i) => `word${i}`);
    expect(shuffleArray(input)).toHaveLength(25);
  });

  it("handles an empty array", () => {
    expect(shuffleArray([])).toEqual([]);
  });

  it("handles a single-element array", () => {
    expect(shuffleArray(["solo"])).toEqual(["solo"]);
  });

  it("produces different orderings across multiple calls (board uniqueness)", () => {
    // With a 25-word pool the probability of two shuffles being identical is
    // 1/25! ≈ 6e-26 — astronomically unlikely, so a persistent failure here
    // means the shuffle is broken (e.g. always returns input order).
    const pool = Array.from({ length: 25 }, (_, i) => `buzzword${i}`);
    const seen = new Set<string>();
    for (let i = 0; i < 10; i += 1) {
      seen.add(shuffleArray(pool).join(","));
    }
    expect(seen.size).toBeGreaterThan(1);
  });
});

