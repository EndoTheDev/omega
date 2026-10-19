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
         backdrop-filter frost renders against the shell's root, so the
         mobile drawers slide under identical frost -->
    <div
      v-if="isMobile && (drawerOpen || inspectorOpen)"
      class="scrim"
      @click="closeOverlays" />

    <div class="pane-area">
      <!-- dock: THE layout tree (card t_63253264). Recursive split/pane
           components render omega.dock-layout; the named region templates
           below flow down and surface inside whichever pane hosts them.
           The chat region is MULTI-INSTANCE (card t_4f0e88f2): every chat
           tab binds a session and renders its own ChatTab keyed by the
           tab's instance key; the same session may live in two tabs
           (settled ruling) and both subscribe to the run's events. -->
      <DockSplit :node="dock.tree.value">
        <template #pane-sidebar>
          <AppSidebar
            class="pane"
            :style="isMobile ? { width: 'calc(100% - 30%)' } : undefined"
            :sessions="sessions"
            :active-id="focusedSessionId"
            :streaming-ids="busyAll"
            :open="drawerOpen"
            :show-archived="showArchived"
            @open="openChatTab"
            @new="openChatTab(null)"
            @pin="togglePin"
            @archive="toggleArchive"
            @remove="askDelete"
            @toggle-archived="showArchived = $event" />
        </template>

        <!-- one ChatTab per chat tab; keyed by the tab's instance key so
             a DnD move (same pane reorder / cross-pane) remounts cleanly
             and tab switching never renders another tab's transcript.
             The module-owned board main-view (kanban) folds into the
             chat pane template per the dock 1/5 merge ruling: chat
             hides, the board shows, in the ACTIVE chat pane only -->
        <template #pane-chat="{ pane, tab }">
          <div :style="{ display: mainView === 'chat' ? 'contents' : 'none' }">
            <ChatTab
              :key="tab.key ?? pane"
              :tab="tab"
              :show-thinking="showThinking"
              :model="selectedModel"
              :models="models"
              :thinking="selectedThinking"
              :appearance="appearance"
              @update:model="selectedModel = $event"
              @update:thinking="selectedThinking = $event" />
          </div>
          <div
            v-if="mainView === 'board' && pane === boardHostPane"
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
        </template>

        <template #pane-inspector>
          <!-- mobile: the slide-in overlay; desktop: a real dock pane.
               `visible` stays the root-listing fetch trigger (drawer flag
               on mobile, always true in the dock tab). -->
          <RightSidebar
            class="pane"
            :style="isMobile ? { width: 'calc(100% - 30%)' } : undefined"
            :open="inspectorOpen"
            :visible="isMobile ? inspectorOpen : true" />
        </template>

        <!-- card 5 (t_35b5594a): settings is a dock-tab singleton - the
             menubar gear opens it into the focused main pane; the overlay
             drawer shape is gone. Same component, in-pane. -->
        <template #pane-settings>
          <SettingsTab
            class="pane"
            v-model:show-thinking="showThinking"
            v-model:appearance="appearance"
            :theme-mode="themeMode"
            :user-themes="userThemes"
            @set-theme="setTheme" />
        </template>
      </DockSplit>
    </div>

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
  import { computed, onMounted, onUnmounted, provide, ref, watch } from "vue";
  import { modulePanels } from "./composables/module-panels.generated";
  import {
    archiveSession,
    deleteSession,
    fetchHealthInfo,
    fetchModels,
    fetchTranscript,
    fetchSessions,
    markSessionRead,
    parseSessionPath,
    sessionPath,
    pinSession,
    parseAppearance,
    APPEARANCE_KEY,
    isAtBottom,
    pillVisible,
    lastUserText,
    dropLastExchange,
    hasInterruptedMarker,
    withoutMarkers,
    createItemKeySource,
    streamChat,
    attachSession,
    fetchHealthInfo,
    persistedTelemetry,
    formatDurationMs,
    StreamInterruptedError,
    fetchThemes,
    applyThemeTokens,
    clearThemeTokens,
    parseThemeSelect,
    type UserTheme,
    type ThemeMode,
  } from "./composables/omega";
  import type { Session, Appearance } from "./composables/omega";
  import {
    allNodes,
    createDock,
    findPane,
    isPane,
    DOCK_API_KEY,
    DOCK_CHAT_OPEN_KEY,
    DOCK_TAB_TITLE_KEY,
    nextTabKey,
  } from "./composables/dock";
  import {
    CHAT_HOST_KEY,
    DEFAULT_CHAT_STATE,
    createChatHost,
    type ChatState,
  } from "./composables/chat";
  import ChatTab from "./components/ChatTab.vue";
  import SettingsTab from "./components/settings/SettingsTab.vue";
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
  // reasoning effort: one app-level selection shared by every chat tab.
  // Persisted in localStorage (last choice wins across reloads, card
  // t_2b474945); "default" = no thinking_level field on the wire.
  const selectedThinking = ref(
    localStorage.getItem("omega.thinking-level") ?? "default",
  );
  watch(selectedThinking, (v) => {
    if (v === "default") localStorage.removeItem("omega.thinking-level");
    else localStorage.setItem("omega.thinking-level", v);
  });
  const sessions = ref<Session[]>([]);
  // archived filter: off (default) lists live sessions only; on fetches
  // with archived=include and shows only - muted - archived rows
  const showArchived = ref(false);
  const drawerOpen = ref(false);
  const inspectorOpen = ref(false);
  const isMobile = ref(false);
  const showThinking = ref(true);
  // chat appearance (card t_84e4653c): 'chat' chronolog | 'stacked'
  // blocks. Persisted like the theme - last choice wins across reloads,
  // garbage restores the default.
  const appearance = ref<Appearance>("chat");
  // the knob flip re-renders instantly; the change persists for the
  // next reload (last choice wins, same pattern as the theme)
  watch(appearance, (a) => {
    localStorage.setItem(APPEARANCE_KEY, a);
  });
  const contextSize = ref(0);

  // --- dock store (card t_63253264): the layout tree is the single
  // source of layout truth. Loading seeds from omega.dock-layout or
  // migrates the pre-dock pane keys once; every mutation persists the
  // whole tree. The components resolve it via injection. ---
  const dock = createDock(window.localStorage);
  provide(DOCK_API_KEY, dock);
  // MenuBar flags: the drawers (mobile) and the tree (desktop). The
  // gear is not a toggle flag (card 5): it OPENS the settings singleton
  // tab, and a second click FOCUSES the live instance wherever it sits.
  const sidebarPaneOpen = computed(() =>
    isMobile.value ? drawerOpen.value : dock.has("sidebar"),
  );
  const inspectorPaneOpen = computed(() =>
    isMobile.value ? inspectorOpen.value : dock.has("inspector"),
  );

  // --- the chat host (card t_4f0e88f2): every chat tab gets its own
  // stream state keyed by the tab's instance key; the gate stays per
  // session (the runHub run slot is per session). ---
  const host = createChatHost({
    setTabSessionRef: (key, sessionId) => dock.setSessionRef(key, sessionId),
    refreshSessions: () => refreshSessions(),
    sessionRow: (id) => sessions.value.find((s) => s.id === id),
    markRead: (id) => markRead(id),
    isFocused: (key) => focusedKey.value === key,
  });

  // --- main-view: chat vs the module-owned board (kanban). Ported
  // from the pre-dock shell per the dock 1/5 merge ruling: the board
  // main-view folds into the chat pane template. Under the dock-era
  // URL ruling (replaceState only, the URL owns the focused chat),
  // /board is a replaceState overlay - the urlPath watcher skips while
  // the board is up and chat reclaims the URL on the way back.
  // ponytail: board renders in ONE pane (the active chat pane); the
  // dockable-contract card gives board its own component slot later.
  const mainView = ref<"chat" | "board">("chat");
  const boardComponent = computed(
    () => modulePanels.find((p) => p.module === "kanban")?.component,
  );
  const boardHostPane = ref<string>("");

  function switchMainView(v: "chat" | "board") {
    if (mainView.value === v) return;
    mainView.value = v;
    if (v === "board") {
      boardHostPane.value = resolveActivePaneId();
      history.replaceState({}, "", "/board");
    } else {
      history.replaceState({}, "", urlPath.value);
    }
  }

  // a board card's transcript chip lands the user in that conversation
  function openBoardSession(id: string) {
    mainView.value = "chat";
    openChatTab(id);
  }
  provide(CHAT_HOST_KEY, host);

  // chat tab titles read the session labels; the sidebar highlight
  // follows the FOCUSED tab - the bar label has to show it
  provide(DOCK_TAB_TITLE_KEY, (tab) => {
    if (tab.component !== "chat") return null;
    if (!tab.sessionRef) return "new chat";
    const s = sessions.value.find((x) => x.id === tab.sessionRef);
    return s?.label || "chat";
  });

  // chat is multi-instance: the '+' / empty-pane "new chat" MUST go
  // through the opener (a minted key + openTab), never the raw
  // setComponent - a keyless chat tab would share one state across
  // every keyless tab in the page
  provide(DOCK_CHAT_OPEN_KEY, (paneId, sessionId) => openChatTab(sessionId, paneId));

  // --- focus + URL (card t_4f0e88f2 rulings) ---
  // The URL owns ONLY the focused chat session: focusing a chat tab
  // replaceStates /chat/<id> (unsaved = /); focus moving to a non-chat
  // tab or pane leaves the URL at the last focused chat session; layout
  // and tab state NEVER enter the URL. No pushState - the app never
  // grows the history stack (the settled ruling), so popstate only
  // fires for entries outside the SPA and re-targets via the URL's id.
  const focusedKey = ref<string | null>(null);
  // the pane a NEW chat tab opens into: the last pane whose active tab
  // was focused as a chat (a sidebar/bucket click never steals opens -
  // it is not a chat focus)
  const activeChatPaneId = ref<string | null>(null);

  const focusedChatState = computed<ChatState>(() =>
    focusedKey.value ? host.states.get(focusedKey.value) ?? DEFAULT_CHAT_STATE : DEFAULT_CHAT_STATE,
  );
  // the sidebar's active-session highlight follows the focused chat tab
  const focusedSessionId = computed(() => focusedChatState.value.sessionId);

  // status bar telemetry reads the focused chat's state (zeros until a
  // session carries numbers - DEFAULT_CHAT_STATE fallback)
  const transcript = computed(() => focusedChatState.value.transcript);
  const tokens = computed(() => focusedChatState.value.tokens);
  const tokensPerSecond = computed(() => focusedChatState.value.tokensPerSecond);
  const liveChunksPerSecond = computed(() => focusedChatState.value.liveChunksPerSecond);
  const lastChunksPerSecond = computed(() => focusedChatState.value.lastChunksPerSecond);
  const messageCount = computed(
    () => transcript.value.filter((i) => i.kind === "message").length,
  );
  const anyBusyNow = host.anyBusyNow;
  const busyAll = host.busyAll;

  function refocusFrom(paneId: string | null) {
    const tree = dock.tree.value;
    if (paneId) {
      const pane = findPane(tree, paneId);
      const tab = pane?.tabs[pane.active];
      if (tab?.component === "chat" && tab.key) {
        activeChatPaneId.value = paneId;
        focusedKey.value = tab.key;
        return;
      }
    }
    // no chat focus in this gesture: keep a still-valid focus, else the
    // first chat tab in the tree, else none (URL home)
    if (focusedKey.value && dock.findChatTab(focusedKey.value)) return;
    const first = dock.firstChatTab();
    focusedKey.value = first?.tab.key ?? null;
  }

  // clicks carry the pane focus: the document-level listener reads the
  // dock-pane id (DnD moves, menus and composer clicks all bubble here)
  // and remembers the LAST FOCUSED pane - the gear's settings tab opens
  // into that pane (card 5's "focused main pane" target).
  const lastPaneId = ref<string | null>(null);
  function onDocClick(e: MouseEvent) {
    const el = e.target instanceof Element ?
      e.target.closest("[data-pane-id]") :
      null;
    const paneId = el?.getAttribute("data-pane-id") ?? null;
    if (paneId) lastPaneId.value = paneId;
    refocusFrom(paneId);
  }

  // the pane a singleton open targets when no click has landed yet: the
  // first pane in the tree (depth-first, same walk openSingleton uses)
  function resolveFocusedPaneId(): string {
    const tree = dock.tree.value;
    const focused = lastPaneId.value ?
      findPane(tree, lastPaneId.value) :
      null;
    if (focused) return focused.id;
    for (const n of allNodes(tree)) {
      if (isPane(n)) return n.id;
    }
    return ""; // unreachable: the tree always contains at least one pane
  }

  function resolveActivePaneId(): string {
    const tree = dock.tree.value;
    const focused = activeChatPaneId.value ?
      findPane(tree, activeChatPaneId.value) :
      null;
    if (focused && focused.tabs.some((t) => t.component === "chat")) return focused.id;
    // not focused on a chat-hosting pane: the first pane that hosts one,
    // else the first pane in the tree (never auto-splits for the user)
    for (const n of allNodes(tree)) {
      if (isPane(n) && n.tabs.some((t) => t.component === "chat")) return n.id;
    }
    for (const n of allNodes(tree)) {
      if (isPane(n)) return n.id;
    }
    return ""; // unreachable: the tree always contains at least one pane
  }

  // openChatTab is THE tab-opener: the sidebar's row click (session id)
  // and the '+' flows (null = unsaved composer-first) go through it.
  // Always a NEW tab - the settled ruling allows the same session in
  // two tabs; the sidebar's highlight follows the focused tab.
  function openChatTab(sessionId: string | null, paneId?: string) {
    const pane = paneId ?? resolveActivePaneId();
    if (!pane) return;
    const key = nextTabKey();
    dock.openTab(pane, {
      component: "chat",
      ...(sessionId ? { sessionRef: sessionId } : {}),
      key,
    });
    focusedKey.value = key;
    if (isMobile.value) drawerOpen.value = false;
    // open == read is owned by the ChatTab mount's load(); a newly
    // opened tab mounts and restores immediately
  }

  // popstate re-targets to the URL's session: an open-by-session tab is
  // focused (and read), otherwise a new chat tab opens in the active
  // pane - the same flow a reload of /chat/<id> takes
  async function onPop() {
    // /board re-enters the kanban view without touching chat focus
    if (/^\/board\/?$/.test(window.location.pathname)) {
      mainView.value = "board";
      boardHostPane.value = resolveActivePaneId();
      return;
    }
    mainView.value = "chat";
    const id = parseSessionPath(window.location.pathname);
    if (id) {
      const found = dock.firstChatTab(id);
      if (found?.tab.key) {
        dock.focus(found.paneId, found.index);
        focusedKey.value = found.tab.key;
        markRead(id);
        return;
      }
      if (await sessionExists(id)) {
        openChatTab(id);
        markRead(id);
      } else {
        history.replaceState({}, "", "/");
      }
      return;
    }
    // home: an existing chat tab takes the focus; with none open,
    // composer-first in the active pane (today's no-session behavior)
    const first = dock.firstChatTab();
    if (first?.tab.key) {
      dock.focus(first.paneId, first.index);
      focusedKey.value = first.tab.key;
    } else {
      openChatTab(null);
    }
  }

  // URL follows focus by derivation: the focused tab's bound session
  // (unsaved = /). The states map re-fires this when the unsaved tab's
  // first send lands its real id.
  const urlPath = computed(() => {
    const id = focusedChatState.value.sessionId;
    return id ? sessionPath(id) : "/";
  });
  watch(urlPath, (p) => {
    // /board is an overlay the URL owns while it is up - the focused
    // chat's path reclaims the URL on the way back to chat
    if (mainView.value === "board") return;
    history.replaceState({}, "", p);
  });
  // structural changes never steal focus, but a gone focus re-resolves
  // (closeTab's neighbor-focus, DnD moves, toggle remove)
  watch(dock.tree, () => refocusFrom(null));

  // per-session stream busy set: a session is busy iff its own stream
  // is live (host.busyAll). Swapped value-semantically so set changes
  // are reactive.
  function markRead(id: string) {
    sessions.value = sessions.value.map((s) =>
      s.id === id ? { ...s, unread: false } : s,
    );
    markSessionRead(id).catch(() => refreshSessions());
  }

  async function refreshSessions() {
    try {
      const list = await fetchSessions(showArchived.value);
      sessions.value = list;
      // server-reported live runs feed the host: they REPLACE (not
      // union into) the snapshot - the sessions a JUST-refreshed page
      // sees as pulsing; local streams carry their own busy entries
      host.noteServerActive(list.filter((s) => s.running).map((s) => s.id));
    } catch {
      // sidebar refresh is best-effort
    }
  }

  // mobile: one drawer at a time - opening any pane closes the other
  function toggleSidebar() {
    if (isMobile.value) {
      // the drawer needs its pane rendered (empty panes are hidden by CSS):
      // absent -> insert it first, then flip the overlay open
      dock.ensureEdge("sidebar", "before");
      inspectorOpen.value = false;
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
  }

  // mobile: one drawer at a time
  function toggleInspector() {
    if (isMobile.value) {
      dock.ensureEdge("inspector", "after"); // overlay needs its pane present
      drawerOpen.value = false;
      inspectorOpen.value = !inspectorOpen.value;
      return;
    }
    // desktop: the TREE owns presence - the store remembers the width and
    // reopen-clamps
    dock.toggleEdge("inspector", "after");
  }

  // card 5 (t_35b5594a): the menubar gear is the settings singleton's
  // opener - NOT a toggle. First click OPENS a settings tab in the
  // focused main pane (the last pane clicked, else the tree's first
  // pane); every later click FOCUSES the existing instance wherever it
  // was dragged to (openSingleton's contract). Closing is the tab's ×.
  function openSettings() {
    dock.openSingleton(resolveFocusedPaneId(), "settings");
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
  // the only door to the API call. Deleting a session closes EVERY chat
  // tab bound to it (docked anywhere) and their stream states.
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
    dock.closeSession(id);
    host.closeSession(id);
    refreshSessions();
  }

  // sessionExists gates the boot/popstate restore: an unknown or deleted
  // id routes home instead of opening a tab on it (the transcript
  // endpoint doubles as the existence probe - restore is transcript-owned)
  async function sessionExists(id: string): Promise<boolean> {
    try {
      await fetchTranscript(id);
      return true;
    } catch {
      return false;
    }
  }

  // toggling the archived filter refetches through the include filter -
  // the two archive sides never mix in one list
  watch(showArchived, () => refreshSessions());

  // --- theme: light / dark / auto / user themes, persisted, auto
  // follows the OS live. A user theme (theme:<id> from GET /themes)
  // recolors live by writing its token overrides into the inline style;
  // the built-in modes undo that (clear overrides, data-theme only). ---
  const themeMode = ref<ThemeMode>("auto");
  let systemPrefersDark = true;
  let mediaQuery: MediaQueryList | null = null;
  const userThemes = ref<UserTheme[]>([]);

  function applyTheme() {
    if (typeof document === "undefined") return;
    const userId = parseThemeSelect(themeMode.value);
    if (userId) {
      const theme = userThemes.value.find((t) => t.id === userId && !t.error);
      if (theme) {
        // overrides stack on the theme file's chosen base palette
        document.documentElement.dataset.theme = theme.base;
        applyThemeTokens(document.documentElement.style, theme.tokens);
        return;
      }
      // the theme file vanished from the folder: fall back to auto and
      // stop persisting the dead id
      themeMode.value = "auto";
      localStorage.setItem("omega.theme-mode", "auto");
    }
    clearThemeTokens(document.documentElement.style);
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
  // a user theme edited (or deleted) on disk re-applies live the moment
  // the refreshed list lands - the drawer refetches on every open, so
  // an edit reflects on the next apply without restart or reload
  watch(
    userThemes,
    () => {
      if (parseThemeSelect(themeMode.value)) applyTheme();
    },
    { deep: true },
  );

  // and the picker refetches on every settings open: a theme dropped
  // into the folder while the app runs shows up (and a broken file
  // disappears from the list) without a restart
  watch(settingsOpen, (open) => {
    if (open) void loadUserThemes();
  });

  // loadUserThemes refreshes the picker's list. Fetched on mount (so a
  // persisted theme:<id> applies) and on every settings open (so a
  // dropped or edited file shows up without a restart). A failure just
  // empties the list - the pane keeps its built-in modes.
  async function loadUserThemes() {
    try {
      userThemes.value = await fetchThemes();
    } catch {
      userThemes.value = [];
    }
    const userId = parseThemeSelect(themeMode.value);
    if (userId && !userThemes.value.some((t) => t.id === userId && !t.error)) {
      themeMode.value = "auto";
      localStorage.setItem("omega.theme-mode", "auto");
    }
    applyTheme();
  }

  // --- tab-title spinner: a global read over the per-session busy map ---
  // Any live stream - in any session, any tab - spins the tab title;
  // individual sessions stay untouched (and unlocked) by it.
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
    // clicks carry pane focus (the tab bars, the empty panes, the
    // composers - anything inside a dock pane)
    document.addEventListener("click", onDocClick);

    // mobile detection: sidebars become slide-in overlays below 768px
    const mobileQuery = window.matchMedia("(max-width: 768px)");
    isMobile.value = mobileQuery.matches;
    mobileQuery.addEventListener("change", (e) => {
      isMobile.value = e.matches;
      closeOverlays(); // crossing the breakpoint resets overlay state
    });

    // restore persisted mode: a built-in mode or theme:<id>
    const saved = localStorage.getItem("omega.theme-mode");
    if (
      saved === "light" ||
      saved === "dark" ||
      saved === "auto" ||
      saved?.startsWith("theme:")
    )
      themeMode.value = saved as ThemeMode;
    void loadUserThemes();

    // restore the chat appearance (chat | stacked) - parseAppearance
    // falls back to the default on a missing/tampered value
    appearance.value = parseAppearance(localStorage.getItem(APPEARANCE_KEY));

    // auto mode: follow prefers-color-scheme reactively
    mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    systemPrefersDark = mediaQuery.matches;
    mediaQuery.addEventListener("change", (e) => {
      systemPrefersDark = e.matches;
      applyTheme();
    });
    applyTheme();

    // history wiring: popstate re-targets via the URL's session id
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

    // boot focus: the URL's session owns it ("reload lands on the
    // focused session") - an existing chat tab bound to that id is
    // focused in place, otherwise a tab opens into the active pane
    // (unknown/deleted ids and home route through the persisted chat
    // focus). Then the ChatTab mount restores + attaches per tab.
    // /board boots into the kanban view (boardHostPane resolves on
    // the first switchMainView; boot keeps the default pane).
    if (/^\/board\/?$/.test(window.location.pathname)) {
      mainView.value = "board";
      boardHostPane.value = resolveActivePaneId();
    }
    const initial = parseSessionPath(window.location.pathname);
    if (initial && dock.firstChatTab(initial)?.tab.sessionRef === initial) {
      const own = dock.firstChatTab(initial)!;
      if (await sessionExists(initial)) {
        dock.focus(own.paneId, own.index);
        focusedKey.value = own.tab.key ?? null;
        markRead(initial);
      } else {
        history.replaceState({}, "", "/");
        focusedKey.value = dock.firstChatTab()?.tab.key ?? null;
      }
    } else if (initial) {
      if (await sessionExists(initial)) openChatTab(initial);
      else {
        history.replaceState({}, "", "/");
        focusedKey.value = dock.firstChatTab()?.tab.key ?? null;
      }
    } else {
      // home: the persisted tree's chat focus carries over
      focusedKey.value = dock.firstChatTab()?.tab.key ?? null;
    }
  });

  onUnmounted(() => {
    document.removeEventListener("click", onDocClick);
    window.removeEventListener("popstate", onPop);
    if (healthTimer) clearInterval(healthTimer);
    if (runningTimer) clearInterval(runningTimer);
    if (spinnerTimer) clearInterval(spinnerTimer);
    if (typeof document !== "undefined" && savedTitle !== null)
      document.title = savedTitle;
  });
</script>