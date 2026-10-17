import MarkdownIt from "markdown-it";

// omega webui: typed client for the http_channel API.
// Endpoints verified live 2026-10-13 against extensions/http_channel.
// SSE events: agent_start, turn_start, response_chunk, thinking_chunk,
// tool_call, stream_end, assistant_message, turn_end, agent_end, error.

export interface OmegaMessage {
  role: "user" | "assistant";
  content: string;
}

export interface Session {
  id: string;
  label?: string;
  created_at: string;
  updated_at: string;
  message_count?: number;
  tool_count?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  model?: string;
  pinned?: boolean;
}

export interface SessionDetail {
  session: Session;
  messages:
    | {
        role?: string;
        content?: string;
        thinking?: string;
        is_error?: boolean;
        tool_call_id?: string;
        // store-level write time injected by the history endpoint; falls
        // back to the payload timestamp for anything older that lacks it
        created_at?: string;
        timestamp?: string;
        tool_calls?: {
          id?: string;
          name?: string;
          arguments?: Record<string, unknown>;
        }[];
      }[]
    | null;
}

export interface ModelsInfo {
  current: string;
  models: string[];
  context_window?: number;
}

// StreamEvents the chat renderer needs, normalized from SSE payloads.
export interface StreamHandlers {
  onThinking: (content: string) => void;
  onResponse: (content: string) => void;
  onToolCall: (tool: { name: string; status: string; output?: string }) => void;
  onToolResult: (result: { content: string; isError: boolean }) => void;
  onStatus: (status: "reasoning" | "responding" | "tools") => void;
  onUsage: (usage: {
    promptTokens: number;
    completionTokens: number;
    cachedTokens: number;
  }) => void;
}

// parseFrame extracts the JSON payload from one SSE frame
// ("event: <name>\ndata: <json>\n\n"). Returns null for frames
// without parseable data - the caller skips those.
export function parseFrame(frame: string): unknown | null {
  const dataLine = frame.split("\n").find((l) => l.startsWith("data: "));
  if (!dataLine) return null;
  try {
    return JSON.parse(dataLine.slice(6));
  } catch {
    return null;
  }
}

// handlePayload dispatches a parsed SSE payload to the stream handlers.
// Pure: all state lives in the state object. THROWS on error payloads -
// streamChat lets the throw propagate so a mid-stream agent error lands
// in the chat's error bubble instead of being swallowed. sawEnd records
// the terminal agent_end event (transport-death detection).
export function handlePayload(
  payload: unknown,
  handlers: StreamHandlers,
  state: { seenResponse: boolean; sawEnd?: boolean },
): void {
  const p = payload as Record<string, unknown>;
  switch (p.type) {
    case "thinking_chunk":
      if (p.content) handlers.onThinking(p.content as string);
      break;
    case "response_chunk":
      if (!state.seenResponse) {
        state.seenResponse = true;
        handlers.onStatus("responding");
      }
      if (p.content) handlers.onResponse(p.content as string);
      break;
    case "tool_call": {
      handlers.onStatus("tools");
      // wire shape: {"type":"tool_call","tool_call":{"id","name","arguments"}}
      const tc = (p.tool_call || {}) as Record<string, unknown>;
      handlers.onToolCall({
        name: (tc.name as string) || (p.name as string) || "tool",
        status: "running",
        output: JSON.stringify(tc.arguments, null, 2),
      });
      break;
    }
    case "tool_result": {
      // {"type":"tool_result","message":{"content","tool_call_id"}}
      const tr = (p.message || {}) as Record<string, unknown>;
      handlers.onToolResult({
        content: (tr.content as string) || "",
        isError: tr.is_error === true,
      });
      break;
    }
    case "stream_end":
      if (p.prompt_eval_count != null || p.eval_count != null) {
        handlers.onUsage({
          promptTokens: (p.prompt_eval_count as number) || 0,
          completionTokens: (p.eval_count as number) || 0,
          cachedTokens: (p.cached_count as number) || 0,
        });
      }
      break;
    case "agent_end":
      // the terminal event: only its arrival makes the stream end CLEAN.
      // A reader that finishes without it means the connection dropped
      // mid-run (server death, proxy cut) - streamChat flags that.
      state.sawEnd = true;
      break;
    case "error":
      throw new Error(String(p.error || "agent error"));
  }
}

