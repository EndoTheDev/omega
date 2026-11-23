import { reactive } from "vue";
import MarkdownIt from "markdown-it";
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

import { isStreaming, isYielded } from "./toolStatus.ts";

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


export interface OmegaImage {
  media_type: string;
  base64: string;
}

export interface OmegaAttachment {
  kind: string;
  name: string;
  size: number;
  path: string;
}

// mirrors the backend's per-file cap (10 MB), so an oversize pick never leaves the browser.
export const MAX_CHAT_FILE_BYTES = 10 * 1024 * 1024;

export interface OmegaMessage {
  role: "user" | "assistant" | "system";
  content: string;
  images?: OmegaImage[];
  // non-image references the store persists; the agent reads them with its own tools and
  // providers never see them
  attachments?: OmegaAttachment[];
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
  last_run?: LastRunStats;
  model?: string;
  pinned?: boolean;
  archived?: boolean;
  last_read_at?: string;
  unread?: boolean;
  running?: boolean;
  parent_id?: string;
  workspace?: string;
}

// one completed run's average rates, server-computed; absent on sessions that never ran.
export interface LastRunStats {
  tokens_per_second?: number;
  chunks_per_second?: number;
}

export interface TranscriptDetail {
  session: Session;
  running?: boolean;
  items: TranscriptWireItem[];
  // branch points with >=2 sibling runs; ABSENT for plain sessions, so no default fills it in.
  variants?: VariantBlock[];
}

export interface VariantBlock {
  message_id: string;
  siblings?: string[];
  count: number;
  active: number;
}

// the transcript component contract, shared by restore AND live: one contract, two sources,
// no reload-side reconstruction (the origin of the reload-order / merge bug class).
export interface MessageItem {
  kind: "message";
  role: "user" | "assistant";
  content: string;
  id?: string;
  streaming?: boolean;
  images?: OmegaImage[];
  // non-image references on user messages: type-icon chips (name + size), never thumbnailed.
  attachments?: OmegaAttachment[];
  time?: string;
  finalMs?: number;
  turnMs?: number;
  elapsedMs?: number;
}
export interface ThinkingItem {
  kind: "thinking";
  content: string;
  id?: string;
  streaming?: boolean;
  finalMs?: number;
  elapsedMs?: number;
}
export interface ToolItem {
  kind: "tool";
  name: string;
  status: string;
  callId?: string;
  id?: string;
  output?: string;
  result?: string;
  // line stamped on the call's result, rendered on this row (absent = no machine decision)
  gate?: string;
  finalMs?: number;
  answerMs?: number;
  elapsedMs?: number;
}
export interface VerdictItem {
  kind: "verdict";
  callId?: string;
  title: string;
  body?: string;
  options: VerdictOptionItem[];
  multi: boolean;
  allowText: boolean;
  timeoutS?: number;
  issuedAt?: number;
  status: "pending" | "settled";
  verdictKind?: "" | "answer" | "skip" | "expired";
  picked?: string;
  answerText?: string;
}

export interface VerdictOptionItem {
  label: string;
  description?: string;
}
export type TranscriptItem =
  | MessageItem
  | ThinkingItem
  | ToolItem
  | VerdictItem;

export type TranscriptWireItem =
  | { kind: "user"; id?: string; content: string; time?: string; images?: OmegaImage[]; attachments?: OmegaAttachment[] }
  | {
      kind: "message";
      id?: string;
      content: string;
      time?: string;
      response_ms?: number;
      running?: boolean;
      elapsed_ms?: number;
    }
  | {
      kind: "thinking";
      // the stored message's row id IS this row's address - never a rendered id (one
      // StoredMessage builds both items from it).
      id?: string;
      content: string;
      thinking_ms?: number;
      running?: boolean;
      elapsed_ms?: number;
    }
  | {
      kind: "tool_call";
      tool_call?: { id?: string; name?: string; arguments?: Record<string, unknown> };
      running?: boolean;
      elapsed_ms?: number;
      started_at?: string;
    }
  | {
      kind: "tool_result";
      tool_call_id?: string;
      content?: string;
      is_error?: boolean;
      gate?: string;
      elapsed_ms?: number;
      answer_ms?: number;
    };

// a tool row's meter offset at PAINT time; never negative (a reader clock behind the server's clamps to 0).
export function toolCallAnchorMs(
  startedAt: string | undefined,
  elapsedMs: number | undefined,
): number | undefined {
  if (startedAt) {
    const start = Date.parse(startedAt);
    if (!Number.isNaN(start)) return Math.max(0, Date.now() - start);
  }
  return elapsedMs;
}

// maps a transcript payload into the components' item shapes: a tool_call opens a tool line
// keyed by its id and the matching tool_result fills its result, error state and elapsed; thin
// items render nothing. A call whose result row is ABSENT renders as PENDING, not a done line:
// the backend only persists a call without its result when the call is yield-class. Pure -
// every call mints its own items; createRestoreTranscript is the keyed twin.
export function renderTranscriptItems(
  items: TranscriptWireItem[] | null | undefined,
): TranscriptItem[] {
  return mapTranscriptItems(items, null);
}

// IDENTITY LAW - ONE address rule for both render paths. A row's identity across refetches is
// its ADDRESS, never its position. The chat panel is a sliding window (40 rows,
// agent/api/chat_panel.go), so ONE append past it moves every surviving index: keyed
// positionally, every row misses its cache entry and the keyed v-for destroys it - worse, the
// tail row can inherit an existing row's object (another row's meter anchor, fold pin and
// streaming bit). Both paths resolve a row's address through this ONE function:
//   row:<store row id>:<shape>   the store has named the row
//   live:<shape>:<ordinal>       it has not yet (in-flight rows carry no id)
// <shape> is needed because one stored message renders TWO items under the SAME id.
// The ordinal counts ID-LESS rows only, so a head slide never moves it; slots are
// swept per paint.
export function transcriptAddress(
  id: string | undefined,
  shape: string,
  ordinals: Map<string, number>,
  consume = true,
): string {
  if (id) return `row:${id}:${shape}`;
  const ordinal = ordinals.get(shape) ?? 0;
  if (consume) ordinals.set(shape, ordinal + 1);
  return `live:${shape}:${ordinal}`;
}

// one wire item's shape, the suffix of its address; chat.ts derives the same `<role>:<type>` strings.
export function wireShape(kind: TranscriptWireItem["kind"]): string {
  switch (kind) {
    case "user":
      return "user:text";
    case "message":
      return "assistant:text";
    case "thinking":
      return "assistant:thinking";
    case "tool_call":
      return "assistant:tool_call";
    case "tool_result":
      return "tool:tool_result";
  }
}

function wireId(it: TranscriptWireItem): string | undefined {
  return "id" in it ? it.id : undefined;
}

interface RestorePaint {
  cache: Map<string, TranscriptItem>;
  slots: Map<string, TranscriptItem>;
  ordinals: Map<string, number>;
  usedSlots: Set<string>;
  lastIdOfShape: ReadonlyMap<string, number>;
}

// the item one wire row paints into: resolve the row's ADDRESS and reuse the object it owns.
// An id-bearing row that is its shape's LAST id-bearing row ADOPTS the slot its live object
// sits in - the live -> persisted transition must not swap the object, or the meter restarts.
// Cached items are reactive proxies, so in-place writes invalidate their readers.
function itemAt<T extends TranscriptItem>(
  paint: RestorePaint | null,
  id: string | undefined,
  shape: string,
  index: number,
  make: () => T,
): T {
  if (!paint) return make();
  const address = transcriptAddress(id, shape, paint.ordinals);
  if (!id) {
    paint.usedSlots.add(address);
    const hit = paint.slots.get(address) as T | undefined;
    if (hit) return hit;
    const fresh = reactive(make()) as T;
    paint.slots.set(address, fresh);
    return fresh;
  }
  const hit = paint.cache.get(address) as T | undefined;
  if (hit) return hit;
  if (paint.lastIdOfShape.get(shape) === index) {
    const slot = transcriptAddress(undefined, shape, paint.ordinals, false);
    const adopted = paint.slots.get(slot) as T | undefined;
    if (adopted) {
      paint.slots.delete(slot);
      paint.cache.set(address, adopted);
      return adopted;
    }
  }
  const fresh = reactive(make()) as T;
  paint.cache.set(address, fresh);
  return fresh;
}

function itemByKey<T extends TranscriptItem>(
  paint: RestorePaint | null,
  key: string,
  live: boolean,
  make: () => T,
): T {
  if (!paint) return make();
  if (live) {
    paint.usedSlots.add(key);
    const hit = paint.slots.get(key) as T | undefined;
    if (hit) return hit;
    const fresh = reactive(make()) as T;
    paint.slots.set(key, fresh);
    return fresh;
  }
  const hit = paint.cache.get(key) as T | undefined;
  if (hit) return hit;
  const fresh = reactive(make()) as T;
  paint.cache.set(key, fresh);
  return fresh;
}

function callKey(
  prefix: "tc" | "verdict",
  callId: string,
  paint: RestorePaint | null,
): { key: string; live: boolean } {
  if (!paint) return { key: "", live: true };
  if (callId) return { key: `${prefix}:${callId}`, live: false };
  return { key: `${prefix}:${transcriptAddress(undefined, wireShape("tool_call"), paint.ordinals)}`, live: true };
}

function lastIdOfShape(items: TranscriptWireItem[] | null | undefined): Map<string, number> {
  const last = new Map<string, number>();
  items?.forEach((it, i) => {
    if (wireId(it)) last.set(wireShape(it.kind), i);
  });
  return last;
}

// the restore path's render source: the same mapping over the previous pass's items, so a refetch updates in place.
export interface RestoreTranscript {
  apply(items: TranscriptWireItem[] | null | undefined, session?: string): TranscriptItem[];
}

export function createRestoreTranscript(): RestoreTranscript {
  let session = "";
  const cache = new Map<string, TranscriptItem>();
  const slots = new Map<string, TranscriptItem>();
  return {
    apply(items, next) {
      const sid = next ?? "";
      if (sid !== session) {
        session = sid;
        cache.clear();
        slots.clear();
      }
      const paint: RestorePaint = {
        cache,
        slots,
        ordinals: new Map(),
        usedSlots: new Set(),
        lastIdOfShape: lastIdOfShape(items),
      };
      const out = mapTranscriptItems(items, paint);
      for (const key of slots.keys()) {
        if (!paint.usedSlots.has(key)) slots.delete(key);
      }
      return out;
    },
  };
}

