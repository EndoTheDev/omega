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
