<template>
  <div class="shell">
    <MenuBar
      :sidebar-open="isMobile ? drawerOpen : sidebarVisible"
      :inspector-open="isMobile ? inspectorOpen : rightVisible"
      @toggle-sidebar="toggleSidebar"
      @toggle-inspector="toggleInspector"
      @settings="openSettings" />

    <!-- drawer scrim: sibling of pane-area, NOT nested inside it - the
         backdrop-filter frost needs the same backdrop root as the settings
         scrim to render identically -->
    <div
      v-if="isMobile && (drawerOpen || inspectorOpen)"
      class="scrim"
      @click="closeOverlays" />

    <div class="pane-area">
      <!-- always-mounted pane groups: closed state slides via negative
           margin (clipped by pane-area overflow) instead of v-if unmount,
           so every pane animates on both desktop and mobile -->
      <div
        class="pane-group"
        :style="{
          marginLeft:
            sidebarVisible || isMobile
              ? '0'
              : -(sidebarWidth + 6) + 'px',
        }">
        <AppSidebar
          class="pane"
          :style="
            isMobile ? { width: 'calc(100% - 30%)' } : { width: sidebarWidth + 'px' }
          "
          :sessions="sessions"
          :active-id="activeId"
          :streaming-ids="busySet"
          :open="drawerOpen"
          @open="openSession"
          @new="newChat"
          @pin="togglePin" />

        <PaneDivider
          v-if="!isMobile && sidebarVisible"
          ref="dividerEl"
          :width="sidebarWidth"
          :min="128"
          storage-key="omega.sidebar-width"
          @update:width="onResize"
          @collapsed="sidebarVisible = false" />
      </div>

      <main class="chat pane">
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
                <button class="resume" @click="retryStream">resume</button>
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
          @send="send" />
      </main>

      <div
        class="pane-group"
        :style="{
          marginRight:
            rightVisible || settingsOpen || isMobile
              ? '0'
              : -(rightWidth + 6) + 'px',
        }">
        <PaneDivider
          v-if="!isMobile && (rightVisible || settingsOpen)"
          ref="rightDividerEl"
          :width="rightWidth"
          :min="128"
          :invert="true"
          storage-key="omega.inspector-width"
          @update:width="onRightResize"
          @collapsed="onRightCollapsed" />

        <!-- both right panes always mounted, stacked in one slot: toggling
             slides one in over the other instead of a v-if content flash -->
        <div
          class="pane-slot"
          :style="isMobile ? undefined : { width: rightWidth + 'px' }">
          <RightSidebar
            class="slot-pane"
            :class="{ active: !settingsOpen }"
            :style="
              isMobile ? { width: 'calc(100% - 30%)' } : { flexBasis: rightWidth + 'px' }
            "
            :open="inspectorOpen"
            :visible="isMobile ? inspectorOpen : rightVisible && !settingsOpen" />

          <SettingsDrawer
            class="slot-pane"
            :class="{ active: settingsOpen }"
            :open="settingsOpen"
            v-model:show-thinking="showThinking"
            :theme-mode="themeMode"
            @set-theme="setTheme" />
        </div>
      </div>
    </div>

    <!-- frosted backdrop only on mobile - on desktop the settings drawer
         is the right sidebar swapped for settings content, no overlay -->
    <div
      v-if="isMobile && settingsOpen"
      class="scrim"
      @click="settingsOpen = false" />

    <StatusBar
      :healthy="healthy"
      :streaming="anyBusyNow"
      :message-count="messageCount"
      :tokens="tokens"
      :context-size="contextSize"
      :tokens-per-second="tokensPerSecond"
      :live-chunks-per-second="liveChunksPerSecond" />
  </div>
</template>

