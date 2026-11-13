<template>
  <header class="menubar">
    <!-- card t_20fd8745 (Endo ruling 2026-11-13): ONE brand - the omega
         glyph and the word in a SINGLE element, far left. U+03A9 is the
         Greek capital omega, so the glyph opens the visible word and the
         element's text carries the accessible name "Ωmega" (no
         aria-hidden: the text IS the name). The centered 'Omega'
         wordmark (`.menubar-wordmark`) is DEAD - its absolute-center seat
         belongs to the nav cluster below. -->
    <span class="brand">Ωmega</span>
    <!-- card t_20fd8745: the nav cluster - back / forward / the
         fake-input palette affordance / the palette trigger - seats DEAD
         CENTER on the bar's FULL width (the absolute-middle trick the
         wordmark used, robust to any left/right width). Below the drawer
         breakpoint it HIDES: the three edge toggles are the mobile drawer
         handles. The dims ride app.vue's nav history; both palette doors
         emit the one `open-palette` intent. -->
    <NavCluster
      :can-back="canBack"
      :can-forward="canForward"
      @back="$emit('back')"
      @forward="$emit('forward')"
      @open-palette="$emit('open-palette')" />
    <span class="menubar-spacer" />
    <!-- card t_47b776ad: the desktop toggles are BACK. "Fixed" means
         identity + reopenability: the sidebars are pinned singleton panes
         with fixed content, but hide/reopen stays - dock.toggleEdge does
         close-then-reopen at the remembered width. card t_aca53fee (the
         responsive-parity complaint): these surface at EVERY breakpoint
         now - below 768px the sidebar toggles drive the drawers (app.vue
         routes the same emits on isMobile) and the bottom toggle drives
         the dock api directly. Same icons, same order, every width. -->
    <button
      class="icon-btn edge-toggle"
      aria-label="Toggle left sidebar"
      :title="sidebarOpen ? 'Close left sidebar' : 'Open left sidebar'"
      @click="$emit('toggle-sidebar')">
      <!-- card t_502b8e81: the six hand-rolled toggle SVGs collapse into
           the wrapper's panel-*-open/close vocabulary (state = which
           icon, same one-name-one-meaning family) -->
      <Icon
        v-if="sidebarOpen"
        name="panel-left-close"
        :size="16" />
      <Icon
        v-else
        name="panel-left-open"
        :size="16" />
    </button>
    <!-- card t_67fc1905 history: the bottom panel's toggle joined this
         cluster; card t_4bf1d5b7: it routes through app.vue like its
         siblings - app.vue dispatches on isMobile (desktop: the dock api
         hides/reopens the tree pane; mobile: the BOTTOM DRAWER flag,
         since the zero-collapse CSS takes the tree pane's cell out of
         flow below the breakpoint regardless of its state). card
         t_aca53fee: no more second icon set - these three ARE the
         handles at every breakpoint. -->
    <button
      class="icon-btn edge-toggle"
      aria-label="Toggle bottom panel"
      :title="bottomOpen ? 'Close bottom panel' : 'Open bottom panel'"
      @click="$emit('toggle-bottom')">
      <Icon
        v-if="bottomOpen"
        name="panel-bottom-close"
        :size="16" />
      <Icon
        v-else
        name="panel-bottom-open"
        :size="16" />
    </button>
    <button
      class="icon-btn edge-toggle"
      aria-label="Toggle right sidebar"
      :title="inspectorOpen ? 'Close right sidebar' : 'Open right sidebar'"
      @click="$emit('toggle-inspector')">
      <Icon
        v-if="inspectorOpen"
        name="panel-right-close"
        :size="16" />
      <Icon
        v-else
        name="panel-right-open"
        :size="16" />
    </button>
    <!-- card t_00e4a784 (Endo ruling 2026-11-09): the settings gear is
         BACK - a menubar button seated right beside the secondary
         sidebar's toggle, opening settings AS a tab (VSCode's read: a
         normal closable main tab, not shell chrome and no longer a
         pinned inspector view). The button is global, so settings stays
         reachable from every pane context. -->
    <button
      class="icon-btn edge-toggle"
      aria-label="Open settings"
      title="Settings"
      @click="$emit('open-settings')">
      <Icon
        name="settings"
        :size="16" />
    </button>
    <!-- card t_180db2cb (Endo ruling 2026-10-29): the bell is GONE -
         the durable feed is the notifications dock tab; its unread
         badge rides the dockable's icon chip. What the menubar keeps
         hosting is the transient TOAST stack only (toasts STAY for live
         events) - the open intents still re-emit to app.vue, which owns
         navigation. -->
    <NotificationToasts @open="$emit('open-notification', $event)" />
  </header>
</template>

<script setup lang="ts">
  // the toggles drive app.vue's emits; app.vue routes each one on
  // isMobile (desktop: dock.toggleEdge / dock.toggleBottom - hide/reopen
  // the pinned edge pane; mobile: the overlay drawer flags - card
  // t_4bf1d5b7). open-state icons read the app-passed flags so the
  // chevrons stay honest on both routes: desktop flags read the dock
  // tree, mobile flags the drawer state.
  import Icon from "../ui/Icon.vue";
  // card t_180db2cb: the notifications surface is the notifications DOCK
  // TAB (the panel lives in app.vue's outlet); the menubar hosts only
  // the live toast stack (the bell button + popover died with it).
  import NotificationToasts from "./NotificationToasts.vue";
  // card t_6215474b: the back/forward/palette cluster; card t_3aed367e:
  // it seats inline after the brand - the wordmark owns the center
  import NavCluster from "../nav/NavCluster.vue";

  defineProps<{
    sidebarOpen: boolean;
    bottomOpen: boolean;
    inspectorOpen: boolean;
    // the nav history's end states (app.vue's computeds) - the arrows dim
    canBack: boolean;
    canForward: boolean;
  }>();

  defineEmits<{
    (e: "toggle-sidebar"): void;
    (e: "toggle-bottom"): void;
    (e: "toggle-inspector"): void;
    (e: "back"): void;
    (e: "forward"): void;
    (e: "open-palette"): void;
    (e: "open-settings"): void;
    (e: "open-notification", n: unknown): void;
  }>();
</script>