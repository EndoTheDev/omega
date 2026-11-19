// omega webui: the observability tab's PURE half. Two reads of the observability capsule
// (harness/observability/routes.go):
//   GET /observability/events?session=&level=&limit= -> {events, buffered, capacity}
//   GET /observability/logs?lines= -> {path, lines} (404 when there is no log file)
//
// The ring is bounded at 1024 and one page caps at PageMax=1000, so an unfiltered page IS
// the whole live history - hence errors-only filters CLIENT-side while the SESSION scope
// is a server parameter (filtering the newest N of everything would starve a quiet
// session). DOM-free so the node suite can pin it (webui/test/observability.test.ts).

export const EVENTS_PATH = "/observability/events";
export const LOGS_PATH = "/observability/logs";

/** one page: the capsule's PageMax ceiling, i.e. essentially the ring */
export const EVENTS_LIMIT = 1000;
/** rows the tab paints: newest in the ring, oldest dropped from the DOM */
export const RENDER_CAP = 300;
/** the tail's line count (the route defaults to the same 200) */
export const TAIL_LINES = 200;
/** the bounded refresh while the tab is mounted: one 5s read, never a poll per record */
export const REFRESH_MS = 5000;

/** one ring record, exactly as the capsule serializes it; `level` is DERIVED server-side (info, warn, error). */
export interface ObservatoryEvent {
  seq: number;
  time: string;
  session?: string;
  type: string;
  level: string;
  tool?: string;
  turn?: number;
  duration_ms?: number;
  summary: string;
  error?: string;
}

export interface EventsPayload {
  events: ObservatoryEvent[];
  buffered: number;
  capacity: number;
}

export interface LogsPayload {
  path: string;
  lines: string[];
}

const str = (v: unknown): string => (typeof v === "string" ? v : "");
const num = (v: unknown): number | undefined =>
  typeof v === "number" && Number.isFinite(v) ? v : undefined;

/** a malformed payload never crashes the pane: null, and the read drops the record. */
export function normalizeEvent(raw: unknown): ObservatoryEvent | null {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const e = raw as Record<string, unknown>;
  const type = str(e.type);
  if (!type) return null;
  const out: ObservatoryEvent = {
    seq: num(e.seq) ?? 0,
    time: str(e.time),
    type,
    level: str(e.level) || "info",
    summary: str(e.summary),
  };
  const session = str(e.session);
  if (session) out.session = session;
  const tool = str(e.tool);
  if (tool) out.tool = tool;
  const turn = num(e.turn);
  if (turn !== undefined) out.turn = turn;
  const duration = num(e.duration_ms);
  if (duration !== undefined) out.duration_ms = duration;
  const error = str(e.error);
  if (error) out.error = error;
  return out;
}

export function parseEventsPayload(body: unknown): EventsPayload {
  const b = (body && typeof body === "object" && !Array.isArray(body)
    ? body
    : {}) as Record<string, unknown>;
  const raw = Array.isArray(b.events) ? b.events : [];
  const events: ObservatoryEvent[] = [];
  for (const r of raw) {
    const e = normalizeEvent(r);
    if (e) events.push(e);
  }
  return {
    events,
    buffered: num(b.buffered) ?? events.length,
    capacity: num(b.capacity) ?? 0,
  };
}

export function parseLogsPayload(body: unknown): LogsPayload {
  const b = (body && typeof body === "object" && !Array.isArray(body)
    ? body
    : {}) as Record<string, unknown>;
  const lines = Array.isArray(b.lines) ? b.lines.filter((l): l is string => typeof l === "string") : [];
  return { path: str(b.path), lines };
}

export function eventsQuery(session: string, limit: number = EVENTS_LIMIT): string {
  const qs = new URLSearchParams();
  if (session) qs.set("session", session);
  qs.set("limit", String(limit));
  return qs.toString();
}

export function logsQuery(lines: number = TAIL_LINES): string {
  return `lines=${lines}`;
}

export function describeReadError(path: string, status: number): string {
  if (status === 404) {
    return path === EVENTS_PATH
      ? "observability module is not loaded (404) - enable it in Modules"
      : "no log file (logging disabled, or a logger with no path)";
  }
  return `GET ${path}: ${status}`;
}

/** the derived level is the one truth: a record with `error` text still reads by its level, never re-derived here. */
export function isErrorEvent(e: ObservatoryEvent): boolean {
  return e.level === "error";
}

/** visibleEvents selects what the tab paints, NEWEST FIRST. */
export function visibleEvents(
  events: readonly ObservatoryEvent[],
  errorsOnly: boolean,
  cap: number = RENDER_CAP,
): ObservatoryEvent[] {
  const picked = errorsOnly ? events.filter(isErrorEvent) : [...events];
  const bound = cap > 0 ? cap : picked.length;
  return picked.slice(Math.max(0, picked.length - bound)).reverse();
}

export function errorCount(events: readonly ObservatoryEvent[]): number {
  return events.filter(isErrorEvent).length;
}

/** the row's clock reading; an unparseable stamp renders verbatim rather than "Invalid Date". */
export function formatEventTime(iso: string): string {
  if (!iso) return "-";
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const pad = (n: number): string => String(n).padStart(2, "0");
  return `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`;
}

/** a missed pair carries no duration at all - an em dash, never a fabricated zero. */
export function formatDuration(ms?: number): string {
  if (ms === undefined || ms < 0) return "-";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function shortSession(id?: string): string {
  if (!id) return "-";
  return id.length > 8 ? id.slice(0, 8) : id;
}
