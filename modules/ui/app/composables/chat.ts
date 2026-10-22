// omega webui: the per-tab chat host (card t_4f0e88f2 - chat tabs +
// URL semantics). Every chat tab binds a session; the SAME session may
// live in two tabs (settled ruling 2026-10-19) and BOTH tabs subscribe
// to GET /sessions/{id}/events - the runHub's multi-subscribers are
// proven server-side, so per-tab subscriptions are the truth here.
//
// consequence: a tab's stream state is keyed by the TAB's instance key
// (dock.ts mints it - stable across pane moves and reloads), never by
// the session id. Two tabs on one session each hold their own
// transcript copy and their own SSE subscription; the busy gate is the
// only thing that stays PER SESSION: the runHub run slot is per
// session, so a second send (this page or another window) is gated
// client-side by the session-wide busy state and server-side by the
// 409 surfaced through streamChat's SessionBusyError.

import { computed, reactive, ref, type ComputedRef, type InjectionKey, type Ref } from "vue";
import {
  withBusy,
  type BusyMap,
  type StreamHandlers,
  type Session,
  StreamInterruptedError,
  SessionBusyError,
  fetchTranscript,
  renderTranscriptItems,
  persistedTelemetry,
  postVariantSelect,
  streamChat,
  attachSession,
  stopSession,
  deleteSession,
  hasInterruptedMarker,
  withoutMarkers,
  lastUserText,
  attachImageUrl,
  verdictCardFromToolArgs,
  verdictFromResultText,
  type VerdictItem,
  type OmegaImage,
  type VariantBlock,
} from "./omega.ts";

// --- transcript item shapes (moved from app.vue: every chat instance
//     renders from them; the app's status bar reads the focused state) ---

export interface MessageItem {
  kind: "message";
  role: "user" | "assistant";
  content: string;
  // store row id (restored history / post-run refetch only) - the
  // variants surface's address (switcher + fork). Live-built items
  // carry none.
  id?: string;
  streaming?: boolean;
  // client-side timestamp for live messages, backend created_at for
  // restored history (ChatMessage renders it as a hover title)
  time?: string;
  // server-measured response duration (restored history)
  finalMs?: number;
  // image attachments on user messages (paste/picker/url) - rendered
  // as thumbnails inside the bubble
  images?: OmegaImage[];
}
export interface ThinkingItem {
  kind: "thinking";
  content: string;
  streaming?: boolean;
  // server-measured thinking duration (restored history)
  finalMs?: number;
}
export interface ToolItem {
  kind: "tool";
  name: string;
  status: string;
  // a dead stream never delivers the tool result - flagged so the
  // elapsed meter in ToolCallLine freezes instead of ticking forever
  interrupted?: boolean;
  output?: string;
  result?: string;
  // server-measured duration from the matched result (restored)
  finalMs?: number;
  // elapsed already consumed by a STILL-RUNNING restored call (the
  // transcript's live-run merge): the meter counts on from here
  elapsedMs?: number;
}
// inline "stream interrupted" line: the SSE transport died mid-run, or
// the send was refused because the session's run slot was taken (409 -
// label names it). Carries the resume affordance - see retry.
export interface InterruptedItem {
  kind: "interrupted";
  label?: string;
}
export type TranscriptItem =
  | MessageItem
  | ThinkingItem
  | ToolItem
  | InterruptedItem
  | VerdictItem;

/** ChatState is ONE chat tab's view of ITS session: transcript, draft,
    telemetry. A stream writes ONLY its own state, so any session can
    stream while another tab renders anywhere in the dock. */
export interface ChatState {
  key: string;
  // the bound session id; null while the tab's chat is unsaved
  // (composer-first) - bare /chat auto-creates it and the id arrives
  // on the response's leading `session` SSE frame
  sessionId: string | null;
  transcript: TranscriptItem[];
  // the composer draft lives per state: switching tabs (unmount) keeps
  // half-written input, exactly like a VS Code editor keeps its buffer
  draft: string;
  // pending image attachments, per tab like the draft: paste/picker/
  // url-attach collect here and the next send carries them
  attachments: OmegaImage[];
  statusLine: string;
  tokens: number;
  tokensPerSecond: number;
  liveChunksPerSecond: number;
  // last-known chunks/s, from the session row's last_run block (or
  // the run that just ended) - the idle state bar's rate display
  lastChunksPerSecond: number;
  // the live run's stop affordance: set by runStream while this state
  // has a stream, cleared in its finally. The composer's cancel button
  // calls it. The stop POST is session-keyed - EITHER tab on the same
  // session cancels the one server-side run. A server run another
  // client started (reattach busy) is stoppable too.
  cancelRun?: () => void;
  // a live SSE is bound to THIS state (its own subscription)
  attached?: boolean;
  // the mounted ChatTab registers its scroll follow here; cleared on
  // unmount - a stream in a hidden/closed tab writes state, never
  // touches a scrollbar
  notify?: (force?: boolean) => void;
  // its tab was closed (or its session deleted) while the run was
  // live: the state stops being rendered - dropped at stream end
  orphan?: boolean;
  // this state already carries the session's full content (a finished
  // live run, or a successful restore) - the unsaved->created flow sets
  // it so the post-create watcher never clobbers the live transcript
  loaded?: boolean;
  // the session's branch points (t_17365247 wire block, restored with
  // the transcript and refreshed after every select + run end). Empty
  // for sessions without variants - the switcher renders nothing then.
  variants: VariantBlock[];
  // the composer's FORK anchor (t_a62eeb43): a historical assistant
  // message the next send continues FROM (POST /chat {fork}). null =
  // plain send at the active path's end. Fork of a USER message does
  // not anchor - it reruns that turn immediately (replay semantics).
  forkFrom: string | null;
  // ephemeral chat (card t_d96a5cd5): this tab's session is a
  // NO-PERSIST `eph-` session - server memory only, gone at restart,
  // no sidebar row, the memory tool denied server-side. Close deletes
  // the server record (the contract: close = gone). Retry/fork/
  // variants never exist here: the store tree they replay is absent.
  ephemeral?: boolean;
}

