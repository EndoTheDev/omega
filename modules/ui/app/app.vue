<template>
  <div class="shell">
    <MenuBar
      :sidebar-open="sidebarPaneOpen"
      :inspector-open="inspectorPaneOpen"
      :view="mainView"
      @toggle-sidebar="toggleSidebar"
      @toggle-inspector="toggleInspector"
      @settings="openSettings"
      @view="switchMainView" />

    <!-- drawer scrim: sibling of pane-area, NOT nested inside it - the
         backdrop-filter frost needs the same backdrop root as the settings
         scrim to render identically -->
    <div
      v-if="isMobile && (drawerOpen || inspectorOpen)"
      class="scrim"
      @click="closeOverlays" />

    <div class="pane-area">
      <!-- dock: THE layout tree (card t_63253264). Recursive split/pane
           components render omega.dock-layout; the named region templates
           below flow down and surface inside whichever pane hosts them.
           A split creates a second, empty pane - its menu (the settled '+'
           component-menu ruling) lists what it can host. Mobile keeps the
           drawer/overlay geometry; mobile cells collapse via kind- CSS. -->
      <DockSplit :node="dock.tree.value">
        <template #pane-sidebar>
          <AppSidebar
            class="pane"
            :style="isMobile ? { width: 'calc(100% - 30%)' } : undefined"
            :sessions="sessions"
            :active-id="activeId"
            :streaming-ids="busyAll"
            :open="drawerOpen"
            :show-archived="showArchived"
            @open="openSession"
            @new="newChat"
            @pin="togglePin"
            @archive="toggleArchive"
            @remove="askDelete"
            @toggle-archived="showArchived = $event" />
        </template>

        <template #pane-chat>
          <main class="chat pane">
            <!-- main-view switcher: chat content hides (display:contents keeps
                 the flex layout transparent so composer/wrap stay direct
                 flex children), the module-owned board (modules/kanban/ui)
                 replaces it through the composition panel list -->
            <div :style="{ display: mainView === 'chat' ? 'contents' : 'none' }">
            <!-- wrap owns the pill's absolute placement: the scroll container
                 itself scrolls its content (an absolutely positioned child of
                 it would scroll away with it) -->
            <div class="messages-wrap">
              <div
                class="messages"
                ref="messagesEl">
                <p
                  v-if="transcript.length === 0"
                  class="empty">
                  say something to
                  <span class="brand">Ωmega</span>
                </p>
                <template
                  v-for="(item, i) in transcript"
                  :key="itemKey(item)">
                  <ChatMessage
                    v-if="item.kind === 'message'"
                    :role="item.role"
                    :content="item.content"
                    :time="item.time"
                    :final-ms="item.finalMs"
                    :streaming="item.streaming" />
                  <ThinkingBlock
                    v-else-if="item.kind === 'thinking' && showThinking"
                    :content="item.content"
                    :final-ms="item.finalMs"
                    :streaming="item.streaming" />
                  <ToolCallLine
                    v-else-if="item.kind === 'tool'"
                    :name="item.name"
                    :status="item.status"
                    :interrupted="item.interrupted"
                    :final-ms="item.finalMs"
                    :output="item.output"
                    :result="item.result" />
                  <div
                    v-else-if="item.kind === 'interrupted'"
                    class="interrupted-line">
                    <span>stream interrupted</span>
                    <button class="resume" @click="retryStream">retry</button>
                  </div>
                </template>
                <div
                  v-if="statusLine"
                  class="status-line">
                  {{ statusLine }}…
                </div>
              </div>
              <button
                v-if="jumpPill"
                class="jump-live"
                @click="jumpToLive">
                ⌄ live
              </button>
            </div>

            <AppComposer
              v-model:draft="draft"
              v-model:model="selectedModel"
              :models="models"
              :busy="composerBusy"
              :can-retry="composerCanRetry"
              @send="send"
              @cancel="cancelActiveRun"
              @retry="retryStream" />
            </div>

            <div
              v-if="mainView === 'board'"
              class="board-pane">
              <component
                :is="boardComponent"
                v-if="boardComponent"
                @open-session="openBoardSession" />
              <p
                v-else
                class="empty">
                board module not loaded
              </p>
            </div>
          </main>
        </template>

        <template #pane-inspector>
          <!-- both right panes always mounted, stacked in one slot: toggling
               slides one in over the other instead of a v-if content flash -->
          <div class="pane-slot">
            <RightSidebar
              class="slot-pane"
              :class="{ active: !settingsOpen }"
              :style="isMobile ? { width: 'calc(100% - 30%)' } : undefined"
              :open="inspectorOpen"
              :visible="isMobile ? inspectorOpen : !settingsOpen" />

            <SettingsDrawer
              class="slot-pane"
              :class="{ active: settingsOpen }"
              :open="settingsOpen"
              v-model:show-thinking="showThinking"
              :theme-mode="themeMode"
              @set-theme="setTheme" />
          </div>
        </template>
      </DockSplit>
    </div>

    <!-- frosted backdrop only on mobile - on desktop the settings drawer
         is the right sidebar swapped for settings content, no overlay -->
    <div
      v-if="isMobile && settingsOpen"
      class="scrim"
      @click="settingsOpen = false" />

    <!-- destructive confirm: delete walks the store cascade server-side,
         so there is no undo - the dialog names the session -->
    <div
      v-if="deleteId"
      class="confirm-scrim"
      @click.self="cancelDelete">
      <div
        class="confirm-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="confirm-delete-title">
        <p
          id="confirm-delete-title"
          class="confirm-title">Delete session?</p>
        <p class="confirm-body">
          "{{ deleteLabel }}" and its full history are removed
          permanently. There is no undo.
        </p>
        <div class="confirm-actions">
          <button
            class="new-btn"
            @click="cancelDelete">
            cancel
          </button>
          <button
            class="confirm-danger"
            @click="confirmDelete">
            delete
          </button>
        </div>
      </div>
    </div>

    <StatusBar
      :healthy="healthy"
      :streaming="anyBusyNow"
      :message-count="messageCount"
      :tokens="tokens"
      :context-size="contextSize"
      :tokens-per-second="tokensPerSecond"
      :live-chunks-per-second="liveChunksPerSecond"
      :last-chunks-per-second="lastChunksPerSecond"
      :version="healthVersion"
      :up-seconds="upSeconds" />
  </div>
