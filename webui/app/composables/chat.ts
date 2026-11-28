
import { computed, reactive, ref, type ComputedRef, type InjectionKey, type Ref } from "vue";
import { cacheShare, durableTokens, estimateTokens, usageTokens, type SessionTokens } from "./tokenstat.ts";
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
  fetchQueue,
  deleteQueueRow,
  sendSteer,
  type QueuedRow,
  type OmegaMessage,
  type RestoreTranscript,
} from "./omega.ts";
import { SHELL_COMMAND_NAMES, dispatchCommand } from "./commands.ts";
import { isLive, isStreaming } from "./toolStatus.ts";

const CHAT_MODULE = "http_channel";

/** ChatViewState is the tab's VIEW record: the values a dock-tab switch destroys.
    DockPane renders one outlet for the ACTIVE tab (no KeepAlive), so the record is keyed by the tab. */
export interface ChatViewState {
  scrollTop: number;
  /** the follow latch: true = new content lands the reader at the tail */
  followPinned: boolean;
  /** fold pins keyed by the row's DURABLE address (toolGroup.ts foldAddress), never by slot. */
  folds: Map<string, boolean>;
  /** the activity line's open phase. Resumed only for the same run AND phase key. */
  activity: { run: number; key: string; startedAt: number } | null;
  /** the loaded-view witness: the row's updated_at as of the last restore ("" = never loaded). */
  stamp: string;
}

/** mints one tab's view record: a function, never a shared literal (map + anchor are per tab). */
export function emptyChatViewState(): ChatViewState {
  return { scrollTop: 0, followPinned: true, folds: new Map(), activity: null, stamp: "" };
}

/** the fold-pin face a chat pane provides to its rows, stored on the tab's ChatViewState. */
export interface ChatFoldPins {
  get(key: string): boolean | undefined;
  set(key: string, open: boolean): void;
}

export const CHAT_FOLD_PINS_KEY: InjectionKey<ChatFoldPins> = Symbol("omega-chat-fold-pins");

export interface ChatState {
  key: string;
  sessionId: string | null;
  restored: TranscriptItem[];
  draft: string;
  attachments: OmegaImage[];
  files: OmegaAttachment[];
  statusLine: string;
  tokens: number;
  // true while `tokens` carries a run-time estimate instead of reported usage
  tokensEstimated: boolean;
  // the last STORE-reported cumulative total: the base a live run counts onto
  tokensDurable: number;
  // the latest response's prompt-cache share (percent, one decimal); null = the
  // provider reported no cache figure - the bar says "not reported", never 0%
  cachePercent: number | null;
  tokensPerSecond: number;
  liveDeltasPerSecond: number;
  lastDeltasPerSecond: number;
  lastChunksPerSecond: number;
  attached?: boolean;
  orphan?: boolean;
  // set once this state carries the session's full content, so the post-create watcher cannot clobber it
  loaded?: boolean;
  variants: VariantBlock[];
  // the composer's FORK anchor; resolving it against the rendered transcript is the view's job
  forkFrom: string | null;
  // the run that just ended and WHY: "yielded" means its last call went out-of-band
  lastFinishReason: string;
  // a NO-PERSIST `eph-` session: close deletes the server record (close = gone), and
  // retry/fork/variants never exist here - the store tree they replay is absent
  ephemeral?: boolean;
  pendingSkill: { name: string; content: string } | null;
  // the draft workspace intent (inert on a session-bound tab); unbound states seed it from server.default_workspace
  workspaceIntent: string;
  view: ChatViewState;
  // monotone per state, so the activity anchor can name its run (a phase key alone repeats across runs)
  runSeq: number;
  // the visible steer queue: sends parked for the live turn, delivered in order at its
  // next batch boundary. SESSION-held server-side, so a crashed turn does not eat them.
  queued: QueuedRow[];
}

/** the dock tab's sessionRef marker for a composer-first draft. Never a real session id. */
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
  tokensEstimated: false,
  tokensDurable: 0,
  cachePercent: null,
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
  queued: [],
};