/** EPHEMERAL_DRAFT_REF is the dock tab's sessionRef marker for a
    composer-first ephemeral tab (card t_d96a5cd5): minted client-side
    before any send, exchanged for the server's `eph-` id on the leading
    `session` frame. Never a real session id - the registry namespace
    prefix is `eph-`. */
export const EPHEMERAL_DRAFT_REF = "ephemeral";

export const DEFAULT_CHAT_STATE: ChatState = {
  key: "",
  sessionId: null,
  transcript: [],
  draft: "",
  attachments: [],
  statusLine: "",
  tokens: 0,
  tokensPerSecond: 0,
  liveChunksPerSecond: 0,
  lastChunksPerSecond: 0,
  variants: [],
  forkFrom: null,
};

/** the app-level callbacks the host needs - the session LIST, the dock
    tree and the focus concept stay app-owned. */
export interface ChatHostDeps {
  /** write the session id back onto the tab (unsaved -> created) */
  setTabSessionRef(key: string, sessionId: string | null): void;
  /** sidebar refresh: fetches sessions, feeds the row list + the
      server-active set back through noteServerActive */
  refreshSessions(): void | Promise<void>;
  /** the refreshed session row for an id (the durable telemetry + the
      label read); undefined when the session is unknown */
  sessionRow(sessionId: string): Session | undefined;
  /** open = read: unmute the sidebar row + stamp the server read time */
  markRead(sessionId: string): void;
  /** whether THIS state key is the app's focused chat tab (the stream
      end only unreads the transcript the user watched) */
  isFocused(key: string): boolean;
}

export interface ChatHost {
  states: Map<string, ChatState>;
  /** busy keyed by STATE key (this page's live streams) */
  busyKeys: Ref<BusyMap>;
  /** busy keyed by SESSION id: local live streams mapped through their
      states, plus the server-reported active set (detached runs other
      windows started - survives this page, like refreshSessions) */
  busyAll: ComputedRef<BusyMap>;
  /** any live stream anywhere - the tab-title spinner source */
  anyBusyNow: ComputedRef<boolean>;
  ensureState(key: string, sessionId?: string | null, ephemeral?: boolean): ChatState;
  /** mount-time restore of one tab's view: persisted history rebuild +
      telemetry, reading the detail's live-run flag to attach. Refetch
      is skipped while this state itself is attached/busy (a refetch
      would clobber the live stream with a mid-flight snapshot). */
  load(key: string, sessionId: string): Promise<void>;
  /** subscribe to the session's live run for THIS tab's state (the
      duplicate-tab path: the other tab already streams locally) */
  attach(key: string): Promise<void>;
  /** the ONE send path (composer submit + retry). Returns false when
      the per-session gate refused (double send, never double run). */
  submit(key: string, text: string, model?: string, thinking?: number): Promise<boolean>;
  /** add picked/pasted images to this tab's pending attachments */
  attachFiles(key: string, images: OmegaImage[]): void;
  /** remove one pending attachment by index */
  removeAttachment(key: string, index: number): void;
  /** server-side fetch of an image URL into this tab's attachments */
  attachUrl(key: string, url: string): Promise<void>;
  /** re-send this tab's last complete user message (interrupted/rebusy
      affordance); no-op while the session's run slot is taken */
  retry(key: string, model?: string, thinking?: number): Promise<boolean>;
  /** variant navigation (t_a62eeb43): select the 0-based sibling at a
      branch point, re-fetch and re-render the active path; false while
      the session's run slot is taken */
  selectVariant(key: string, messageId: string, index?: number): Promise<boolean>;
  /** fork affordance on a historical message: an ASSISTANT target
      anchors the composer (the next typed send POSTs /chat {fork}); a
      USER target reruns that turn immediately (replay semantics). No-op
      while the session's run slot is taken. */
  fork(key: string, messageId: string, model?: string, thinking?: number): Promise<void>;
  /** drop the anchor without sending */
  clearFork(key: string): void;
  /** stop THIS tab's bound session's live run */
  cancel(key: string): void;
  /** the composer's busy bit for one tab: the per-session run slot
      (busyAll over its session id), or the unsaved create window */
  composerBusy(key: string): boolean;
  /** a tab (or an orphaned state) stopped being rendered: state is
      dropped now unless a run still feeds it - that lands at stream end */
  closeState(key: string): void;
  /** session deleted: its states go the same way */
  closeSession(sessionId: string): void;
  /** refreshSessions feeds the server-reported active rows in */
  noteServerActive(ids: string[]): void;
}

export const CHAT_HOST_KEY: InjectionKey<ChatHost> = Symbol("omega-chat-host");

// --- the ONE composer button's face (t_357cbe9c) ---
//
// retry and send merge into a single morphing affordance: while this
// session's run streams the button is the stop control; idle, a typed
// draft or a pending attachment means send (the button disables when
// there is nothing to send), and an empty composer with an exchange in
// the transcript means retry. Draft beats retry: typed input is a new
// message, never a replay. Pure so the morph states stay testable.

