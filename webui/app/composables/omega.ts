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

export interface OmegaImage {
  media_type: string;
  base64: string;
}

export interface OmegaMessage {
  role: "user" | "assistant";
  content: string;
  // image attachments (paste / picker / drag-drop / server-fetched URL),
  // the same shape ai.User.Images round-trips through the backend
  images?: OmegaImage[];
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
  // server-side archive flag: the row renders in the archived group at
  // the BOTTOM of the sidebar (muted), still searchable
  archived?: boolean;
  // server-side read stamp + read-time unread (updated_at > last_read_at)
  last_read_at?: string;
  unread?: boolean;
  // live-run flag from the server (always present, true/false): the
  // runHub, not the client, owns "is this session streaming" - every
  // tab reconciles its pulse against the row
  running?: boolean;
  // parent session id (branch children, t_6bf696e4): present + non-empty
  // only on forked sessions - the sidebar's "↳ branch" row badge reads
  // it; the list itself stays linear (roots and children together)
  parent_id?: string;
  // the session's pinned working folder (card t_9de5b009): file
  // operations root there; empty = the process working directory
  workspace?: string;
}

// LastRunStats is one completed run's average rates, stored on the
// session row at run end and shown by the status bar while no stream
// is live. Server-computed (chunk counts never leave the server any
// other way), absent on sessions that never ran.
export interface LastRunStats {
  tokens_per_second?: number;
  chunks_per_second?: number;
}

// TranscriptDetail is the GET /sessions/{id}/transcript payload: the
// session row (telemetry, running flag) plus the server-rendered
// transcript items - restore's single fetch, no client-side
// reconstruction.
export interface TranscriptDetail {
  session: Session;
  // live-run flag (always present, true/false): true while a detached
  // /chat run streams this session - openSession auto-subscribes to the
  // live events stream when it sees this, instead of only showing
  // history. The runHub, not the client, owns "is this session
  // streaming" - every tab reconciles against the row.
  running?: boolean;
  items: TranscriptWireItem[];
  // variants block (t_17365247): branch points with >=2 sibling runs.
  // ABSENT for sessions without variants - a plain session must render
  // exactly as it always did, so no default fills it client-side.
  variants?: VariantBlock[];
}

// VariantBlock is one wire branch point (agent.BranchInfo): message_id
// names the PARENT message (the branch point - a user turn re-run by
// retry/fork), siblings lists every run-head id in add order, count the
// variant count and active the active sibling's index.
export interface VariantBlock {
  message_id: string;
  siblings?: string[];
  count: number;
  active: number;
}

// --- the transcript component contract (shared by restore AND live) ---
//
// The chat renderer speaks exactly these item shapes. Live, they are
// built incrementally by the stream handlers; on restore they arrive
// ready-made from GET /sessions/{id}/transcript and are rendered as-is
// (renderTranscriptItems). One contract, two sources, no reload-side
// reconstruction - the reconstruction loop this replaced was the origin
// of the reload-order / thinking-merge / lost-duration bug class.
export interface MessageItem {
  kind: "message";
  role: "user" | "assistant";
  content: string;
  // store row id (restored history only) - addresses the variants
  // surface (switcher + fork). Live-built items carry none.
  id?: string;
  streaming?: boolean;
  // image attachments on user messages (ai.User.Images) - rendered as
  // thumbnails inside the bubble, click opens the raw data URL
  images?: OmegaImage[];
  // client-side timestamp for live messages, backend created_at for
  // restored history (ChatMessage renders it as a hover title)
  time?: string;
  // server-measured response duration (restored history)
  finalMs?: number;
}
export interface ThinkingItem {
  kind: "thinking";
  content: string;
  // store row id (canvas-mapped stored rows) - a run HEAD can be a
  // thinking row (a multi-row replay), and the variant switcher anchors
  // on it; live-built items carry none
  id?: string;
  streaming?: boolean;
  // server-measured thinking duration (restored history)
  finalMs?: number;
}
export interface ToolItem {
  kind: "tool";
  name: string;
  status: string;
  // store row id (the tool_CALL row) - a run HEAD can be a tool call
  // row (a multi-row replay without an opening thinking block); the
  // result merge keeps the call item's identity, so the id stays the
  // call row's. Orphan results carry none. Live-built items carry none.
  id?: string;
  output?: string;
  result?: string;
  // server-measured duration from the matched result (restored)
  finalMs?: number;
  // elapsed already consumed by a STILL-RUNNING restored call (the
  // transcript's live-run merge): the meter counts on from here
  elapsedMs?: number;
}
// inline transport markers do not exist anymore (chat-last 2a): a dead
// stream's reason renders in the statusLine above the composer; the
// transcript belongs to the canvas panel never to transport state.
// Verdict cards v1 (card t_2ecf9c51): a verdict.request tool call
// renders as an interactive card, NOT a tool line. Live items come from
// the chat panel's tool_call rows; restored items from the persisted
// assistant tool_call row. The matching tool_result's JSON content
// (built by modules/verdict resultMap) carries the settled verdict.
export interface VerdictItem {
  kind: "verdict";
  // the tool_call id - settles the card against its tool_result
  callId?: string;
  title: string;
  body?: string;
  // the pick rows, top to bottom: label plus optional description
  options: VerdictOptionItem[];
  // multi-select: rows toggle, a confirm button submits every pick
  multi: boolean;
  // a "type your own answer" row renders between options and skip
  allowText: boolean;
  timeoutS?: number;
  // live mounts anchor the client countdown to the frame's arrival;
  // restored items never count down (issuedAt unknown client-side, the
  // countdown is an approximation either way)
  issuedAt?: number;
  status: "pending" | "settled";
  // the settled verdict, rendered as the card's outcome line
  verdictKind?: "" | "answer" | "skip" | "expired";
  // the picked label(s), already joined for multi answers
  picked?: string;
  answerText?: string;
  // the stream ended while the card was still pending: no answer can
  // arrive anymore - render the card dead, not actionable
  stale?: boolean;
}

// VerdictOptionItem is one pick row: the label the row shows and an
// optional one-line description under it (v1.1: options may also
// arrive as bare strings - one-word rows without a description).
export interface VerdictOptionItem {
  label: string;
  description?: string;
}
export type TranscriptItem =
  | MessageItem
  | ThinkingItem
  | ToolItem
  | VerdictItem;

// TranscriptWireItem is one server-rendered transcript item, in the
// wire order history produced it. The shapes mirror what the live SSE
// path delivers per item kind (thinking text, response text, the
// tool_call frame's nested meta, the tool_result frame's outcome), so
// restore and live converge on the component contract above.
// `running` marks the live tail's in-flight items the reattach merge
// serves behind the persisted history (card t_692254f7): the open
// thinking/response block streams from the restored item, and the
// running tool line carries its elapsed-so-far in elapsed_ms.
export type TranscriptWireItem =
  | { kind: "user"; id?: string; content: string; time?: string; images?: OmegaImage[] }
  | { kind: "message"; id?: string; content: string; time?: string; response_ms?: number; running?: boolean }
  | { kind: "thinking"; content: string; thinking_ms?: number; running?: boolean }
  | {
      kind: "tool_call";
      tool_call?: { id?: string; name?: string; arguments?: Record<string, unknown> };
      running?: boolean;
      elapsed_ms?: number;
    }
  | {
      kind: "tool_result";
      tool_call_id?: string;
      content?: string;
      is_error?: boolean;
      elapsed_ms?: number;
    };