// Per-session stream busy map: a session is busy iff its own stream is
// live. Pure operations on value-semantic sets - the caller swaps the
// whole set per change, so the state stays reactive and the logic is
// unit-testable without a component framework.
export type BusyMap = ReadonlySet<string>;

export function withBusy(base: BusyMap, id: string, on: boolean): Set<string> {
  const next = new Set(base);
  if (on) next.add(id);
  else next.delete(id);
  return next;
}

export function isBusy(b: BusyMap, id: string): boolean {
  return b.has(id);
}

// Global view concerns (tab-title spinner, statusbar) read ANY live
// stream from the map - they never gate a session.
export function anyBusy(b: BusyMap): boolean {
  return b.size > 0;
}

// --- URL <-> session routing (hand-rolled History wiring) ---

// parseSessionPath extracts the session id from a client route path.
// Only /chat/<id> matches; home, assets, anything else -> null.
export function parseSessionPath(p: string): string | null {
  if (!p.startsWith("/chat/")) return null;
  const id = p.slice("/chat/".length);
  // a trailing slash or empty id is no route
  if (!id || id.includes("/")) return null;
  return id;
}

// sessionPath is the URL for an open session.
export function sessionPath(id: string): string {
  return `/chat/${id}`;
}

// --- session list: pure bucketing / filtering / formatting helpers ---

// relativeAge renders a compact age for the row meta ("35m", "2h", "3d").
// Invalid timestamps render "" and the caller hides the label.
export function relativeAge(iso: string, now: Date = new Date()): string {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  const mins = Math.floor((now.getTime() - t.getTime()) / 60000);
  if (mins < 1) return "now";
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

// formatTokens k-abbreviates above 1000 (2135 -> "2.1k"), passes anything
// smaller through unchanged.
export function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  const k = Math.floor(n / 100) / 10;
  return `${k}k`;
}

// filterSessions matches the search query as a case-insensitive substring
// of the session label. An empty query matches everything (no filter).
export function filterSessions(sessions: Session[], query: string): Session[] {
  const q = query.trim().toLowerCase();
  if (!q) return sessions;
  return sessions.filter((s) => (s.label ?? s.id).toLowerCase().includes(q));
}

// byNewest orders a session group by last activity, newest first. A
// missing or malformed updated_at sorts last ("now" beats the unknown).
function byNewest(a: Session, b: Session): number {
  const ta = Date.parse(a.updated_at);
  const tb = Date.parse(b.updated_at);
  if (Number.isNaN(ta)) return 1;
  if (Number.isNaN(tb)) return -1;
  return tb - ta;
}

// SessionBuckets is the grouped sidebar list: pinned sessions first (the
// keep-at-top section), then the Today / Yesterday / Earlier time
// buckets by local calendar day of updated_at. Every group is sorted by
// last activity within itself.
export interface SessionBuckets {
  pinned: Session[];
  today: Session[];
  yesterday: Session[];
  earlier: Session[];
}

