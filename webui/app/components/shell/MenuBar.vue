<template>
  <header class="menubar">
    <span class="brand">Ωmega</span>
    <!-- card t_6215474b: the nav cluster - back / forward / palette
         trigger. Seats INLINE after the brand (card t_3aed367e,
         Endo's ruling 2026-11-06: the center wordmark vacated the
         cluster's old absolute-center seat - the cluster's original
         centering was card t_6215474b, the pane-toggle bar NOT the
         frame rail). The dims ride app.vue's nav history; the palette
         intent routes back up. -->
    <NavCluster
      :can-back="canBack"
      :can-forward="canForward"
      @back="$emit('back')"
      @forward="$emit('forward')"
      @open-palette="$emit('open-palette')" />
    <!-- card t_3aed367e (Endo revision 2026-11-06): the PLAIN wordmark
         seats dead center on the menubar's FULL width (absolute middle,
         robust to any left/right width) - brand left, toggles far
         right. No version here: the bottom status bar owns version +
         uptime, no duplication. -->
    <span class="menubar-wordmark">Omega</span>
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
    <!-- the settings gear is GONE (card t_47b776ad): settings is a
         dock-tab singleton opened from the pane '+' menu, not the window
         chrome. -->
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
    (e: "open-notification", n: unknown): void;
  }>();
</script>