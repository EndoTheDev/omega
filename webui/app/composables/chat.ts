// omega webui: the chat host - the composer transport side of chat.

import { computed, reactive, ref, type ComputedRef, type InjectionKey, type Ref } from "vue";
import {
  attachSession,
  chatPanelContent,
  handlePayload,
  lastUserText,
  normalizeChatPayload,
  persistedTelemetry,
  postCanvasAction,
  postVariantSelect,
  isVerdictTool,
  renderTranscriptItems,
  createRestoreTranscript,
  transcriptAddress,
  applyTurnTotals,
  streamChat,
  verdictCardFromToolArgs,
  verdictFromResultText,
  withBusy,
  attachImageUrl,
  uploadAttachment,
  deleteSession,
  type BusyMap,
  type CanvasPanel,
  type ChatPayloadWire,
  type ChatRowWire,
  type ChatBlockWire,
  type OmegaAttachment,
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
  runActive,
  toolCallAnchorMs,
  transcriptHasPending,
  type OmegaMessage,
  type RestoreTranscript,
} from "./omega.ts";
import { SHELL_COMMAND_NAMES, dispatchCommand } from "./commands.ts";
import { isLive, isStreaming } from "./toolStatus.ts";

// The chat actions are module-qualified on the wire.
const CHAT_MODULE = "http_channel";

/** ChatViewState is the tab's VIEW record (card t_15572849): the values a
    dock-tab switch destroys. DockPane renders ONE outlet for the ACTIVE tab
    and there is no KeepAlive anywhere in the tree, so every switch unmounts
    the pane and takes the scrollport, the follow latch, the fold pins and
    the activity clock with it - the "unmounts and remounts but the values
    are not stored" family.

    The settled design is THIS RECORD, not `<KeepAlive>` on the outlet:
    keep-alive would keep every closed tab's whole DOM alive (every
    ResizeObserver target, every `<details>`, every elapsed interval) and it
    still cannot survive a reload, a session rebind, or the DnD move that
    deliberately re-keys the instance (app.vue). The record is the smaller
    runtime and the honest one for a 40-row window
    (agent/api/chat_panel.go), and it is keyed by the tab because this state
    IS the tab. */
export interface ChatViewState {
  /** the reader's place in the scrollport (px, this tab's own element) */
  scrollTop: number;
  /** the follow latch: true = new content lands the reader at the tail */
  followPinned: boolean;
  /** manual fold pins, keyed by the row's DURABLE address (toolGroup.ts
      foldAddress: `call:<call id>`, else `row:<store row id>`; a group pin
      keys `group:<head address>`). No slot floor on purpose - a slot is
      stable only within one payload, so a slot pin would land on a
      different call after a remount. */
  folds: Map<string, boolean>;
  /** the activity line's open phase stretch: which run, which phase, and
      the wall clock it started at. Resumed only for the SAME run and the
      SAME phase key, so a remount keeps counting and a stale anchor can
      never be resumed onto a new stretch. */
  activity: { run: number; key: string; startedAt: number } | null;
  /** the loaded-view witness: the session row's `updated_at` as of the last
      restore ("" = never loaded). load() re-uses an already-loaded view
      while the row still carries this stamp and no run is live; anything
      that ran since moves the stamp and the refetch happens. */
  stamp: string;
}

/** emptyChatViewState mints one tab's view record. A function, never a
    shared literal: the fold map and the anchor are per tab. */
export function emptyChatViewState(): ChatViewState {
  return { scrollTop: 0, followPinned: true, folds: new Map(), activity: null, stamp: "" };
}

/** ChatFoldPins is the fold-pin face a chat pane PROVIDES to its rows: the
    reader's manual open/close, stored on the tab's ChatViewState so it
    survives the pane's unmount. A row rendered outside a chat pane (no
    provider) keeps its pin in its own instance, exactly as before. */
export interface ChatFoldPins {
  get(key: string): boolean | undefined;
  set(key: string, open: boolean): void;
}

/** the pane -> row seam for fold pins (provide/inject, so a row three
    component layers down reaches it without prop-drilling the whole map) */
export const CHAT_FOLD_PINS_KEY: InjectionKey<ChatFoldPins> = Symbol("omega-chat-fold-pins");

export interface ChatState {
  key: string;
  sessionId: string | null;
  restored: TranscriptItem[];
  // the composer draft lives per state: switching tabs (unmount) keeps
  // half-written input, exactly like a VS Code editor keeps its buffer
  draft: string;
  attachments: OmegaImage[];
  files: OmegaAttachment[];
  statusLine: string;
  tokens: number;
  tokensPerSecond: number;
  liveDeltasPerSecond: number;
  lastDeltasPerSecond: number;
  lastChunksPerSecond: number;
  attached?: boolean;
  orphan?: boolean;
  // this state already carries the session's full content (a finished
  // live run, or a successful restore) - the unsaved->created flow sets
  // it so the post-create watcher never clobbers the view
  loaded?: boolean;
  variants: VariantBlock[];
  // the composer's FORK anchor. Validity is the view's concern (the anchor must resolve
  // against the rendered transcript); replay semantics (a user-turn target reruns the
  // turn immediately) ride the same state.
  forkFrom: string | null;
  // the run that just ended (or is ending) on this state, and WHY it ended - the
  // agent_end frame's finish_reason (async agent loop phase 3): "yielded" names a run
  // whose last call went out-of-band, and the pending watcher arms on exactly.
  lastFinishReason: string;
  // ephemeral chat: this tab's session is a NO-PERSIST `eph-` session. Close deletes
  // the server record (the contract: close = gone). Retry/fork/ variants never exist
  // here: the store tree they replay is absent.
  ephemeral?: boolean;
  pendingSkill: { name: string; content: string } | null;
  // the draft workspace intent. A session-bound tab reads the row instead (this field
  // is inert); an ephemeral draft never applies it.: a fresh unbound state INITIALIZES
  // from the loaded server.default_workspace (the deps.defaultWorkspace read of the
  // same /settings payload SettingsTab renders) - no default, no seed; an explicit
  // clear in the chip wins over the seed.
  workspaceIntent: string;
  // the tab's VIEW record (card t_15572849): scroll place + follow
  // latch, fold pins, the activity phase anchor, the loaded-view
  // witness. A dock-tab switch unmounts the pane, so the view state
  // cannot live in the instance - it lives here, keyed by the tab.
  view: ChatViewState;
  // monotone per-state counter: every transport interaction this state
  // starts (a send, an attach, a wake ride) bumps it, so the activity
  // line's phase anchor can name the run it belongs to. The phase key
  // alone repeats across runs ("thinking"), which is exactly how a
  // stale anchor would resume onto the wrong stretch.
  runSeq: number;
}