// bucketSessions groups sessions into the sidebar sections. Bucketing is
// by LOCAL calendar day (midnight boundaries), not a 24h offset - a
// session touched at 23:50 yesterday stays "Yesterday" at 00:10 today.
export function bucketSessions(
  sessions: Session[],
  now: Date = new Date(),
): SessionBuckets {
  const startOfToday = new Date(now);
  startOfToday.setHours(0, 0, 0, 0);
  const startOfYesterday = startOfToday.getTime() - 86400000;

  const out: SessionBuckets = {
    pinned: [],
    today: [],
    yesterday: [],
    earlier: [],
  };
  for (const s of sessions) {
    if (s.pinned) {
      // the pinned section owns the row - no day-bucket duplicate
      out.pinned.push(s);
      continue;
    }
    const t = Date.parse(s.updated_at);
    if (!Number.isNaN(t) && t >= startOfToday.getTime()) out.today.push(s);
    else if (!Number.isNaN(t) && startOfYesterday <= t) out.yesterday.push(s);
    else out.earlier.push(s);
  }
  for (const group of Object.values(out)) group.sort(byNewest);
  return out;
}

// rowMeta renders the row's second line:
// "<model> - 12 msgs - 3 tools - 2.1k tok". Unknown pieces are omitted -
// zero tool counts and zero token totals carry no information, and a
// session with nothing known renders no meta line at all.
export function rowMeta(s: Session): string {
  const parts: string[] = [];
  if (s.model) parts.push(s.model);
  if (s.message_count != null && s.message_count > 0)
    parts.push(`${s.message_count} msgs`);
  if ((s.tool_count ?? 0) > 0) parts.push(`${s.tool_count} tools`);
  const tokens = (s.prompt_tokens ?? 0) + (s.completion_tokens ?? 0);
  if (tokens > 0) parts.push(`${formatTokens(tokens)} tok`);
  return parts.join(" - ");
}

// --- inspector: file manager helpers ---

export interface FileEntry {
  name: string;
  size_bytes: number;
  is_dir: boolean;
  mod_time: string;
}

export interface FilesBrowsePayload {
  entries: FileEntry[];
}

export interface FilesReadPayload {
  content: string;
  truncated: boolean;
  binary: boolean;
}

// cleanRelPath mirrors the server's path rules so the UI never sends a
// request the backend rejects: no absolute paths, no empty segments, no
// dot-prefixed segments (hidden files AND .. traversal die under one
// rule). Returns the cleaned path, or null when invalid ("" = root).
export function cleanRelPath(p: string): string | null {
  if (p === "") return "";
  if (p.startsWith("/")) return null;
  const parts = p.split("/");
  if (parts.some((seg) => !seg || seg.startsWith("."))) return null;
  return parts.join("/");
}

// joinRelPath descends one entry: the clicked name onto the current dir.
export function joinRelPath(base: string, name: string): string {
  return base ? `${base}/${name}` : name;
}

export interface FileCrumb {
  name: string;
  path: string;
}

// fileCrumbs derives the breadcrumb trail: the root crumb (path "") plus
// one crumb per segment, each carrying the path it jumps to.
export function fileCrumbs(path: string): FileCrumb[] {
  const crumbs: FileCrumb[] = [{ name: "/", path: "" }];
  const cleaned = cleanRelPath(path);
  if (!cleaned) return crumbs;
  let acc = "";
  for (const seg of cleaned.split("/")) {
    acc = joinRelPath(acc, seg);
    crumbs.push({ name: seg, path: acc });
  }
  return crumbs;
}

// sortFileEntries orders a directory listing: directories first, then
// names, case-insensitive. Pure - sorts a copy, never the caller's array.
export function sortFileEntries(entries: FileEntry[]): FileEntry[] {
  return [...entries].sort((a, b) => {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
    return a.name.toLowerCase().localeCompare(b.name.toLowerCase());
  });
}

// filesQuery fetches a /files endpoint and unwraps the structured error
// envelope: a failed request throws the server's error message, not a
// bare status code.
async function filesQuery<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) {
    let msg = `request: ${r.status}`;
    try {
      const body = await r.json();
      if (body?.error) msg = body.error;
    } catch {
      // non-JSON failure body: the status-code message stands
    }
    throw new Error(msg);
  }
  return r.json();
}

