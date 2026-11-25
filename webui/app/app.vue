<template>
  <div class="shell">
    <MenuBar
      :sidebar-open="isMobile ? drawerOpen : sidebarDocked"
      :bottom-open="isMobile ? bottomDrawerOpen : bottomDocked"
      :inspector-open="isMobile ? inspectorOpen : inspectorDocked"
      :can-back="canBack"
      :can-forward="canForward"
      :palette-open="paletteOpen"
      @back="nav.back()"
      @forward="nav.forward()"
      @toggle-sidebar="toggleSidebar"
      @toggle-bottom="toggleBottomPanel"
      @toggle-inspector="toggleInspector"
      @open-palette="togglePalette"
      @open-settings="openSettingsTab"
      @open-notification="openNotification" />

    <!-- drawer scrim: sibling of pane-area, NOT nested inside it. -->
    <div
      v-if="isMobile && (drawerOpen || inspectorOpen || bottomDrawerOpen)"
      class="scrim"
      @click="closeOverlays" />

    <div class="pane-area">
      <DockSplit :node="dock.tree.value">
        <template #pane-sidebar>
          <!-- the edge-pane content is desktop chrome: below 768px the mobile drawer
               renders the same views independently, so a sidebar pruned from the tree
               never blanks the drawer -->
          <AppSidebar
            v-if="!isMobile"
            class="pane"
            :sessions="sessions"
            :active-id="focusedSessionId"
            :streaming-ids="busyAll"
            :open="drawerOpen"
            :draft-focused="draftConversationFocused"
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

        <!-- one ChatTab per chat tab, keyed by the tab's instance key so a DnD move
             remounts cleanly and a switch never renders another tab's transcript -->
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

        <template #pane-files>
          <FilesTab
            v-if="!isMobile"
            class="pane"
            :open="inspectorOpen" />
        </template>
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
        <!-- the terminal tab: the shell keeps running while a sibling tab activates -
             the outlet unmounts the SSE, never the pty -->
        <template #pane-terminal>
          <component
            :is="terminalTabView"
            v-if="terminalTabView && !isMobile"
            class="pane"
            :focused-session="focusedSessionId" />
        </template>
        <template #pane-observability>
          <component
            :is="observabilityTabView"
            v-if="observabilityTabView && !isMobile"
            class="pane"
            :session="focusedSessionId" />
        </template>
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

        <!-- a row name click opens the agent's most-recent session in a chat tab (window
             event -> openChatTab); the per-agent session LIST never renders here -->
        <template #pane-agents>
          <AgentsPanel
            v-if="!isMobile"
            class="pane" />
        </template>

        <!-- settings is an ON-DEMAND main-area tab: the gear opens it, it closes like any
             tab, and the pane renders wherever the tab lives (mobile included - the main
             pane is the one surface that never collapses) -->
        <template #pane-settings="{ pane }">
          <SettingsTab
            class="pane"
            :can-close="settingsClosable(pane)"
            v-model:show-thinking="showThinking"
            v-model:appearance="appearance"
            v-model:tool-display="toolDisplay"
            v-model:thinking-display="thinkingDisplay"
            :color-mode="colorMode"
            :theme-id="themeId"
            :user-themes="userThemes"
            @open-module="openModuleTab"
            @close="closeSettingsTab(pane)"
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
        <template #pane-kanban>
          <InspectorPanel
            v-if="!isMobile"
            class="pane"
            module="kanban"
            :open="isMobile ? inspectorOpen : false" />
        </template>
        <template #pane-insights>
          <InsightsTab
            v-if="!isMobile"
            class="pane" />
        </template>
        <template #pane-notifications>
          <component
            :is="notificationsPanelView"
            v-if="notificationsPanelView && !isMobile"
            class="pane"
            @open="openNotification" />
        </template>
      </DockSplit>
    </div>

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
        :draft-focused="draftConversationFocused"
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
      <!-- agents' home is the PRIMARY sidebar now (the seat map decides, not this file) -->
      <AgentsPanel
        v-else-if="sidebarDrawerActiveId === 'agents'"
        class="pane" />
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
        @open-module="openModuleTab"
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
        @open-module="openModuleTab"
        @set-color-mode="setColorMode"
        @set-theme="setTheme" />
      <InsightsTab
        v-else-if="inspectorDrawerActiveId === 'insights'"
        class="pane" />
      <component
        :is="notificationsPanelView"
        v-else-if="notificationsPanelView && inspectorDrawerActiveId === 'notifications'"
        class="pane"
        @open="openNotification" />
      <!-- agents is a SHELL-owned view (the component ships from harness/) -->
      <AgentsPanel
        v-else-if="inspectorDrawerActiveId === 'agents'"
        class="pane" />
      <InspectorPanel
        v-else-if="inspectorDrawerActiveId"
        class="pane"
        :module="inspectorDrawerActiveId"
        :open="inspectorOpen" />
    </MobileDrawer>

    <!-- the BOTTOM drawer. Below 768px the zero-collapse CSS (bottompanel.css) takes the
         .edge-bottom cell out of flow regardless of tree state, so the tree never reaches
         it. Tabs derive from the live bottom pane or the registry's bottom-home default
         (logs), the same contract as the other drawers. -->
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
        @open-module="openModuleTab"
        @set-color-mode="setColorMode"
        @set-theme="setTheme" />
      <!-- a TAB capsule in the bottom drawer rides its manifest view -->
      <component
        :is="observabilityTabView"
        v-else-if="bottomDrawerActiveId === 'observability' && observabilityTabView"
        class="pane"
        :session="focusedSessionId" />
      <!-- the terminal is a TAB capsule too, and the drawer's FIRST default -->
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

    <!-- the command palette: fixed top-center above every chrome, and it never touches the
         agent. Its FIELD is not rendered here - it teleports into the top bar's palette
         seat, leaving this panel as the results dropdown hanging below the bar. -->
    <CommandPalette
      :open="paletteOpen"
      :in-place="!isMobile"
      :commands="paletteCommands"
      @close="paletteOpen = false" />

    <!-- THE context menu: exactly one instance for the window. Surfaces register their rows
         into the store; nothing here is surface-specific. -->
    <ContextMenu />

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
      :tokens-estimated="tokensEstimated"
      :cache-percent="cachePercent"
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
      :current-origin="currentOrigin"
      :hidden-stats="dock.hiddenStats.value"
      :provider-reachable="healthProviderReachable"
      :provider-checked-at="healthProviderCheckedAt"
      :provider-detail="healthProviderDetail"
      :database-healthy="healthDatabaseHealthy"
      :database-detail="healthDatabaseDetail"
      :last-error-message="healthLastErrorMessage"
      :last-error-at="healthLastErrorAt"
      @update:workdir="onBarWorkdir"
      @toggle-stat="dock.setStatHidden"
      @focus-pending="focusPendingCalls" />
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, onUnmounted, provide, ref, watch } from "vue";
  import { modulePanels } from "./composables/module-panels.generated";
  import { moduleTabs } from "./composables/module-tabs.generated";
  // app.vue binds the tab views explicitly: files' ui/ carries two.vue views, so the
  // manifest's module lookup alone cannot pick the tab body.
  import FilesTab from "../../harness/files/ui/FilesTab.vue";
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
    listedSessions,
    markSessionRead,
    parseSessionPath,
    sessionPath,
    isSettingsPath,
    settingsAddress,
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
    fetchSettingKnob,
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
    PendingVerdictCard,
  } from "./composables/omega";
  import {
    useToolDisplay,
    TOOL_DISPLAY_KEY,
    type ToolDisplay,
  } from "./composables/toolDisplay";
  import {
    useThinkingDisplay,
    THINKING_DISPLAY_KEY,
  } from "./composables/thinkingDisplay";
  // the ONE copy path - insecure-context-proof
  import { copyText } from "./composables/clipboard";
  import {
    createDock,
    edgeOfArea,
    findPane,
    findEdgePane,
    findComponentTab,
    isPinnedPane,
    lastTabLocked,
    firstChatMainPaneId,
    chatForegroundPlan,
    DOCK_API_KEY,
    DOCK_CHAT_OPEN_KEY,
    DOCK_TAB_TITLE_KEY,
    DOCK_DRAFT_FOCUS_KEY,
    isDraftChatKey,
    DOCK_LAYOUT_KEY,
    DOCK_LAYOUT_BAK_KEY,
    nextTabKey,
    type DockApi,
    type DockNode,
    type DockTab,
  } from "./composables/dock";
  import { useBottomWidthMode } from "./composables/bottomWidth";
  // the palette's shell set: /resume resolves a session reference the same way the store
  // does, so the webui and the terminal loop answer alike
  import { resolveSessionRef } from "./composables/commands";
  import {
    INSPECTOR_TAB_COMPONENTS,
    PRIMARY_TAB_COMPONENTS,
    COMPONENT_TITLES,
    DOCKABLES,
    compositionModuleIds,
    defaultHostTabs,
    isClosableTab,
    moduleGated,
    type DockComponentName,
    type ModuleGate,
  } from "./composables/dockables";
  // the ONE module-state store.
  import {
    hydrateModuleState,
    loadModules,
    modulesOff,
  } from "./composables/moduleState";
  import {
    drawerTabs,
    DRAWER_RENDERABLE,
    clampDrawerTab,
    routeShellToggle,
    type MobileShell,
  } from "./composables/mobileDrawer";
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
    FOCUSED_SESSION_EVENT,
    FOCUSED_SESSION_QUERY_EVENT,
    createChatHost,
    type ChatState,
  } from "./composables/chat";
  import {
    COMPOSER_FOCUS_KEY,
    createComposerFocusSignal,
    shouldCaptureTyping,
  } from "./composables/focus";
  // the model-switch dialog's open flag: its keystrokes are its own, never the composer's
  import { modelSwitchDialogOpen } from "./composables/modelSwitch";
  // the canvas store: the webui's one view-model state. Panel presence lives in the
  // store, never a hardcoded list.
  import { announceCanvasReset, createCanvasStore, emitLocalToast } from "./composables/canvas";
  import { normalizeCanvasPanel } from "./composables/omega";
  import {
    createSocketClient,
    type HealthPush,
    type SocketClient,
    type SocketStatus,
  } from "./composables/socket";
  import { ingestChipFrame } from "./composables/chips";
  import {
    notificationFeedSettled,
    refreshNotifications,
  } from "./components/shell/notificationFeed";
  import ChatTab from "./components/ChatTab.vue";
  import CommandPalette from "./components/nav/CommandPalette.vue";
  import ContextMenu from "./components/ui/ContextMenu.vue";
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
  // the MODULES category id + the palette's jump signal.
  import { MODULES_CATEGORY } from "./components/settings/settingsNav";
  import { jumpToSettingsSection } from "./composables/settingsJump";
  import type { OmegaNotification } from "./composables/notifications";
  import {
    resolveTaskBoard,
    selectBoardFor,
  } from "./composables/notifications";
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

  // the tab capsules' views are static manifest lookups, not imports. A capsule whose ui/
  // view is gone yields undefined, so its outlet hosts nothing - exactly like a panel
  // capsule's InspectorPanel body after a removal.
  const terminalTabView = moduleTabs.find((t) => t.module === "terminal")?.component;
  const observabilityTabView = moduleTabs.find(
    (t) => t.module === "observability",
  )?.component;
  const browserTabView = moduleTabs.find((t) => t.module === "browser")?.component;
  const notificationsPanelView = moduleTabs.find(
    (t) => t.module === "notifications",
  )?.component;
  const healthVersion = ref("");
  const upSeconds = ref(0);
  const healthProviderReachable = ref<boolean | null>(null);
  const healthProviderCheckedAt = ref(0);
  const healthProviderDetail = ref("");
  const healthDatabaseHealthy = ref<boolean | null>(null);
  const healthDatabaseDetail = ref("");
  const healthLastErrorMessage = ref<string | null>(null);
  const healthLastErrorAt = ref(0);
  // noteHealthFacts copies a /health body's optional facts into the popover's refs. One
  // function for all three read paths (boot fetch, reconnect catch-up, pushed frame) so
  // they can never disagree.
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
  // the GROUPED catalog: one section per provider.
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
  // the status bar's connection control compares a saved row against the page's own
  // origin; a pre-render pass has no window, so it starts empty and the control
  // says "This instance".
  const currentOrigin = typeof window === "undefined" ? "" : window.location.origin;
  // the BOTTOM drawer's mobile open flag - never coupled to the dock tree, so the
  // desktop tree state stays untouched below the breakpoint.
  const bottomDrawerOpen = ref(false);
  const showThinking = ref(true);
  const appearance = ref<Appearance>("chat");
  const toolDisplay = useToolDisplay();
  watch(toolDisplay, (mode) => {
    localStorage.setItem(TOOL_DISPLAY_KEY, mode);
  });
  const thinkingDisplay = useThinkingDisplay();
  watch(thinkingDisplay, (mode) => {
    localStorage.setItem(THINKING_DISPLAY_KEY, mode);
  });
  watch(appearance, (a) => {
    localStorage.setItem(APPEARANCE_KEY, a);
  });
  const contextSize = ref(0);

  const moduleOwnedIds = compositionModuleIds(modulePanels, moduleTabs);
  function currentModuleGate(): ModuleGate {
    return { owned: moduleOwnedIds, off: modulesOff.value };
  }
  hydrateModuleState(window.localStorage);
  const rawDock = createDock(window.localStorage);
  // the gate applies to the loaded tree BEFORE the first render, then again on change.
  rawDock.setModuleGate(currentModuleGate());
  watch(
    () => modulesOff.value,
    () => rawDock.setModuleGate(currentModuleGate()),
  );

  // back/forward history: entries are (paneId, tabId) pairs pushed on tab activation
  // AND pane focus; consecutive duplicates suppress.
  const nav = createNavHistory((e) => {
    const i = locateEntry(rawDock.tree.value, e);
    if (i === null) return false;
    const tab = findPane(rawDock.tree.value, e.paneId)?.tabs[i];
    rawDock.focus(e.paneId, i);
    if (tab && tab.component === "chat" && tab.key) focusedKey.value = tab.key;
    return true;
  });

  // THE one record path: the pane's NOW-active tab.
  function recordNav(paneId: string) {
    const pane = findPane(rawDock.tree.value, paneId);
    const tab = pane?.tabs[pane.active];
    if (pane && tab) nav.push({ paneId, tabId: navTabId(tab) });
  }

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
  // a repaired layout surfaces ONCE at boot - one toast for the whole record, never one
  // per repair and never a silent default-tree fallback. It rides the local emit funnel
  // (canvas.ts emitLocalToast: same subscriber set and fingerprint dedupe as the canvas
  // feed), and read stays true so it never counts unread in the notifications
  // feed. onMounted puts the emit after the NotificationCenter subscription
  // (children mount first).
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
  // ONE module-state read at boot. A host with no toggle seam or a transient failure
  // leaves the gate on the cached set - nothing is hidden that the runtime never owned.
  onMounted(() => {
    loadModules().catch(() => {
    });
  });
  // a reveal refused for lack of room is a no-op in the dock store.
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
  // the sidebars are FIXED panes: no open/close state exists on desktop.

  const bottomWidthMode = useBottomWidthMode();
  function clearBottomInsets(area: HTMLElement) {
    area.style.removeProperty("--bottom-inset-left");
    area.style.removeProperty("--bottom-inset-right");
    area.style.removeProperty("--bottom-sidebars-overhang");
  }
  function applyBottomWidth() {
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
    const bandH = Math.max(0, Math.round(bottom.offsetHeight));
    area.style.setProperty(
      "--bottom-sidebars-overhang",
      -(bandH + 4) + "px",
    );
  }
  // re-measure the pass after the DOM catches up with the layout change
  watch([() => dock.tree.value, bottomWidthMode], () => nextTick(applyBottomWidth));

  // the window-narrow give-way. The main pane never dips below its 320px contract floor
  // and is never overlapped: edge panes collapse until it fits - inspector first, then the
  // sidebar (dock.narrowSqueeze owns the priority and remembers each hidden width).
  function applyNarrowSqueeze() {
    if (isMobile.value) return;
    const root = document.querySelector<HTMLElement>(".pane-area > .dock-root");
    if (!root) return;
    dock.narrowSqueeze(root.clientWidth);
  }
  function onWinResize() {
    applyBottomWidth();
    applyNarrowSqueeze();
  }
  let areaObserver: ResizeObserver | null = null;

  // the give-way also runs after any STRUCTURAL tree change. Idempotent: once the tree
  // fits, no-op - the watch settles immediately.
  watch(
    () => dock.tree.value,
    () => nextTick(applyNarrowSqueeze),
  );

  // the canvas: one store per page (panels are global state).
  const canvas = createCanvasStore();

  // runCommand is the palette's local executor: "/" never reaches the agent. It sits above
  // createChatHost only because it closes over `host`; invocations happen after both exist,
  // so the TDZ never bites.
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
        // the reference surface is the palette (Ctrl+K), whose list carries every shell
        // command with its description; the status line answers with the pointer. Opening,
        // never toggling: /help always leaves the reference UP.
        paletteOpen.value = true;
        note("the palette is the command reference - it is open");
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
        // /new never forks a conversation the MACHINERY owns (an agent's canonical); the
        // human's own conversations fork as they always did - a fresh tab.
        const id = focusedSessionId.value;
        if (id) {
          try {
            const res = await fetch(`/sessions/${encodeURIComponent(id)}/new`, {
              method: "POST",
            });
            if (res.ok) {
              const body = (await res.json()) as { kind?: string; compacted?: number; summary?: boolean };
              if (body.kind === "compacted") {
                note(
                  body.summary
                    ? "compacted this conversation - the summary heads the next context"
                    : "compacted this conversation - its context starts fresh here",
                );
                await canvas.refresh();
                return true;
              }
            }
          } catch {
            /* a compact that never landed falls through to the fork */
          }
        }
        openChatTab(null);
        return true;
      }
      // /resume opens a session HERE: the pane is the shell's, so the shell is the door.
      // #N counts the rows the sessions pane renders (listedSessions); the fall-through for
      // an id and a label prefix is resolveSessionRef's, the same rule the store answers.
      // The OPEN TAB is the answer (exactly like /new) - only a refusal needs a line.
      case "/resume": {
        if (!arg) {
          note("resume: usage /resume <#|id|label> (# counts the rows in the sessions pane)");
          return true;
        }
        const hit = resolveSessionRef(listedSessions(sessions.value), arg);
        if (!hit) {
          note(`resume: no session matches "${arg}"`);
          return true;
        }
        openChatTab(hit.id);
        return true;
      }
      // /branch creates the child through the store's REST parity AND opens it - a server
      // action could create the row but never a tab. The child's own tab is the receipt.
      case "/branch": {
        const parent = arg || focusedSessionId.value;
        if (!parent) {
          note("branch: no session in this tab - /resume <#|id|label> first, or /branch <id>");
          return true;
        }
        const child = await branchChatSession(parent);
        if (!child) note(`branch: the server refused to branch ${parent}`);
        return true;
      }
      // /label writes the row the sidebar renders, so the label lands where the human sees
      // it - no server-side action, no second copy of the write.
      case "/label": {
        const id = focusedSessionId.value;
        if (!id) {
          note("label: no session in this tab - the label rides a conversation");
          return true;
        }
        const ok = await renameChatSession(id, arg);
        if (!ok) {
          note(`label: the write for ${id} failed - the sidebar still shows the old label`);
          return true;
        }
        note(arg ? `[label: ${arg}]` : "[label cleared]");
        return true;
      }
      case "/sessions": {
        if (!sidebarDocked.value) toggleSidebar();
        return true;
      }
      case "/panels":
      case "/modules":
      case "/extensions": {
        // module toggles are CONFIGURATION, not content.
        openSettingsTab();
        jumpToSettingsSection(MODULES_CATEGORY);
        if (!st) return true;
        note(name === "/panels" ? "modules: the panels live under the module rows" : "modules: settings > modules is up");
        return true;
      }
      default:
        return false;
    }
  }

  // the chat host: every chat tab gets its own stream state, keyed by its state key.
  const host = createChatHost({
    defaultWorkspace: defaultWorkspaceNow,
    setTabSessionRef: (key, sessionId) => dock.setSessionRef(key, sessionId),
    refreshSessions: () => refreshSessions(),
    sessionRow: (id) => sessions.value.find((s) => s.id === id),
    markRead: (id) => markRead(id),
    isFocused: (key) => focusedKey.value === key,
    canvasDelta: (panel) => canvas.applyDelta(panel),
    chatPanel: () => chatPanelOf(canvas.panels),
    canvasRefresh: () => canvas.refresh(),
    // the webui shell commands: theme, panes, fork, retry, copy / new / sessions - all
    // local, never an agent round-trip
    runCommand: (key, text) => runCommand(key, text),
  });

  // main-view: chat vs the module-owned board (kanban). The board renders in ONE pane (the
  // active chat pane) until it gets its own dockable slot.
  const mainView = ref<"chat" | "board">("chat");
  const boardComponent = computed(
    () => modulePanels.find((p) => p.module === "kanban")?.component,
  );
  const boardHostPane = ref<string>("");

  // The address bar names the settings tab while it is up. Settings is still a CLOSABLE
  // main tab, so the claim is a FOCUS claim, never a pinned view: every chat focus hands
  // the path back and the chat derivation below resumes.
  const settingsRoute = ref(false);

  /**
   * claimSettingsPath: the settings tab names the address bar now. The write is the CLAIM,
   * not a side effect of the open - a landing that already arrived on the path writes
   * nothing (its query carries the deep-linked section, SettingsTab's to keep), and
   * replaceState is the only history call: the app never grows the stack.
   */
  function claimSettingsPath() {
    settingsRoute.value = true;
    if (isSettingsPath(window.location.pathname)) return;
    history.replaceState({}, "", settingsAddress(window.location.search));
  }

  /** settingsTabMounted: the landing's own view is still in the dock. */
  function settingsTabMounted(): boolean {
    return findComponentTab(dock.tree.value, "settings") !== null;
  }

  /**
   * landSettingsRoute: the settings path is up. mainView returns to chat for the reason the
   * board route sets it too - the panes must render, or a /board overlay left up by an
   * earlier landing would shadow the settings tab. The claim here is a no-op write (the
   * path already is the settings one) and keeps the mark in ONE writer.
   */
  function landSettingsRoute() {
    claimSettingsPath();
    mainView.value = "chat";
    openSettingsTab();
  }

  function openBoardSession(id: string) {
    mainView.value = "chat";
    openChatTab(id);
  }

  // the notification open intent routes by source kind. Chat content never changes on the
  // click.
  function openNotification(n: OmegaNotification | unknown) {
    // a pending APPROVAL rides this router too.
    const card = n as PendingVerdictCard | null;
    if (card && typeof card.session_id === "string") {
      paletteOpenDockable("notifications");
      return;
    }
    const rec = n as OmegaNotification;
    if (!rec || typeof rec.kind !== "string") return;
    switch (rec.kind) {
      case "task": {
        if (!findEdgePane(dock.tree.value, "inspector")) {
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

  // the notification's board crosses as an event (the stash + CustomEvent in
  // composables/notifications.ts) because module UI imports no shell code; the stash
  // covers the mount race, since the tab activates on the tick AFTER openSingleton.
  async function focusNotificationBoard(taskId: string) {
    const board = await resolveTaskBoard(taskId);
    if (!board) return;
    selectBoardFor(board);
  }
  provide(CHAT_HOST_KEY, host);

  const composerFocus = createComposerFocusSignal();
  provide(COMPOSER_FOCUS_KEY, composerFocus);

  provide(DOCK_TAB_TITLE_KEY, (tab) => {
    if (tab.component === "chat") {
      // ephemeral tabs never match a session row (store-invisible): named by contract
      if (tab.sessionRef === EPHEMERAL_DRAFT_REF || tab.sessionRef?.startsWith("eph-")) {
        return "ephemeral";
      }
      if (!tab.sessionRef) return "new chat";
      const s = sessions.value.find((x) => x.id === tab.sessionRef);
      // a session the SIDEBAR list does not carry (an agent home's - the Agents roster is
      // its only door) has no label: the id prefix names the tab, so two agent chats never
      // render as two identical "chat" chips.
      return s?.label || `${tab.sessionRef.slice(0, 8)}...`;
    }
    // the viewer tab is named by its CONTENT (the file base name / the skill name)
    if (tab.component === "viewer" && tab.viewer) {
      return viewerTabTitle(tab.viewer);
    }
    const panel = modulePanels.find((p) => p.module === tab.component);
    return panel?.dockable?.title ?? null;
  });

  // chat is multi-instance: "new chat" MUST go through the opener (a minted key +
  // openTab), never the raw setComponent - a keyless chat tab would share one state with
  // every other keyless tab in the page.
  provide(DOCK_CHAT_OPEN_KEY, (paneId, sessionId) => openChatTab(sessionId, paneId));

  provide(DOCK_VIEWER_OPEN_KEY, (v) => dock.openViewer(v));

  // module-owned openers (a skills row click) hand the open through this event.
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

  // The Agents tab's row click, through the ORDINARY chat opener: a session already in a
  // tab is focused wherever it lives and only an unbound one mints - never two tabs on one
  // session.
  function onChatSessionOpenEvent(e: Event) {
    const id = (e as CustomEvent).detail;
    if (typeof id === "string" && id !== "") openChatTab(id);
  }
  window.addEventListener(CHAT_SESSION_OPEN_EVENT, onChatSessionOpenEvent);
  onUnmounted(() =>
    window.removeEventListener(CHAT_SESSION_OPEN_EVENT, onChatSessionOpenEvent),
  );

  // The URL owns ONLY the focused chat session: focusing a chat tab replaceStates
  // /chat/<id> (unsaved = /), while focus moving to a non-chat tab or pane leaves the URL
  // at the last focused chat session. Layout and tab state NEVER enter the URL, and there
  // is no pushState - popstate only fires for entries outside the SPA.
  const focusedKey = ref<string | null>(null);
  // the pane a NEW chat tab opens into: the last pane whose active tab was focused as a
  // chat (a sidebar/bucket click never steals opens - it is not a chat focus)
  const activeChatPaneId = ref<string | null>(null);

  const focusedChatState = computed<ChatState>(() =>
    focusedKey.value ? host.states.get(focusedKey.value) ?? DEFAULT_CHAT_STATE : DEFAULT_CHAT_STATE,
  );
  const focusedSessionId = computed(() => focusedChatState.value.sessionId);

  // THE NEW-CONVERSATION DIM, provided once for every door.
  const draftConversationFocused = computed(() =>
    isDraftChatKey(dock.tree.value, focusedKey.value),
  );
  provide(DOCK_DRAFT_FOCUS_KEY, draftConversationFocused);

  function announceFocusedSession(id: string | null | undefined): void {
    window.dispatchEvent(
      new CustomEvent(FOCUSED_SESSION_EVENT, { detail: id ?? "" }),
    );
  }
  watch(focusedSessionId, (id) => announceFocusedSession(id), {
    flush: "post",
  });
  // the OPENING announcement: a panel that mounts while a session already has the focus
  // never sees a change
  onMounted(() => announceFocusedSession(focusedSessionId.value));

  function onFocusedSessionQuery(): void {
    announceFocusedSession(focusedSessionId.value);
  }
  window.addEventListener(FOCUSED_SESSION_QUERY_EVENT, onFocusedSessionQuery);
  onUnmounted(() =>
    window.removeEventListener(
      FOCUSED_SESSION_QUERY_EVENT,
      onFocusedSessionQuery,
    ),
  );

  provide(FOCUSED_SESSION_KEY, focusedSessionId);

  const chatPanelPayload = computed(() => {
    const panel = chatPanelOf(canvas.panels);
    return panel ? normalizeChatPayload(panel.payload) : null;
  });
  const tokens = computed(() => focusedChatState.value.tokens);
  const tokensEstimated = computed(() => focusedChatState.value.tokensEstimated);
  const cachePercent = computed(() => focusedChatState.value.cachePercent);
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

  // the status bar's LEFT zone = work context. The workdir selector moved here from the
  // composer chip and renders the same value it did: the never-blank law lives in
  // composables/defaultWorkspace.ts.
  const focusedChatTab = computed(() =>
    focusedKey.value ? dock.findChatTab(focusedKey.value) : null,
  );
  const barWorkdir = computed(() => {
    const loc = focusedChatTab.value;
    // no chat tab focused: the configured default IS the work context.
    if (!loc) return defaultWorkspaceNow();
    const st = host.states.get(loc.tab.key ?? "");
    return effectiveWorkdir(
      !!st?.sessionId,
      chatSession(loc.tab)?.workspace,
      st?.workspaceIntent,
    );
  });
  const barWorkdirTarget = computed(() => focusedChatTab.value !== null);

  // the write half: the row when bound, else the draft's intent.
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
        // a chat tab in focus is chat's claim on the URL again
        settingsRoute.value = false;
        return;
      }
      if (tab?.component === "settings") claimSettingsPath();
    }
    const keep = focusedKey.value && dock.findChatTab(focusedKey.value) ? focusedKey.value : null;
    if (!keep) focusedKey.value = (dock.activeChatTab() ?? dock.firstChatTab())?.tab.key ?? null;
    // This branch resolves a CHAT focus, so it is chat's claim on the URL: closing the
    // settings tab from a /settings/ landing lands here and must let the path go, or the
    // paused derivation freezes the URL on the landing and a reload re-opens it. The
    // release reads LIVE dock state - a settings tab still MOUNTED is the landing itself -
    // and adopts the tab the dock is SHOWING, so the URL names the chat the user is
    // looking at. The mark falls FIRST so the focus transition reaches the composer gate.
    if (settingsRoute.value && !settingsTabMounted()) {
      settingsRoute.value = false;
      const showing = dock.activeChatTab();
      if (showing?.tab.key) focusedKey.value = showing.tab.key;
    }
  }

  const lastPaneId = ref<string | null>(null);
  function onDocClick(e: MouseEvent) {
    if (
      e.target instanceof Element &&
      e.target.closest(".command-palette, #nav-palette-slot")
    )
      return;
    const el = e.target instanceof Element ?
      e.target.closest("[data-pane-id]") :
      null;
    const paneId = el?.getAttribute("data-pane-id") ?? null;
    if (paneId) lastPaneId.value = paneId;
    if (paneId) recordNav(paneId);
    refocusFrom(paneId);
  }

  // the pane the BOARD view hosts into (and chat opens fall back to): an actually
  // chat-hosting pane first - the FOCUSED one, else the first main pane that hosts a
  // chat tab - else the first main pane (never the pinned sidebars). (journey 98): the
  // middle step matters - a chatless content pane (the browser singleton) sitting FIRST
  // in the tree must not host the board route while a chat-hosting main pane exists.
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

  // every /board activation goes through one door: the board folds into the chat pane
  // outlet, which DockPane renders only while the pane's ACTIVE tab is chat, so a
  // persisted content pane must not shadow the route or Board.vue never mounts.
  function resolveBoardRoute() {
    mainView.value = "board";
    const paneId = resolveActivePaneId();
    const plan = chatForegroundPlan(dock.tree.value, paneId);
    if (plan.mode === "focus") dock.focus(paneId, plan.index);
    else if (plan.mode === "mint") openChatTab(null, paneId);
    boardHostPane.value = paneId;
  }

  // openChatTab is THE tab-opener. THE ROUTING LAW: a session ALWAYS opens in the main
  // pane and a tab already bound to it is FOCUSED wherever it lives - never a duplicate.
  // The target is the ACTIVE center chat pane (resolveActivePaneId), falling back to the
  // main column; a pane that ASKED for the tab (a sidebar's "+" via DOCK_CHAT_OPEN_KEY) is
  // honored only when it is a main pane, so a sidebar never hosts a session. An open DRAFT
  // tab is focused wherever it sits, never duplicated.
  function openChatTab(sessionId: string | null, paneId?: string) {
    // a chat tab taking the focus is chat's claim on the URL again.
    settingsRoute.value = false;
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
    const pane = paneId ? dock.sessionPane(paneId) : resolveActivePaneId();
    const key = nextTabKey();
    dock.openTab(pane, {
      component: "chat",
      ...(sessionId ? { sessionRef: sessionId } : {}),
      key,
    });
    focusedKey.value = key;
    if (isMobile.value) drawerOpen.value = false;
    // open == read is owned by the ChatTab mount's load.
  }

  // openEphemeralTab: the sidebar's "+ New Session" - always mints fresh, never reuses a
  // draft (a plain draft and an ephemeral one are different contracts).
  function openEphemeralTab() {
    // same URL claim as openChatTab.
    settingsRoute.value = false;
    const pane = resolveActivePaneId();
    const key = nextTabKey();
    dock.openTab(pane, { component: "chat", sessionRef: EPHEMERAL_DRAFT_REF, key });
    focusedKey.value = key;
    if (isMobile.value) drawerOpen.value = false;
  }

  // popstate re-targets to the URL's session.
  async function onPop() {
    if (isSettingsPath(window.location.pathname)) {
      landSettingsRoute();
      return;
    }
    if (/^\/board\/?$/.test(window.location.pathname)) {
      resolveBoardRoute();
      return;
    }
    mainView.value = "chat";
    settingsRoute.value = false;
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
    const first = dock.firstChatTab();
    if (first?.tab.key) {
      dock.focus(first.paneId, first.index);
      focusedKey.value = first.tab.key;
    } else {
      openChatTab(null);
    }
  }

  // URL follows focus by derivation: the focused tab's bound session (unsaved = /).
  const urlPath = computed(() => {
    const id = focusedChatState.value.sessionId;
    return id ? sessionPath(id) : "/";
  });
  watch(urlPath, (p) => {
    // /board and the settings claim both own the path while they are up: a reload of
    // /settings/ must not turn into /chat/<id> a tick later.
    if (mainView.value === "board") return;
    if (settingsRoute.value) return;
    history.replaceState({}, "", p);
  });
  // A released claim hands the URL back by WRITING what the pause held; the pathname check
  // keeps that write to the case the claim actually owns.
  watch(settingsRoute, (up) => {
    if (up) return;
    if (!isSettingsPath(window.location.pathname)) return;
    history.replaceState({}, "", urlPath.value);
  });
  // structural changes never steal focus, but a gone focus re-resolves.
  watch(dock.tree, () => refocusFrom(null));

  // composer auto-focus on select. A transition to null never requests; moving to
  // board/settings never changes focusedKey, so those views never steal.
  watch(focusedKey, (key) => {
    // the settings landing keeps the boot chat focus out of the way: the path names
    // settings, so the chat pane must not grab the caret
    if (key && mainView.value === "chat" && !settingsRoute.value) composerFocus.request(key);
  });

  // type-to-capture: the first printable key jumps into the focused chat's composer. A
  // hidden chat tab renders no matching instance, so a key that is nowhere on screen simply
  // does not capture.
  function onKeyCapture(e: KeyboardEvent) {
    if (
      !shouldCaptureTyping(e, {
        mainView: mainView.value,
        modalOpen: !!deleteId.value || modelSwitchDialogOpen.value,
      })
    )
      return;
    const key = focusedKey.value;
    if (key) composerFocus.request(key);
  }

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
      host.noteServerActive(list.filter((s) => s.running).map((s) => s.id));
    } catch {
    }
  }

  // socket is the page's ONE push channel.
  let socket: SocketClient | null = null;
  const socketStatus = ref<SocketStatus>("idle");

  // hintNotifications pulls finished-things + pending-verdict truth on an event hint,
  // debounced: the verdict hub has no topic of its own (issuance rides the verdict_issued
  // chat frame, the answer rides tool_result), and a burst of run frames must not stampede
  // the REST routes.
  let notificationsHint: ReturnType<typeof setTimeout> | null = null;
  function hintNotifications() {
    if (notificationsHint !== null) return;
    notificationsHint = setTimeout(() => {
      notificationsHint = null;
      void refreshNotifications();
    }, 1500);
  }

  // onRunEdge folds one runs-topic edge into the shell. A session this page has never seen
  // is a row created elsewhere (cron, delegate) - the list has no push of its own - so an
  // unknown id refetches it; a run END refetches for the persisted telemetry and label.
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
    hintNotifications();
  }

  // catchUp runs on every RECONNECT (never replayed server-side).
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
    }
    void refreshSessions();
    void canvas.refresh();
    void refreshNotifications();
    void host.reconnectCatchUp();
    // the canvas snapshot above refreshes the SHELL's panels.
    announceCanvasReset();
  }

  // the session's remembered context --- chatSession resolves a tab's binding.
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

  // the menubar toggles are BACK - on desktop they drive the dock tree (toggleEdge /
  // toggleBottom: hide/reopen the pinned edge pane at its edge, reopen at the clamp
  // width; a reload re-asserts the fixed shape via ensurePinnedEdges).: below 768px the
  // SAME emits drive the overlay drawer flags (one at a time) and NEVER the tree - the
  // zero-collapse CSS takes the edge cells out of flow regardless of tree state, so a
  // tree flip would be invisible (the bottom toggle's dead click). Returning to desktop
  // finds the layout exactly as the user left it.
  const sidebarDocked = computed(() => findEdgePane(dock.tree.value, "sidebar") !== null);
  const inspectorDocked = computed(() => findEdgePane(dock.tree.value, "inspector") !== null);
  const bottomDocked = computed(() => findEdgePane(dock.tree.value, "bottom") !== null);

  // the mobile drawer's tab bar. The drawer renders the pinned-edge content
  // independently of the dock tree (REPRO 1: a desktop-closed sidebar prunes the edge
  // pane - the drawer must still open with content), so its tab set reads the live edge
  // pane when it exists and joins the registry floor's missing ids when it does not.
  // The active tab is the drawer's OWN state - switching it never touches the dock tree
  // (the desktop hidden state is page-lifetime by design and must not be coupled to the
  // mobile drawer, REPRO 1's root cause).
  const mobileSidebarTab = ref(0);
  const mobileInspectorTab = ref(0);

  function drawerTabTitle(id: DockComponentName): string {
    // the same voice the dock tabbar uses: a module dockable.json title
    // wins, the registry title is the fallback (chat never reaches here)
    const panel = modulePanels.find((p) => p.module === id);
    return panel?.dockable?.title ?? COMPONENT_TITLES[id];
  }

  // the drawer's tab set is CLAMPED to the ids its own branch chain resolves. Every
  // edge-home tab stays reachable: drawerTabs unions the live pane with the registry
  // floor, and the floor is exactly the home strip.
  function drawerKeeps(shell: MobileShell, id: DockComponentName): boolean {
    if ((DRAWER_RENDERABLE[shell] as readonly string[]).includes(id)) return true;
    // the generic catch-all IS the honest resolution for a module panel
    return modulePanels.some((p) => p.module === id);
  }
  function drawerTabsFor(shell: MobileShell, floor: readonly DockComponentName[]) {
    return drawerTabs(
      findEdgePane(dock.tree.value, shell),
      floor,
      drawerTabTitle,
      (id) => drawerKeeps(shell, id),
    );
  }

  // canvas C1: the primary sidebar's registry floor - its class members
  // (sessions, agents, cron), gated modules skipped exactly like the
  // inspector floor below (the drawer is a second face of the same strip)
  const sidebarFloor = computed(() =>
    PRIMARY_TAB_COMPONENTS.filter((id) => !moduleGated(id, currentModuleGate())),
  );
  const sidebarDrawerTabs = computed(() => drawerTabsFor("sidebar", sidebarFloor.value));
  const inspectorFloor = computed(() =>
    INSPECTOR_TAB_COMPONENTS.filter((id) => !moduleGated(id, currentModuleGate())),
  );
  const inspectorDrawerTabs = computed(() =>
    drawerTabsFor("inspector", inspectorFloor.value),
  );

  // the active tab id, clamped against the live tab set (the edge pane's
  // tabs can change under a DnD while the drawer is closed; the drawer
  // never indexes past the end)
  const sidebarDrawerActiveId = computed(() =>
    clampDrawerTab(sidebarDrawerTabs.value, mobileSidebarTab.value)?.id,
  );
  const inspectorDrawerActiveId = computed(() =>
    clampDrawerTab(inspectorDrawerTabs.value, mobileInspectorTab.value)?.id,
  );

  const mobileBottomTab = ref(0);
  const bottomFloor = computed(() =>
    defaultHostTabs("bottom").filter(
      (id) => !moduleGated(id, currentModuleGate()),
    ),
  );
  const bottomDrawerTabs = computed(() =>
    drawerTabsFor("bottom", bottomFloor.value),
  );
  const bottomDrawerActiveId = computed(() =>
    clampDrawerTab(bottomDrawerTabs.value, mobileBottomTab.value)?.id,
  );

  // a has-ui extension row and a notification's target both come through here. canvas
  // C1: WHERE it opens follows the seat map, not this function - the class assigns the
  // home shell, so a primary-sidebar view (cron, agents) lands in the left shell
  // exactly like an inspector view lands in the right one. Main-class ids keep their
  // own doors (the chat opener, the gear) - a wire payload never mints those. The guard
  // is honest against malformed endpoint payloads: an id outside the registry never
  // reaches the dock.: a module the runtime reports OFF has no surface to open - the
  // gate refuses the route instead of minting a dead pane.
  function openModuleTab(moduleId: string) {
    const entry = DOCKABLES.find((d) => d.id === moduleId);
    if (!entry) return;
    if (moduleGated(moduleId, currentModuleGate())) return;
    if (edgeOfArea(entry.area) === null) return;
    paletteOpenDockable(entry.id);
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

  // the command palette (Ctrl+K, the cluster button, one overlay) - a plain command
  // array over the shell's own flows; the registry never owns app state
  // (PaletteCommandDeps is the one seam).
  const paletteOpen = ref(false);
  // ONE toggle for both doors: Ctrl+K and the cluster's palette button.
  function togglePalette() {
    if (deleteId.value) return; // the confirm dialog owns the window then
    paletteOpen.value = !paletteOpen.value;
  }

  function paletteOpenDockable(id: DockComponentName) {
    if (id === "settings") claimSettingsPath();
    const area = DOCKABLES.find((d) => d.id === id)?.area ?? "main";
    // canvas C1: the view's HOME decides the reveal and the seat - through ONE adapter
    // (edgeOfArea), never a raw zone name handed to findEdgePane (which speaks shell
    // components; a zone name would answer the bottom pane)
    const shell = edgeOfArea(area);
    if (shell === "sidebar" && !findEdgePane(dock.tree.value, "sidebar")) {
      dock.toggleEdge("sidebar", "after");
    } else if (shell === "inspector" && !findEdgePane(dock.tree.value, "inspector")) {
      dock.toggleEdge("inspector", "after");
    } else if (shell === "bottom" && !findEdgePane(dock.tree.value, "bottom")) {
      dock.toggleBottom();
    }
    const tree = dock.tree.value;
    const found = findComponentTab(tree, id);
    if (found) {
      dock.focus(found.paneId, found.index);
      return;
    }
    const paneId = shell
      ? (findEdgePane(tree, shell)?.id ?? dock.ensureMainPane())
      : dock.ensureMainPane();
    dock.openSingleton(paneId, id);
  }

  // the menubar gear's opener. A live instance focuses in place (never a duplicate);
  // otherwise the tab opens in the LAST FOCUSED main pane - VSCode's
  // active-editor-group read, the same focus memory "close current tab" uses - and
  // falls back to the first main pane (minted when the tree holds none).
  function openSettingsTab() {
    claimSettingsPath();
    const found = findComponentTab(dock.tree.value, "settings");
    if (found) {
      dock.focus(found.paneId, found.index);
      return;
    }
    const focused = lastPaneId.value ? findPane(dock.tree.value, lastPaneId.value) : null;
    const paneId = focused && !focused.edge ? focused.id : dock.ensureMainPane();
    dock.openSingleton(paneId, "settings");
  }

  // the settings tab's own right-click Close: the SAME refusal its X reads - a pane holding
  // only Settings is the locked last tab, so no row is offered there.
  function settingsClosable(paneId: string): boolean {
    const pane = findPane(dock.tree.value, paneId);
    if (!pane || lastTabLocked(pane)) return false;
    return pane.tabs.some((t) => t.component === "settings");
  }

  function closeSettingsTab(paneId: string) {
    const pane = findPane(dock.tree.value, paneId);
    if (!pane || lastTabLocked(pane)) return;
    const index = pane.tabs.findIndex((t) => t.component === "settings");
    if (index < 0) return;
    dock.closeTab(pane.id, index);
  }

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

  function paletteCloseCurrent() {
    const paneId = lastPaneId.value ??
      dock.activeChatTab()?.paneId ??
      dock.ensureMainPane();
    const pane = findPane(dock.tree.value, paneId);
    if (!pane) return;
    const tab = pane.tabs[pane.active];
    if (!tab || !isClosableTab(tab.component)) return;
    if (pane.tabs.length <= 1) return; // the locked last tab
    dock.closeTab(pane.id, pane.active);
  }

  function clearSavedLayout() {
    // the primary record + the repair backup (v0.14.4 law) both die.
    localStorage.removeItem(DOCK_LAYOUT_KEY);
    localStorage.removeItem(DOCK_LAYOUT_BAK_KEY);
    window.location.reload();
  }

  //
  // ACTIVE chat tab - the same host.command path a typed "/theme" takes
  // (one router, one status line). No chat tab anywhere means no status
  // line to answer on, so the verb runs unbound rather than silently
  // doing nothing - the palette is shell-wide, not chat-scoped.
  function runSlashCommand(name: string): void {
    const key = dock.activeChatTab()?.tab.key ?? focusedKey.value ?? "";
    if (key) void host.command(key, name);
    else void runCommand("", name);
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
      //
      // opens it, so the "/" verbs (name + description, from commands.ts)
      // ride the same list and run through the shell's own executor.
      runSlashCommand,
      //
      // focus - the same fact every other new-conversation door reads
      draftFocused: draftConversationFocused.value,
      hiddenDockable: (id) => moduleGated(id, currentModuleGate()),
    }),
  );

  const canBack = nav.canBack;
  const canForward = nav.canForward;

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

  // togglePin flips the pinned flag on a session row.
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

  // toggleArchive flips the archive flag.
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

  // branchSession flow: POST creates the child. The child's id comes back so a narrator
  // (the /branch palette command) can name what it opened; null = the store refused.
  async function branchChatSession(id: string): Promise<string | null> {
    let child: Session | null = null;
    try {
      child = await branchSession(id);
    } catch {
      return null;
    }
    if (!child) return null;
    await refreshSessions();
    openChatTab(child.id);
    return child.id;
  }

  // renameChatSession writes the label the sidebar renders; true = the server took it.
  async function renameChatSession(id: string, label: string): Promise<boolean> {
    const updateList = (list: Session[]) =>
      list.map((s) => (s.id === id ? { ...s, label } : s));
    const before = sessions.value;
    sessions.value = updateList(sessions.value);
    try {
      await renameSession(id, label);
    } catch {
      sessions.value = before;
      refreshSessions();
      return false;
    }
    return true;
  }

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
      // a failed DELETE leaves the row in place - the refresh shows the truth
      refreshSessions();
      return;
    }
    dock.closeSession(id);
    host.closeSession(id);
    refreshSessions();
  }

  // sessionExists gates the boot/popstate restore.
  async function sessionExists(id: string): Promise<boolean> {
    try {
      await fetchTranscript(id);
      return true;
    } catch {
      return false;
    }
  }

  const colorMode = ref<ColorMode>("auto");
  const themeId = ref<string>(DEFAULT_THEME_ID);
  let systemPrefersDark = true;
  let mediaQuery: MediaQueryList | null = null;
  const userThemes = ref<UserTheme[]>([]);
  let userThemesLoaded = false;

  function applyTheme() {
    if (typeof document === "undefined") return;
    if (themeId.value !== DEFAULT_THEME_ID) {
      const theme = userThemes.value.find((t) => t.id === themeId.value && !t.error);
      if (theme) {
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
  watch(
    userThemes,
    () => {
      if (themeId.value !== DEFAULT_THEME_ID) applyTheme();
    },
    { deep: true },
  );

  // the gear is BACK: settings opens from the menubar button as an on-demand main tab.
  watch(
    () => dock.has("settings"),
    (present, was) => {
      if (present && !was) void loadUserThemes();
    },
  );

  // loadUserThemes refreshes the picker's list.
  async function loadUserThemes() {
    try {
      userThemes.value = await fetchThemes();
    } catch {
      userThemes.value = [];
    } finally {
      userThemesLoaded = true;
    }
    const resolved = resolveThemeId(themeId.value, userThemes.value);
    if (resolved !== themeId.value) {
      themeId.value = resolved;
      localStorage.setItem(THEME_ID_KEY, resolved);
    }
    applyTheme();
  }

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
    document.addEventListener("click", onDocClick);
    window.addEventListener("keydown", onKeyCapture);
    window.addEventListener("keydown", onNavKeys);
    // the give-way belt: the resize event alone misses layout changes that never.
    // Idempotent: once the tree fits, no-op.
    const areaEl = document.querySelector(".pane-area");
    if (areaEl && typeof ResizeObserver !== "undefined") {
      areaObserver = new ResizeObserver(() => applyNarrowSqueeze());
      areaObserver.observe(areaEl);
    }
    window.addEventListener("resize", onWinResize);
    void nextTick(onWinResize); // the boot pass (the watch needs a change)

    const mobileQuery = window.matchMedia("(max-width: 768px)");
    isMobile.value = mobileQuery.matches;
    mobileQuery.addEventListener("change", (e) => {
      isMobile.value = e.matches;
      closeOverlays(); // crossing the breakpoint resets overlay state
    });

    const restored = restoreThemeKnobs(
      localStorage.getItem(THEME_ID_KEY),
      localStorage.getItem(COLOR_MODE_KEY),
    );
    colorMode.value = restored.mode;
    themeId.value = restored.theme;
    localStorage.setItem(COLOR_MODE_KEY, restored.mode);
    localStorage.setItem(THEME_ID_KEY, restored.theme);
    void loadUserThemes();

    appearance.value = parseAppearance(localStorage.getItem(APPEARANCE_KEY));

    mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    systemPrefersDark = mediaQuery.matches;
    mediaQuery.addEventListener("change", (e) => {
      systemPrefersDark = e.matches;
      applyTheme();
    });
    applyTheme();

    // history wiring: popstate re-targets via the URL's session id
    window.addEventListener("popstate", onPop);

    // the push channel: ONE websocket per page load carries every live fact.
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
        if (
          event === "tool_call" ||
          event === "tool_result" ||
          event === "turn_end" ||
          event === "agent_end" ||
          event === "verdict_issued"
        )
          hintNotifications();
      },
      open: ({ reconnect }) => {
        if (!reconnect) return;
        void catchUp();
      },
    });
    socket.connect();
    watch(socket.status, (v) => (socketStatus.value = v), { immediate: true });
    const h = await fetchHealthInfo();
    healthy.value = h !== null;
    if (h) {
      healthVersion.value = h.version ?? "";
      upSeconds.value = h.uptime_seconds ?? 0;
      noteHealthFacts(h);
    }
    // the health interval is GONE. That is the marker's whole job: a journey recipe
    // cannot tell a boot read from a poller (`ab wait --load networkidle` returns
    // during hydration - measured 1.5-2.3s of network quiet between the last static
    // resource and the first app fetch at 8x CPU throttle), so the shell publishes the
    // state instead of leaving the witness to guess. Set only after every read RESOLVES
    // - issued is not settled, which is exactly the distinction that produced three
    // nightly reds of 111-socket-push.
    const bootReads: Promise<unknown>[] = [
      notificationFeedSettled(),
      // the models fetch does NOT block boot.
      refreshModels()
        .then((m) => {
          selectedModel.value = m.current;
        })
        .catch(() => {
        }),
      // the boot read of server.default_workspace.
      fetchSettingKnob(DEFAULT_WORKSPACE_KNOB)
        .then((knob) => {
          setDefaultWorkspace(knob?.value);
          if (defaultWorkspaceNow()) host.seedDefaultWorkspace(defaultWorkspaceNow());
        })
        .catch(() => {
        }),
      refreshSessions(),
      // the running-state poll is GONE.
      canvas.refresh(),
    ];
    void Promise.allSettled(bootReads).then(() => {
      document.documentElement.dataset.omegaBooted = "1";
    });

    // boot focus: the URL's session owns it ("reload lands on the focused session").
    if (isSettingsPath(window.location.pathname)) {
      landSettingsRoute();
    } else if (/^\/board\/?$/.test(window.location.pathname)) {
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
        // the carried focus is the tree's ACTIVE chat tab (the pane's active tab), not
        // the first chat tab in tree order - the shipped default pane holds a
        // sessionless draft at index 0 while the user's session tab is active beside
        // it, and a draft pick left the focused-session seam null (the Files tab's boot
        // re-root never fired - it stayed on the fallback).
        focusedKey.value = (dock.activeChatTab() ?? dock.firstChatTab())?.tab.key ?? null;
      }
    } else if (initial) {
      if (await sessionExists(initial)) openChatTab(initial);
      else {
        history.replaceState({}, "", "/");
        // carried focus = the ACTIVE chat tab (see above)
        focusedKey.value = (dock.activeChatTab() ?? dock.firstChatTab())?.tab.key ?? null;
      }
    } else {
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