/**
 * EPHEMERAL_DRAFT_REF is the dock tab's sessionRef marker for a composer-first.
 * Never a real session id - the registry namespace prefix is `eph-`.
 */
export const EPHEMERAL_DRAFT_REF = "ephemeral";

export const DEFAULT_CHAT_STATE: ChatState = {
  key: "",
  sessionId: null,
  restored: [],
  draft: "",
  attachments: [],
  files: [],
  statusLine: "",
  tokens: 0,
  tokensPerSecond: 0,
  liveDeltasPerSecond: 0,
  lastDeltasPerSecond: 0,
  lastChunksPerSecond: 0,
  variants: [],
  forkFrom: null,
  lastFinishReason: "",
  pendingSkill: null,
  workspaceIntent: "",
  view: emptyChatViewState(),
  runSeq: 0,
};

/**
 * a "/" command send's context: marks the dispatch as a SEND - a resolving skill arms
 * AND posts the token as a message (the reference pill survives the transcript);
 * without it the dispatch keeps the law (arm, never a transcript row).
 */
export interface CommandSend {
  model?: string;
  thinking?: number;
  fork?: string;
}

// the app-level callbacks the host needs.
export interface ChatHostDeps {
  setTabSessionRef(key: string, sessionId: string | null): void;
  refreshSessions(): void | Promise<void>;
  sessionRow(sessionId: string): Session | undefined;
  markRead(sessionId: string): void;
  /**
   * whether THIS state key is the app's focused chat tab.
   */
  isFocused(key: string): boolean;
  canvasDelta?(panel: CanvasPanel): void;
  chatPanel?(): CanvasPanel | null;
  canvasRefresh?(): void | Promise<void>;
  /**
   * the webui shell command handler: /theme /fork /retry /variant /copy /new /sessions
   * /panels /extensions /help act locally in the app - never an agent round-trip.
   */
  runCommand?(stateKey: string, text: string): boolean | Promise<boolean>;
  /** the loaded server.default_workspace. */
  defaultWorkspace?(): string;
}

export interface ChatHost {
  states: Map<string, ChatState>;
  busyKeys: Ref<BusyMap>;
  busyAll: ComputedRef<BusyMap>;
  anyBusyNow: ComputedRef<boolean>;
  ensureState(key: string, sessionId?: string | null, ephemeral?: boolean): ChatState;
  chatPanel(): CanvasPanel | null;
  /** mount-time restore of one tab's view: persisted history rebuild +
      telemetry, reading the detail's live-run flag to attach. Refetch
      is skipped while this state itself is attached/busy (a refetch
      would clobber the live view with a mid-flight snapshot), and - card
      t_15572849 - while the state already holds this session's content
      and no run has moved the session row since it was restored (the
      dock-tab switch's remount is exactly that case). force = the truth
      moved for a reason the row stamp cannot see yet. */
  load(key: string, sessionId: string, force?: boolean): Promise<void>;
  /** subscribe to the session's live run for THIS tab's state (the
      duplicate-tab path: the other tab already streams locally) */
  attach(key: string): Promise<void>;
  /**
   * the ONE send path (composer submit).
   * Returns false when the per-session gate refused (double send, never double run).
   * The fork anchor is the caller's validity (it resolves against the rendered
   * transcript).
   */
  submit(
    key: string,
    text: string,
    model?: string,
    thinking?: number,
    fork?: string,
  ): Promise<boolean>;
  attachFiles(key: string, images: OmegaImage[]): void;
  /**
   * upload picked NON-IMAGE files to /attach-file and park the returned references.
   * A failure lands in statusLine, never a throw.
   */
  attachFileRefs(key: string, files: File[]): Promise<void>;
  removeAttachment(key: string, index: number): void;
  removeFileAttachment(key: string, index: number): void;
  /** server-side fetch of an image URL into this tab's attachments */
  attachUrl(key: string, url: string): Promise<void>;
  /**
   * set / clear the composer's FORK anchor.
   */
  setFork(key: string, messageId: string | null): void;
  /** replay a USER-turn anchor immediately (fork of a user message reruns that turn. */
  replayFork(key: string, messageId: string, model?: string, thinking?: number): Promise<void>;
  selectVariant(key: string, messageId: string, index?: number): Promise<boolean>;
  /** stop THIS tab's bound session's live run. */
  cancel(key: string): void;
  /**
   * run a "/" command: shell commands dispatch to the app's local handler; module
   * commands and /<skill-name> dispatch to POST /commands/dispatch (the
   * ctx.CommandHandler chain and the skill content arm - never the agent loop).
   */
  command(key: string, text: string, send?: CommandSend): Promise<boolean>;
  retry(key: string): Promise<boolean>;
  composerBusy(key: string): boolean;
  closeState(key: string): void;
  closeSession(sessionId: string): void;
  /** refreshSessions feeds the server-reported active rows in */
  noteServerActive(ids: string[]): void;
  /**
   * one runs-topic edge: a session's live-run truth moved.
   */
  noteRunState(session: string, active: boolean): void;
  /** one chat-topic frame for the tabs NOT attached to that run's stream */
  noteSocketChat(session: string, event: string, payload: unknown): void;
  /** the socket came back: refetch every open pane through the GET routes */
  reconnectCatchUp(): Promise<void>;
  /**
   * the boot backfill: seed the JUST-LOADED server.default_workspace onto.
   * Boot-time, idempotent.
   */
  seedDefaultWorkspace(path: string): void;
  sessionRow(sessionId: string): Session | undefined;
}

export const CHAT_HOST_KEY: InjectionKey<ChatHost> = Symbol("omega-chat-host");

/** the module->shell chat-open event. */
export const CHAT_SESSION_OPEN_EVENT = "omega:open-chat-session";

/** the shell->module FOCUSED-SESSION event. */
export const FOCUSED_SESSION_EVENT = "omega:focused-session";

/** the module->shell PULL for that same id. */
export const FOCUSED_SESSION_QUERY_EVENT = "omega:query-focused-session";

// the ONE composer button's face --- stop and send stay in ONE morphing affordance.

export type ComposerFace = "stop" | "send";

export function composerFace(
  busy: boolean,
): ComposerFace {
  if (busy) return "stop";
  return "send";
}

// the activity line --- Phase-aware narration for the line above the composer.

