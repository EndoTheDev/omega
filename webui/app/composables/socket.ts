// omega webui: the push channel client.

import { ref, type Ref } from "vue";

// SOCKET_TOPICS is the closed topic set this client speaks, in wire order, and the. The
// SERVER's closed set is one wider: `logs` (agent/api/sockets.go) still ships and keeps
// its Go pins, subscription-gated so a page that never asked for it receives no log
// frames. A `logs` frame is now an unknown topic here and drops silently.
export const SOCKET_TOPICS = ["chat", "chips", "canvas", "runs", "health"] as const;
export type SocketTopic = (typeof SOCKET_TOPICS)[number];

export type SocketStatus = "idle" | "connecting" | "open" | "reconnecting" | "closed";

// SocketEnvelope is one pushed frame: {topic, session, event, seq, data}.
export interface SocketEnvelope {
  topic: string;
  session?: string;
  event?: string;
  seq?: number;
  data?: unknown;
}

// ChipFrame mirrors agent/api.ChipFrame.
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

// HealthProviderFact is the /health body's provider reachability fact.
export interface HealthProviderFact {
  reachable?: boolean;
  checked_at?: number;
  detail?: string;
}

// HealthDatabaseFact is the store seam's liveness ping.
export interface HealthDatabaseFact {
  healthy?: boolean;
  detail?: string;
}

// HealthErrorFact is the newest error the server's logger recorded.
export interface HealthErrorFact {
  message?: string;
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

export interface SocketHandlers {
  health?(health: HealthPush): void;
  runs?(session: string, active: boolean): void;
  chips?(session: string, frame: ChipFrame): void;
  /** one canvas panel descriptor (canvas topic; the SSE frame verbatim) */
  canvas?(event: string, payload: unknown): void;
  /** one chat frame - the run's SSE event verbatim, session-scoped */
  chat?(session: string, event: string, payload: unknown): void;
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

// SOCKET_BACKOFF_BASE_MS / SOCKET_BACKOFF_MAX_MS bound the reconnect ladder. capped at
// 15s, with jitter so a restarted lane never gets a synchronized herd of reconnects.
export const SOCKET_BACKOFF_BASE_MS = 500;
export const SOCKET_BACKOFF_MAX_MS = 15_000;

// SOCKET_LIVENESS_MS is the quiet-wire budget. A half-open TCP (a wifi flap, a
// suspended laptop, the devtools offline toggle) never fires `close` - without this the
// client would sit on a corpse and the reconnect catch-up would never run.
export const SOCKET_LIVENESS_MS = 15_000;

// backoffDelay is the delay before reconnection attempt N (0-based).
export function backoffDelay(attempt: number, rand: () => number = Math.random): number {
  const n = Number.isFinite(attempt) && attempt > 0 ? Math.floor(attempt) : 0;
  const base = Math.min(SOCKET_BACKOFF_MAX_MS, SOCKET_BACKOFF_BASE_MS * 2 ** Math.min(n, 16));
  const jitter = base * 0.2 * (rand() * 2 - 1);
  return Math.max(50, Math.round(base + jitter));
}

// normalizeChipFrame guards one chips payload.
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

// dispatchFrame routes one envelope to its typed handler.
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
      // the payload is the SSE frame verbatim: {type: "view_model_delta", panel: {..}}.
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
    default:
      return;
  }
}

// socketURL builds the endpoint from the page's own origin.
export function socketURL(loc: { protocol: string; host: string } | undefined): string {
  if (!loc) return "/ws";
  const scheme = loc.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${loc.host}/ws`;
}

export interface SocketClientOptions {
  url?: string;
  factory?: (url: string) => WebSocket;
  rand?: () => number;
  livenessMs?: number;
}

export interface SocketClient {
  status: Ref<SocketStatus>;
  /** idempotent: a second call while connecting/open is a no-op */
  connect(): void;
  close(): void;
  subscribe(topics: readonly SocketTopic[]): void;
  topics(): SocketTopic[];
}

// createSocketClient owns one page's connection.
export function createSocketClient(
  handlers: SocketHandlers,
  opts: SocketClientOptions = {},
): SocketClient {
  const status = ref<SocketStatus>("idle");
  const rand = opts.rand ?? Math.random;
  const budget = () => opts.livenessMs ?? SOCKET_LIVENESS_MS;
  let topics: SocketTopic[] = [...SOCKET_TOPICS];
  let ws: WebSocket | null = null;
  let attempt = 0;
  let retry: ReturnType<typeof setTimeout> | null = null;
  let liveness: ReturnType<typeof setTimeout> | null = null;
  let lastFrameAt = 0;
  let closedByUser = false;
  let everOpened = false;

  // armLiveness records the last frame and starts the quiet-wire check. Never an
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
        // re-arm for the REMAINDER.
        scheduleLivenessCheck(budget() - silence);
        return;
      }
      try {
        socket.close();
      } catch {
      }
      // Hand recovery to the ladder NOW. The browser's close EVENT is not a promise: on
      // a half-open wire it can be deferred until the network returns (measured: the
      // entire dark window under `ab set offline`) or never arrive at all.
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
    }
  }

  // dropConnection takes the CURRENT socket out of the client's hands and hands. Both
  // routes into it - the browser's close event and the liveness cut - must leave
  // exactly one reconnect pending, never two: `ws = null` plus `schedule`'s own guard
  // is what keeps the ladder single-file, and the stale-socket guard in onclose is what
  // stops a late close from stepping it twice.
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
      // a socket we already dropped is not news.
      if (ws !== socket) return;
      dropConnection();
    };
    socket.onerror = () => {
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
