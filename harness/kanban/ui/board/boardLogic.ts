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

// The URL the board view lives at.
export const BOARD_PATH = "/board";

/** true when p is the board route ("/board", trailing slash tolerated). */
export function isBoardPath(p: string): boolean {
  return p === BOARD_PATH || p === BOARD_PATH + "/";
}

// lane rails: ANY lane folds to a thin vertical
// rail - the Hermes lane-collapse model, ruled in for omega. The old
// settled-only fold (done/archived collapsed, the "N folded" note) is
// superseded and gone: the lane model below is the only one.
//
// Two numbers are geometry contracts, not taste:
// the rail is ~36px wide (32-40 ruled), wide enough for a dot, a
//     vertical name and a count, narrow enough to keep the strip usable;
// the fold eases over ~180ms (Hermes snaps; omega ruled an ease).
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

// A pin is the user's override for one lane.
export type LanePin = "open" | "collapsed";
export type LanePins = Record<string, LanePin>;

/** laneFolded is the ONE fold law. */
export function laneFolded(status: string, count: number, pins: LanePins): boolean {
  const pin = pins[status];
  if (pin === "open") return false;
  if (pin === "collapsed") return true;
  return count <= 0; // a drained lane collapses - the auto-collapse law
}

/** toggleLanePin flips one lane between its rail and its column. */
export function toggleLanePin(
  status: string,
  count: number,
  pins: LanePins,
): LanePins {
  const next = laneFolded(status, count, pins) ? "open" : "collapsed";
  return { ...pins, [status]: next };
}

// Narrow-board adaptation: the inspector's ~300px column cannot host the wide column.
export const NARROW_MAX_PX = 360;

/** narrow below this rendered width; zero (unmounted measure noise) never is */
export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}

// One task row, exactly what GET /tasks returns.

// last_heartbeat_at rides only when the store stamps it.
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
  last_heartbeat_at?: string;
  // board is the NAME of the board a row lives on. Only the all-boards read (GET
  // /tasks?scope=all, the view's all-boards search scope) carries it: every other route
  // addresses a card by (board, id), so a mixed list must say which board a row belongs
  // to.
  board?: string;
}

// priority: an integer urgency tiebreak in the Hermes shape.

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

/** bodyPreview is the card's dimmed teaser. */
export function bodyPreview(body: string): string {
  const line = (body ?? "")
    .split("\n")
    .map((s) => s.trim())
    .find((s) => s !== "");
  if (line === undefined) return "";
  return line.length > 80 ? line.slice(0, 79) + "…" : line;
}

// card polish: the card is a reading surface, not a wall of text.

/** the card text clamp: two lines, title and teaser alike. */
export const CARD_CLAMP_LINES = 2;

// the running heartbeat chip
// "the running card dont have a heartbeat"). The store kept
// the data the eye never showed: a running card knows when it started,
// and a beating worker knows when it last spoke. The chip says.
//
// Two clocks, deliberately different:
// the WORKING duration only ever grows (from started_at);
// STALENESS only comes back when a beat lands (from
//     last_heartbeat_at) - a card cannot heal itself by waiting.
//
// HEARTBEAT_STALE_MS is the eye's threshold, not the dispatcher's: a
// reclaim waits an hour, and an hour of a card claiming to work while
// nothing beats is exactly the lie this chip exists to kill. Two
// minutes is a couple of missed beats of a worker that should tick
// every few seconds, and short enough to see across the board.
export const HEARTBEAT_STALE_MS = 120000;

/**
 * the chip's clock cadence: the label reads whole minutes, so a 5s tick advances it on
 * the minute without turning the board into a polling surface (this timer never
 * fetches).
 */
export const HEARTBEAT_TICK_MS = 5000;

// One running chip's rendered state.
export interface HeartbeatChip {
  label: string;
  stale: boolean;
  runningMs: number;
}

/** whole minutes, floored and never negative - the chip's own unit. */
export function minuteLabel(ms: number): string {
  return `${Math.max(0, Math.floor(ms / 60000))}m`;
}

/**
 * heartbeatChip is the chip for one card, or null when there is nothing to say.
 * A start it cannot parse is not a duration to invent, so that card wears the bare
 * "working" face.
 */