export interface ActivityPhase {
  /**
   * stable identity - the component restarts its elapsed meter ONLY when this changes
   * (content deltas recompute but never move it) / /
   */
  key: string;
  label: string;
  /**
   * elapsed ALREADY CONSUMED at observation: a restored still-running tool counts on
   * from the server's elapsed-so-far (live-run merge), the same initialMs contract the
   * ToolCallLine meter runs on / /
   */
  initialMs?: number;
}

// findMessageItem locates a rendered MESSAGE item by its store row id.
// Canvas-mapped items carry ids for stored rows, live rows carry none and are never
// found here.
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
  let tool: ToolItem | null = null;
  let streaming: ThinkingItem | MessageItem | null = null;
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i];
    // isStreaming, NOT isLive - a named decision (composables/toolStatus.ts).
    if (
      tool === null &&
      item?.kind === "tool" &&
      isStreaming(item.status)
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
  return { key: "thinking", label: "thinking" };
}

/**
 * the title goroutine writes the session label seconds AFTER the first exchange ends.
 */
const TITLE_REFRESH_MS = 10000;

export function createChatHost(deps: ChatHostDeps): ChatHost {
  // reactive(Map) wraps values on READ.
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

  // the loaded server.default_workspace.
  const defaultWorkspace = () => deps.defaultWorkspace?.() ?? "";

  function seedDefaultWorkspace(path: string) {
    if (!path) return;
    for (const st of states.values()) {
      if (!st.sessionId && !st.ephemeral && st.workspaceIntent === "")
        st.workspaceIntent = path;
    }
  }

  function ensureState(key: string, sessionId: string | null = null, ephemeral?: boolean): ChatState {
    let st = states.get(key);
    if (!st) {
      const fresh: ChatState = {
        key,
        sessionId,
        restored: [],
        draft: "",
        attachments: [],
        files: [],
        statusLine: "",
        tokens: 0,
        tokensPerSecond: 0,
        liveDeltasPerSecond: 0,
        lastDeltasPerSecond: 0,
        lastChunksPerSecond: 0,
        variants: [],
        forkFrom: null,
        ephemeral: !!ephemeral,
        lastFinishReason: "",
        // a new session is born with the configured default workspace - the chip +
        // intent carry it exactly as if the user had picked it (plain sticky).
        workspaceIntent: !sessionId && !ephemeral ? defaultWorkspace() : "",
        pendingSkill: null,
        view: emptyChatViewState(),
        runSeq: 0,
      };
      states.set(key, fresh);
      st = states.get(key) as ChatState;
      return st;
    }
    // the tab binds the session. The ephemeral bit NEVER flips: the server-minted
    // `eph-` id adopting onto an unsaved tab keeps the no-persist contract attached to
    // the state.
    if (sessionId && st.sessionId !== sessionId) st.sessionId = sessionId;
    if (ephemeral !== undefined) st.ephemeral = ephemeral;
    return st;
  }

  function chatPanel(): CanvasPanel | null {
    return deps.chatPanel?.() ?? null;
  }

  function noteServerActive(ids: string[]) {
    // server-reported live runs REPLACE (not union into) the snapshot.
    serverBusy.value = new Set(ids);
  }

  // the push channel's chat-host surface ----------- noteRunState is the runs.
  function noteRunState(session: string, active: boolean) {
    if (!session) return;
    serverBusy.value = withBusy(serverBusy.value, session, active);
    if (active) wakePending(session);
  }

  // noteSocketChat feeds one chat frame (the run's SSE event verbatim) into the tabs.
  function noteSocketChat(session: string, event: string, payload: unknown) {
    if (!session) return;
    for (const st of states.values()) {
      if (st.sessionId !== session) continue;
      if (st.attached || busyKeys.value.has(st.key)) continue;
      if (event === "session") continue; // the auto-create id rides the sender's own stream
      const handlers: StreamHandlers = {
        onUsage: (u) => {
          st.tokens = u.promptTokens + u.completionTokens;
        },
        onViewModelDelta: (panel) => deps.canvasDelta?.(panel),
        onRunEnd: (finishReason) => {
          st.lastFinishReason = finishReason;
          if (finishReason === "yielded") watchPending(st);
        },
      };
      try {
        handlePayload(payload, handlers, {});
      } catch {
        // an error payload on a background tab: the tab's own load/attach
        // paints the transcript - a socket hint never throws into the loop
      }
    }
  }

  // reconnectCatchUp is the socket's catch-up half: nothing replayed, so
  // every open pane refetches through the ordinary GET routes (events are
  // hints, state is truth). The restore also re-arms a pending waiter.
  // Every call is forced: a socket gap is exactly the window in which the
  // local view may have missed a run whose row stamp never reached us.
  async function reconnectCatchUp(): Promise<void> {
    const loads: Promise<void>[] = [];
    for (const [key, st] of states) {
      if (!st.sessionId || busyKeys.value.has(key) || st.attached) continue;
      loads.push(load(key, st.sessionId, true));
    }
    await Promise.all(loads);
  }

  // makeTransportHandlers builds the SSE handler set one stream runs.
  function makeTransportHandlers(st: ChatState): StreamHandlers {
    const sentAt = Date.now();
    st.tokens = 0;
    st.tokensPerSecond = 0;
    st.liveDeltasPerSecond = 0;
    st.lastDeltasPerSecond = 0;
    st.lastChunksPerSecond = 0;
    // each run states its own ending (async agent loop phase 3): the yielded flag from
    // a PREVIOUS run must not arm the pending watcher off this one's streams
    st.lastFinishReason = "";
    // a fresh transport interaction is a fresh RUN (card t_15572849):
    // the activity line's phase anchor records this number, so a pane
    // that mounts into the same phase key ("thinking") can tell "the
    // same stretch, keep counting" from "an earlier run's clock"
    st.runSeq += 1;
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
      onRunEnd: (finishReason) => {
        st.lastFinishReason = finishReason;
      },
    };
  }

  // refreshVariants pulls the session's branch block again. False when unknown
  // (ephemeral, live run, fetch fault) - the caller's own yielded flag decides arming
  // then.
  async function refreshVariants(st: ChatState): Promise<boolean> {
    const sessionId = st.sessionId;
    if (!sessionId || st.ephemeral) return false;
    try {
      const detail = await fetchTranscript(sessionId);
      if (detail.running) return false;
      st.variants = detail.variants ?? [];
      st.forkFrom = null; // stale anchors retire: resolved against the view
      return transcriptHasPending(renderTranscriptItems(detail.items));
    } catch {
      return false;
    }
  }

  // the pending watcher (async agent loop phase 3, rulings 6/7) --- A yielded run. The
  // origin /chat stream is over, so without a subscriber the wake's view_model_delta
  // frames land on nobody and the pending chip never fills. One arming probe per wait
  // covers the edge that fired in the gap between the view's fetch and the arming; one
  // budget deadline retires a waiter whose wake never comes (a parked home, a page that
  // lost the socket) and the next reconnect catch-up re-arms it.
  const PENDING_WATCH_MS = 30 * 60 * 1000; // cap: the server's run timeout budget

  const pendingWaiters = new Map<string, string>(); // state key -> session id
  const pendingDeadlines = new Map<string, ReturnType<typeof setTimeout>>();

  // pendingInTranscript reads the REST truth once: are any chips still pending? A fetch
  // fault answers true - a transient blip must not disarm the watch.
  async function pendingInTranscript(sessionId: string): Promise<boolean> {
    try {
      const detail = await fetchTranscript(sessionId);
      if (detail.running) return true; // a run holds the slot: the wake is live
      return transcriptHasPending(renderTranscriptItems(detail.items));
    } catch {
      return true;
    }
  }

  // disarmPending retires one waiter (settled, parked, or budget spent).
  function disarmPending(key: string): void {
    pendingWaiters.delete(key);
    const t = pendingDeadlines.get(key);
    if (t) clearTimeout(t);
    pendingDeadlines.delete(key);
  }

  // armDeadline is the budget guard: one truth recheck per window, never a loop.
  function armDeadline(key: string, sid: string): void {
    const t = setTimeout(() => {
      pendingDeadlines.delete(key);
      const st = states.get(key);
      if (!st || st.sessionId !== sid) return;
      void (async () => {
        if (!(await pendingInTranscript(sid))) {
          disarmPending(key);
          await deps.canvasRefresh?.();
          return;
        }
        if (pendingWaiters.has(key)) armDeadline(key, sid);
      })();
    }, PENDING_WATCH_MS);
    pendingDeadlines.set(key, t);
  }

  // ridePending attaches the wake run and rides it to its end.
  async function ridePending(key: string, sid: string): Promise<void> {
    const st = states.get(key);
    if (!st || st.sessionId !== sid) {
      disarmPending(key);
      return;
    }
    if (busyKeys.value.has(key) || st.attached) return; // a live stream owns it; its end re-arms
    disarmPending(key);
    st.attached = true;
    await runStream(st, (handlers) => attachSession(sid, handlers));
  }

  // wakePending: one session's run went live.
  function wakePending(session: string): void {
    if (!session) return;
    for (const [key, sid] of [...pendingWaiters]) {
      if (sid === session) void ridePending(key, sid);
    }
  }

  // watchPending arms ONE waiter per state key.
  function watchPending(st: ChatState) {
    const sid = st.sessionId;
    if (!sid || pendingWaiters.has(st.key)) return;
    pendingWaiters.set(st.key, sid);
    armDeadline(st.key, sid);
    void (async () => {
      if (!(await runActive(sid))) return;
      await ridePending(st.key, sid);
    })();
  }

  // runStream drives one transport interaction.
  async function runStream(st: ChatState, start: (handlers: StreamHandlers) => Promise<unknown>) {
    busyKeys.value = withBusy(busyKeys.value, st.key, true);
    const handlers = makeTransportHandlers(st);
    try {
      st.statusLine = "";
      await start(handlers);
    } catch (e) {
      if (e instanceof StreamInterruptedError) {
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
      // A stream ending on the FOCUSED tab never unreads itself.
      if (st.sessionId && !st.ephemeral && deps.isFocused(st.key)) deps.markRead(st.sessionId);
      await deps.refreshSessions();
      const row = st.sessionId ? deps.sessionRow(st.sessionId) : undefined;
      if (row) {
        const durable = persistedTelemetry(row);
        st.tokens = durable.tokens;
        st.tokensPerSecond = durable.tps;
        st.lastChunksPerSecond = durable.cps;
      }
      // the title goroutine writes the session label seconds AFTER the run really ends.
      // Ephemeral sessions never grow a row or a label - skip the timer.
      if (!st.ephemeral && (!row || !row.label)) {
        setTimeout(() => {
          deps.refreshSessions();
        }, TITLE_REFRESH_MS);
      }
      const pendingStill = await refreshVariants(st);
      if (!st.orphan && (st.lastFinishReason === "yielded" || pendingStill)) {
        watchPending(st);
      }
      if (st.orphan) {
        states.delete(st.key);
        dropEphemeralServerRecord(st.sessionId, st.key);
      }
    }
  }

  // --- the restore path's identity (card t_a20fbad7) ------------------
  //
  // st.restored used to be rebuilt from scratch by every load(): a
  // remount, a tab switch, a variant select each refetched the
  // transcript and every rendered row became a NEW object, so the keyed
  // v-for destroyed and re-created every row - the fold pins, the
  // elapsed meters and the selection died, and the whole session the
  // canvas panel does not cover (a second tab, any session after a
  // reload) paid it. The cache lives with the STATE that renders it
  // (one per chat state, collected with it) and applies the same
  // address rule the canvas paint uses (omega.ts transcriptAddress), so
  // both paths keep one identity: the row that streamed on the canvas
  // keeps its object after the REST restore adopts it, and a refetch
  // updates rows in place instead of remounting them.
  const restoreCaches = new WeakMap<ChatState, RestoreTranscript>();
  function restoreFor(st: ChatState): RestoreTranscript {
    let cache = restoreCaches.get(st);
    if (!cache) {
      cache = createRestoreTranscript();
      restoreCaches.set(st, cache);
    }
    return cache;
  }

  // viewIsStale answers the ONE question load()'s reuse guard needs: does
  // this state's rendered view still match the session's truth? Two
  // witnesses, both cheap and both owned elsewhere:
  //  - a live run on the session (the runHub truth, through busyAll) makes
  //    any local view a mid-flight picture;
  //  - the session row's updated_at moved since this state restored it.
  //    Reading the row is right: every writer of conversation content
  //    (a run's persist, a variant select, a fork, a background cron run
  //    the sidebar refreshed) moves that stamp, while the list-row
  //    concerns (pinned / read / archived) deliberately do not.
  // An UNKNOWN row is not a "no change" - the guard refetches rather than
  // guess (a session the list has not loaded yet still renders correctly).
  function viewIsStale(st: ChatState, sessionId: string): boolean {
    if (busyAll.value.has(sessionId)) return true;
    const row = deps.sessionRow(sessionId);
    if (!row) return true;
    return (row.updated_at ?? "") !== st.view.stamp;
  }

  async function load(key: string, sessionId: string, force = false): Promise<void> {
    const st = ensureState(key, sessionId);
    // a stream live on THIS state owns its feed.
    if (busyKeys.value.has(key) || st.attached) return;
    // the loaded-view reuse (card t_15572849): a dock-tab switch unmounts
    // the pane and the mount calls load() again. The state already carries
    // this session's content - refetching it costs a REST round trip and
    // re-renders the view for nothing. The refusal is EVIDENCE-BASED, never
    // sticky: the session row must still carry the updated_at this state
    // restored, and no run may be live on it. A run since then (this tab,
    // another tab, a detached run seen through the row) is exactly what
    // makes the local view stale - then the refetch happens as before.
    // force = the callers that KNOW the truth moved (the socket catch-up,
    // a variant select); they must never be skipped.
    if (!force && st.loaded && !viewIsStale(st, sessionId)) return;
    try {
      const detail = await fetchTranscript(sessionId);
      const durable = persistedTelemetry(detail.session);
      st.tokens = durable.tokens;
      st.tokensPerSecond = durable.tps;
      st.lastChunksPerSecond = durable.cps;
      // restore IS rendering for the sessions the canvas panel does not
      // cover: wire-order transcript items rendered onto the same
      // component contract the canvas rows paint through. Through the
      // state's cache, so a refetch re-uses the object of every row it
      // already rendered (unchanged rows keep their component instance).
      st.restored = restoreFor(st).apply(detail.items, sessionId);
      // the branch points ride the restore (t_a62eeb43): the switcher
      // and the fork affordance read this block. Absent key = plain
      // session, an empty list keeps the shape total.
      st.variants = detail.variants ?? [];
      st.loaded = true;
      // the reuse witness: this restore now stands for the session AT this
      // row stamp. A later mount compares against it (viewIsStale).
      st.view.stamp = detail.session?.updated_at ?? "";
      // Reattach: a session mid-generation reports running=true on its
      // transcript - this tab auto-subscribes to the live events stream
      // and feeds the canvas store from it (multi-subscriber is fine).
      if (detail.running && !busyKeys.value.has(key)) {
        st.attached = true;
        await runStream(st, (handlers) => attachSession(sessionId, handlers));
      }
      if (!detail.running && transcriptHasPending(st.restored)) {
        watchPending(st);
      }
    } catch {
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
    if (!text && !st.attachments.length && !st.files.length) return false;
    // per-session gate: the run slot is per session - a live run on the
    // SAME session anywhere (either of this page's tabs, the server's
    // active set) blocks a second send locally; the runHub 409 is the
    // backstop for the window the client set cannot see.
    if (st.sessionId && busyAll.value.has(st.sessionId)) return false;
    if (!st.sessionId && busyKeys.value.has(key)) return false;
    // The create-then-send dance is GONE.
    const images = st.attachments.slice();
    st.attachments = [];
    // the pending file references ride the SAME user turn.
    const files = st.files.slice();
    st.files = [];
    // the armed skill rides THIS send as a system preamble; one send consumes it
    const messages: OmegaMessage[] = [
      {
        role: "user",
        content: text,
        images: images.length ? images : undefined,
        attachments: files.length ? files : undefined,
      },
    ];
    if (st.pendingSkill?.content) {
      messages.unshift({ role: "system", content: st.pendingSkill.content });
      st.statusLine = `[skill: ${st.pendingSkill.name}] carried with this send`;
      st.pendingSkill = null;
    }
    // the draft workspace intent rides ONLY the bare create.
    const ws = !st.sessionId && !st.ephemeral ? st.workspaceIntent : "";
    st.workspaceIntent = "";
    if (fork && st.forkFrom === fork) st.forkFrom = null;
    await runStream(st, (handlers) =>
      streamChat(
        messages,
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
        // ephemeral: no-persist contract on the wire - the server mints (or continues)
        // the `eph-` session in memory, denies the memory tool, and never writes the
        // store
        st.ephemeral,
        ws,
      ),
    );
    return true;
  }

  // retry dispatches the chat panel's retry action through the pipe. The composer's
  // model/thinking chips do not ride the action (the pipe action carries the session
  // alone) - the replay takes the session's own run configuration.
  async function retry(key: string): Promise<boolean> {
    const st = ensureState(key);
    if (composerBusy(key)) return false;
    // a retry has to land in an EXISTING session.
    const sessionId = st.sessionId;
    if (!sessionId || st.ephemeral) return false;
    try {
      await postCanvasAction(CHAT_MODULE, "retry", { session: sessionId });
    } catch (e) {
      st.statusLine = `retry failed: ${e instanceof Error ? e.message : String(e)}`;
      return false;
    }
    st.statusLine = "";
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

  // attachFileRefs uploads each picked non-image file to /attach-file and parks.
  async function attachFileRefs(key: string, files: File[]): Promise<void> {
    const st = ensureState(key);
    for (const file of files) {
      try {
        st.files.push(await uploadAttachment(file, st.sessionId));
      } catch (e) {
        st.statusLine = `attach failed: ${e instanceof Error ? e.message : String(e)}`;
      }
    }
  }

  function removeFileAttachment(key: string, index: number): void {
    const st = ensureState(key);
    st.files = st.files.filter((_, i) => i !== index);
  }
  // command runs a "/" draft. Shell commands go to the app's local handler
  // (deps.runCommand - theme, panes, fork, retry, copy: never an agent round-trip). A
  // failure lands in statusLine, never as a swallowed throw.
  async function command(
    key: string,
    text: string,
    send?: CommandSend,
  ): Promise<boolean> {
    const st = ensureState(key);
    const fields = text.split(/\s+/);
    const name = fields[0] ?? "";
    const args = fields.slice(1).join(" ").trim();
    if (SHELL_COMMAND_NAMES.has(name)) {
      if (!deps.runCommand) {
        st.statusLine = `${name}: shell surface mounted without a handler`;
        return true;
      }
      try {
        const handled = await deps.runCommand(key, text);
        if (!handled) st.statusLine = `${name}: nothing to act on here`;
      } catch (e) {
        st.statusLine = `${name} failed: ${e instanceof Error ? e.message : String(e)}`;
      }
      return true;
    }
    try {
      const res = await dispatchCommand(name, args, st.sessionId || "");
      if (res.kind === "skill") {
        st.pendingSkill = { name: res.name || name.slice(1), content: res.content || "" };
        if (send) {
          await submit(key, text, send.model, send.thinking, send.fork);
          return true;
        }
        st.statusLine = `[skill: ${st.pendingSkill!.name}] armed - the next send carries it`;
      } else {
        // the first line that carries TEXT: several session commands (/tree, /sessions,
        // /agents) open their table with a blank line, and a blank status line reads
        // exactly like nothing happened
        const first =
          (res.text || `${name}: done`)
            .split("\n")
            .map((l) => l.trim())
            .find((l) => l !== "") ?? `${name}: done`;
        st.statusLine = first.length > 160 ? first.slice(0, 157) + "..." : first;
      }
    } catch (e) {
      st.statusLine = `${name}: ${e instanceof Error ? e.message : String(e)}`;
    }
    return true;
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

  // replayFork runs a USER-turn fork anchor immediately.
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

  // selectVariant flips a branch point's active variant server-side.
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
    await Promise.all([deps.canvasRefresh?.() ?? Promise.resolve(), load(key, st.sessionId, true)]);
    return true;
  }

  function cancel(key: string) {
    const st = states.get(key);
    if (!st?.sessionId) return;
    postCanvasAction(CHAT_MODULE, "stop", { session: st.sessionId }).catch((e) => {
      st.statusLine = `stop failed: ${e instanceof Error ? e.message : String(e)}`;
    });
  }

  // composerBusy is the PER-SESSION gate.
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

  // dropEphemeralServerRecord enforces the close = gone contract. A miss (record
  // already gone, another window still bound) is a benign race / no-op, never an error
  // surfaces to the user.
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
    // a dropped state must not keep a wake waiter.
    disarmPending(key);
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
    command,
    attachFiles,
    attachFileRefs,
    removeAttachment,
    removeFileAttachment,
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
    noteRunState,
    noteSocketChat,
    reconnectCatchUp,
    seedDefaultWorkspace,
    sessionRow: deps.sessionRow,
  };
}

// --- the canvas transcript (chat-last 2a) --------------------------------
//
// createCanvasTranscript is the pane's render source for the chat
// panel: it maps the payload's rows onto the components' item shapes,
// KEEPING ITEM IDENTITY across deltas (a 250ms repaint must not remount
// every bubble - the tool elapsed meters, the verdict countdowns and
// the keyed v-for all read identity). Cached per row ADDRESS (the store
// row id when persisted, else an explicit id-less slot), mutated in
// place, reused in the rebuilt array.
//
// IDENTITY LAW (card t_494639c8). A prose row's address is its STORE
// ROW ID, never its position. The rule itself lives in ONE place -
// omega.ts transcriptAddress - because the REST restore path
// (createRestoreTranscript) paints the same rows from the same wire and
// must resolve the same address (card t_a20fbad7). The payload is a
// sliding window - the
// newest 40 rows (agent/api/chat_panel.go) - so one append past the
// window shifts every surviving index by one. A positional key missed
// on every surviving row (each one re-minted, each one a fresh key to
// the keyed v-for: destroy + re-create) and, worse, let the incoming
// tail row inherit an EXISTING row's cached object - a row wearing
// another row's meter anchor, fold pin and streaming bit. The id is the
// row's own address: it rides the slide, so a surviving row keeps its
// object and its component instance, and a row that was never on the
// canvas can never pick up another row's.
//
// THE ID-LESS CASE, stated: a stream's in-flight rows are not persisted
// yet, so the wire carries no id for them (core.ChatRow.ID - "a live
// stream's in-flight rows carry none"). Those rows get an explicit SLOT
// address built from what the wire does promise: role, block type and
// the row's ordinal among the payload's id-less rows of that shape -
// `live:assistant:text:0`. The ordinal counts ID-LESS rows only, so a
// head slide (which drops a persisted row) never moves it; the open
// tail grows at the END, so it does not move on an append either. When
// the row persists and arrives with an id, its slot item is ADOPTED
// onto the id address - the live -> persisted transition must not swap
// the object either (omega.test.ts pins the user turn doing this). A
// slot dies with the payload that stopped carrying its row
// (sweepSlots), so an id-less row that left the canvas leaves no object
// behind for a later row to inherit.
//
// REACTIVE ITEMS (t_078a2c35). Cache items are reactive proxies: an
// in-place write (a delta mutating content/streaming/elapsedMs)
// invalidates exactly the render effects that read those fields
// (ChatItem's :item-id, ChatMessage's variantLabel). Under the id law a
// sibling variant is a DIFFERENT store row - its own address, its own
// item, no in-place rewrite across variants (the positional fold that
// wrote one variant's id onto the other's item is what this card
// retired).

// VERDICT COUNTDOWN ANCHOR. A card's deadline is a
// fact about the CALL, not about the pane that happens to render it.
// The anchor used to be a Map inside createCanvasTranscript - one per
// ChatTab instance - so every dock-tab switch re-anchored the card at
// Date.now (DockPane renders only the active tab, no KeepAlive: the
// pane remounts, and a card four minutes into a five-minute deadline
// read 5:00 again - the remaining time misreported in the one surface
// where it is the point).
//
// Two anchors, most honest first:
//   1. the SERVER's stamp. An UNANSWERED tool_call block carries
//      `started_at` (RFC 3339, core.ChatBlock.StartedAt - the call's own
//      open wall clock, recorded at the tool_call event and dropped when
//      the result lands, agent/api/server.go). A verdict call blocks
//      until its card settles, so a pending card carries one, and it is
//      right on the FIRST sight too: a tab opened four minutes in reads
//      1:00, not 5:00. A fixed instant, so re-deriving it at every paint
//      never drifts.
//   2. first sight at MODULE scope, keyed by the call id - for a payload
//      served without a clock. The call id is the anchor's address: it
//      survives the mount, the pane, a session switch and the sliding
//      window, so a remount reads the anchor the first pane wrote.
//      Bounded (oldest insertion leaves when full): a card that old is
//      settled or gone.
const verdictAnchors = new Map<string, number>();
const VERDICT_ANCHOR_MAX = 256;

// verdictAnchorMs resolves one card's countdown anchor.
function verdictAnchorMs(callId: string | undefined, startedAt?: string): number | undefined {
  if (startedAt) {
    const start = Date.parse(startedAt);
    if (!Number.isNaN(start)) return start;
  }
  if (!callId) return undefined;
  const seen = verdictAnchors.get(callId);
  if (seen !== undefined) return seen;
  const now = Date.now();
  verdictAnchors.set(callId, now);
  if (verdictAnchors.size > VERDICT_ANCHOR_MAX) {
    const oldest = verdictAnchors.keys().next();
    if (!oldest.done) verdictAnchors.delete(oldest.value);
  }
  return now;
}

export interface CanvasTranscript {
  apply(payload: ChatPayloadWire): TranscriptItem[];
}

export function createCanvasTranscript(): CanvasTranscript {
  let session = "";
  // persisted rows: one entry per STORE ROW ID (`row:<id>`) or per stable wire.
  const cache = new Map<string, TranscriptItem>();
  // id-less rows: one entry per SLOT address (`live:<role>:<block type>:<n>`), swept.
  const slots = new Map<string, TranscriptItem>();
  // the verdict countdowns anchor at MODULE scope on the call id, never here.

  interface Paint {
    resultIds: ReadonlySet<string>;
    ordinals: Map<string, number>;
    usedSlots: Set<string>;
    lastIdOfShape: ReadonlyMap<string, number>;
  }

  // liveAddress is an id-less row's slot address - resolved by the SAME
  // rule the REST restore path uses (omega.ts transcriptAddress), never a
  // local spelling. consume=true hands out the next ordinal for the shape
  // (a row being painted); consume=false PEEKS at the ordinal such a row
  // would get - the adoption path, where a just-persisted row looks for
  // the slot its own live object sits in.
  function liveAddress(row: ChatRowWire, block: ChatBlockWire, ctx: Paint, consume: boolean): string {
    return transcriptAddress(undefined, `${row.role}:${block.type}`, ctx.ordinals, consume);
  }

  // rowAddress: the store id AND the block type when the row is persisted,
  // else its slot. The block type is part of the address because ONE store
  // row lands as SEVERAL wire rows: an assistant turn that reasoned and then
  // answered is one row id with a thinking row and a text row (agent/api
  // expands a stored row's blocks). Keyed by the id alone those two wire
  // rows shared ONE cached item and each paint wrote the other's `kind`
  // over it - a CHANGED reactive write on every paint of an unchanged
  // payload, which invalidated the effects that read it and re-armed the
  // paint that made it: the pane spun, painting itself, until the tab died
  // (card t_a356fcc7). It also silently dropped the reasoning row from the
  // live pane (the same object cannot be both). The type is stable across
  // the window's slide, so identity survives exactly as the id law wants;
  // the ordinal below is still the id-less row's, where the shape already
  // carries it. The spelling itself lives in ONE place - omega.ts
  // transcriptAddress - because the REST restore path resolves the same
  // addresses from the same wire (card t_a20fbad7).
  function rowAddress(row: ChatRowWire, block: ChatBlockWire, ctx: Paint): string {
    return transcriptAddress(row.id, `${row.role}:${block.type}`, ctx.ordinals);
  }

  // lookup returns the item a prose row paints into. A row never receives another row's
  // object: the id path reads only its own key, adoption is limited to the shape's last
  // ID-BEARING row in the payload (the store persists the open tail last, so a row that
  // just gained an id is its shape's newest id-bearing row - an earlier row of that
  // shape is a different row and never takes the object), the adopted entry is always
  // an ID-LESS slot (a persisted item is never parked in `slots`), and sweepSlots drops
  // a slot the moment its row leaves the payload.
  function lookup<T extends TranscriptItem>(
    row: ChatRowWire,
    block: ChatBlockWire,
    index: number,
    ctx: Paint,
    make: () => T,
  ): T {
    const address = rowAddress(row, block, ctx);
    if (!row.id) {
      const live = peek<T>(address, true, ctx);
      if (live) return live;
      const fresh = make();
      put(address, true, fresh);
      return fresh;
    }
    const hit = cache.get(address) as T | undefined;
    if (hit) return hit;
    const shape = `${row.role}:${block.type}`;
    if (ctx.lastIdOfShape.get(shape) === index) {
      const slot = liveAddress(row, block, ctx, false);
      const adopted = slots.get(slot) as T | undefined;
      if (adopted) {
        slots.delete(slot);
        cache.set(address, adopted);
        return adopted;
      }
    }
    const fresh = make();
    cache.set(address, fresh);
    return fresh;
  }

  // peek/put: the tool, verdict and result rows are addressed by the same law.
  // A persisted item sits in the cache; an ID-LESS one sits in `slots`, so the sweep
  // below retires it with the payload that stopped carrying its row - an id-less row
  // must not hand its object to a later row of its shape either. `live` is passed
  // explicitly, never derived from the key, because a call id is a stable wire identity
  // even on an id-less row.
  function peek<T extends TranscriptItem>(key: string, live: boolean, ctx: Paint): T | undefined {
    if (!live) return cache.get(key) as T | undefined;
    ctx.usedSlots.add(key);
    return slots.get(key) as T | undefined;
  }

  function put(key: string, live: boolean, item: TranscriptItem): void {
    if (live) slots.set(key, item);
    else cache.set(key, item);
  }

  function item(row: ChatRowWire, index: number, ctx: Paint): TranscriptItem | null {
    const block = row.blocks?.[0];
    if (!block) return null;
    const running = row.status === "streaming";
    const blockRunning = running && block.running;
    const resultIds = ctx.resultIds;
    if (row.role === "user") {
      const it = lookup(row, block, index, ctx, () => reactive({ kind: "message", role: "user", content: "" }) as MessageItem);
      it.kind = "message";
      it.role = "user";
      it.content = block.content ?? "";
      it.images = block.images;
      it.attachments = block.attachments;
      it.id = row.id;
      // the row's wall clock: the persisted user row's stamp is the turn's START.
      it.time = row.time;
      return it;
    }
    if (row.role === "assistant" && block.type === "thinking") {
      const it = lookup(row, block, index, ctx, () => reactive({ kind: "thinking", content: "" }) as ThinkingItem);
      it.kind = "thinking";
      it.content = block.content ?? "";
      it.id = row.id;
      it.streaming = running ? true : undefined;
      // the duration rides the block both ways: a completed reasoning phase keeps its
      // server-measured span, and a still-streaming one carries the elapsed-so-far its
      // meter counts on from after a remount - never the mount moment
      it.finalMs = block.thinking_ms;
      it.elapsedMs = running ? block.elapsed_ms : undefined;
      return it;
    }
    if (row.role === "tool") {
      const target = (block.tool_call_id ? cache.get(`tc:${block.tool_call_id}`) : undefined) ??
        (block.tool_call_id ? cache.get(`verdict:${block.tool_call_id}`) : undefined);
      if (target?.kind === "tool") {
        target.result = block.content ?? "";
        target.status = block.is_error ? "error" : "done";
        if (block.gate) target.gate = block.gate;
        if (!blockRunning) target.elapsedMs = undefined;
        target.finalMs = block.elapsed_ms;
        target.answerMs = block.answer_ms;
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
      const rkey = `result:${rowAddress(row, block, ctx)}`;
      const live = !row.id;
      const it = peek<ToolItem>(rkey, live, ctx) ?? (reactive({ kind: "tool", name: "tool", status: "done" }) as ToolItem);
      it.kind = "tool";
      it.name = it.name || "tool";
      it.status = block.is_error ? "error" : "done";
      it.result = block.content ?? "";
      if (block.gate) it.gate = block.gate;
      it.finalMs = block.elapsed_ms;
      it.answerMs = block.answer_ms;
      put(rkey, live, it);
      return it;
    }
    if (block.type === "tool_call") {
      const callId = block.tool_call?.id;
      if (isVerdictTool(block.tool_call?.name)) {
        const live = !callId && !row.id;
        const vsig = callId ? `verdict:${callId}` : `verdict:${rowAddress(row, block, ctx)}`;
        const cached = peek<VerdictItem>(vsig, live, ctx);
        if (cached) {
          // the card is issued; only its anchor moves. the anchor is re-derived at
          // every paint (the elapsed-meter law): a payload that carries the server's
          // start stamp only on a later delta moves the countdown onto the true clock,
          // once - a stamp is a fixed instant, so a re-derivation never drifts.
          if (cached.status === "pending") {
            cached.issuedAt = verdictAnchorMs(callId, block.started_at) ?? cached.issuedAt;
          }
          return cached;
        }
        const card = reactive(verdictCardFromToolArgs(block.tool_call?.arguments));
        card.callId = callId;
        card.status = "pending";
        // anchored whether or not a stream is live: a yielded run's card is still
        // pending, and its remaining time is the call's truth, never the panel's
        // running flag
        card.issuedAt = verdictAnchorMs(callId, block.started_at);
        put(vsig, live, card);
        return card;
      }
      // the call row is keyed by its CALL id whenever the wire carries one (the result
      // row merges onto it there); a call with neither a call id nor a store id gets
      // its own slot - a payload index is never an address
      const live = !callId && !row.id;
      const csig = callId ? `tc:${callId}` : `tc:${rowAddress(row, block, ctx)}`;
      const it = peek<ToolItem>(csig, live, ctx) ?? (reactive({ kind: "tool", name: "tool" }) as ToolItem);
      it.kind = "tool";
      it.name = block.tool_call?.name || "tool";
      it.callId = callId;
      // the store id rides the CALL row.
      it.id = row.id;
      it.output =
        block.tool_call?.arguments != null ?
          JSON.stringify(block.tool_call.arguments, null, 2)
        : undefined;
      // async agent loop phase 3: an unanswered call is the PENDING chip, never.
      it.status =
        blockRunning ? "running"
        : callId && !resultIds.has(callId) ? "pending"
        : "done";
      // a LIVE call meters from its open wall clock (card t_6bad9f6f
      // round 3), resolved ONCE PER ITEM (card t_a356fcc7): the anchor is
      // `now - started_at` at FIRST SIGHT, so a rebuild whose fresh
      // identity cache meets a payload older than the call's yield still
      // counts from the call's real start instead of zero. It is NOT
      // rewritten on later paints. A re-derived value differs on EVERY
      // paint of the same payload, and these items are the reactive cached
      // rows - a changed write invalidates the effects that read it,
      // including the computeds that called the paint, so the pane re-armed
      // itself and spun until the tab died. The row's own meter counts on
      // from the first-sight base; a settled result clears the offset (the
      // result branch above) and nothing re-mints one.
      if (isLive(it.status) && it.elapsedMs === undefined) {
        const anchor = toolCallAnchorMs(block.started_at, block.elapsed_ms);
        if (anchor !== undefined) it.elapsedMs = anchor;
      }
      put(csig, live, it);
      return it;
    }
    // prose (text block): one assistant bubble per response segment. The block's own
    // duration rides it: response_ms on a completed segment (the frozen label survives
    // a remount) and elapsed_ms on the open one (the remounted meter counts on from the
    // answer's elapsed-so-far, exactly like a running tool row).
    const it = lookup(row, block, index, ctx, () => reactive({ kind: "message", role: "assistant", content: "" }) as MessageItem);
    it.kind = "message";
    it.role = "assistant";
    it.content = block.content ?? "";
    it.streaming = running ? true : undefined;
    // the row's wall clock: the persisted answer row's stamp is the turn's END.
    it.time = row.time;
    it.finalMs = block.response_ms;
    it.elapsedMs = running ? block.elapsed_ms : undefined;
    it.id = row.id;
    return it;
  }

  function apply(payload: ChatPayloadWire): TranscriptItem[] {
    const out: TranscriptItem[] = [];
    // one push per item object: a merge RESOLVES to an item the call row already pushed
    // - re-pushing it would render the line twice and hand the keyed v-for a duplicate
    // key
    const pushed = new Set<TranscriptItem>();
    const rows = payload.rows ?? [];
    const resultIds = new Set<string>();
    for (const r of rows) {
      if (r?.role !== "tool") continue;
      for (const b of r.blocks ?? []) {
        if (b?.type === "tool_result" && b.tool_call_id) resultIds.add(b.tool_call_id);
      }
    }
    if ((payload.session ?? "") !== session) {
      // another conversation took the panel over. The verdict anchors do NOT: they are
      // keyed by call id at module scope, and a card's remaining time is its call's
      // fact - clearing them here is what made a tab switch (or a pane retarget)
      // restart a countdown mid-deadline
      session = payload.session ?? "";
      cache.clear();
      slots.clear();
    }
    // the shape's LAST ID-BEARING row is the only row that may adopt a live slot.
    // An earlier row of that shape is a different row and must never take the object.
    const lastIdOfShape = new Map<string, number>();
    rows.forEach((row, i) => {
      const b = row.blocks?.[0];
      if (b && row.id) lastIdOfShape.set(`${row.role}:${b.type}`, i);
    });
    const ctx: Paint = {
      resultIds,
      ordinals: new Map<string, number>(),
      usedSlots: new Set<string>(),
      lastIdOfShape,
    };
    for (let i = 0; i < rows.length; i++) {
      const it = item(rows[i]!, i, ctx);
      if (it && !pushed.has(it)) {
        out.push(it);
        pushed.add(it);
      }
    }
    // sweepSlots: a slot lives only while its row is in the payload.
    sweepSlots(ctx);
    // the answer row's turn total: the SAME pass the restore builder runs, over the
    // same two row stamps - the live pane and a reloaded transcript cannot disagree
    applyTurnTotals(out);
    return out;
  }

  function sweepSlots(ctx: Paint): void {
    for (const key of slots.keys()) {
      if (!ctx.usedSlots.has(key)) slots.delete(key);
    }
  }

  return { apply };
}

// the retry gate moved with the affordance.