<template>
  <div class="shell">
    <MenuBar
      :sidebar-open="isMobile ? drawerOpen : sidebarDocked"
      :inspector-open="isMobile ? inspectorOpen : inspectorDocked"
      @toggle-sidebar="toggleSidebar"
      @toggle-inspector="toggleInspector"
      @open-notification="openNotification" />

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
          <!-- card t_e76ce7b6: the dock's edge-pane content is desktop
               chrome. Below 768px it is suppressed (the mobile drawer
               renders the same content independently - see the
               MobileDrawer blocks after .pane-area) so a desktop-closed
               sidebar (the pane pruned from the tree) never blanks the
               drawer. -->
          <AppSidebar
            v-if="!isMobile"
            class="pane"
            :sessions="sessions"
            :active-id="focusedSessionId"
            :streaming-ids="busyAll"
            :open="drawerOpen"
            @open="openChatTab"
            @new="openChatTab(null)"
            @new-ephemeral="openEphemeralTab"
            @pin="togglePin"
            @archive="toggleArchive"
            @branch="branchChatSession"
            @rename="renameChatSession"
            @remove="askDelete"
          />
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

        <!-- card t_4ae27e0d: the RIGHT sidebar's views are REAL dock tabs
             (Files / Cron / Logs / Skills / Tools - the chip strip is
             gone). Files is the shell's own panel; each module view is
             the composition panel body, resolved by module name - the
             dock tabbar hosts the tab, this outlet only hosts content. -->
        <template #pane-files>
          <FilesTab
            v-if="!isMobile"
            class="pane"
            :open="inspectorOpen" />
        </template>
        <template #pane-cron>
          <InspectorPanel
            v-if="!isMobile"
            class="pane"
            module="cron"
            :open="isMobile ? inspectorOpen : false" />
        </template>
        <template #pane-http_channel>
          <InspectorPanel
            v-if="!isMobile"
            class="pane"
            module="http_channel"
            :open="isMobile ? inspectorOpen : false" />
        </template>
        <template #pane-skills>
          <InspectorPanel
            v-if="!isMobile"
            class="pane"
            module="skills"
            :open="isMobile ? inspectorOpen : false" />
        </template>
        <template #pane-tools>
          <InspectorPanel
            v-if="!isMobile"
            class="pane"
            module="tools"
            :open="isMobile ? inspectorOpen : false" />
        </template>

        <!-- card t_7e2b7002: the agents panel - profile inventory with
             scaffold + new-session-as-agent (own fetch, inspector
             family). -->
        <template #pane-agents>
          <AgentsPanel
            v-if="!isMobile"
            class="pane" />
        </template>

        <!-- card t_49d0b308: settings is an INSPECTOR tab - no other entry
             point exists (the '+' menu no longer lists it); the tab lives
             pinned in the inspector pane. The mount loads on activation
             (the component's onMounted), the mobile drawer shares the
             same open flag as the module panels. -->
        <template #pane-settings>
          <SettingsTab
            v-if="!isMobile"
            class="pane"
            v-model:show-thinking="showThinking"
            v-model:appearance="appearance"
            v-model:tool-display="toolDisplay"
            :theme-mode="themeMode"
            :user-themes="userThemes"
            :open="inspectorOpen"
            @set-theme="setTheme" />
        </template>
        <!-- card t_49d0b308: the kanban board joins the inspector - the
             composition panel renders the compact/narrow-adaptive board
             in the ~300px sidebar column -->
        <template #pane-kanban>
          <InspectorPanel
            v-if="!isMobile"
            class="pane"
            module="kanban"
            :open="isMobile ? inspectorOpen : false" />
        </template>
        <!-- card t_14f73742: the extensions inventory joins the inspector -
             the webui parity of the TUI /extensions command. A SHELL tab
             over GET /extensions (composition ruling in extensionsRows.ts):
             has-ui rows emit open-module and openModuleTab routes to the
             module's inspector tab. /help parity: the webui has no
             slash-command bar - this tab IS the /extensions surface. -->
        <template #pane-extensions>
          <ExtensionsTab
            v-if="!isMobile"
            class="pane"
            :open="inspectorOpen"
            @open-module="openModuleTab" />
        </template>
        <!-- card t_e1e61680: insights was a main-area tab; card t_d7fd3ad3
             moved its home to the SECONDARY sidebar (a default-visible
             inspector tab, label "Insights"). The outlet renders wherever
             the tab lives; the '+' menu entry stays and FOCUSES this
             instance (singleton). The mount loads its own payload on
             activation (onMounted). -->
        <template #pane-insights>
          <InsightsTab
            v-if="!isMobile"
            class="pane" />
        </template>
        <!-- arc 3c: the canvas host - the generic panel renderer mounts
             INSIDE this outlet per panel the canvas store holds. The
             store (snapshot boot fetch + run-stream deltas) is the only
             source of panel presence (Q5a); the dock never knows one. -->
        <template #pane-canvas>
          <div
            v-if="!isMobile"
            class="pane canvas-pane">
            <CanvasPanel
              v-for="p in canvas.panels"
              :key="p.id"
              :panel="p"
              :state-of="canvas.actionState"
              @invoke="canvas.invoke" />
            <p
              v-if="canvas.panels.length === 0"
              class="canvas-empty">
              no panels mounted
            </p>
          </div>
        </template>
      </DockSplit>
    </div>

    <!-- card t_e76ce7b6: the mobile drawers. Below 768px the dock's
         pinned-edge panes collapse to zero-width empty cells (their slot
         content is suppressed above); these drawers render the same
         content with their own tab bar, independent of whether the edge
         pane currently lives in the dock tree. REPRO 1: a desktop-closed
         sidebar (pane pruned) no longer blanks the drawer. REPRO 2: the
         tab bar renders inside the drawer and switches its content. The
         scrim above covers both. -->
    <MobileDrawer
      v-if="isMobile"
      side="left"
      :open="drawerOpen"
      :tabs="sidebarDrawerTabs"
      aria-label="sessions"
      v-model="mobileSidebarTab">
      <AppSidebar
        v-if="sidebarDrawerActiveId === 'sidebar'"
        class="pane"
        :sessions="sessions"
        :active-id="focusedSessionId"
        :streaming-ids="busyAll"
        :open="drawerOpen"
        @open="openChatTab"
        @new="openChatTab(null)"
        @new-ephemeral="openEphemeralTab"
        @pin="togglePin"
        @archive="toggleArchive"
        @remove="askDelete"
      />
      <FilesTab
        v-else-if="sidebarDrawerActiveId === 'files'"
        class="pane"
        :open="drawerOpen" />
      <SettingsTab
        v-else-if="sidebarDrawerActiveId === 'settings'"
        class="pane"
        v-model:show-thinking="showThinking"
        v-model:appearance="appearance"
        v-model:tool-display="toolDisplay"
        :theme-mode="themeMode"
        :user-themes="userThemes"
        :open="drawerOpen"
        @set-theme="setTheme" />
      <InspectorPanel
        v-else-if="sidebarDrawerActiveId"
        class="pane"
        :module="sidebarDrawerActiveId"
        :open="drawerOpen" />
    </MobileDrawer>

    <MobileDrawer
      v-if="isMobile"
      side="right"
      :open="inspectorOpen"
      :tabs="inspectorDrawerTabs"
      aria-label="inspector"
      v-model="mobileInspectorTab">
      <FilesTab
        v-if="inspectorDrawerActiveId === 'files'"
        class="pane"
        :open="inspectorOpen" />
      <SettingsTab
        v-else-if="inspectorDrawerActiveId === 'settings'"
        class="pane"
        v-model:show-thinking="showThinking"
        v-model:appearance="appearance"
        v-model:tool-display="toolDisplay"
        :theme-mode="themeMode"
        :user-themes="userThemes"
        :open="inspectorOpen"
        @set-theme="setTheme" />
      <!-- card t_14f73742: the mobile drawer seats the shell inventory
           tab like any inspector view - before the module catch-all,
           which would try InspectorPanel(module="extensions") and
           render an empty panel body.
           card t_d7fd3ad3: insights joins the same class - its home is
           the right shell now, so the drawer needs its branch before the
           catch-all (InspectorPanel(module="insights") would render an
           empty body - insights is shell-owned, not a module panel). -->
      <ExtensionsTab
        v-else-if="inspectorDrawerActiveId === 'extensions'"
        class="pane"
        :open="inspectorOpen"
        @open-module="openModuleTab" />
      <InsightsTab
        v-else-if="inspectorDrawerActiveId === 'insights'"
        class="pane" />
      <!-- arc 3c: the canvas joins the mobile drawer like every
           inspector tab - before the module catch-all, which would try
           InspectorPanel(module="canvas") and render empty -->
      <div
        v-else-if="inspectorDrawerActiveId === 'canvas'"
        class="pane canvas-pane">
        <CanvasPanel
          v-for="p in canvas.panels"
          :key="p.id"
          :panel="p"
          :state-of="canvas.actionState"
          @invoke="canvas.invoke" />
        <p
          v-if="canvas.panels.length === 0"
          class="canvas-empty">
          no panels mounted
        </p>
      </div>
      <InspectorPanel
        v-else-if="inspectorDrawerActiveId"
        class="pane"
        :module="inspectorDrawerActiveId"
        :open="inspectorOpen" />
    </MobileDrawer>

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
      :live-deltas-per-second="liveDeltasPerSecond"
      :last-chunks-per-second="lastChunksPerSecond"
      :version="healthVersion"
      :up-seconds="upSeconds" />
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, onUnmounted, provide, ref, watch } from "vue";
  import { modulePanels } from "./composables/module-panels.generated";
  import {
    archiveSession,
    branchSession,
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
    renameSession,
    APPEARANCE_KEY,
    isAtBottom,
    pillVisible,
    lastUserText,
    chatPanelMessageCount,
    chatPanelOf,
    normalizeChatPayload,
    persistedTelemetry,
    formatDurationMs,
    fetchThemes,
    applyThemeTokens,
    clearThemeTokens,
    parseThemeSelect,
    type UserTheme,
    type ThemeMode,
  } from "./composables/omega";
  import type { Session, Appearance } from "./composables/omega";
  import {
    useToolDisplay,
    TOOL_DISPLAY_KEY,
    type ToolDisplay,
  } from "./composables/toolDisplay";
  import {
    createDock,
    findPane,
    findEdgePane,
    isPinnedPane,
    DOCK_API_KEY,
    DOCK_CHAT_OPEN_KEY,
    DOCK_TAB_TITLE_KEY,
    nextTabKey,
  } from "./composables/dock";
  // card t_d7fd3ad3 item 6: the bottom panel's width mode - the applier
  // reads the shared ref; settings writes it
  import { useBottomWidthMode } from "./composables/bottomWidth";
  // card t_e76ce7b6: the mobile drawer renders the pinned-edge content
  // (sessions / inspector tabs) independently of the dock tree, so it
  // reads the dockable registry directly for the default tab set + the
  // tab titles (the same voice the dock tabbar uses)
  import {
    INSPECTOR_TAB_COMPONENTS,
    COMPONENT_TITLES,
    type DockComponentName,
  } from "./composables/dockables";
  // card t_e76ce7b6: the mobile drawer's pure tab derivation (decoupled
  // from the dock tree - REPRO 1's fix)
  import { drawerTabsForEdge, clampDrawerTab } from "./composables/mobileDrawer";
  import {
    CHAT_HOST_KEY,
    DEFAULT_CHAT_STATE,
    EPHEMERAL_DRAFT_REF,
    createChatHost,
    type ChatState,
  } from "./composables/chat";
  import {
    COMPOSER_FOCUS_KEY,
    createComposerFocusSignal,
    shouldCaptureTyping,
  } from "./composables/focus";
  // arc 3c: the canvas store - the webui's one view-model state. The
  // snapshot lands at boot; the deltas ride the run streams through the
  // host's canvasDelta dep. Panel presence lives in the store, never a
  // hardcoded list (Q5a).
  import { createCanvasStore } from "./composables/canvas";
  import CanvasPanel from "./components/canvas/CanvasPanel.vue";
  import ChatTab from "./components/ChatTab.vue";
  import SettingsTab from "./components/settings/SettingsTab.vue";
  // card t_5d9032d3: notification open intents route here from the bell;
  // card t_556ee727: the board select seams (the task row's board pick)
  import type { OmegaNotification } from "./composables/notifications";
  import {
    resolveTaskBoard,
    selectBoardFor,
  } from "./composables/notifications";
  // card t_e1e61680: insights joins the main area (registry: singleton,
  // closable) - the outlet renders it when its tab is active
  import InsightsTab from "./components/insights/InsightsTab.vue";
  import AgentsPanel from "./components/agents/AgentsPanel.vue";
  // card t_14f73742: the extensions inventory joins the inspector (registry:
  // singleton, pinned right) - the outlet renders it when its tab is active
  import ExtensionsTab from "./components/shell/ExtensionsTab.vue";
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
  // canvas snapshot poll: the between-runs catch-up (arc 4b toast law)
  const CANVAS_POLL_MS = 15_000;
  let canvasTimer: ReturnType<typeof setInterval> | null = null;
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
  const drawerOpen = ref(false);
  const inspectorOpen = ref(false);
  const isMobile = ref(false);
  const showThinking = ref(true);
  // chat appearance (card t_84e4653c): 'chat' chronolog | 'stacked'
  // blocks. Persisted like the theme - last choice wins across reloads,
  // garbage restores the default.
  const appearance = ref<Appearance>("chat");
  // tool result display (card t_868633b3): auto | expanded | collapsed -
  // v-model binds the SHARED ref (composables/toolDisplay.ts) so the
  // settings knob writes the same signal the tool rows read, wherever
  // they render. The ref restores from localStorage at module load;
  // this watch keeps the choice persisted (same pattern as appearance).
  const toolDisplay = useToolDisplay();
  watch(toolDisplay, (mode) => {
    localStorage.setItem(TOOL_DISPLAY_KEY, mode);
  });
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
  // the sidebars are FIXED panes (card t_4ae27e0d): no open/close state
  // exists on desktop. drawerOpen/inspectorOpen are the MOBILE overlay
  // flags only; the MenuBar toggles surface at every breakpoint (card
  // t_aca53fee) - below 768px they drive these drawer ref flags.

  // --- card t_d7fd3ad3 item 6: the bottom panel's width modes ---
  // aligned (the default): the panel's width matches the MAIN panes'
  // bounding box - the VS Code panel read, the sidebars stand clear.
  // full: the panel spans the dock area (the pre-card behavior).
  // The applier measures the rendered panes and writes the insets as CSS
  // custom properties on the ROOT split element; bottompanel.css puts
  // them on the bottom pane AND the root divider (the handle stays over
  // the panel it drags). Defaults are 0, so an un-applied boot renders
  // full-width and no stale measurement can inset anything.
  // card t_556ee727: aligned mode ALSO re-asserts the sidebars'
  // full height - the shell is a root-column child, so the row above
  // (sidebars included) loses the band's height; the applier measures
  // the band and writes --bottom-sidebars-overhang (negative), and
  // bottompanel.css lets the two pinned sidebars overhang downward
  // beside the band (its insets clear the way). full/hide clears the
  // properties: plain stretch returns.
  const bottomWidthMode = useBottomWidthMode();
  function clearBottomInsets(area: HTMLElement) {
    area.style.removeProperty("--bottom-inset-left");
    area.style.removeProperty("--bottom-inset-right");
    area.style.removeProperty("--bottom-sidebars-overhang");
  }
  function applyBottomWidth() {
    // the bottom shell hides (toggle/drag-close) - clear the pass (a
    // stale overhang would keep the sidebars stretched past the dock,
    // a stale inset nothing consumes)
    const bottom = document.querySelector<HTMLElement>(".dock-pane.edge-bottom");
    const area = bottom?.parentElement ??
      document.querySelector<HTMLElement>(".pane-area > .dock-root");
    if (!bottom || !area) {
      if (area) clearBottomInsets(area);
      return;
    }
    const reset = () => clearBottomInsets(area);
    if (bottomWidthMode.value !== "aligned") return reset();
    const mains = [
      ...document.querySelectorAll<HTMLElement>(".dock-pane:not(.pinned)"),
    ];
    if (mains.length === 0) return reset();
    const areaBox = area.getBoundingClientRect();
    let left = Infinity;
    let right = -Infinity;
    for (const pane of mains) {
      const r = pane.getBoundingClientRect();
      left = Math.min(left, r.left);
      right = Math.max(right, r.right);
    }
    if (!(right > left)) return reset(); // no rendered main box - stay full
    const insetL = Math.max(0, Math.round(left - areaBox.left));
    const insetR = Math.max(0, Math.round(areaBox.right - right));
    area.style.setProperty("--bottom-inset-left", insetL + "px");
    area.style.setProperty("--bottom-inset-right", insetR + "px");
    // the sidebars keep their full height in aligned mode: the band's
    // measured height (plus the 4px root divider) is the overhang the
    // pinned sidebars' CSS extends by - a NEGATIVE margin bottom lets
    // the stretch re-claim the band region beside the band
    const bandH = Math.max(0, Math.round(bottom.offsetHeight));
    area.style.setProperty(
      "--bottom-sidebars-overhang",
      -(bandH + 4) + "px",
    );
  }
  // re-measure the pass after the DOM catches up with the layout change
  watch([() => dock.tree.value, bottomWidthMode], () => nextTick(applyBottomWidth));

  // card t_33f8756c: the window-narrow give-way. The main pane never dips
  // below its 320px contract floor and is never overlapped: the SECONDARY
  // sidebar (inspector) collapses first, then the PRIMARY - dock.narrowSqueeze
  // owns the priority and remembers each hidden pane's width for its toggle.
  // The content box is the root split's rendered width (the pane-area's
  // padding already excluded). Below the breakpoint the mobile drawers own
  // the layout - the squeeze stands down there.
  function applyNarrowSqueeze() {
    if (isMobile.value) return;
    const root = document.querySelector<HTMLElement>(".pane-area > .dock-root");
    if (!root) return;
    dock.narrowSqueeze(root.clientWidth);
  }
  // one listener: the bottom-width applier and the give-way run together
  function onWinResize() {
    applyBottomWidth();
    applyNarrowSqueeze();
  }
  // the pane-area resize observer (the give-way belt) - declared here so
  // onUnmounted can reach it whatever the mount path observed
  let areaObserver: ResizeObserver | null = null;

  // the give-way also runs after any STRUCTURAL tree change: a menubar
  // toggle reopen into a still-narrow window changes no container width
  // (no resize event, no pane-area resize) - the squeeze re-checks after
  // the DOM settles and gives the just-reopened pane away again (the chat
  // floor is a hard wall; the toggle is a no-op below the seat width).
  // Idempotent: once the tree fits, no-op - the watch settles immediately.
  watch(
    () => dock.tree.value,
    () => nextTick(applyNarrowSqueeze),
  );

  // arc 3c: the canvas - one store per page (panels are global state,
  // settled Q10a). Boot fetches the snapshot below; live runs feed it
  // deltas through the host's canvasDelta dep.
  const canvas = createCanvasStore();

  // --- the chat host (card t_4f0e88f2): every chat tab gets its own
  // stream state keyed by the tab's instance key; the gate stays per
  // session (the runHub run slot is per session). ---
  const host = createChatHost({
    setTabSessionRef: (key, sessionId) => dock.setSessionRef(key, sessionId),
    refreshSessions: () => refreshSessions(),
    sessionRow: (id) => sessions.value.find((s) => s.id === id),
    markRead: (id) => markRead(id),
    isFocused: (key) => focusedKey.value === key,
    // arc 3c: canvas panel deltas ride every run stream - the app's
    // canvas store is where they merge
    canvasDelta: (panel) => canvas.applyDelta(panel),
    // chat-last 2a: the chat pane renders from the canvas' chat panel
    // (snapshot + deltas) - the host hands the descriptor to the view
    chatPanel: () => chatPanelOf(canvas.panels),
    // the snapshot refetch: variant selects and action-driven repaints
    // (the deltas ride run streams only)
    canvasRefresh: () => canvas.refresh(),
  });

  // --- main-view: chat vs the module-owned board (kanban). Ported
  // from the pre-dock shell per the dock 1/5 merge ruling: the board
  // main-view folds into the chat pane template. Under the dock-era
  // URL ruling (replaceState only, the URL owns the focused chat),
  // /board is a replaceState overlay - the urlPath watcher skips while
  // the board is up and chat reclaims the URL on the way back.
  // card t_33f8756c: the menubar's chat/board switch is GONE (the kanban
  // inspector tab carries the board now) - mainView survives as the
  // /board route + bell-task overlay state; no menubar control re-emits.
  // ponytail: board renders in ONE pane (the active chat pane); the
  // dockable-contract card gives board its own component slot later.
  const mainView = ref<"chat" | "board">("chat");
  const boardComponent = computed(
    () => modulePanels.find((p) => p.module === "kanban")?.component,
  );
  const boardHostPane = ref<string>("");

  // a board card's transcript chip lands the user in that conversation
  function openBoardSession(id: string) {
    mainView.value = "chat";
    openChatTab(id);
  }

  // card t_5d9032d3: the bell's open intent routes by source kind -
  // task -> the kanban PANE comes to the foreground (the card lives
  // there), cron -> the cron inspector tab, chat -> the session when
  // one is bound (a dispatched subagent run id is not a local session;
  // the route is a no-op then).
  // card t_556ee727: the task route no longer hijacks the MAIN pane
  // (the old mainView board overlay + /board replaceState is dead on
  // this path) - the click opens the inspector if it is hidden, focuses
  // the kanban tab, and hands the board select to the module board
  // through the event seam. Chat content never changes on the click.
  function openNotification(n: OmegaNotification | unknown) {
    const rec = n as OmegaNotification;
    if (!rec || typeof rec.kind !== "string") return;
    switch (rec.kind) {
      case "task": {
        if (!findEdgePane(dock.tree.value, "inspector")) {
          // the inspector is hidden (toggle / drag-close / the narrow
          // give-way pruned it) - the reopen re-asserts the full strip
          dock.toggleEdge("inspector", "after");
        }
        const pane = findEdgePane(dock.tree.value, "inspector");
        if (pane) dock.openSingleton(pane.id, "kanban");
        void focusNotificationBoard(rec.source_id);
        return;
      }
      case "cron":
        openModuleTab("cron");
        return;
      case "chat":
        if (sessions.value.some((s) => s.id === rec.source_id)) {
          openChatTab(rec.source_id);
        }
        return;
    }
  }

  // the notification's board: resolve the task's board over the wire
  // (registry + per-board task scan) and hand the pick to the module
  // board. The channel is the event seam (the stash + CustomEvent in
  // composables/notifications.ts): module UI imports no shell code, so
  // the selection crosses as an event the Board component listens on -
  // the stash covers the mount race (the tab activation mounts the
  // board component on the tick AFTER openSingleton, but the resolve's
  // fetches usually beat even that).
  async function focusNotificationBoard(taskId: string) {
    const board = await resolveTaskBoard(taskId);
    if (!board) return;
    selectBoardFor(board);
  }
  provide(CHAT_HOST_KEY, host);

  // --- composer auto-focus (card t_b40131ac): the signal every chat
  // tab listens on; the gating rules live in composables/focus.ts ---
  const composerFocus = createComposerFocusSignal();
  provide(COMPOSER_FOCUS_KEY, composerFocus);

  // chat tab titles read the session labels; the sidebar highlight
  // follows the FOCUSED tab - the bar label has to show it. Non-chat
  // tabs: the inspector's module views resolve their ui/dockable.json
  // title (the same voice the old chip strip used); the registry
  // fallback covers the rest (files = "Files").
  provide(DOCK_TAB_TITLE_KEY, (tab) => {
    if (tab.component === "chat") {
      // ephemeral tabs (card t_d96a5cd5): the draft marker or a bound
      // `eph-` id never matches a session row (they are store-invisible)
      // - the tab is named by contract, not by lookup
      if (tab.sessionRef === EPHEMERAL_DRAFT_REF || tab.sessionRef?.startsWith("eph-")) {
        return "ephemeral";
      }
      if (!tab.sessionRef) return "new chat";
      const s = sessions.value.find((x) => x.id === tab.sessionRef);
      return s?.label || "chat";
    }
    const panel = modulePanels.find((p) => p.module === tab.component);
    return panel?.dockable?.title ?? null;
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
  // session carries numbers - DEFAULT_CHAT_STATE fallback). The message
  // count derives from the chat panel's rows while the canvas covers the
  // focused session, the restore's fallback otherwise (chat-last 2a).
  const chatPanelPayload = computed(() => {
    const panel = chatPanelOf(canvas.panels);
    return panel ? normalizeChatPayload(panel.payload) : null;
  });
  const tokens = computed(() => focusedChatState.value.tokens);
  const tokensPerSecond = computed(() => focusedChatState.value.tokensPerSecond);
  const liveDeltasPerSecond = computed(() => focusedChatState.value.liveDeltasPerSecond);
  const lastChunksPerSecond = computed(() => focusedChatState.value.lastChunksPerSecond);
  const messageCount = computed(() => {
    const payload = chatPanelPayload.value;
    if (payload && payload.session === focusedSessionId.value)
      return chatPanelMessageCount(payload);
    return focusedChatState.value.restored.filter((i) => i.kind === "message").length;
  });
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

  // the pane the BOARD view hosts into (and chat opens fall back to): an
  // actually chat-hosting pane first, else the first main pane (never the
  // pinned sidebars). The dock's ensureMainPane re-inserts a flexible pane
  // when the user managed to remove every main pane.
  function resolveActivePaneId(): string {
    const tree = dock.tree.value;
    const focused = activeChatPaneId.value ?
      findPane(tree, activeChatPaneId.value) :
      null;
    if (focused && !isPinnedPane(focused) && focused.tabs.some((t) => t.component === "chat")) return focused.id;
    return dock.ensureMainPane();
  }

  // openChatTab is THE tab-opener: the sidebar's row click (session id)
  // and the '+' flows (null = unsaved composer-first) go through it.
  // card t_4ae27e0d routing: a session ALWAYS opens in the main pane, and
  // a tab already bound to that session is FOCUSED wherever it lives -
  // never a duplicate. Only "new" (or an unbound session) mints a tab.
  // card t_311838e4: a DRAFT tab (no session binding yet) already open?
  // Focus it wherever it sits - no duplicate new-chat tab. Mirrors the
  // focus-existing rule above for opened sessions; only when no draft
  // tab lives anywhere does '+' mint a fresh one.
  function openChatTab(sessionId: string | null, paneId?: string) {
    if (sessionId) {
      const found = dock.firstChatTab(sessionId);
      if (found?.tab.key) {
        dock.focus(found.paneId, found.index);
        focusedKey.value = found.tab.key;
        markRead(sessionId);
        if (isMobile.value) drawerOpen.value = false;
        return;
      }
    } else {
      const draft = dock.firstDraftChatTab();
      if (draft?.tab.key) {
        dock.focus(draft.paneId, draft.index);
        focusedKey.value = draft.tab.key;
        if (isMobile.value) drawerOpen.value = false;
        return;
      }
    }
    const pane = paneId ?? dock.ensureMainPane();
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

  // openEphemeralTab (card t_d96a5cd5): the "+ new" dropdown's no-persist
  // entry. A composer-first tab CARRYING the EPHEMERAL_DRAFT_REF marker
  // - ChatTab reads the marker into the state's ephemeral bit, the send
  // rides /chat {ephemeral:true} and the server's `eph-` id adopts onto
  // the tab. Always mints fresh (no draft reuse: a plain draft and an
  // ephemeral draft are different contracts and must not merge).
  function openEphemeralTab() {
    const pane = dock.ensureMainPane();
    const key = nextTabKey();
    dock.openTab(pane, { component: "chat", sessionRef: EPHEMERAL_DRAFT_REF, key });
    focusedKey.value = key;
    if (isMobile.value) drawerOpen.value = false;
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

  // --- composer auto-focus (card t_b40131ac) ---
  // focus-on-select: a chat selection is a focusedKey TRANSITION while
  // the chat view is up - sidebar row, dock tab click (onDocClick ->
  // refocusFrom), new chat, board chip. A transition to null never
  // requests; moving to board/settings never changes focusedKey, so
  // those views never steal. A freshly minted tab mounts after this
  // request (openChatTab sets focusedKey synchronously) - its mount
  // re-checks the pending signal and delivers there.
  watch(focusedKey, (key) => {
    if (key && mainView.value === "chat") composerFocus.request(key);
  });

  // type-to-capture: with the omega window focused, the first printable
  // key without modifiers jumps into the focused chat's composer - the
  // character goes along (the signal's sync watcher focuses during the
  // keydown, so the insertion event targets the textarea). A hidden
  // chat tab never renders a matching instance, so a focused key that
  // is nowhere on screen simply does not capture. The predicates (no
  // steal from a focused input, no modifier combos, no modals) live in
  // composables/focus.ts.
  function onKeyCapture(e: KeyboardEvent) {
    if (
      !shouldCaptureTyping(e, {
        mainView: mainView.value,
        modalOpen: !!deleteId.value,
      })
    )
      return;
    const key = focusedKey.value;
    if (key) composerFocus.request(key);
  }

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
      const list = await fetchSessions();
      sessions.value = list;
      // server-reported live runs feed the host: they REPLACE (not
      // union into) the snapshot - the sessions a JUST-refreshed page
      // sees as pulsing; local streams carry their own busy entries
      host.noteServerActive(list.filter((s) => s.running).map((s) => s.id));
    } catch {
      // sidebar refresh is best-effort
    }
  }

  // card t_47b776ad: the menubar toggles are BACK - on desktop they
  // drive dock.toggleEdge (hide/reopen the pinned edge panes at their
  // edge, reopen at the clamp width; a reload re-asserts the fixed
  // shape via ensurePinnedEdges). On mobile the same emits keep flipping
  // the overlay drawers (one at a time) - the toggles render only above
  // the 768px breakpoint, the drawer handles only below it.
  const sidebarDocked = computed(() => findEdgePane(dock.tree.value, "sidebar") !== null);
  // the right shell lives while its marker exists - even empty (free
  // content: tabs come and go, the shell stays)
  const inspectorDocked = computed(() => findEdgePane(dock.tree.value, "inspector") !== null);

  // card t_e76ce7b6: the mobile drawer's tab bar. The drawer renders the
  // pinned-edge content independently of the dock tree (REPRO 1: a
  // desktop-closed sidebar prunes the edge pane - the drawer must still
  // open with content), so its tab set reads the live edge pane when it
  // exists and falls back to the registry default when it does not. The
  // active tab is the drawer's OWN state - switching it never touches the
  // dock tree (the desktop hidden state is page-lifetime by design and
  // must not be coupled to the mobile drawer, REPRO 1's root cause).
  const mobileSidebarTab = ref(0);
  const mobileInspectorTab = ref(0);

  function drawerTabTitle(id: DockComponentName): string {
    // the same voice the dock tabbar uses: a module dockable.json title
    // wins, the registry title is the fallback (chat never reaches here)
    const panel = modulePanels.find((p) => p.module === id);
    return panel?.dockable?.title ?? COMPONENT_TITLES[id];
  }

  const sidebarDrawerTabs = computed(() =>
    drawerTabsForEdge(
      findEdgePane(dock.tree.value, "sidebar"),
      ["sidebar"],
      drawerTabTitle,
    ),
  );
  const inspectorDrawerTabs = computed(() => {
    const live = drawerTabsForEdge(
      findEdgePane(dock.tree.value, "inspector"),
      INSPECTOR_TAB_COMPONENTS,
      drawerTabTitle,
    );
    // card t_67fc1905: logs is a DUAL-HOST domain now - a fresh boot seats
    // it in the bottom panel (its defaultHome), so the inspector pane's
    // strip no longer names it. The drawer reads the live strip, which
    // would strand the logs view on mobile (its pane instance is
    // zero-collapsed there). The registry's right-domain list is the
    // drawer's floor: missing entries join the live tabs at the end.
    const seen = new Set(live.map((t) => t.id));
    return live.concat(
      INSPECTOR_TAB_COMPONENTS
        .filter((id) => !seen.has(id))
        .map((id) => ({ id, title: drawerTabTitle(id) })),
    );
  });

  // the active tab id, clamped against the live tab set (the edge pane's
  // tabs can change under a DnD while the drawer is closed; the drawer
  // never indexes past the end)
  const sidebarDrawerActiveId = computed(() =>
    clampDrawerTab(sidebarDrawerTabs.value, mobileSidebarTab.value)?.id,
  );
  const inspectorDrawerActiveId = computed(() =>
    clampDrawerTab(inspectorDrawerTabs.value, mobileInspectorTab.value)?.id,
  );

  // card t_14f73742: a has-ui extension row routes to the module's
  // inspector tab - focus its live instance, open it if the strip
  // dropped one (the loader restores the full set anyway). The guard is
  // honest against malformed endpoint payloads: an id outside the
  // registry never reaches the dock engine.
  function openModuleTab(moduleId: string) {
    if (!(INSPECTOR_TAB_COMPONENTS as readonly string[]).includes(moduleId)) return;
    const pane = findEdgePane(dock.tree.value, "inspector");
    if (!pane) return;
    dock.openSingleton(pane.id, moduleId as DockComponentName);
  }

  function toggleSidebar() {
    if (!isMobile.value) {
      dock.toggleEdge("sidebar", "before");
      return;
    }
    inspectorOpen.value = false;
    drawerOpen.value = !drawerOpen.value;
  }

  function closeOverlays() {
    drawerOpen.value = false;
    inspectorOpen.value = false;
  }

  // mobile: one drawer at a time
  function toggleInspector() {
    if (!isMobile.value) {
      dock.toggleEdge("inspector", "after");
      return;
    }
    drawerOpen.value = false;
    inspectorOpen.value = !inspectorOpen.value;
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
  // the list already holds flips its flag locally and re-buckets on the
  // next render - into the archived group at the bottom, or back into
  // its time bucket. A failed PATCH reverts and re-syncs.
  async function toggleArchive(id: string, target: boolean) {
    const updateList = (list: Session[]) =>
      list.map((s) => (s.id === id ? { ...s, archived: target } : s));
    const before = sessions.value;
    sessions.value = updateList(sessions.value);
    try {
      await archiveSession(id, target);
    } catch {
      sessions.value = before;
      refreshSessions();
    }
  }

  // branchSession flow (t_6bf696e4, the /branch parity): POST creates
  // the child (parent_id set, history inherited server-side), the
  // refresh brings the -badge'd rows in, and the child opens like any
  // session. A failed POST leaves the parent untouched - the refresh
  // shows the truth.
  async function branchChatSession(id: string) {
    let child: Session | null = null;
    try {
      child = await branchSession(id);
    } catch {
      return;
    }
    if (!child) return;
    await refreshSessions();
    openChatTab(child.id);
  }

  // rename flow (the /label parity): the optimistic label flip paints
  // instantly, the PUT lands the real one; a failed PUT reverts and
  // re-syncs (same shape as the pin/archive toggles).
  async function renameChatSession(id: string, label: string) {
    const updateList = (list: Session[]) =>
      list.map((s) => (s.id === id ? { ...s, label } : s));
    const before = sessions.value;
    sessions.value = updateList(sessions.value);
    try {
      await renameSession(id, label);
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

  // the gear is gone (card t_47b776ad): settings now opens from the
  // pane '+' menu / the dock. The refetch-on-open survives the gear's
  // death - this watch catches every absent -> present transition of
  // the settings singleton, however it was opened (the drawer-era
  // settingsOpen watch got the same job in the dock era).
  watch(
    () => dock.has("settings"),
    (present, was) => {
      if (present && !was) void loadUserThemes();
    },
  );

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
    // type-to-capture: window-level keydown (any printable key jumps
    // into the focused chat's composer when no text field holds focus)
    window.addEventListener("keydown", onKeyCapture);
    // the give-way belt: the resize event alone misses layout changes that
    // never touch window.width (emulated viewports, a toggle-reopen into a
    // still-narrow window) - the pane-area observer feeds the same squeeze
    // on ANY root width change. Idempotent: once the tree fits, no-op.
    const areaEl = document.querySelector(".pane-area");
    if (areaEl && typeof ResizeObserver !== "undefined") {
      areaObserver = new ResizeObserver(() => applyNarrowSqueeze());
      areaObserver.observe(areaEl);
    }
    // the width pass re-measures on viewport changes too (the insets are
    // px measurements against the rendered panes)
    window.addEventListener("resize", onWinResize);
    void nextTick(onWinResize); // the boot pass (the watch needs a change)

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
    // the models fetch does NOT block boot: a slow or stalled provider
    // /models round-trip (shared keypool stall) used to hold the whole
    // sidebar hostage. Fire it off and let the picker fill in when it
    // lands - the session list renders independent of provider reachability.
    void fetchModels()
      .then((m) => {
        models.value = m.models;
        selectedModel.value = m.current;
        if (m.context_window) contextSize.value = m.context_window;
      })
      .catch(() => {
        // models are decoration until a chat runs
      });
    refreshSessions();
    // running-state poller: every tab reconciles against the rows
    runningTimer = setInterval(tickRunningPoll, RUNNING_POLL_MS);
    // the canvas snapshot: the load-time catch-up (settled Q3b) - the
    // panels mounted before this page loaded arrive here; everything
    // later rides the run-stream deltas. Deltas only flow while a run
    // is live, so a modest snapshot poll is the canvas' between-runs
    // catch-up too (arc 4b: a toast emitted while nothing runs arrives
    // here, fingerprint-deduped) - the same poll shape the dock keeps
    // for its REST list.
    void canvas.refresh();
    canvasTimer = setInterval(() => { void canvas.refresh(); }, CANVAS_POLL_MS);

    // boot focus: the URL's session owns it ("reload lands on the
    // focused session") - an existing chat tab bound to that id is
    // focused in place, otherwise a tab opens into the active pane
    // (unknown/deleted ids and home route through the persisted chat
    // focus). Then the ChatTab mount restores + attaches per tab.
    // /board boots into the kanban view (boardHostPane resolves on
    // the first board overlay activation; boot keeps the default pane).
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
    window.removeEventListener("keydown", onKeyCapture);
    window.removeEventListener("resize", onWinResize);
    areaObserver?.disconnect();
    window.removeEventListener("popstate", onPop);
    if (healthTimer) clearInterval(healthTimer);
    if (runningTimer) clearInterval(runningTimer);
    if (canvasTimer) clearInterval(canvasTimer);
    if (spinnerTimer) clearInterval(spinnerTimer);
    if (typeof document !== "undefined" && savedTitle !== null)
      document.title = savedTitle;
  });
</script>