// fetchFilesBrowse lists one directory of the working dir ("" = root).
export function fetchFilesBrowse(path = ""): Promise<FilesBrowsePayload> {
  const suffix = path ? `?path=${encodeURIComponent(path)}` : "";
  return filesQuery(`/files/browse${suffix}`);
}

// fetchFilesRead previews one file: up to 64 KiB, truncated flag, and
// binary=true (empty content) for non-UTF-8 bytes.
export function fetchFilesRead(path: string): Promise<FilesReadPayload> {
  return filesQuery(`/files/read?path=${encodeURIComponent(path)}`);
}

// --- message hover timestamps ---

const MONTHS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

// formatMsgTime renders a message timestamp for the hover title: local
// HH:MM for the current day, "17 Oct 14:32" for older dates (the year
// prefixes any other year). Malformed input renders "" and the caller
// hides the attribute.
export function formatMsgTime(iso: string, now: Date = new Date()): string {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  const hm = `${pad(t.getHours())}:${pad(t.getMinutes())}`;
  const sameDay =
    t.getFullYear() === now.getFullYear() &&
    t.getMonth() === now.getMonth() &&
    t.getDate() === now.getDate();
  if (sameDay) return hm;
  const day = `${t.getDate()} ${MONTHS[t.getMonth()]}`;
  if (t.getFullYear() === now.getFullYear()) return `${day} ${hm}`;
  return `${t.getFullYear()} ${day} ${hm}`;
}

// --- thinking elapsed counter ---

export const ELAPSED_TICK_MS = 1000;

const pad2 = (n: number) => String(n).padStart(2, "0");