// renderTranscriptItems maps a transcript payload into the components'
// item shapes. user/message/thinking become bubbles; a tool_call opens
// a tool line keyed by its id and the matching tool_result (matched by
// tool_call_id, exactly like a live tool_result frame) fills the
// result, error state and elapsed onto it. Thin items (empty content)
// render nothing. Pure.
export function renderTranscriptItems(
  items: TranscriptWireItem[] | null | undefined,
): TranscriptItem[] {
  const out: TranscriptItem[] = [];
  if (!items) return out;
  const toolById = new Map<string, ToolItem>();
  const verdictById = new Map<string, VerdictItem>();
  for (const it of items) {
    switch (it.kind) {
      case "user":
        if (it.content)
          out.push({
            kind: "message",
            role: "user",
            content: it.content,
            time: it.time,
            images: it.images,
            id: it.id,
          });
        break;
      case "message":
        if (it.content)
          out.push({
            kind: "message",
            role: "assistant",
            content: it.content,
            time: it.time,
            finalMs: it.response_ms,
            id: it.id,
            // only the merge's live tail streams from restore - persisted
            // items mount final (the driver adopts the streaming ones)
            streaming: it.running ? true : undefined,
          });
        break;
      case "thinking":
        if (it.content)
          out.push({
            kind: "thinking",
            content: it.content,
            finalMs: it.thinking_ms,
            streaming: it.running ? true : undefined,
          });
        break;
      case "tool_call": {
        const t: ToolItem = {
          kind: "tool",
          name: it.tool_call?.name || "tool",
          // running = the call the results have not answered yet (the
          // live-run merge): renders as the running line with its
          // elapsed meter counting on from elapsed_ms, result attaches
          // when the stream delivers it
          status: it.running ? "running" : "done",
          output:
            it.tool_call?.arguments ?
              JSON.stringify(it.tool_call.arguments, null, 2)
            : undefined,
          elapsedMs: it.running ? (it.elapsed_ms ?? 0) : undefined,
        };
        if (it.tool_call?.id) toolById.set(it.tool_call.id, t);
        // a verdict.request call (Q10 namespaced identity, v0.12)
        // renders as a verdict card and NO tool line (the card is the
        // interactive surface AND the technical record - its result
        // below still settles the card). The matching tool_result is
        // consumed by the card branch, never by the tool-line map.
        if (t.name === "verdict.request") {
          const v = verdictCardFromToolArgs(it.tool_call?.arguments);
          if (it.tool_call?.id) verdictById.set(it.tool_call.id, v);
          v.status = "pending"; // settled by the tool_result below
          // a restored card whose call is NOT part of a live run tail
          // will never see a tool_result - render it dead from the
          // start; a matching tool_result still overrides to settled
          v.stale = it.running !== true;
          out.push(v);
          break;
        }
        out.push(t);
        break;
      }
      case "tool_result": {
        // a verdict's result settles the card (the technical record
        // rides the card, there is no plain tool line for it)
        const v = it.tool_call_id ? verdictById.get(it.tool_call_id) : undefined;
        if (v) {
          const settled = verdictFromResultText(it.content || "");
          v.status = "settled";
          v.verdictKind = settled.verdictKind || "expired";
          v.picked = settled.picked;
          v.answerText = settled.answerText;
          break;
        }
        if (!it.tool_call_id) continue;
        const t = toolById.get(it.tool_call_id);
        if (!t) continue;
        t.result = it.content || "";
        t.status = it.is_error ? "error" : "done";
        t.finalMs = it.elapsed_ms;
        // the call settled: the running meter's offset is spent
        t.elapsedMs = undefined;
        break;
      }
    }
  }
  return out;
}

// --- the chat panel's wire vocabulary (chat-last 2a) --------------------
//
// The conversation itself rides the canvas (card t_ec45b670): one chat
// panel whose payload is core.ChatPayload verbatim. The webui chat
// renderer consumes it - snapshot at load, deltas after - and maps the
// rows onto the SAME component contract renderTranscriptItems serves
// for the persisted-restore path. One component contract, two wire
// sources, zero per-frontend streaming logic.

// ChatBlockWire mirrors core.ChatBlock's json shape. A tool_call block
// carries the call verbatim; content blocks carry their text body.
export interface ChatBlockWire {
  type: string; // text | thinking | tool_call | tool_result
  content?: string;
  images?: OmegaImage[];
  running?: boolean; // the in-flight block of a live stream
  tool_call?: { id?: string; name?: string; arguments?: unknown };
  tool_call_id?: string;
  is_error?: boolean;
  elapsed_ms?: number;
}

// ChatRowWire mirrors core.ChatRow: one message row (role + its blocks
// + the row status). Status streaming while any block is still in
// flight, complete otherwise.
export interface ChatRowWire {
  id?: string; // store row id when persisted; live rows carry none
  role: string; // user | assistant | tool
  status: string; // streaming | complete
  blocks?: ChatBlockWire[];
}

// ChatPayloadWire mirrors core.ChatPayload: the session the rows cover,
// whether a live run streams it now, and the newest rows (40 max - the
// panel is small by law, the store keeps the rest).
export interface ChatPayloadWire {
  session?: string;
  running?: boolean;
  rows?: ChatRowWire[];
}

// normalizeChatPayload guards one chat-kind panel's payload: wrong
// shapes degrade to null (the panel is absent from the render, never a
// crashed pane). Rows drop malformed entries; fields guard like every
// wire guard here does. Pure.
export function normalizeChatPayload(payload: unknown): ChatPayloadWire | null {
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) return null;
  const p = payload as Record<string, unknown>;
  const rows: ChatRowWire[] = [];
  if (Array.isArray(p.rows)) {
    for (const raw of p.rows) {
      if (!raw || typeof raw !== "object" || Array.isArray(raw)) continue;
      const r = raw as Record<string, unknown>;
      if (typeof r.role !== "string" || !r.role) continue;
      if (typeof r.status !== "string" || !r.status) continue;
      // the payload vocabulary is closed: unknown roles/statuses drop
      // (a future core kind arrives with this guard knowing it)
      if (r.role !== "user" && r.role !== "assistant" && r.role !== "tool") continue;
      if (r.status !== "streaming" && r.status !== "complete") continue;
      const blocks: ChatBlockWire[] = [];
      if (Array.isArray(r.blocks)) {
        for (const bRaw of r.blocks) {
          if (!bRaw || typeof bRaw !== "object" || Array.isArray(bRaw)) continue;
          const b = bRaw as Record<string, unknown>;
          if (typeof b.type !== "string" || !b.type) continue;
          blocks.push({
            type: b.type,
            content: typeof b.content === "string" ? b.content : undefined,
            images: Array.isArray(b.images) ? (b.images as OmegaImage[]) : undefined,
            running: b.running === true ? true : undefined,
            tool_call:
              b.tool_call && typeof b.tool_call === "object"
                ? (b.tool_call as ChatBlockWire["tool_call"])
                : undefined,
            tool_call_id: typeof b.tool_call_id === "string" ? b.tool_call_id : undefined,
            is_error: b.is_error === true ? true : undefined,
            elapsed_ms: typeof b.elapsed_ms === "number" ? b.elapsed_ms : undefined,
          });
        }
      }
      rows.push({
        id: typeof r.id === "string" && r.id ? r.id : undefined,
        role: r.role,
        status: r.status,
        blocks,
      });
    }
  }
  return {
    session: typeof p.session === "string" && p.session ? p.session : undefined,
    running: p.running === true ? true : undefined,
    rows,
  };
}

// chatPanelOf finds the chat panel descriptor among the canvas panels.
// Pure.
export function chatPanelOf(panels: CanvasPanel[] | null | undefined): CanvasPanel | null {
  for (const p of panels ?? []) {
    if (p.kind === "chat") return p;
  }
  return null;
}

