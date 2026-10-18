import MarkdownIt from "markdown-it";
// Syntax highlighting: highlight.js core + a curated language set, not
// the full bundle or Shiki - weight matters on the Pi. Only fenced blocks
// with a registered language get highlighted.
import hljs from "highlight.js/lib/core";
import go from "highlight.js/lib/languages/go";
import javascript from "highlight.js/lib/languages/javascript";
import typescript from "highlight.js/lib/languages/typescript";
import python from "highlight.js/lib/languages/python";
import bash from "highlight.js/lib/languages/bash";
import json from "highlight.js/lib/languages/json";
import yaml from "highlight.js/lib/languages/yaml";
import xml from "highlight.js/lib/languages/xml";
import css from "highlight.js/lib/languages/css";
import mdLang from "highlight.js/lib/languages/markdown";
import sql from "highlight.js/lib/languages/sql";
import rust from "highlight.js/lib/languages/rust";
import cLang from "highlight.js/lib/languages/c";
import cpp from "highlight.js/lib/languages/cpp";
import diff from "highlight.js/lib/languages/diff";

hljs.registerLanguage("go", go);
hljs.registerLanguage("javascript", javascript);
hljs.registerLanguage("typescript", typescript);
hljs.registerLanguage("python", python);
hljs.registerLanguage("bash", bash);
hljs.registerLanguage("json", json);
hljs.registerLanguage("yaml", yaml);
hljs.registerLanguage("xml", xml);
hljs.registerLanguage("css", css);
hljs.registerLanguage("markdown", mdLang);
hljs.registerLanguage("sql", sql);
hljs.registerLanguage("rust", rust);
hljs.registerLanguage("c", cLang);
hljs.registerLanguage("cpp", cpp);
hljs.registerLanguage("diff", diff);

// omega webui: typed client for the http_channel API.
// Endpoints verified live 2026-10-13 against modules/http_channel.
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
  // last-known run rates, computed at run end by the server and
  // persisted with the session - the status bar's durability payload
  last_run?: LastRunStats;
  model?: string;
  pinned?: boolean;
  // server-side archive flag: hidden from the default list, revealed
  // by the sidebar's archived toggle (muted rows), still searchable
  archived?: boolean;
  // server-side read stamp + read-time unread (updated_at > last_read_at)
  last_read_at?: string;
  unread?: boolean;
  // live-run flag from the server: a detached /chat run is streaming
  // this session (drives the post-refresh streaming pulse)
  active?: boolean;
}

// LastRunStats is one completed run's average rates, stored on the
// session row at run end and shown by the status bar while no stream
// is live. Server-computed (chunk counts never leave the server any
// other way), absent on sessions that never ran.
export interface LastRunStats {
  tokens_per_second?: number;
  chunks_per_second?: number;
}

