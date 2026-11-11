// omega webui: the push channel client (card t_896cf5a8; design of
// record Workshop/omega/websocket-channel.md, rulings 1-4).
//
// ONE websocket per page load (`GET /ws`), every live update a typed
// topic on it. The server buffers and replays nothing: a dropped
// connection is caught up by refetching the visible panes through the
// ordinary GET routes - events are hints, state is truth. `seq` is
// diagnostic only (a narrowed subscription sees jumps), so nothing here
// reads it as a loss signal.
//
// The transport is deliberately dumb: parse the envelope, dispatch by
// topic, reconnect with exponential backoff. The topic payloads are the
// server's shapes (agent/api/sockets.go) and the handlers do the typing.
//
// Pure halves (parseEnvelope, backoffDelay, dispatchFrame,
// normalizeChipFrame) are node-tested: test/socket.test.ts.

import { ref, type Ref } from "vue";

// SOCKET_TOPICS is the closed server-side topic set, in wire order.
export const SOCKET_TOPICS = ["chat", "chips", "canvas", "runs", "health", "logs"] as const;
export type SocketTopic = (typeof SOCKET_TOPICS)[number];

// DEFAULT_SOCKET_TOPICS is what a page asks for on connect. `logs` is
// deliberately absent: the log tail is high-frequency and only a mounted
// logs pane wants it, so the shell adds the topic when such a pane
// mounts and drops it when the last one unmounts (card t_0e7bc0bb). The
// server gates on the subscription itself, so a page that never asked
// receives no log frames at all.
export const DEFAULT_SOCKET_TOPICS = ["chat", "chips", "canvas", "runs", "health"] as const;

// The logs pane's two window seams (module UI imports no shell code, so
// the pane and this client meet on CustomEvents - the same seam the
// kanban board uses for omega-select-board):
//
//   pane -> shell: LOG_SUBSCRIBE_EVENT, detail {on: boolean} - a logs
//                  pane mounted or went away
//   shell -> pane: LOG_STREAM_EVENT, detail {lines: string[]} to append,
//                  or {reset: true} after a reconnect (the pane re-reads
//                  its GET /logs snapshot; nothing is replayed)
export const LOG_SUBSCRIBE_EVENT = "omega-logs-subscribe";
export const LOG_STREAM_EVENT = "omega-log-stream";

export type SocketStatus = "idle" | "connecting" | "open" | "reconnecting" | "closed";

// SocketEnvelope is one pushed frame: {topic, session, event, seq, data}.
// session scopes to a conversation (absent on the global topics), event
// carries the chat topic's SSE event name, data is the payload (SSE bytes
// verbatim on chat/canvas, a typed struct elsewhere).
export interface SocketEnvelope {
  topic: string;
  session?: string;
  event?: string;
  seq?: number;
  data?: unknown;
}

// ChipFrame mirrors agent/api.ChipFrame: the pending chip's live tail
// (fill false, text growing) and its fill when the call settles.
export interface ChipFrame {
  call_id: string;
  tool?: string;
  fill?: boolean;
  text?: string;
  content?: string;
  truncated?: boolean;
  is_error?: boolean;
  timeout?: boolean;
  elapsed_ms?: number;
}

// HealthProviderFact is the /health body's provider reachability fact
// (card t_5e2f3095): the server's cached model-endpoint probe. Absent =
// no probe has landed yet (or an older server) - the popover renders
// "not reported" rather than a guess.
export interface HealthProviderFact {
  reachable?: boolean;
  /** unix seconds of the probe that produced this verdict */
  checked_at?: number;
  /** the probe's error text when it failed */
  detail?: string;
}

// HealthDatabaseFact is the store seam's liveness ping: present = the
// server can answer for its database, absent = it has no source for the
// fact (no store mounted, older build).
export interface HealthDatabaseFact {
  healthy?: boolean;
  detail?: string;
}

// HealthErrorFact is the newest error the server's logger recorded.
// An object with an EMPTY message is a sourced "none since boot" - only
// an absent object means "not reported".
export interface HealthErrorFact {
  message?: string;
  /** unix seconds of the entry */
  at?: number;
}

// HealthPush is the /health body the health topic pushes.
export interface HealthPush {
  status?: string;
  uptime_seconds?: number;
  version?: string;
  provider?: HealthProviderFact;
  database?: HealthDatabaseFact;
  last_error?: HealthErrorFact;
}