// chatPanelContent sums the assistant prose + thinking content a chat
// panel's rows carry - the streaming token estimate (chars/4, the same
// shape the wire meter ran) for the statusbar while a run streams
// through deltas. Pure.
export function chatPanelContent(payload: ChatPayloadWire): number {
  let total = 0;
  for (const row of payload.rows ?? []) {
    if (row.role !== "assistant") continue;
    for (const b of row.blocks ?? []) {
      if (b.type === "text" || b.type === "thinking") total += b.content?.length ?? 0;
    }
  }
  return total;
}

// chatPanelMessageCount counts the conversation's message rows the
// panel's payload carries (user bubbles + assistant prose) - the
// statusbar's message count reads it while the canvas covers the
// session. Pure.
export function chatPanelMessageCount(payload: ChatPayloadWire): number {
  let total = 0;
  for (const row of payload.rows ?? []) {
    if (row.role === "user") total++;
    else if (row.role === "assistant" && row.blocks?.[0]?.type === "text") total++;
  }
  return total;
}

// --- verdict cards v1 (card t_2ecf9c51) ---

// verdictCardFromToolArgs builds a card item from a verdict tool
// call's arguments. Pure - the live stream and the restored
// transcript both call this; a malformed args blob degrades to an
// untitled free-text card, never a crashed render.
export function verdictCardFromToolArgs(args: unknown): VerdictItem {
  const a = (args ?? {}) as Record<string, unknown>;
  const opts: VerdictOptionItem[] = Array.isArray(a.options)
    ? a.options
        .map((o): VerdictOptionItem | null => {
          if (typeof o === "string" && o !== "") return { label: o };
          if (o && typeof o === "object") {
            const label = (o as Record<string, unknown>).label;
            const desc = (o as Record<string, unknown>).description;
            if (typeof label === "string" && label !== "") {
              // no description key when the option has none
              return typeof desc === "string" && desc ? { label, description: desc } : { label };
            }
          }
          return null; // junk row: dropped, not fatal
        })
        .filter((o): o is VerdictOptionItem => o !== null)
    : [];
  return {
    kind: "verdict",
    title: typeof a.title === "string" && a.title ? a.title : "question",
    body: typeof a.body === "string" && a.body ? a.body : undefined,
    options: opts,
    multi: a.multi === true,
    allowText: a.allow_text === true,
    timeoutS: typeof a.timeout_s === "number" && a.timeout_s > 0 ? a.timeout_s : undefined,
    status: "pending",
  };
}

// verdictFromResultText parses the verdict tool result's JSON
// (modules/verdict resultMap). Non-JSON content (a refusal, an error
// string) means the card never issued: the card is dead, no verdict.
export function verdictFromResultText(text: string): {
  verdictKind: "" | "answer" | "skip" | "expired";
  picked?: string;
  answerText?: string;
} {
  try {
    const j = JSON.parse(text) as Record<string, unknown>;
    const kind = j.verdict;
    if (kind === "answer" || kind === "skip" || kind === "expired") {
      let picked: string | undefined;
      if (Array.isArray(j.options)) {
        // a multi answer: every picked label, joined for the outcome line
        picked = j.options
          .filter((o): o is string => typeof o === "string" && o !== "")
          .join(", ") || undefined;
      } else if (typeof j.option === "string" && j.option) {
        picked = j.option;
      }
      const answerText = typeof j.text === "string" && j.text ? j.text : undefined;
      return { verdictKind: kind, picked, answerText };
    }
    return { verdictKind: "expired" };
  } catch {
    return { verdictKind: "expired" };
  }
}

// PendingVerdictCard is the /verdicts listing row (the bell count and
// the re-render-after-reload source). ExpiresAt is a Unix ms epoch.
export interface PendingVerdictCard {
  id: string;
  session_id: string;
  title: string;
  body?: string;
  options?: VerdictOptionItem[];
  multi?: boolean;
  allow_text?: boolean;
  timeout_s?: number;
  expires_at: number;
}

// fetchVerdicts lists pending cards: one session's cards, or every
// session's when empty (the bell count).
export async function fetchVerdicts(sessionId = ""): Promise<{ cards: PendingVerdictCard[]; count: number }> {
  const url = sessionId
    ? `/sessions/${encodeURIComponent(sessionId)}/verdicts`
    : `/verdicts`;
  const r = await fetch(url);
  if (r.status === 501) return { cards: [], count: 0 }; // seam unmounted
  if (!r.ok) throw new Error(`verdicts: ${r.status}`);
  return r.json();
}

// postVerdict records the user's answer on one card. card_id is
// optional: absent means the session's oldest pending card (the common
// case - one run per session). A 409 (the race was lost: answered,
// skipped, or expired) throws with the server's reason so the losing
// surface shows it instead of retrying.
export async function postVerdict(
  sessionId: string,
  body: { card_id?: string; option_indexes?: number[]; option_index?: number; option?: string; text?: string; skip?: boolean },
): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(sessionId)}/verdicts`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    let msg = `verdict: ${r.status}`;
    try {
      const j = (await r.json()) as { error?: string };
      if (j?.error) msg = j.error;
    } catch { /* the status-code message stands */ }
    throw new Error(msg);
  }
}

// --- chat appearance (card t_84e4653c) ---
//
// 'chat' is the chronological transcript (the pre-stacked appearance,
// default); 'stacked' renders each turn as a self-contained block - the
// user message plus everything the run produced in response. The
// choice persists in localStorage under APPEARANCE_KEY (same pattern
// as omega.theme-mode / omega.thinking-level).

export type Appearance = "chat" | "stacked";

export const APPEARANCE_KEY = "omega.appearance";

// parseAppearance guards the localStorage round-trip: anything but the
// exact "stacked" string (missing key, older build, tampered value)
// falls back to the default appearance.
export function parseAppearance(v: unknown): Appearance {
  return v === "stacked" ? "stacked" : "chat";
}

// splitBlocks cuts the transcript where the backend's segmented
// persistence does: every user message opens a block and everything
// after it belongs to that block; non-user items BEFORE the first user
// message (an orphaned error bubble from a failed session create) form
// their own block, and an assistant-only tail stays in the block it
// follows. Live streams append into the CURRENT block (the stream
// never opens one), so while a run is producing, the block that holds
// its user message also holds the answer underneath it. Pure.
export function splitBlocks(items: TranscriptItem[]): TranscriptItem[][] {
  const blocks: TranscriptItem[][] = [];
  for (const item of items) {
    if (item.kind === "message" && item.role === "user") {
      blocks.push([item]);
    } else if (blocks.length === 0) {
      blocks.push([item]);
    } else {
      blocks[blocks.length - 1]!.push(item);
    }
  }
  return blocks;
}

// liveRunSlice derives the stacked live-run layout (card t_c40164cf):
// the LAST user message is the pinned question header, everything after
// it is the run's agent body, and older history rides out of view -
// a layout state, not a scroll position. No new state: both regions cut
// from the same transcript the settled column renders. Pure.
export function liveRunSlice(items: TranscriptItem[]): {
  user: TranscriptItem | null;
  body: TranscriptItem[];
} {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i]!;
    if (item.kind === "message" && item.role === "user") {
      return { user: item, body: items.slice(i + 1) };
    }
  }
  return { user: null, body: [] };
}

export async function fetchTranscript(id: string): Promise<TranscriptDetail> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}/transcript`);
  if (!r.ok) throw new Error(`transcript: ${r.status}`);
  return r.json();
}

// --- variants (card t_a62eeb43): switcher + fork navigation ---
//
// The server derives the branch tree (t_17365247); this surface only
// reads its wire block and posts selections. The rendered transcript IS
// the active path, so a sibling's switcher data rides the bubble's
// store id - live-built items carry none and render no switcher.