function mapTranscriptItems(
  items: TranscriptWireItem[] | null | undefined,
  paint: RestorePaint | null,
): TranscriptItem[] {
  const out: TranscriptItem[] = [];
  if (!items) return out;
  const resultIds = new Set<string>();
  for (const it of items) {
    if (it.kind === "tool_result" && it.tool_call_id) resultIds.add(it.tool_call_id);
  }
  const toolById = new Map<string, ToolItem>();
  const verdictById = new Map<string, VerdictItem>();
  for (let index = 0; index < items.length; index++) {
    const it = items[index]!;
    switch (it.kind) {
      case "user": {
        if (!(it.content || it.images?.length || it.attachments?.length)) break;
        const m = itemAt<MessageItem>(paint, it.id, wireShape(it.kind), index, () => ({
          kind: "message",
          role: "user",
          content: "",
        }));
        m.kind = "message";
        m.role = "user";
        m.content = it.content;
        m.time = it.time;
        m.images = it.images;
        m.attachments = it.attachments;
        m.id = it.id;
        out.push(m);
        break;
      }
      case "message": {
        if (!it.content) break;
        const m = itemAt<MessageItem>(paint, it.id, wireShape(it.kind), index, () => ({
          kind: "message",
          role: "assistant",
          content: "",
        }));
        m.kind = "message";
        m.role = "assistant";
        m.content = it.content;
        m.time = it.time;
        m.finalMs = it.response_ms;
        m.id = it.id;
        // only the merge's live tail streams from restore; a streaming segment's elapsed-so-far lets a remounted meter count on.
        m.streaming = it.running ? true : undefined;
        m.elapsedMs = it.running ? it.elapsed_ms : undefined;
        out.push(m);
        break;
      }
      case "thinking": {
        if (!it.content) break;
        // the id here is this row's ADDRESS, never stamped: an id on a thinking item moves variantAnchors' head resolution.
        const think = itemAt<ThinkingItem>(paint, wireId(it), wireShape(it.kind), index, () => ({
          kind: "thinking",
          content: "",
        }));
        think.kind = "thinking";
        think.content = it.content;
        think.finalMs = it.thinking_ms;
        think.streaming = it.running ? true : undefined;
        think.elapsedMs = it.running ? it.elapsed_ms : undefined;
        out.push(think);
        break;
      }
      case "tool_call": {
        const callId = it.tool_call?.id || "";
        // an ABSENT result on an idle session is the yield-class pending chip, not a done line; running outranks it.
        const pending = !!callId && !resultIds.has(callId);
        // a verdict call renders as a verdict card and NO tool line - the card is the surface AND the record.
        if (isVerdictTool(it.tool_call?.name)) {
          const { key, live } = callKey("verdict", callId, paint);
          const built = verdictCardFromToolArgs(it.tool_call?.arguments);
          const card = itemByKey<VerdictItem>(paint, key, live, () => built);
          card.kind = "verdict";
          card.title = built.title;
          card.body = built.body;
          card.options = built.options;
          card.multi = built.multi;
          card.allowText = built.allowText;
          card.timeoutS = built.timeoutS;
          card.callId = callId || undefined;
          card.status = "pending"; // settled by the tool_result below
          // the countdown's anchor is the server's start stamp, a fixed instant, so re-deriving it never drifts.
          card.issuedAt = verdictIssuedAt(it.started_at);
          if (callId) verdictById.set(callId, card);
          out.push(card);
          break;
        }
        const { key, live } = callKey("tc", callId, paint);
        const t = itemByKey<ToolItem>(paint, key, live, () => ({
          kind: "tool",
          name: "tool",
          status: "done",
        }));
        t.kind = "tool";
        t.name = it.tool_call?.name || "tool";
        t.callId = callId || undefined;
        t.status =
          it.running ? "running"
          : pending ? "pending"
          : "done";
        t.output =
          it.tool_call?.arguments ?
            JSON.stringify(it.tool_call.arguments, null, 2)
          : undefined;
        // a RUNNING or PENDING call meters from its open wall clock; a yielded call pushes no delta, so restored elapsed_ms is stale.
        t.elapsedMs =
          it.running || pending ?
            toolCallAnchorMs(it.started_at, it.elapsed_ms)
          : undefined;
        if (callId) toolById.set(callId, t);
        out.push(t);
        break;
      }
      case "tool_result": {
        const v = it.tool_call_id ? verdictById.get(it.tool_call_id) : undefined;
        if (v) {
          const settled = verdictFromResultText(it.content || "");
          v.status = "settled";
          v.verdictKind = settled.verdictKind || "expired";
          v.picked = settled.picked;
          v.answerText = settled.answerText;
          break;
        }
        if (!it.tool_call_id) break;
        const t = toolById.get(it.tool_call_id);
        if (!t) break;
        t.result = it.content || "";
        t.status = it.is_error ? "error" : "done";
        t.gate = it.gate;
        t.finalMs = it.elapsed_ms;
        t.answerMs = it.answer_ms;
        t.elapsedMs = undefined;
        break;
      }
    }
  }
  applyTurnTotals(out);
  return out;
}

// an unanswered yield-class call: a tool chip or a pending verdict card, the SAME contract. A
// pending card must arm a watcher too, or the wake's frames land on nobody and nothing paints.
export function awaitsWake(it: TranscriptItem | undefined): boolean {
  if (!it) return false;
  if (it.kind === "tool") return isYielded(it.status);
  return it.kind === "verdict" && it.status === "pending";
}

// stamps each turn's ANSWER row with the turn's wall-clock total: user message ARRIVING to
// final answer LANDING, off the two created_at stamps the transcript already carries. ONLY the
// answer row - earlier prose segments are PHASES, and a tool call (or a verdict card, which IS a
// call) below a segment means it was a phase. Recomputed at every paint, so live and restored
// agree; a missing stamp leaves the row its own span, never a fabricated number. Mutates in
// place (the item objects ARE the render identity) and is idempotent.
export function applyTurnTotals(items: TranscriptItem[]): TranscriptItem[] {
  let from = 0;
  for (let i = 0; i <= items.length; i++) {
    const it = items[i];
    const boundary = i === items.length || (it?.kind === "message" && it.role === "user");
    if (!boundary) continue;
    stampTurnTotal(items, from, i);
    from = i;
  }
  return items;
}

// WRITE LAW: this runs inside the canvas PAINT, over the transcript's REACTIVE cached items, so
// every assignment is a reactive write on the objects the paint handed out. A write that changes
// a value invalidates every computed that read it - and the paint ran BECAUSE one of those
// computeds is dirty - so a value that differs between two paints of the same payload re-arms
// the paint and the app spins. Resolve the wanted value FIRST, assign ONLY on a real change: a
// paint of an unchanged payload must write nothing at all.
function stampTurnTotal(items: TranscriptItem[], from: number, to: number): void {
  const head = items[from];
  if (!head || head.kind !== "message" || head.role !== "user") return;
  let answerAt = -1;
  for (let i = from + 1; i < to; i++) {
    const it = items[i];
    if (it?.kind !== "message" || it.role !== "assistant") continue;
    answerAt = i;
  }
  if (answerAt < 0) return;
  let continues = false;
  for (let i = answerAt + 1; i < to; i++) {
    const kind = items[i]?.kind;
    if (kind === "tool" || kind === "verdict") {
      continues = true;
      break;
    }
  }
  let total: number | undefined;
  if (!continues) {
    const start = stampMs(head.time);
    const end = stampMs((items[answerAt] as MessageItem).time);
    total = start !== null && end !== null && end >= start ? end - start : undefined;
  }
  for (let i = from + 1; i < to; i++) {
    const it = items[i];
    if (it?.kind !== "message" || it.role !== "assistant") continue;
    const want = i === answerAt ? total : undefined;
    if (it.turnMs !== want) it.turnMs = want;
  }
}

// a row stamp (RFC 3339) as epoch; null for anything unparseable - never NaN in the label.
function stampMs(stamp: string | undefined): number | null {
  if (!stamp) return null;
  const ms = Date.parse(stamp);
  return Number.isNaN(ms) ? null : ms;
}

export function transcriptHasPending(items: readonly TranscriptItem[]): boolean {
  return items.some(awaitsWake);
}

export function transcriptPendingCount(items: readonly TranscriptItem[]): number {
  let n = 0;
  for (const it of items) if (awaitsWake(it)) n++;
  return n;
}


export interface ChatBlockWire {
  type: string; // text | thinking | tool_call | tool_result
  content?: string;
  images?: OmegaImage[];
  attachments?: OmegaAttachment[];
  running?: boolean; // the in-flight block of a live stream
  tool_call?: { id?: string; name?: string; arguments?: unknown };
  tool_call_id?: string;
  is_error?: boolean;
  gate?: string; // the smart gate's decision record
  elapsed_ms?: number;
  started_at?: string;
  answer_ms?: number; // response-return duration
  response_ms?: number;
  thinking_ms?: number;
}

export interface ChatRowWire {
  id?: string; // store row id when persisted; live rows carry none
  role: string; // user | assistant | tool
  status: string; // streaming | complete
  time?: string; // persisted row's created_at (absent on in-flight rows)
  blocks?: ChatBlockWire[];
}

export interface ChatPayloadWire {
  session?: string;
  running?: boolean;
  rows?: ChatRowWire[];
}

