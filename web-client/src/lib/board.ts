// Fisher-Yates shuffle — returns a new shuffled array, does not mutate the input
export function shuffleArray<T>(arr: T[]): T[] {
  const result = arr.slice();
  for (let i = result.length - 1; i > 0; i -= 1) {
    const j = Math.floor(Math.random() * (i + 1));
    [result[i], result[j]] = [result[j], result[i]];
  }
  return result;
}

export type BoardCell = {
  id: string;
  text: string;
  marked: boolean;
};

export type BoardState = {
  rows: number;
  cols: number;
  cells: BoardCell[];
};

export function toCellId(row: number, col: number): string {
  return `${String.fromCharCode(65 + row)}${col + 1}`;
}

export function hasBingo(board: BoardState): boolean {
  const marked = new Set(board.cells.filter((cell) => cell.marked).map((cell) => cell.id));

  for (let r = 0; r < board.rows; r += 1) {
    let rowWin = true;
    for (let c = 0; c < board.cols; c += 1) {
      if (!marked.has(toCellId(r, c))) {
        rowWin = false;
      }
    }
    if (rowWin) {
      return true;
    }
  }

  for (let c = 0; c < board.cols; c += 1) {
    let colWin = true;
    for (let r = 0; r < board.rows; r += 1) {
      if (!marked.has(toCellId(r, c))) {
        colWin = false;
      }
    }
    if (colWin) {
      return true;
    }
  }

  let diagMain = true;
  let diagReverse = true;
  for (let i = 0; i < board.rows; i += 1) {
    if (!marked.has(toCellId(i, i))) {
      diagMain = false;
    }
    if (!marked.has(toCellId(i, board.cols - 1 - i))) {
      diagReverse = false;
    }
  }

  return diagMain || diagReverse;
}