// variantOf resolves ONE assistant bubble's branch point: an assistant
// message whose store id sits in a branch block's siblings. A session
// without variants (the block is absent) and live/streaming bubbles
// resolve to null - the switcher renders ONLY when siblings exist.
export function variantOf(
  message: MessageItem | null | undefined,
  variants: VariantBlock[] | null | undefined,
): VariantBlock | null {
  if (!variants?.length || !message || message.role !== "assistant" || !message.id) {
    return null;
  }
  for (const v of variants) {
    if (v.count >= 2 && v.siblings?.includes(message.id)) return v;
  }
  return null;
}

// variantAnchors resolves WHERE a branch block's switcher renders
// (card t_0df4efd8): the branch point's siblings are the run HEAD row
// ids (DeriveTree: the first assistant row of each run), and on the
// active path exactly one sibling is rendered - but that row can be a
// THINKING row or a TOOL CALL row (a multi-row replay), which render
// as ThinkingBlock/ToolCallLine, not bubbles. One anchor per branch,
// resolved in this order:
//   1. the head row is an assistant bubble (a plain one-row reply -
//      the classic case) - the bubble anchors, variantOf semantics.
//   2. head renders as thinking/tool and the run carries a stored TEXT
//      row - the run's last stored assistant bubble anchors (its id is
//      not a sibling; variantLabel falls back to the block's active
//      index, which is this bubble's position).
//   3. no stored text row - the head row itself anchors (the chip
//      mounts on its thinking block / tool line).
// The keyed map is by the anchor item's store id; live items (no id)
// resolve to nothing - the switcher mounts only on stored rows. Pure.
export function variantAnchors(
  items: TranscriptItem[],
  variants: VariantBlock[] | null | undefined,
): Map<string, VariantBlock> {
  const anchors = new Map<string, VariantBlock>();
  if (!variants?.length) return anchors;
  const idOf = (it: TranscriptItem): string | undefined =>
    it.kind === "message" || it.kind === "thinking" || it.kind === "tool" ?
      it.id
    : undefined;
  for (const v of variants) {
    if (!v || v.count < 2) continue;
    const head = v.siblings?.[Math.min(Math.max(v.active, 0), (v.siblings?.length ?? 1) - 1)];
    if (!head) continue;
    const headItem = items.find((it) => idOf(it) === head);
    if (!headItem) continue; // branch not on the rendered path
    if (headItem.kind === "message" && variantOf(headItem, [v])) {
      anchors.set(head, v);
      continue;
    }
    // the segment this run replays: strictly after the branch point's
    // user bubble, up to the next user bubble - a later branch point
    // (deeper fork) owns its own segment
    let start = -1;
    for (let i = items.length - 1; i >= 0; i--) {
      const it = items[i]!;
      if (it.kind === "message" && it.role === "user" && it.id === v.message_id) {
        start = i;
        break;
      }
    }
    if (start >= 0) {
      let end = items.length;
      for (let i = start + 1; i < items.length; i++) {
        const it = items[i]!;
        if (it.kind === "message" && it.role === "user") {
          end = i;
          break;
        }
      }
      for (let i = end - 1; i > start; i--) {
        const it = items[i]!;
        if (it.kind === "message" && it.role === "assistant" && it.id) {
          anchors.set(it.id, v);
          break;
        }
      }
    }
    if (![...anchors.values()].includes(v) &&
        (headItem.kind === "thinking" || headItem.kind === "tool")) {
      anchors.set(head, v);
    }
  }
  return anchors;
}

// variantLabel is the one-indexed « 2/3 » math: THIS bubble's position
// among its siblings (its own id when listed, else the block's active
// index), clamped into range so a stale id can never render 4/3.
export function variantLabel(
  v: VariantBlock,
  id?: string,
): { index: number; count: number } {
  const count = Math.max(1, v.count);
  let index = v.active;
  if (id && v.siblings) {
    const i = v.siblings.indexOf(id);
    if (i >= 0) index = i;
  }
  return { index: Math.min(Math.max(index, 0), count - 1), count };
}

