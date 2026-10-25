// omega webui: the chat host - the composer transport side of chat.
// chat-last 2a: the CONVERSATION renders from the canvas chat panel
// (snapshot at load, view_model_delta frames after - batched cadence on
// the backend, card t_ec45b670). The per-tab transcript copies, the SSE
// stream driver that composed them incrementally and the restore-side
// live-run adoption are deleted: the deltas are the streaming render.
// What survives per tab: the composer transport (a send still POSTs
// /chat: it binds the auto-created session and carries the fork anchor;
// stop and retry dispatch through the action pipe), telemetry, the
// variants block (REST surface, stays) and the restore's fallback
// transcript for sessions the single chat panel does not cover (the
// panel holds ONE conversation - the focus law: a live run's session,
// else the newest unarchived; this falls back to REST for the rest).
//
// consequence: stream handlers no longer build rendering state
// (StreamHandlers carries only usage/session/canvas-delta callbacks);
// busy stays PER SESSION (the runHub run slot), keyed by the tab's
// state key locally and by session id server-side.

import { computed, reactive, ref, type ComputedRef, type InjectionKey, type Ref } from "vue";
import {
  attachSession,
  chatPanelContent,
  lastUserText,
  normalizeChatPayload,
  persistedTelemetry,
  postCanvasAction,
  postVariantSelect,
  renderTranscriptItems,
  streamChat,
  verdictCardFromToolArgs,
  verdictFromResultText,
  withBusy,
  attachImageUrl,
  deleteSession,
  type BusyMap,
  type CanvasPanel,
  type ChatPayloadWire,
  type ChatRowWire,
  type OmegaImage,
  type Session,
  type StreamHandlers,
  type MessageItem,
  type ThinkingItem,
  type ToolItem,
  type TranscriptItem,
  type VerdictItem,
  type VariantBlock,
  SessionBusyError,
  StreamInterruptedError,
  fetchTranscript,
} from "./omega.ts";

// The chat actions are module-qualified on the wire: http_channel owns
// the chat panel's stop/retry handlers (the pipe checks module + id).
const CHAT_MODULE = "http_channel";

