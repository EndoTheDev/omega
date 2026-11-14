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
 * laneFolded is the ONE fold law. A drained lane folds itself, a lane
 * holding cards stays open - and a CLICK outranks both, because since
 * the 0.17.3 make-over a collapsed lane expands on click whether it
 * holds cards or not (the empty lane included: the rail carries the same
 * + the header does, so an empty lane is a place to look, not a dead
 * strip). The pin is the click's record; absent = no override.
 */
export function laneFolded(status: string, count: number, pins: LanePins): boolean {
  const pin = pins[status];
  if (pin === "open") return false;
  if (pin === "collapsed") return true;
  return count <= 0; // a drained lane collapses - the auto-collapse law
}

/**
 * toggleLanePin flips one lane between its rail and its column. The flip
 * reads the lane's CURRENT shape (law: a click on a collapsed lane
 * expands it), so an EMPTY lane - folded by default - opens on the same
 * click and keeps its open pin until the user folds it again. This is
 * the 0.17.3 change: reality (count 0) no longer vetoes the click, so
 * there is nothing to reconcile away later either.
 */
export function toggleLanePin(
  status: string,
  count: number,
  pins: LanePins,
): LanePins {
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

// last_heartbeat_at rides only when the store stamps it (the Hermes
// board does; omega's own store has no heartbeat column yet, so its
// rows end at started_at). Optional on purpose: the card's chip reads
// the field when the wire grows it, and no view has to know which
// store answered.
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
  // board is the NAME of the board a row lives on. Only the all-boards
  // read (GET /tasks?scope=all, the view's all-boards search scope)
  // carries it: every other route addresses a card by (board, id), so a
  // mixed list must say which board a row belongs to. Empty/absent on a
  // board-scoped read.
  board?: string;
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

// --- card polish (card t_65ea61a1): the card is a reading surface, not
// a wall of text. The Hermes grammar is a 2-line title, a clamped body
// teaser, the assignee as a chip and ONE meta row. The clamp is one
// number fed to CSS as a custom property (the same shape as the lane
// ease): the constant drives the rendered line count instead of
// agreeing with it by luck.

/** the card text clamp: two lines, title and teaser alike. */
export const CARD_CLAMP_LINES = 2;

// --- the running heartbeat chip (card t_65ea61a1, Endo ruling
// 2026-11-12: "the running card dont have a heartbeat"). The store kept
// the data the eye never showed: a running card knows when it started,
// and a beating worker knows when it last spoke. The chip says both.
//
// Two clocks, deliberately different:
//   - the WORKING duration only ever grows (from started_at);
//   - STALENESS only comes back when a beat lands (from
//     last_heartbeat_at) - a card cannot heal itself by waiting.
//
// HEARTBEAT_STALE_MS is the eye's threshold, not the dispatcher's: a
// reclaim waits an hour, and an hour of a card claiming to work while
// nothing beats is exactly the lie this chip exists to kill. Two
// minutes is a couple of missed beats of a worker that should tick
// every few seconds, and short enough to see across the board.
export const HEARTBEAT_STALE_MS = 120000;

/** the chip's clock cadence: the label reads whole minutes, so a 5s
 * tick advances it on the minute without turning the board into a
 * polling surface (this timer never fetches). */
export const HEARTBEAT_TICK_MS = 5000;

// One running chip's rendered state.
export interface HeartbeatChip {
  /** the face: "working - 3m" | "working" | "stale - 4m quiet" */
  label: string;
  /** true once the heartbeat has aged past HEARTBEAT_STALE_MS */
  stale: boolean;
  /** whole ms the card has been running (0 when the row carries no start) */
  runningMs: number;
}

/** whole minutes, floored and never negative - the chip's own unit. */
export function minuteLabel(ms: number): string {
  return `${Math.max(0, Math.floor(ms / 60000))}m`;
}

/**
 * heartbeatChip is the chip for one card, or null when there is nothing
 * to say: only a RUNNING card beats. A start it cannot parse is not a
 * duration to invent, so that card wears the bare "working" face.
 *
 * Staleness needs an AGED stamp. A row with no last_heartbeat_at is not
 * stale - silence is not evidence, and no wire field at all is not the
 * same claim as a field that stopped moving.
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

// --- the assignee source (0.17.3 make-over) ---------------------------
// The create form's assignee control is a COMBOBOX fed by the SAME source
// the cron editor's agent field reads: GET /agents (the roster inventory -
// the main profile first, then every agents/<name> home, named `default`
// for the home root). The fetcher is COPIED here rather than imported: a
// module UI owns its own copy and imports no other module's code.
//
// Typing stays legal: a card may name a worker the registry no longer
// lists (or never did), and the form must never strand that card. A failed
// GET degrades to no options - the field stays a plain text input - never
// to a blocked form.

export interface AgentOption {
  value: string;
  label: string;
  hint: string;
}

// agentRowWire is the subset of GET /agents this view reads (the roster
// row shape the cron editor reads too).
interface AgentRowWire {
  name?: string;
  description?: string;
  continuous?: boolean;
}

/**
 * fetchAgentOptions reads the agent registry. The fetch is a parameter so
 * the node suite can stub it (the same shape fetchBoardsRetry uses).
 */
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

// --- board removal / rename (card t_95c5527f; 0.17.3 make-over) -------
// The board's own surface lives in the panel band: the selector, the
// search seat, and the two destructive ops. Since 0.17.3 NOTHING is
// reserved - main and the current board are removable, and a home may end
// on zero boards (the panel then renders its create-board empty state).
// The server is still the law about WHEN: a board holding a live card
// refuses, naming the count, and the refusal text surfaces verbatim.

export const DEFAULT_BOARD_NAME = "main";

/** the create-board POST path (the empty state's only control). */
export const BOARDS_PATH = "/boards";

/** true when a named board may carry the remove control: any board the
 * registry actually named. An empty selection has nothing to remove. */
export function canRemoveBoard(name: string): boolean {
  return name !== "";
}

/** the DELETE path for a registry board (name always encoded). */
export function boardDeletePath(name: string): string {
  return `/boards/${encodeURIComponent(name)}`;
}

/** the PATCH path a rename rides (same address as the delete: the board's
 * own row). */
export function boardRenamePath(name: string): string {
  return boardDeletePath(name);
}

// --- search / filter (0.17.3 make-over) -------------------------------
// One seat, TWO scopes: the board you are looking at, and every board.
// The scope is a server read (?scope=all), not a client union of N
// boards' lists - the store joins once and stamps each row with its
// board, so a mixed result can still address a card (board, id).

export type SearchScope = "board" | "all";

/**
 * filterTasks is the search itself: a case-insensitive substring over the
 * fields a reader can see or type - title, body, assignee and card id.
 * An empty query is every card (the seat is idle, not a filter).
 */
export function filterTasks(tasks: BoardTask[], query: string): BoardTask[] {
  const q = (query ?? "").trim().toLowerCase();
  if (q === "") return tasks;
  return tasks.filter((t) =>
    `${t.title ?? ""}\n${t.body ?? ""}\n${t.assignee ?? ""}\n${t.id ?? ""}`
      .toLowerCase()
      .includes(q),
  );
}

/**
 * tasksPath is the list read for one scope: the current board (the same
 * URL the grid has always used) or every board at once. status=all rides
 * both: the grid carries the archived lane too.
 */
export function tasksPath(board: string, scope: SearchScope): string {
  if (scope === "all") return "/tasks?status=all&scope=all";
  return withBoard("/tasks?status=all", board);
}

/**
 * boardOf names the board a card belongs to: the row's own board when the
 * all-boards read stamped one, else the board being viewed. Every drawer
 * and mutation call addresses a card through this.
 */
export function boardOf(task: BoardTask, viewed: string): string {
  return (task.board ?? "").trim() || viewed;
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