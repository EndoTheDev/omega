// Board logic for the kanban module UI. Pure and module-owned: the shell never imports
// this - Board.vue rides the composition, so this file and its test live in the module.

// the canonical column order: the exact 9-status hermes grammar
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

export const BOARD_PATH = "/board";

export function isBoardPath(p: string): boolean {
  return p === BOARD_PATH || p === BOARD_PATH + "/";
}

// Lane rails: any lane folds to a thin vertical rail. Two numbers are geometry contracts,
// not taste - the rail stays ~36px (32-40: a dot, a vertical name and a count still fit)
// and the fold eases over ~180ms.
export const LANE_RAIL_PX = 36;
export const LANE_EASE_MS = 180;

export const LANE_TONES: Record<BoardStatus, string> = {
  triage: "#8f8f8f",
  todo: "#cfcfcf",
  scheduled: "#a78bfa",
  ready: "#60a5fa",
  running: "#34d399",
  blocked: "#f87171",
  review: "#fbbf24",
  done: "#6f6f6f",
  archived: "#4a4a4a",
};

export function laneTone(status: string): string {
  return LANE_TONES[status as BoardStatus] ?? LANE_TONES.triage;
}

export type LanePin = "open" | "collapsed";
export type LanePins = Record<string, LanePin>;

/** laneFolded is the ONE fold law: an explicit pin wins, else a drained lane auto-collapses. */
export function laneFolded(status: string, count: number, pins: LanePins): boolean {
  const pin = pins[status];
  if (pin === "open") return false;
  if (pin === "collapsed") return true;
  return count <= 0;
}

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

/** zero is unmounted-measure noise, never narrow. */
export function isNarrowWidth(px: number): boolean {
  return px > 0 && px < NARROW_MAX_PX;
}

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
  // board is the NAME of the board a row lives on: only the all-boards read carries it,
  // and every other route addresses a card by (board, id).
  board?: string;
}

export const PRIORITY_LEVELS = [0, 1, 2, 3] as const;

export function priorityLabel(n: number): string {
  return `P${n}`;
}

export function hasPriorityChip(n: number): boolean {
  return n > 0;
}

export function priorityOptions(): { value: number; label: string }[] {
  return PRIORITY_LEVELS.map((n) => ({
    value: n,
    label: n === 0 ? "P0 (normal)" : priorityLabel(n),
  }));
}

export function bodyPreview(body: string): string {
  const line = (body ?? "")
    .split("\n")
    .map((s) => s.trim())
    .find((s) => s !== "");
  if (line === undefined) return "";
  return line.length > 80 ? line.slice(0, 79) + "…" : line;
}

/** the card text clamp: two lines, title and teaser alike. */
export const CARD_CLAMP_LINES = 2;

// The running heartbeat chip: a running card knows when it started, a beating worker knows
// when it last spoke. Two clocks, deliberately different - the WORKING duration only ever
// grows (started_at), STALENESS only clears when a beat lands (last_heartbeat_at), so a
// card cannot heal itself by waiting.
//
// HEARTBEAT_STALE_MS is the eye's threshold, not the dispatcher's: reclaim waits an hour,
// and two minutes is a couple of missed beats while staying visible across the board.
export const HEARTBEAT_STALE_MS = 120000;

/** the label reads whole minutes, so a 5s tick advances it on the minute; this timer never fetches. */
export const HEARTBEAT_TICK_MS = 5000;

export interface HeartbeatChip {
  label: string;
  stale: boolean;
  runningMs: number;
}

export function minuteLabel(ms: number): string {
  return `${Math.max(0, Math.floor(ms / 60000))}m`;
}

/** null when there is nothing to say; an unparseable start is not a duration to invent (the card wears bare "working"). */
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

// board switcher: named boards + a current-board pointer. There is no cross-board move
// anywhere - a card lives on exactly one board.

export interface BoardInfo {
  name: string;
  current: boolean;
}

export function isCurrentBoard(b: BoardInfo): boolean {
  return b.current;
}

export function withBoard(path: string, board: string): string {
  const trimmed = (board ?? "").trim();
  if (trimmed === "") return path;
  const join = path.includes("?") ? "&" : "?";
  return `${path}${join}board=${encodeURIComponent(trimmed)}`;
}

export function pickBootBoard(boards: BoardInfo[]): string {
  if (!Array.isArray(boards) || boards.length === 0) return "";
  const cur = boards.find((b) => b && isCurrentBoard(b));
  return cur ? cur.name : boards[0].name;
}

// Assignee options: typing stays legal - a card may name a worker the registry no longer
// lists - and a failed GET degrades to no options, never to a blocked form.

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