// postVariantSelect flips the active variant server-side: message_id is
// either a branch point (select = the 0-based sibling index - the
// switcher's arrows) or any conversation message (bare - the fork
// anchor flow, which selects that message's own run). Persists as a
// variant_select bookkeeping entry; the response is the session's
// updated branch block.
export async function postVariantSelect(
  sessionId: string,
  body: { message_id: string; select?: number },
): Promise<VariantBlock[]> {
  const r = await fetch(`/sessions/${encodeURIComponent(sessionId)}/variants`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error(`variants: ${r.status}`);
  const j = (await r.json()) as { variants?: VariantBlock[] };
  return j.variants ?? [];
}

export interface ModelsInfo {
  current: string;
  models: string[];
  context_window?: number;
}

// Stream handlers the composer transport paths consume. The
// conversation's RENDER data no longer rides these callbacks at all
// (chat-last 2a): streaming renders through the canvas chat panel's
// view_model_delta frames; what remains is the composer transport
// itself. onUsage/onViewModelDelta are the live ones on every stream;
// onSession only fires on the auto-create origin stream (a /chat sent
// with NO session_id - the attach path and session-keyed send streams
// never do).
export interface StreamHandlers {
  onUsage?: (usage: {
    promptTokens: number;
    completionTokens: number;
    cachedTokens: number;
  }) => void;
  /** the server auto-created this chat's session - bind the id (the
      tab learns /chat/<id>, the binding lands in the dock tree) */
  onSession?: (sessionId: string) => void;
  /** one canvas panel descriptor changed (arc 3b: the delta rides
      every live run stream) - the caller merges it into its canvas */
  onViewModelDelta?: (panel: CanvasPanel) => void;
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
  state: { sawEnd?: boolean },
): void {
  const p = payload as Record<string, unknown>;
  switch (p.type) {
    case "thinking_chunk":
      // render frames (thinking/response/tool) no longer carry handlers:
      // the chat renderer consumes the canvas panel's deltas (chat-last
      // 2a). The frames are skipped here, the delta frames carry the
      // view-model the component paints.
      break;
    case "response_chunk":
      break;
    case "tool_call":
    case "tool_result":
      // render frames: no transport consumer anymore (chat-last 2a) -
      // the canvas delta frames carry the rows the renderer paints.
      break;
    case "stream_end":
      if (p.prompt_eval_count != null || p.eval_count != null) {
        handlers.onUsage?.({
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
    case "view_model_delta": {
      // the canvas contract's delta frame (arc 3b): one panel
      // descriptor, replace-in-place. A malformed descriptor is
      // skipped - the snapshot is the catch-up for anything lost.
      const panel = normalizeCanvasPanel(p.panel);
      if (panel) handlers.onViewModelDelta?.(panel);
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

// runningIds projects the /sessions rows into the busy set the pulse
// reads: the row's server-owned running flag IS the stream state, so a
// tab that never held the SSE reconciles against it. Pure, so polling
// refreshes stay unit-testable without a component framework.
export function runningIds(list: Pick<Session, "id" | "running">[]): Set<string> {
  const ids = new Set<string>();
  for (const s of list) if (s.running) ids.add(s.id);
  return ids;
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

// Modules that ship a MAIN-pane view (a switcher destination, not an
// inspector panel) claim it by module name - the shell mounts the panel
// by lookup (app.vue) and never re-exports the module's ownership. The
// inspector's tab list is FIXED (card t_4ae27e0d: files + cron +
// http_channel + skills + tools, dock tabbar-hosted), so this set is
// declaration-only chrome for future main-view modules.
export const mainViewModules = new Set<string>(["kanban"]);

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

// shortVersion shortens the statusbar build string for DISPLAY only
// ("v0.8.1-0.20261019172201-fe5b0106e0f7" -> "v0.8.1 fe5b010"; the full
// string keeps living in /health and in the segment's tooltip). A plain
// tag ("v0.8.1") and anything unparsable pass through unchanged.
export function shortVersion(v: string): string {
  const m = /^(v[^-\s]+)-0\.\d+-([0-9a-f]+)/.exec(v);
  return m ? `${m[1]} ${m[2].slice(0, 7)}` : v;
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
// buckets by local calendar day of updated_at, then archived sessions
// (always the LAST group). Every group is sorted by last activity
// within itself.
export interface SessionBuckets {
  pinned: Session[];
  today: Session[];
  yesterday: Session[];
  earlier: Session[];
  archived: Session[];
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
    archived: [],
  };
  for (const s of sessions) {
    // the archived group owns the row: archived beats pinned AND the
    // day buckets, so the section is always the last one, whole
    if (s.archived) {
      out.archived.push(s);
      continue;
    }
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
export const SIDEBAR_BUCKETS = ["pinned", "today", "yesterday", "earlier", "archived"];

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

// --- insights (card t_e1e61680): the webui's /insights parity ---
// The payload mirrors the TUI's /insights report: overview counts, a
// per-tool breakdown, and weekday activity. The wire shape is the
// handler's insightsPayload (modules/http_channel/server_insights.go).
export interface InsightsToolRow {
  name: string;
  count: number;
}

export interface InsightsDayRow {
  day: string;
  count: number;
}

export interface InsightsNotable {
  value: number;
  detail: string;
}

export interface InsightsPayload {
  period: string;
  period_start: string;
  period_end: string;
  days: number;
  sessions: number;
  messages: number;
  user_messages: number;
  tool_calls: number;
  total_tokens: number;
  avg_session_msgs: number;
  tools: InsightsToolRow[];
  daily: InsightsDayRow[];
  notable_msgs: InsightsNotable;
  notable_tokens: InsightsNotable;
  notable_tools: InsightsNotable;
}

// fetchInsights reads the cross-session usage analytics. days mirrors
// the TUI command's arg semantics: null = the server default (30),
// 0 = all time. The server's lists are nil Go slices on empty windows -
// they come over the wire as null (tools) - normalized to [] here so
// the view never reads .length off null (card t_d7fd3ad3: the journey
// caught the render TypeError on an empty tools window).
export async function fetchInsights(days: number | null = null): Promise<InsightsPayload> {
  const suffix = days === null ? "" : `?days=${days}`;
  const p = await filesQuery<InsightsPayload>(`/insights${suffix}`);
  return {
    ...p,
    tools: p.tools ?? [],
    daily: p.daily ?? [],
  };
}

// --- canvas (view-model, arc 3c): the web half of the canvas contract ---

// CanvasPanelAction is one action a panel may invoke through the action
// pipe (POST /ui/actions). Action ids are module-qualified (settled
// Q4b): the POST carries the action's module verbatim.
export interface CanvasPanelAction {
  id: string;
  label: string;
  module: string;
}

// CanvasPanel is one panel descriptor: schema v1 exactly (settled Q2b) -
// id + title + kind + payload + actions. The payload is the panel
// kind's render data, opaque to the contract; v1 renders it plain
// (JSON display). Rich kinds land with their real migrations (phase
// 4+) - no speculative kinds here.
export interface CanvasPanel {
  id: string;
  title: string;
  kind: string;
  payload: unknown;
  actions: CanvasPanelAction[];
}

// normalizeCanvasPanel guards one wire descriptor: wrong shapes drop
// out (a malformed panel is absent, never a crashed render), label-less
// actions fall back to their id, an absent payload normalizes to null
// (the plain render's "no payload" state). Pure.
export function normalizeCanvasPanel(raw: unknown): CanvasPanel | null {
  if (!raw || typeof raw !== "object") return null;
  const p = raw as Record<string, unknown>;
  if (typeof p.id !== "string" || !p.id) return null;
  const actions: CanvasPanelAction[] = [];
  if (Array.isArray(p.actions)) {
    for (const a of p.actions as unknown[]) {
      if (!a || typeof a !== "object") continue;
      const rec = a as Record<string, unknown>;
      if (typeof rec.id !== "string" || !rec.id) continue;
      if (typeof rec.module !== "string" || !rec.module) continue;
      actions.push({
        id: rec.id,
        module: rec.module,
        label: typeof rec.label === "string" && rec.label ? rec.label : rec.id,
      });
    }
  }
  return {
    id: p.id,
    title: typeof p.title === "string" && p.title ? p.title : p.id,
    kind: typeof p.kind === "string" && p.kind ? p.kind : "default",
    payload: p.payload === undefined ? null : p.payload,
    actions,
  };
}

// fetchCanvas loads the panel snapshot (GET /ui/canvas): the
// descriptors verbatim. Nothing mounted is a 200 with [] - a state,
// not an error. Malformed entries drop out; the array shape stands.
export async function fetchCanvas(): Promise<CanvasPanel[]> {
  const r = await fetch("/ui/canvas");
  if (!r.ok) throw new Error(`canvas: ${r.status}`);
  const list: unknown = await r.json();
  if (!Array.isArray(list)) return [];
  const out: CanvasPanel[] = [];
  for (const item of list) {
    const p = normalizeCanvasPanel(item);
    if (p) out.push(p);
  }
  return out;
}

// postCanvasAction sends one action-pipe call (POST /ui/actions).
// Route failures (unknown module/action) are 404s; the handler's own
// error is a 500 - both carry the server's plain-text reason, which the
// panel's error state renders verbatim. Success resolves the
// {"result": ...} body; the panel updates through the deltas, never
// through this response (the call may emit none of its own). An args
// payload rides the call when the invoking binding declares one
// (row bindings carry the arguments the action needs).
export async function postCanvasAction(
  mod: string,
  action: string,
  args?: unknown,
): Promise<unknown> {
  const r = await fetch("/ui/actions", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ module: mod, action, args }),
  });
  if (!r.ok) {
    let msg = `action: ${r.status}`;
    try {
      const text = await r.text();
      if (text) msg = text;
    } catch {
      // non-text failure body: the status-code message stands
    }
    throw new Error(msg);
  }
  const j = (await r.json()) as { result?: unknown };
  return j.result;
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
// initialMs is the elapsed ALREADY CONSUMED when the meter starts (the
// reattach merge's restored running tool counts on from the server's
// elapsed-so-far instead of restarting the call's clock at zero).
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
  initialMs = 0,
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
      startedAt = now() - initialMs;
      onElapsed(formatElapsed(startedAt, now()));
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

// scrollFlushTick picks how a notify-driven scroll reaches the layout.
// requestAnimationFrame is frozen for the lifetime of a hidden document
// (send -> switch tab/window: the run's body would never follow the tail
// again until the tab came back). A timer flush queues behind the same
// microtask render flush rAF was covering, so the DOM the callback
// measures is current in both cases.
export type ScrollFlushTick = "raf" | "timer";
export function scrollFlushTick(hidden: boolean): ScrollFlushTick {
  return hidden ? "timer" : "raf";
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

// the list ALWAYS carries both archive sides (the archived sessions
// render in their own bottom group, card t_e527b3e8) - one fetch
export async function fetchSessions(): Promise<Session[]> {
  const r = await fetch("/sessions?archived=include");
  if (!r.ok) throw new Error(`sessions: ${r.status}`);
  return r.json();
}

// --- extensions inventory: GET /extensions (card t_14f73742) ---

// OmegaExtension is one loaded module: the TUI /extensions facts (name,
// tool + command counts, seams) plus the module's own one-line
// description (omitted when the module doesn't implement agent.Describer).
// has-ui is NOT on the wire - the shell derives it from the composition
// manifest (modules/<name>/ui/dockable.json) at build time.
export interface OmegaExtension {
  name: string;
  description?: string;
  tools: number;
  commands: number;
  seams: string[];
}

export async function fetchExtensions(): Promise<OmegaExtension[]> {
  const r = await fetch("/extensions");
  if (!r.ok) throw new Error(`extensions: ${r.status}`);
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
  // options drive select knobs: plain strings are value+label; the
  // object form splits them (user themes label their names, value
  // theme:<id>). Server knobs only ever send plain strings.
  options?: (string | { value: string; label: string })[];
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

// --- modules: the live module toggle surface (v0.12 E2) ---

// ModuleRow is one registry-introspected module. state is the live
// mount state; dependents lists the modules that follow this one down
// when it disables (the Q3 edge, surfaced).
export interface ModuleRow {
  name: string;
  description?: string;
  provides?: string[];
  requires?: string[];
  state: "mounted" | "disabled";
  toggleable: boolean;
  dependents?: string[];
}

// ModuleRefusal is one ledger entry: a rejection the UI surfaces as an
// event (Q12).
export interface ModuleRefusal {
  module: string;
  predicate: string;
  err: string;
  at: string;
}

export interface ModulesPayload {
  modules: ModuleRow[];
  refusals: ModuleRefusal[];
}

// normalizeModulesPayload sanitizes a /modules response: junk entries
// drop out, names stay strings, states fold to the two known values.
// Pure, like the settings normalizer.
export function normalizeModulesPayload(raw: unknown): ModulesPayload {
  const modules: ModuleRow[] = [];
  const list =
    raw && typeof raw === "object" && Array.isArray((raw as Record<string, unknown>).modules)
      ? ((raw as Record<string, unknown>).modules as unknown[])
      : [];
  for (const m of list) {
    if (!m || typeof m !== "object") continue;
    const mod = m as Record<string, unknown>;
    if (typeof mod.name !== "string" || mod.name.length === 0) continue;
    const state = mod.state === "disabled" ? "disabled" : "mounted";
    modules.push({
      name: mod.name,
      description: typeof mod.description === "string" ? mod.description : undefined,
      provides: Array.isArray(mod.provides) ? (mod.provides as string[]) : undefined,
      requires: Array.isArray(mod.requires) ? (mod.requires as string[]) : undefined,
      state,
      toggleable: mod.toggleable === true,
      dependents: Array.isArray(mod.dependents) ? (mod.dependents as string[]) : undefined,
    });
  }
  const refusals: ModuleRefusal[] = [];
  const entries =
    raw && typeof raw === "object" && Array.isArray((raw as Record<string, unknown>).refusals)
      ? ((raw as Record<string, unknown>).refusals as unknown[])
      : [];
  for (const e of entries) {
    if (!e || typeof e !== "object") continue;
    const ref = e as Record<string, unknown>;
    if (typeof ref.module !== "string" || typeof ref.err !== "string") continue;
    refusals.push({
      module: ref.module,
      predicate: typeof ref.predicate === "string" ? ref.predicate : "",
      err: ref.err,
      at: typeof ref.at === "string" ? ref.at : "",
    });
  }
  return { modules, refusals };
}

// fetchModules loads the module list. 501 = the host serves no toggle
// seam: the caller hides the section, like the settings pane does.
export async function fetchModules(): Promise<ModulesPayload> {
  const r = await fetch("/modules");
  if (!r.ok) {
    let msg = `modules: ${r.status}`;
    try {
      const body = await r.json();
      if (typeof body?.error === "string") msg = body.error;
    } catch {
      // non-JSON failure body: the status-code message stands
    }
    throw new Error(msg);
  }
  return normalizeModulesPayload(await r.json());
}

// toggleModule PUTs one module's new state and returns the refreshed
// introspection. Rejections throw with the server's ledger text (Q11:
// the roll-back text surfaces verbatim).
export async function toggleModule(name: string, enabled: boolean): Promise<ModulesPayload> {
  const r = await fetch(`/modules/${encodeURIComponent(name)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
  });
  if (!r.ok) {
    let msg = `module toggle: ${r.status}`;
    try {
      // the error envelope is plain text (http.Error), not JSON
      const text = await r.text();
      if (text) msg = text;
    } catch {
      // keep the status-code message
    }
    throw new Error(msg);
  }
  return normalizeModulesPayload(await r.json());
}

// toggleNote builds the confirm line for a toggle: not-toggleable
// modules state their always-on law, dependents name what goes down
// with the module (the Q3 semantics surfaced), everything else just
// asks. Empty string = apply without a confirm.
export function toggleNote(m: ModuleRow, enable: boolean): string {
  if (!m.toggleable) return `${m.name} is machine-critical or engine ring - always on`;
  if (enable) return "";
  if (m.dependents && m.dependents.length > 0)
    return `disable ${m.name}? this also disables: ${m.dependents.join(", ")}`;
  return `disable ${m.name}?`;
}

// filterModuleRows narrows the module list live, same contract as
// filterSettingGroups: a row survives when the case-insensitive query
// is a substring of its name or description (the two human-readable
// strings a row renders); rows with no match drop out. An empty (or
// whitespace-only) query returns everything unchanged. Pure - never
// touches the caller's arrays.
export function filterModuleRows(
  rows: ModuleRow[],
  query: string,
): ModuleRow[] {
  const q = query.trim().toLowerCase();
  if (!q) return rows;
  return rows.filter(
    (m) =>
      m.name.toLowerCase().includes(q) ||
      (m.description ?? "").toLowerCase().includes(q),
  );
}

// --- themes: user token themes behind /themes ---

// ThemeMode is the theme select's value space: the three built-in
// modes, or a user theme applied by id (theme:<id> from GET /themes).
export type ThemeMode = "auto" | "light" | "dark" | `theme:${string}`;

export interface UserTheme {
  id: string;
  name: string;
  base: "dark" | "light";
  tokens: Record<string, string>;
  error?: string;
}

// THEME_TOKEN_KEYS mirrors the server's token allowlist (the color set
// main.css defines, plus the two layer shadows). It exists to CLEAR
// overrides when a user theme is switched off (built-in modes must
// restore :root / [data-theme=light] values untouched); the server
// validates what may be set.
export const THEME_TOKEN_KEYS = [
  "accent",
  "unread",
  "z0",
  "z1",
  "z1-raised",
  "z2",
  "stroke",
  "text",
  "text-dim",
  "ok",
  "err",
  "shadow-1",
  "shadow-2",
] as const;

// themeSelectValue / parseThemeSelect: the wire value of a user theme
// in the theme select.
const THEME_SELECT_PREFIX = "theme:";
export const themeSelectValue = (id: string) =>
  `${THEME_SELECT_PREFIX}${id}`;
export const parseThemeSelect = (value: string): string | null =>
  value.startsWith(THEME_SELECT_PREFIX)
    ? value.slice(THEME_SELECT_PREFIX.length)
    : null;

// usableThemes: the picker lists only themes that parsed and validated.
export function usableThemes(themes: UserTheme[]): UserTheme[] {
  return themes.filter((t) => !t.error);
}

// normalizeThemesPayload sanitizes a /themes response: malformed
// entries drop out; error-flagged entries survive so the server's loud
// failure stays inspectable. Pure.
export function normalizeThemesPayload(raw: unknown): UserTheme[] {
  const list =
    raw && typeof raw === "object" && "themes" in (raw as Record<string, unknown>)
      ? (raw as Record<string, unknown>).themes
      : raw;
  if (!Array.isArray(list)) return [];
  const out: UserTheme[] = [];
  for (const item of list) {
    if (!item || typeof item !== "object") continue;
    const v = item as Record<string, unknown>;
    if (typeof v.id !== "string") continue;
    const base = v.base === "light" ? "light" : "dark";
    const error = typeof v.error === "string" && v.error ? v.error : undefined;
    const tokens: Record<string, string> = {};
    if (v.tokens && typeof v.tokens === "object") {
      for (const [k, tok] of Object.entries(v.tokens as Record<string, unknown>)) {
        if (typeof tok === "string") tokens[k] = tok;
      }
    }
    out.push({
      id: v.id,
      name: typeof v.name === "string" && v.name ? v.name : v.id,
      base,
      tokens,
      error,
    });
  }
  return out;
}

// fetchThemes loads the user theme list. Throws on non-2xx with the
// server's message; the pane survives a failure (list just goes empty).
export async function fetchThemes(): Promise<UserTheme[]> {
  const r = await fetch("/themes");
  if (!r.ok) {
    let msg = `themes: ${r.status}`;
    try {
      const body = await r.json();
      if (typeof body?.error === "string") msg = body.error;
    } catch {
      // non-JSON failure body: the status-code message stands
    }
    throw new Error(msg);
  }
  return normalizeThemesPayload(await r.json());
}

// clearThemeTokens removes every token override the inline style
// carries - the undo half of applying a user theme.
export function clearThemeTokens(style: CSSStyleDeclaration): void {
  for (const k of THEME_TOKEN_KEYS) style.removeProperty(`--${k}`);
}

// applyThemeTokens writes a user theme's overrides into the element's
// inline style. Inline style beats both :root and [data-theme=light],
// so overrides stack cleanly on the theme file's chosen base; clearing
// first makes a re-apply (a file edited on disk) replace, not mix.
export function applyThemeTokens(
  style: CSSStyleDeclaration,
  tokens: Record<string, string>,
): void {
  clearThemeTokens(style);
  for (const [k, v] of Object.entries(tokens)) style.setProperty(`--${k}`, v);
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

// --- session search: one endpoint owns title AND content hits ---

// SessionSearchHit is one /sessions/search row. The backend ranks
// label matches ahead of content matches and dedups to one hit per
// session; the snippet is context for later polish - today only the
// id feeds the sidebar, titleMatch marks label hits.
export interface SessionSearchHit {
  id: string;
  snippet: string;
  titleMatch?: boolean;
}

// searchSessions queries the unified endpoint - one query owns BOTH
// title and content hits. archived=include rides every query: the
// archived hits land in the sidebar's archived group like any other
// archived row. Throws on non-2xx (400 = FTS rejected the query syntax).
export async function searchSessions(
  query: string,
): Promise<SessionSearchHit[]> {
  const r = await fetch(
    `/sessions/search?q=${encodeURIComponent(query)}&archived=include`,
  );
  if (!r.ok) throw new Error(`session search: ${r.status}`);
  return r.json();
}

// Debounce for the sidebar's session search - slow enough not to fire
// per keystroke, fast enough to feel live.
export const SESSION_SEARCH_DEBOUNCE_MS = 300;

// withSearchHits maps the backend's ranked ids onto the session list:
// the ONE source of truth for what a query matches, titles and content
// alike - no in-memory filter runs alongside it. hitIds null means no
// query or the first response has not landed yet: the full list stands.
// Ids the list no longer holds (deleted since the search) are dropped.
export function withSearchHits(sessions: Session[], hitIds: string[] | null): Session[] {
  if (!hitIds) return sessions;
  const byId = new Map(sessions.map((s) => [s.id, s] as const));
  const out: Session[] = [];
  const seen = new Set<string>();
  for (const id of hitIds) {
    if (seen.has(id)) continue;
    seen.add(id);
    const s = byId.get(id);
    if (s) out.push(s);
  }
  return out;
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

// attachImageUrl asks the SERVER to fetch a remote image URL (POST
// /attach-image) and answer the ai.ImageContent shape - the browser
// never fetches cross-origin image bytes itself. Throws with the
// server's rejection text (oversize, non-image, HTTP failure).
export async function attachImageUrl(url: string): Promise<OmegaImage> {
  const r = await fetch("/attach-image", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ url }),
  });
  if (!r.ok) {
    const text = await r.text();
    throw new Error(`attach-url: ${r.status}${text ? `: ${text}` : ""}`);
  }
  return (await r.json()) as OmegaImage;
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

// setSessionContext pins the session's remembered send context (card
// t_9de5b009): the model it replays with and the folder its file
// operations root at. Pass only what changes - omitted fields stay
// untouched; "" clears one. updated_at never moves.
export async function setSessionContext(
  id: string,
  model?: string,
  workspace?: string,
): Promise<void> {
  const body: Record<string, unknown> = {};
  if (model !== undefined) body.model = model;
  if (workspace !== undefined) body.workspace = workspace;
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    const text = (await r.text()).trim();
    throw new Error(`session context: ${r.status}${text ? `: ${text}` : ""}`);
  }
}

// WorkspaceListing is one GET /workspaces/browse response: the resolved
// dir, its parent ("" at the root - the climb hides there) and the
// visible child directories (dirs only, hidden entries filtered
// server-side).
export interface WorkspaceListing {
  path: string;
  parent: string;
  dirs: { name: string; path: string }[];
}

// browseWorkspaces lists one directory's child folders for the session
// workspace picker. Root (no arg) lists "/". Read-only; the server
// validates every path before the disk is touched.
export async function browseWorkspaces(path?: string): Promise<WorkspaceListing> {
  const q = path ? `?path=${encodeURIComponent(path)}` : "";
  const r = await fetch(`/workspaces/browse${q}`);
  if (!r.ok) {
    const text = (await r.text()).trim();
    throw new Error(text || `browse: ${r.status}`);
  }
  return (await r.json()) as WorkspaceListing;
}

// deleteSession removes a session PERMANENTLY (messages cascade
// server-side). There is no undo - the caller must confirm first.
export async function deleteSession(id: string): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!r.ok) throw new Error(`delete: ${r.status}`);
}

// branchSession forks a session (POST /sessions/{id}/branch - the
// /branch parity). Returns the CHILD row (parent_id set); the caller
// refreshes the list and opens the child like any session. The child's
// transcript + context inherit the parent's history server-side.
export async function branchSession(id: string): Promise<Session> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}/branch`, {
    method: "POST",
  });
  if (!r.ok) throw new Error(`branch: ${r.status}`);
  return (await r.json()) as Session;
}

// renameSession PUTs the label on the session row (the /label parity -
// same endpoint the webui title flow uses).
export async function renameSession(id: string, label: string): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ label }),
  });
  if (!r.ok) throw new Error(`rename: ${r.status}`);
}

// exportSessionUrl is the JSONL export (the /export parity) as a plain
// download href - one {"role","content"} line per message, served as an
// attachment; the anchor's `download` attribute keeps the filename.
export function exportSessionUrl(id: string): string {
  return `/sessions/${encodeURIComponent(id)}/export`;
}

// sessionRowBadge is the subtle branch mark for parented sessions (the
// fork lineage, t_6bf696e4): "↳ branch" inside the row, nothing for
// plain sessions - badge-less rows render exactly as they always did.
export function sessionRowBadge(s: Pick<Session, "parent_id">): string {
  return s.parent_id ? "↳ branch" : "";
}

// commitSessionRename reduces an inline rename draft to its commit
// form: a no-op draft (empty, or unchanged after trim) returns null -
// the edit just closes - a real edit returns the trimmed label for the
// PUT. Pure: the component owns the editing state.
export function commitSessionRename(
  draft: string,
  current: string | undefined,
): string | null {
  const trimmed = draft.trim();
  if (!trimmed || trimmed === (current ?? "")) return null;
  return trimmed;
}

// Minimal structural view of a transcript item for the retry helpers -
// only the fields lastCompleteUserIndex/lastUserText read; the app's
// TranscriptItem union satisfies it (all member shapes carry kind, and
// role/content when applicable).
export interface TranscriptItemLike {
  kind: string;
  role?: string;
  content?: string;
}

// lastCompleteUserIndex finds the transcript's last complete user
// message (kind "message", role "user"). A partial assistant bubble and
// any trailing marker sit after it; -1 when there is no user message.
export function lastCompleteUserIndex(items: TranscriptItemLike[]): number {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item?.kind === "message" && item.role === "user") return i;
  }
  return -1;
}

// lastUserText is the transcript check behind the composer's retry
// affordance (t_357cbe9c): an exchange exists to replay when a user
// message is present. The SERVER re-runs the persisted last user turn -
// no helper needs the text itself any more.
export function lastUserText(items: TranscriptItemLike[]): string | null {
  const idx = lastCompleteUserIndex(items);
  if (idx === -1) return null;
  return items[idx]?.content ?? null;
}

// dataUrl turns an image attachment into an img/click-open src.
export function dataUrl(img: OmegaImage): string {
  return `data:${img.media_type};base64,${img.base64}`;
}

// MEDIA_MAGIC mirrors the ai/image.go magic signatures so the
// composer-side validation matches what the backend accepts. Order
// matters visually but not logically: first hit wins, and webp needs
// its RIFF marker confirmed at offset 8.
export function mediaTypeFromBase64(base64: string): string | null {
  try {
    const head = atob(base64.slice(0, 16));
    const bytes = Array.from(head, (c) => c.charCodeAt(0));
    const starts = (sig: number[]) =>
      sig.every((b, i) => bytes[i] === b);
    const sigs: [string, number[]][] = [
      ["image/png", [0x89, 0x50, 0x4e, 0x47]],
      ["image/jpeg", [0xff, 0xd8, 0xff]],
      ["image/gif", [0x47, 0x49, 0x46, 0x38]],
      ["image/bmp", [0x42, 0x4d]],
    ];
    for (const [mediaType, sig] of sigs) {
      if (starts(sig)) return mediaType;
    }
    if (starts([0x52, 0x49, 0x46, 0x46]) && head.slice(8, 12) === "WEBP") {
      return "image/webp";
    }
    return null;
  } catch {
    return null;
  }
}

// hasInterruptedMarker/withoutMarkers are gone with the interrupted
// transcript marker (chat-last 2a): the statusLine carries a dead
// stream's reason, the composer's retry face is the resume affordance.

// createItemKeySource gives transcript items a stable key surviving the
// identity-breaking transcript rebuilds (card
// t_bc62c9ba round 1): every render pulls a rebuilt history array
// (`st.transcript = items` from the canvas replay or the restored
// history), re-creating the item objects the transcript holds. Index-keyed
// v-fors reuse the bubble
// COMPONENT across those swaps, so a restored-history message inherited
// the previous session's frozen elapsed label and a retried tool call
// resumed the dead attempt's timer. Keying on object identity fixes both:
// an item that survives a rebuild carries the same object, so it keeps
// its component and its state; every newly pushed object mounts fresh.
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
// bubble. The caller surfaces the death in the chat's statusLine above
// the composer (the panel keeps its last paint); the composer's retry
// face is the resume affordance.
export class StreamInterruptedError extends Error {
  constructor(message = "stream interrupted") {
    super(message);
    this.name = "StreamInterruptedError";
  }
}

// SessionBusyError: POST /chat answered 409 - the runHub already holds a
// live run for the session (detached-run slot, server.go) so nothing was
// persisted. The caller renders the busy affordance; retrying once the
// run ends re-sends through the normal path.
export class SessionBusyError extends Error {
  constructor(message = "session already running") {
    super(message);
    this.name = "SessionBusyError";
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
  const state = { sawEnd: false };

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
      // Named control frames ride the SSE event line, not the payload's
      // type field (`event: session` + {"id":...} - the auto-created
      // session's id, sent before the run's own frames). Dispatch by
      // event name first; everything else is a typed payload.
      const evName = frame.split("\n").find((l) => l.startsWith("event: "));
      if (evName?.slice(7).trim() === "session") {
        const s = parseFrame(frame) as { id?: string } | null;
        if (typeof s?.id === "string" && s.id) handlers.onSession?.(s.id);
        continue;
      }
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

// THINKING_LEVELS mirrors ai.ThinkingLevels (ai/provider.go): the
// /chat thinking_level field is the index into this array, 0 = "none"
// = no thinking parameter sent (provider default).
export const THINKING_LEVELS = [
  "none",
  "off",
  "on",
  "minimal",
  "low",
  "medium",
  "high",
  "extra high",
  "max",
  "ultra",
] as const;

// UI options for the effort chip: "default" replaces "none" - same
// wire value (field omitted), one label in the dropdown. "off" is
// real: it disables thinking explicitly (providers that take a
// thinking parameter get think=false), not a fallback to default.
export const THINKING_UI_LEVELS = ["default", "off", ...THINKING_LEVELS.slice(2)];

// thinkingLevelWire maps a UI selection to the outgoing /chat value:
// "default" stays unset (the field is omitted), everything else is
// its index into THINKING_LEVELS.
export function thinkingLevelWire(selection: string): number | undefined {
  if (selection === "default") return undefined;
  const idx = THINKING_LEVELS.indexOf(selection);
  return idx === -1 ? undefined : idx;
}

// streamChat POSTs messages to /chat and drives the handler callbacks from
// the SSE stream. Error payloads throw - the caller surfaces them.
// fork (t_a62eeb43): sends chained onto a historical message - the
// server truncates context at the target and parents the run there.
// fork of a user turn is replay semantics - no messages ride the wire
// there either (the caller passes none). Retries are NOT a /chat flag
// on this path anymore: the retry face rides the chat panel's action
// pipe (chat-last 2a).
// ephemeral (card t_d96a5cd5): the run is a NO-PERSIST chat - no store
// row, nothing on disk; the server mints the `eph-` id (or continues
// the in-memory session when sessionId carries one) and the memory
// tool never runs. Retry/fork are refused there by design.
export async function streamChat(
  messages: OmegaMessage[],
  sessionId: string | null,
  handlers: StreamHandlers,
  model?: string,
  thinkingLevel?: number,
  fork?: string,
  ephemeral?: boolean,
): Promise<void> {
  const body: Record<string, unknown> = { messages };
  if (sessionId) body.session_id = sessionId;
  if (ephemeral) body.ephemeral = true;
  if (model) body.model = model;
  if (thinkingLevel !== undefined) body.thinking_level = thinkingLevel;
  if (fork) body.fork = fork;
  const resp = await fetch("/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (resp.status === 409) throw new SessionBusyError();
  if (!resp.ok || !resp.body) throw new Error(`chat: ${resp.status}`);
  await consumeSSE(resp, handlers);
}

// stopSession is gone with the REST stop affordance (chat-last 2a): the
// stop face dispatches the chat panel's stop action through the action
// pipe (POST /ui/actions, module http_channel). The backend surface
// POST /sessions/{id}/stop stays - it is the capsule's public REST
// route, no frontend rides it anymore.
// attachSession subscribes to a session's LIVE server-side run and
// drives the transport handlers the composer paths use (usage +
// canvas delta forwarding - the conversation renders from the panel's
// deltas). Returns false when the session has no live run (404
// "session not active") - the caller renders settled history instead.
// Everything else streams like streamChat: terminal agent_end
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
