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
  createSession,
  fetchTranscript,
  renderTranscriptItems,
  persistedTelemetry,
  streamChat,
  attachSession,
  stopSession,
  hasInterruptedMarker,
  withoutMarkers,
  lastUserText,
  attachImageUrl,
  type OmegaImage,
} from "./omega.ts";

// --- transcript item shapes (moved from app.vue: every chat instance
//     renders from them; the app's status bar reads the focused state) ---

export interface MessageItem {
  kind: "message";
  role: "user" | "assistant";
  content: string;
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
  | InterruptedItem;

/** ChatState is ONE chat tab's view of ITS session: transcript, draft,
    telemetry. A stream writes ONLY its own state, so any session can
    stream while another tab renders anywhere in the dock. */
export interface ChatState {
  key: string;
  // the bound session id; null while the tab's chat is unsaved
  // (composer-first) - the FIRST send creates it via POST /sessions
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
}

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
  ensureState(key: string, sessionId?: string | null): ChatState;
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

  function ensureState(key: string, sessionId: string | null = null): ChatState {
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
      });
      st = states.get(key) as ChatState;
      return st;
    }
    // the tab binds the session: an unsaved state that gained its id
    // (the create path wrote it right after this ensure) adopts it - the
    // key stays the tab instance's identity
    if (sessionId && st.sessionId !== sessionId) st.sessionId = sessionId;
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
        assistantPushed = true;
      }
    };
    // Tool phases split the turn into response segments: each text after
    // a tool phase is its own bubble, so the timeline reads
    // chronologically (preamble -> tools -> final answer).
    let segmentBreak = false;
    let thinking: ThinkingItem | null = null;
    // anything at all arrived - the finalize no-ghost-bubble rule needs
    // this: a reattach to an ALREADY finished run (404 path) pushes nothing
    let sawActivity = false;
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
        sawActivity = true;
        if (!thinking) {
          thinking = { kind: "thinking", content: "", streaming: true };
          st.transcript.push(thinking);
        }
        thinking.content += content;
        onChunk(content.length);
        st.notify?.();
      },
      onResponse: (content) => {
        sawActivity = true;
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
        pushAssistant();
        assistant.content += content;
        onChunk(content.length);
        st.notify?.();
      },
      onToolCall: (tool) => {
        sawActivity = true;
        segmentBreak = true;
        if (thinking) {
          thinking.streaming = false;
          thinking = null;
        }
        // the segment's text is over - the tool lines carry the activity
        // now; the next onResponse builds a fresh streaming bubble
        assistant.streaming = false;
        st.transcript.push({
          kind: "tool",
          name: tool.name,
          status: "running",
          output: tool.output,
        });
        st.notify?.();
      },
      onToolResult: (result) => {
        sawActivity = true;
        // attach the result to the most recent tool line (status -> done)
        for (let i = st.transcript.length - 1; i >= 0; i--) {
          const item = st.transcript[i];
          if (item?.kind !== "tool") continue;
          if (item.status !== "running") continue;
          item.status = result.isError ? "error" : "done";
          item.result = result.content;
          break;
        }
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
      finalize(interrupted: boolean) {
        st.lastChunksPerSecond = st.liveChunksPerSecond;
        st.liveChunksPerSecond = 0;
        for (const item of st.transcript) {
          if (item.kind === "tool" && item.status === "running")
            item.interrupted = true;
        }
        // thinking is assigned inside stream callbacks the type system
        // can't track across the closure - cast to the runtime type
        const liveThinking = thinking as ThinkingItem | null;
        if (liveThinking?.streaming) liveThinking.streaming = false;
        // interrupted with no output keeps only the marker, no empty
        // ghost bubble; a run with zero events (404 reattach race) also
        // pushes nothing.
        if ((!interrupted || assistant.content) && (sawActivity || assistant.content))
          pushAssistant();
        assistant.streaming = false;
        // the live transcript IS the session's content now: a remount
        // may refetch, but a stream that just ended never needs to
        st.loaded = true;
      },
    };
  }
  type StreamDriver = ReturnType<typeof makeStreamDriver>;

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
    // (a benign 404/409 race).
    const sessionId = st.sessionId ?? "";
    st.cancelRun = () => {
      stopSession(sessionId).catch(() => {});
    };
    st.statusLine = "reasoning";
    let interrupted = false;
    try {
      await start(drv);
    } catch (e) {
      if (e instanceof StreamInterruptedError) {
        // transport death mid-stream: the partial bubble stays exactly
        // as it is - the marker carries the resume affordance instead
        // of appending error text to it
        interrupted = true;
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
      drv.finalize(interrupted);
      busyKeys.value = withBusy(busyKeys.value, st.key, false);
      st.attached = false;
      st.cancelRun = undefined;
      st.statusLine = "";
      // A stream ending on the FOCUSED tab never unreads itself - the
      // user watched the content live, so it is marked read. A
      // background stream (this page or another) leaves its row
      // unread: that is the feature.
      if (st.sessionId && deps.isFocused(st.key)) deps.markRead(st.sessionId);
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
      // run really ends - the refresh above runs too early to see it
      if (!row || !row.label) {
        setTimeout(() => {
          deps.refreshSessions();
        }, TITLE_REFRESH_MS);
      }
      st.notify?.();
      if (st.orphan) states.delete(st.key);
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
      if (hadInterrupted) st.transcript.push({ kind: "interrupted" });
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
    // a fresh send retires a stale resume affordance - after a later
    // exchange the marker no longer describes the transcript state
    if (hasInterruptedMarker(st.transcript))
      st.transcript = withoutMarkers(st.transcript);
    st.transcript.push({
      kind: "message",
      role: "user",
      content: text,
      images: st.attachments.length ? st.attachments.slice() : undefined,
      time: new Date().toISOString(),
    });
    st.notify?.(true);

    // A session must exist before the first /chat or the backend 404s.
    // An unsaved tab creates it here and writes the binding back onto
    // its tab (the dock tree owns the sessionRef) - no rekey: the
    // state keeps its tab key, only the binding moves.
    if (!st.sessionId) {
      try {
        const s = await createSession();
        st.sessionId = s.id;
        deps.setTabSessionRef(key, s.id);
        deps.refreshSessions();
      } catch (e) {
        st.transcript.push({
          kind: "message",
          role: "assistant",
          content: `[error: ${e}]`,
        });
        return false;
      }
    }
    const images = st.attachments.slice();
    st.attachments = [];
    await runStream(st, (drv) =>
      streamChat(
        [{ role: "user", content: text, images: images.length ? images : undefined }],
        st.sessionId!,
        drv.handlers,
        model,
        thinking,
      ),
    );
    return true;
  }

  // retry re-sends the session's last COMPLETE user message as a FRESH
  // turn: the interrupted exchange stays in the transcript - its partial
  // output and marker are the honest record - and submit appends a NEW
  // user bubble; history is never rewritten.
  async function retry(key: string, model?: string, thinking?: number): Promise<boolean> {
    const st = ensureState(key);
    if (composerBusy(key)) return false;
    const text = lastUserText(st.transcript);
    if (text === null) return false;
    submit(key, text, model, thinking);
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

  function closeState(key: string) {
    const st = states.get(key);
    if (!st) return;
    if (busyKeys.value.has(key)) st.orphan = true;
    else states.delete(key);
  }

  function closeSession(sessionId: string) {
    for (const [key, st] of [...states.entries()]) {
      if (st.sessionId !== sessionId) continue;
      closeState(key);
    }
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
    cancel,
    composerBusy,
    closeState,
    closeSession,
    noteServerActive,
  };
}