/** marks a "/" command dispatch as a SEND: a resolving skill arms AND posts its token as a message. */
export interface CommandSend {
  model?: string;
  thinking?: number;
  fork?: string;
}

export interface ChatHostDeps {
  setTabSessionRef(key: string, sessionId: string | null): void;
  refreshSessions(): void | Promise<void>;
  sessionRow(sessionId: string): Session | undefined;
  markRead(sessionId: string): void;
  isFocused(key: string): boolean;
  canvasDelta?(panel: CanvasPanel): void;
  chatPanel?(): CanvasPanel | null;
  canvasRefresh?(): void | Promise<void>;
  /** the webui shell command handler: /theme /fork /retry /variant /copy /new /panels act locally, never an agent round-trip. */
  runCommand?(stateKey: string, text: string): boolean | Promise<boolean>;
  defaultWorkspace?(): string;
}

export interface ChatHost {
  states: Map<string, ChatState>;
  busyKeys: Ref<BusyMap>;
  busyAll: ComputedRef<BusyMap>;
  anyBusyNow: ComputedRef<boolean>;
  ensureState(key: string, sessionId?: string | null, ephemeral?: boolean): ChatState;
  chatPanel(): CanvasPanel | null;
  /** mount-time restore: history + telemetry, then attach if the detail reports a live run.
      Refetch is skipped while attached/busy or already current; force = the caller KNOWS. */
  load(key: string, sessionId: string, force?: boolean): Promise<void>;
  /** subscribe to the session's live run for THIS tab (the duplicate-tab path) */
  attach(key: string): Promise<void>;
  /** the ONE send path; false when the per-session gate refused (double send, never double run). */
  submit(
    key: string,
    text: string,
    model?: string,
    thinking?: number,
    fork?: string,
  ): Promise<boolean>;
  attachFiles(key: string, images: OmegaImage[]): void;
  /** upload picked NON-IMAGE files to /attach-file and park the references; a failure lands in statusLine. */
  attachFileRefs(key: string, files: File[]): Promise<void>;
  removeAttachment(key: string, index: number): void;
  removeFileAttachment(key: string, index: number): void;
  attachUrl(key: string, url: string): Promise<void>;
  setFork(key: string, messageId: string | null): void;
  replayFork(key: string, messageId: string, model?: string, thinking?: number): Promise<void>;
  selectVariant(key: string, messageId: string, index?: number): Promise<boolean>;
  cancel(key: string): void;
  /** run a "/" command: shell commands go local, module commands and /<skill-name> to POST /commands/dispatch - never the agent loop. */
  command(key: string, text: string, send?: CommandSend): Promise<boolean>;
  retry(key: string): Promise<boolean>;
  composerBusy(key: string): boolean;
  /** drop one queued steer row before the live turn's boundary reaches it */
  deleteQueued(key: string, id: string): Promise<boolean>;
  /** re-read the session's waiting rows (server truth) */
  refreshQueued(key: string): Promise<void>;
  closeState(key: string): void;
  closeSession(sessionId: string): void;
  noteServerActive(ids: string[]): void;
  noteRunState(session: string, active: boolean): void;
  noteSocketChat(session: string, event: string, payload: unknown): void;
  reconnectCatchUp(): Promise<void>;
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


export type ComposerFace = "stop" | "send";

export function composerFace(
  busy: boolean,
): ComposerFace {
  if (busy) return "stop";
  return "send";
}

/** SendHint names what the next send will DO. One line, and the idle empty state stays
    clean: no run, no draft, nothing to explain. */
export function sendHint(args: {
  busy: boolean;
  /** no run, but the session holds unsettled calls: a send wakes the agent now */
  sleeping: boolean;
  hasDraft: boolean;
}): string {
  if (args.busy) return "send steers the running turn";
  if (args.sleeping) return "send wakes the agent now";
  if (args.hasDraft) return "send starts a turn";
  return "";
}

/** mergeQueuedRows appends the rows a send parked, dropping ids the caller already holds
    (a resent/duplicated frame must not double-render a row). */
export function mergeQueuedRows(current: QueuedRow[], incoming: QueuedRow[]): QueuedRow[] {
  const seen = new Set(current.map((r) => r.id));
  const out = current.slice();
  for (const row of incoming) {
    if (!row?.id || seen.has(row.id)) continue;
    seen.add(row.id);
    out.push(row);
  }
  return out;
}

/** dropQueuedRow retires one delivered (or deleted) row. */
export function dropQueuedRow(current: QueuedRow[], id: string): QueuedRow[] {
  if (!id) return current;
  const out = current.filter((r) => r.id !== id);
  return out.length === current.length ? current : out;
}

/** switchWorkspace moves a session's root: the store first, the row second. The row's
    workspace is the FILES tab's only re-root trigger, so a write sent before the PATCH
    answers spends the pane's one read on the OLD store row - and the confirmed value
    that lands after it equals what the row already shows, so the stale root survives a
    reload. `refresh` is the shell's server-read of the session list; it runs on both
    outcomes, so a refused switch restores the row instead of leaving a lie in it. */
export async function switchWorkspace(
  row: { id: string; workspace?: string | null },
  workspace: string,
  deps: {
    patch(id: string, workspace: string): Promise<unknown>;
    refresh(): void | Promise<void>;
  },
): Promise<boolean> {
  if (!workspace || row.workspace === workspace) return false;
  try {
    await deps.patch(row.id, workspace);
    return true;
  } catch {
    return false;
  } finally {
    try {
      await deps.refresh();
    } catch {
      // the row keeps its last server-confirmed value; the next list read retries it
    }
  }
}

export interface ActivityPhase {
  /** stable identity - the component restarts its elapsed meter ONLY when this changes */
  key: string;
  label: string;
  /** elapsed ALREADY CONSUMED at observation: counts on from the server's elapsed-so-far. */
  initialMs?: number;
}

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

// the title goroutine writes the session label seconds AFTER the first exchange ends; ephemeral sessions never grow one.
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