export function normalizeAttachments(raw: unknown): OmegaAttachment[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const out: OmegaAttachment[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue;
    const r = item as Record<string, unknown>;
    if (typeof r.kind !== "string" || typeof r.name !== "string" || !r.name) continue;
    if (typeof r.size !== "number") continue;
    out.push({
      kind: r.kind,
      name: r.name,
      size: r.size,
      path: typeof r.path === "string" ? r.path : "",
    });
  }
  return out.length ? out : undefined;
}

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
            attachments: normalizeAttachments(b.attachments),
            running: b.running === true ? true : undefined,
            tool_call:
              b.tool_call && typeof b.tool_call === "object"
                ? (b.tool_call as ChatBlockWire["tool_call"])
                : undefined,
            tool_call_id: typeof b.tool_call_id === "string" ? b.tool_call_id : undefined,
            is_error: b.is_error === true ? true : undefined,
            gate: typeof b.gate === "string" && b.gate ? b.gate : undefined,
            elapsed_ms: typeof b.elapsed_ms === "number" ? b.elapsed_ms : undefined,
            started_at: typeof b.started_at === "string" && b.started_at ? b.started_at : undefined,
            answer_ms: typeof b.answer_ms === "number" ? b.answer_ms : undefined,
            response_ms: typeof b.response_ms === "number" ? b.response_ms : undefined,
            thinking_ms: typeof b.thinking_ms === "number" ? b.thinking_ms : undefined,
          });
        }
      }
      rows.push({
        id: typeof r.id === "string" && r.id ? r.id : undefined,
        role: r.role,
        status: r.status,
        time: typeof r.time === "string" && r.time ? r.time : undefined,
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

export function chatPanelOf(panels: CanvasPanel[] | null | undefined): CanvasPanel | null {
  for (const p of panels ?? []) {
    if (p.kind === "chat") return p;
  }
  return null;
}

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

export function chatPanelMessageCount(payload: ChatPayloadWire): number {
  let total = 0;
  for (const row of payload.rows ?? []) {
    if (row.role === "user") total++;
    else if (row.role === "assistant" && row.blocks?.[0]?.type === "text") total++;
  }
  return total;
}

// still-pending tool calls in the payload, over the OPEN session only (the caller gates on `payload.session`).
export function chatPanelPendingCount(payload: ChatPayloadWire): number {
  const settled = new Set<string>();
  for (const row of payload.rows ?? [])
    for (const b of row.blocks ?? [])
      if (b.type === "tool_result" && b.tool_call_id) settled.add(b.tool_call_id);
  let total = 0;
  for (const row of payload.rows ?? [])
    for (const b of row.blocks ?? []) {
      if (b.type !== "tool_call") continue;
      const id = b.tool_call?.id;
      if (!id || b.running === true) continue;
      if (!settled.has(id)) total++;
    }
  return total;
}


// VERDICT_TOOL_NAMES is the WIRE vocabulary of the card surface. A name comparison at each
// call site is how a rename silently kills the surface, with nothing failing. Add a new dotted
// verdict verb HERE, never there.
export const VERDICT_TOOL_NAMES: readonly string[] = ["verdict.request"];

export function isVerdictTool(name: string | undefined): boolean {
  return !!name && VERDICT_TOOL_NAMES.includes(name);
}

export function verdictIssuedAt(startedAt: string | undefined): number | undefined {
  if (!startedAt) return undefined;
  const at = Date.parse(startedAt);
  return Number.isNaN(at) ? undefined : at;
}

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

export async function fetchVerdicts(sessionId = ""): Promise<{ cards: PendingVerdictCard[]; count: number }> {
  const url = sessionId
    ? `/sessions/${encodeURIComponent(sessionId)}/verdicts`
    : `/verdicts`;
  const r = await fetch(url);
  if (r.status === 501) return { cards: [], count: 0 }; // seam unmounted
  if (!r.ok) throw new Error(`verdicts: ${r.status}`);
  return r.json();
}

// records the answer on one card; a 409 (the race was lost) throws the server's reason so the loser shows it.
export async function postVerdict(
  sessionId: string,
  body: { card_id?: string; option_indexes?: number[]; option_index?: number; option?: string; text?: string; message?: string; skip?: boolean },
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


export function approveDenyOptions(
  options: VerdictOptionItem[] | undefined,
): { approve: number; deny: number } | null {
  let approve = 0;
  let deny = 0;
  (options ?? []).forEach((o, i) => {
    const label = (o.label || "").trim().toLowerCase();
    if (!approve && label === "approve") approve = i + 1;
    if (!deny && label === "deny") deny = i + 1;
  });
  return approve && deny ? { approve, deny } : null;
}

export function gateAnswerBody(
  card: { id?: string; options?: VerdictOptionItem[] },
  kind: "approve" | "deny" | "followup",
  text = "",
): Parameters<typeof postVerdict>[1] {
  const ix = approveDenyOptions(card.options) ?? { approve: 1, deny: 2 };
  const body: Parameters<typeof postVerdict>[1] = {};
  if (card.id) body.card_id = card.id;
  body.option_index = kind === "approve" ? ix.approve : ix.deny;
  if (kind === "followup" && text.trim() !== "") body.message = text.trim();
  return body;
}

export function isSettledRace(message: string): boolean {
  return (
    message.includes("already answered") ||
    message.includes("already skipped") ||
    message.includes("already expired")
  );
}

export function toolCallCommand(item: ToolItem): string {
  if (!item.output) return "";
  try {
    const j = JSON.parse(item.output) as Record<string, unknown>;
    return typeof j.command === "string" ? j.command : "";
  } catch {
    return "";
  }
}

export interface GateBinding {
  assigned: { item: ToolItem; card: PendingVerdictCard }[];
  standalone: PendingVerdictCard[];
}

export function bindGateQuestions(
  items: TranscriptItem[],
  cards: PendingVerdictCard[],
  sessionId: string | null | undefined,
): GateBinding {
  const out: GateBinding = { assigned: [], standalone: [] };
  if (!sessionId) return out;
  const mine = cards.filter((c) => c.session_id === sessionId);
  if (!mine.length) return out;
  // a pending verdict-tool card IS the question this run awaits: answerable exactly while its call has no result row.
  if (items.some((it) => it.kind === "verdict" && it.status === "pending")) {
    return out;
  }
  const running = items.filter(
    // isStreaming, NOT isLive - a named decision (composables/toolStatus.ts).
    (it): it is ToolItem =>
      it.kind === "tool" && isStreaming(it.status) && it.name === "shell.run",
  );
  const taken = new Set<ToolItem>();
  for (const card of mine) {
    const cmd = card.title; // the command preview
    let target: ToolItem | undefined;
    for (const it of running) {
      if (taken.has(it)) continue;
      const command = toolCallCommand(it);
      if (command && command.slice(0, 100) === cmd.slice(0, 100)) {
        target = it;
        break;
      }
    }
    if (!target) {
      target = running.find((it) => !taken.has(it));
    }
    if (target) {
      taken.add(target);
      out.assigned.push({ item: target, card });
    } else {
      out.standalone.push(card);
    }
  }
  return out;
}


export type Appearance = "chat" | "stacked";

export const APPEARANCE_KEY = "omega.appearance";

export function parseAppearance(v: unknown): Appearance {
  return v === "stacked" ? "stacked" : "chat";
}

// cuts where the backend's segmented persistence does: a live stream appends into the CURRENT block, so the user block holds the answer.
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

// resolves WHERE a branch block's switcher renders. The branch point's siblings are each run's
// HEAD row id (DeriveTree), and that head can be a thinking or tool row, which renders as a
// block. One anchor per branch: the head row when it is an assistant bubble; else the run's last
// stored assistant bubble; else the head row itself. Live items (no id) never anchor.
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

// the one-indexed « 2/3 » math, clamped so a stale id can never render 4/3.
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
  capabilities?: Record<string, string[]>;
  context_window?: number;
  providers?: ModelProviderSection[];
}

export interface ModelProviderSection {
  name: string;
  models: string[];
  capabilities?: Record<string, string[]>;
  error?: string;
}

export interface StreamHandlers {
  onUsage?: (usage: {
    promptTokens: number;
    completionTokens: number;
    /** null when the provider reported no cache figure for this response */
    cachedTokens: number | null;
  }) => void;
  onRunEnd?: (finishReason: string) => void;
  onSession?: (sessionId: string) => void;
  onViewModelDelta?: (panel: CanvasPanel) => void;
  /** the send was not a run: a live turn took it as a steer and these rows are its queue */
  onQueued?: (rows: QueuedRow[]) => void;
  /** one queue row reached the live turn - the surface retires it by id */
  onSteerDelivered?: (rowId: string) => void;
}

/** QueuedRow is one steer waiting for the live turn's next batch boundary. */
export interface QueuedRow {
  id: string;
  content: string;
  at: string;
}

export function parseFrame(frame: string): unknown | null {
  const dataLine = frame.split("\n").find((l) => l.startsWith("data: "));
  if (!dataLine) return null;
  try {
    return JSON.parse(dataLine.slice(6));
  } catch {
    return null;
  }
}

export function handlePayload(
  payload: unknown,
  handlers: StreamHandlers,
  state: { sawEnd?: boolean },
): void {
  const p = payload as Record<string, unknown>;
  switch (p.type) {
    case "thinking_chunk":
      break;
    case "response_chunk":
      break;
    case "tool_call":
    case "tool_result":
      break;
    case "stream_end":
      if (p.prompt_eval_count != null || p.eval_count != null) {
        handlers.onUsage?.({
          promptTokens: (p.prompt_eval_count as number) || 0,
          completionTokens: (p.eval_count as number) || 0,
          // an ABSENT cached_count is not zero: the provider reported no cache
          // fact, and 0% would claim a measured miss.
          cachedTokens: p.cached_count == null ? null : Number(p.cached_count),
        });
      }
      break;
    case "agent_end": {
      // the terminal event: only its arrival makes the stream end CLEAN.
      state.sawEnd = true;
      // "yielded" is not an error shape - the run handed its last call to the wake path, and the host arms its watcher here.
      handlers.onRunEnd?.(String(p.finish_reason || ""));
      // a cancelled run unwinds through the loop's normal path: surface it as interrupted.
      if (p.finish_reason === "cancelled" && p.error === "context canceled") {
        throw new StreamInterruptedError("run cancelled");
      }
      break;
    }
    case "view_model_delta": {
      const panel = normalizeCanvasPanel(p.panel);
      if (panel) handlers.onViewModelDelta?.(panel);
      break;
    }
    case "steer_delivered": {
      // A queued row reached the live turn: it is ordinary user content now, and the row
      // leaves the queue surface.
      const id = typeof p.id === "string" ? p.id : "";
      if (id) handlers.onSteerDelivered?.(id);
      break;
    }
    case "error":
      throw new Error(String(p.error || "agent error"));
  }
}

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

export function anyBusy(b: BusyMap): boolean {
  return b.size > 0;
}

export function runningIds(list: Pick<Session, "id" | "running">[]): Set<string> {
  const ids = new Set<string>();
  for (const s of list) if (s.running) ids.add(s.id);
  return ids;
}


export function parseSessionPath(p: string): string | null {
  if (!p.startsWith("/chat/")) return null;
  const id = p.slice("/chat/".length);
  if (!id || id.includes("/")) return null;
  return id;
}

export function sessionPath(id: string): string {
  return `/chat/${id}`;
}

// the client path naming the settings tab, same scheme as /chat/<id> with no id part. The
// TRAILING SLASH is the contract: the settings API owns the bare `/settings` noun, and the
// catch-all hands every unclaimed path to the SPA shell.
export const SETTINGS_PATH = "/settings/";

export function isSettingsPath(p: string): boolean {
  return p === SETTINGS_PATH;
}

export function settingsAddress(search: string): string {
  if (!search || search === "?") return SETTINGS_PATH;
  return `${SETTINGS_PATH}${search.startsWith("?") ? search : `?${search}`}`;
}

export const mainViewModules = new Set<string>(["kanban"]);


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

export function formatTokens(n: number): string {
  if (n < 1000) return String(n);
  const k = Math.floor(n / 100) / 10;
  return `${k}k`;
}

export function shortVersion(v: string): string {
  const m = /^(v[^-\s]+)-0\.\d+-([0-9a-f]+)/.exec(v);
  return m ? `${m[1]} ${m[2].slice(0, 7)}` : v;
}

function byNewest(a: Session, b: Session): number {
  const ta = Date.parse(a.updated_at);
  const tb = Date.parse(b.updated_at);
  if (Number.isNaN(ta)) return 1;
  if (Number.isNaN(tb)) return -1;
  return tb - ta;
}

export interface SessionBuckets {
  pinned: Session[];
  today: Session[];
  yesterday: Session[];
  earlier: Session[];
  archived: Session[];
}

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
    if (s.archived) {
      out.archived.push(s);
      continue;
    }
    if (s.pinned) {
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


// malformed JSON and non-string entries are ignored: a bad key restores everything expanded, never a blank pane.
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

export function serializeCollapsedKeys(set: Iterable<string>): string {
  return JSON.stringify([...set]);
}

// the only labels a section can take; anything else persisted is stale or corrupt and drops.
export const SIDEBAR_BUCKETS = ["pinned", "today", "yesterday", "earlier", "archived"];

// listedSessions flattens the buckets in the order the pane RENDERS them (SIDEBAR_BUCKETS,
// newest first inside each). /resume's #N counts these rows, so a number a human typed
// lands on the row they were looking at - never on the sessions-table's created_at order.
export function listedSessions(sessions: Session[], now: Date = new Date()): Session[] {
  const buckets = bucketSessions(sessions, now);
  return SIDEBAR_BUCKETS.flatMap((key) => buckets[key as keyof SessionBuckets]);
}

export function parseCollapsedLabels(raw: string | null): Set<string> {
  const all = parseCollapsedKeys(raw);
  return new Set([...all].filter((v) => SIDEBAR_BUCKETS.includes(v)));
}


export type RowStatusShape = "dot" | "star";
export type RowStatusColor = "idle" | "streaming" | "unread";

export interface RowStatus {
  shape: RowStatusShape;
  color: RowStatusColor;
}

// the viewed session never reads unread: the open is the read (the PATCH confirms it).
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


export interface FileEntry {
  name: string;
  size_bytes: number;
  is_dir: boolean;
  mod_time: string;
}

export interface FilesBrowsePayload {
  entries: FileEntry[];
  root?: string;
  source?: "session" | "default" | "cwd";
}

export interface FilesReadPayload {
  content: string;
  truncated: boolean;
  binary: boolean;
  mime?: string;
  size?: number;
}

export function filesRawUrl(
  path: string,
  session?: string,
  opts?: { download?: boolean },
): string {
  const q = new URLSearchParams();
  q.set("path", path);
  if (session) q.set("session", session);
  if (opts?.download) q.set("download", "1");
  return `/files/raw?${q.toString()}`;
}

// ONE viewer body per content class: the mime decides pdf/image (sniffed bytes, never the extension), else markdown/monospace.
export type FileViewKind = "markdown" | "pdf" | "image" | "code" | "binary";

export function fileViewKind(name: string, mime: string, binary: boolean): FileViewKind {
  if (mime === "application/pdf") return "pdf";
  if (mime.startsWith("image/")) return "image";
  if (binary) return "binary";
  const dot = name.lastIndexOf(".");
  const ext = dot >= 0 ? name.slice(dot + 1).toLowerCase() : "";
  if (ext === "md" || ext === "markdown") return "markdown";
  return "code";
}

export function bytesLabel(n?: number): string {
  if (n === undefined || n === null) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 ** 2) return `${Math.round((n / 1024) * 10) / 10} KiB`;
  if (n < 1024 ** 3) return `${Math.round((n / 1024 ** 2) * 10) / 10} MiB`;
  return `${Math.round((n / 1024 ** 3) * 10) / 10} GiB`;
}

export function cleanRelPath(p: string): string | null {
  if (p === "") return "";
  if (p.startsWith("/")) return null;
  const parts = p.split("/");
  if (parts.some((seg) => !seg || seg === "." || seg === "..")) return null;
  return parts.join("/");
}

export function joinRelPath(base: string, name: string): string {
  return base ? `${base}/${name}` : name;
}


export const FILETREE_STATE_KEY = "omega.filetree-expanded";

// malformed JSON parses empty and every entry must be a valid relative path (the backend's /files rules).
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

export function serializeExpandedPaths(paths: Iterable<string>): string {
  const valid = [...paths].filter((p) => p !== "" && cleanRelPath(p) === p);
  valid.sort();
  return JSON.stringify(valid);
}

// orders a directory listing. Pure - sorts a copy, never the caller's array.
export function sortFileEntries(entries: FileEntry[]): FileEntry[] {
  return [...entries].sort((a, b) => {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
    return a.name.toLowerCase().localeCompare(b.name.toLowerCase());
  });
}


export function createTargetDir(
  selected?: { path: string; isDir: boolean } | null,
): string {
  if (!selected) return "";
  if (selected.isDir) return selected.path;
  const cut = selected.path.lastIndexOf("/");
  return cut >= 0 ? selected.path.slice(0, cut) : "";
}

export function revealAncestors(path: string): string[] {
  const parts = path.split("/");
  const out: string[] = [];
  for (let i = 1; i < parts.length; i++) {
    out.push(parts.slice(0, i).join("/"));
  }
  return out;
}

export interface SelectedNode {
  path: string;
  isDir: boolean;
}

// the ONE seam to the FileTree's internal expansion store. Counters never reset (like a nonce).
export const fileTreeBus = reactive({
  refresh: 0,
  collapse: 0,
  reveal: "",
});

async function filesQuery<T>(url: string): Promise<T> {
  const r = await fetch(url);
  if (!r.ok) {
    let msg = `request: ${r.status}`;
    try {
      const body = await r.json();
      if (body?.error) msg = body.error;
    } catch {
    }
    throw new Error(msg);
  }
  return r.json();
}

// lists one directory of the RESOLVED root; the server resolves it from the session row / default_workspace / cwd chain.
export function fetchFilesBrowse(path = "", session?: string): Promise<FilesBrowsePayload> {
  const q = new URLSearchParams();
  if (path) q.set("path", path);
  if (session) q.set("session", session);
  const suffix = q.toString() ? `?${q.toString()}` : "";
  return filesQuery(`/files/browse${suffix}`);
}

export function filesRootName(root: string): string {
  const base = root.split(/[\\/]/).filter(Boolean).pop();
  return base ?? root;
}

export function filesHeaderVisible(source: string | undefined): boolean {
  return source === "session" || source === "default";
}

// the read the /files/browse jail (CWD-rooted, relative paths) cannot make.
export async function fetchWorkspaceFiles(path: string): Promise<FilesBrowsePayload> {
  const r = await fetch(`/workspaces/files?path=${encodeURIComponent(path)}`);
  if (!r.ok) {
    const text = (await r.text()).trim();
    throw new Error(text || `workspace files: ${r.status}`);
  }
  const body = (await r.json()) as { entries?: { name: string; is_dir: boolean }[] };
  return {
    entries: (body.entries ?? []).map((e) => ({
      name: e.name,
      size_bytes: 0,
      is_dir: e.is_dir,
      mod_time: "",
    })),
  };
}

// up to 64 KiB, a truncated flag, binary=true for unrenderable bytes; mime is the server's sniff, so /files/raw can bind.
export function fetchFilesRead(path: string, session?: string): Promise<FilesReadPayload> {
  const q = new URLSearchParams();
  q.set("path", path);
  if (session) q.set("session", session);
  return filesQuery(`/files/read?${q.toString()}`);
}


async function filesPost(url: string, body: unknown): Promise<void> {
  const r = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    let msg = `request: ${r.status}`;
    try {
      const j = (await r.json()) as { error?: string };
      if (j?.error) msg = j.error;
    } catch {
    }
    throw new Error(msg);
  }
}

export function postFilesWrite(
  path: string,
  content: string,
  session?: string,
): Promise<void> {
  return filesPost("/files/write", session ? { path, content, session } : { path, content });
}

export function postFilesMkdir(path: string, session?: string): Promise<void> {
  return filesPost("/files/mkdir", session ? { path, session } : { path });
}

// postFilesRename moves one node's leaf in place (POST /files/rename {path, name}).
export function postFilesRename(path: string, name: string, session?: string): Promise<void> {
  return filesPost("/files/rename", session ? { path, name, session } : { path, name });
}

// postFilesDelete removes one node (POST /files/delete {path}); the root refuses server-side.
export function postFilesDelete(path: string, session?: string): Promise<void> {
  return filesPost("/files/delete", session ? { path, session } : { path });
}

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

export async function fetchInsights(days: number | null = null): Promise<InsightsPayload> {
  const suffix = days === null ? "" : `?days=${days}`;
  const p = await filesQuery<InsightsPayload>(`/insights${suffix}`);
  return {
    ...p,
    tools: p.tools ?? [],
    daily: p.daily ?? [],
  };
}


export interface CanvasPanelAction {
  id: string;
  label: string;
  module: string;
}

// one panel descriptor: schema v1 exactly; the payload is opaque to the contract.
export interface CanvasPanel {
  id: string;
  title: string;
  kind: string;
  payload: unknown;
  actions: CanvasPanelAction[];
}

// malformed panels drop, label-less actions fall back to their id, an absent payload is null.
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

// the panel updates through the deltas, never through this response.
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
    }
    throw new Error(msg);
  }
  const j = (await r.json()) as { result?: unknown };
  return j.result;
}