export interface BoardComment {
  id: number;
  task_id: string;
  author: string;
  body: string;
  created_at: string;
}

export interface BoardEvent {
  id: number;
  task_id: string;
  kind: string;
  detail: string;
  created_at: string;
}

/** unknown statuses cannot exist (the store validates at the door) and must not throw if one does. */
export function columns(tasks: BoardTask[]): BoardTask[][] {
  const by = new Map<string, BoardTask[]>(
    BOARD_STATUSES.map((s) => [s, [] as BoardTask[]]),
  );
  for (const t of tasks) {
    const col = by.get(t.status);
    if (!col) continue;
    col.push(t);
  }
  return BOARD_STATUSES.map((s) => by.get(s) as BoardTask[]);
}

export function step(status: string, dir: -1 | 1): string | null {
  const i = BOARD_STATUSES.indexOf(status as BoardStatus);
  if (i < 0) return null;
  const j = i + dir;
  if (j < 0 || j >= BOARD_STATUSES.length) return null;
  return BOARD_STATUSES[j];
}

export const DEFAULT_BOARD_NAME = "main";

export const BOARDS_PATH = "/boards";

/** an unnamed board has no row to remove; the default board's refusal is the server's. */
export function canRemoveBoard(name: string): boolean {
  return name !== "";
}

export function boardDeletePath(name: string): string {
  return `/boards/${encodeURIComponent(name)}`;
}

/** the PATCH path a rename rides (same address as the delete: the board's own row). */
export function boardRenamePath(name: string): string {
  return boardDeletePath(name);
}

// The dropdown's VERBS ride the SAME ladder as the boards (one roving tabindex, no second
// popup grammar), so a verb cannot collide with a board name: the server's boardNameRE
// starts every name with [a-z0-9]. Exact-match only, so a stray name never fires a verb.

export const BOARD_MENU_RENAME = "!rename";

export const BOARD_MENU_REMOVE = "!remove";

export type BoardMenuPick =
  | { kind: "board"; name: string }
  | { kind: "rename" }
  | { kind: "remove" };

export function boardMenuPick(value: string): BoardMenuPick {
  if (value === BOARD_MENU_RENAME) return { kind: "rename" };
  if (value === BOARD_MENU_REMOVE) return { kind: "remove" };
  return { kind: "board", name: value };
}

export function boardMenuOptions(
  boards: BoardInfo[],
  armed = false,
): { value: string; label: string; cls?: string }[] {
  return [
    ...boards.map((b) => ({ value: b.name, label: b.name })),
    { value: BOARD_MENU_RENAME, label: "rename board" },
    {
      value: BOARD_MENU_REMOVE,
      label: armed ? "confirm delete" : "delete board",
      cls: armed ? "armed" : undefined,
    },
  ];
}

export function boardCount(shown: number, total: number, filtering: boolean): string {
  if (filtering) return `${shown}/${total}`;
  return `${total} card${total === 1 ? "" : "s"}`;
}

export type SearchScope = "board" | "all";

export function filterTasks(tasks: BoardTask[], query: string): BoardTask[] {
  const q = (query ?? "").trim().toLowerCase();
  if (q === "") return tasks;
  return tasks.filter((t) =>
    `${t.title ?? ""}\n${t.body ?? ""}\n${t.assignee ?? ""}\n${t.id ?? ""}`
      .toLowerCase()
      .includes(q),
  );
}

export function tasksPath(board: string, scope: SearchScope): string {
  if (scope === "all") return "/tasks?status=all&scope=all";
  return withBoard("/tasks?status=all", board);
}

/** a row with no board of its own belongs to the board being viewed. */
export function boardOf(task: BoardTask, viewed: string): string {
  return (task.board ?? "").trim() || viewed;
}

export function taskDeletePath(id: string): string {
  return `/tasks/${encodeURIComponent(id)}`;
}

/** afterId names the card ABOVE the insertion slot ("" = top) - the exact pair the server's PATCH after_id takes. */
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

// Board.vue gates the switcher chip on boards.length > 0 and the fetch runs ONCE per mount,
// so a miss under suite load would leave no chip for the page's whole life: bounded retry.

export const BOARD_FETCH_ATTEMPTS = 3;
export const BOARD_FETCH_BACKOFF_MS = 300;

export type BoardFetch = (url: string) => Promise<Response>;

export type FetchBoardsOpts = {
  attempts?: number;
  backoffMs?: number;
  sleep?: (ms: number) => Promise<void>;
};

/** null when the capability is absent or every attempt missed - the board still renders. */
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