export function heartbeatChip(
  task: BoardTask,
  now: Date = new Date(),
): HeartbeatChip | null {
  if (task.status !== "running") return null;
  const startMs = Date.parse(task.started_at ?? "");
  const hasStart = !Number.isNaN(startMs);
  const runningMs = hasStart ? Math.max(0, now.getTime() - startMs) : 0;
  const beatMs = Date.parse(task.last_heartbeat_at ?? "");
  const hasBeat = !Number.isNaN(beatMs);
  const quietMs = hasBeat ? Math.max(0, now.getTime() - beatMs) : 0;
  const stale = hasBeat && quietMs > HEARTBEAT_STALE_MS;
  let label: string;
  if (stale) label = `stale - ${minuteLabel(quietMs)} quiet`;
  else if (!hasStart) label = "working";
  else label = `working - ${minuteLabel(runningMs)}`;
  return { label, stale, runningMs };
}

// board switcher: named boards + a current-board pointer, Hermes-shaped. There is no
// cross-board move anywhere - a card lives on exactly one board.

// One registry row (GET /boards payload).
export interface BoardInfo {
  name: string;
  current: boolean;
}

/** true when a registry row names the current board. */
export function isCurrentBoard(b: BoardInfo): boolean {
  return b.current;
}

/** withBoard appends ?board=. */
export function withBoard(path: string, board: string): string {
  const trimmed = (board ?? "").trim();
  if (trimmed === "") return path;
  const join = path.includes("?") ? "&" : "?";
  return `${path}${join}board=${encodeURIComponent(trimmed)}`;
}

/** pickBootBoard chooses the switcher's initial selection from the /boards payload. */
export function pickBootBoard(boards: BoardInfo[]): string {
  if (!Array.isArray(boards) || boards.length === 0) return "";
  const cur = boards.find((b) => b && isCurrentBoard(b));
  return cur ? cur.name : boards[0].name;
}

// the assignee source (0.17.3 make-over) --------------------------- The create. Typing
// stays legal: a card may name a worker the registry no longer lists (or never did),
// and the form must never strand that card. A failed GET degrades to no options - the
// field stays a plain text input - never to a blocked form.

export interface AgentOption {
  value: string;
  label: string;
  hint: string;
}

// agentRowWire is the subset of GET /agents this view reads.
interface AgentRowWire {
  name?: string;
  description?: string;
  continuous?: boolean;
}