// formatElapsed renders the time since a stream started: "12s" below a
// minute, then "2m 05s", then "1h 02m". Pure - the ticking lives in
// createElapsedMeter.
export function formatElapsed(startedMs: number, nowMs: number): string {
  const total = Math.max(0, Math.floor((nowMs - startedMs) / 1000));
  if (total < 60) return `${total}s`;
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${pad2(m)}m`;
  return `${m}m ${pad2(s)}s`;
}

// ElapsedMeter drives the live "thinking 12s" summary label: start()
// records the start once and emits a fresh label every tick, stop()
// cancels the ticking and emits the frozen final label. The component
// owns the ref; this owns only the timing.
export interface ElapsedMeter {
  start(): void;
  stop(): void;
}

// createElapsedMeter takes the tick callback and optional clock parts -
// a fake clock in tests, setInterval/clearTimeout in the component.
export function createElapsedMeter(
  onElapsed: (label: string) => void,
  now: () => number = () => Date.now(),
  timers: RenderTimers = {
    // setTimeout is a window method in the browser - a bare reference
    // invoked on this plain object throws Illegal invocation (WebIDL
    // receiver check). Wrap it, like clear.
    set: (fn, ms) => setTimeout(fn, ms),
    clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
  },
): ElapsedMeter {
  let startedAt: number | null = null;
  let handle: unknown = null;
  const fire = () => {
    handle = null;
    if (startedAt === null) return;
    onElapsed(formatElapsed(startedAt, now()));
    if (handle === null) handle = timers.set(fire, ELAPSED_TICK_MS);
  };
  return {
    start() {
      if (startedAt !== null) return;
      startedAt = now();
      onElapsed(formatElapsed(startedAt, startedAt));
      if (handle === null) handle = timers.set(fire, ELAPSED_TICK_MS);
    },
    stop() {
      // clear FIRST, then emit - the timer must be gone before the final
      // label lands, or the next tick re-emits past the frozen value
      if (handle !== null) timers.clear(handle);
      handle = null;
      if (startedAt === null) return;
      onElapsed(formatElapsed(startedAt, now()));
    },
  };
}

// --- transcript scroll position (jump-to-live pill) ---

export const SCROLL_NEAR_PX = 40;

// isAtBottom is the shared near-bottom check: new-content auto-scroll
// follows the stream only while the user holds the bottom, and the pill
// is its negation. A container that cannot scroll counts as at bottom.
export function isAtBottom(
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
  threshold: number = SCROLL_NEAR_PX,
): boolean {
  if (scrollHeight <= clientHeight) return true;
  return scrollHeight - scrollTop - clientHeight < threshold;
}

// pillVisible derives the jump-to-live pill: scrolled up past the bottom
// threshold AND with something to scroll back to.
export function pillVisible(
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
  threshold: number = SCROLL_NEAR_PX,
): boolean {
  return !isAtBottom(scrollTop, clientHeight, scrollHeight, threshold);
}

// --- markdown rendering (assistant bubbles) ---

// Standard markdown-it, no plugins, no custom rules. html:false is the
// sanitizer contract: raw HTML in model output is escaped to text by the
// lib's default path, never executed. Tables and fenced blocks are in
// the default preset; an unclosed fence still renders as a code block.
const markdown = new MarkdownIt({ html: false });

export function renderMarkdown(src: string): string {
  return markdown.render(src);
}

// Debounced render tick for streaming markdown. Chunks only move the
// latest source snapshot; one timer renders once per tick (~150ms), so
// partial markdown never re-parses per chunk or flash-resizes mid-block.
// The final full render is the caller's direct renderMarkdown at stream
// end - see ChatMessage.
export const RENDER_TICK_MS = 150;

// Timers are injectable for tests: the debounce tests run against a
// manual fake clock instead of node's mock.timers (node 26 test runner
// deadlocks when a file mixes mock timers with multiple suites - flaky,
// bug) and the component uses the real timer pair.
export interface RenderTimers {
  set: (fn: () => void, ms: number) => unknown;
  clear: (handle: unknown) => void;
}

export interface RenderScheduler {
  schedule(src: string): void;
  cancel(): void;
}

export function createRenderScheduler(
  render: (src: string) => void,
  intervalMs: number = RENDER_TICK_MS,
  timers: RenderTimers = {
    // same wrap as createElapsedMeter - bare setTimeout is Illegal
    // invocation in the browser (window method, wrong receiver)
    set: (fn, ms) => setTimeout(fn, ms),
    clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
  },
): RenderScheduler {
  let latest: string | null = null;
  let handle: unknown = null;
  const fire = () => {
    handle = null;
    const src = latest;
    latest = null;
    if (src !== null) render(src);
  };
  return {
    schedule(src: string) {
      latest = src;
      if (handle === null) handle = timers.set(fire, intervalMs);
    },
    cancel() {
      if (handle !== null) timers.clear(handle);
      handle = null;
      latest = null;
    },
  };
}

export async function fetchHealth(): Promise<boolean> {
  try {
    const r = await fetch("/health");
    return r.ok;
  } catch {
    return false;
  }
}

export async function fetchModels(): Promise<ModelsInfo> {
  const r = await fetch("/models");
  if (!r.ok) throw new Error(`models: ${r.status}`);
  return r.json();
}

export async function fetchSessions(): Promise<Session[]> {
  const r = await fetch("/sessions");
  if (!r.ok) throw new Error(`sessions: ${r.status}`);
  return r.json();
}

export async function fetchSession(id: string): Promise<SessionDetail> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`);
  if (!r.ok) throw new Error(`session: ${r.status}`);
  return r.json();
}

// pinSession PATCHes the pinned flag directly on the session row.
export async function pinSession(id: string, pinned: boolean): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ pinned }),
  });
  if (!r.ok) throw new Error(`pin: ${r.status}`);
}

// Minimal structural view of a transcript item for the retry helpers -
// the app's TranscriptItem union satisfies it (all member shapes carry
// these fields or none of them).
export interface TranscriptItemLike {
  kind: string;
  role?: string;
  content?: string;
  name?: string;
  status?: string;
  output?: string;
  result?: string;
  streaming?: boolean;
  time?: string;
}

