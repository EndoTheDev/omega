// Board logic for the kanban module UI. Pure and module-owned: the
// shell never imports this - Board.vue rides the composition, so this
// file (and its test) live inside modules/kanban/ui/ with the module.

// The canonical column order - the exact 9-status hermes grammar.
export const BOARD_STATUSES = [
  "triage",
  "todo",
  "scheduled",
  "ready",
  "running",
  "blocked",
  "review",
  "done",
  "archived",
] as const;

export type BoardStatus = (typeof BOARD_STATUSES)[number];

// The URL the board view lives at (the shell anchors the main-pane
// switcher here, the same way /chat/<id> anchors a session).
export const BOARD_PATH = "/board";

/** true when p is the board route ("/board", trailing slash tolerated). */
export function isBoardPath(p: string): boolean {
  return p === BOARD_PATH || p === BOARD_PATH + "/";
}

/** true for the two resting columns (rendered dimmed / collapsed). */
export function isSettled(status: string): boolean {
  return status === "done" || status === "archived";
}

// Narrow-board adaptation (card t_49d0b308): the inspector's ~300px
// column cannot host the wide column strip, so the rendered width flips
// the `narrow` class and the columns stack instead of scrolling
// sideways. The threshold sits above the sidebar's default 240-300 and
// under every snap point's slack: a drag past 320 widens back.
export const NARROW_MAX_PX = 360;

/** narrow below this rendered width; zero (unmounted measure noise) never is */
export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}

// One task row, exactly what GET /tasks returns.
export interface BoardTask {
  id: string;
  title: string;
  body: string;
  assignee: string;
  status: string;
  session_id: string;
  source: string;
  created_at: string;
  started_at: string;
  completed_at: string;
}

// --- board switcher (card t_d7a7beaa): named boards + a current-board
// pointer, Hermes-shaped. The pointer serves the default; /boards
// lists the registry. There is no cross-board move anywhere - a card
// lives on exactly one board.

// One registry row (GET /boards payload).
export interface BoardInfo {
  name: string;
  current: boolean;
}

/** true when a registry row names the current board. */
export function isCurrentBoard(b: BoardInfo): boolean {
  return b.current;
}

/**
 * withBoard appends ?board=... to an API path, keeping any existing
 * query string intact. Empty board adds nothing (the server then
 * serves its own default - the pointer target), so pre-registry
 * installs hit the exact same URLs as before.
 */
export function withBoard(path: string, board: string): string {
  const trimmed = (board ?? "").trim();
  if (trimmed === "") return path;
  const join = path.includes("?") ? "&" : "?";
  return `${path}${join}board=${encodeURIComponent(trimmed)}`;
}

/**
 * pickBootBoard chooses the switcher's initial selection from the
 * /boards payload: the flagged current board, else "main". An empty
 * registry (capability absent) degrades to "" - no switcher.
 */
export function pickBootBoard(boards: BoardInfo[]): string {
  if (!Array.isArray(boards) || boards.length === 0) return "";
  const cur = boards.find((b) => b && isCurrentBoard(b));
  return cur ? cur.name : boards[0].name;
}

// One comment row (GET /tasks/{id} payload).
export interface BoardComment {
  id: number;
  task_id: string;
  author: string;
  body: string;
  created_at: string;
}

/**
 * columns buckets tasks into the canonical column order. Unknown
 * statuses cannot exist (the store validates at the door), but nothing
 * here throws on one regardless - it just lands nowhere and the count
 * mismatch would show.
 */
export function columns(tasks: BoardTask[]): BoardTask[][] {
  const by = new Map<string, BoardTask[]>(
    BOARD_STATUSES.map((s) => [s, [] as BoardTask[]]),
  );
  for (const t of tasks) {
    const col = by.get(t.status);
    if (!col) continue; // unknown status is not a column
    col.push(t);
  }
  return BOARD_STATUSES.map((s) => by.get(s) as BoardTask[]);
}

/**
 * step moves one frame along the board for the arrow buttons: +1 right,
 * -1 left, clamped at the edges (out-of-range returns null - the button
 * does not render).
 */
export function step(status: string, dir: -1 | 1): string | null {
  const i = BOARD_STATUSES.indexOf(status as BoardStatus);
  if (i < 0) return null;
  const j = i + dir;
  if (j < 0 || j >= BOARD_STATUSES.length) return null;
  return BOARD_STATUSES[j];
}

// --- board removal (card t_95c5527f): the remove control beside the
// switcher. "main" is the legacy default board (DefaultBoardName on the
// Go side); its control stays disabled and the API refuses it anyway -
// DeleteBoard is the law, this is just the double-take before firing.

export const DEFAULT_BOARD_NAME = "main";

/** true when a named board may carry the remove control: anything but
 * the default board, and never the empty registry selection. */
export function canRemoveBoard(name: string): boolean {
  return name !== "" && name !== DEFAULT_BOARD_NAME;
}

/** the DELETE path for removing a registry board (name always encoded). */
export function boardDeletePath(name: string): string {
  return `/boards/${encodeURIComponent(name)}`;
}

/**
 * age renders a card's created_at as a compact relative age
 * ("now", "42m", "3h", "5d"). Unparseable stamps render nothing.
 */
export function age(iso: string, now: Date = new Date()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const s = Math.max(0, Math.floor((now.getTime() - t) / 1000));
  if (s < 60) return "now";
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}