  const defaultWorkspace = () => deps.defaultWorkspace?.() ?? "";

  function seedDefaultWorkspace(path: string) {
    if (!path) return;
    for (const st of states.values()) {
      if (!st.sessionId && !st.ephemeral && st.workspaceIntent === "")
        st.workspaceIntent = path;
    }
  }

  // the ONE writer of the bar's token figure. The live paths (run start, usage,
  // estimate) and the restore paths (session row, run end) all land here, so the
  // two can never drift into different meanings for the same session.
  function paintTokens(st: ChatState, next: SessionTokens) {
    st.tokens = next.total;
    st.tokensEstimated = next.estimated;
  }

  // a store-reported total: cumulative and exact, and the base the next run counts onto.
  function paintDurable(st: ChatState, total: number) {
    paintTokens(st, durableTokens(total));
    st.tokensDurable = Math.max(0, total);
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
        tokensEstimated: false,
        tokensDurable: 0,
        cachePercent: null,
        tokensPerSecond: 0,
        liveDeltasPerSecond: 0,
        lastDeltasPerSecond: 0,
        lastChunksPerSecond: 0,
        variants: [],
        forkFrom: null,
        ephemeral: !!ephemeral,
        lastFinishReason: "",
        // a new session is born with the configured default workspace, exactly as if picked
        workspaceIntent: !sessionId && !ephemeral ? defaultWorkspace() : "",
        pendingSkill: null,
        view: emptyChatViewState(),
        runSeq: 0,
        queued: [],
      };
      states.set(key, fresh);
      st = states.get(key) as ChatState;
      return st;
    }
    // the ephemeral bit NEVER flips: an `eph-` id adopting onto an unsaved tab keeps the no-persist contract
    if (sessionId && st.sessionId !== sessionId) {
      st.sessionId = sessionId;
      // a DIFFERENT session starts at its own total: the previous one is not its base
      paintDurable(st, 0);
      st.cachePercent = null;
    }
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

  function noteRunState(session: string, active: boolean) {
    if (!session) return;
    serverBusy.value = withBusy(serverBusy.value, session, active);
    if (active) wakePending(session);
  }

  function noteSocketChat(session: string, event: string, payload: unknown) {
    if (!session) return;
    for (const st of states.values()) {
      if (st.sessionId !== session) continue;
      if (st.attached || busyKeys.value.has(st.key)) continue;
      if (event === "session") continue; // the auto-create id rides the sender's own stream
      const handlers: StreamHandlers = {
        onUsage: (u) => {
          paintTokens(st, usageTokens(st.tokensDurable, u.promptTokens, u.completionTokens));
          st.cachePercent = cacheShare(u.cachedTokens, u.promptTokens);
        },
        onViewModelDelta: (panel) => deps.canvasDelta?.(panel),
        onRunEnd: (finishReason) => {
          st.lastFinishReason = finishReason;
          if (finishReason === "yielded") watchPending(st);
        },
        onSteerDelivered: (id) => {
          st.queued = dropQueuedRow(st.queued, id);
        },
      };
      try {
        handlePayload(payload, handlers, {});
      } catch {
        // an error payload on a background tab never throws into the loop - its own load paints
      }
    }
  }

  // the socket's catch-up: events are hints, state is truth, so every open pane refetches through
  // the GET routes. Forced - a gap is the window the row stamp cannot witness.
  async function reconnectCatchUp(): Promise<void> {
    const loads: Promise<void>[] = [];
    for (const [key, st] of states) {
      if (!st.sessionId || busyKeys.value.has(key) || st.attached) continue;
      loads.push(load(key, st.sessionId, true));
    }
    await Promise.all(loads);
  }

  function makeTransportHandlers(st: ChatState): StreamHandlers {
    const sentAt = Date.now();
    // a run counts ONTO the session's durable total: the bar never drops to one run
    const base = st.tokensDurable;
    // the panel's visible output at run start, so the estimate counts THIS run only
    const panelAtStart = normalizeChatPayload(chatPanel()?.payload);
    const outputAtStart = panelAtStart ? chatPanelContent(panelAtStart) : 0;
    let usageSeen = false;
    paintTokens(st, durableTokens(base));
    st.tokensPerSecond = 0;
    st.liveDeltasPerSecond = 0;
    st.lastDeltasPerSecond = 0;
    st.lastChunksPerSecond = 0;
    // each run states its own ending: a PREVIOUS run's yielded flag must not arm this one
    st.lastFinishReason = "";
    // a fresh transport interaction is a fresh RUN: the activity anchor records this number
    st.runSeq += 1;
    // delta rate meter: panel-repaint arrival times over the last second
    const deltaTimes: number[] = [];
    const meter = (payload: ChatPayloadWire | null) => {
      const now = Date.now();
      deltaTimes.push(now);
      while (deltaTimes.length && now - (deltaTimes[0] ?? now) > 1000) deltaTimes.shift();
      st.liveDeltasPerSecond = deltaTimes.length;
      // an estimate never outranks reported usage for the same run
      if (payload && !usageSeen)
        paintTokens(st, estimateTokens(base, chatPanelContent(payload) - outputAtStart));
    };
    return {
      onSession: undefined, // the send path overrides (below)
      onUsage: (usage) => {
        usageSeen = true;
        paintTokens(st, usageTokens(base, usage.promptTokens, usage.completionTokens));
        st.cachePercent = cacheShare(usage.cachedTokens, usage.promptTokens);
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
      onSteerDelivered: (id) => {
        // a queued steer reached THIS turn's boundary: the row is spent and the message is
        // ordinary transcript content (a panel repaint carries it)
        st.queued = dropQueuedRow(st.queued, id);
      },
      onQueued: (rows) => {
        // The busy snapshot was stale: this send took the stream path while a run was
        // already live, and the server parked it as a steer row. Show it like any other
        // parked row - the send was accepted, so it must never vanish.
        st.queued = mergeQueuedRows(st.queued, rows);
        st.statusLine = "";
        deps.refreshSessions();
      },
    };
  }

  // false when the branch block is unknown (ephemeral, live run, fault); the caller's yielded flag decides then
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

  // the pending watcher: a yielded run's origin stream is over, so without a subscriber the wake's
  // deltas land on nobody and the chip never fills. One arming probe plus one budget deadline covers it.
  const PENDING_WATCH_MS = 30 * 60 * 1000; // cap: the server's run timeout budget

  const pendingWaiters = new Map<string, string>(); // state key -> session id
  const pendingDeadlines = new Map<string, ReturnType<typeof setTimeout>>();

  // a fetch fault answers true: a transient blip must not disarm the watch.
  async function pendingInTranscript(sessionId: string): Promise<boolean> {
    try {
      const detail = await fetchTranscript(sessionId);
      if (detail.running) return true; // a run holds the slot: the wake is live
      return transcriptHasPending(renderTranscriptItems(detail.items));
    } catch {
      return true;
    }
  }

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

  function wakePending(session: string): void {
    if (!session) return;
    for (const [key, sid] of [...pendingWaiters]) {
      if (sid === session) void ridePending(key, sid);
    }
  }

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
        paintDurable(st, durable.tokens);
        st.tokensPerSecond = durable.tps;
        st.lastChunksPerSecond = durable.cps;
      }
      // the title goroutine writes the label seconds after the run ends; ephemeral sessions never grow one
      if (!st.ephemeral && (!row || !row.label)) {
        setTimeout(() => {
          deps.refreshSessions();
        }, TITLE_REFRESH_MS);
      }
      const pendingStill = await refreshVariants(st);
      if (!st.orphan && (st.lastFinishReason === "yielded" || pendingStill)) {
        watchPending(st);
      }
      // the run slot is free again: any row the turn never reached is still the server's,
      // and the truth is one cheap read away
      void refreshQueued(st.key);
      if (st.orphan) {
        states.delete(st.key);
        dropEphemeralServerRecord(st.sessionId, st.key);
      }
    }
  }

  // the restore path's identity: without the cache a refetch re-mints every row and the keyed
  // v-for destroys it. The cache lives with the STATE and resolves addresses by the same rule
  // the canvas paint uses (omega.ts transcriptAddress), so a refetch updates rows in place.
  const restoreCaches = new WeakMap<ChatState, RestoreTranscript>();
  function restoreFor(st: ChatState): RestoreTranscript {
    let cache = restoreCaches.get(st);
    if (!cache) {
      cache = createRestoreTranscript();
      restoreCaches.set(st, cache);
    }
    return cache;
  }

  // does this state's view still match the truth? A live run makes it mid-flight; otherwise the
  // row must still carry the restored stamp. An UNKNOWN row refetches, never guesses.
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
    // the reuse refusal is evidence-based (row stamp intact, no live run), never sticky; force = the caller KNOWS.
    if (!force && st.loaded && !viewIsStale(st, sessionId)) return;
    try {
      const [detail, queued] = await Promise.all([
        fetchTranscript(sessionId),
        fetchQueue(sessionId).catch(() => [] as QueuedRow[]),
      ]);
      st.queued = queued;
      const durable = persistedTelemetry(detail.session);
      paintDurable(st, durable.tokens);
      st.tokensPerSecond = durable.tps;
      st.lastChunksPerSecond = durable.cps;
      // restore IS rendering where the canvas panel does not cover, through the cache, so rows keep their instance
      st.restored = restoreFor(st).apply(detail.items, sessionId);
      // the branch points ride the restore; absent key = plain session, [] keeps it total
      st.variants = detail.variants ?? [];
      st.loaded = true;
      // the reuse witness: this restore now stands for the session AT this row stamp
      st.view.stamp = detail.session?.updated_at ?? "";
      // a session mid-generation reports running=true: subscribe to its live stream
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
    // a draft tab with a send already streaming has no session to steer yet
    if (!st.sessionId && busyKeys.value.has(key)) return false;
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
    // The run slot is per session. While it is taken the send is a STEER: the server parks
    // the row and the live turn takes it at its next batch boundary - no second run, no 409.
    if (st.sessionId && busyAll.value.has(st.sessionId)) {
      try {
        const rows = await sendSteer(messages, st.sessionId, model, thinking);
        if (rows) {
          st.queued = mergeQueuedRows(st.queued, rows);
          st.statusLine = "";
          deps.refreshSessions();
          return true;
        }
        // The run ended between the busy read and the post: that send STARTED a run. Ride
        // it (its stream is detached; the panel and the runs topic carry the rest).
        await load(key, st.sessionId, true);
        return true;
      } catch (e) {
        st.statusLine = `steer failed: ${e instanceof Error ? e.message : String(e)}`;
        return false;
      }
    }
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
        // ephemeral: no-persist - the server mints (or continues) an in-memory session, denies the
        // memory tool and never writes the store
        st.ephemeral,
        ws,
      ),
    );
    return true;
  }

  // the pipe action carries the session alone - the replay takes the session's own run configuration.
  async function retry(key: string): Promise<boolean> {
    const st = ensureState(key);
    if (composerBusy(key)) return false;
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
  // shell commands go local (never an agent round-trip); a failure lands in statusLine, never a swallowed throw.
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
        // the first line that carries TEXT: several commands open with a blank line, which reads as nothing happened.
        // An answer with no text is an answer with NO TEXT - the fabricated "done" is precisely how /compact read as
        // a success while it ran nothing at all.
        const first = (res.text ?? "")
          .split("\n")
          .map((l) => l.trim())
          .find((l) => l !== "");
        st.statusLine = !first
          ? `${name}: no answer from the server`
          : first.length > 160
            ? first.slice(0, 157) + "..."
            : first;
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

  function composerBusy(key: string): boolean {
    const st = states.get(key);
    if (!st) return false;
    return st.sessionId ?
      busyAll.value.has(st.sessionId)
      : busyKeys.value.has(key);
  }

  // refreshQueued re-reads the session's waiting rows. The queue is SERVER truth
  // (session-held, surviving the run that queued it), so a reattach, a stale delete or a
  // crash recovery all settle on one read instead of a local guess.
  async function refreshQueued(key: string): Promise<void> {
    const st = states.get(key);
    const sid = st?.sessionId;
    if (!st || !sid) return;
    try {
      const rows = await fetchQueue(sid);
      // a tab re-pointed at another session mid-read never adopts foreign rows
      if (st.sessionId !== sid) return;
      st.queued = rows;
    } catch {
      // a queue read is a nicety: a fault leaves the rows the surface already holds
    }
  }

  // deleteQueued drops one row before the boundary reaches it (rule 4's delete).
  async function deleteQueued(key: string, id: string): Promise<boolean> {
    const st = states.get(key);
    const sid = st?.sessionId;
    if (!st || !sid || !id) return false;
    try {
      const removed = await deleteQueueRow(sid, id);
      if (st.sessionId !== sid) return removed;
      st.queued = dropQueuedRow(st.queued, id);
      // 404 = the turn already took it: re-read, never resurrect the row locally
      if (!removed) await refreshQueued(key);
      return removed;
    } catch (e) {
      st.statusLine = `queue delete failed: ${e instanceof Error ? e.message : String(e)}`;
      return false;
    }
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

  // close = gone: delete the ephemeral record once no other state holds it; a miss is a benign race.
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
    deleteQueued,
    refreshQueued,
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

// createCanvasTranscript is the pane's render source for the chat panel: rows -> component
// items, KEEPING ITEM IDENTITY across deltas (a 250ms repaint must not remount every
// bubble - meters, countdowns and the keyed v-for all read identity).
//
// IDENTITY LAW: a row's address is its STORE ROW ID, never its position, and the rule
// lives in ONE place (omega.ts transcriptAddress) because the REST restore paints the same
// rows from the same wire. The payload is a sliding window (40 rows,
// agent/api/chat_panel.go): one append shifts every surviving index, so a positional key
// re-mints every row AND lets the tail row inherit an existing row's object - another
// row's meter anchor, fold pin and streaming bit.
//
// THE ID-LESS CASE, stated: an in-flight row carries no id (core.ChatRow.ID). Its SLOT
// address is role + block type + ordinal among the payload's id-less rows of that shape,
// so a head slide moves nothing. A persisted row ADOPTS its slot item onto the id address
// (the transition must not swap the object); slots are swept, so none can be inherited -
// the wire's in-flight rows carry none. Cache items are reactive proxies, so an in-place
// delta write invalidates exactly the effects reading those fields.

// VERDICT COUNTDOWN ANCHOR: a card's deadline is a fact about the CALL, not the pane. The
// SERVER's `started_at` is a fixed instant, so re-deriving it at every paint never drifts
// and a tab opened four minutes in reads 1:00, not 5:00. Without one, first sight at MODULE
// scope keyed by the call id (bounded: a card that old is settled or gone).
const verdictAnchors = new Map<string, number>();
const VERDICT_ANCHOR_MAX = 256;

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

  interface Paint {
    resultIds: ReadonlySet<string>;
    ordinals: Map<string, number>;
    usedSlots: Set<string>;
    lastIdOfShape: ReadonlyMap<string, number>;
  }

  // an id-less row's slot address by the SAME rule the restore uses; consume=false only PEEKS at it.
  function liveAddress(row: ChatRowWire, block: ChatBlockWire, ctx: Paint, consume: boolean): string {
    return transcriptAddress(undefined, `${row.role}:${block.type}`, ctx.ordinals, consume);
  }

  // the store id AND block type when persisted, else the slot. The type belongs to the address
  // because ONE store row lands as SEVERAL wire rows (a turn that reasoned then answered is one
  // id with a thinking row and a text row): keyed by the id alone they shared ONE object and each
  // paint wrote the other's `kind` over it - a CHANGED reactive write on an unchanged payload
  // that re-armed the paint and spun the pane.
  function rowAddress(row: ChatRowWire, block: ChatBlockWire, ctx: Paint): string {
    return transcriptAddress(row.id, `${row.role}:${block.type}`, ctx.ordinals);
  }

  // a row never receives another row's object: adoption is limited to the shape's last ID-BEARING
  // row (the store persists the open tail last), and the adopted entry is always an id-less slot.
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

  // `live` is passed explicitly, never derived from the key - a call id is a stable wire identity even without a row id
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
      // the duration rides the block both ways: a completed phase keeps its server-measured span,
      // a streaming one the elapsed-so-far its meter counts on from
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
          // the card is issued; only its anchor moves, re-derived at every paint (the
          // elapsed-meter law): a later delta carrying the start stamp moves it once.
          if (cached.status === "pending") {
            cached.issuedAt = verdictAnchorMs(callId, block.started_at) ?? cached.issuedAt;
          }
          return cached;
        }
        const card = reactive(verdictCardFromToolArgs(block.tool_call?.arguments));
        card.callId = callId;
        card.status = "pending";
        // anchored whether or not a stream is live: a yielded run's card is still pending
        card.issuedAt = verdictAnchorMs(callId, block.started_at);
        put(vsig, live, card);
        return card;
      }
      // keyed by its CALL id when the wire carries one (the result row merges onto it there);
      // otherwise its own slot - a payload index is never an address
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
      // an unanswered call is the PENDING chip, never a done line
      it.status =
        blockRunning ? "running"
        : callId && !resultIds.has(callId) ? "pending"
        : "done";
      // a LIVE call meters from its open wall clock, resolved ONCE PER ITEM at first sight and NOT
      // rewritten later: a re-derived value differs on every paint of the same payload, and a changed
      // write re-arms the computeds that called the paint - the pane spins.
      if (isLive(it.status) && it.elapsedMs === undefined) {
        const anchor = toolCallAnchorMs(block.started_at, block.elapsed_ms);
        if (anchor !== undefined) it.elapsedMs = anchor;
      }
      put(csig, live, it);
      return it;
    }
    // prose (text block): one bubble per response segment - response_ms when completed, elapsed_ms on the open one
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
    // one push per item object: a merge RESOLVES to an item already pushed, and a re-push would
    // render the line twice with a duplicate key
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
      // another conversation took the panel over; the verdict anchors do NOT clear - a countdown
      // keyed by call id must not restart mid-deadline.
      session = payload.session ?? "";
      cache.clear();
      slots.clear();
    }
    // only the shape's LAST ID-BEARING row may adopt a live slot; an earlier row is a different row.
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
    // the same turn-total pass the restore builder runs, over the same two row stamps.
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