// LogLinesPush is the logs topic's payload: the lines appended to the
// server's log file since the previous frame - a delta, never the tail
// (the pane's first read stays GET /logs).
export interface LogLinesPush {
  lines?: string[];
}

export interface SocketHandlers {
  health?(health: HealthPush): void;
  /** one session's live-run truth moved (the runs topic) */
  runs?(session: string, active: boolean): void;
  /** one chip frame: live tail while the call runs, fill when it settles */
  chips?(session: string, frame: ChipFrame): void;
  /** one canvas panel descriptor (canvas topic; the SSE frame verbatim) */
  canvas?(event: string, payload: unknown): void;
  /** one chat frame - the run's SSE event verbatim, session-scoped */
  chat?(session: string, event: string, payload: unknown): void;
  /** the log lines appended since the last frame (never a snapshot) */
  logs?(lines: string[]): void;
  /** the connection opened; reconnect is true iff it was open before */
  open?(info: { reconnect: boolean }): void;
  close?(): void;
}

// parseEnvelope guards one pushed message. A malformed frame (bad JSON, a
// non-object, a missing topic) degrades to null - the caller skips it
// instead of crashing the socket loop.
export function parseEnvelope(raw: unknown): SocketEnvelope | null {
  let value: unknown = raw;
  if (typeof raw === "string") {
    if (!raw) return null;
    try {
      value = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const e = value as Record<string, unknown>;
  if (typeof e.topic !== "string" || !e.topic) return null;
  const out: SocketEnvelope = { topic: e.topic };
  if (typeof e.session === "string" && e.session) out.session = e.session;
  if (typeof e.event === "string" && e.event) out.event = e.event;
  if (typeof e.seq === "number" && Number.isFinite(e.seq)) out.seq = e.seq;
  out.data = e.data;
  return out;
}

// SOCKET_BACKOFF_BASE_MS / SOCKET_BACKOFF_MAX_MS bound the reconnect
// ladder: 0.5s, 1s, 2s ... capped at 15s, with jitter so a restarted
// lane never gets a synchronized herd of reconnects.
export const SOCKET_BACKOFF_BASE_MS = 500;
export const SOCKET_BACKOFF_MAX_MS = 15_000;

// SOCKET_LIVENESS_MS is the quiet-wire budget: the server pushes the
// health topic every 5s, so 15s of silence means the connection is dead
// even though no close event arrived. A half-open TCP (a wifi flap, a
// suspended laptop, the devtools offline toggle) never fires `close` -
// without this the client would sit on a corpse and the reconnect
// catch-up would never run.
export const SOCKET_LIVENESS_MS = 15_000;

// backoffDelay is the delay before reconnection attempt N (0-based):
// exponential, capped, +/-20% jitter. Pure (rand injected) so the node
// test pins the ladder and the cap.
export function backoffDelay(attempt: number, rand: () => number = Math.random): number {
  const n = Number.isFinite(attempt) && attempt > 0 ? Math.floor(attempt) : 0;
  const base = Math.min(SOCKET_BACKOFF_MAX_MS, SOCKET_BACKOFF_BASE_MS * 2 ** Math.min(n, 16));
  const jitter = base * 0.2 * (rand() * 2 - 1);
  return Math.max(50, Math.round(base + jitter));
}

// normalizeChipFrame guards one chips payload: a frame without a call id
// has nothing to attach to and drops.
export function normalizeChipFrame(data: unknown): ChipFrame | null {
  if (!data || typeof data !== "object" || Array.isArray(data)) return null;
  const f = data as Record<string, unknown>;
  if (typeof f.call_id !== "string" || !f.call_id) return null;
  const out: ChipFrame = { call_id: f.call_id };
  if (typeof f.tool === "string" && f.tool) out.tool = f.tool;
  if (f.fill === true) out.fill = true;
  if (typeof f.text === "string" && f.text) out.text = f.text;
  if (typeof f.content === "string" && f.content) out.content = f.content;
  if (f.truncated === true) out.truncated = true;
  if (f.is_error === true) out.is_error = true;
  if (f.timeout === true) out.timeout = true;
  if (typeof f.elapsed_ms === "number" && Number.isFinite(f.elapsed_ms)) out.elapsed_ms = f.elapsed_ms;
  return out;
}

// dispatchFrame routes one envelope to its typed handler. Unknown topics
// are ignored (the set is closed server-side). Pure: every state change
// lives in the handlers.
export function dispatchFrame(env: SocketEnvelope, handlers: SocketHandlers): void {
  switch (env.topic) {
    case "health": {
      const data = env.data;
      if (!data || typeof data !== "object" || Array.isArray(data)) return;
      handlers.health?.(data as HealthPush);
      return;
    }
    case "runs": {
      const data = env.data;
      if (!env.session || !data || typeof data !== "object" || Array.isArray(data)) return;
      handlers.runs?.(env.session, (data as Record<string, unknown>).active === true);
      return;
    }
    case "chips": {
      const frame = normalizeChipFrame(env.data);
      if (frame) handlers.chips?.(env.session ?? "", frame);
      return;
    }
    case "canvas": {
      // the payload is the SSE frame verbatim: {type: "view_model_delta",
      // panel: {...}}. The event name rides the envelope too - fall back
      // to the payload's own type when a producer omits it.
      const data = env.data as Record<string, unknown> | undefined;
      const event =
        env.event ||
        (data && typeof data === "object" && typeof data.type === "string" ? data.type : "view_model_delta");
      handlers.canvas?.(event, env.data);
      return;
    }
    case "chat": {
      const event = env.event ?? "";
      handlers.chat?.(env.session ?? "", event, env.data);
      return;
    }
    case "logs": {
      // the delta: an array of raw lines appended to the log file. A
      // frame with no usable lines is a no-op, never a crash.
      const data = env.data as LogLinesPush | undefined;
      const lines = Array.isArray(data?.lines)
        ? data.lines.filter((l): l is string => typeof l === "string")
        : [];
      if (lines.length) handlers.logs?.(lines);
      return;
    }
    default:
      return;
  }
}

// socketURL builds the endpoint from the page's own origin - the same
// host the SPA was served from (loopback bind; the upgrader refuses a
// foreign Origin anyway).
export function socketURL(loc: { protocol: string; host: string } | undefined): string {
  if (!loc) return "/ws";
  const scheme = loc.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${loc.host}/ws`;
}

export interface SocketClientOptions {
  /** override the endpoint (tests, a proxied lane) */
  url?: string;
  /** websocket constructor (tests inject a fake) */
  factory?: (url: string) => WebSocket;
  /** jitter source (tests pin it) */
  rand?: () => number;
  /** quiet-wire budget (tests shorten it) */
  livenessMs?: number;
}

export interface SocketClient {
  /** connecting | open | reconnecting | closed */
  status: Ref<SocketStatus>;
  /** idempotent: a second call while connecting/open is a no-op */
  connect(): void;
  /** stops the ladder - the page is going away */
  close(): void;
  /** replace this connection's topic set; an empty list keeps "all" */
  subscribe(topics: readonly SocketTopic[]): void;
  /** the topic set the client asks for on every (re)connect */
  topics(): SocketTopic[];
}

// createSocketClient owns one page's connection. One per page load: the
// app mounts it on mount, closes it on unmount.
export function createSocketClient(
  handlers: SocketHandlers,
  opts: SocketClientOptions = {},
): SocketClient {
  const status = ref<SocketStatus>("idle");
  const rand = opts.rand ?? Math.random;
  const budget = () => opts.livenessMs ?? SOCKET_LIVENESS_MS;
  let topics: SocketTopic[] = [...DEFAULT_SOCKET_TOPICS];
  let ws: WebSocket | null = null;
  let attempt = 0;
  let retry: ReturnType<typeof setTimeout> | null = null;
  let liveness: ReturnType<typeof setTimeout> | null = null;
  let lastFrameAt = 0;
  let closedByUser = false;
  let everOpened = false;

  // armLiveness records the last frame and starts the quiet-wire check.
  // ONE timer at a time (a chat run frames hundreds of deltas a second -
  // clearing and re-creating a timer per frame would be its own
  // problem); the check re-schedules itself while frames keep landing
  // and cuts the socket once the wire has been quiet for the budget.
  // Expiry means the connection is dead WITHOUT a close event, so the
  // socket is closed by hand and the ladder rebuilds it. Never an
  // interval.
  function armLiveness(): void {
    lastFrameAt = Date.now();
    if (liveness !== null) return;
    scheduleLivenessCheck();
  }

  function scheduleLivenessCheck(delayMs?: number): void {
    liveness = setTimeout(() => {
      liveness = null;
      const socket = ws;
      if (!socket) return;
      const silence = Date.now() - lastFrameAt;
      if (silence < budget()) {
        // re-arm for the REMAINDER: the budget is a deadline measured from
        // the LAST FRAME, so a fresh full-budget timer lets a dead wire sit
        // undetected for up to 2x the budget (the check can land one tick
        // before the last frame, then wait another whole budget). At the
        // shipped 15s that is a 30s discovery window - longer than a
        // reconnect catch-up is given, so the half-open socket outlives the
        // outage and revives with the network instead of reconnecting
        // (card t_71f28526).
        scheduleLivenessCheck(budget() - silence);
        return;
      }
      try {
        socket.close();
      } catch {
        // already dead: the teardown below still owns the recovery
      }
      // Hand recovery to the ladder NOW. The browser's close EVENT is not a
      // promise: on a half-open wire it can be deferred until the network
      // returns (measured: the entire dark window under `ab set offline`) or
      // never arrive at all. Waiting for it leaves the client dark with no
      // timer armed - no reconnect, no onopen, no catch-up refetch, and the
      // pane stays stale (card t_71f28526). A late close is ignored by the
      // stale-socket guard in onclose.
      if (ws === socket) dropConnection();
    }, delayMs ?? budget());
  }

  function disarmLiveness(): void {
    if (liveness !== null) clearTimeout(liveness);
    liveness = null;
  }

  function factory(url: string): WebSocket {
    if (opts.factory) return opts.factory(url);
    return new WebSocket(url);
  }

  function endpoint(): string {
    if (opts.url) return opts.url;
    if (typeof window === "undefined") return "/ws";
    return socketURL(window.location);
  }

  function sendSubscribe(): void {
    if (!ws || ws.readyState !== 1) return;
    try {
      ws.send(JSON.stringify({ type: "subscribe", topics }));
    } catch {
      // a send on a dying socket is the close handler's business
    }
  }

  // dropConnection takes the CURRENT socket out of the client's hands and
  // hands recovery to the ladder. Both routes into it - the browser's close
  // event and the liveness cut - must leave exactly one reconnect pending,
  // never two: `ws = null` plus `schedule()`'s own guard is what keeps the
  // ladder single-file, and the stale-socket guard in onclose is what stops
  // a late close from stepping it twice.
  function dropConnection(): void {
    ws = null;
    disarmLiveness();
    if (closedByUser) {
      status.value = "closed";
      return;
    }
    status.value = "reconnecting";
    handlers.close?.();
    schedule();
  }

  function connect(): void {
    if (closedByUser) return;
    if (ws && (ws.readyState === 0 || ws.readyState === 1)) return;
    status.value = everOpened ? "reconnecting" : "connecting";
    const socket = factory(endpoint());
    ws = socket;
    socket.onopen = () => {
      attempt = 0;
      status.value = "open";
      armLiveness();
      sendSubscribe();
      handlers.open?.({ reconnect: everOpened });
      everOpened = true;
    };
    socket.onmessage = (ev: MessageEvent) => {
      armLiveness();
      const env = parseEnvelope(ev.data);
      if (env) dispatchFrame(env, handlers);
    };
    socket.onclose = () => {
      // a socket we already dropped is not news: its close can land after the
      // ladder has replaced it (the liveness cut does not wait for it), and
      // nulling the live socket here would strand the connection
      if (ws !== socket) return;
      dropConnection();
    };
    socket.onerror = () => {
      // the close event always follows; nothing to do here
    };
  }

  function schedule(): void {
    if (closedByUser || retry !== null) return;
    const delay = backoffDelay(attempt, rand);
    attempt += 1;
    retry = setTimeout(() => {
      retry = null;
      connect();
    }, delay);
  }

  function close(): void {
    closedByUser = true;
    disarmLiveness();
    if (retry !== null) {
      clearTimeout(retry);
      retry = null;
    }
    const socket = ws;
    ws = null;
    if (socket) {
      try {
        socket.close();
      } catch {
        // already dead
      }
    }
    status.value = "closed";
  }

  function subscribe(next: readonly SocketTopic[]): void {
    const known = new Set<string>(SOCKET_TOPICS);
    const picked = next.filter((t): t is SocketTopic => known.has(t));
    if (!picked.length) return; // empty keeps the current set (server law)
    topics = picked;
    sendSubscribe();
  }

  return { status, connect, close, subscribe, topics: () => [...topics] };
}