<script setup lang="ts">
  import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
  import {
    anyBusy,
    withBusy,
    pinSession,
    markSessionRead,
    parseSessionPath,
    sessionPath,
    isAtBottom,
    pillVisible,
    lastUserText,
    dropLastExchange,
    hasInterruptedMarker,
    withoutMarkers,
    createItemKeySource,
    StreamInterruptedError,
  } from "./composables/omega";
  import type { BusyMap } from "./composables/omega";

  interface MessageItem {
    kind: "message";
    role: "user" | "assistant";
    content: string;
    streaming?: boolean;
    // client-side timestamp for live messages, backend created_at for
    // restored history (ChatMessage renders it as a hover title)
    time?: string;
    // server-measured response duration (restored history)
    finalMs?: number;
  }
  interface ThinkingItem {
    kind: "thinking";
    content: string;
    streaming?: boolean;
    // server-measured thinking duration (restored history)
    finalMs?: number;
  }
  interface ToolItem {
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
  // inline "stream interrupted" marker: appears only when the SSE
  // transport died mid-run (never on a clean end). Carries the resume
  // affordance - see retryStream.
  interface InterruptedItem {
    kind: "interrupted";
  }
  type TranscriptItem =
    | MessageItem
    | ThinkingItem
    | ToolItem
    | InterruptedItem;

  const healthy = ref(false);
  const models = ref<string[]>([]);
  const selectedModel = ref("");
  const sessions = ref<Session[]>([]);
  const activeId = ref<string | null>(null);
  const draft = ref("");
  const messagesEl = ref<HTMLElement | null>(null);
  const drawerOpen = ref(false);
  const inspectorOpen = ref(false);
  const isMobile = ref(false);

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
  }
  const EMPTY_STATE: SessionState = {
    transcript: [],
    statusLine: "",
    tokens: 0,
    tokensPerSecond: 0,
    liveChunksPerSecond: 0,
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

  function syncURL(id: string | null, push: boolean) {
    if (typeof window === "undefined") return;
    const url = id ? sessionPath(id) : "/";
    if (push) history.pushState({}, "", url);
    else history.replaceState({}, "", url);
  }

  // back/forward walk session entries; switching on popstate is a pure
  // view change, so it never interrupts a stream
  function onPop() {
    fromHistory = true;
    const id = parseSessionPath(window.location.pathname);
    if (id) openSession(id);
    else newChat();
    fromHistory = false;
  }

  // per-session busy set: a session is busy iff its own stream is live.
  // Swapped value-semantically (withBusy) so set changes are reactive.
  const busySet = ref<BusyMap>(new Set());
  // global view concerns: read-only over the busy map, never a lock
  const anyBusyNow = computed(() => anyBusy(busySet.value));

  // the view reads its own session's state
  const transcript = computed(() => view.value.transcript);
  const statusLine = computed(() => view.value.statusLine);
  const tokens = computed(() => view.value.tokens);
  const tokensPerSecond = computed(() => view.value.tokensPerSecond);
  const liveChunksPerSecond = computed(() => view.value.liveChunksPerSecond);

  // keyed v-for: identity, not index (createItemKeySource) - see omega.ts
  const itemKey = createItemKeySource();

  // mobile: sidebars are overlays (slide from their side over a scrim);
  // desktop: same toggles mount/unmount the panes inline + squeeze chain
  // mobile: one drawer at a time - opening any pane closes the other two
  function toggleSidebar() {
    if (isMobile.value) {
      inspectorOpen.value = false;
      settingsOpen.value = false;
      drawerOpen.value = !drawerOpen.value;
    } else {
      sidebarVisible.value = !sidebarVisible.value;
    }
  }

  function closeOverlays() {
    drawerOpen.value = false;
    inspectorOpen.value = false;
    settingsOpen.value = false;
  }
  const settingsOpen = ref(false);
  const showThinking = ref(true);

  const sidebarWidth = ref(240);
  const rightWidth = ref(240);
  const dividerEl = ref<{ persist: (w: number) => void } | null>(null);
  const rightDividerEl = ref<{ persist: (w: number) => void } | null>(null);

  // desktop pane open-state persists across reloads (omega.sidebar-open /
  // omega.inspector-open, boolean strings, defaults true / false). Mobile
  // is deliberately NOT persisted: the drawers stay closed every load -
  // these flags are desktop-layout state only, guarded at the same 768px
  // breakpoint the overlay mode uses. The gear/settings drawer is a
  // headless toggle and stays non-persisted.
  function loadPaneFlag(key: string, fallback: boolean): boolean {
    if (typeof window === "undefined") return fallback;
    const raw = localStorage.getItem(key);
    if (raw === "true") return true;
    if (raw === "false") return false;
    return fallback;
  }
  const sidebarVisible = ref(loadPaneFlag("omega.sidebar-open", true));
  // first visit: the inspector starts closed (the pane width IS persisted,
  // via the divider's storage key)
  const rightVisible = ref(loadPaneFlag("omega.inspector-open", false));

  // persisted writes happen on toggle only, desktop only - the mobile
  // overlay toggles flip drawerOpen/inspectorOpen, these refs move on
  // desktop toggles and squeeze collapse (dividers are desktop-only).
  // The guard reads the LIVE matchMedia, not the isMobile ref: at a
  // breakpoint cross the squeeze chain can fire before the media-query
  // listener updates the ref, and by-ref would then persist a mobile
  // squeeze closure over the desktop flag.
  const MOBILE_MEDIA = "(max-width: 768px)";
  function isDesktopNow(): boolean {
    if (typeof window === "undefined") return false;
    return !window.matchMedia(MOBILE_MEDIA).matches;
  }
  watch(sidebarVisible, (v) => {
    if (isDesktopNow()) localStorage.setItem("omega.sidebar-open", String(v));
  });
  watch(rightVisible, (v) => {
    if (isDesktopNow()) localStorage.setItem("omega.inspector-open", String(v));
  });
  const contextSize = ref(0);

  const messageCount = computed(
    () => transcript.value.filter((i) => i.kind === "message").length,
  );

  // transcript models the live stream: user message, thinking blocks (one per
  // reasoning run), tool-call lines, assistant message. Restored sessions map
  // the persisted Assistant{Thinking, ToolCalls, Content} back into the same
  // three shapes so a reload renders everything that happened.
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

  async function openSession(id: string) {
    if (!fromHistory) syncURL(id, true);
    activeId.value = id;
    drawerOpen.value = false;
    inspectorOpen.value = false;
    markRead(id);
    const st = ensureState(id);
    // a live stream IS the content - refetching now would clobber the
    // stream state with a mid-flight persisted snapshot
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
      const detail = await fetchSession(id);
      if (detail.messages) {
        const items: TranscriptItem[] = [];
        const toolById = new Map<string, ToolItem>();
        for (const m of detail.messages) {
          if (m.role === "tool") continue; // consumed by the result pass below
          if (!m.content && !m.thinking && !m.tool_calls?.length) continue;
          const role = (m.role === "assistant" ? "assistant" : "user") as
            | "user"
            | "assistant";
          if (role === "assistant" && m.thinking) {
            items.push({
              kind: "thinking",
              content: m.thinking,
              finalMs: m.timing?.thinking_ms,
            });
          }
          if (m.content) {
            items.push({
              kind: "message",
              role,
              content: m.content,
              time: m.created_at || m.timestamp || undefined,
              finalMs: m.timing?.response_ms,
            });
          }
          if (role === "assistant" && m.tool_calls?.length) {
            for (const tc of m.tool_calls) {
              const t: ToolItem = {
                kind: "tool",
                name: tc.name || "tool",
                status: "done",
                output:
                  tc.arguments ?
                    JSON.stringify(tc.arguments, null, 2)
                  : undefined,
              };
              if (tc.id) toolById.set(tc.id, t);
              items.push(t);
            }
          }
        }
        // second pass: tool-role messages carry the persisted results,
        // matched to their call by tool_call_id
        for (const m of detail.messages) {
          if (m.role !== "tool" || !m.tool_call_id) continue;
          const t = toolById.get(m.tool_call_id);
          if (!t) continue;
          t.result = m.content || "";
          t.status = m.is_error ? "error" : "done";
          t.finalMs = m.timing?.elapsed_ms;
        }
        st.transcript = items;
        if (hadInterrupted) st.transcript.push({ kind: "interrupted" });
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
    // fresh unsaved-chat state - UNLESS an unsaved stream is live, in
    // which case +new returns to the view watching it (no global lock)
    if (!busySet.value.has(NEW_KEY)) streams.delete(NEW_KEY);
    ensureState(NEW_KEY);
    drawerOpen.value = false;
  }

  // opening the inspector is the ONE moment the right sidebar may push back:
  // if the left sidebar grew so wide the inspector auto-collapsed, reopening
  // shrinks the left sidebar just enough to fit inspector + chat floor.
  function toggleInspector() {
    // mobile: same slot rule - free it first
    if (isMobile.value) {
      if (settingsOpen.value) settingsOpen.value = false;
      drawerOpen.value = false;
      inspectorOpen.value = !inspectorOpen.value;
      return;
    }
    // desktop: the right group hosts one pane - if settings is open, the
    // inspector toggle CLOSES settings and explicitly OPENS the inspector
    if (settingsOpen.value) {
      settingsOpen.value = false;
      rightVisible.value = true;
      return;
    }
    if (rightVisible.value) {
      rightVisible.value = false;
      return;
    }
    pushBackLeft();
    rightVisible.value = true;
  }

  // shared by the inspector reopen and openSettings: shrink the left
  // sidebar to its floor when it grew too wide for the right group to fit
  function pushBackLeft() {
    if (typeof window === "undefined") return;
    const area = document.querySelector(".pane-area");
    if (!area) return;
    const available = area.clientWidth - 8;
    const needed = sidebarWidth.value + 320 + 128 + 8;
    if (available < needed) {
      sidebarWidth.value = Math.max(128, available - 320 - 128 - 8);
      dividerEl.value?.persist(sidebarWidth.value);
    }
  }

  // gear is a toggle; the right group hosts one pane - opening settings
  // first closes the inspector if open, then opens settings (and vice
  // versa in toggleInspector). No interaction with the left sidebar.
  function openSettings() {
    if (settingsOpen.value) {
      settingsOpen.value = false;
      return;
    }
    if (isMobile.value) {
      // mobile: one drawer at a time - close both other drawers
      inspectorOpen.value = false;
      drawerOpen.value = false;
    } else {
      // desktop: same push-back as the inspector reopen - shrink the left
      // sidebar if it grew too wide for the right group to fit
      rightVisible.value = false;
      pushBackLeft();
    }
    settingsOpen.value = true;
  }

  function onResize(w: number) {
    w = Math.max(128, w); // floor: sub-min values (incl. stale localStorage) dead-lock the divider
    sidebarWidth.value = w;
    // squeeze the RIGHT first (shrink/close), then cap against the
    // post-squeeze room - mirrors onRightResize so both dividers behave
    // identically
    squeezeCheck();
    const cap = dragCap(
      rightVisible.value || settingsOpen.value ? rightWidth.value : 0,
    );
    if (cap >= 128 && sidebarWidth.value > cap) sidebarWidth.value = cap;
    dividerEl.value?.persist(sidebarWidth.value);
  }

  // JS width-wall: the CSS group cap alone lets an oversized inline width
  // overflow the group (content sliding out of bounds). Clamping the ref
  // here means the stored/dragged width can never exceed the chat floor.
  function dragCap(otherWidth: number): number {
    if (typeof window === "undefined") return Infinity;
    const area = document.querySelector(".pane-area");
    if (!area) return Infinity;
    return area.clientWidth - 8 - otherWidth - 320 - 8; // dividers + chat floor
  }

  function onRightResize(w: number) {
    w = Math.max(128, w); // floor: sub-min values (incl. stale localStorage) dead-lock the divider
    rightWidth.value = w;
    // squeeze the LEFT first - it shrinks and eventually closes, freeing
    // space; only THEN cap against the post-squeeze room so the chain
    // stays draggable (drag wide -> left closes -> right grows to the wall)
    squeezeCheckLeft();
    const cap = dragCap(sidebarVisible.value ? sidebarWidth.value : 0);
    if (cap >= 128 && rightWidth.value > cap) rightWidth.value = cap;
    rightDividerEl.value?.persist(rightWidth.value);
  }

  // squeeze below the min floor closes whichever pane is visible in the
  // right slot - settings or the inspector
  function onRightCollapsed() {
    if (settingsOpen.value) settingsOpen.value = false;
    else rightVisible.value = false;
  }

  // mirrored squeeze: dragging the right divider wide starves the left
  // sidebar first - it shrinks to fit, and closes below its floor, exactly
  // like the right sidebar does when the left divider over-drags it.
  function squeezeCheckLeft() {
    if (!sidebarVisible.value || typeof window === "undefined") return;
    const area = document.querySelector(".pane-area");
    if (!area) return;
    const available = area.clientWidth - 8;
    const room = available - 320 - rightWidth.value - 8; // chat floor + dividers
    if (room < 128) {
      sidebarVisible.value = false;
      return;
    }
    if (sidebarWidth.value > room) {
      sidebarWidth.value = room;
      dividerEl.value?.persist(room);
    }
  }

  // squeeze chain (mirror of squeezeCheckLeft): dragging the left sidebar
  // wide starves the right first - it SHRINKS to the remaining room, and
  // closes below its floor. Runs on left-drag and window resize.
  function squeezeCheck() {
    const rightActive = rightVisible.value || settingsOpen.value;
    if (!rightActive || typeof window === "undefined") return;
    const area = document.querySelector(".pane-area");
    if (!area) return;
    const available = area.clientWidth - 8;
    const room = available - 320 - sidebarWidth.value - 8; // chat floor + dividers
    if (room < 128) {
      // close whichever pane is visible in the right slot
      if (settingsOpen.value) settingsOpen.value = false;
      else rightVisible.value = false;
      return;
    }
    if (rightWidth.value > room) {
      rightWidth.value = room;
      rightDividerEl.value?.persist(room);
    }
  }

  function send() {
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
    if (busySet.value.has(startKey)) return;
    const st = ensureState(startKey);
    // a fresh send retires a stale resume affordance - after a later
    // exchange the marker no longer describes the transcript state
    if (hasInterruptedMarker(st.transcript))
      st.transcript = withoutMarkers(st.transcript);
    // unsaved chats live under NEW_KEY until /sessions returns the real
    // id; state AND busy entry re-key to the id right after creation
    let busyKey = startKey;
    busySet.value = withBusy(busySet.value, busyKey, true);
    st.statusLine = "reasoning";

    st.transcript.push({
      kind: "message",
      role: "user",
      content: text,
      time: new Date().toISOString(),
    });
    scrollDown(true);
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
    // transport-death flag: an interrupted run with NO partial output does
    // not get an empty ghost bubble - the marker alone carries the state
    let interrupted = false;
    const sentAt = Date.now();
    // live stream telemetry: per-chunk arrivals make the statusbar a
    // visible proof that the frontend receives chunks as they come -
    // a ticking counter means streaming, a frozen-then-jumping one
    // means something buffers between omega and the page.
    st.tokens = 0;
    st.liveChunksPerSecond = 0;
    const chunkTimes: number[] = [];
    // the stream writes only its own session's state; auto-scroll follows
    // it only while the view is actually watching this stream - a stream
    // running in the background never yanks an open session's scrollbar
    const isViewed = () => streams.get(activeKey.value) === st;
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

    try {
      // A session must exist before the first /chat or the backend 404s.
      if (startKey === NEW_KEY) {
        const r = await fetch("/sessions", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: "{}",
        });
        if (!r.ok) throw new Error(`create session: ${r.status}`);
        const s = await r.json();
        streams.delete(NEW_KEY);
        streams.set(s.id, st);
        busySet.value = withBusy(withBusy(busySet.value, busyKey, false), s.id, true);
        busyKey = s.id;
        // the row (and its pulse) exists once the sidebar refreshes; the
        // view moves to the session only if the user stayed on this chat
        refreshSessions();
        if (activeKey.value === NEW_KEY) {
          activeId.value = s.id;
          // the temp id never lands in history: the entry it replaces is
          // home, so back from the real session returns home
          syncURL(s.id, false);
        }
      }
      const payload: OmegaMessage[] = [{ role: "user", content: text }];
      await streamChat(
        payload,
        busyKey,
        {
          onThinking: (content) => {
            if (!thinking) {
              thinking = { kind: "thinking", content: "", streaming: true };
              st.transcript.push(thinking);
            }
            thinking.content += content;
            onChunk(content.length);
            if (isViewed()) scrollDown();
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
            pushAssistant();
            assistant.content += content;
            onChunk(content.length);
            if (isViewed()) scrollDown();
          },
          onToolCall: (tool) => {
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
            st.statusLine = s;
          },
          onUsage: (usage) => {
            st.tokens = usage.promptTokens + usage.completionTokens;
            const seconds = (Date.now() - sentAt) / 1000;
            st.tokensPerSecond =
              seconds > 0.2 ? Math.round(usage.completionTokens / seconds) : 0;
          },
        },
        selectedModel.value || undefined,
      );
      assistant.streaming = false;
    } catch (e) {
      if (e instanceof StreamInterruptedError) {
        // transport death mid-stream: the partial bubble stays exactly as
        // it is (finally finalizes its streaming flag) - the marker carries
        // the resume affordance instead of appending error text to it
        interrupted = true;
        st.transcript.push({ kind: "interrupted" });
      } else {
        assistant.content +=
          assistant.content ? `\n[error: ${e}]` : `[error: ${e}]`;
      }
      assistant.streaming = false;
    } finally {
      st.liveChunksPerSecond = 0;
      // no more events are coming: a tool call still "running" never
      // receives its result - freeze its elapsed meter now (ToolCallLine
      // stops ticking) without touching the honest running status
      for (const item of st.transcript) {
        if (item.kind === "tool" && item.status === "running")
          item.interrupted = true;
      }
      // thinking is assigned inside stream callbacks the type system can't
      // track across the closure - cast to the item type it is at runtime
      const liveThinking = thinking as ThinkingItem | null;
      if (liveThinking?.streaming) liveThinking.streaming = false;
      // stream failed before any response chunk: make sure the (error)
      // bubble made it into the transcript at all - EXCEPT an interrupted
      // run with no output: it keeps only the marker, no empty bubble
      if (!interrupted || assistant.content) pushAssistant();
      assistant.streaming = false;
      busySet.value = withBusy(busySet.value, busyKey, false);
      st.statusLine = "";
      // A stream ending on the VIEWED session never unreads itself -
      // the user watched the content live, so it is marked read. A
      // background stream (session B while viewing A) leaves its row
      // unread: that is the feature.
      if (activeId.value === busyKey) markRead(busyKey);
      refreshSessions();
      // the title goroutine writes the session label seconds AFTER the
      // first exchange ends - the refresh above runs too early to see
      // it. One delayed refresh picks it up without a manual reload.
      const row = sessions.value.find(s => s.id === busyKey);
      if (!row || !row.label) {
        setTimeout(() => { refreshSessions(); }, 10000);
      }
      if (isViewed()) scrollDown();
    }
  }

  async function refreshSessions() {
    try {
      sessions.value = await fetchSessions();
    } catch {
      // sidebar refresh is best-effort
    }
  }

  // resume replays the interrupted run: the transcript tail the failed
  // run produced (partial bubble, thinking, tool lines, marker) is
  // dropped, and the message that kicked it off goes out again through
  // the normal send path - a clean replacement response, NOT a
  // continuation of the severed stream. Reads and writes only THIS
  // session's state, so other sessions' transcripts and busy flags are
  // untouched.
  function retryStream() {
    const key = activeKey.value;
    const st = ensureState(key);
    if (busySet.value.has(key)) return;
    const text = lastUserText(st.transcript);
    if (text === null) return;
    st.transcript = dropLastExchange(st.transcript);
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

    // squeeze check on window resize (left drag checks in onResize)
    window.addEventListener("resize", squeezeCheck);

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

    healthy.value = await fetchHealth();
    try {
      const m = await fetchModels();
      models.value = m.models;
      selectedModel.value = m.current;
      if (m.context_window) contextSize.value = m.context_window;
    } catch {
      // models are decoration until a chat runs
    }
    refreshSessions();

    // restore the session named by the URL, if it exists - an unknown or
    // deleted id routes home (and the URL comes with it)
    const initial = parseSessionPath(window.location.pathname);
    if (initial) {
      try {
        await fetchSession(initial);
        fromHistory = true;
        await openSession(initial);
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
    messagesEl.value?.removeEventListener("scroll", onMessagesScroll);
    window.removeEventListener("resize", squeezeCheck);
    if (spinnerTimer) clearInterval(spinnerTimer);
    if (typeof document !== "undefined" && savedTitle !== null)
      document.title = savedTitle;
  });
</script>
