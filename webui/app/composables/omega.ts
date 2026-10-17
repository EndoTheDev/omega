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
// in the chat's error bubble instead of being swallowed.
export function handlePayload(
  payload: unknown,
  handlers: StreamHandlers,
  state: { seenResponse: boolean },
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
  const state = { seenResponse: false };

  for (;;) {
    const { done, value } = await reader.read();
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
}