export interface SessionDetail {
  session: Session;
  // live-run flag (GET /sessions/{id}): true while a detached /chat run
  // streams this session - openSession auto-subscribes to the live
  // events stream when it sees this, instead of only showing history.
  active?: boolean;
  messages:
    | {
        role?: string;
        content?: string;
        thinking?: string;
        is_error?: boolean;
        tool_call_id?: string;
        // server-measured generation durations (persisted with the
        // message; absent on history written before timing existed)
        timing?: {
          thinking_ms?: number;
          response_ms?: number;
          elapsed_ms?: number;
        };
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
    case "agent_end": {
      // the terminal event: only its arrival makes the stream end CLEAN.
      // A reader that finishes without it means the connection dropped
      // mid-run (server death, proxy cut) - streamChat flags that.
      state.sawEnd = true;
      // A run cancelled through the stop affordance unwinds through the
      // loop's NORMAL ctx path and lands HERE with finish_reason
      // "cancelled" and error "context canceled" - for EVERY subscriber,
      // not just the tab that cancelled. Throwing the transport error
      // routes the ending through the interrupt-marker path (partial
      // output kept, no error text splattered in the bubble), the same
      // semantics an SSE drop already had. The wall-clock timeout lands
      // with "context deadline exceeded" and keeps its own error-frame
      // path, so only the exact cancel string fires here.
      if (p.finish_reason === "cancelled" && p.error === "context canceled") {
        throw new StreamInterruptedError("run cancelled");
      }
      break;
    }
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

// --- sidebar: persisted collapsed sections ---

// parseCollapsedKeys reads a persisted collapsed-sections value: a JSON
// array of string keys. Malformed JSON and non-string entries are ignored -
// a bad key restores everything expanded, it never blanks the pane.
// Group-key agnostic on purpose: the sidebar buckets are allowlisted (see
// parseCollapsedLabels below), the settings groups are dynamic server data.
export function parseCollapsedKeys(raw: string | null): Set<string> {
  const out = new Set<string>();
  if (!raw) return out;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (Array.isArray(parsed))
      for (const v of parsed) if (typeof v === "string") out.add(v);
  } catch {
    return new Set();
  }
  return out;
}

// serializeCollapsedKeys writes the inverse: a Set to the JSON array
// the parser above accepts (round-trip stable).
export function serializeCollapsedKeys(set: Iterable<string>): string {
  return JSON.stringify([...set]);
}

// the only labels a sidebar section can take - anything else in the
// persisted value is stale (renamed bucket) or corrupt and gets dropped
export const SIDEBAR_BUCKETS = ["pinned", "today", "yesterday", "earlier"];

// parseCollapsedLabels is parseCollapsedKeys narrowed to the bucket
// allowlist.
export function parseCollapsedLabels(raw: string | null): Set<string> {
  const all = parseCollapsedKeys(raw);
  return new Set([...all].filter((v) => SIDEBAR_BUCKETS.includes(v)));
}

// --- sidebar: unified row status (dot / star x state color) ---

export type RowStatusShape = "dot" | "star";
export type RowStatusColor = "idle" | "streaming" | "unread";

export interface RowStatus {
  shape: RowStatusShape;
  color: RowStatusColor;
}

// sessionRowStatus derives the sidebar's ONE per-row status element.
// Shape channel is pinned: pinned rows show a star; hovering an
// unpinned row morphs the dot into a dim star (the pin affordance);
// otherwise a dot. Color channel is state, streaming winning over
// unread over idle - a live stream reads as motion whatever else
// holds. The session being viewed never reads unread: the open is the
// read (and the read PATCH confirms it server-side).
export function sessionRowStatus(
  s: Pick<Session, "id" | "pinned" | "unread">,
  streamingSet: ReadonlySet<string>,
  hovered: boolean,
  activeId: string | null,
): RowStatus {
  const shape: RowStatusShape = s.pinned || hovered ? "star" : "dot";
  const color: RowStatusColor =
    streamingSet.has(s.id) ? "streaming"
    : s.unread && s.id !== activeId
      ? "unread"
      : "idle";
  return { shape, color };
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

// joinRelPath descends one entry: the clicked name onto its parent dir.
// The tree component owns path accounting - the UI never tracks a
// current directory, it only asks "children of X".
export function joinRelPath(base: string, name: string): string {
  return base ? `${base}/${name}` : name;
}

// --- inspector file tree state ---

// FILETREE_STATE_KEY is the localStorage entry holding the set of
// expanded tree directories across sessions.
export const FILETREE_STATE_KEY = "omega.filetree-expanded";

// parseExpandedPaths restores the persisted expanded-directory set:
// malformed JSON parses empty, and every entry must be a valid relative
// path (the same rules the backend enforces on /files) - anything else
// is dropped. Duplicates collapse; order is preserved so deeper levels
// restore after their parents.
export function parseExpandedPaths(raw: string): string[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(parsed)) return [];
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of parsed) {
    if (typeof item !== "string") continue;
    if (item === "" || cleanRelPath(item) !== item) continue;
    if (seen.has(item)) continue;
    seen.add(item);
    out.push(item);
  }
  return out;
}