const MONTHS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun",
  "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

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


export const ELAPSED_TICK_MS = 1000;

const pad2 = (n: number) => String(n).padStart(2, "0");

export function formatElapsed(startedMs: number, nowMs: number): string {
  const delta = Math.max(0, nowMs - startedMs);
  const total = Math.floor(delta / 1000);
  if (total < 1) return `${Math.round(delta)}ms`;
  if (total < 60) return `${total}s`;
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h}h ${pad2(m)}m`;
  return `${m}m ${pad2(s)}s`;
}

export function formatDurationMs(ms: number): string {
  return formatElapsed(0, ms);
}

export interface ElapsedMeter {
  start(): void;
  stop(): void;
  // reanchor REPLACES the consumed span; monotone, so a stale (smaller) span is a no-op.
  reanchor(initialMs: number): void;
}

export function createElapsedMeter(
  onElapsed: (label: string) => void,
  now: () => number = () => Date.now(),
  timers: RenderTimers = {
    set: (fn, ms) => setTimeout(fn, ms),
    clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>),
  },
  initialMs = 0,
): ElapsedMeter {
  let base = Math.max(0, initialMs);
  let startedAt: number | null = null;
  let running = false;
  let handle: unknown = null;
  const fire = () => {
    handle = null;
    if (startedAt === null) return;
    onElapsed(formatElapsed(startedAt, now()));
    if (handle === null) handle = timers.set(fire, ELAPSED_TICK_MS);
  };
  return {
    start() {
      running = true;
      if (startedAt !== null) return;
      startedAt = now() - base;
      onElapsed(formatElapsed(startedAt, now()));
      if (handle === null) handle = timers.set(fire, ELAPSED_TICK_MS);
    },
    stop() {
      running = false;
      if (handle !== null) timers.clear(handle);
      handle = null;
      if (startedAt === null) return;
      onElapsed(formatElapsed(startedAt, now()));
    },
    reanchor(next: number) {
      // REPLACE, never add: the offset is the call's elapsed-so-far, not a delta.
      const ms = Math.max(0, next);
      if (!(ms > base)) return;
      base = ms;
      if (!running) return;
      startedAt = now() - base;
      onElapsed(formatElapsed(startedAt, now()));
    },
  };
}


export const SCROLL_NEAR_PX = 40;

// the shared near-bottom check. A container that cannot scroll counts as at bottom.
export function isAtBottom(
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
  threshold: number = SCROLL_NEAR_PX,
): boolean {
  if (scrollHeight <= clientHeight) return true;
  return scrollHeight - scrollTop - clientHeight < threshold;
}

export function pillVisible(
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
  threshold: number = SCROLL_NEAR_PX,
): boolean {
  return !isAtBottom(scrollTop, clientHeight, scrollHeight, threshold);
}

// the guard checks against the height the LAST scroll event saw: growth cannot shrink, a fold can.
export function resumesFollow(
  prevPinned: boolean,
  lastScrollHeight: number,
  scrollTop: number,
  clientHeight: number,
  scrollHeight: number,
  threshold: number = SCROLL_NEAR_PX,
): boolean {
  if (prevPinned) {
    return isAtBottom(scrollTop, clientHeight, scrollHeight, threshold);
  }
  return scrollHeight >= lastScrollHeight && isAtBottom(scrollTop, clientHeight, scrollHeight, threshold);
}

// requestAnimationFrame is frozen in a hidden document, so the tail would stop being followed.
export type ScrollFlushTick = "raf" | "timer";
export function scrollFlushTick(hidden: boolean): ScrollFlushTick {
  return hidden ? "timer" : "raf";
}


function highlightFence(code: string, lang: string): string {
  if (!lang || !hljs.getLanguage(lang)) return "";
  try {
    const value = hljs.highlight(code, { language: lang, ignoreIllegals: true }).value;
    return `<pre><code class="hljs language-${lang}">${value}</code></pre>`;
  } catch {
    return "";
  }
}

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

// Standard markdown-it, no plugins, no custom rules. html:false is the sanitizer
// contract: raw HTML in model output is escaped to text by the lib's default path,
// never executed.
const markdown = new MarkdownIt({ html: false, highlight: highlightFence });
// The streaming twin: identical grammar, no highlighter - used while the
// source ends inside an unclosed fence so a mid-stream snapshot renders
// plain (unstyled) code and never highlights half a token stream.
const markdownPlain = new MarkdownIt({ html: false });

export function renderMarkdown(src: string): string {
  return (endsInOpenFence(src) ? markdownPlain : markdown).render(src);
}

// Debounced render tick for streaming markdown. Chunks only move the latest source
// snapshot; one timer renders once per tick (~150ms), so partial markdown never
// re-parses per chunk or flash-resizes mid-block. The final full render is the caller's
// direct renderMarkdown at stream end - see ChatMessage.
export const RENDER_TICK_MS = 150;

// Timers are injectable for tests.
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

// HealthInfo is the /health payload beyond the status string.
export interface HealthInfo {
  version?: string;
  uptime_seconds?: number;
  provider?: {
    reachable?: boolean;
    checked_at?: number;
    detail?: string;
  };
  database?: { healthy?: boolean; detail?: string };
  last_error?: { message?: string; at?: number };
}

export async function fetchHealthInfo(): Promise<HealthInfo | null> {
  try {
    const r = await fetch("/health");
    if (!r.ok) return null;
    return (await r.json()) as HealthInfo;
  } catch {
    return null;
  }
}

// persistedTelemetry picks the status bar's durable values from a session row: the
// row's token total (prompt + completion, exactly what the sidebar's tok readout shows)
// and the last-run average rates.
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

// the list ALWAYS carries both archive sides - one fetch
export async function fetchSessions(): Promise<Session[]> {
  const r = await fetch("/sessions?archived=include");
  if (!r.ok) throw new Error(`sessions: ${r.status}`);
  return r.json();
}

export interface OmegaExtension {
  name: string;
  description?: string;
  tools: number;
  commands: number;
  seams: string[];
  tool_names?: string[];
  command_names?: string[];
}

export async function fetchExtensions(): Promise<OmegaExtension[]> {
  const r = await fetch("/extensions");
  if (!r.ok) throw new Error(`extensions: ${r.status}`);
  return r.json();
}

export interface SettingKnob {
  key: string;
  label: string;
  type:
    | "toggle"
    | "number"
    | "text"
    | "secret"
    | "select"
    | "textarea"
    | "bindlist"
    | "model"
    | "providers";
  value: unknown;
  // default is the knob's effective default. The settings pane's modified dot and
  // reset-to-default compare against exactly this field.
  default?: unknown;
  options?: (string | { value: string; label: string })[];
  filter?: "decision" | "vision";
  slot?: boolean;
  // defaultOption marks an AUXILIARY model knob whose picker offers the "default"
  // entry. The choice persists as the literal word "default", never a frozen model
  // name.
  defaultOption?: boolean;
  min?: number;
  max?: number;
  step?: number;
  help?: string;
  live?: boolean;
  // subgroup is the settings NAV's nesting declaration. The pane stamps it from ONE
  // data table (components/settings/settingsNav.ts KNOB_SUBGROUPS) - the server never
  // sends it, because the chat.* and dock.* knobs have no server schema row.
  subgroup?: string;
}

// ProviderInstanceRow is one providers-knob row.
export interface ProviderInstanceRow {
  name: string;
  type: string;
  host: string;
  api_key: string;
}

export interface SettingGroup {
  key: string;
  label: string;
  knobs: SettingKnob[];
}

// SettingsScopeKnob is one knob inside an agent scope. That flag is the seam rule's
// "differs from main" for a secret, whose masked value cannot be compared.
export interface SettingsScopeKnob {
  key: string;
  value: unknown;
  overridden: boolean;
}

// SettingsScopeGroup is one of the four overridable groups an agent tab renders, in
// wire order.
export interface SettingsScopeGroup {
  key: string;
  overridden: boolean;
  knobs: SettingsScopeKnob[];
}

// SettingsScope is one agent home in the settings payload.
export interface SettingsScope {
  name: string;
  groups: SettingsScopeGroup[];
  error?: string;
}

export interface SettingsPayload {
  groups: SettingGroup[];
  scopes: SettingsScope[];
}

// normalizeSettingsPayload sanitizes a /settings response: malformed
// groups/knobs are dropped (wrong shape never blanks the pane with an
// exception), knobs keep only known types - an unknown type would have
// no control to render, so it falls out like any other stale entry.
const SETTING_KNOB_TYPES = ["toggle", "number", "text", "secret", "select", "textarea", "bindlist", "model", "providers"];

// normalizeSettingKnob sanitizes ONE knob object - the shape the groups carry and the
// one-knob read (`GET /settings?key=`) answers verbatim: both readers run through this
// one function, so the lazy read can never disagree with the group walk about a knob's
// fields.
export function normalizeSettingKnob(raw: unknown): SettingKnob | null {
  if (!raw || typeof raw !== "object") return null;
  const kn = raw as Record<string, unknown>;
  if (typeof kn.key !== "string" || typeof kn.label !== "string") return null;
  if (typeof kn.type !== "string" || !SETTING_KNOB_TYPES.includes(kn.type)) return null;
  if (kn.value === undefined) return null;
  return {
    key: kn.key,
    label: kn.label,
    type: kn.type as SettingKnob["type"],
    value: kn.value,
    // the default rides EVERY knob.
    default: kn.default,
    options: Array.isArray(kn.options) ? (kn.options as string[]) : undefined,
    filter:
      kn.filter === "decision" || kn.filter === "vision"
        ? kn.filter
        : undefined,
    slot: kn.slot === true ? true : undefined,
    defaultOption: kn.default_option === true ? true : undefined,
    min: typeof kn.min === "number" ? kn.min : undefined,
    max: typeof kn.max === "number" ? kn.max : undefined,
    step: typeof kn.step === "number" ? kn.step : undefined,
    help: typeof kn.help === "string" ? kn.help : undefined,
    live: kn.live === true,
    subgroup: typeof kn.subgroup === "string" && kn.subgroup !== "" ? kn.subgroup : undefined,
  };
}

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
        const knob = normalizeSettingKnob(k);
        if (knob) knobs.push(knob);
      }
      groups.push({ key: gr.key, label: gr.label, knobs });
    }
  }
  // same tolerance as the groups above: a malformed scope entry drops whole, a
  // malformed knob drops, and the list is ALWAYS an array (the pane decides "no
  // strip" from scopes.length === 0, never from a missing key).
  const scopes: SettingsScope[] = [];
  const rawScopes = raw && typeof raw === "object" ? (raw as Record<string, unknown>).scopes : undefined;
  if (Array.isArray(rawScopes)) {
    for (const s of rawScopes as unknown[]) {
      if (!s || typeof s !== "object") continue;
      const sr = s as Record<string, unknown>;
      if (typeof sr.name !== "string" || sr.name === "") continue;
      if (!Array.isArray(sr.groups)) continue;
      const scopeGroups: SettingsScopeGroup[] = [];
      for (const g of sr.groups as unknown[]) {
        if (!g || typeof g !== "object") continue;
        const gr = g as Record<string, unknown>;
        if (typeof gr.key !== "string" || !Array.isArray(gr.knobs)) continue;
        const knobs: SettingsScopeKnob[] = [];
        for (const k of gr.knobs as unknown[]) {
          if (!k || typeof k !== "object") continue;
          const kn = k as Record<string, unknown>;
          if (typeof kn.key !== "string" || kn.value === undefined) continue;
          knobs.push({ key: kn.key, value: kn.value, overridden: kn.overridden === true });
        }
        scopeGroups.push({ key: gr.key, overridden: gr.overridden === true, knobs });
      }
      scopes.push({
        name: sr.name,
        groups: scopeGroups,
        ...(typeof sr.error === "string" && sr.error !== "" ? { error: sr.error } : {}),
      });
    }
  }
  return { groups, scopes };
}

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

// fetchSettingKnob reads ONE knob through the server's own one-knob door. The knob
// object rides back exactly as it sits inside its group; a null answer means the host
// named no such knob (an old host, a renamed key).
export async function fetchSettingKnob(key: string): Promise<SettingKnob | null> {
  const r = await fetch(`/settings?key=${encodeURIComponent(key)}`);
  if (!r.ok) throw await settingsFailure(r, "settings read");
  return normalizeSettingKnob(await r.json());
}

export async function fetchSettingsScopes(): Promise<SettingsScope[]> {
  const r = await fetch("/settings?scopes=1");
  if (!r.ok) throw await settingsFailure(r, "settings scopes");
  return normalizeSettingsPayload(await r.json()).scopes;
}

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
    }
    throw new Error(msg);
  }
  return normalizeSettingsPayload(await r.json());
}

async function settingsFailure(r: Response, what: string): Promise<Error> {
  let msg = `${what}: ${r.status}`;
  try {
    const text = (await r.text()).trim();
    if (text) msg = text;
  } catch {
  }
  return new Error(msg);
}

// fetchConfigYaml loads the config FILE as YAML text. Secrets ride masked: the server
// never sends a credential on the wire, and a mask saved back means "keep the value on
// disk".
export async function fetchConfigYaml(): Promise<string> {
  const r = await fetch("/settings/raw");
  if (!r.ok) throw await settingsFailure(r, "config read");
  return await r.text();
}

// saveConfigYaml PUTs a whole YAML document through the server's config
// write path (validate + atomic write + hot-apply) and returns the
// refreshed knob payload, so the pane re-renders from one round trip.
export async function saveConfigYaml(text: string): Promise<SettingsPayload> {
  const r = await fetch("/settings/raw", {
    method: "PUT",
    headers: { "Content-Type": "application/yaml" },
    body: text,
  });
  if (!r.ok) throw await settingsFailure(r, "config save");
  return normalizeSettingsPayload(await r.json());
}

// resetSettings asks the server to restore the config defaults. The defaults belong to
// the host - the request carries no body, so the pane can never ship a second default
// set.
export async function resetSettings(): Promise<SettingsPayload> {
  const r = await fetch("/settings/reset", { method: "POST" });
  if (!r.ok) throw await settingsFailure(r, "config reset");
  return normalizeSettingsPayload(await r.json());
}

// configExportFilename names an exported config file: config-<date>.yaml,
// so two exports land side by side instead of overwriting one another.
export function configExportFilename(now: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  const day = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
  return `config-${day}.yaml`;
}

// settingValueOf reads one knob's server-computed value out of a settings payload.
// users: the app-level server.default_workspace read rides the SAME
// fetchSettings/normalizeSettingsPayload path the Settings tab renders - never a second
// config endpoint.
export function settingValueOf(payload: SettingsPayload, key: string): unknown {
  for (const g of payload.groups) {
    const k = g.knobs.find((kn) => kn.key === key);
    if (k) return k.value;
  }
  return undefined;
}

// ModuleRow is one registry-introspected module.
export interface ModuleRow {
  name: string;
  description?: string;
  provides?: string[];
  requires?: string[];
  state: "mounted" | "disabled";
  toggleable: boolean;
  dependents?: string[];
}

// ModuleRefusal is one ledger entry: a rejection the UI surfaces as an event.
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
    }
    throw new Error(msg);
  }
  return normalizeModulesPayload(await r.json());
}

// toggleNote builds the consequence half of a toggle hint.
export function toggleNote(m: ModuleRow, enable: boolean): string {
  if (!m.toggleable) return `${m.name} is always on - the tier law: agent-loop, provider and logging`;
  if (enable) return "";
  if (m.dependents && m.dependents.length > 0)
    return `this also disables: ${m.dependents.join(", ")}`;
  return "";
}

// ColorMode is the color-mode knob's value space.
export type ColorMode = "auto" | "dark" | "light";

// The theme knob's value space.
export const DEFAULT_THEME_ID = "default";

// localStorage keys: the color mode reuses the old key.
export const COLOR_MODE_KEY = "omega.theme-mode";
export const THEME_ID_KEY = "omega.theme-id";

export interface ColorModeTheme {
  mode: ColorMode;
  theme: string;
}

export interface UserTheme {
  id: string;
  name: string;
  base: "dark" | "light";
  tokens: Record<string, string>;
  error?: string;
}

// THEME_TOKEN_KEYS mirrors the server's token allowlist (the color set in main.css). It
// exists to CLEAR overrides when a user theme is switched off (built-in modes must
// restore `:root` / [data-theme=light] values untouched); the server validates what may be
// set.
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

export const isColorMode = (v: unknown): v is ColorMode =>
  v === "auto" || v === "light" || v === "dark";

// the legacy wire prefix of the old merged select's theme files
const THEME_SELECT_PREFIX = "theme:";

// restoreThemeKnobs resolves the persisted pair into today's two-knob shape.
export function restoreThemeKnobs(
  persistedId: string | null,
  legacyMode: string | null | undefined,
): ColorModeTheme {
  if (persistedId !== null && persistedId.trim() !== "") {
    return { mode: isColorMode(legacyMode) ? legacyMode : "auto", theme: persistedId };
  }
  if (legacyMode && legacyMode.startsWith(THEME_SELECT_PREFIX)) {
    const id = legacyMode.slice(THEME_SELECT_PREFIX.length);
    if (id.trim() !== "") return { mode: "auto", theme: id };
  }
  if (isColorMode(legacyMode)) {
    return { mode: legacyMode, theme: DEFAULT_THEME_ID };
  }
  return { mode: "auto", theme: DEFAULT_THEME_ID };
}

export function usableThemes(themes: UserTheme[]): UserTheme[] {
  return themes.filter((t) => !t.error);
}

// resolveThemeId guards a persisted theme id. A vanished or broken file falls back to
// the default - the color mode is never touched.
export function resolveThemeId(themeId: string, themes: UserTheme[]): string {
  if (themeId === DEFAULT_THEME_ID) return themeId;
  return usableThemes(themes).some((t) => t.id === themeId) ?
      themeId
    : DEFAULT_THEME_ID;
}

// themePickOptions builds the theme knob's option list. A file whose id is "default" is
// shadowed - the seeded default.yaml parses and serves clean, but in the picker
// "default" names the built-in palettes, so the file entry would just duplicate the
// option.
export function themePickOptions(
  themes: UserTheme[],
): (string | { value: string; label: string })[] {
  return [
    DEFAULT_THEME_ID,
    ...usableThemes(themes)
      .filter((t) => t.id !== DEFAULT_THEME_ID)
      .map((t) => ({ value: t.id, label: t.name })),
  ];
}

// colorModeOptions: the color-mode knob's option list, in pick order.
export const colorModeOptions: ColorMode[] = ["auto", "dark", "light"];

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

export function clearThemeTokens(style: CSSStyleDeclaration): void {
  for (const k of THEME_TOKEN_KEYS) style.removeProperty(`--${k}`);
}

export function applyThemeTokens(
  style: CSSStyleDeclaration,
  tokens: Record<string, string>,
): void {
  clearThemeTokens(style);
  for (const [k, v] of Object.entries(tokens)) style.setProperty(`--${k}`, v);
}

// filterSettingGroups narrows the settings list live. Pure - never touches the caller's
// arrays.
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

// knobHasDefault: the wire answers default: null for a knob whose default the config
// layer cannot name. Such a row gets no modified dot and no reset affordance -
// "unknown" is never rendered as "modified".
export function knobHasDefault(def: unknown): boolean {
  return def !== null && def !== undefined;
}

// blankValue folds an UNSET value and the empty string into one shape: a knob cleared
// to "" and a knob whose default is "" are the same state, so the empty-default text
// knobs (server.default_workspace, logging.file) never light a modified dot.
function blankValue(v: unknown): unknown {
  return v === undefined || v === null ? "" : v;
}

// knobDiffersFromDefault is the settings pane's modified test. A knob with no known
// default is never modified.
export function knobDiffersFromDefault(value: unknown, def: unknown): boolean {
  if (!knobHasDefault(def)) return false;
  return !valuesEqual(blankValue(value), blankValue(def));
}

export function valuesEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a !== "object" || typeof b !== "object" || a === null || b === null) return false;
  return JSON.stringify(a) === JSON.stringify(b);
}

// settingsDiff lists draft values that differ from the loaded ones - only changed knobs
// go on the wire, so an unrelated knob edited server-side between load and save is
// never clobbered by stale data.
export function settingsDiff(
  loaded: Record<string, unknown>,
  draft: Record<string, unknown>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(draft)) {
    if (!valuesEqual(draft[key], loaded[key])) out[key] = draft[key];
  }
  return out;
}

// AGENT_KNOB_FIELDS maps an agent-scope knob's dotted key onto the one write field.
export const AGENT_KNOB_FIELDS: Record<string, string> = {
  "provider.type": "provider_type",
  "provider.model_name": "model_name",
  "provider.host": "host",
  "provider.api_key": "api_key",
  system_prompt: "system_prompt",
  description: "description",
  max_turns: "max_turns",
};

// agentConfigUpdate builds the PUT /agents/{name} body for one agent tab save: the
// changed knob values mapped onto their wire fields, plus the "unset" list - the reset
// verb that DELETES a key so the scope falls back to main (never a write of main's
// value, which would pin a phantom override and, on the secret, persist the mask). A
// key in `unset` never rides as a value too (the endpoint refuses set+unset on one
// key); a knob outside the four overridable groups is dropped (nothing else is writable
// from this pane).
export function agentConfigUpdate(
  changed: Record<string, unknown>,
  unset: string[],
): { body: Record<string, unknown>; invalid?: string } {
  const body: Record<string, unknown> = {};
  const dropped = new Set(unset);
  for (const [key, value] of Object.entries(changed)) {
    if (dropped.has(key)) continue;
    const field = AGENT_KNOB_FIELDS[key];
    if (!field) continue;
    if (field === "max_turns") {
      const raw = String(value ?? "").trim();
      const n = typeof value === "number" ? value : Number(raw);
      // an EMPTY field is not 0: 0 is a real cap ("no turn limit"), so
      // emptiness can never be inferred into it - the caller's unset verb
      // is the only "inherit" (see scopeConfigDiff)
      if (raw === "" || !Number.isInteger(n) || n < 0) return { body, invalid: `max turns: "${String(value ?? "")}" is not a whole number` };
      body.max_turns = n;
      continue;
    }
    body[field] = value === null || value === undefined ? "" : String(value);
  }
  if (unset.length > 0) body.unset = [...unset];
  return { body };
}

// scopeConfigDiff builds one agent tab's PUT body from its local state. An EMPTIED
// max_turns field is a removal (the wire's "inherit" verb) - 0 is a real cap, so
// emptiness is never inferred as a value; on a knob with no override there is nothing
// to delete and the emptiness is dropped. Keys outside the four overridable groups
// never ride. Pure - the pane's save count and the wire body are the same derivation,
// so they cannot drift.
export function scopeConfigDiff(
  loaded: Record<string, unknown>,
  draft: Record<string, unknown>,
  queuedUnset: string[],
  overridden: Iterable<string>,
): { body: Record<string, unknown>; invalid?: string } {
  const own = new Set(overridden);
  const unset = new Set(queuedUnset.filter((key) => own.has(key)));
  const changed: Record<string, unknown> = {};
  for (const key of Object.keys(draft)) {
    if (!AGENT_KNOB_FIELDS[key]) continue;
    const value = draft[key];
    if (!unset.has(key) && valuesEqual(value, loaded[key])) continue;
    if (key === "max_turns" && (value === "" || value === null || value === undefined)) {
      if (own.has(key)) unset.add(key);
      continue;
    }
    changed[key] = value;
  }
  return agentConfigUpdate(changed, [...unset]);
}

// scopeKnobDiffersFromMain is the agent tab's dot predicate. Three answers, in order: a
// queued override removal is on its way back to main, so it shows no dot; a knob the
// agent's layer OWNS is structural (the layer file names the key) - and that is the
// only honest answer for provider.api_key, whose masked value cannot be compared;
// otherwise a draft edit departs from main exactly when the two values differ. Empty
// and unset are one value, so an untouched blank knob never lights the dot.
export function scopeKnobDiffersFromMain(
  key: string,
  draftValue: unknown,
  mainValue: unknown,
  overridden: boolean,
  unset: readonly string[],
): boolean {
  if (unset.includes(key)) return false;
  if (overridden) return true;
  return !valuesEqual(blankValue(draftValue), blankValue(mainValue));
}

// updateAgentConfig PUTs one agent's config diff through the EXISTING agents edit. The
// response is the refreshed Profile; the caller re-reads /settings for the scope
// payload, because that is computed from the layer on disk. Throws the endpoint's own
// words on a non-2xx - a 409 (the locked default, a layer that will not parse) is
// rendered read-only by the pane, never retried.
export async function updateAgentConfig(
  name: string,
  body: Record<string, unknown>,
): Promise<void> {
  const r = await fetch(`/agents/${encodeURIComponent(name)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (r.ok) return;
  let msg = `agent save: ${r.status}`;
  try {
    // the error envelope is plain text (http.Error), not JSON
    const text = (await r.text()).trim();
    if (text) msg = text;
  } catch {
  }
  throw new Error(msg);
}

