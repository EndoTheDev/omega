<template>
  <header class="menubar">
    <span class="brand">Ωmega</span>
    <span class="menubar-spacer" />
    <!-- card t_47b776ad: the desktop toggles are BACK. "Fixed" means
         identity + reopenability: the sidebars are pinned singleton panes
         with fixed content, but hide/reopen stays - dock.toggleEdge does
         close-then-reopen at the remembered width. These surface ONLY
         above the 768px breakpoint (CSS hides them below). -->
    <button
      class="icon-btn edge-toggle"
      aria-label="Toggle left sidebar"
      :title="sidebarOpen ? 'Close left sidebar' : 'Open left sidebar'"
      @click="$emit('toggle-sidebar')">
      <svg
        v-if="sidebarOpen"
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M9 3v18m7-6l-3-3l3-3" />
        </g>
      </svg>
      <svg
        v-else
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M9 3v18m5-12l3 3l-3 3" />
        </g>
      </svg>
    </button>
    <!-- card t_33f8756c: the three edge toggles read left | bottom | right,
         matching the screen edge each one controls (the old order was
         left, right, bottom). t_67fc1905 history: the bottom panel's
         toggle joined this cluster; it stays SELF-CONTAINED - the
         drawer-repairs card owns app.vue's wiring, so the button drives
         the dock api directly. Hidden below 768px with the rest
         (the media block owns that). -->
    <button
      class="icon-btn edge-toggle"
      aria-label="Toggle bottom panel"
      :title="bottomOpen ? 'Close bottom panel' : 'Open bottom panel'"
      @click="toggleBottom">
      <svg
        v-if="bottomOpen"
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M3 15h18m9 6l-3-3m3 3l3-3" />
        </g>
      </svg>
      <svg
        v-else
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M3 15h18m9 6v-6m0 0l-3 3m3-3l3 3" />
        </g>
      </svg>
    </button>
    <button
      class="icon-btn edge-toggle"
      aria-label="Toggle right sidebar"
      :title="inspectorOpen ? 'Close right sidebar' : 'Open right sidebar'"
      @click="$emit('toggle-inspector')">
      <svg
        v-if="inspectorOpen"
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M15 3v18M8 9l3 3l-3 3" />
        </g>
      </svg>
      <svg
        v-else
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M15 3v18m-5-6l-3-3l3-3" />
        </g>
      </svg>
    </button>
    <!-- mobile only: the dock panes turn into slide-in drawers below
         768px - these buttons surface ONLY there, as the drawers'
         handles (the desktop toggles above are display:none there). -->
    <button
      class="icon-btn drawer-btn"
      aria-label="Open the sessions drawer"
      title="sessions"
      @click="$emit('toggle-sidebar')">
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M9 3v18" />
        </g>
      </svg>
    </button>
    <button
      class="icon-btn drawer-btn"
      aria-label="Open the inspector drawer"
      title="inspector"
      @click="$emit('toggle-inspector')">
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24">
        <g
          fill="none"
          stroke="currentColor"
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="2">
          <rect
            width="18"
            height="18"
            x="3"
            y="3"
            rx="2" />
          <path d="M15 3v18" />
        </g>
      </svg>
    </button>
    <!-- the settings gear is GONE (card t_47b776ad): settings is a
         dock-tab singleton opened from the pane '+' menu, not the window
         chrome. -->
    <!-- card t_5d9032d3: the notifications bell joins the right cluster
         (rightmost on desktop - the drawer handles hide above 768px).
         The open intents re-emit to app.vue, which owns navigation. -->
    <NotificationCenter @open="$emit('open-notification', $event)" />
  </header>
</template>

<script setup lang="ts">
  // the toggles drive dock.toggleEdge (hide/reopen the pinned edge
  // panes - desktop only); the drawer buttons drive the mobile overlay
  // drawer flips (below 768px, via the same emits dispatched on
  // isMobile in app.vue). open-state icons read the DOCK tree, not the
  // drawer state.
  import { computed, inject } from "vue";
  import { DOCK_API_KEY, findEdgePane } from "../../composables/dock";
  // card t_5d9032d3: the finished-things bell (badge, dropdown panel,
  // live toasts) - the shell hosts it, app.vue routes its open intents.
  import NotificationCenter from "../notifications/NotificationCenter.vue";

  // card t_67fc1905: the bottom toggle is self-contained - reads + drives
  // the dock api directly (the sidebar toggles went through emits because
  // the SAME button doubles as the mobile drawer handle; desktop chrome
  // hiding below 768px comes from the edge-toggle class, so this button
  // needs no app.vue wiring at all).
  const dock = inject(DOCK_API_KEY);
  const bottomOpen = computed(
    () => dock !== undefined && findEdgePane(dock.tree.value, "bottom") !== null,
  );
  function toggleBottom() {
    dock?.toggleBottom();
  }

  defineProps<{
    sidebarOpen: boolean;
    inspectorOpen: boolean;
  }>();

  defineEmits<{
    (e: "toggle-sidebar"): void;
    (e: "toggle-inspector"): void;
    (e: "open-notification", n: unknown): void;
  }>();
</script>