// serializeExpandedPaths writes the set back: paths are sorted so the
// stored shape is stable regardless of expansion order.
export function serializeExpandedPaths(paths: Iterable<string>): string {
  const valid = [...paths].filter((p) => p !== "" && cleanRelPath(p) === p);
  valid.sort();
  return JSON.stringify(valid);
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
  const delta = Math.max(0, nowMs - startedMs);
  const total = Math.floor(delta / 1000);
  // sub-second spans are real measurements (server timing records
  // milliseconds) - flooring them to "0s" reads as a broken timer.
  // Show the milliseconds instead (in-app comment 4, 2026-10-18).
  if (total < 1) return `${Math.round(delta)}ms`;
  if (total < 60) return `${total}s`;
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${pad2(m)}m`;
  return `${m}m ${pad2(s)}s`;
}

// formatDurationMs renders a server-measured duration (restored
// sessions carry real elapsed on the wire; live meters freeze their
// own) in the same voice as formatElapsed.
export function formatDurationMs(ms: number): string {
  return formatElapsed(0, ms);
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

// html:false stays - the sanitizer contract from above: the highlighter
// operates on the code span TEXT only, its .value output is hljs's own
// escaping. Unknown/missing info string falls through to markdown-it's
// default escaped rendering.
// Emits the complete fence output for a registered language - markdown-it
// hands a full <pre> block straight through when the highlight result
// starts with '<pre'. Returns '' for unknown languages so the default
// (escaped, unstyled) path takes over.
function highlightFence(code: string, lang: string): string {
  if (!lang || !hljs.getLanguage(lang)) return "";
  try {
    const value = hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
    return `<pre><code class="hljs language-${lang}">${value}</code></pre>`;
  } catch {
    return "";
  }
}

// True when src ends inside an unclosed fenced block - the streaming case
// (fence opened, closing fence not yet received). Same fence grammar as
// markdown-it: /^ {0,3}(```+|~~~+)/ opens, a bare same-char fence at the
// same or greater run closes; a backtick fence's info string may not
// contain backticks.
export function endsInOpenFence(src: string): boolean {
  let open = false;
  let marker = "";
  let minLen = 0;
  for (const line of src.split("\n")) {
    const m = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
    if (!m) continue;
    const fence = m[1];
    if (!open) {
      if (fence[0] === "`" && m[2].includes("`")) continue;
      open = true;
      marker = fence[0];
      minLen = fence.length;
    } else if (fence[0] === marker && fence.length >= minLen && m[2].trim() === "") {
      open = false;
    }
  }
  return open;
}

// Standard markdown-it, no plugins, no custom rules. html:false is the
// sanitizer contract: raw HTML in model output is escaped to text by the
// lib's default path, never executed. Tables and fenced blocks are in
// the default preset; an unclosed fence still renders as a code block.
const markdown = new MarkdownIt({ html: false, highlight: highlightFence });
// The streaming twin: identical grammar, no highlighter - used while the
// source ends inside an unclosed fence so a mid-stream snapshot renders
// plain (unstyled) code and never highlights half a token stream.
const markdownPlain = new MarkdownIt({ html: false });

