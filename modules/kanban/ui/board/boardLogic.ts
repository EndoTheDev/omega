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