export interface ChatState {
  key: string;
  // the bound session id; null while the tab's chat is unsaved
  // (composer-first) - bare /chat auto-creates it and the id arrives
  // on the response's leading `session` SSE frame
  sessionId: string | null;
  // the REST restore's items for this session (GET
  // /sessions/{id}/transcript rendered): the fallback transcript the
  // tab shows when the canvas chat panel does not cover it. Rendered
  // items only - the LIVE view is the canvas panel's job.
  restored: TranscriptItem[];
  // the composer draft lives per state: switching tabs (unmount) keeps
  // half-written input, exactly like a VS Code editor keeps its buffer
  draft: string;
  // pending image attachments, per tab like the draft: paste/picker/
  // url-attach collect here and the next send carries them
  attachments: OmegaImage[];
  // the non-activity line above the composer: interrupted runs, pipe
  // failures, attach failures. Phase narration is derived (activityPhase).
  statusLine: string;
  tokens: number;
  tokensPerSecond: number;
  // the live run's delta arrivals/s (the canvas cadence proof: the
  // renderer genuinely repaints on deltas as they come)
  liveDeltasPerSecond: number;
  // last-known delta rate, from the run that just ended - the idle
  // state bar's rate display. The durable persisted rate still rides
  // lastChunksPerSecond (the row's last_run block).
  lastDeltasPerSecond: number;
  // last-known chunks/s, from the session row's last_run block (or
  // the run that just ended) - the idle state bar's rate display
  lastChunksPerSecond: number;
  // a live SSE is bound to THIS state (its own subscription)
  attached?: boolean;
  // its tab was closed (or its session deleted) while the run was
  // live: the state stops being rendered - dropped at stream end
  orphan?: boolean;
  // this state already carries the session's full content (a finished
  // live run, or a successful restore) - the unsaved->created flow sets
  // it so the post-create watcher never clobbers the view
  loaded?: boolean;
  // the session's branch points (t_17365247 wire block, restored with
  // the transcript and refreshed after every select + run end). Empty
  // for sessions without variants - the switcher renders nothing then.
  variants: VariantBlock[];
  // the composer's FORK anchor (t_a62eeb43): a historical message the
  // next send continues FROM (POST /chat {fork}). Validity is the
  // view's concern (the anchor must resolve against the rendered
  // transcript); replay semantics (a user-turn target reruns the turn
  // immediately) ride the same state.
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
  restored: [],
  draft: "",
  attachments: [],
  statusLine: "",
  tokens: 0,
  tokensPerSecond: 0,
  liveDeltasPerSecond: 0,
  lastDeltasPerSecond: 0,
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
  /** one canvas panel delta arrived on a run stream (arc 3c) - the
      app-level canvas store merges it; absent = no canvas, the frame
      is dropped (the snapshot is the catch-up, same as the server) */
  canvasDelta?(panel: CanvasPanel): void;
  /** the canvas' chat panel descriptor (the renderer's one streaming
      source); null when the canvas store holds no chat-kind panel yet */
  chatPanel?(): CanvasPanel | null;
  /** the canvas snapshot refetch - variant selects and action calls
      repaint the panel through it (the deltas ride run streams only) */
  canvasRefresh?(): void | Promise<void>;
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
  /** the canvas' chat panel descriptor (view source accessor) */
  chatPanel(): CanvasPanel | null;
  /** mount-time restore of one tab's view: persisted history rebuild +
      telemetry, reading the detail's live-run flag to attach. Refetch
      is skipped while this state itself is attached/busy (a refetch
      would clobber the live view with a mid-flight snapshot). */
  load(key: string, sessionId: string): Promise<void>;
  /** subscribe to the session's live run for THIS tab's state (the
      duplicate-tab path: the other tab already streams locally) */
  attach(key: string): Promise<void>;
  /** the ONE send path (composer submit). Returns false when the
      per-session gate refused (double send, never double run). The
      fork anchor is the caller's validity (it resolves against the
      rendered transcript). */
  submit(
    key: string,
    text: string,
    model?: string,
    thinking?: number,
    fork?: string,
  ): Promise<boolean>;
  /** add picked/pasted images to this tab's pending attachments */
  attachFiles(key: string, images: OmegaImage[]): void;
  /** remove one pending attachment by index */
  removeAttachment(key: string, index: number): void;
  /** server-side fetch of an image URL into this tab's attachments */
  attachUrl(key: string, url: string): Promise<void>;
  /** set / clear the composer's FORK anchor (the view resolved the
      target against the rendered transcript already) */
  setFork(key: string, messageId: string | null): void;
  /** replay a USER-turn anchor immediately (fork of a user message
      reruns that turn - the same /chat {fork} stream, no messages) */
  replayFork(key: string, messageId: string, model?: string, thinking?: number): Promise<void>;
  /** variant navigation (t_a62eeb43): select the 0-based sibling at a
      branch point, refresh the view (canvas snapshot + restore);
      false while the session's run slot is taken */
  selectVariant(key: string, messageId: string, index?: number): Promise<boolean>;
  /** stop THIS tab's bound session's live run - through the chat
      panel's action pipe (the one canvas route; no REST stop affordance) */
  cancel(key: string): void;
  /** retry: the chat panel's retry action through the pipe - the
      server replays the session's last user turn as a sibling variant.
      False while the gate refused or the pipe call failed. */
  retry(key: string): Promise<boolean>;
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
// message, never a replay. Retry/stop dispatch through the action
// pipe (the same faces the panel declares); send stays native /chat.
// Pure so the morph states stay testable.

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
// from the rendered transcript + the session's busy gate - no new
// state. The phase source is the canvas-rendered items (or the restore
// fallback): a call still open (its result has not landed yet) is the
// run's activity, else the last streaming item's kind narrates the
// stream. "thinking" is also what a run shows before anything lands.

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

// findMessageItem locates a rendered MESSAGE item by its store row id
// (the variants surface's address - fork + switcher). Canvas-mapped
// items carry ids for stored rows, live rows carry none and are never
// found here. Pure.
export function findMessageItem(
  items: TranscriptItem[],
  id: string,
): MessageItem | null {
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it?.kind === "message" && it.id === id) return it;
  }
  return null;
}

