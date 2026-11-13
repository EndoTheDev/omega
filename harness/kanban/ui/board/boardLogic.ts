// Board logic for the kanban module UI. Pure and module-owned: the
// shell never imports this - Board.vue rides the composition, so this
// file (and its test) live inside harness/kanban/ui/ with the module.

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

// --- lane rails (card t_50a8c145): ANY lane folds to a thin vertical
// rail - the Hermes lane-collapse model, ruled in for omega. The old
// settled-only fold (done/archived collapsed, the "N folded" note) is
// superseded and gone: the lane model below is the only one.
//
// Two numbers are geometry contracts, not taste:
//   - the rail is ~36px wide (32-40 ruled), wide enough for a dot, a
//     vertical name and a count, narrow enough to keep the strip usable;
//   - the fold eases over ~180ms (Hermes snaps; omega ruled an ease).
export const LANE_RAIL_PX = 36;
export const LANE_EASE_MS = 180;

/** the per-lane dot tone, the Hermes status mapping (ruled verbatim). */
export const LANE_TONES: Record<BoardStatus, string> = {
  triage: "#8f8f8f", // gray
  todo: "#cfcfcf", // light gray
  scheduled: "#a78bfa", // violet
  ready: "#60a5fa", // blue
  running: "#34d399", // green
  blocked: "#f87171", // red
  review: "#fbbf24", // amber
  done: "#6f6f6f", // gray
  archived: "#4a4a4a", // dim
};

/** the dot tone for a lane; an unknown status falls back to triage's gray. */
export function laneTone(status: string): string {
  return LANE_TONES[status as BoardStatus] ?? LANE_TONES.triage;
}

// A pin is the user's override for one lane: "open" (pinned open) or
// "collapsed" (pinned to its rail). Absent = no override, which reads as
// open for a lane holding cards.
export type LanePin = "open" | "collapsed";
export type LanePins = Record<string, LanePin>;

/**
 * laneFolded is the ONE fold law. Reality outranks the override: an
 * EMPTY lane is always folded to its rail, pinned open or not. A
 * non-empty lane follows its pin, and with no pin it stays expanded.
 */
export function laneFolded(status: string, count: number, pins: LanePins): boolean {
  if (count <= 0) return true; // a drained lane collapses - the auto-collapse law
  return pins[status] === "collapsed";
}

/**
 * reconcileLanePins drops every override whose lane is now empty. This
 * is the second half of the auto-collapse law: a pin holds while the
 * lane holds cards, and dies the moment the lane drains - so a lane
 * that refills comes back to its default (expanded) instead of
 * remembering an override reality has already overruled. Call it once
 * per list read; it is pure (an unchanged map is returned as-is only
 * when nothing was dropped).
 */
export function reconcileLanePins(
  counts: Record<string, number>,
  pins: LanePins,
): LanePins {
  let next: LanePins | null = null;
  for (const status of Object.keys(pins)) {
    if ((counts[status] ?? 0) > 0) continue;
    if (next === null) next = { ...pins };
    delete next[status];
  }
  return next ?? pins;
}

/**
 * toggleLanePin flips one lane between its rail and its column. An empty
 * lane cannot be opened - reality wins there too, so the click is a
 * no-op and no override is written (the rail stays the drop target).
 */
export function toggleLanePin(
  status: string,
  count: number,
  pins: LanePins,
): LanePins {
  if (count <= 0) return pins;
  const next = laneFolded(status, count, pins) ? "open" : "collapsed";
  return { ...pins, [status]: next };
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
  priority: number;
  session_id: string;
  source: string;
  created_at: string;
  started_at: string;
  completed_at: string;
}

// --- priority (the round t_f81e845e): an integer urgency tiebreak in
// the Hermes shape (higher = more urgent, 0 = normal). The store
// accepts any non-negative int (tools/CLI may set more); the board UI
// offers the 0..3 ladder and the card chip renders whatever stands.

export const PRIORITY_LEVELS = [0, 1, 2, 3] as const;

/** the chip/sel label for a priority value. */
export function priorityLabel(n: number): string {
  return `P${n}`;
}

/** true when a card wears its priority chip (0 = normal, silent). */
export function hasPriorityChip(n: number): boolean {
  return n > 0;
}