// SessionSearchHit is one /sessions/search row.
export interface SessionSearchHit {
  id: string;
  snippet: string;
  titleMatch?: boolean;
}

// searchSessions queries the unified endpoint.
export async function searchSessions(
  query: string,
): Promise<SessionSearchHit[]> {
  const r = await fetch(
    `/sessions/search?q=${encodeURIComponent(query)}&archived=include`,
  );
  if (!r.ok) throw new Error(`session search: ${r.status}`);
  return r.json();
}

// Debounce for the sidebar's session search.
export const SESSION_SEARCH_DEBOUNCE_MS = 300;

// withSearchHits maps the backend's ranked ids onto the session list.
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

// attachImageUrl asks the SERVER to fetch a remote image URL (POST /attach-image) and
// answer the ai.ImageContent shape - the browser never fetches cross-origin image bytes
// itself.
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

// uploadAttachment posts one picked File to POST /attach-file. The cap
// is checked here too - the same number (MAX_CHAT_FILE_BYTES) the composer checks, so a
// caller that skipped the picker's gate still never ships an oversize file past the
// browser. PART ORDER IS LOAD-BEARING: the handler reads parts in wire order, so
// session_id must precede the file part - reversed, the file stages under
// <uploads>/_pending and only /chat's relocation rescues it into the session dir.
export async function uploadAttachment(
  file: File,
  sessionId?: string | null,
): Promise<OmegaAttachment> {
  if (file.size > MAX_CHAT_FILE_BYTES) {
    throw new Error(
      `${file.name || "file"} too large: ${(file.size / 1048576).toFixed(1)} MB (max ${MAX_CHAT_FILE_BYTES / 1048576} MB)`,
    );
  }
  const form = new FormData();
  if (sessionId) form.append("session_id", sessionId);
  form.append("file", file, file.name || "file");
  const r = await fetch("/attach-file", { method: "POST", body: form });
  if (!r.ok) {
    const text = await r.text();
    throw new Error(`attach-file: ${r.status}${text ? `: ${text.trim()}` : ""}`);
  }
  return (await r.json()) as OmegaAttachment;
}