</template>

<script setup lang="ts">
  import { computed, onMounted, onUnmounted, provide, reactive, ref, watch } from "vue";
  import { modulePanels } from "./composables/module-panels.generated";
  import {
    anyBusy,
    withBusy,
    runningIds,
    pinSession,
    markSessionRead,
    archiveSession,
    deleteSession,
    parseSessionPath,
    sessionPath,
    isAtBottom,
    pillVisible,
    lastUserText,
    hasInterruptedMarker,
    withoutMarkers,
    createItemKeySource,
    streamChat,
    attachSession,
    stopSession,
    fetchTranscript,
    renderTranscriptItems,
    fetchHealthInfo,
    persistedTelemetry,
    formatDurationMs,
    StreamInterruptedError,
  } from "./composables/omega";
  import type {
    BusyMap,
    TranscriptDetail,
    TranscriptItem,
    MessageItem,
    ThinkingItem,
    ToolItem,
    InterruptedItem,
  } from "./composables/omega";
  import { createDock, DOCK_API_KEY } from "./composables/dock";
  const healthy = ref(false);
  // /health durability payload: server build tag + uptime (polled, the
  // bar's version + up seg - and the health dot gets a periodic check)
  const healthVersion = ref("");
  const upSeconds = ref(0);
  const HEALTH_POLL_MS = 30000;
  let healthTimer: ReturnType<typeof setInterval> | null = null;
  // running-state poll: the /sessions row's running flag is the single
  // truth for "is this session streaming", so the webui polls it - that
  // is what lets a tab that never held the SSE (every other tab, every
  // future tab) reconcile its pulse instead of owning stream state.
  // A hidden tab skips its ticks; an in-flight poll never overlaps.
  const RUNNING_POLL_MS = 2000;
  let runningTimer: ReturnType<typeof setInterval> | null = null;
  let runningPollInFlight = false;
  function tickRunningPoll() {
    if (typeof document !== "undefined" && document.hidden) return;
    if (runningPollInFlight) return;
    runningPollInFlight = true;
    refreshSessions().finally(() => {
      runningPollInFlight = false;
    });
  }
  const models = ref<string[]>([]);
  const selectedModel = ref("");
  const sessions = ref<Session[]>([]);
  // archived filter: off (default) lists live sessions only; on fetches
  // with archived=include and shows only - muted - archived rows
  const showArchived = ref(false);
  const activeId = ref<string | null>(null);
  const draft = ref("");
  const messagesEl = ref<HTMLElement | null>(null);
  const drawerOpen = ref(false);
  const inspectorOpen = ref(false);
  const isMobile = ref(false);

  // --- dock store (card t_63253264): the layout tree is the single
  // source of layout truth. Loading seeds from omega.dock-layout or
  // migrates the pre-dock pane keys once; every mutation persists the
  // whole tree. The components resolve it via injection. ---
  const dock = createDock(window.localStorage);
  provide(DOCK_API_KEY, dock);
  // MenuBar flags: the drawers (mobile) and the tree (desktop) - the
  // inspector flag reads settingsOpen too, mirroring the shared right slot
  const sidebarPaneOpen = computed(() =>
    isMobile.value ? drawerOpen.value : dock.has("sidebar"),
  );
  const inspectorPaneOpen = computed(() =>
    isMobile.value ? inspectorOpen.value : dock.has("inspector") && !settingsOpen.value,
  );

  // --- per-session stream state (Lvl 2) ---
  // Every session, including the unsaved new chat, owns its transcript and
  // telemetry. A stream writes ONLY its own state, so any session can
  // stream while the view sits anywhere else. NEW_KEY holds the state of
  // an unsaved chat until /sessions returns the real id.
  const NEW_KEY = "new";
  interface SessionState {
    transcript: TranscriptItem[];
    statusLine: string;
    tokens: number;
    tokensPerSecond: number;
    liveChunksPerSecond: number;
    // last-known chunks/s, from the session row's last_run block (or
    // the run that just ended) - the idle state bar's rate display
    lastChunksPerSecond: number;
    // the live run's stop affordance: set by runStream when the session
    // gains a stream, cleared in its finally. The composer's cancel
    // button calls it. A server run another client started (reattach
    // busy) is stoppable too - the stop POST is session-keyed.
    cancelRun?: () => void;
  }
  const EMPTY_STATE: SessionState = {
    transcript: [],
    statusLine: "",
    tokens: 0,
    tokensPerSecond: 0,
    liveChunksPerSecond: 0,
    lastChunksPerSecond: 0,
  };
  const streams = reactive(new Map<string, SessionState>());

  function ensureState(key: string): SessionState {
    let st = streams.get(key);
    if (!st) {
      st = {
        transcript: [],
        statusLine: "",
        tokens: 0,
        tokensPerSecond: 0,
        liveChunksPerSecond: 0,
        lastChunksPerSecond: 0,
      };
      streams.set(key, st);
      // reactive(Map) wraps values on READ - keep the wrapped handle, not
      // the raw object, or state writes stop triggering the view
      st = streams.get(key) as SessionState;
    }
    return st;
  }

  const activeKey = computed(() => activeId.value ?? NEW_KEY);
  const view = computed<SessionState>(
    () => streams.get(activeKey.value) ?? EMPTY_STATE,
  );

  // --- URL reflects the session: hand-rolled History wiring. Every open
  // session is a history entry so back/forward and reload work; switching
  // stays a pure view change (no stream is interrupted). fromHistory is
  // set while a navigation ORIGINATES in the history API - the URL is
  // already correct then, so no sync runs (and one is not appended).
  let fromHistory = false;

  // view is the main-pane view the entry anchors: "chat" walks session
  // paths, "board" roots at /board (the module-owned kanban view)
  function syncURL(id: string | null, push: boolean, view: "chat" | "board" = "chat") {
    if (typeof window === "undefined") return;
    const url = view === "board" ? "/board" : id ? sessionPath(id) : "/";
    if (push) history.pushState({}, "", url);
    else history.replaceState({}, "", url);
  }

  // --- main-view: chat transcript vs the module-owned board (the
  // kanban module ships it through the composition panel list; the shell
  // only knows the mount slot). Switching is the same pure view change a
  // session switch is - no stream is interrupted; /board is a history
  // entry like every open session.
  const mainView = ref<"chat" | "board">("chat");
  const boardComponent = computed(
    () => modulePanels.find((p) => p.module === "kanban")?.component,
  );

  function switchMainView(v: "chat" | "board") {
    if (mainView.value === v) return;
    mainView.value = v;
    if (!fromHistory) syncURL(activeId.value, true, v);
  }

  // a board card's transcript chip lands the user in that conversation
  function openBoardSession(id: string) {
    mainView.value = "chat";
    openSession(id);
  }

  // back/forward walk board and session entries; switching on popstate is
  // a pure view change, so it never interrupts a stream
  function onPop() {
    fromHistory = true;
    if (/^\/board\/?$/.test(window.location.pathname)) {
      mainView.value = "board";
    } else {
      mainView.value = "chat";
      const id = parseSessionPath(window.location.pathname);
      if (id) openSession(id);
      else newChat();
    }
    fromHistory = false;
  }

  // per-session busy set: a session is busy iff its own stream is live.
  // Swapped value-semantically (withBusy) so set changes are reactive.
  // busySet holds LOCALLY initiated streams; serverBusy holds the
  // sessions the BACKEND reports as running (the detached-run active
  // flag on /sessions rows - survives a page refresh, the in-memory map
  // does not). busyAll is the union every read goes through.
  const busySet = ref<BusyMap>(new Set());
  const serverBusy = ref<BusyMap>(new Set());
  const busyAll = computed<BusyMap>(() => {
    const all = new Set(busySet.value);
    for (const id of serverBusy.value) all.add(id);
    return all;
  });
  // global view concerns: read-only over the union, never a lock
  const anyBusyNow = computed(() => anyBusy(busyAll.value));

  // the view reads its own session's state
  const transcript = computed(() => view.value.transcript);
  const statusLine = computed(() => view.value.statusLine);
  const tokens = computed(() => view.value.tokens);
  const tokensPerSecond = computed(() => view.value.tokensPerSecond);
  const liveChunksPerSecond = computed(() => view.value.liveChunksPerSecond);
  const lastChunksPerSecond = computed(() => view.value.lastChunksPerSecond);

  // composer state: the send button is CANCEL while this session's run
  // streams; a retry affordance shows when idle and the transcript has
  // a last user message to re-send (interrupted stream or not)
  const composerBusy = computed(() => busyAll.value.has(activeKey.value));
  const composerCanRetry = computed(
    () => !composerBusy.value && lastUserText(transcript.value) !== null,
  );

  // keyed v-for: identity, not index (createItemKeySource) - see omega.ts
  const itemKey = createItemKeySource();

  // mobile: sidebars are overlays (slide from their side over a scrim);
  // desktop: same toggles mount/unmount the panes inline + squeeze chain
  // mobile: one drawer at a time - opening any pane closes the other two
  function toggleSidebar() {
    if (isMobile.value) {
      // the drawer needs its pane rendered (empty panes are hidden by CSS):
      // absent -> insert it first, then flip the overlay open
      dock.ensureEdge("sidebar", "before");
      inspectorOpen.value = false;
      settingsOpen.value = false;
      drawerOpen.value = !drawerOpen.value;
    } else {
      // desktop: the TREE owns presence - remove/re-insert the edge pane
      // (the store remembers the width and applies the reopen-clamp)
      dock.toggleEdge("sidebar", "before");
    }
  }

  function closeOverlays() {
    drawerOpen.value = false;
    inspectorOpen.value = false;
    settingsOpen.value = false;
  }
  const settingsOpen = ref(false);
  const showThinking = ref(true);
  const contextSize = ref(0);

  const messageCount = computed(
    () => transcript.value.filter((i) => i.kind === "message").length,
  );

  // transcript models the live stream: user message, thinking blocks (one per
  // reasoning run), tool-call lines, assistant message. Restored sessions get
  // the SAME items from the server's GET /sessions/{id}/transcript - no
  // reload-side mapping, one contract (renderTranscriptItems) for both.
  // Read-marking: opening a session IS reading it - live-watched
  // content never unreads itself. The row's unread clears locally so
  // the status element goes grey instantly; the PATCH {"read": true}
  // confirms the server-side stamp (updated_at untouched - reading is
  // not activity). A failed PATCH defers to the next list refresh.
  function markRead(id: string) {
    sessions.value = sessions.value.map((s) =>
      s.id === id ? { ...s, unread: false } : s,
    );
    markSessionRead(id).catch(() => refreshSessions());
  }

  async function openSession(id: string, preloaded?: TranscriptDetail) {
    if (!fromHistory) syncURL(id, true);
    // the switcher follows: opening a chat is a main-view move (from the
    // board, from the sidebar, from history - all land in the transcript)
    mainView.value = "chat";
    activeId.value = id;
    drawerOpen.value = false;
    inspectorOpen.value = false;
    markRead(id);
    const st = ensureState(id);
    // A LOCALLY live stream IS the content - refetching now would clobber
    // the stream state with a mid-flight persisted snapshot. A server-
    // reported active run (serverBusy, e.g. right after a page refresh)
    // must NOT skip this: there is no local transcript to clobber, the
    // transcript fetch below renders persisted history and the attach
    // lands in the live stream.
    if (busySet.value.has(id)) {
      scrollDown(true);
      return;
    }
    st.statusLine = "";
    // an interrupted-stream marker is per-session live state: the refetch
    // below restores PERSISTED history, not the marker - re-append it so
    // the resume affordance survives a view switch (card: switching is a
    // pure view change, retry stays per-session)
    const hadInterrupted = hasInterruptedMarker(st.transcript);
    try {
      const detail = preloaded ?? (await fetchTranscript(id));
      // durable telemetry straight from the persisted row: the token
      // total and the last-run rates land in the status bar before any
      // stream runs - reload shows what the session actually has
      const row = detail.session;
      const durable = persistedTelemetry(row);
      st.tokens = durable.tokens;
      st.tokensPerSecond = durable.tps;
      st.lastChunksPerSecond = durable.cps;
      // restore IS rendering now: the server hands over wire-order
      // transcript items (thinking/text/calls/results with their timing)
      // and renderTranscriptItems maps them straight onto the component
      // contract the live stream paints - no reload-side reconstruction
      // loop to get wrong (reload order, thinking merge, lost durations).
      st.transcript = renderTranscriptItems(detail.items);
      if (hadInterrupted) st.transcript.push({ kind: "interrupted" });
      // Reattach: the run is server-side now (card t_2f9b6110), so a
      // session mid-generation reports active=true on its transcript -
      // the refreshed page renders the persisted history (the user message
      // persisted pre-run, nothing after it yet) and then auto-subscribes
      // to the live events stream. Re-check LOCAL busy only: serverBusy
      // holds this session right now (the run IS active) and must not
      // block the attach - only a double-call within this page does.
      if (detail.running && !busySet.value.has(id)) {
        await runStream(id, st, async (drv) => {
          await attachSession(id, drv.handlers);
        });
      }
    } catch {
      // empty or missing session: keep the previous view rather than
      // dropping it on a transient fetch failure
    }
    scrollDown(true);
  }

  function newChat() {
    if (!fromHistory) syncURL(null, true);
    activeId.value = null;
    mainView.value = "chat";
    // fresh unsaved-chat state - UNLESS an unsaved stream is live, in
    // which case +new returns to the view watching it (no global lock)
    if (!busyAll.value.has(NEW_KEY)) streams.delete(NEW_KEY);
    ensureState(NEW_KEY);
    drawerOpen.value = false;
  }

  // mobile: one drawer at a time - free the slot first
  function toggleInspector() {
    if (isMobile.value) {
      if (settingsOpen.value) settingsOpen.value = false;
      dock.ensureEdge("inspector", "after"); // overlay needs its pane present
      drawerOpen.value = false;
      inspectorOpen.value = !inspectorOpen.value;
      return;
    }
    // desktop: the right edge pane hosts ONE visible pane - the inspector
    // toggle closes settings and shows the inspector; otherwise it toggles
    // the pane itself (the store remembers the width + reopen-clamps)
    if (settingsOpen.value) {
      settingsOpen.value = false;
      dock.ensureEdge("inspector", "after");
      return;
    }
    dock.toggleEdge("inspector", "after");
  }

  // gear is a toggle; the right edge pane hosts settings and the inspector
  // stacked (slot-pane transforms swap them) - opening settings just makes
  // sure the pane is present.
  function openSettings() {
    if (settingsOpen.value) {
      settingsOpen.value = false;
      return;
    }
    if (isMobile.value) {
      // mobile: one drawer at a time - close both other drawers
      inspectorOpen.value = false;
      drawerOpen.value = false;
    }
    dock.ensureEdge("inspector", "after"); // the pane hosts the drawer too
    settingsOpen.value = true;
  }

  // makeStreamDriver builds the per-run stream state over ONE session
  // state: the SSE handler set plus the finalization that freezes meters
  // and bubbles when the stream ends. Shared by BOTH transports - the
  // send stream (POST /chat) and the reattach stream (GET /sessions/<id>/
  // events, the refresh-mid-generation landing path) - so a reattached
  // page renders the run's remaining events exactly like the original
  // sender did. All mutable state (thinking block, current assistant
  // bubble, segment tracking) lives in this closure.
  function makeStreamDriver(st: SessionState) {
    let assistant: MessageItem = {
      kind: "message",
      role: "assistant",
      content: "",
      streaming: true,
    };
    // Lazy push: the assistant bubble enters the transcript at the first
    // response chunk so thinking blocks and tool lines (which arrive earlier
    // in the stream) render BEFORE the reply, matching the event order.
    let assistantPushed = false;
    const pushAssistant = () => {
      if (!assistantPushed) {
        // stamped once, at first appearance - it stays the receive time
        assistant.time = assistant.time ?? new Date().toISOString();
        st.transcript.push(assistant);
        assistantPushed = true;
      }
    };
    // Tool phases split the turn into response segments: each text after a
    // tool phase is its own bubble, so the timeline reads chronologically
    // (preamble -> tools -> final answer).
    let segmentBreak = false;
    let thinking: ThinkingItem | null = null;
    // anything at all arrived (chunks, tool lines, status) - the finalize
    // no-ghost-bubble rule needs this: a reattach to an ALREADY finished
    // run (detail.running raced the finish, the 404 path) pushes nothing
    let sawActivity = false;
    const sentAt = Date.now();
    // a fresh stream takes the meters over: the last-known idle values
    // step aside until this run's own numbers arrive
    st.tokens = 0;
    st.tokensPerSecond = 0;
    st.liveChunksPerSecond = 0;
    st.lastChunksPerSecond = 0;
    // live stream telemetry: per-chunk arrivals make the statusbar a
    // visible proof that the frontend receives chunks as they come - a
    // ticking counter means streaming, a frozen-then-jumping one means
    // something buffers between omega and the page.
    const chunkTimes: number[] = [];
    const onChunk = (len: number) => {
      const now = Date.now();
      chunkTimes.push(now);
      // [0] cannot be undefined while length > 0; the fallback keeps
      // the narrowing honest without an assertion
      while (chunkTimes.length && now - (chunkTimes[0] ?? now) > 1000) chunkTimes.shift();
      st.liveChunksPerSecond = chunkTimes.length;
      // ponytail: chars/4 token estimate - corrected by real usage at end
      st.tokens += Math.ceil(len / 4);
    };
    // the stream writes only its own session's state; auto-scroll follows
    // it only while the view is actually watching this stream - a stream
    // running in the background never yanks an open session's scrollbar
    const isViewed = () => streams.get(activeKey.value) === st;

    const handlers: StreamHandlers = {
      onThinking: (content) => {
        sawActivity = true;
        if (!thinking) {
          thinking = { kind: "thinking", content: "", streaming: true };
          st.transcript.push(thinking);
        }
        thinking.content += content;
        onChunk(content.length);
        if (isViewed()) scrollDown();
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
        if (isViewed()) scrollDown();
      },
      onToolCall: (tool) => {
        sawActivity = true;
        segmentBreak = true;
        // the tool phase closes the thinking block too: turn 2's
        // thinking must mount as a FRESH item after the tool
        // lines, not append to the pre-tool block above them
        if (thinking) {
          thinking.streaming = false;
          thinking = null;
        }
        // the segment's text is over - the tool lines carry the
        // activity now. Killing the flag here hides the streaming
        // cursor during the whole tool phase (in-app comment 5);
        // the next onResponse builds a fresh streaming bubble.
        assistant.streaming = false;
        st.transcript.push({
          kind: "tool",
          name: tool.name,
          status: "running",
          output: tool.output,
        });
        if (isViewed()) scrollDown();
      },
      onToolResult: (result) => {
        sawActivity = true;
        // attach the result to the most recent tool line (status -> done)
        for (let i = st.transcript.length - 1; i >= 0; i--) {
          const item = st.transcript[i];
          // strict mode: indexing can yield undefined - skip before narrowing
          if (item?.kind !== "tool") continue;
          if (item.status !== "running") continue;
          item.status = result.isError ? "error" : "done";
          item.result = result.content;
          break;
        }
        if (isViewed()) scrollDown();
      },
      onStatus: (s) => {
        sawActivity = true;
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
      // payload, non-2xx /chat) into the current assistant bubble - the
      // finalize push guarantees it lands in the transcript.
      appendError(e: unknown) {
        assistant.content += assistant.content ? `\n[error: ${e}]` : `[error: ${e}]`;
      },
      // finalize mirrors the pre-detach finally block: freeze the chunk
      // meter, freeze running tools (results never coming - ToolCallLine
      // stops ticking), close thinking and bubbles.
      finalize(interrupted: boolean) {
        // the run's final live chunk rate becomes the last-known one -
        // the idle bar keeps showing the rate the user just watched
        st.lastChunksPerSecond = st.liveChunksPerSecond;
        st.liveChunksPerSecond = 0;
        for (const item of st.transcript) {
          if (item.kind === "tool" && item.status === "running")
            item.interrupted = true;
        }
        // thinking is assigned inside stream callbacks the type system can't
        // track across the closure - cast to the item type it is at runtime
        const liveThinking = thinking as ThinkingItem | null;
        if (liveThinking?.streaming) liveThinking.streaming = false;
        // interrupted with no output keeps only the marker, no empty
        // ghost bubble; a run with zero events (404 reattach race) also
        // pushes nothing.
        if ((!interrupted || assistant.content) && (sawActivity || assistant.content))
          pushAssistant();
        assistant.streaming = false;
      },
    };
  }
  type StreamDriver = ReturnType<typeof makeStreamDriver>;

  // runStream drives one interaction over a transport: start() performs
  // the POST /chat (send path) or the GET /sessions/<id>/events attach
  // (reattach path) and consumes the SSE with the driver's handlers.
  // Owns the busy entry for the session key, error rendering (interrupted
  // marker vs error text), read-marking and the refresh follow-ups - so
  // both transports end in the SAME session state.
  async function runStream(key: string, st: SessionState, start: (drv: StreamDriver) => Promise<void>) {
    const drv = makeStreamDriver(st);
    busySet.value = withBusy(busySet.value, key, true);
    // the stop affordance is bound to THIS stream: the cancel button
    // stops the run whose events are landing here. The stop POST is
    // harmless after the run already ended (a benign 404/409 race).
    st.cancelRun = () => {
      stopSession(key).catch(() => {});
    };
    st.statusLine = "reasoning";
    let interrupted = false;
    try {
      await start(drv);
    } catch (e) {
      if (e instanceof StreamInterruptedError) {
        // transport death mid-stream: the partial bubble stays exactly as
        // it is (finalize freezes its streaming flag) - the marker carries
        // the resume affordance instead of appending error text to it
        interrupted = true;
        st.transcript.push({ kind: "interrupted" });
      } else {
        drv.appendError(e);
      }
    } finally {
      drv.finalize(interrupted);
      busySet.value = withBusy(busySet.value, key, false);
      st.cancelRun = undefined;
      st.statusLine = "";
      // A stream ending on the VIEWED session never unreads itself -
      // the user watched the content live, so it is marked read. A
      // background stream (session B while viewing A) leaves its row
      // unread: that is the feature.
      if (activeId.value === key) markRead(key);
      // refresh once the row carries the run's persisted numbers, then
      // re-seed the idle telemetry from it - the bar's last-known values
      // and the post-reload display come from the SAME source (the row),
      // not from the live meter's instantaneous last-second tick
      await refreshSessions();
      const row = sessions.value.find((s) => s.id === key);
      if (row && streams.get(activeKey.value) === st) {
        const durable = persistedTelemetry(row);
        st.tokens = durable.tokens;
        st.tokensPerSecond = durable.tps;
        st.lastChunksPerSecond = durable.cps;
      }
      // the title goroutine writes the session label seconds AFTER the
      // first exchange ends - the refresh above runs too early to see
      // it. One delayed refresh picks it up without a manual reload.
      if (!row || !row.label) {
        setTimeout(() => { refreshSessions(); }, 10000);
      }
      if (streams.get(activeKey.value) === st) scrollDown();
    }
  }

  async function send() {
    const text = draft.value.trim();
    if (!text) return;
    draft.value = "";
    submitText(text);
  }

  // submitText is the ONE send path - the composer and the interrupted
  // retry both land here. Everything keys off THIS session's state and
  // busy entry, so retrying session A never touches session B.
  async function submitText(text: string) {
    const startKey = activeKey.value;
    // per-session guard: only THIS session's live stream blocks a send
    if (busyAll.value.has(startKey)) return;
    const st = ensureState(startKey);
    // a fresh send retires a stale resume affordance - after a later
    // exchange the marker no longer describes the transcript state
    if (hasInterruptedMarker(st.transcript))
      st.transcript = withoutMarkers(st.transcript);
    let key = startKey;
    st.transcript.push({
      kind: "message",
      role: "user",
      content: text,
      time: new Date().toISOString(),
    });
    scrollDown(true);

    // A session must exist before the first /chat or the backend 404s.
    // Unsaved chats live under NEW_KEY until /sessions returns the real
    // id; the state AND the busy entry re-key right after creation.
    if (startKey === NEW_KEY) {
      busySet.value = withBusy(busySet.value, NEW_KEY, true);
      st.statusLine = "reasoning";
      try {
        const r = await fetch("/sessions", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: "{}",
        });
        if (!r.ok) throw new Error(`create session: ${r.status}`);
        const s = await r.json();
        streams.delete(NEW_KEY);
        streams.set(s.id, st);
        busySet.value = withBusy(busySet.value, NEW_KEY, false);
        key = s.id;
        // the row (and its pulse) exists once the sidebar refreshes; the
        // view moves to the session only if the user stayed on this chat
        refreshSessions();
        if (activeKey.value === NEW_KEY) {
          activeId.value = s.id;
          // the temp id never lands in history: the entry it replaces is
          // home, so back from the real session returns home
          syncURL(s.id, false);
        }
      } catch (e) {
        busySet.value = withBusy(busySet.value, NEW_KEY, false);
        st.statusLine = "";
        st.transcript.push({
          kind: "message",
          role: "assistant",
          content: `[error: ${e}]`,
        });
        return;
      }
    }
    await runStream(key, st, (drv) =>
      streamChat(
        [{ role: "user", content: text }],
        key,
        drv.handlers,
        selectedModel.value || undefined,
      ),
    );
  }

  async function refreshSessions() {
    try {
      const list = await fetchSessions(showArchived.value);
      sessions.value = list;
      // server-reported live runs replace (not union into) the last
      // snapshot: these are the sessions a JUST-refreshed page sees as
      // pulsing - local streams carry their own busy entries in busySet.
      serverBusy.value = runningIds(list);
    } catch {
      // sidebar refresh is best-effort
    }
  }

  // cancelActiveRun stops the VIEWED session's live run (the composer's
  // cancel button). The run's unwind keeps what streamed; the cancelled
  // agent_end lands as the interrupt marker (handlePayload), which the
  // retry affordance reads. No-op when this view has no bound stream.
  function cancelActiveRun() {
    const st = streams.get(activeKey.value);
    st?.cancelRun?.();
  }

  // retry re-sends the session's last COMPLETE user message as a FRESH
  // turn (card t_6c97a95a): the interrupted exchange stays in the
  // transcript - its partial output and marker are the honest record -
  // and submitText appends a NEW user bubble; history is never
  // rewritten. (The interrupted marker retires per the fresh-send rule:
  // after a later exchange it would no longer describe the transcript.)
  // Reads and writes only THIS session's state, so other sessions'
  // transcripts and busy flags are untouched.
  function retryStream() {
    const key = activeKey.value;
    const st = ensureState(key);
    if (busyAll.value.has(key)) return;
    const text = lastUserText(st.transcript);
    if (text === null) return;
    submitText(text);
  }

  // togglePin flips the pinned flag on a session row: optimistically, so
  // the section move is instant, then PATCHes it onto the server row - a
  // failed PATCH reverts the local state and the refresh stays honest.
  async function togglePin(id: string, target: boolean) {
    const updateList = (list: Session[]) =>
      list.map((s) => (s.id === id ? { ...s, pinned: target } : s));
    const before = sessions.value;
    sessions.value = updateList(sessions.value);
    try {
      await pinSession(id, target);
    } catch {
      sessions.value = before;
      refreshSessions();
    }
  }

  // toggleArchive flips the archive flag. Instant + reversible: the row
  // leaves the list the moment the flag lands locally (the server list
  // already carries only the visible archive side, so the patched row
  // is gated exactly like refreshSessions - on in archived view, off in
  // the live view). A failed PATCH reverts and re-syncs.
  async function toggleArchive(id: string, target: boolean) {
    const updateList = (list: Session[]) =>
      list
        .map((s) => (s.id === id ? { ...s, archived: target } : s))
        .filter((s) => (showArchived.value ? s.archived : !s.archived));
    const before = sessions.value;
    sessions.value = updateList(sessions.value);
    try {
      await archiveSession(id, target);
    } catch {
      sessions.value = before;
      refreshSessions();
    }
  }

  // delete flow: the sidebar only signals intent; the confirm dialog is
  // the only door to the API call. Deleting the VIEWED session walks the
  // view back to an unsaved chat.
  const deleteId = ref<string | null>(null);
  const deleteLabel = ref("");
  function askDelete(id: string) {
    const s = sessions.value.find((x) => x.id === id);
    deleteId.value = s ? s.id : "";
    deleteLabel.value = (s?.label || s?.id || "").slice(0, 80);
  }
  function cancelDelete() {
    deleteId.value = null;
  }
  async function confirmDelete() {
    const id = deleteId.value;
    deleteId.value = null;
    if (!id) return;
    try {
      await deleteSession(id);
    } catch {
      // a failed DELETE leaves the row in place - the refresh shows the
      // truth
      refreshSessions();
      return;
    }
    if (activeId.value === id) newChat();
    refreshSessions();
  }

  // toggling the archived filter refetches through the include filter -
  // the two archive sides never mix in one list
  watch(showArchived, () => refreshSessions());

  // scrollDown keeps the view pinned to the newest content. "pinned" tracks
  // the USER's intent: it only changes on actual scroll events, not when
  // new content lands (post-append bottom-measurement misreads fresh
  // content bursts >40px as "user scrolled up" and kills the follow).
  const pinned = ref(true);

  // jump-to-live pill: a scroll-position affordance - visible whenever the
  // transcript is scrolled up, stream or not. Derived on scroll events only.
  const jumpPill = ref(false);

  function nearBottom(): boolean {
    const el = messagesEl.value;
    if (!el) return true;
    return isAtBottom(el.scrollTop, el.clientHeight, el.scrollHeight);
  }

  function onMessagesScroll() {
    pinned.value = nearBottom();
    const el = messagesEl.value;
    if (!el) return;
    jumpPill.value = pillVisible(el.scrollTop, el.clientHeight, el.scrollHeight);
  }

  function jumpToLive() {
    jumpPill.value = false;
    scrollDown(true);
  }

  function scrollDown(force = false) {
    if (force) pinned.value = true;
    requestAnimationFrame(() => {
      const el = messagesEl.value;
      if (!el) return;
      if (!force && !pinned.value) return;
      el.scrollTop = el.scrollHeight;
    });
  }

  // --- theme: light / dark / auto, persisted, auto follows the OS live ---
  type ThemeMode = "auto" | "light" | "dark";
  const themeMode = ref<ThemeMode>("auto");
  let systemPrefersDark = true;
  let mediaQuery: MediaQueryList | null = null;

  function applyTheme() {
    if (typeof document === "undefined") return;
    const dark =
      themeMode.value === "auto" ?
        systemPrefersDark
      : themeMode.value === "dark";
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  }

  function setTheme(mode: ThemeMode) {
    themeMode.value = mode;
    localStorage.setItem("omega.theme-mode", mode);
    applyTheme();
  }

  watch(themeMode, applyTheme);

  // --- tab-title spinner: a global read over the per-session busy map ---
  // Any live stream - in any session - spins the tab title; individual
  // sessions stay untouched (and unlocked) by it.
  const TITLE_FRAMES = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
  let spinnerTimer: ReturnType<typeof setInterval> | null = null;
  let savedTitle: string | null = null;

  watch(anyBusyNow, (live) => {
    if (typeof document === "undefined") return;
    if (live) {
      if (savedTitle === null) savedTitle = document.title;
      let i = 0;
      spinnerTimer = setInterval(() => {
        i = (i + 1) % TITLE_FRAMES.length;
        document.title = `${TITLE_FRAMES[i]} omega`;
      }, 120);
    } else {
      if (spinnerTimer) clearInterval(spinnerTimer);
      spinnerTimer = null;
      if (savedTitle !== null) document.title = savedTitle;
      savedTitle = null;
    }
  });

  onMounted(async () => {
    // stick-to-bottom: user scroll events drive the pinned flag
    messagesEl.value?.addEventListener("scroll", onMessagesScroll);

    // mobile detection: sidebars become slide-in overlays below 768px
    const mobileQuery = window.matchMedia("(max-width: 768px)");
    isMobile.value = mobileQuery.matches;
    mobileQuery.addEventListener("change", (e) => {
      isMobile.value = e.matches;
      closeOverlays(); // crossing the breakpoint resets overlay state
    });

    // restore persisted mode
    const saved = localStorage.getItem("omega.theme-mode") as ThemeMode | null;
    if (saved === "light" || saved === "dark" || saved === "auto")
      themeMode.value = saved;

    // auto mode: follow prefers-color-scheme reactively
    mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    systemPrefersDark = mediaQuery.matches;
    mediaQuery.addEventListener("change", (e) => {
      systemPrefersDark = e.matches;
      applyTheme();
    });
    applyTheme();

    // history wiring: back/forward open the session the URL describes
    window.addEventListener("popstate", onPop);

    const h = await fetchHealthInfo();
    healthy.value = h !== null;
    if (h) {
      healthVersion.value = h.version ?? "";
      upSeconds.value = h.uptime_seconds ?? 0;
    }
    // the health poll keeps the status dot honest and ticks the uptime
    healthTimer = setInterval(async () => {
      const h = await fetchHealthInfo();
      if (h) {
        healthy.value = true;
        healthVersion.value = h.version ?? "";
        upSeconds.value = h.uptime_seconds ?? 0;
      } else {
        healthy.value = false;
      }
    }, HEALTH_POLL_MS);
    try {
      const m = await fetchModels();
      models.value = m.models;
      selectedModel.value = m.current;
      if (m.context_window) contextSize.value = m.context_window;
    } catch {
      // models are decoration until a chat runs
    }
    refreshSessions();
    // running-state poller: every tab reconciles against the rows
    runningTimer = setInterval(tickRunningPoll, RUNNING_POLL_MS);

    // restore the view named by the URL: /board mounts the kanban view,
    // a session path restores that session, anything else routes home
    // (and the URL comes with it)
    if (/^\/board\/?$/.test(window.location.pathname)) {
      mainView.value = "board";
    }
    const initial = parseSessionPath(window.location.pathname);
    if (initial) {
      try {
        const detail = await fetchTranscript(initial);
        fromHistory = true;
        await openSession(initial, detail);
        fromHistory = false;
      } catch {
        history.replaceState({}, "", "/");
        activeId.value = null;
        ensureState(NEW_KEY);
      }
    }
  });

  onUnmounted(() => {
    window.removeEventListener("popstate", onPop);
    if (healthTimer) clearInterval(healthTimer);
    if (runningTimer) clearInterval(runningTimer);
    messagesEl.value?.removeEventListener("scroll", onMessagesScroll);
    if (spinnerTimer) clearInterval(spinnerTimer);
    if (typeof document !== "undefined" && savedTitle !== null)
      document.title = savedTitle;
  });
</script>
