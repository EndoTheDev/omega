// omega webui: the observability tab's PURE half (card t_e703180e, the
// face side of the observability arc). The tab renders two reads of the
// observability capsule (harness/observability/routes.go):
//
//	GET /observability/events?session=&level=&limit=
//	  -> {events: Event[], returned, buffered, capacity}
//	     Event = {seq, time, session?, type, level, tool?, turn?,
//	              duration_ms?, summary, error?}
//	GET /observability/logs?lines=
//	  -> {path, lines: string[]}   (404 when there is no log file)
//
// The ring is bounded at 1024 and one page is capped at PageMax=1000,
// so a single unfiltered page IS the whole live history - that is why
// the errors-only toggle filters CLIENT-side (an instant chip, the
// severity-chips precedent on the Logs strip) while the SESSION scope is
// a server parameter (the ring's Page() selects the newest N of that
// session; filtering the newest N of everything would starve a quiet
// session).
//
// Everything here is DOM-free so the node suite can pin it: the wire
// guards, the filter, the render cap and the two formatters.
// webui/test/observability.test.ts is the pin.

/** the capsule's structured stream read */
export const EVENTS_PATH = "/observability/events";
/** the capsule's raw tail read */
export const LOGS_PATH = "/observability/logs";

/** one page: the capsule's PageMax ceiling, i.e. essentially the ring */
export const EVENTS_LIMIT = 1000;
/** rows the tab paints: newest in the ring, oldest dropped from the DOM */
export const RENDER_CAP = 300;
/** the tail's line count (the route defaults to the same 200) */
export const TAIL_LINES = 200;
/** the bounded refresh while the tab is mounted (the Logs strip's
 * 2.5s poll is the sibling precedent; the ring is quieter than a
 * file that every request writes to) */
export const REFRESH_MS = 5000;

/** One ring record, exactly as the capsule serializes it. `level` is
 * DERIVED server-side (never configured): info, warn or error. */
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

/** normalizeEvent guards one record: a row without a type has nothing to
 * show and drops; every other field degrades to its empty form. A
 * malformed payload never crashes the pane (the shell's wire law). */
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

/** parseEventsPayload reads the events body: unknown records drop, a
 * missing ring size reads 0 rather than throwing. */
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

/** parseLogsPayload reads the tail body: lines stay VERBATIM (the tail
 * is the process's own words - no reformatting, no filtering). */
export function parseLogsPayload(body: unknown): LogsPayload {
  const b = (body && typeof body === "object" && !Array.isArray(body)
    ? body
    : {}) as Record<string, unknown>;
  const lines = Array.isArray(b.lines) ? b.lines.filter((l): l is string => typeof l === "string") : [];
  return { path: str(b.path), lines };
}

/** eventsQuery builds the read's query string. `session` empty = every
 * session (the capsule's own convention); the errors-only toggle is NOT
 * a server parameter (see the header: it filters the page in the tab). */
export function eventsQuery(session: string, limit: number = EVENTS_LIMIT): string {
  const qs = new URLSearchParams();
  if (session) qs.set("session", session);
  qs.set("limit", String(limit));
  return qs.toString();
}

/** logsQuery is the tail's query string. */
export function logsQuery(lines: number = TAIL_LINES): string {
  return `lines=${lines}`;
}

/** describeReadError turns an HTTP status into the pane's sentence. A
 * 404 on either route is a STATE, not a failure: the capsule is absent
 * (disabled in Modules, so its routes answer at the module mux) or the
 * logger has no file (logging off / a NopLogger - the capsule's own
 * documented degradation). */
export function describeReadError(path: string, status: number): string {
  if (status === 404) {
    return path === EVENTS_PATH
      ? "observability module is not loaded (404) - enable it in Modules"
      : "no log file (logging disabled, or a logger with no path)";
  }
  return `GET ${path}: ${status}`;
}

/** isErrorEvent is the errors-only test: the derived level is the one
 * truth (a record can carry an `error` and still read as error - the
 * server derives it, the tab never re-derives). */
export function isErrorEvent(e: ObservatoryEvent): boolean {
  return e.level === "error";
}

/** visibleEvents selects what the tab paints, NEWEST FIRST: the newest
 * `cap` records of the page (oldest-first on the wire), reversed so the
 * row that just happened is at the top of the pane instead of behind a
 * scroll. errorsOnly keeps the error rows only - the errors-only chip. */
export function visibleEvents(
  events: readonly ObservatoryEvent[],
  errorsOnly: boolean,
  cap: number = RENDER_CAP,
): ObservatoryEvent[] {
  const picked = errorsOnly ? events.filter(isErrorEvent) : [...events];
  const bound = cap > 0 ? cap : picked.length;
  return picked.slice(Math.max(0, picked.length - bound)).reverse();
}

/** errorCount feeds the meta line's "N errors of M" reading. */
export function errorCount(events: readonly ObservatoryEvent[]): number {
  return events.filter(isErrorEvent).length;
}

/** formatEventTime renders the record's RFC3339Nano stamp as the row's
 * clock reading (local HH:MM:SS - the same clock the raw tail prints).
 * An unparsable stamp falls through verbatim: showing the server's own
 * bytes beats showing an empty cell. */
export function formatEventTime(iso: string): string {
  if (!iso) return "-";
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const pad = (n: number): string => String(n).padStart(2, "0");
  return `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`;
}

/** formatDuration renders the derived pair duration: sub-second in ms,
 * beyond it in seconds with one decimal. A record whose pair was missed
 * carries no duration at all - an em dash, never a fabricated zero. */
export function formatDuration(ms?: number): string {
  if (ms === undefined || ms < 0) return "-";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

/** shortSession is the row's session attribution in the all-sessions
 * scope: the id's first 8 characters (session ids are long hex; the
 * transcript surfaces shorten them the same way). */
export function shortSession(id?: string): string {
  if (!id) return "-";
  return id.length > 8 ? id.slice(0, 8) : id;
}