export type ComposerFace = "stop" | "retry" | "send";

export function composerFace(
  busy: boolean,
  canRetry: boolean,
  canSend: boolean,
): ComposerFace {
  if (busy) return "stop";
  if (!canSend && canRetry) return "retry";
  return "send";
}

// --- the activity line (card t_e9baac4b) ---
//
// Phase-aware narration for the line above the composer, derived PURELY
// from the transcript + the session's busy gate - no new state. The
// run's phases ARE the transcript: a call still open (its result has
// not landed yet) is the run's activity, else the last streaming item's
// kind narrates the stream. "thinking" is also what a run shows before
// anything lands in the transcript (the send beat the first delta) and
// in the gap between a landed tool result and the model's next move.

export interface ActivityPhase {
  /** stable identity - the component restarts its elapsed meter ONLY
      when this changes (content deltas recompute but never move it) */
  key: string;
  /** the narration: "thinking", "running web.search", "responding" */
  label: string;
  /** elapsed ALREADY CONSUMED at observation: a restored still-running
      tool counts on from the server's elapsed-so-far (live-run merge),
      the same initialMs contract the ToolCallLine meter runs on */
  initialMs?: number;
}

// findMessageItem locates a transcript MESSAGE item by its store row id
// (the variants surface's address). Live-built items carry no id and are
// never found here - exactly right: the fork affordance works on
// historical messages only. Pure.
export function findMessageItem(
  items: TranscriptItem[],
  id: string,
): MessageItem | null {
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i]!;
    if (it.kind === "message" && it.id === id) return it;
  }
  return null;
}

export function activityPhase(
  items: TranscriptItem[],
  busy: boolean,
): ActivityPhase | null {
  if (!busy) return null;
  // newest open tool call + newest streaming item. interrupted is NOT
  // open: a dead stream's result is never coming - narrating that call
  // as running would tick forever over a run that cannot finish.
  let tool: ToolItem | null = null;
  let streaming: ThinkingItem | MessageItem | null = null;
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i]!;
    if (
      tool === null &&
      item.kind === "tool" &&
      item.status === "running" &&
      !item.interrupted
    ) {
      tool = item;
    } else if (
      streaming === null &&
      (item.kind === "thinking" || item.kind === "message") &&
      item.streaming
    ) {
      streaming = item;
    }
  }
  // a running tool outranks the streaming items: the model's preamble
  // already streamed - the tool call is what the run is DOING now
  if (tool) {
    const name = tool.name || "tool";
    return {
      key: `tool:${name}`,
      label: `running ${name}`,
      initialMs: tool.elapsedMs || undefined,
    };
  }
  if (streaming?.kind === "thinking") {
    return { key: "thinking", label: "thinking" };
  }
  if (streaming) {
    return { key: "responding", label: "responding" };
  }
  // busy with nothing narrated yet: the run just started (or is between
  // its tool result and the model's next move) - waiting on the model
  // IS thinking
  return { key: "thinking", label: "thinking" };
}

/** the title goroutine writes the session label seconds AFTER the first
    exchange ends - one delayed refresh picks the label up without a
    manual reload (the pre-card rule carried over from app.vue). */
const TITLE_REFRESH_MS = 10000;

