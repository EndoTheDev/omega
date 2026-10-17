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
          v-if="!isMobile"
          ref="dividerEl"
          :width="sidebarWidth"
          :min="128"
          storage-key="omega.sidebar-width"
          @update:width="onResize"
          @collapsed="sidebarVisible = false" />
      </div>

      <main class="chat pane">
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
            :key="i">
            <ChatMessage
              v-if="item.kind === 'message'"
              :role="item.role"
              :content="item.content"
              :streaming="item.streaming" />
            <ThinkingBlock
              v-else-if="item.kind === 'thinking' && showThinking"
              :content="item.content"
              :streaming="item.streaming" />
            <ToolCallLine
              v-else-if="item.kind === 'tool'"
              :name="item.name"
              :status="item.status"
              :output="item.output"
              :result="item.result" />
          </template>
          <div
            v-if="statusLine"
            class="status-line">
            {{ statusLine }}…
          </div>
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
          v-if="!isMobile"
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
            :open="inspectorOpen" />

          <SettingsDrawer
            class="slot-pane"
            :class="{ active: settingsOpen }"
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
    parseSessionPath,
    sessionPath,
  } from "./composables/omega";
  import type { BusyMap } from "./composables/omega";

  interface MessageItem {
    kind: "message";
    role: "user" | "assistant";
    content: string;
    streaming?: boolean;
  }
  interface ThinkingItem {
    kind: "thinking";
    content: string;
    streaming?: boolean;
  }
  interface ToolItem {
    kind: "tool";
    name: string;
    status: string;
    output?: string;
    result?: string;
  }
  type TranscriptItem = MessageItem | ThinkingItem | ToolItem;

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
  const sidebarVisible = ref(true);
  const rightVisible = ref(true);
  const contextSize = ref(0);

  const messageCount = computed(
    () => transcript.value.filter((i) => i.kind === "message").length,
  );

  // transcript models the live stream: user message, thinking blocks (one per
  // reasoning run), tool-call lines, assistant message. Restored sessions map
  // the persisted Assistant{Thinking, ToolCalls, Content} back into the same
  // three shapes so a reload renders everything that happened.
  async function openSession(id: string) {
    if (!fromHistory) syncURL(id, true);
    activeId.value = id;
    drawerOpen.value = false;
    inspectorOpen.value = false;
    const st = ensureState(id);
    // a live stream IS the content - refetching now would clobber the
    // stream state with a mid-flight persisted snapshot
    if (busySet.value.has(id)) {
      scrollDown(true);
      return;
    }
    st.statusLine = "";
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
            items.push({ kind: "thinking", content: m.thinking });
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
          if (m.content) {
            items.push({ kind: "message", role, content: m.content });
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
        }
        st.transcript = items;
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

  async function send() {
    const text = draft.value.trim();
    if (!text) return;
    const startKey = activeKey.value;
    // per-session guard: only THIS session's live stream blocks a send
    if (busySet.value.has(startKey)) return;
    draft.value = "";
    const st = ensureState(startKey);
    // unsaved chats live under NEW_KEY until /sessions returns the real
    // id; state AND busy entry re-key to the id right after creation
    let busyKey = startKey;
    busySet.value = withBusy(busySet.value, busyKey, true);
    st.statusLine = "reasoning";

    st.transcript.push({ kind: "message", role: "user", content: text });
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
        st.transcript.push(assistant);
        assistantPushed = true;
      }
    };
    // Tool phases split the turn into response segments: each text after a
    // tool phase is its own bubble, so the timeline reads chronologically
    // (preamble -> tools -> final answer).
    let segmentBreak = false;
    let thinking: ThinkingItem | null = null;
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
      while (chunkTimes.length && now - chunkTimes[0] > 1000) chunkTimes.shift();
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
      assistant.content +=
        assistant.content ? `\n[error: ${e}]` : `[error: ${e}]`;
      assistant.streaming = false;
    } finally {
      st.liveChunksPerSecond = 0;
      // thinking is assigned inside stream callbacks the type system can't
      // track across the closure - cast to the item type it is at runtime
      const liveThinking = thinking as ThinkingItem | null;
      if (liveThinking?.streaming) liveThinking.streaming = false;
      // stream failed before any response chunk: make sure the (error) bubble
      // made it into the transcript at all
      pushAssistant();
      assistant.streaming = false;
      busySet.value = withBusy(busySet.value, busyKey, false);
      st.statusLine = "";
      refreshSessions();
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

  function isAtBottom(): boolean {
    const el = messagesEl.value;
    if (!el) return true;
    return el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  }

  function onMessagesScroll() {
    pinned.value = isAtBottom();
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