export function renderMarkdown(src: string): string {
  return (endsInOpenFence(src) ? markdownPlain : markdown).render(src);
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

// HealthInfo is the /health payload beyond the status string: the
// server's build tag and uptime - the status bar's version + up seg.
export interface HealthInfo {
  version?: string;
  uptime_seconds?: number;
}

export async function fetchHealthInfo(): Promise<HealthInfo | null> {
  try {
    const r = await fetch("/health");
    if (!r.ok) return null;
    return (await r.json()) as HealthInfo;
  } catch {
    // unreachable server: the health dot shows offline, telemetry goes stale
    return null;
  }
}

// persistedTelemetry picks the status bar's durable values from a
// session row: the row's token total (prompt + completion, exactly what
// the sidebar's tok readout shows) and the last-run average rates.
// A session with no row or no run yields zeros - no phantom telemetry.
export function persistedTelemetry(
  s: Session | undefined,
): { tokens: number; tps: number; cps: number } {
  return {
    tokens: (s?.prompt_tokens ?? 0) + (s?.completion_tokens ?? 0),
    tps: s?.last_run?.tokens_per_second ?? 0,
    cps: s?.last_run?.chunks_per_second ?? 0,
  };
}

export async function fetchModels(): Promise<ModelsInfo> {
  const r = await fetch("/models");
  if (!r.ok) throw new Error(`models: ${r.status}`);
  return r.json();
}

export async function fetchSessions(includeArchived = false): Promise<Session[]> {
  const url = includeArchived ? "/sessions?archived=include" : "/sessions";
  const r = await fetch(url);
  if (!r.ok) throw new Error(`sessions: ${r.status}`);
  return r.json();
}

export async function fetchSession(id: string): Promise<SessionDetail> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`);
  if (!r.ok) throw new Error(`session: ${r.status}`);
  return r.json();
}

// --- settings pane: config knobs behind /settings ---

// SettingKnob is one exposed config field. Type drives the control:
// toggle, number, text, secret, select, textarea, bindlist (comma
// separated address list rendered as one text input). Live knobs apply
// without a restart; the rest say so in their help text.
export interface SettingKnob {
  key: string;
  label: string;
  type: "toggle" | "number" | "text" | "secret" | "select" | "textarea" | "bindlist";
  value: unknown;
  options?: string[];
  min?: number;
  max?: number;
  step?: number;
  help?: string;
  live?: boolean;
}

export interface SettingGroup {
  key: string;
  label: string;
  knobs: SettingKnob[];
}

export interface SettingsPayload {
  groups: SettingGroup[];
}

// normalizeSettingsPayload sanitizes a /settings response: malformed
// groups/knobs are dropped (wrong shape never blanks the pane with an
// exception), knobs keep only known types - an unknown type would have
// no control to render, so it falls out like any other stale entry.
const SETTING_KNOB_TYPES = ["toggle", "number", "text", "secret", "select", "textarea", "bindlist"];

export function normalizeSettingsPayload(raw: unknown): SettingsPayload {
  const groups: SettingGroup[] = [];
  if (raw && typeof raw === "object" && Array.isArray((raw as Record<string, unknown>).groups)) {
    for (const g of (raw as Record<string, unknown>).groups as unknown[]) {
      if (!g || typeof g !== "object") continue;
      const gr = g as Record<string, unknown>;
      if (typeof gr.key !== "string" || typeof gr.label !== "string") continue;
      if (!Array.isArray(gr.knobs)) continue;
      const knobs: SettingKnob[] = [];
      for (const k of gr.knobs as unknown[]) {
        if (!k || typeof k !== "object") continue;
        const kn = k as Record<string, unknown>;
        if (typeof kn.key !== "string" || typeof kn.label !== "string") continue;
        if (typeof kn.type !== "string" || !SETTING_KNOB_TYPES.includes(kn.type)) continue;
        if (kn.value === undefined) continue;
        knobs.push({
          key: kn.key,
          label: kn.label,
          type: kn.type as SettingKnob["type"],
          value: kn.value,
          options: Array.isArray(kn.options) ? (kn.options as string[]) : undefined,
          min: typeof kn.min === "number" ? kn.min : undefined,
          max: typeof kn.max === "number" ? kn.max : undefined,
          step: typeof kn.step === "number" ? kn.step : undefined,
          help: typeof kn.help === "string" ? kn.help : undefined,
          live: kn.live === true,
        });
      }
      groups.push({ key: gr.key, label: gr.label, knobs });
    }
  }
  return { groups };
}

// fetchSettings loads the config-knob payload. Throws on non-2xx with
// the server's error message (501 = host serves no settings seam).
export async function fetchSettings(): Promise<SettingsPayload> {
  const r = await fetch("/settings");
  if (!r.ok) {
    let msg = `settings: ${r.status}`;
    try {
      const body = await r.json();
      if (typeof body?.error === "string") msg = body.error;
    } catch {
      // non-JSON failure body: the status-code message stands
    }
    throw new Error(msg);
  }
  return normalizeSettingsPayload(await r.json());
}

// saveSettings PUTs the changed knob values and returns the refreshed
// payload the server computed from disk after the save.
export async function saveSettings(values: Record<string, unknown>): Promise<SettingsPayload> {
  const r = await fetch("/settings", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ values }),
  });
  if (!r.ok) {
    let msg = `settings save: ${r.status}`;
    try {
      // the error envelope is plain text (http.Error), not JSON
      const text = await r.text();
      if (text) msg = text;
    } catch {
      // keep the status-code message
    }
    throw new Error(msg);
  }
  return normalizeSettingsPayload(await r.json());
}

// filterSettingGroups narrows the settings list live: a group survives
// when any of its knobs matches the case-insensitive query as a
// substring of the label or help text; groups with no matching knobs
// drop out. An empty (or whitespace-only) query returns everything
// unchanged. Pure - never touches the caller's arrays.
export function filterSettingGroups(
  groups: SettingGroup[],
  query: string,
): SettingGroup[] {
  const q = query.trim().toLowerCase();
  if (!q) return groups;
  const out: SettingGroup[] = [];
  for (const g of groups) {
    const knobs = g.knobs.filter(
      (k) =>
        k.label.toLowerCase().includes(q) ||
        (k.help ?? "").toLowerCase().includes(q),
    );
    if (knobs.length > 0) out.push({ ...g, knobs });
  }
  return out;
}

// settingsDiff lists draft values that differ from the loaded ones -
// only changed knobs go on the wire, so an unrelated knob edited
// server-side between load and save is never clobbered by stale data.
// Pure; values compare by strict equality (JSON round-trips numbers,
// booleans and strings losslessly for these knob types).
export function settingsDiff(
  loaded: Record<string, unknown>,
  draft: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(draft)) {
    if (draft[key] !== loaded[key]) out[key] = draft[key];
  }
  return out;
}

// --- content search: FTS5 over message content, behind /sessions/search ---

// SessionContentHit is one backend search row. The snippet comes back
// from the store but is not rendered yet - matching context is later
// polish; today only the id feeds the sidebar union.
export interface SessionContentHit {
  id: string;
  snippet: string;
}

// searchSessionContent queries the store's FTS5 index through the
// server. includeArchived toggles the backend's archived filter - with
// the sidebar's archived toggle off the hits stay off (default), with
// it on the archived sessions' content is searched too. Throws on
// non-2xx (400 = FTS rejected the query syntax).
export async function searchSessionContent(
  query: string,
  includeArchived = false,
): Promise<SessionContentHit[]> {
  const suffix = includeArchived ? "&archived=include" : "";
  const r = await fetch(
    `/sessions/search?q=${encodeURIComponent(query)}${suffix}`,
  );
  if (!r.ok) throw new Error(`session search: ${r.status}`);
  return r.json();
}

// Debounce for the sidebar's content search - slow enough not to fire
// per keystroke, fast enough to feel live.
export const CONTENT_SEARCH_DEBOUNCE_MS = 300;

// withContentMatches unions the title-filtered row with the sessions
// that matched by CONTENT (ids from the backend, null before the first
// response arrives). A content-only match surfaces even though its
// title never contained the query; ids the session list no longer holds
// (deleted since the search) are dropped. Title matches come first,
// then the backend-ranked content hits - bucketing re-sorts groups.
export function withContentMatches(
  filtered: Session[],
  sessions: Session[],
  contentIds: string[] | null,
): Session[] {
  if (!contentIds || contentIds.length === 0) return filtered;
  const byId = new Map(sessions.map((s) => [s.id, s] as const));
  const present = new Set(filtered.map((s) => s.id));
  const extra: Session[] = [];
  for (const id of contentIds) {
    if (present.has(id)) continue;
    const s = byId.get(id);
    if (s) {
      extra.push(s);
      present.add(id);
    }
  }
  return filtered.concat(extra);
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

// markSessionRead stamps the session's last-read time (PATCH {"read":
// true}). It touches nothing else: reading is not activity, updated_at
// must not move.
export async function markSessionRead(id: string): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ read: true }),
  });
  if (!r.ok) throw new Error(`read: ${r.status}`);
}

// archiveSession PATCHes the archived flag directly on the session row.
// Same contract as pin: a list concern, updated_at never moves.
export async function archiveSession(id: string, archived: boolean): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ archived }),
  });
  if (!r.ok) throw new Error(`archive: ${r.status}`);
}

// deleteSession removes a session PERMANENTLY (messages cascade
// server-side). There is no undo - the caller must confirm first.
export async function deleteSession(id: string): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!r.ok) throw new Error(`delete: ${r.status}`);
}

// filterArchived keeps one side of the archive flag: the archived rows
// (toggle on) or the live ones (toggle off). The server already filters
// the fetched list to match - this is the client-side gate for the rows
// left in state after a patch (archive drops the row instantly, before
// the refresh lands).
export function filterArchived(
  sessions: Session[],
  includeArchived: boolean,
): Session[] {
  return sessions.filter((s) => (includeArchived ? s.archived : !s.archived));
}

// Minimal structural view of a transcript item for the retry helpers -
// only the fields lastCompleteUserIndex/lastUserText/
// hasInterruptedMarker read; the app's TranscriptItem union satisfies
// it (all member shapes carry kind, and role/content when applicable).
export interface TranscriptItemLike {
  kind: string;
  role?: string;
  content?: string;
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

// lastUserText is the text the retry re-sends through the normal send
// path (null when there is nothing to resend from).
export function lastUserText(items: TranscriptItemLike[]): string | null {
  const idx = lastCompleteUserIndex(items);
  if (idx === -1) return null;
  return items[idx]?.content ?? null;
}

// hasInterruptedMarker reports whether an interrupted-stream marker is
// still live in this transcript.
export function hasInterruptedMarker(items: TranscriptItemLike[]): boolean {
  return items.some((item) => item?.kind === "interrupted");
}

// withoutMarkers strips interrupted-stream markers, keeps order. Used
// when a fresh send retires a stale resume affordance.
export function withoutMarkers<T extends TranscriptItemLike>(
  items: T[],
): T[] {
  return items.filter((item) => item?.kind !== "interrupted");
}

// createItemKeySource gives transcript items a stable key surviving the
// two identity-breaking operations the transcript goes through (card
// t_bc62c9ba round 1): `st.transcript = items` swaps in a rebuilt history
// array, and `withoutMarkers + push` strips a stale marker while a retry
// appends its fresh turn and re-indexes everything after it. Index-keyed
// v-fors reuse the bubble
// COMPONENT across those swaps, so a restored-history message inherited
// the previous session's frozen elapsed label and a retried tool call
// resumed the dead attempt's timer. Keying on object identity fixes both:
// a kept object (history head, pre-interrupt exchange) keeps its
// component and its state; every newly pushed object mounts fresh.
// WeakMap: the mapping dies with the item, no leak. This is per-source
// state, not a global counter - two transcripts never share keys anyway.
export function createItemKeySource(): (item: object) => number {
  const keys = new WeakMap<object, number>();
  let seq = 0;
  return (item) => {
    let key = keys.get(item);
    if (key === undefined) {
      key = ++seq;
      keys.set(item, key);
    }
    return key;
  };
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

// consumeSSE reads frames off a fetch Response body and drives the
// handler callbacks. Shared by both transports: the send stream (POST
// /chat) and the reattach stream (GET /sessions/<id>/events) - the frame
// grammar and the clean-end rule are the same. Error payloads throw -
// the caller surfaces them.
async function consumeSSE(resp: Response, handlers: StreamHandlers): Promise<void> {
  if (!resp.body) throw new Error("chat: empty response body");
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
  await consumeSSE(resp, handlers);
}

// stopSession asks the server to cancel the session's live run
// (POST /sessions/<id>/stop). The run unwinds through its normal loop
// path - partial output persists, the cancelled agent_end goes out over
// every subscriber's wire. A miss is a benign race, not an error: 404
// means the run already ended, 409 that it has not started yet.
export async function stopSession(id: string): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}/stop`, {
    method: "POST",
  });
  if (!r.ok && r.status !== 404 && r.status !== 409) {
    throw new Error(`stop: ${r.status}`);
  }
}

// attachSession subscribes to a session's LIVE server-side run and
// drives the same handlers the send stream uses (card t_2f9b6110: the
// run is detached server-side, so a page refresh mid-generation lands
// back in the live stream). Returns false when the session has no live
// run (404 "session not active") - the caller renders settled history
// instead. Everything else streams like streamChat: terminal agent_end
// means clean, its absence throws StreamInterruptedError.
export async function attachSession(
  sessionId: string,
  handlers: StreamHandlers,
): Promise<boolean> {
  const resp = await fetch(`/sessions/${encodeURIComponent(sessionId)}/events`);
  if (!resp.ok || !resp.body) return false;
  await consumeSSE(resp, handlers);
  return true;
}