export function createChatHost(deps: ChatHostDeps): ChatHost {
  // reactive(Map) wraps values on READ - always keep the wrapped handle
  // (ensureState does), or state writes stop triggering the view
  const states = reactive(new Map<string, ChatState>());
  const busyKeys = ref<BusyMap>(new Set());
  const serverBusy = ref<BusyMap>(new Set());
  const busyAll = computed<BusyMap>(() => {
    const out = new Set(serverBusy.value);
    for (const key of busyKeys.value) {
      const s = states.get(key);
      if (s?.sessionId) out.add(s.sessionId);
    }
    return out;
  });
  const anyBusyNow = computed(
    () => busyAll.value.size > 0 || busyKeys.value.size > 0,
  );

  function ensureState(key: string, sessionId: string | null = null, ephemeral?: boolean): ChatState {
    let st = states.get(key);
    if (!st) {
      states.set(key, {
        key,
        sessionId,
        transcript: [],
        draft: "",
        attachments: [],
        statusLine: "",
        tokens: 0,
        tokensPerSecond: 0,
        liveChunksPerSecond: 0,
        lastChunksPerSecond: 0,
        variants: [],
        forkFrom: null,
        ephemeral: !!ephemeral,
      });
      st = states.get(key) as ChatState;
      return st;
    }
    // the tab binds the session: an unsaved state that gained its id
    // (the create path wrote it right after this ensure) adopts it - the
    // key stays the tab instance's identity. The ephemeral bit NEVER
    // flips: the server-minted `eph-` id adopting onto an unsaved tab
    // keeps the no-persist contract attached to the state.
    if (sessionId && st.sessionId !== sessionId) st.sessionId = sessionId;
    if (ephemeral !== undefined) st.ephemeral = ephemeral;
    return st;
  }

  function noteServerActive(ids: string[]) {
    // server-reported live runs REPLACE (not union into) the snapshot:
    // these are the sessions a freshly refreshed page sees as pulsing -
    // local streams carry their own busy entries in busyKeys
    serverBusy.value = new Set(ids);
  }

  // makeStreamDriver builds the per-run stream state over ONE tab's
  // session state: the SSE handler set plus the finalization that
  // freezes meters and bubbles when the stream ends. Both transports
  // use it - the send stream (POST /chat) and the tab attach (GET
  // /sessions/<id>/events). Scrolling is not ours: the driver notifies
  // the mounted ChatTab (st.notify) which owns its own pinned logic.
  function makeStreamDriver(st: ChatState) {
    let assistant: MessageItem = {
      kind: "message",
      role: "assistant",
      content: "",
      streaming: true,
    };
    // Lazy push: the assistant bubble enters the transcript at the first
    // response chunk so thinking blocks and tool lines (which arrive
    // earlier in the stream) render BEFORE the reply.
    let assistantPushed = false;
    const pushAssistant = () => {
      if (!assistantPushed) {
        assistant.time = assistant.time ?? new Date().toISOString();
        st.transcript.push(assistant);
        // reactivity: the raw literal bypasses the reactive proxy, so
        // later streaming-flag mutations would not re-render (the
        // stuck-cursor bug) - re-capture the PROXY the array hands back
        assistant = st.transcript[st.transcript.length - 1] as MessageItem;
        assistantPushed = true;
      }
    };
    // the server-restored tail stream helper: the reattach merge serves
    // the run's open thinking/response items with streaming set (card
    // t_692254f7). Returns the LAST transcript item when it is that
    // kind and still streaming - a stream that starts after the refresh
    // appends into that block (restore plus attach read as ONE
    // continuous run) instead of opening a duplicate one. The driver's
    // own live items never qualify: they are claimed through its
    // thinking/assistant vars, not looked up here.
    function restoredTail(kind: "thinking" | "message"): ThinkingItem | MessageItem | null {
      const last = st.transcript[st.transcript.length - 1];
      if (!last || last.kind !== kind) return null;
      const item = last as ThinkingItem | MessageItem;
      return item.streaming ? item : null;
    }
    // Tool phases split the turn into response segments: each text after
    // a tool phase is its own bubble, so the timeline reads
    // chronologically (preamble -> tools -> final answer).
    let segmentBreak = false;
    let thinking: ThinkingItem | null = null;
    // tool call ids seen live in THIS stream: the materialize path
    // resolves an orphaned result's row name from its tool_call_id
    const toolNames = new Map<string, string>();
    const sentAt = Date.now();
    // a fresh stream takes the meters over: the last-known idle values
    // step aside until this run's own numbers arrive
    st.tokens = 0;
    st.tokensPerSecond = 0;
    st.liveChunksPerSecond = 0;
    st.lastChunksPerSecond = 0;
    // live stream telemetry: per-chunk arrivals make the statusbar a
    // visible proof that the frontend receives chunks as they come
    const chunkTimes: number[] = [];
    const onChunk = (len: number) => {
      const now = Date.now();
      chunkTimes.push(now);
      while (chunkTimes.length && now - (chunkTimes[0] ?? now) > 1000) chunkTimes.shift();
      st.liveChunksPerSecond = chunkTimes.length;
      // ponytail: chars/4 token estimate - corrected by real usage at end
      st.tokens += Math.ceil(len / 4);
    };

    const handlers: StreamHandlers = {
      onThinking: (content) => {
        if (!thinking) {
          // adopt the merge's open thinking block when the restored
          // item is still streaming (no duplicate block on reattach)
          thinking = (restoredTail("thinking") as ThinkingItem | null) ?? null;
          if (!thinking) {
            thinking = { kind: "thinking", content: "", streaming: true };
            st.transcript.push(thinking);
            // reactivity: same proxy re-capture as pushAssistant
            thinking = st.transcript[st.transcript.length - 1] as ThinkingItem;
          }
        }
        thinking.content += content;
        onChunk(content.length);
        st.notify?.();
      },
      onResponse: (content) => {
        if (thinking?.streaming) thinking.streaming = false;
        if (segmentBreak) {
          // a tool phase closed the previous segment - start a fresh
          // bubble so the final answer renders after the tool lines
          assistant.streaming = false;
          assistant = {
            kind: "message",
            role: "assistant",
            content: "",
            streaming: true,
          };
          assistantPushed = false;
          segmentBreak = false;
        }
        // adopt the merge's open response block (streaming restored
        // assistant bubble): the run's text continues into it
        if (!assistantPushed) {
          const tail = restoredTail("message") as MessageItem | null;
          if (tail) {
            assistant = tail;
            assistantPushed = true;
          }
        }
        pushAssistant();
        assistant.content += content;
        onChunk(content.length);
        st.notify?.();
      },
      onToolCall: (tool) => {
        segmentBreak = true;
        if (tool.id) toolNames.set(tool.id, tool.name);
        if (thinking) {
          thinking.streaming = false;
          thinking = null;
        }
        // the segment's text is over - the tool lines carry the activity
        // now; the next onResponse builds a fresh streaming bubble
        assistant.streaming = false;
        if (tool.name === "request_verdict") {
          // Verdict cards v1 (card t_2ecf9c51): the card renders ABOVE
          // the tool line as the interactive surface. issuedAt anchors
          // the client countdown; the matching tool_result settles it.
          // The args arrive pre-serialized on this path - the parse is
          // verbatim the JSON the server built the prompt from.
          let parsed: unknown = null;
          try {
            parsed = tool.output ? JSON.parse(tool.output) : null;
          } catch {
            parsed = null;
          }
          const card = verdictCardFromToolArgs(parsed);
          card.callId = tool.id;
          card.issuedAt = Date.now();
          card.timeoutS = card.timeoutS && card.timeoutS > 0 ? card.timeoutS : 1200; // 20 min default
          st.transcript.push(card);
          st.notify?.();
          return;
        }
        st.transcript.push({
          kind: "tool",
          name: tool.name,
          status: "running",
          output: tool.output,
        });
        st.notify?.();
      },
      onToolResult: (result) => {
        // a verdict's result settles its card INSTEAD of the tool-line
        // path - the card IS the render (no plain tool line was pushed
        // for request_verdict), so the materialize fallback must not
        // ghost a bare "tool" row for it
        for (let i = st.transcript.length - 1; i >= 0; i--) {
          const item = st.transcript[i];
          if (item?.kind !== "verdict" || item.status !== "pending") continue;
          if (result.toolCallId && item.callId && result.toolCallId !== item.callId) continue;
          const settled = verdictFromResultText(result.content);
          item.status = "settled";
          item.verdictKind = settled.verdictKind || "expired";
          item.picked = settled.picked;
          item.answerText = settled.answerText;
          st.notify?.();
          return;
        }
        // attach the result to the most recent tool line (status -> done)
        for (let i = st.transcript.length - 1; i >= 0; i--) {
          const item = st.transcript[i];
          if (item?.kind !== "tool") continue;
          if (item.status !== "running") continue;
          item.status = result.isError ? "error" : "done";
          item.result = result.content;
          st.notify?.();
          return;
        }
        // No running tool line exists: the result's call happened on the
        // OTHER side of the refresh boundary (the call frame was
        // broadcast between the transcript snapshot and the attach
        // subscribe, or the call predates a mid-phase refresh). Drop is
        // not an option - it would erase the tool's work from the view
        // entirely. Materialize the row retroactively, named by the
        // call id when this stream saw the call, result attached.
        st.transcript.push({
          kind: "tool",
          name: (result.toolCallId ? toolNames.get(result.toolCallId) : undefined) ?? "tool",
          status: result.isError ? "error" : "done",
          result: result.content,
        });
        st.notify?.();
      },
      onStatus: (s) => {
        st.statusLine = s;
      },
      onUsage: (usage) => {
        st.tokens = usage.promptTokens + usage.completionTokens;
        const seconds = (Date.now() - sentAt) / 1000;
        st.tokensPerSecond =
          seconds > 0.2 ? Math.round(usage.completionTokens / seconds) : 0;
      },
    };

    return {
      handlers,
      // appendError folds a transport-independent failure (agent error
      // payload, non-2xx /chat) into the current assistant bubble
      appendError(e: unknown) {
        assistant.content += assistant.content ? `\n[error: ${e}]` : `[error: ${e}]`;
      },
      // finalize freezes the chunk meter, freezes running tools (results
      // never coming - ToolCallLine stops ticking), closes thinking and
      // bubbles, and carries the state's end-of-run bookkeeping.
      finalize() {
        st.lastChunksPerSecond = st.liveChunksPerSecond;
        st.liveChunksPerSecond = 0;
        // the sweep covers OWNED items (the driver's running tools,
        // its thinking, its assistant) AND adopted ones the merge
        // restored streaming but this stream never continued (the run
        // moved to another phase before the first post-refresh frame)
        for (const item of st.transcript) {
          if (item.kind === "tool" && item.status === "running")
            item.interrupted = true;
          if ((item.kind === "thinking" || item.kind === "message") && item.streaming)
            item.streaming = false;
          // a verdict card still pending at finalize: the stream's last
          // hope (the tool_result this session's run had) is gone - the
          // card renders as dead, never actionable. st.transcript is
          // reactive across two tabs on one session; the OTHER tab's
          // stream may still settle it, but this tab's view must not
          // promise a click that can still run.
          if (item.kind === "verdict" && item.status === "pending") item.stale = true;
        }
        // thinking is assigned inside stream callbacks the type system
        // can't track across the closure - cast to the runtime type
        const liveThinking = thinking as ThinkingItem | null;
        if (liveThinking?.streaming) liveThinking.streaming = false;
        // interrupted with no output keeps only the marker, no empty
        // ghost bubble; a run with zero events (404 reattach race) also
        // pushes nothing. The no-ghost rule is about CONTENT: thinking
        // blocks and tool lines render through their own items - a run
        // with tool activity but zero text pushes no empty bubble
        // (the orphan-result materialize ends exactly like that).
        if (assistant.content) pushAssistant();
        assistant.streaming = false;
        // the live transcript IS the session's content now: a remount
        // may refetch, but a stream that just ended never needs to
        st.loaded = true;
      },
    };
  }
  type StreamDriver = ReturnType<typeof makeStreamDriver>;

  // refetchTranscript replaces this state's view with the server's
  // rendered ACTIVE PATH (card t_a62eeb43): the ids + variants block the
  // live transcript never carries arrive only on the wire. Callers:
  // a run that just ended (a retry mid-session just forged a variant -
  // the switcher must appear without a manual reload) and a variant
  // select. Nothing is fetched while a run is live on the SERVER (the
  // runHub tail would race the restore) or when the fetch fails (the
  // live view stays). Sets loaded - the view IS the session's content.
  async function refetchTranscript(st: ChatState): Promise<void> {
    const sessionId = st.sessionId;
    if (!sessionId) return;
    try {
      const detail = await fetchTranscript(sessionId);
      if (detail.running) return;
      st.variants = detail.variants ?? [];
      st.transcript = renderTranscriptItems(detail.items);
      // a fork anchor survives only while its target stays rendered
      if (st.forkFrom && !st.transcript.some((i) => i.kind === "message" && i.id === st.forkFrom)) {
        st.forkFrom = null;
      }
      st.loaded = true;
      st.notify?.(true);
    } catch {
      // keep the previous view on a transient fetch failure
    }
  }

  // runStream drives one interaction over a transport: start() performs
  // the POST /chat (send path) or the GET /sessions/<id>/events attach
  // (tab attach path) and consumes the SSE with the driver's handlers.
  // Owns the per-STATE busy entry, error rendering (interrupted marker
  // vs error text vs the 409 busy line), read-marking for the focused
  // view and the refresh follow-ups.
  async function runStream(st: ChatState, start: (drv: StreamDriver) => Promise<void>) {
    const drv = makeStreamDriver(st);
    busyKeys.value = withBusy(busyKeys.value, st.key, true);
    // the stop affordance is SESSION-keyed: either tab's cancel stops
    // the one server-side run. Harmless after the run already ended
    // (a benign 404/409 race). The id may ARRIVE after the stream
    // opened (auto-create sent no session_id): read it at call time,
    // never closure-frozen.
    st.cancelRun = () => {
      if (st.sessionId) stopSession(st.sessionId).catch(() => {});
    };
    try {
      await start(drv);
    } catch (e) {
      if (e instanceof StreamInterruptedError) {
        // transport death mid-stream: the partial bubble stays exactly
        // as it is - the marker carries the resume affordance instead
        // of appending error text to it
        st.transcript.push({ kind: "interrupted" });
      } else if (e instanceof SessionBusyError) {
        // the runHub 409: nothing was run, nothing persisted - show the
        // busy state, never an error splat in the bubble
        st.transcript.push({
          kind: "interrupted",
          label: "session busy",
        });
      } else {
        drv.appendError(e);
      }
    } finally {
      drv.finalize();
      busyKeys.value = withBusy(busyKeys.value, st.key, false);
      st.attached = false;
      st.cancelRun = undefined;
      st.statusLine = "";
      // A stream ending on the FOCUSED tab never unreads itself - the
      // user watched the content live, so it is marked read. A
      // background stream (this page or another) leaves its row
      // unread: that is the feature. Ephemeral sessions have no row:
      // the read-stamp POST would only 204 into nothing.
      if (st.sessionId && !st.ephemeral && deps.isFocused(st.key)) deps.markRead(st.sessionId);
      // refresh once the row carries the run's persisted numbers - the
      // sidebar's pulse, the title and the durable telemetry feed from it
      await deps.refreshSessions();
      const row = st.sessionId ? deps.sessionRow(st.sessionId) : undefined;
      if (row) {
        // the run's persisted totals become the state's idle telemetry -
        // the bar's last-known values and the post-reload display come
        // from the SAME source (the row), not from the live meter's
        // instantaneous last-second tick
        const durable = persistedTelemetry(row);
        st.tokens = durable.tokens;
        st.tokensPerSecond = durable.tps;
        st.lastChunksPerSecond = durable.cps;
      }
      // the title goroutine writes the session label seconds AFTER the
      // run really ends - the refresh above runs too early to see it.
      // Ephemeral sessions never grow a row or a label - skip the timer.
      if (!st.ephemeral && (!row || !row.label)) {
        setTimeout(() => {
          deps.refreshSessions();
        }, TITLE_REFRESH_MS);
      }
      // A run that just ended may have forged or moved variants (a
      // retry's fresh sibling, a fork's new sub-branch, a cancel's
      // partial). The live transcript has ids on nothing - one refetch
      // brings the ids, the variants block and the server-derived path
      // in. Skip it when an interrupted marker is present: the marker
      // is per-tab live state the restore does not carry (load() keeps
      // only a REFETCHED one, and running:false would drop ours).
      // Ephemeral sessions have no store tree, no ids, no variants -
      // the in-memory registry serves restore on reload (load()), the
      // live view IS the content here.
      if (!st.ephemeral && !hasInterruptedMarker(st.transcript)) await refetchTranscript(st);
      st.notify?.();
      if (st.orphan) {
        states.delete(st.key);
        dropEphemeralServerRecord(st.sessionId, st.key);
      }
    }
  }

  async function load(key: string, sessionId: string): Promise<void> {
    const st = ensureState(key, sessionId);
    // A LOCALLY live stream on THIS state is the content - refetching
    // now would clobber the stream state with a mid-flight persisted
    // snapshot. A run feeding the session from ANOTHER tab/window is
    // NOT a reason to skip: this state holds no live transcript, the
    // fetch below renders persisted history and the attach lands in
    // the live stream.
    if (busyKeys.value.has(key) || st.attached) {
      st.notify?.(true);
      return;
    }
    st.notify?.(true);
    // an interrupted-stream marker is per-tab live state: the refetch
    // below restores PERSISTED history, not the marker - re-append it so
    // the resume affordance survives a view switch (retry is per-tab)
    const hadInterrupted = hasInterruptedMarker(st.transcript);
    try {
      const detail = await fetchTranscript(sessionId);
      // durable telemetry straight from the persisted row: the token
      // total and the last-run rates land in the status bar before any
      // stream runs - reload shows what the session actually has
      const durable = persistedTelemetry(detail.session);
      st.tokens = durable.tokens;
      st.tokensPerSecond = durable.tps;
      st.lastChunksPerSecond = durable.cps;
      // restore IS rendering: the server hands over wire-order
      // transcript items and renderTranscriptItems maps them onto the
      // same component contract the live stream paints (the
      // backend-owns-facts rework) - no reload-side reconstruction loop
      st.transcript = renderTranscriptItems(detail.items);
      // the branch points ride the restore (t_a62eeb43): the switcher
      // and the fork affordance read this block. Absent key = plain
      // session, an empty list keeps the shape total.
      st.variants = detail.variants ?? [];
      // a stale resume affordance retires when its premise died: a
      // session with a live run proves the stream did NOT end - the
      // merge below is now the content, and the fresh attach appends
      // its own marker only if ITS transport actually dies
      if (hadInterrupted && !detail.running) st.transcript.push({ kind: "interrupted" });
      st.loaded = true;
      // Reattach: the run is server-side now, so a session
      // mid-generation reports running=true on its transcript - the
      // mount renders the persisted history (the user message persisted
      // pre-run, nothing after it yet) and then auto-subscribes to the
      // live events stream. Multi-subscriber is fine (settled ruling).
      if (detail.running && !busyKeys.value.has(key)) {
        st.attached = true;
        await runStream(st, async (drv) => {
          await attachSession(sessionId, drv.handlers);
        });
      }
    } catch {
      // empty or missing session: keep this tab's previous view rather
      // than dropping it on a transient fetch failure
    }
    st.notify?.(true);
  }

  async function submit(
    key: string,
    text: string,
    model?: string,
    thinking?: number,
  ): Promise<boolean> {
    const st = ensureState(key);
    if (!text && !st.attachments.length) return false;
    // per-session gate: the run slot is per session - a live run on the
    // SAME session anywhere (either of this page's tabs, the server's
    // active set) blocks a second send locally; the runHub 409 is the
    // backstop for the window the client set cannot see.
    if (st.sessionId && busyAll.value.has(st.sessionId)) return false;
    // unsaved chat create window: one send at a time per tab
    if (!st.sessionId && busyKeys.value.has(key)) return false;
    // FORK anchor (t_a62eeb43): a set anchor changes the send's target -
    // the forked message chains onto the anchor server-side (context
    // truncates there) and the live-view user bubble splices in right
    // after it. A stale anchor (its bubble left the transcript through
    // a refetch/swap) silently degrades to a plain send.
    const forkID = st.forkFrom && findMessageItem(st.transcript, st.forkFrom) ? st.forkFrom : null;
    st.forkFrom = null;
    // a fresh send retires a stale resume affordance - after a later
    // exchange the marker no longer describes the transcript state
    if (hasInterruptedMarker(st.transcript))
      st.transcript = withoutMarkers(st.transcript);
    const userItem: MessageItem = {
      kind: "message",
      role: "user",
      content: text,
      images: st.attachments.length ? st.attachments.slice() : undefined,
      time: new Date().toISOString(),
    };
    if (forkID) {
      const at = st.transcript.findIndex((i) => i.kind === "message" && i.id === forkID);
      if (at >= 0) st.transcript.splice(at + 1, 0, userItem);
      else st.transcript.push(userItem);
    } else {
      st.transcript.push(userItem);
    }
    st.notify?.(true);

    // The create-then-send dance is GONE (Endo 2026-10-18: /chat is
    // always durable): a null sessionId POSTs /chat bare and the
    // server auto-creates the session - the leading `session` SSE
    // frame carries the id, the onSession write-back binds it onto
    // the state and the dock tree (URL /chat/<id>, sidebar row) while
    // the run streams.
    const images = st.attachments.slice();
    st.attachments = [];
    await runStream(st, (drv) =>
      streamChat(
        [{ role: "user", content: text, images: images.length ? images : undefined }],
        st.sessionId,
        {
          ...drv.handlers,
          onSession: (id) => {
            st.sessionId = id;
            deps.setTabSessionRef(key, id);
            deps.refreshSessions();
          },
        },
        model,
        thinking,
        false,
        forkID ?? undefined,
        // ephemeral: no-persist contract on the wire - the server mints
        // (or continues) the `eph-` session in memory, denies the
        // memory tool, and never writes the store (card t_d96a5cd5)
        st.ephemeral,
      ),
    );
    return true;
  }

  // retry re-runs the session's last user message through the SERVER
  // (t_357cbe9c): the /chat retry flag replays the persisted last user
  // turn - no duplicate user bubble is stored, the previous (partial or
  // complete) response stays put and the fresh one appends BELOW it.
  // No client transcript surgery, reload-safe; the run slot, the 409
  // gate and the cancel semantics are the normal send's. The old
  // client-side resend path (submit-the-old-text) is retired.
  async function retry(key: string, model?: string, thinking?: number): Promise<boolean> {
    const st = ensureState(key);
    if (composerBusy(key)) return false;
    // a retry has to land in an EXISTING session - an unsaved tab has
    // nothing persisted to replay
    const sessionId = st.sessionId;
    if (!sessionId) return false;
    // ephemeral: nothing persisted to replay - the server refuses the
    // flag combination, the affordance never renders there (ChatTab
    // gates canRetry off the same bit)
    if (st.ephemeral) return false;
    // the transcript's last user message is the retry target: the
    // affordance exists only when an exchange sits there, and the
    // server re-validates against the persisted history (trailing
    // assistant partials never reach the wire - the server replays
    // up to the last user turn it stored)
    if (lastUserText(st.transcript) === null) return false;
    // a stale resume affordance retires when its retry fires - the
    // fresh stream appends its own marker if it dies the same way
    if (hasInterruptedMarker(st.transcript))
      st.transcript = withoutMarkers(st.transcript);
    await runStream(st, (drv) =>
      // messages stay empty: the body is the retry flag alone
      streamChat([], sessionId, drv.handlers, model, thinking, true),
    );
    return true;
  }

  function attachFiles(key: string, images: OmegaImage[]): void {
    const st = ensureState(key);
    st.attachments = st.attachments.concat(images);
  }

  function removeAttachment(key: string, index: number): void {
    const st = ensureState(key);
    st.attachments = st.attachments.filter((_, i) => i !== index);
  }

  async function attachUrl(key: string, url: string): Promise<void> {
    const st = ensureState(key);
    try {
      st.attachments.push(await attachImageUrl(url));
    } catch (e) {
      st.statusLine = `attach failed: ${e}`;
    }
  }

  // selectVariant flips a branch point's active variant server-side
  // (t_a62eeb43); the re-fetch re-renders the ACTIVE PATH - everything
  // after the branch point swaps to the selected branch. CONTINUE =
  // SELECT: the composer's next plain send POSTs /chat with no fork,
  // and the server's context IS the selected path - the composer sends,
  // it never needs to know the branch shape.
  async function selectVariant(
    key: string,
    messageId: string,
    index?: number,
  ): Promise<boolean> {
    const st = ensureState(key);
    if (composerBusy(key) || !st.sessionId) return false;
    try {
      await postVariantSelect(st.sessionId, { message_id: messageId, select: index });
    } catch (e) {
      st.statusLine = `variant select failed: ${e}`;
      return false;
    }
    await load(key, st.sessionId);
    return true;
  }

  // fork anchors the NEXT send at a historical message (t_a62eeb43,
  // per Endo's ruling - the affordance is explicit and visible): an
  // assistant target sets forkFrom, the composer chip shows it and the
  // next send POSTs /chat {fork: id}. A user target carries no message
  // (the server's rule: a user-turn fork takes no messages - replay
  // semantics), so it reruns that turn immediately: same /chat {fork}
  // stream, empty messages. Both sides are gated on the session's run
  // slot; a target not in the transcript (stale) is a no-op.
  async function fork(
    key: string,
    messageId: string,
    model?: string,
    thinking?: number,
  ): Promise<void> {
    const st = ensureState(key);
    if (composerBusy(key) || !st.sessionId) return;
    const target = findMessageItem(st.transcript, messageId);
    if (!target) return;
    if (target.role === "user") {
      // replay at that turn: a new variant of that user message's
      // response, appended below (the run-stream refetch renders it)
      await runStream(st, (drv) =>
        streamChat([], st.sessionId!, drv.handlers, model, thinking, false, messageId),
      );
      return;
    }
    st.forkFrom = messageId;
    st.notify?.();
  }

  function clearFork(key: string) {
    const st = states.get(key);
    if (!st?.forkFrom) return;
    st.forkFrom = null;
    st.notify?.();
  }

  function cancel(key: string) {
    states.get(key)?.cancelRun?.();
  }

  // composerBusy is the PER-SESSION gate: the session's run slot, read
  // through busyAll (this page's live streams mapped by session + the
  // server's active set). Two tabs on one live session both show the
  // streaming state; the second send from either gets the busy
  // affordance, not a double run.
  function composerBusy(key: string): boolean {
    const st = states.get(key);
    if (!st) return false;
    return st.sessionId ?
      busyAll.value.has(st.sessionId)
      : busyKeys.value.has(key);
  }

  async function attach(key: string): Promise<void> {
    const st = states.get(key);
    if (!st?.sessionId || st.attached) return;
    if (busyKeys.value.has(key)) return;
    st.attached = true;
    await runStream(st, async (drv) => {
      await attachSession(st.sessionId!, drv.handlers);
    });
  }

  // dropEphemeralServerRecord enforces the close = gone contract: when
  // the LAST state bound to an `eph-` session stops being rendered, the
  // server's in-memory record is deleted (card t_d96a5cd5). A miss
  // (record already gone, another window still bound) is a benign
  // race / no-op, never an error surfaces to the user.
  function dropEphemeralServerRecord(sessionId: string | null, exceptKey: string | null) {
    if (!sessionId || !sessionId.startsWith("eph-")) return;
    for (const [key, s] of states.entries()) {
      if (key !== exceptKey && s.sessionId === sessionId) return;
    }
    deleteSession(sessionId).catch(() => {});
  }

  function closeState(key: string) {
    const st = states.get(key);
    if (!st) return;
    if (busyKeys.value.has(key)) st.orphan = true;
    else {
      states.delete(key);
      dropEphemeralServerRecord(st.sessionId, key);
    }
  }

  function closeSession(sessionId: string) {
    for (const [key, st] of [...states.entries()]) {
      if (st.sessionId !== sessionId) continue;
      closeState(key);
    }
    dropEphemeralServerRecord(sessionId, null);
  }

  return {
    states,
    busyKeys,
    busyAll,
    anyBusyNow,
    ensureState,
    load,
    attach,
    submit,
    attachFiles,
    removeAttachment,
    attachUrl,
    retry,
    selectVariant,
    fork,
    clearFork,
    cancel,
    composerBusy,
    closeState,
    closeSession,
    noteServerActive,
  };
}