/** fetchAgentOptions reads the agent registry. */
export async function fetchAgentOptions(
  fetchImpl: BoardFetch = fetch,
): Promise<AgentOption[]> {
  const res = await fetchImpl("/agents");
  if (!res.ok) throw new Error(`GET /agents: ${res.status}`);
  const rows = (await res.json()) as AgentRowWire[];
  const out: AgentOption[] = [];
  for (const r of rows ?? []) {
    const name = (r?.name ?? "").trim();
    if (name === "") continue;
    out.push({
      value: name,
      label: name,
      hint:
        r.description ||
        (r.continuous
          ? "agent home"
          : "the main agent - in-process, never wakes"),
    });
  }
  return out;
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
 * columns buckets tasks into the canonical column order.
 * Unknown statuses cannot exist (the store validates at the door), but nothing here
 * throws on one regardless - it just lands nowhere and the count mismatch would show.
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

/** step moves one frame along the board for the arrow buttons. */
export function step(status: string, dir: -1 | 1): string | null {
  const i = BOARD_STATUSES.indexOf(status as BoardStatus);
  if (i < 0) return null;
  const j = i + dir;
  if (j < 0 || j >= BOARD_STATUSES.length) return null;
  return BOARD_STATUSES[j];
}

// board removal / rename ------- The board's own surface lives in the panel band.

export const DEFAULT_BOARD_NAME = "main";

/** the create-board POST path (the empty state's only control). */
export const BOARDS_PATH = "/boards";

/** true when a named board may carry the remove control. */
export function canRemoveBoard(name: string): boolean {
  return name !== "";
}

/** the DELETE path for a registry board (name always encoded). */
export function boardDeletePath(name: string): string {
  return `/boards/${encodeURIComponent(name)}`;
}

/** the PATCH path a rename rides (same address as the delete: the board's own row). */
export function boardRenamePath(name: string): string {
  return boardDeletePath(name);
}

// the board dropdown's VERBS --------------------- The band holds title + board. They
// ride the SAME ladder as the boards (one roving tabindex, one keyboard law - no second
// popup grammar), so a verb is an option value that cannot collide with a board name:
// the server's boardNameRE starts every name with [a-z0-9]. The values are exact-match
// only, so a stray `!`-prefixed board name from any other source switches to a board
// and never fires a verb.

/** the dropdown row that starts the inline rename. */
export const BOARD_MENU_RENAME = "!rename";

/** the dropdown row that removes the current board (armed, click again). */
export const BOARD_MENU_REMOVE = "!remove";

/** what a dropdown pick means: the board to switch to, or a verb. */
export type BoardMenuPick =
  | { kind: "board"; name: string }
  | { kind: "rename" }
  | { kind: "remove" };

/** boardMenuPick classifies a BandSelect value. */
export function boardMenuPick(value: string): BoardMenuPick {
  if (value === BOARD_MENU_RENAME) return { kind: "rename" };
  if (value === BOARD_MENU_REMOVE) return { kind: "remove" };
  return { kind: "board", name: value };
}

/** boardMenuOptions is the dropdown ladder: every board, then the two verbs. */
export function boardMenuOptions(
  boards: BoardInfo[],
  armed = false,
): { value: string; label: string; cls?: string }[] {
  return [
    ...boards.map((b) => ({ value: b.name, label: b.name })),
    { value: BOARD_MENU_RENAME, label: "rename board" },
    {
      value: BOARD_MENU_REMOVE,
      label: armed ? "confirm remove" : "remove board",
      cls: armed ? "armed" : undefined,
    },
  ];
}

/** boardCount is the band's lead count. */
export function boardCount(shown: number, total: number, filtering: boolean): string {
  if (filtering) return `${shown}/${total}`;
  return `${total} card${total === 1 ? "" : "s"}`;
}

// search / filter (0.17.3 make-over) ------------------------------- One seat, TWO.

export type SearchScope = "board" | "all";

/** filterTasks is the search itself. */
export function filterTasks(tasks: BoardTask[], query: string): BoardTask[] {
  const q = (query ?? "").trim().toLowerCase();
  if (q === "") return tasks;
  return tasks.filter((t) =>
    `${t.title ?? ""}\n${t.body ?? ""}\n${t.assignee ?? ""}\n${t.id ?? ""}`
      .toLowerCase()
      .includes(q),
  );
}

/** tasksPath is the list read for one scope. */
export function tasksPath(board: string, scope: SearchScope): string {
  if (scope === "all") return "/tasks?status=all&scope=all";
  return withBoard("/tasks?status=all", board);
}

/** boardOf names the board a card belongs to. */
export function boardOf(task: BoardTask, viewed: string): string {
  return (task.board ?? "").trim() || viewed;
}

/** taskDeletePath is the DELETE path for one card (id always encoded). */
export function taskDeletePath(id: string): string {
  return `/tasks/${encodeURIComponent(id)}`;
}

/**
 * reorderAfter moves one task to (status, after a sibling) inside the board's flat.
 * afterId names the card ABOVE the insertion slot ("" = top of the column), the exact
 * pair the server's PATCH after_id takes, so the optimistic board view is exactly what
 * persisted.
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
 * age renders a card's created_at as a compact relative age ("now", "42m", "3h", "5d").
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

// the mounted /boards fetch retry: Board.vue gates the switcher chip on `boards.length
// > 0` (0.17.3 make-over; the pre-make-over gate was `boards.length > 1 || boardName`,
// so the chip is a MORE present control now, never less), and the fetch runs ONCE per
// mount - a fetch that misses under CI suite load leaves no chip for the whole page
// life.

export const BOARD_FETCH_ATTEMPTS = 3;
export const BOARD_FETCH_BACKOFF_MS = 300;

export type BoardFetch = (url: string) => Promise<Response>;

export type FetchBoardsOpts = {
  attempts?: number;
  backoffMs?: number;
  sleep?: (ms: number) => Promise<void>;
};

/**
 * fetchBoardsRetry GETs /boards with a bounded retry and returns the board registry.
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
    }
    if (attempt < attempts - 1) await sleep(backoffMs);
  }
  return null; // final miss: silent, the board still renders
}