// lastCompleteUserIndex finds the transcript item a RESEND kicks off from:
// the last complete user message (kind "message", role "user"). A partial
// assistant bubble and any trailing marker sit after it; -1 when the
// transcript has no user message at all.
export function lastCompleteUserIndex(items: TranscriptItemLike[]): number {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item?.kind === "message" && item.role === "user") return i;
  }
  return -1;
}

// lastUserText is the text the interrupted retry re-sends through the
// normal send path (null when there is nothing to resend from).
export function lastUserText(items: TranscriptItemLike[]): string | null {
  const idx = lastCompleteUserIndex(items);
  if (idx === -1) return null;
  return items[idx]?.content ?? null;
}

// dropLastExchange drops the last exchange FROM its opening user message:
// the run's user text, any tool lines / thinking, the partial assistant
// bubble, and the marker all leave the transcript, so the retry's send
// path can push the user message back and stream a clean replacement
// response (no duplicate user bubble). No user message means nothing to
// drop: the transcript is returned unchanged.
export function dropLastExchange(
  items: TranscriptItemLike[],
): TranscriptItemLike[] {
  const idx = lastCompleteUserIndex(items);
  if (idx === -1) return items;
  return items.slice(0, idx);
}

// hasInterruptedMarker reports whether an interrupted-stream marker is
// still live in this transcript.
export function hasInterruptedMarker(items: TranscriptItemLike[]): boolean {
  return items.some((item) => item?.kind === "interrupted");
}

// withoutMarkers strips interrupted-stream markers, keeps order. Used
// when a fresh send retires a stale resume affordance.
export function withoutMarkers(
  items: TranscriptItemLike[],
): TranscriptItemLike[] {
  return items.filter((item) => item?.kind !== "interrupted");
}

// StreamInterruptedError: the SSE transport died mid-stream (reader or
// fetch failure, connection drop before the terminal event) - NOT an
// agent error payload, which throws a plain Error and lands in the
// bubble. The caller reacts with the interrupt marker + resume pill.
export class StreamInterruptedError extends Error {
  constructor(message = "stream interrupted") {
    super(message);
    this.name = "StreamInterruptedError";
  }
}

// streamChat POSTs messages to /chat and drives the handler callbacks from
// the SSE stream. Error payloads throw - the caller surfaces them.
export async function streamChat(
  messages: OmegaMessage[],
  sessionId: string | null,
  handlers: StreamHandlers,
  model?: string,
): Promise<void> {
  const body: Record<string, unknown> = { messages };
  if (sessionId) body.session_id = sessionId;
  if (model) body.model = model;
  const resp = await fetch("/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!resp.ok || !resp.body) throw new Error(`chat: ${resp.status}`);

  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  const state = { seenResponse: false, sawEnd: false };

  for (;;) {
    let read: ReadableStreamReadResult<Uint8Array>;
    try {
      read = await reader.read();
    } catch {
      // transport death (network cut, server drop mid-run): frames
      // already delivered stay with the caller; the failure is flagged
      // so the stream is marked interrupted, not silently dropped.
      throw new StreamInterruptedError();
    }
    const { done, value } = read;
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    // Parse complete SSE frames: "event: <name>\ndata: <json>\n\n".
    // Bad frames are skipped (parseFrame null); error payloads THROW out
    // of the loop - a mid-stream agent error is real and belongs in the
    // chat bubble, not in a skip-catch.
    let idx: number;
    while ((idx = buffer.indexOf("\n\n")) !== -1) {
      const frame = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      const payload = parseFrame(frame);
      if (payload === null) continue;
      handlePayload(payload, handlers, state);
    }
  }

  // A reader finishing WITHOUT the terminal event is an abnormal
  // termination - the server never finished the run (dropped connection,
  // process death). A clean end always passes through agent_end.
  if (!state.sawEnd) throw new StreamInterruptedError();
}