/** the BandSelect options for the priority ladder (0 = normal). */
export function priorityOptions(): { value: number; label: string }[] {
  return PRIORITY_LEVELS.map((n) => ({
    value: n,
    label: n === 0 ? "P0 (normal)" : priorityLabel(n),
  }));
}

/**
 * bodyPreview is the card's dimmed teaser: the body's first non-empty
 * line, truncated for the column width.
 */
export function bodyPreview(body: string): string {
  const line = (body ?? "")
    .split("\n")
    .map((s) => s.trim())
    .find((s) => s !== "");
  if (line === undefined) return "";
  return line.length > 80 ? line.slice(0, 79) + "…" : line;
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

// One audit-trail row (GET /tasks/{id} payload, the "events" array).
export interface BoardEvent {
  id: number;
  task_id: string;
  kind: string;
  detail: string;
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

/** the DELETE path for a registry board (name always encoded). */
export function boardDeletePath(name: string): string {
  return `/boards/${encodeURIComponent(name)}`;
}

/**
 * taskDeletePath is the DELETE path for one card (id always encoded) -
 * the detail path with the DELETE method; board selection rides
 * withBoard like every other task route.
 */
export function taskDeletePath(id: string): string {
  return `/tasks/${encodeURIComponent(id)}`;
}

/**
 * reorderAfter moves one task to (status, after a sibling) inside the
 * board's flat list and returns the new array - or null when the id is
 * unknown. afterId names the card ABOVE the insertion slot ("" = top
 * of the column), the exact pair the server's PATCH after_id takes, so
 * the optimistic board view is exactly what persisted.
 */
export function reorderAfter(
  tasks: BoardTask[],
  id: string,
  status: string,
  afterId: string,
): BoardTask[] | null {
  const from = tasks.findIndex((t) => t.id === id);
  if (from < 0) return null;
  const rest = tasks.slice();
  const moved = rest.splice(from, 1)[0];
  let at = 0;
  if (afterId) {
    const j = rest.findIndex((t) => t.id === afterId);
    at = j < 0 ? 0 : j + 1;
  }
  rest.splice(at, 0, { ...moved, status });
  return rest;
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

// --- the mounted /boards fetch retry (nightlies 42268953671 and
// 42260775149, cards t_7cdac186 / t_82e5d149): the board switcher chip
// gates on `boards.length > 1 || boardName`, and fetchBoards() runs
// ONCE per mount - a fetch that misses under CI suite load left no
// chip for the whole page life. Bounded retry: a few attempts with a
// short backoff; a 404 exits immediately (single-board installs will
// not heal on a wait); a final miss keeps the old behavior - silent,
// the board still renders, the journey's reload fallback stays the
// last resort. Lives here so the stubbed-fetch test can pin it.

export const BOARD_FETCH_ATTEMPTS = 3;
export const BOARD_FETCH_BACKOFF_MS = 300;

export type BoardFetch = (url: string) => Promise<Response>;

export type FetchBoardsOpts = {
  attempts?: number;
  backoffMs?: number;
  sleep?: (ms: number) => Promise<void>;
};

/**
 * fetchBoardsRetry GETs /boards with a bounded retry and returns the
 * board registry, or null when the capability is absent (404) or every
 * attempt misses. Lives in boardLogic because it is pure: the fetch
 * comes in as a parameter, the sleep is injectable, no window state.
 */
export async function fetchBoardsRetry(
  fetchImpl: BoardFetch,
  opts: FetchBoardsOpts = {},
): Promise<BoardInfo[] | null> {
  const attempts = opts.attempts ?? BOARD_FETCH_ATTEMPTS;
  const backoffMs = opts.backoffMs ?? BOARD_FETCH_BACKOFF_MS;
  const sleep =
    opts.sleep ?? ((ms: number) => new Promise<void>((r) => setTimeout(r, ms)));
  for (let attempt = 0; attempt < attempts; attempt++) {
    try {
      const res = await fetchImpl("/boards");
      if (res.ok) {
        const body = await res.json();
        return (body.boards ?? []) as BoardInfo[];
      }
      if (res.status === 404) return null; // capability absent: single-board install - a wait will not heal it
    } catch {
      // hiccup: transient under suite load, ride the backoff below
    }
    if (attempt < attempts - 1) await sleep(backoffMs);
  }
  return null; // final miss: silent, the board still renders
}