// markSessionRead stamps the session's last-read time (PATCH {"read": true}). It
// touches nothing else: reading is not activity, updated_at must not move.
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

// setSessionContext pins the session's remembered send context. updated_at never moves.
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

/** one GET /sessions/{id}/context answer: the model-switch dialog's two numbers. */
export interface SessionContextInfo {
  tokens: number;
  context_window: number;
  model?: string;
}

/** fetchSessionContext reads the live context estimate and the TARGET slot's window. */
export async function fetchSessionContext(
  id: string,
  model: string,
): Promise<SessionContextInfo> {
  const q = model ? `?model=${encodeURIComponent(model)}` : "";
  const r = await fetch(`/sessions/${encodeURIComponent(id)}/context${q}`);
  if (!r.ok) throw new Error(`session context: ${r.status}`);
  return (await r.json()) as SessionContextInfo;
}

// WorkspaceListing is one GET /workspaces/browse response.
export interface WorkspaceListing {
  path: string;
  parent: string;
  dirs: { name: string; path: string }[];
}

// browseWorkspaces lists one directory's child folders for the session workspace.
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

// branchSession forks a session (POST /sessions/{id}/branch - the /branch parity).
// Returns the CHILD row (parent_id set); the caller refreshes the list and opens the
// child like any session.
export async function branchSession(id: string): Promise<Session> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}/branch`, {
    method: "POST",
  });
  if (!r.ok) throw new Error(`branch: ${r.status}`);
  return (await r.json()) as Session;
}

export async function renameSession(id: string, label: string): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ label }),
  });
  if (!r.ok) throw new Error(`rename: ${r.status}`);
}

// exportSessionUrl is the JSONL export (the /export parity) as a plain download href.
export function exportSessionUrl(id: string): string {
  return `/sessions/${encodeURIComponent(id)}/export`;
}

// sessionRowBadge is the subtle branch mark for parented sessions: "↳ branch" inside
// the row, nothing for plain sessions - badge-less rows render exactly as they always
// did.
export function sessionRowBadge(s: Pick<Session, "parent_id">): string {
  return s.parent_id ? "↳ branch" : "";
}

export function commitSessionRename(
  draft: string,
  current: string | undefined,
): string | null {
  const trimmed = draft.trim();
  if (!trimmed || trimmed === (current ?? "")) return null;
  return trimmed;
}

// Minimal structural view of a transcript item for the retry helpers.
export interface TranscriptItemLike {
  kind: string;
  role?: string;
  content?: string;
}

export function lastCompleteUserIndex(items: TranscriptItemLike[]): number {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (item?.kind === "message" && item.role === "user") return i;
  }
  return -1;
}

// lastUserText is the transcript check behind the composer's retry affordance.
export function lastUserText(items: TranscriptItemLike[]): string | null {
  const idx = lastCompleteUserIndex(items);
  if (idx === -1) return null;
  return items[idx]?.content ?? null;
}

// dataUrl turns an image attachment into an img/click-open src.
export function dataUrl(img: OmegaImage): string {
  return `data:${img.media_type};base64,${img.base64}`;
}

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

// createItemKeySource gives transcript items a stable key. WeakMap: the
// mapping dies with the item, no leak. This is per-source state, not a global counter -
// two transcripts never share keys anyway.
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

// StreamInterruptedError: the SSE transport died mid-stream.
// The caller surfaces the death in the chat's statusLine above the composer (the panel
// keeps its last paint); the composer's retry face is the resume affordance.
export class StreamInterruptedError extends Error {
  constructor(message = "stream interrupted") {
    super(message);
    this.name = "StreamInterruptedError";
  }
}

// SessionBusyError: POST /chat answered 409. The caller renders the busy affordance;
// retrying once the run ends re-sends through the normal path.
export class SessionBusyError extends Error {
  constructor(message = "session already running") {
    super(message);
    this.name = "SessionBusyError";
  }
}

// consumeSSE reads frames off a fetch Response body and drives the handler callbacks.
// Error payloads throw - the caller surfaces them.
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
    let idx: number;
    while ((idx = buffer.indexOf("\n\n")) !== -1) {
      const frame = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      // Named control frames ride the SSE event line, not the payload's type field.
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

  // A reader finishing WITHOUT the terminal event is an abnormal termination - the
  // server never finished the run (dropped connection, process death).
  if (!state.sawEnd) throw new StreamInterruptedError();
}

// THINKING_LEVELS mirrors ai.ThinkingLevels (llm/provider.go).
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

// UI options for the effort chip.
export const THINKING_UI_LEVELS = ["default", "off", ...THINKING_LEVELS.slice(2)];

// thinkingLevelWire maps a UI selection to the outgoing /chat value.
export function thinkingLevelWire(selection: string): number | undefined {
  if (selection === "default") return undefined;
  const idx = THINKING_LEVELS.indexOf(selection);
  return idx === -1 ? undefined : idx;
}

// streamChat POSTs messages to /chat and drives the handler callbacks from the SSE.
// Error payloads throw - the caller surfaces them. fork of a user turn is replay
// semantics - no messages ride the wire there either (the caller passes none).
// ephemeral: the run is a NO-PERSIST chat - no store row, nothing on disk; the server
// mints the `eph-` id (or continues the in-memory session when sessionId carries one)
// and the memory tool never runs.
export async function streamChat(
  messages: OmegaMessage[],
  sessionId: string | null,
  handlers: StreamHandlers,
  model?: string,
  thinkingLevel?: number,
  fork?: string,
  ephemeral?: boolean,
  workspace?: string,
): Promise<void> {
  const resp = await fetch("/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(
      chatBody(messages, sessionId, { model, thinkingLevel, fork, ephemeral, workspace }),
    ),
  });
  if (resp.status === 202) {
    // A live run took the send as a STEER: the server parked it and answered the row. No
    // stream started, so the caller settles the send on the queue, not on run handlers.
    const queued = await readQueuedRows(resp);
    handlers.onQueued?.(queued);
    return;
  }
  if (resp.status === 409) throw new SessionBusyError();
  if (!resp.ok || !resp.body) throw new Error(`chat: ${resp.status}`);
  await consumeSSE(resp, handlers);
}

/** chatBody builds the /chat wire body: one shape for a run send and a queued steer. */
export function chatBody(
  messages: OmegaMessage[],
  sessionId: string | null,
  opts: {
    model?: string;
    thinkingLevel?: number;
    fork?: string;
    ephemeral?: boolean;
    workspace?: string;
  } = {},
): Record<string, unknown> {
  const body: Record<string, unknown> = { messages };
  if (sessionId) body.session_id = sessionId;
  if (opts.ephemeral) body.ephemeral = true;
  if (opts.model) body.model = opts.model;
  if (opts.thinkingLevel !== undefined) body.thinking_level = opts.thinkingLevel;
  if (opts.fork) body.fork = opts.fork;
  if (opts.workspace && !sessionId) body.workspace = opts.workspace;
  return body;
}

/** readQueuedRows reads the 202 body: the rows a steer send parked. */
export async function readQueuedRows(resp: Response): Promise<QueuedRow[]> {
  const body = (await resp.json()) as { queued?: QueuedRow[] };
  return Array.isArray(body?.queued) ? body.queued : [];
}

// sendSteer posts one mid-turn message and returns the rows the server parked. The caller
// only reaches for this when the session's run slot is taken; the server decides for real,
// so a race that ends the run first falls back to the stream send (null = a run started).
// A real refusal throws: it must speak, never read as delivery.
export async function sendSteer(
  messages: OmegaMessage[],
  sessionId: string,
  model?: string,
  thinkingLevel?: number,
): Promise<QueuedRow[] | null> {
  for (let attempt = 0; attempt < 2; attempt++) {
    const resp = await fetch("/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(chatBody(messages, sessionId, { model, thinkingLevel })),
    });
    if (resp.status === 202) return readQueuedRows(resp);
    if (resp.status === 409 && attempt === 0) {
      // The slot filled between the busy read and this post: the run that holds it can
      // take the same body now, so one retry lands it as the steer the caller meant.
      await resp.body?.cancel().catch(() => {});
      continue;
    }
    if (!resp.ok) {
      const detail = await resp.text().catch(() => "");
      throw new Error(`chat: ${resp.status}${detail ? ` ${detail.trim()}` : ""}`);
    }
    // 200: the run ended between the client's busy read and this post - the send STARTED a
    // run. Its stream is detached server-side; the caller re-reads the session.
    await resp.body?.cancel().catch(() => {});
    return null;
  }
  throw new Error("chat: session busy");
}

// fetchQueue reads the session's waiting steer rows (the visible queue after a reload).
export async function fetchQueue(sessionId: string): Promise<QueuedRow[]> {
  const r = await fetch(`/sessions/${encodeURIComponent(sessionId)}/queue`);
  if (!r.ok) throw new Error(`queue: ${r.status}`);
  const body = (await r.json()) as { queued?: QueuedRow[] };
  return Array.isArray(body?.queued) ? body.queued : [];
}

// deleteQueueRow drops one row before the boundary reaches it. A 404 is a stale row (the
// turn already took it): the caller refreshes rather than pretending it removed anything.
export async function deleteQueueRow(sessionId: string, rowId: string): Promise<boolean> {
  const r = await fetch(
    `/sessions/${encodeURIComponent(sessionId)}/queue/${encodeURIComponent(rowId)}`,
    { method: "DELETE" },
  );
  if (r.status === 404) return false;
  if (!r.ok) throw new Error(`queue delete: ${r.status}`);
  return true;
}

// stopSessionRun is the REST stop (POST /sessions/{id}/stop): the user's hard stop of the
// session's live run. It refuses an idle session (404 "session not active"), which is why
// the menu offers it only on a running row.
export async function stopSessionRun(id: string): Promise<void> {
  const r = await fetch(`/sessions/${encodeURIComponent(id)}/stop`, {
    method: "POST",
  });
  if (!r.ok) throw new Error(`stop: ${r.status}`);
}

// attachSession's false means the session has no live run (404 "session not active") -
// the caller renders settled history instead.
export async function attachSession(
  sessionId: string,
  handlers: StreamHandlers,
): Promise<boolean> {
  const resp = await fetch(`/sessions/${encodeURIComponent(sessionId)}/events`);
  if (!resp.ok || !resp.body) return false;
  await consumeSSE(resp, handlers);
  return true;
}

// runActive probes whether a session holds a LIVE run right now (async agent loop phase
// 3): the pending watcher asks before committing to a full attach, because GET
// /sessions/{id}/events 404s "session not active" the moment no run holds the slot -
// exactly the home-sleeps window between a yielded turn's end and its wake launch. A
// true answer ABORTS the probe stream: the watcher re-fetches for the real ride (the
// race between probe and attach is benign - a run that ends in the gap turns the attach
// into a 404 and the loop probes again). Never throws: a probe is a question, not
// transport.
export async function runActive(sessionId: string): Promise<boolean> {
  try {
    const resp = await fetch(`/sessions/${encodeURIComponent(sessionId)}/events`);
    if (resp.ok) {
      await resp.body?.cancel().catch(() => {});
      return true;
    }
    return false;
  } catch {
    return false;
  }
}

export type RegistryKind = "skills" | "tools" | "agents" | "insights";

export interface DisabledLists {
  skills: string[];
  tools: string[];
  agents: string[];
}

// RegistryRow is the shared list pattern's row. `value` is the read-only stat row's
// number (the insights consumer's bars and counts read it; toggle-less rows never set
// it). Cron's row voice - the state word rides the dim line, exactly like cron's "next
// HH:MM".
export interface RegistryRow {
  name: string;
  description?: string;
  detail?: string[];
  disabled: boolean;
  value?: number;
  noToggle?: boolean;
  status?: { on: boolean; label: string };
  /**
   * the one-line preview under the row name.
   * Empty = no preview line at all (a subject that never spoke has nothing to say).
   */
  preview?: string;
  previewAt?: string;
}

// normalizeDisabledPayload never trusts the wire.
export function normalizeDisabledPayload(raw: unknown): DisabledLists {
  const out: DisabledLists = { skills: [], tools: [], agents: [] };
  if (raw === null || typeof raw !== "object") return out;
  for (const kind of ["skills", "tools", "agents"] as const) {
    const names = (raw as Record<string, unknown>)[kind];
    if (Array.isArray(names)) {
      out[kind] = names.filter((n): n is string => typeof n === "string" && n.length > 0);
    }
  }
  return out;
}

// withDisabledFlags merges one namespace's fold into the registry rows by name. A fold
// entry whose name matches no row is IGNORED here (the registry listing is the tab's
// truth; the server refuses unknown-name disables so the fold never widens past it).
export function withDisabledFlags<T extends { name: string }>(
  rows: readonly T[],
  disabled: readonly string[],
): (T & { disabled: boolean })[] {
  const set = new Set(disabled);
  return rows.map((r) => ({ ...r, disabled: set.has(r.name) }));
}

// fetchRegistryDisabled loads the fold. 501 = the host serves no
// registry toggle seam: the caller renders the list read-only, same
// fallback the settings pane uses for a missing knob seam.
export async function fetchRegistryDisabled(): Promise<DisabledLists> {
  const r = await fetch("/disabled");
  if (!r.ok) {
    let msg = `disabled: ${r.status}`;
    try {
      const text = await r.text();
      if (text) msg = text;
    } catch {
    }
    throw new Error(msg);
  }
  return normalizeDisabledPayload(await r.json());
}

// toggleRegistryEntry PUTs one entry's new state and returns the fresh fold.
export async function toggleRegistryEntry(
  kind: RegistryKind,
  name: string,
  enabled: boolean,
): Promise<DisabledLists> {
  const r = await fetch(`/disabled/${kind}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, enabled }),
  });
  if (!r.ok) {
    let msg = `registry toggle: ${r.status}`;
    try {
      const text = await r.text();
      if (text) msg = text;
    } catch {
    }
    throw new Error(msg);
  }
  return normalizeDisabledPayload(await r.json());
}

// registryNote builds the consequence a disabled-direction toggle shows. Insights
// (a read-only stat consumer) has no exclusion surface - the note is the empty
// string, and the toggleable seam never reaches it.
export function registryNote(kind: RegistryKind, enable: boolean): string {
  if (enable || kind === "insights") return "";
  const consequence: Record<Exclude<RegistryKind, "insights">, string> = {
    skills: "excluded from the session prompt build",
    tools: "absent from the agent tool list",
    agents: "roster row and wake triggers off; home untouched",
  };
  return consequence[kind];
}

// filterRegistryRows narrows the list live. Pure - never touches the caller's arrays.
export function filterRegistryRows(
  rows: readonly RegistryRow[],
  query: string,
): RegistryRow[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...rows];
  return rows.filter(
    (r) =>
      r.name.toLowerCase().includes(q) ||
      (r.description ?? "").toLowerCase().includes(q),
  );
}