export function activityPhase(
  items: TranscriptItem[],
  busy: boolean,
): ActivityPhase | null {
  if (!busy) return null;
  // newest open tool call + newest streaming item
  let tool: ToolItem | null = null;
  let streaming: ThinkingItem | MessageItem | null = null;
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    if (
      tool === null &&
      item?.kind === "tool" &&
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
      const fresh: ChatState = {
        key,
        sessionId,
        restored: [],
        draft: "",
        attachments: [],
        statusLine: "",
        tokens: 0,
        tokensPerSecond: 0,
        liveDeltasPerSecond: 0,
        lastDeltasPerSecond: 0,
        lastChunksPerSecond: 0,
        variants: [],
        forkFrom: null,
        ephemeral: !!ephemeral,
      };
      states.set(key, fresh);
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

  function chatPanel(): CanvasPanel | null {
    return deps.chatPanel?.() ?? null;
  }

  function noteServerActive(ids: string[]) {
    // server-reported live runs REPLACE (not union into) the snapshot:
    // these are the sessions a freshly refreshed page sees as pulsing -
    // local streams carry their own busy entries in busyKeys
    serverBusy.value = new Set(ids);
  }

  // makeTransportHandlers builds the SSE handler set one stream runs
  // with: usage telemetry + delta metering (the live run's repaints/s)
  // + the canonical canvas forward. NO transcript composition - the
  // renderer paints from the canvas panel's deltas.
  function makeTransportHandlers(st: ChatState): StreamHandlers {
    const sentAt = Date.now();
    // a fresh stream takes the meters over: the last-known idle values
    // step aside until this run's own numbers arrive
    st.tokens = 0;
    st.tokensPerSecond = 0;
    st.liveDeltasPerSecond = 0;
    st.lastDeltasPerSecond = 0;
    st.lastChunksPerSecond = 0;
    // the delta rate meter: arrival times of the panel repaints (the
    // canvas cadence, batched server-side) make the statusbar a visible
    // proof that the frontend receives the stream as it comes
    const deltaTimes: number[] = [];
    const meter = (payload: ChatPayloadWire | null) => {
      const now = Date.now();
      deltaTimes.push(now);
      while (deltaTimes.length && now - (deltaTimes[0] ?? now) > 1000) deltaTimes.shift();
      st.liveDeltasPerSecond = deltaTimes.length;
      if (payload) {
        // the streaming token estimate rides the panel content the
        // deltas carry (assistant prose + thinking, chars/4) - corrected
        // by the real usage frame at stream end
        st.tokens = Math.ceil(chatPanelContent(payload) / 4);
      }
    };
    return {
      onSession: undefined, // the send path overrides (below)
      onUsage: (usage) => {
        st.tokens = usage.promptTokens + usage.completionTokens;
        const seconds = (Date.now() - sentAt) / 1000;
        st.tokensPerSecond =
          seconds > 0.2 ? Math.round(usage.completionTokens / seconds) : 0;
      },
      onViewModelDelta: (panel) => { deps.canvasDelta?.(panel);
        if (panel.kind === "chat") meter(normalizeChatPayload(panel.payload));
      },
    };
  }

  // refreshVariants pulls the session's branch block again - a run that
  // just forged a sibling (retry, fork, cancel partial) needs no manual
  // reload for the switcher. Id-ful rows + the rendered items the
  // variantOf resolution needs come from the same fetch.
  async function refreshVariants(st: ChatState): Promise<void> {
    const sessionId = st.sessionId;
    if (!sessionId || st.ephemeral) return;
    try {
      const detail = await fetchTranscript(sessionId);
      if (detail.running) return;
      st.variants = detail.variants ?? [];
      st.forkFrom = null; // stale anchors retire: resolved against the view
    } catch {
      // transient fetch failure keeps the previous block
    }
  }

  // runStream drives one transport interaction (the send's POST /chat
  // or the attach GET /sessions/<id>/events): busy bookkeeping, error
  // surfacing into statusLine (the transcript belongs to the canvas -
  // errors are transport concerns, not transcript items), read-marking
  // for the focused view and the refresh follow-ups.
  async function runStream(st: ChatState, start: (handlers: StreamHandlers) => Promise<unknown>) {
    busyKeys.value = withBusy(busyKeys.value, st.key, true);
    const handlers = makeTransportHandlers(st);
    try {
      st.statusLine = "";
      await start(handlers);
    } catch (e) {
      if (e instanceof StreamInterruptedError) {
        // transport death mid-stream: the panel keeps its last paint -
        // the exit flush (server-side) restores the persisted truth,
        // this line names the dead transport for the resume affordance
        st.statusLine = "stream interrupted";
      } else if (e instanceof SessionBusyError) {
        // the runHub 409: nothing was run, nothing persisted
        st.statusLine = "session busy";
      } else {
        st.statusLine = `[error: ${e instanceof Error ? e.message : String(e)}]`;
      }
    } finally {
      busyKeys.value = withBusy(busyKeys.value, st.key, false);
      st.attached = false;
      st.lastDeltasPerSecond = st.liveDeltasPerSecond;
      st.liveDeltasPerSecond = 0;
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
      // variants may have moved (a retry's fresh sibling, a fork's new
      // sub-branch, a cancel's partial) - one fetch, switcher without
      // a manual reload
      await refreshVariants(st);
      if (st.orphan) {
        states.delete(st.key);
        dropEphemeralServerRecord(st.sessionId, st.key);
      }
    }
  }

  async function load(key: string, sessionId: string): Promise<void> {
    const st = ensureState(key, sessionId);
    // a stream live on THIS state owns its feed - the panel repaints
    // (local deltas) are current; refetching now would trade them for
    // a mid-flight snapshot
    if (busyKeys.value.has(key) || st.attached) return;
    try {
      const detail = await fetchTranscript(sessionId);
      // durable telemetry straight from the persisted row: the token
      // total and the last-run rates land in the status bar before any
      // stream runs - reload shows what the session actually has
      const durable = persistedTelemetry(detail.session);
      st.tokens = durable.tokens;
      st.tokensPerSecond = durable.tps;
      st.lastChunksPerSecond = durable.cps;
      // restore IS rendering for the sessions the canvas panel does not
      // cover: wire-order transcript items rendered onto the same
      // component contract the canvas rows paint through
      st.restored = renderTranscriptItems(detail.items);
      // the branch points ride the restore (t_a62eeb43): the switcher
      // and the fork affordance read this block. Absent key = plain
      // session, an empty list keeps the shape total.
      st.variants = detail.variants ?? [];
      st.loaded = true;
      // Reattach: a session mid-generation reports running=true on its
      // transcript - this tab auto-subscribes to the live events stream
      // and feeds the canvas store from it (multi-subscriber is fine).
      if (detail.running && !busyKeys.value.has(key)) {
        st.attached = true;
        await runStream(st, (handlers) => attachSession(sessionId, handlers));
      }
    } catch {
      // empty or missing session: keep this tab's previous view rather
      // than dropping it on a transient fetch failure
    }
  }

  async function submit(
    key: string,
    text: string,
    model?: string,
    thinking?: number,
    fork?: string,
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
    // The create-then-send dance is GONE: a null sessionId POSTs /chat
    // bare and the server auto-creates the session - the leading
    // `session` SSE frame carries the id while the run streams. The
    // conversation paint is the canvas panel's (the run's beginRun
    // takes the panel over and the user turn rides the first delta).
    const images = st.attachments.slice();
    st.attachments = [];
    // the anchor is consumed by the send that carries it (cleared only
    // when this submit actually fires the forked body)
    if (fork && st.forkFrom === fork) st.forkFrom = null;
    await runStream(st, (handlers) =>
      streamChat(
        [{ role: "user", content: text, images: images.length ? images : undefined }],
        st.sessionId,
        {
          ...handlers,
          onSession: (id) => {
            st.sessionId = id;
            deps.setTabSessionRef(key, id);
            deps.refreshSessions();
          },
        },
        model,
        thinking,
        fork,
        // ephemeral: no-persist contract on the wire - the server mints
        // (or continues) the `eph-` session in memory, denies the
        // memory tool, and never writes the store (card t_d96a5cd5)
        st.ephemeral,
      ),
    );
    return true;
  }

  // retry dispatches the chat panel's retry action through the pipe:
  // the server replays the session's last user turn as a sibling
  // variant (detached) - the reattach + panel deltas are the surfaces,
  // no wire messages from here. The composer's model/thinking chips do
  // not ride the action (the pipe action carries the session alone) -
  // the replay takes the session's own run configuration.
  async function retry(key: string): Promise<boolean> {
    const st = ensureState(key);
    if (composerBusy(key)) return false;
    // a retry has to land in an EXISTING session - an unsaved tab has
    // nothing persisted to replay; ephemeral sessions refuse the
    // affordance the same way the server refuses the flag combination
    const sessionId = st.sessionId;
    if (!sessionId || st.ephemeral) return false;
    try {
      await postCanvasAction(CHAT_MODULE, "retry", { session: sessionId });
    } catch (e) {
      st.statusLine = `retry failed: ${e instanceof Error ? e.message : String(e)}`;
      return false;
    }
    st.statusLine = "";
    // the replay runs detached: subscribe for its deltas as soon as the
    // runHub shows the slot taken (attachSession 404s benignly if the
    // busy poll beats us - the reconcile watcher re-attaches then)
    if (!st.attached) {
      st.attached = true;
      await runStream(st, (handlers) => attachSession(sessionId, handlers));
    }
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

  function setFork(key: string, messageId: string | null): void {
    const st = states.get(key);
    if (!st) return;
    st.forkFrom = messageId;
  }

  // replayFork runs a USER-turn fork anchor immediately: the /chat fork
  // body takes no messages for a user target - replay semantics, the
  // fresh response appends as a sibling variant. A fork replay is a
  // SEND (native /chat, model + thinking ride it), not an action-pipe
  // call - the pipe's replay is the retry face.
  async function replayFork(
    key: string,
    messageId: string,
    model?: string,
    thinking?: number,
  ): Promise<void> {
    const st = ensureState(key);
    if (composerBusy(key) || !st.sessionId) return;
    await runStream(st, (handlers) =>
      streamChat([], st.sessionId!, handlers, model, thinking, messageId),
    );
  }

  // selectVariant flips a branch point's active variant server-side
  // (t_a62eeb43); the branch data re-reads (the switcher's « n/n » and
  // the fork chip are server-derived) and the view swaps to the active
  // path: the canvas snapshot refetch repaints a panel-covered tab,
  // the restore reload covers the rest.
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
    await Promise.all([deps.canvasRefresh?.() ?? Promise.resolve(), load(key, st.sessionId)]);
    return true;
  }

  function cancel(key: string) {
    const st = states.get(key);
    // the id read at call time: an auto-created session binds mid-send
    if (!st?.sessionId) return;
    postCanvasAction(CHAT_MODULE, "stop", { session: st.sessionId }).catch((e) => {
      st.statusLine = `stop failed: ${e instanceof Error ? e.message : String(e)}`;
    });
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
    await runStream(st, (handlers) => {
      return attachSession(st.sessionId!, handlers);
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
    chatPanel,
    load,
    attach,
    submit,
    attachFiles,
    removeAttachment,
    attachUrl,
    setFork,
    replayFork,
    retry,
    selectVariant,
    cancel,
    composerBusy,
    closeState,
    closeSession,
    noteServerActive,
  };
}

// --- the canvas transcript (chat-last 2a) --------------------------------
//
// createCanvasTranscript is the pane's render source for the chat
// panel: it maps the payload's rows onto the components' item shapes,
// KEEPING ITEM IDENTITY across deltas (a 250ms repaint must not remount
// every bubble - the tool elapsed meters, the verdict countdowns and
// the keyed v-for all read identity). Cached per row signature (the
// store row id when persisted, else the stable role/type slot), mutated
// in place, reused in the rebuilt array.
//
// REACTIVE ITEMS (t_078a2c35). The positional sig folds the sibling
// variant rows onto ONE cached item: variant-2's row 392 and variant-1's
// row 391 share `assistant:1:1`, so a select's refetch mutates the SAME
// cached object's fields in place (it.id among them - the switcher's
// label and the fork/copy targets ride item.id). The items flow into
// ChatItem through the keyed v-for as the same object reference, so the
// child's own props compare equal and the patch skips it - a plain
// object's in-place mutation can never invalidate anything. The render
// effects that read the fields (ChatItem's :item-id, ChatMessage's
// variantLabel) must see the mutation: cache items are reactive
// proxies, so every in-place write invalidates exactly its readers.

export interface CanvasTranscript {
  apply(payload: ChatPayloadWire): TranscriptItem[];
}

export function createCanvasTranscript(): CanvasTranscript {
  let session = "";
  const cache = new Map<string, TranscriptItem>();
  // the verdict countdowns anchor on first sight of the card (a live
  // card repaints every delta; issuedAt = first paint, the countdown
  // ticks once, settled by the result row's later delta)
  const issuedAt = new Map<string, number>();

  function item(row: ChatRowWire, index: number, panelRunning: boolean): TranscriptItem | null {
    const block = row.blocks?.[0];
    if (!block) return null;
    const running = row.status === "streaming";
    const blockRunning = running && block.running;
    // the identity sig: stable across a row's live->persisted transition
    // (ids arrive when the row persists - the object must not swap then)
    const sig = `${row.role}:${index}:${row.blocks?.length ?? 0}`;
    if (row.role === "user") {
      const it = (cache.get(sig) ?? reactive({ kind: "message", role: "user", content: "" })) as MessageItem;
      it.kind = "message";
      it.role = "user";
      it.content = block.content ?? "";
      it.images = block.images;
      it.id = row.id;
      cache.set(sig, it);
      return it;
    }
    if (row.role === "assistant" && block.type === "thinking") {
      const it = (cache.get(sig) ?? reactive({ kind: "thinking", content: "" })) as ThinkingItem;
      it.kind = "thinking";
      it.content = block.content ?? "";
      it.streaming = running ? true : undefined;
      it.finalMs = undefined;
      cache.set(sig, it);
      return it;
    }
    if (row.role === "tool") {
      // a result row: merge onto its call item (matched by call id,
      // chronologically adjacent - the panel composes persisted tail +
      // open items) or onto the verdict card it settles
      // the result resolves against its call id, whether the call came
      // from a plain tool line or a verdict card (both sit in the cache
      // under their call id - one truth per call on the canvas)
      const target = (block.tool_call_id ? cache.get(`tc:${block.tool_call_id}`) : undefined) ??
        (block.tool_call_id ? cache.get(`verdict:${block.tool_call_id}`) : undefined);
      if (target?.kind === "tool") {
        target.result = block.content ?? "";
        target.status = block.is_error ? "error" : "done";
        target.interrupted = undefined;
        // the call settled: the running meter's offset is spent
        if (!blockRunning) target.elapsedMs = undefined;
        target.finalMs = block.elapsed_ms;
        return target;
      }
      if (target?.kind === "verdict") {
        const settled = verdictFromResultText(block.content ?? "");
        target.status = "settled";
        target.verdictKind = settled.verdictKind || "expired";
        target.picked = settled.picked;
        target.answerText = settled.answerText;
        target.issuedAt = undefined;
        return target;
      }
      // orphan result (the call row left the newest-40 window): render
      // the bare line rather than erase the tool's work
      const it = (cache.get(`result:${index}:${sig}`) ?? reactive({ kind: "tool", name: "tool", status: "done" })) as ToolItem;
      it.kind = "tool";
      it.name = it.name || "tool";
      it.status = block.is_error ? "error" : "done";
      it.result = block.content ?? "";
      it.interrupted = undefined;
      it.finalMs = block.elapsed_ms;
      cache.set(`result:${index}:${sig}`, it);
      return it;
    }
    // role assistant:
    if (block.type === "tool_call") {
      const callId = block.tool_call?.id;
      // verdict calls render as cards (the card IS the surface, no
      // plain tool line - the matching result row settles it)
      if (block.tool_call?.name === "verdict") {
        const vsig = callId ? `verdict:${callId}` : sig;
        const cached = cache.get(vsig) as VerdictItem | undefined;
        if (cached) {
          // the card is issued; only the stale bit moves. The run is
          // the truth: a pending card under a LIVE panel run is
          // actively awaited (the compose dedupe can hand the card the
          // persisted, closed copy of its own call row) - staleness is
          // the panel's running flag, not the row's.
          cached.stale = !panelRunning;
          return cached;
        }
        const card = reactive(verdictCardFromToolArgs(block.tool_call?.arguments));
        card.callId = callId;
        card.status = "pending";
        card.stale = !panelRunning;
        if (callId && (blockRunning || running || panelRunning)) {
          card.issuedAt = issuedAt.get(callId) ?? Date.now();
          issuedAt.set(callId, card.issuedAt);
        }
        cache.set(vsig, card);
        return card;
      }
      const csig = callId ? `tc:${callId}` : `tc:${index}`;
      const it = (cache.get(csig) ?? reactive({ kind: "tool", name: "tool" })) as ToolItem;
      it.kind = "tool";
      it.name = block.tool_call?.name || "tool";
      it.output =
        block.tool_call?.arguments != null ?
          JSON.stringify(block.tool_call.arguments, null, 2)
        : undefined;
      it.status = blockRunning ? "running" : "done";
      it.interrupted = undefined;
      // elapsed already consumed by a STILL-RUNNING restored call (the
      // persisted history's live-run merge): the meter counts on
      if (blockRunning) it.elapsedMs = block.elapsed_ms ?? 0;
      cache.set(csig, it);
      return it;
    }
    // prose (text block): one assistant bubble per response segment -
    // the backend composes the transcript's segment boundaries, the
    // renderer stays flat. Streaming is ROW-level truth: the open
    // text/thinking rows carry status "streaming" (their blocks carry
    // no running bit - the meter is a tool-row thing).
    const it = (cache.get(sig) ?? reactive({ kind: "message", role: "assistant", content: "" })) as MessageItem;
    it.kind = "message";
    it.role = "assistant";
    it.content = block.content ?? "";
    it.streaming = running ? true : undefined;
    it.time = undefined;
    it.finalMs = undefined;
    it.id = row.id;
    cache.set(sig, it);
    return it;
  }

  function apply(payload: ChatPayloadWire): TranscriptItem[] {
    const out: TranscriptItem[] = [];
    const rows = payload.rows ?? [];
    if ((payload.session ?? "") !== session) {
      // another conversation took the panel over: identities are
      // worthless across sessions - the cache resets
      session = payload.session ?? "";
      cache.clear();
      issuedAt.clear();
    }
    for (let i = 0; i < rows.length; i++) {
      const it = item(rows[i]!, i, !!payload.running);
      if (it) out.push(it);
    }
    return out;
  }

  return { apply };
}

// hasUserExchange is the pure gate behind the composer's retry face:
// the transcript carries at least one user message.
export function hasUserExchange(items: TranscriptItem[]): boolean {
  return lastUserText(items) !== null;
}