<template>
  <div class="shell">
    <MenuBar
      :sidebar-open="isMobile ? drawerOpen : sidebarDocked"
      :bottom-open="isMobile ? bottomDrawerOpen : bottomDocked"
      :inspector-open="isMobile ? inspectorOpen : inspectorDocked"
      :can-back="canBack"
      :can-forward="canForward"
      @back="nav.back()"
      @forward="nav.forward()"
      @toggle-sidebar="toggleSidebar"
      @toggle-bottom="toggleBottomPanel"
      @toggle-inspector="toggleInspector"
      @open-palette="togglePalette"
      @open-settings="openSettingsTab"
      @open-notification="openNotification" />

    <!-- drawer scrim: sibling of pane-area, NOT nested inside it - the
         backdrop-filter frost renders against the shell's root, so the
         mobile drawers slide under identical frost -->
    <div
      v-if="isMobile && (drawerOpen || inspectorOpen || bottomDrawerOpen)"
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
              :groups="modelGroups"
              :session="chatSession(tab)"
              :thinking="selectedThinking"
              :appearance="appearance"
              @update:model="onChatModel(tab, $event)"
              @update:thinking="selectedThinking = $event"
              @refresh-models="refreshModels().catch(() => {})" />
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
             (Files / Cron / Skills / Tools - the chip strip is gone).
             Files is the shell's own panel; each module view is the
             composition panel body, resolved by module name - the dock
             tabbar hosts the tab, this outlet only hosts content.
             card t_01c53e78: the raw-tail Logs panel (a http_channel
             module panel) retired with its component - no outlet, no
             manifest entry; the bottom shell's surviving tab is the
             observability capsule's, named "Logs". -->
        <template #pane-files>
          <FilesTab
            v-if="!isMobile"
            class="pane"
            :open="inspectorOpen" />
        </template>
        <!-- card t_36ceb99b: the content viewer tab - the file/skill a
             row click opened. Keyed by the payload so a switch to a
             different viewer tab mounts a fresh fetch; the watch inside
             covers a hand-off on the same instance. -->
        <template #pane-viewer="{ tab }">
          <ViewerTab
            v-if="tab.viewer"
            :key="tab.viewer.kind + ':' + tab.viewer.name"
            class="pane"
            :viewer="tab.viewer" />
        </template>
        <template #pane-cron>
          <InspectorPanel
            v-if="!isMobile"
            class="pane"
            module="cron"
            :open="isMobile ? inspectorOpen : false" />
        </template>
        <!-- card t_55a3fc4b: the terminal tab - the integrated shell
             (the terminal module's own view rides it; the shell
             keeps running while a sibling tab activates - the outlet
             unmounts the SSE, never the pty). The view is the capsule's
             own (harness/terminal/ui/, card t_617c2ad0): bound from the
             tabs manifest, missing = the outlet hosts nothing. -->
        <template #pane-terminal>
          <component
            :is="terminalTabView"
            v-if="terminalTabView && !isMobile"
            class="pane"
            :focused-session="focusedSessionId" />
        </template>
        <!-- card t_e703180e: the observability tab - the structured
             run-event stream (primary) over the observability capsule's
             own two reads, the raw log tail parked behind a chip in the
             same band (ruling 1: not a peer tab). The view is the
             capsule's own (harness/observability/ui/, the tabs
             manifest); its default scope is the FOCUSED session, and it
             reads on mount - the dock unmounts an inactive outlet, so a
             background tab costs nothing. -->
        <template #pane-observability>
          <component
            :is="observabilityTabView"
            v-if="observabilityTabView && !isMobile"
            class="pane"
            :session="focusedSessionId" />
        </template>
        <!-- card t_95c91f9c: the general web browser tab - the main-pane
             content tab (like chat/viewer), the URL bar + iframe embed
             over harness/browser's /browser/last. Singleton: the opener
             FOCUSES the one instance. The view is the capsule's own
             (harness/browser/ui/, card t_617c2ad0): bound from the tabs
             manifest, missing = the outlet hosts nothing. -->
        <template #pane-browser>
          <component
            :is="browserTabView"
            v-if="browserTabView && !isMobile"
            class="pane" />
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
             scaffold (own fetch, inspector family). card t_2d8deb8c:
             the tab renders DETAILS + EDIT on expand. card t_81109e1c:
             a row name click opens the agent's most-recent session in a
             chat tab (window event -> openChatTab below); the per-agent
             session LIST still never renders here. -->
        <template #pane-agents>
          <AgentsPanel
            v-if="!isMobile"
            class="pane" />
        </template>

        <!-- card t_00e4a784 (Endo ruling 2026-11-09): settings is an
             ON-DEMAND main-area tab now - the menubar gear opens it
             (VSCode's settings tab), it closes like any tab, and the
             pane renders here wherever the tab lives (mobile included:
             the main pane is the one surface that never collapses).
             The mount loads on activation (the component's onMounted),
             so no `open` overlay flag rides the desktop tab; the mobile
             drawers below keep their own copy for a free-content drag
             of the tab into an edge shell. -->
        <template #pane-settings>
          <SettingsTab
            class="pane"
            v-model:show-thinking="showThinking"
            v-model:appearance="appearance"
            v-model:tool-display="toolDisplay"
            v-model:thinking-display="thinkingDisplay"
            :color-mode="colorMode"
            :theme-id="themeId"
            :user-themes="userThemes"
            @set-color-mode="setColorMode"
            @set-theme="setTheme" />
        </template>
        <template #pane-mcp>
          <InspectorPanel
            v-if="!isMobile"
            class="pane"
            module="mcp"
            :open="isMobile ? inspectorOpen : false" />
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
             a module-command surface (/extensions on the seam). A SHELL
             tab over GET /extensions (composition ruling in extensionsRows.ts):
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
        <!-- card t_180db2cb: the notifications tab - the durable
             finished-things feed rehomed from the top-right bell (which
             died entirely), same data + rendering, mounted when its
             inspector tab is active. The mount marks the feed read (the
             tab chip's badge clears on open). The view is the capsule's
             own (harness/notifications/ui/, card t_617c2ad0). -->
        <template #pane-notifications>
          <component
            :is="notificationsPanelView"
            v-if="notificationsPanelView && !isMobile"
            class="pane"
            @open="openNotification" />
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
        v-model:thinking-display="thinkingDisplay"
        :color-mode="colorMode"
        :theme-id="themeId"
        :user-themes="userThemes"
        :open="drawerOpen"
        @set-color-mode="setColorMode"
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
        v-model:thinking-display="thinkingDisplay"
        :color-mode="colorMode"
        :theme-id="themeId"
        :user-themes="userThemes"
        :open="inspectorOpen"
        @set-color-mode="setColorMode"
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
      <!-- card t_180db2cb: the notifications feed panel seats through its
           own branch like the other shell-owned tabs - the module
           catch-all would render an empty InspectorPanel(module=
           "notifications") body (the feed view is a TAB capsule, not a
           panel; card t_617c2ad0). -->
      <component
        :is="notificationsPanelView"
        v-else-if="notificationsPanelView && inspectorDrawerActiveId === 'notifications'"
        class="pane"
        @open="openNotification" />
      <!-- card t_07ad4fda: agents is a SHELL-owned view (harness/
           continuous ships the component, the shell owns the tab and its
           fetch) - neither a module panel nor a tab capsule, so the
           drawer seats it by its own branch like ExtensionsTab and
           InsightsTab above. The catch-all would resolve
           InspectorPanel(module="agents"), find no entry in the PANEL
           manifest, and render "module not loaded" for a tab that is
           mounted and loaded. -->
      <AgentsPanel
        v-else-if="inspectorDrawerActiveId === 'agents'"
        class="pane" />
      <!-- v0.14.3: the canvas drawer branch left with the tab (the fold
           ruling) - module panels seat through the module catch-all,
           chat + toasts keep their own surfaces -->
      <InspectorPanel
        v-else-if="inspectorDrawerActiveId"
        class="pane"
        :module="inspectorDrawerActiveId"
        :open="inspectorOpen" />
    </MobileDrawer>

    <!-- card t_4bf1d5b7: the BOTTOM drawer. Below 768px the zero-collapse
         CSS (bottompanel.css) takes the .edge-bottom cell out of flow
         regardless of tree state - desktop chrome a mobile toggle can't
         reach. So the menubar's bottom toggle drives THIS overlay (the
         same fixed-drawer + scrim rule the other two ride), not the
         desktop tree reveal: toggleBottomPanel flips bottomDrawerOpen on
         mobile and the tree keeps its shape for the return to desktop.
         Tabs derive from the live bottom pane or the registry's
         bottom-home default (logs), same contract as the other drawers. -->
    <MobileDrawer
      v-if="isMobile"
      side="bottom"
      :open="bottomDrawerOpen"
      :tabs="bottomDrawerTabs"
      aria-label="bottom panel"
      v-model="mobileBottomTab">
      <FilesTab
        v-if="bottomDrawerActiveId === 'files'"
        class="pane"
        :open="bottomDrawerOpen" />
      <SettingsTab
        v-else-if="bottomDrawerActiveId === 'settings'"
        class="pane"
        v-model:show-thinking="showThinking"
        v-model:appearance="appearance"
        v-model:tool-display="toolDisplay"
        v-model:thinking-display="thinkingDisplay"
        :color-mode="colorMode"
        :theme-id="themeId"
        :user-themes="userThemes"
        :open="bottomDrawerOpen"
        @set-color-mode="setColorMode"
        @set-theme="setTheme" />
      <!-- card t_e703180e: a TAB capsule in the bottom drawer rides its
           manifest view. The generic InspectorPanel catch-all below
           resolves the PANEL manifest, where a tab capsule has no entry -
           it would render "module not loaded" for a tab that is loaded. -->
      <component
        :is="observabilityTabView"
        v-else-if="bottomDrawerActiveId === 'observability' && observabilityTabView"
        class="pane"
        :session="focusedSessionId" />
      <!-- card t_07ad4fda: the terminal is a TAB capsule too, and it is
           the drawer's FIRST default bottom-home tab - so it needs the
           same manifest resolution ahead of the catch-all that card
           t_e703180e gave the Logs tab. Without this branch the drawer
           opens on "module not loaded": InspectorPanel(module=
           "terminal") reads the PANEL manifest, where a tab capsule has
           no entry. -->
      <component
        :is="terminalTabView"
        v-else-if="bottomDrawerActiveId === 'terminal' && terminalTabView"
        class="pane"
        :focused-session="focusedSessionId" />
      <InspectorPanel
        v-else-if="bottomDrawerActiveId"
        class="pane"
        :module="bottomDrawerActiveId"
        :open="bottomDrawerOpen" />
    </MobileDrawer>

    <!-- card t_6215474b: the command palette (Ctrl+K / cluster button) -
         fixed top-center above every chrome; it never touches the agent -->
    <CommandPalette
      :open="paletteOpen"
      :commands="paletteCommands"
      @close="paletteOpen = false" />

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
      :up-seconds="upSeconds"
      :workdir="barWorkdir"
      :workdir-target="barWorkdirTarget"
      :pending="pendingCount"
      :socket="socketStatus"
      :provider-reachable="healthProviderReachable"
      :provider-checked-at="healthProviderCheckedAt"
      :provider-detail="healthProviderDetail"
      :database-healthy="healthDatabaseHealthy"
      :database-detail="healthDatabaseDetail"
      :last-error-message="healthLastErrorMessage"
      :last-error-at="healthLastErrorAt"
      @update:workdir="onBarWorkdir"
      @focus-pending="focusPendingCalls" />
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, onUnmounted, provide, ref, watch } from "vue";
  import { modulePanels } from "./composables/module-panels.generated";
  // card t_617c2ad0: the tab capsules' views (terminal / browser /
  // notifications) ride this generated manifest - dockables.ts TAB_MODULES
  // declares the composition, the codegen carries the import.
  import { moduleTabs } from "./composables/module-tabs.generated";
  // card t_48eeb015 (the shell-ring shrink wave 1): the file explorer
  // rides out of the shell - the files capsule ships its own views
  // (harness/files/ui/, the module-panels codegen lists them). app.vue
  // binds the tab view explicitly: files' ui/ carries two .vue views,
  // so the manifest's module lookup alone cannot pick the tab body.
  import FilesTab from "../../harness/files/ui/FilesTab.vue";
  // card t_490fed88 (the shell-ring shrink wave 2): the sessions pane,
  // the roster surface and the insights view ride their capsules the
  // same way (store ships the sessions pane + insights view; continuous
  // ships the roster). Explicit imports like FilesTab - the capsule owns
  // the component, the shell owns the composition (the wiring stays in
  // app.vue's outlets, so removing the capsule fails this build loud -
  // the deletability proof).
  import AppSidebar from "../../harness/store/ui/AppSidebar.vue";
  import InsightsTab from "../../harness/store/ui/InsightsTab.vue";
  import AgentsPanel from "../../harness/continuous/ui/AgentsPanel.vue";
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
    setSessionContext,
    APPEARANCE_KEY,
    isAtBottom,
    pillVisible,
    lastUserText,
    chatPanelMessageCount,
    chatPanelPendingCount,
    transcriptPendingCount,
    chatPanelOf,
    normalizeChatPayload,
    persistedTelemetry,
    formatDurationMs,
    fetchThemes,
    fetchSettings,
    settingValueOf,
    applyThemeTokens,
    clearThemeTokens,
    DEFAULT_THEME_ID,
    COLOR_MODE_KEY,
    THEME_ID_KEY,
    restoreThemeKnobs,
    resolveThemeId,
    type UserTheme,
    type ColorMode,
  } from "./composables/omega";
  import type {
    Session,
    Appearance,
    ModelProviderSection,
  } from "./composables/omega";
  import {
    useToolDisplay,
    TOOL_DISPLAY_KEY,
    type ToolDisplay,
  } from "./composables/toolDisplay";
  // card t_428ed4fb: the thinking block's display knob rides the same
  // shared-ref shape as the tool rows' (composables/thinkingDisplay.ts)
  import {
    useThinkingDisplay,
    THINKING_DISPLAY_KEY,
  } from "./composables/thinkingDisplay";
  // card t_a92450ea: the ONE copy path - insecure-context-proof
  import { copyText } from "./composables/clipboard";
  import {
    createDock,
    findPane,
    findEdgePane,
    findComponentTab,
    isPinnedPane,
    firstChatMainPaneId,
    chatForegroundPlan,
    DOCK_API_KEY,
    DOCK_CHAT_OPEN_KEY,
    DOCK_TAB_TITLE_KEY,
    DOCK_LAYOUT_KEY,
    DOCK_LAYOUT_BAK_KEY,
    nextTabKey,
    type DockApi,
    type DockNode,
    type DockTab,
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
    DOCKABLES,
    defaultHostTabs,
    isClosableTab,
    type DockComponentName,
  } from "./composables/dockables";
  // card t_e76ce7b6: the mobile drawer's pure tab derivation (decoupled
  // from the dock tree - REPRO 1's fix). Card t_4bf1d5b7 adds the
  // menubar toggle route: the shell flags flip BELOW the breakpoint
  // (one drawer at a time), the dock tree only above it.
  import {
    drawerTabsForEdge,
    clampDrawerTab,
    routeShellToggle,
    type MobileShell,
  } from "./composables/mobileDrawer";
  // card t_30137f67: the shared default-workspace value (module-scoped)
  // - SettingsTab's reads keep it fresh, the chat host reads it when a
  // draft state is born, the boot read backfills the early tabs
  import {
    DEFAULT_WORKSPACE_KNOB,
    defaultWorkspaceNow,
    effectiveWorkdir,
    setDefaultWorkspace,
  } from "./composables/defaultWorkspace";
  import {
    CHAT_HOST_KEY,
    CHAT_SESSION_OPEN_EVENT,
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
  import { announceCanvasReset, createCanvasStore, emitLocalToast } from "./composables/canvas";
  import { normalizeCanvasPanel } from "./composables/omega";
  import {
    createSocketClient,
    type HealthPush,
    type SocketClient,
    type SocketStatus,
  } from "./composables/socket";
  import { ingestChipFrame } from "./composables/chips";
  import { refreshNotifications } from "./components/shell/notificationFeed";
  import ChatTab from "./components/ChatTab.vue";
  // card t_6215474b: the command palette overlay (Ctrl+K) + the nav
  // history composable + its command registry
  import CommandPalette from "./components/nav/CommandPalette.vue";
  import {
    createNavHistory,
    locateEntry,
    navTabId,
    type NavEntry,
  } from "./composables/navHistory";
  import {
    buildPaletteCommands,
    type PaletteCommand,
  } from "./composables/commandRegistry";
  import SettingsTab from "./components/settings/SettingsTab.vue";
  // card t_5d9032d3: notification open intents route here from the bell;
  // card t_556ee727: the board select seams (the task row's board pick)
  import type { OmegaNotification } from "./composables/notifications";
  import {
    resolveTaskBoard,
    selectBoardFor,
  } from "./composables/notifications";
  // card t_e1e61680: insights joins the main area (registry: singleton,
  // closable) - the outlet renders it when its tab is active; the view
  // moved to harness/store/ui/ (the wave-2 disclosure: insights is a
  // view over store data and rides the store capsule)
  // card t_180db2cb: the notifications dock tab (the durable feed; the
  // bell entry point is gone) - the view rides the tabs manifest
  // (harness/notifications/ui/, card t_617c2ad0)
  // the roster surface moved to harness/continuous/ui/ (the wave-2
  // continuous capsule)
  // card t_14f73742: the extensions inventory joins the inspector (registry:
  // singleton, pinned right) - the outlet renders it when its tab is active
  import ExtensionsTab from "./components/shell/ExtensionsTab.vue";
  // card t_36ceb99b: the content viewer tab (a file or a skill opened
  // from a row click) - outlet below, opener + titles wired at the
  // provides
  import ViewerTab from "./components/ViewerTab.vue";
  import {
    DOCK_VIEWER_OPEN_KEY,
    FOCUSED_SESSION_KEY,
    VIEWER_OPEN_EVENT,
  } from "./composables/viewer";
  import {
    validViewerRef,
    viewerTabTitle,
    type ViewerRef,
  } from "./composables/dockables";
  const healthy = ref(false);

  // --- the tab capsules' views (card t_617c2ad0): static manifest
  // lookups, not imports. A capsule whose ui/ view is gone (deleted
  // capsule, the deletability proof) yields undefined - the outlet
  // template then hosts nothing, exactly like a panel capsule's
  // InspectorPanel body after a removal.
  const terminalTabView = moduleTabs.find((t) => t.module === "terminal")?.component;
  const observabilityTabView = moduleTabs.find(
    (t) => t.module === "observability",
  )?.component;
  const browserTabView = moduleTabs.find((t) => t.module === "browser")?.component;
  const notificationsPanelView = moduleTabs.find(
    (t) => t.module === "notifications",
  )?.component;
  // /health durability payload: server build tag + uptime (the bar's
  // version + up seg). PUSHED since card t_896cf5a8: the health topic
  // carries the /health body every 5s - the 30s poll is gone. The boot
  // read below still runs once (the dot is honest before the first
  // frame), and a reconnect does one GET /health before trusting the
  // socket again.
  const healthVersion = ref("");
  const upSeconds = ref(0);
  // the health popover's three optional facts (card t_5e2f3095). null is
  // the unsourced state - the popover renders "not reported" - and each
  // is set only when the /health body actually carries it, so a server
  // from before the enrichment keeps today's honest gap.
  const healthProviderReachable = ref<boolean | null>(null);
  const healthProviderCheckedAt = ref(0);
  const healthProviderDetail = ref("");
  const healthDatabaseHealthy = ref<boolean | null>(null);
  const healthDatabaseDetail = ref("");
  const healthLastErrorMessage = ref<string | null>(null);
  const healthLastErrorAt = ref(0);
  // noteHealthFacts copies a /health body's optional facts into the
  // popover's refs. One function for all three read paths (boot fetch,
  // reconnect catch-up, pushed frame) so they can never disagree.
  function noteHealthFacts(h: HealthPush): void {
    if (h.provider && typeof h.provider.reachable === "boolean") {
      healthProviderReachable.value = h.provider.reachable;
      healthProviderCheckedAt.value = h.provider.checked_at ?? 0;
      healthProviderDetail.value = h.provider.detail ?? "";
    }
    if (h.database && typeof h.database.healthy === "boolean") {
      healthDatabaseHealthy.value = h.database.healthy;
      healthDatabaseDetail.value = h.database.detail ?? "";
    }
    if (h.last_error && typeof h.last_error.message === "string") {
      healthLastErrorMessage.value = h.last_error.message;
      healthLastErrorAt.value = h.last_error.at ?? 0;
    }
  }
  const models = ref<string[]>([]);
  const selectedModel = ref("");
  // the GROUPED catalog (card t_1914a8c9): one section per provider -
  // the chat picker renders collapsible sections from these; the
  // picker's Refresh action re-runs the fetch.
  const modelGroups = ref<ModelProviderSection[]>([]);
  async function refreshModels() {
    const m = await fetchModels();
    models.value = m.models;
    modelGroups.value = m.providers ?? [
      { name: "default", models: m.models, capabilities: m.capabilities },
    ];
    if (m.context_window) contextSize.value = m.context_window;
    return m;
  }
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
  // card t_4bf1d5b7: the BOTTOM drawer's mobile open flag - view state
  // only (the overlay drawer rule the other two ride). It never couples
  // to the dock tree: the t_aa9dbada closed-flag semantics and the
  // desktop tree state stay untouched below the breakpoint.
  const bottomDrawerOpen = ref(false);
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
  // thinking display (card t_428ed4fb): auto | expanded | collapsed -
  // v-model binds the SHARED ref (composables/thinkingDisplay.ts) so
  // the settings knob writes the same signal the thinking blocks read,
  // wherever they render. The ref restores from localStorage at module
  // load; this watch keeps the choice persisted (same shape as above).
  const thinkingDisplay = useThinkingDisplay();
  watch(thinkingDisplay, (mode) => {
    localStorage.setItem(THINKING_DISPLAY_KEY, mode);
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
  // card t_6215474b: the RAW store is wrapped below - every ACTIVATION
  // op (focus / openTab / openSingleton / setComponent / openViewer)
  // records the (paneId, tabId) pair into the back/forward history, one
  // record path for tab clicks, opener flows, the palette and the
  // keyboard.
  const rawDock = createDock(window.localStorage);

  // --- back/forward history (card t_6215474b, Endo's ruling 2026-11-02):
  // entries are (paneId, tabId) pairs pushed on tab activation AND pane
  // focus; consecutive duplicates suppress. The activator is the one
  // path back/forward take: it resolves the entry against the live tree
  // (a dead pane/tab returns false and the walk continues past it) and
  // reuses the raw store's focus - a chat entry moves the chat focus too,
  // like the same click would. ---
  const nav = createNavHistory((e) => {
    const i = locateEntry(rawDock.tree.value, e);
    if (i === null) return false;
    const tab = findPane(rawDock.tree.value, e.paneId)?.tabs[i];
    rawDock.focus(e.paneId, i);
    if (tab && tab.component === "chat" && tab.key) focusedKey.value = tab.key;
    return true;
  });

  // THE one record path: the pane's NOW-active tab (a navTabId identity,
  // not an index - indices move on close/reorder, identities survive
  // both). A closed pane or an empty pane records nothing.
  function recordNav(paneId: string) {
    const pane = findPane(rawDock.tree.value, paneId);
    const tab = pane?.tabs[pane.active];
    if (pane && tab) nav.push({ paneId, tabId: navTabId(tab) });
  }

  // locate a tab by predicate - the viewer opener's record rides the
  // tab's payload identity (navTabId of the viewer ref)
  function findNavEntryBy(tree: DockNode, pred: (t: DockTab) => boolean): NavEntry | null {
    if (tree.kind === "pane") {
      const i = tree.tabs.findIndex(pred);
      return i >= 0 ? { paneId: tree.id, tabId: navTabId(tree.tabs[i]!) } : null;
    }
    for (const c of tree.children) {
      const f = findNavEntryBy(c, pred);
      if (f) return f;
    }
    return null;
  }

  const dock: DockApi = {
    ...rawDock,
    focus: (paneId, index) => {
      rawDock.focus(paneId, index);
      recordNav(paneId);
    },
    openTab: (paneId, tab) => {
      rawDock.openTab(paneId, tab);
      recordNav(paneId);
    },
    openSingleton: (paneId, component) => {
      const r = rawDock.openSingleton(paneId, component);
      recordNav(paneId);
      return r;
    },
    setComponent: (paneId, component) => {
      rawDock.setComponent(paneId, component);
      recordNav(paneId);
    },
    openViewer: (v) => {
      rawDock.openViewer(v);
      const landed = findNavEntryBy(
        rawDock.tree.value,
        (t) => t.component === "viewer" && !!t.viewer &&
          t.viewer.kind === v.kind && t.viewer.name === v.name,
      );
      if (landed) nav.push(landed);
    },
  };
  provide(DOCK_API_KEY, dock);
  // card t_d54c718a (v0.14.4 ruling 6): a repaired layout surfaces ONCE
  // at boot - one toast for the whole receipt, never one per repair,
  // never a silent default-tree fallback. The record rides the local
  // emit funnel (canvas.ts emitLocalToast, the same subscriber set +
  // fingerprint dedupe as the canvas feed); read stays true, so the
  // receipt never counts unread in the bell. onMounted puts the emit
  // after the NotificationCenter subscription (children mount first) -
  // the boot itself never waits on this.
  onMounted(() => {
    const repairs = dock.bootRepairs;
    if (repairs.length === 0) return;
    emitLocalToast({
      id: 0,
      kind: "chat",
      source_id: "dock-layout",
      title: `layout repaired: ${repairs.length} constraints eased`,
      verdict: "ok",
      created_at: new Date().toISOString(),
      read: true,
    });
  });
  // card t_8bcbe72e: a reveal refused for lack of room is a no-op in the
  // dock store (the toggle stays closed, zero churn) - the one-line hint
  // keeps the toggle from reading as dead. Same local emit funnel as the
  // boot repair receipt above (read stays true; nothing counts unread).
  watch(
    () => dock.revealRefused.value,
    (comp) => {
      if (!comp) return;
      emitLocalToast({
        id: 0,
        kind: "chat",
        source_id: "dock-layout",
        title: "no room - close a pane first",
        verdict: "ok",
        created_at: new Date().toISOString(),
        read: true,
      });
      dock.revealRefused.value = null;
    },
  );
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
  // below its 320px contract floor and is never overlapped: edge panes
  // collapse until it fits - inspector first, then the sidebar (dock
  // .narrowSqueeze owns the priority and remembers each hidden pane's
  // width for its toggle). card t_66fa358e exception: the pane a user JUST
  // revealed wins the space (last-requested) - the squeeze that re-checks
  // the reveal protects it instead, and pure resizes keep the default
  // order. The content box is the root split's rendered width (the
  // pane-area's padding already excluded). Below the breakpoint the mobile
  // drawers own the layout - the squeeze stands down there.
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
  // the DOM settles. card t_66fa358e: that re-check is the reveal's own
  // squeeze - the just-revealed pane wins the space (the OTHER edge pane
  // gives way to it, last-requested) and only the chat floor is a hard
  // wall when even that is not enough.
  // Idempotent: once the tree fits, no-op - the watch settles immediately.
  watch(
    () => dock.tree.value,
    () => nextTick(applyNarrowSqueeze),
  );

  // arc 3c: the canvas - one store per page (panels are global state,
  // settled Q10a). Boot fetches the snapshot below; live runs feed it
  // deltas through the host's canvasDelta dep.
  const canvas = createCanvasStore();

  // --- the webui shell commands (t_07aed3f9) ---
  //
  // runCommand is the palette's local executor: "/" never reaches the
  // agent. Every verb is a webui-native local handler
  // (minus /exit - nothing to quit). The command output
  // rides the tab's statusLine like every other notice. The function
  // is hoisted above createChatHost only because it closes over `host`
  // (retry); invocations happen after both exist, so the TDZ never
  // bites.
  async function runCommand(stateKey: string, text: string): Promise<boolean> {
    const fields = text.split(/\s+/);
    const name = fields[0] ?? "";
    const arg = fields.slice(1).join(" ").trim();
    const st = host.states.get(stateKey);
    const note = (line: string) => {
      if (st) st.statusLine = line;
    };
    switch (name) {
      case "/help": {
        note(
          "commands: /copy /modules /fork /help /new /panels /retry /sessions /theme /variant - plus module commands and /<skill-name>; @file @session:<id> @skill:<name> on any send",
        );
        return true;
      }
      case "/theme": {
        if (!arg) {
          const user = userThemes.value.map((t) => t.name).join(", ");
          note(`themes: dark, light, auto${user ? ", " + user : ""} (usage: /theme <name>)`);
          return true;
        }
        if (arg === "dark" || arg === "light" || arg === "auto") {
          setTheme(arg);
          note(`theme: ${arg}`);
          return true;
        }
        const user = userThemes.value.find(
          (t) => t.name.toLowerCase() === arg.toLowerCase() || t.id === arg,
        );
        if (user && !user.error) {
          setTheme(`theme:${user.id}`);
          note(`theme: ${user.name}`);
          return true;
        }
        note(`theme: unknown theme "${arg}"`);
        return true;
      }
      case "/fork": {
        // the newest anchored message becomes the next send's FROM point
        const payload = normalizeChatPayload(host.chatPanel()?.payload);
        const rows = payload?.rows ?? [];
        for (let i = rows.length - 1; i >= 0; i--) {
          const r = rows[i]!;
          if (r.role === "assistant" && r.id) {
            host.setFork(stateKey, r.id);
            note("fork: the next send continues from the newest response");
            return true;
          }
        }
        note("fork: no anchored message on this session (use the fork affordance on a response block)");
        return true;
      }
      case "/retry": {
        const ok = await host.retry(stateKey);
        if (ok) return true;
        note("retry: nothing to replay (no idle bound session)");
        return true;
      }
      case "/variant": {
        const variants = st?.variants ?? [];
        if (!variants.length) note("variants: none on the active path");
        else note(`variants: ${variants.length} branch point(s), ${variants.reduce((n, v) => n + v.count, 0)} sibling(s) - use the < n > switcher on a response block`);
        return true;
      }
      case "/copy": {
        const payload = normalizeChatPayload(host.chatPanel()?.payload);
        const rows = payload?.rows ?? [];
        let text2 = "";
        for (let i = rows.length - 1; i >= 0 && !text2; i--) {
          const r = rows[i]!;
          if (r.role === "assistant") {
            text2 = (r.blocks ?? [])
              .filter((b) => b.type === "text" && b.content)
              .map((b) => b.content!)
              .join("\n");
          }
        }
        if (!text2) {
          note("copy: no assistant response to copy");
          return true;
        }
        const ok = await copyText(text2);
        if (!ok) {
          note("copy: clipboard write failed (no secure context and no execCommand fallback)");
          return true;
        }
        note("copied the last response to the clipboard");
        return true;
      }
      case "/new": {
        openChatTab(null);
        return true;
      }
      case "/sessions": {
        if (!sidebarDocked.value) toggleSidebar();
        return true;
      }
      case "/panels":
      case "/modules":
      case "/extensions": {
        // v0.14.3: /panels folds into the Modules tab too - the panels
        // render under their module's expanded row (the fold ruling).
        if (!findEdgePane(dock.tree.value, "inspector")) {
          dock.toggleEdge("inspector", "after");
        }
        openModuleTab("extensions");
        if (!st) return true;
        note(name === "/panels" ? "modules: the panels live under the module rows" : "modules: the cockpit tab is up");
        return true;
      }
      default:
        return false;
    }
  }

  // --- the chat host (card t_4f0e88f2): every chat tab gets its own
  // stream state keyed by the tab's instance key; the gate stays per
  // session (the runHub run slot is per session). ---
  // card t_30137f67: the loaded server.default_workspace rides the
  // host through the shared-value getter (defaultWorkspace.ts, the
  // same value SettingsTab's reads keep fresh) - new drafts are born
  // with the chip (ensureState) and the boot read backfills the
  // pre-resolve ones (seedDefaultWorkspace). No default configured ->
  // "" everywhere.
  const host = createChatHost({
    defaultWorkspace: defaultWorkspaceNow,
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
    // the webui shell commands (t_07aed3f9): theme, panes, fork, retry,
    // copy / new / sessions - all local, never an agent round-trip
    runCommand: (key, text) => runCommand(key, text),
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
      // card t_81109e1c: a session the SIDEBAR list does not carry (an
      // agent home's session - the only door to those is the Agents
      // roster) has no label to read; the id prefix names the tab, so
      // two agent chats never render as two identical "chat" chips.
      return s?.label || `${tab.sessionRef.slice(0, 8)}...`;
    }
    // card t_36ceb99b: the viewer tab is named by its CONTENT (the file
    // base name / the skill name) - the payload rides the tab
    if (tab.component === "viewer" && tab.viewer) {
      return viewerTabTitle(tab.viewer);
    }
    const panel = modulePanels.find((p) => p.module === tab.component);
    return panel?.dockable?.title ?? null;
  });

  // chat is multi-instance: the '+' / empty-pane "new chat" MUST go
  // through the opener (a minted key + openTab), never the raw
  // setComponent - a keyless chat tab would share one state across
  // every keyless tab in the page
  provide(DOCK_CHAT_OPEN_KEY, (paneId, sessionId) => openChatTab(sessionId, paneId));

  // card t_36ceb99b: the viewer opener - the same provide shape the chat
  // opener rides (FilesTab injects; the store's openViewer carries the
  // focus-reuse + ensured-main-pane routing)
  provide(DOCK_VIEWER_OPEN_KEY, (v) => dock.openViewer(v));

  // module-owned openers (the skills tab's RegistryList row click) hand
  // the open over a WINDOW EVENT - module UI imports no shell code (the
  // Board precedent), so the literal is copied in SkillsList and the
  // viewer test pins both spellings. Listener: page-lifetime (app mounts
  // once); the removal is hygiene for hot-reloading harnesses.
  function onViewerOpenEvent(e: Event) {
    const v = (e as CustomEvent).detail;
    if (validViewerRef(v)) {
      dock.openViewer(v as ViewerRef);
    }
  }
  window.addEventListener(VIEWER_OPEN_EVENT, onViewerOpenEvent);
  onUnmounted(() =>
    window.removeEventListener(VIEWER_OPEN_EVENT, onViewerOpenEvent),
  );

  // card t_81109e1c: the Agents tab's row click - the CAPSULE hands the
  // agent's most-recent session id over (module UI imports no shell
  // code; the viewer seam's shape). The open is the ORDINARY chat
  // opener, so a session already in a tab is focused wherever it lives
  // and only an unbound one mints - never two tabs on one session.
  function onChatSessionOpenEvent(e: Event) {
    const id = (e as CustomEvent).detail;
    if (typeof id === "string" && id !== "") openChatTab(id);
  }
  window.addEventListener(CHAT_SESSION_OPEN_EVENT, onChatSessionOpenEvent);
  onUnmounted(() =>
    window.removeEventListener(CHAT_SESSION_OPEN_EVENT, onChatSessionOpenEvent),
  );

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

  // card t_a5f9d185: the focused-session seam - the Files tab (and the
  // viewer it mints) send this ID with every /files request; the server
  // resolves the root from the session row / default_workspace / cwd.
  provide(FOCUSED_SESSION_KEY, focusedSessionId);

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

  // --- the status bar's two zones (card t_8fd38f88) -----------------------
  //
  // LEFT = work context. The workdir selector moved here from the composer
  // (the chip died with it) and renders the SAME value the chip did: the
  // never-blank law lives in composables/defaultWorkspace.ts.
  // RIGHT = live metrics + build; the pending count is the OPEN session's
  // own transcript (the panel payload when it covers the focused session,
  // the REST restore otherwise - the messageCount precedent).
  const focusedChatTab = computed(() =>
    focusedKey.value ? dock.findChatTab(focusedKey.value) : null,
  );
  const barWorkdir = computed(() => {
    const loc = focusedChatTab.value;
    // no chat tab focused: the configured default IS the work context (a
    // new session starts there) and the picker has nothing to write.
    if (!loc) return defaultWorkspaceNow();
    const st = host.states.get(loc.tab.key ?? "");
    return effectiveWorkdir(
      !!st?.sessionId,
      chatSession(loc.tab)?.workspace,
      st?.workspaceIntent,
    );
  });
  const barWorkdirTarget = computed(() => focusedChatTab.value !== null);

  // the write half, byte for byte the composer chip's old two targets: a
  // bound tab pins the session row (the app PATCHes), an unbound tab holds
  // the draft intent the first send consumes.
  function onBarWorkdir(v: string) {
    const loc = focusedChatTab.value;
    if (!loc) return;
    if (loc.tab.sessionRef && loc.tab.sessionRef !== EPHEMERAL_DRAFT_REF) {
      onChatWorkspace(loc.tab, v);
      return;
    }
    const st = host.states.get(loc.tab.key ?? "");
    if (st) st.workspaceIntent = v;
  }

  const pendingCount = computed(() => {
    const payload = chatPanelPayload.value;
    if (payload && payload.session === focusedSessionId.value)
      return chatPanelPendingCount(payload);
    return transcriptPendingCount(focusedChatState.value.restored);
  });

  // the pending segment's click: jump to the pane that hosts the open
  // session (a board view gives the chat outlet back first - the chip
  // lives inside the chat pane's transcript).
  function focusPendingCalls() {
    const loc = focusedChatTab.value;
    if (!loc) return;
    mainView.value = "chat";
    dock.focus(loc.paneId, loc.index);
    activeChatPaneId.value = loc.paneId;
  }

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
    // chat tab that is SHOWING (the active one - t_3e12c367), else the
    // first chat tab in the tree, else none (URL home)
    if (focusedKey.value && dock.findChatTab(focusedKey.value)) return;
    focusedKey.value = (dock.activeChatTab() ?? dock.firstChatTab())?.tab.key ?? null;
  }

  // clicks carry the pane focus: the document-level listener reads the
  // dock-pane id (DnD moves, menus and composer clicks all bubble here)
  // and remembers the LAST FOCUSED pane - the gear's settings tab opens
  // into that pane (VSCode's active-editor-group read, card t_00e4a784).
  const lastPaneId = ref<string | null>(null);
  function onDocClick(e: MouseEvent) {
    const el = e.target instanceof Element ?
      e.target.closest("[data-pane-id]") :
      null;
    const paneId = el?.getAttribute("data-pane-id") ?? null;
    if (paneId) lastPaneId.value = paneId;
    // card t_6215474b: a pane focus records too (ruling 2) - the click
    // seam is where every pane gesture (tab bars, DnD landings, menus,
    // composers) already reports; the dedupe swallows the no-ops
    if (paneId) recordNav(paneId);
    refocusFrom(paneId);
  }

  // the pane the BOARD view hosts into (and chat opens fall back to): an
  // actually chat-hosting pane first - the FOCUSED one, else the first
  // main pane that hosts a chat tab - else the first main pane (never the
  // pinned sidebars). The dock's ensureMainPane re-inserts a flexible pane
  // when the user managed to remove every main pane. card t_96844dcb
  // (journey 98): the middle step matters - a chatless content pane (the
  // browser singleton) sitting FIRST in the tree must not host the board
  // route while a chat-hosting main pane exists.
  function resolveActivePaneId(): string {
    const tree = dock.tree.value;
    const focused = activeChatPaneId.value ?
      findPane(tree, activeChatPaneId.value) :
      null;
    if (focused && !isPinnedPane(focused) && focused.tabs.some((t) => t.component === "chat")) return focused.id;
    const host = firstChatMainPaneId(tree);
    if (host) return host;
    return dock.ensureMainPane();
  }

  // card t_96844dcb (journey 98): every /board activation goes through
  // one door - mainView flips AND the host pane's foreground reclaims to
  // chat. The board folds into the chat pane outlet, which DockPane
  // renders only while the pane's ACTIVE tab is chat: a persisted content
  // pane (the browser singleton left in the foreground by a previous
  // boot) must not shadow the route, or Board.vue never mounts. The plan
  // is pure (chatForegroundPlan); focus stays with the dock, the minted
  // draft with openChatTab.
  function resolveBoardRoute() {
    mainView.value = "board";
    const paneId = resolveActivePaneId();
    const plan = chatForegroundPlan(dock.tree.value, paneId);
    if (plan.mode === "focus") dock.focus(paneId, plan.index);
    else if (plan.mode === "mint") openChatTab(null, paneId);
    boardHostPane.value = paneId;
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

  // openEphemeralTab (card t_d96a5cd5): the session sidebar's "+ New
  // Session" dropdown's no-persist entry (card t_55a571ff: entry label
  // "Ephemeral"). A composer-first tab CARRYING the EPHEMERAL_DRAFT_REF
  // marker
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
    // /board re-enters the kanban view without touching chat focus;
    // enterBoardRoute (t_96844dcb) reclaims the host pane's foreground
    if (/^\/board\/?$/.test(window.location.pathname)) {
      resolveBoardRoute();
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

  // --- the push channel's shell half (card t_896cf5a8) -------------------
  //
  // socket is the page's ONE connection. It is created in onMounted and
  // closed in onUnmounted; every fact the old timers fetched arrives as
  // a frame instead (health, runs, canvas, chips, chat).
  let socket: SocketClient | null = null;
  // the health popover reads the push channel's state ("socket open" vs
  // reconnecting) - the client owns it as a ref, this mirror makes it
  // template-readable from before the client exists (card t_8fd38f88).
  const socketStatus = ref<SocketStatus>("idle");

  // hintNotifications pulls the finished-things + pending-verdict truth
  // on an event hint, debounced: the verdict hub has no topic of its own
  // (issuance and the answer ride chat), and a burst of run frames must
  // not stampede the REST routes. One pull per 1.5s window is plenty for
  // a human-paced question.
  let notificationsHint: ReturnType<typeof setTimeout> | null = null;
  function hintNotifications() {
    if (notificationsHint !== null) return;
    notificationsHint = setTimeout(() => {
      notificationsHint = null;
      void refreshNotifications();
    }, 1500);
  }

  // onRunEdge folds one runs-topic edge into the shell: the host's busy
  // set (the composer's stop face + the tab title spinner), the sidebar
  // row's running flag, and the session LIST. A session this page has
  // never seen is a row created elsewhere (cron, delegate) - the list has
  // no push of its own, so an unknown id refetches it; a run END refetches
  // it for the persisted telemetry and the generated label.
  let sessionsHint: ReturnType<typeof setTimeout> | null = null;
  function hintSessions() {
    if (sessionsHint !== null) return;
    sessionsHint = setTimeout(() => {
      sessionsHint = null;
      void refreshSessions();
    }, 300);
  }
  function onRunEdge(session: string, active: boolean) {
    if (!session) return;
    host.noteRunState(session, active);
    const row = sessions.value.find((s) => s.id === session);
    if (row) row.running = active;
    if (!row || !active) hintSessions();
    // a run edge may issue or settle a verdict card while nothing streams
    // (the pending-list truth rides the REST read, not the edge)
    hintNotifications();
  }

  // catchUp runs on every RECONNECT (never replayed server-side): one
  // health read, then the visible panes through the ordinary GET routes.
  // Events are hints, state is truth.
  async function catchUp() {
    try {
      const h = await fetchHealthInfo();
      healthy.value = h !== null;
      if (h) {
        healthVersion.value = h.version ?? "";
        upSeconds.value = h.uptime_seconds ?? 0;
        noteHealthFacts(h);
      }
    } catch {
      // a failed health read leaves the dot red until the next frame
    }
    void refreshSessions();
    void canvas.refresh();
    void refreshNotifications();
    void host.reconnectCatchUp();
    // the canvas snapshot above refreshes the SHELL's panels; a module
    // view that renders from its own REST route (the kanban board) reads
    // none of them, and the delta it missed while the socket was down is
    // gone (nothing is replayed). Re-announce on the same canvas event so
    // that pane re-reads its own GET route (card t_b5784de3).
    announceCanvasReset();
  }

  // --- the session's remembered context (card t_9de5b009) ---
  //
  // chatSession resolves a tab's binding to its row (the ChatTab props
  // read the pinned model + workspace from it). onChatModel /
  // onChatWorkspace write a picker change THROUGH to the row: the
  // optimistic local edit keeps the chip + sidebar instant, the PATCH
  // persists it, the refresh adopts the server's truth. A draft tab
  // (no binding) only moves the app default model - there is no row to
  // pin on until the first send adopts an id.
  function chatSession(tab: { sessionRef?: string | null }): Session | null {
    if (!tab.sessionRef || tab.sessionRef === EPHEMERAL_DRAFT_REF) return null;
    return sessions.value.find((s) => s.id === tab.sessionRef) ?? null;
  }
  function onChatModel(tab: { sessionRef?: string | null }, v: string) {
    selectedModel.value = v;
    const s = chatSession(tab);
    if (!s || s.model === v) return;
    s.model = v;
    setSessionContext(s.id, v)
      .then(refreshSessions)
      .catch(() => refreshSessions());
  }
  function onChatWorkspace(tab: { sessionRef?: string | null }, v: string) {
    const s = chatSession(tab);
    if (!s || s.workspace === v) return;
    s.workspace = v;
    setSessionContext(s.id, undefined, v)
      .then(refreshSessions)
      .catch(() => refreshSessions());
  }

  // card t_47b776ad: the menubar toggles are BACK - on desktop they
  // drive the dock tree (toggleEdge / toggleBottom: hide/reopen the
  // pinned edge pane at its edge, reopen at the clamp width; a reload
  // re-asserts the fixed shape via ensurePinnedEdges). Card t_4bf1d5b7:
  // below 768px the SAME emits drive the overlay drawer flags (one at a
  // time) and NEVER the tree - the zero-collapse CSS takes the edge
  // cells out of flow regardless of tree state, so a tree flip would be
  // invisible (the bottom toggle's dead click). Returning to desktop
  // finds the layout exactly as the user left it.
  const sidebarDocked = computed(() => findEdgePane(dock.tree.value, "sidebar") !== null);
  // the right shell lives while its marker exists - even empty (free
  // content: tabs come and go, the shell stays)
  const inspectorDocked = computed(() => findEdgePane(dock.tree.value, "inspector") !== null);
  // the bottom shell's desktop open-state icon reads the tree (the pane
  // may be fully closed there - t_aa9dbada); mobile reads the drawer flag
  const bottomDocked = computed(() => findEdgePane(dock.tree.value, "bottom") !== null);

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

  // card t_4bf1d5b7: the bottom drawer's tab set - the live bottom pane
  // when the tree holds one, else the registry's bottom-home default
  // (logs). Its active state is the drawer's own, like the other two.
  const mobileBottomTab = ref(0);
  const bottomDrawerTabs = computed(() =>
    drawerTabsForEdge(
      findEdgePane(dock.tree.value, "bottom"),
      defaultHostTabs("bottom"),
      drawerTabTitle,
    ),
  );
  const bottomDrawerActiveId = computed(() =>
    clampDrawerTab(bottomDrawerTabs.value, mobileBottomTab.value)?.id,
  );

  // card t_14f73742: a has-ui extension row routes to the module's
  // inspector tab - focus its live instance, open it if the strip
  // dropped one (the loader restores the full set anyway). The guard is
  // honest against malformed endpoint payloads: an id outside the
  // registry never reaches the dock.
  function openModuleTab(moduleId: string) {
    if (!(INSPECTOR_TAB_COMPONENTS as readonly string[]).includes(moduleId)) return;
    const pane = findEdgePane(dock.tree.value, "inspector");
    if (!pane) return;
    dock.openSingleton(pane.id, moduleId as DockComponentName);
  }

  // the menubar toggle route: one decision point for all three edges -
  // routeShellToggle (mobileDrawer.ts) flips the dock tree on desktop
  // and the drawer flags on mobile (never both, never the tree below
  // the breakpoint). The node suite pins that contract behaviorally.
  function toggleSidebar() {
    applyShellToggle("sidebar");
  }
  function toggleBottomPanel() {
    applyShellToggle("bottom");
  }
  function applyShellToggle(shell: MobileShell) {
    const next = routeShellToggle(
      shell,
      isMobile.value,
      dock,
      {
        sidebar: drawerOpen.value,
        inspector: inspectorOpen.value,
        bottom: bottomDrawerOpen.value,
      },
    );
    drawerOpen.value = next.sidebar;
    inspectorOpen.value = next.inspector;
    bottomDrawerOpen.value = next.bottom;
  }

  function closeOverlays() {
    drawerOpen.value = false;
    inspectorOpen.value = false;
    bottomDrawerOpen.value = false;
  }

  // mobile: one drawer at a time (the flip lives in applyShellToggle)
  function toggleInspector() {
    applyShellToggle("inspector");
  }

  // --- card t_6215474b: the command palette (Ctrl+K, the cluster button,
  // one overlay) - a plain command array over the shell's own flows; the
  // registry never owns app state (PaletteCommandDeps is the one seam).
  const paletteOpen = ref(false);
  // ONE toggle for both doors: Ctrl+K and the cluster's palette button. The
  // overlay's outside-click handler skips the trigger (`data-palette-trigger`)
  // so this click is the only one that runs - no close-then-reopen flicker.
  function togglePalette() {
    if (deleteId.value) return; // the confirm dialog owns the window then
    paletteOpen.value = !paletteOpen.value;
  }

  // open (or focus) any dockable from the palette - the singleton opener
  // focuses a live instance wherever it sits; a hidden edge is revealed
  // first (the same route openNotification takes), a main dockable mints
  // into the active/ensured main pane.
  function paletteOpenDockable(id: DockComponentName) {
    const area = DOCKABLES.find((d) => d.id === id)?.area ?? "main";
    if (area === "left" && !findEdgePane(dock.tree.value, "sidebar")) {
      dock.toggleEdge("sidebar", "after");
    } else if (area === "right" && !findEdgePane(dock.tree.value, "inspector")) {
      dock.toggleEdge("inspector", "after");
    } else if (area === "bottom" && !findEdgePane(dock.tree.value, "bottom")) {
      dock.toggleBottom();
    }
    const tree = dock.tree.value;
    const found = findComponentTab(tree, id);
    if (found) {
      dock.focus(found.paneId, found.index);
      return;
    }
    const paneId = area === "main"
      ? dock.ensureMainPane()
      : (findEdgePane(tree, area)?.id ?? dock.ensureMainPane());
    dock.openSingleton(paneId, id);
  }

  // card t_00e4a784 (Endo ruling 2026-11-09): the menubar gear's opener -
  // settings is an on-demand MAIN tab (VSCode's settings tab). A live
  // instance focuses in place (never a duplicate); otherwise the tab opens
  // in the LAST FOCUSED main pane - VSCode's active-editor-group read, the
  // same focus memory "close current tab" uses - and falls back to the
  // first main pane (minted when the tree holds none).
  function openSettingsTab() {
    const found = findComponentTab(dock.tree.value, "settings");
    if (found) {
      dock.focus(found.paneId, found.index);
      return;
    }
    const focused = lastPaneId.value ? findPane(dock.tree.value, lastPaneId.value) : null;
    const paneId = focused && !focused.edge ? focused.id : dock.ensureMainPane();
    dock.openSingleton(paneId, "settings");
  }

  // "Switch to recent session" - the newest non-archived row, or the
  // sessions pane when the store is empty.
  function openRecentSessionTab() {
    const rec = sessions.value
      .filter((s) => !s.archived)
      .sort((a, b) => (Date.parse(b.updated_at) || 0) - (Date.parse(a.updated_at) || 0))[0];
    if (rec) openChatTab(rec.id);
    else openSessionsPane();
  }

  function openSessionsPane() {
    const pane = findEdgePane(dock.tree.value, "sidebar");
    if (!pane) {
      dock.toggleEdge("sidebar", "after");
      return;
    }
    const i = pane.tabs.findIndex((t) => t.component === "sidebar");
    dock.focus(pane.id, i >= 0 ? i : pane.active);
  }

  // "Close current tab" - the last-focused pane's active tab, when it is
  // closable; a locked/pinned tab refuses (the X policy reads isClosableTab).
  function paletteCloseCurrent() {
    const paneId = lastPaneId.value ??
      dock.activeChatTab()?.paneId ??
      dock.ensureMainPane();
    const pane = findPane(dock.tree.value, paneId);
    if (!pane) return;
    const tab = pane.tabs[pane.active];
    if (!tab || !isClosableTab(tab.component)) return;
    dock.closeTab(pane.id, pane.active);
  }

  function clearSavedLayout() {
    // the primary record + the repair backup (v0.14.4 law) both die - the
    // reload boots the default shape and re-asserts the pinned edges
    localStorage.removeItem(DOCK_LAYOUT_KEY);
    localStorage.removeItem(DOCK_LAYOUT_BAK_KEY);
    window.location.reload();
  }

  const paletteCommands = computed<readonly PaletteCommand[]>(() =>
    buildPaletteCommands({
      openDockable: paletteOpenDockable,
      openChat: () => openChatTab(null),
      closeCurrentTab: paletteCloseCurrent,
      toggleBottomPanel,
      toggleSidebar: (side) => {
        if (side === "left") toggleSidebar();
        else toggleInspector();
      },
      openSessionsSidebar: openSessionsPane,
      openRecentSession: openRecentSessionTab,
      reloadLayout: () => window.location.reload(),
      clearSavedLayout,
    }),
  );

  // the cluster + keys share the nav's back/forward (one path); the
  // template reads the exposed computeds (top-level refs unwrap there)
  const canBack = nav.canBack;
  const canForward = nav.canForward;

  // Alt+Left / Alt+Right walk the history; Ctrl+K opens the palette
  // (ruling 1). Same paths the buttons emit - nothing here re-implements.
  function onNavKeys(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === "k" && !e.altKey && !e.shiftKey) {
      e.preventDefault();
      togglePalette();
      return;
    }
    if (e.altKey && e.key === "ArrowLeft" && !e.ctrlKey && !e.metaKey) {
      e.preventDefault(); // the browser's own back never fires instead
      nav.back();
      return;
    }
    if (e.altKey && e.key === "ArrowRight" && !e.ctrlKey && !e.metaKey) {
      e.preventDefault();
      nav.forward();
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

  // --- theme: color mode + theme, two knobs, both persisted ---
  // Color mode (auto/dark/light) picks the built-in palette; auto
  // follows the OS live. Theme ("default" or a themes-folder file id)
  // rides the color mode: default applies the built-in palette the
  // color mode chose; a file theme applies ITS base + token overrides
  // on top. A vanished or broken file falls back to default without
  // touching the mode. ---
  const colorMode = ref<ColorMode>("auto");
  const themeId = ref<string>(DEFAULT_THEME_ID);
  let systemPrefersDark = true;
  let mediaQuery: MediaQueryList | null = null;
  const userThemes = ref<UserTheme[]>([]);
  // the list starts empty and loads async - applyTheme's vanished-file
  // fallback may only fire after the first fetch settled, or a boot-time
  // watch on the restored id would reset it against the not-yet-loaded
  // list and the persisted pick would be lost on every reload
  let userThemesLoaded = false;

  function applyTheme() {
    if (typeof document === "undefined") return;
    if (themeId.value !== DEFAULT_THEME_ID) {
      const theme = userThemes.value.find((t) => t.id === themeId.value && !t.error);
      if (theme) {
        // overrides stack on the theme file's chosen base palette
        document.documentElement.dataset.theme = theme.base;
        applyThemeTokens(document.documentElement.style, theme.tokens);
        return;
      }
      if (userThemesLoaded) {
        // the theme file vanished or turned invalid: fall back to the
        // default theme; the color mode is never touched
        themeId.value = DEFAULT_THEME_ID;
        localStorage.setItem(THEME_ID_KEY, DEFAULT_THEME_ID);
      }
      // still loading: keep the pick, the built-in palette applies
      // until the refreshed list re-applies the file theme
    }
    clearThemeTokens(document.documentElement.style);
    const dark =
      colorMode.value === "auto" ? systemPrefersDark : colorMode.value === "dark";
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  }

  function setColorMode(mode: ColorMode) {
    colorMode.value = mode;
    localStorage.setItem(COLOR_MODE_KEY, mode);
    applyTheme();
  }

  function setTheme(id: string) {
    themeId.value = id;
    localStorage.setItem(THEME_ID_KEY, id);
    applyTheme();
  }

  watch(colorMode, applyTheme);
  watch(themeId, applyTheme);
  // a user theme edited (or deleted) on disk re-applies live the moment
  // the refreshed list lands - the drawer refetches on every open, so
  // an edit reflects on the next apply without restart or reload
  watch(
    userThemes,
    () => {
      if (themeId.value !== DEFAULT_THEME_ID) applyTheme();
    },
    { deep: true },
  );

  // the gear is BACK (card t_00e4a784): settings opens from the menubar
  // button as an on-demand main tab. The refetch-on-open rides the open
  // transition of the settings singleton, however it was opened (the
  // palette's "Open Settings" is the second door).
  watch(
    () => dock.has("settings"),
    (present, was) => {
      if (present && !was) void loadUserThemes();
    },
  );

  // loadUserThemes refreshes the picker's list. Fetched on mount (so a
  // persisted theme id applies) and on every settings open (so a
  // dropped or edited file shows up without a restart). A failure just
  // empties the list - the pane keeps its built-in modes.
  async function loadUserThemes() {
    try {
      userThemes.value = await fetchThemes();
    } catch {
      userThemes.value = [];
    } finally {
      // the first fetch settled: the vanished-file fallback may fire -
      // a failure just empties the list (the pane keeps its built-ins)
      userThemesLoaded = true;
    }
    // a file id the folder no longer serves clean falls back to the
    // built-in; the persisted pair rewrites only on the miss
    const resolved = resolveThemeId(themeId.value, userThemes.value);
    if (resolved !== themeId.value) {
      themeId.value = resolved;
      localStorage.setItem(THEME_ID_KEY, resolved);
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
    // card t_6215474b: Ctrl+K palette + Alt+Left/Right history walk
    window.addEventListener("keydown", onNavKeys);
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

    // restore the persisted knobs; the old merged select's value
    // migrates once (restoreThemeKnobs): stored theme:<id> -> theme
    // <id> + mode auto, a stored plain mode -> theme default + the
    // mode. Both keys are written back in the new shape immediately -
    // the second boot reads only the new pair.
    const restored = restoreThemeKnobs(
      localStorage.getItem(THEME_ID_KEY),
      localStorage.getItem(COLOR_MODE_KEY),
    );
    colorMode.value = restored.mode;
    themeId.value = restored.theme;
    localStorage.setItem(COLOR_MODE_KEY, restored.mode);
    localStorage.setItem(THEME_ID_KEY, restored.theme);
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

    // --- the push channel (card t_896cf5a8): ONE websocket per page
    // load carries every live fact the pollers used to fetch. Events
    // are hints, state is truth: a frame updates the view, and a
    // reconnect refetches the visible panes through the GET routes.
    socket = createSocketClient({
      health: (s) => {
        healthy.value = (s.status ?? "ok") === "ok";
        if (typeof s.version === "string") healthVersion.value = s.version;
        if (typeof s.uptime_seconds === "number") upSeconds.value = s.uptime_seconds;
        noteHealthFacts(s);
      },
      runs: (session, active) => onRunEdge(session, active),
      canvas: (_event, payload) => {
        const frame = payload as { type?: string; panel?: unknown } | null;
        if (!frame || typeof frame !== "object" || frame.type !== "view_model_delta") return;
        const panel = normalizeCanvasPanel(frame.panel);
        if (panel) canvas.applyDelta(panel);
      },
      chips: (_session, frame) => ingestChipFrame(frame),
      chat: (session, event, payload) => {
        host.noteSocketChat(session, event, payload);
        // the verdict gap (t_896cf5a8): issuance and the answer ride the
        // chat topic's structural frames - a chatty token delta is not a
        // signal, so only the call/turn edges pull the pending list
        if (
          event === "tool_call" ||
          event === "tool_result" ||
          event === "turn_end" ||
          event === "agent_end"
        )
          hintNotifications();
      },
      open: ({ reconnect }) => {
        if (!reconnect) return;
        // nothing replayed server-side: one health read, then the
        // visible panes - the same GET routes a cold load uses
        void catchUp();
      },
    });
    socket.connect();
    // the popover's socket line follows the client's own state ref
    watch(socket.status, (v) => (socketStatus.value = v), { immediate: true });
    // boot liveness: the dot + the sidebar render before the first
    // pushed frame (up to 5s of health cadence)
    const h = await fetchHealthInfo();
    healthy.value = h !== null;
    if (h) {
      healthVersion.value = h.version ?? "";
      upSeconds.value = h.uptime_seconds ?? 0;
      noteHealthFacts(h);
    }
    // the health interval is GONE (card t_896cf5a8): the health topic
    // pushes the same body every 5s, and a reconnect does one plain read
    // the models fetch does NOT block boot: a slow or stalled provider
    // /models round-trip (shared keypool stall) used to hold the whole
    // sidebar hostage. Fire it off and let the picker fill in when it
    // lands - the session list renders independent of provider reachability.
    void refreshModels()
      .then((m) => {
        selectedModel.value = m.current;
      })
      .catch(() => {
        // models are decoration until a chat runs
      });
    // card t_30137f67: the boot read of server.default_workspace - the
    // SAME /settings payload the Settings tab renders (shared
    // fetchSettings read, no second config path). It feeds the shared
    // signal SettingsTab's reads keep fresh, and seedDefaultWorkspace
    // backfills the unbound drafts born before this resolved. A host
    // without the settings seam (501) or a failed read leaves "" - the
    // unconfigured behavior, the chip starts blank.
    void fetchSettings()
      .then((p) => {
        setDefaultWorkspace(settingValueOf(p, DEFAULT_WORKSPACE_KNOB));
        if (defaultWorkspaceNow()) host.seedDefaultWorkspace(defaultWorkspaceNow());
      })
      .catch(() => {
        // no settings seam or unreachable server: no default known
      });
    refreshSessions();
    // the running-state poll is GONE (card t_896cf5a8): the runs topic
    // pushes one session's live-run truth on every run-slot edge, and
    // onRunEdge folds it into the pulse + the sidebar row
    // the canvas snapshot: the load-time catch-up (settled Q3b) - the
    // panels mounted before this page loaded arrive here; everything
    // later rides the canvas topic's deltas, so the 15s between-runs
    // poll is gone too (a toast or a board change now reaches an idle
    // page). The snapshot stays the cold-load read.
    void canvas.refresh();

    // boot focus: the URL's session owns it ("reload lands on the
    // focused session") - an existing chat tab bound to that id is
    // focused in place, otherwise a tab opens into the active pane
    // (unknown/deleted ids and home route through the persisted chat
    // focus). Then the ChatTab mount restores + attaches per tab.
    // /board boots into the kanban view through the same door as the
    // popstate route (t_96844dcb: the foreground reclaims to chat - a
    // persisted browser pane must not shadow the board on a fresh boot).
    if (/^\/board\/?$/.test(window.location.pathname)) {
      resolveBoardRoute();
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
        // card t_3e12c367: the carried focus is the tree's ACTIVE chat
        // tab (the pane's active tab), not the first chat tab in tree
        // order - the shipped default pane holds a sessionless draft at
        // index 0 while the user's session tab is active beside it, and
        // a draft pick left the focused-session seam null (the Files
        // tab's boot re-root never fired - it stayed on the fallback).
        focusedKey.value = (dock.activeChatTab() ?? dock.firstChatTab())?.tab.key ?? null;
      }
    } else if (initial) {
      if (await sessionExists(initial)) openChatTab(initial);
      else {
        history.replaceState({}, "", "/");
        // t_3e12c367: carried focus = the ACTIVE chat tab (see above)
        focusedKey.value = (dock.activeChatTab() ?? dock.firstChatTab())?.tab.key ?? null;
      }
    } else {
      // home: the persisted tree's chat focus carries over
      focusedKey.value = (dock.activeChatTab() ?? dock.firstChatTab())?.tab.key ?? null;
    }
  });

  onUnmounted(() => {
    document.removeEventListener("click", onDocClick);
    window.removeEventListener("keydown", onKeyCapture);
    window.removeEventListener("keydown", onNavKeys);
    window.removeEventListener("resize", onWinResize);
    areaObserver?.disconnect();
    window.removeEventListener("popstate", onPop);
    if (socket) {
      socket.close();
      socket = null;
    }
    if (notificationsHint !== null) clearTimeout(notificationsHint);
    if (sessionsHint !== null) clearTimeout(sessionsHint);
    if (spinnerTimer) clearInterval(spinnerTimer);
    if (typeof document !== "undefined" && savedTitle !== null)
      document.title = savedTitle;
  });
</script>