<template>
  <header class="menubar">
    <!-- the two caption strips are the window's drag surface in a Wails window
         (data-caption-region); every control keeps its own clicks -->
    <span
      class="brand"
      data-caption-region>Ωmega</span>
    <NavCluster
      :can-back="canBack"
      :can-forward="canForward"
      :palette-open="paletteOpen"
      @back="$emit('back')"
      @forward="$emit('forward')"
      @open-palette="$emit('open-palette')" />
    <span
      class="menubar-spacer"
      data-caption-region />
    <button
      class="icon-btn edge-toggle"
      aria-label="Toggle left sidebar"
      :title="sidebarOpen ? 'Close left sidebar' : 'Open left sidebar'"
      @click="$emit('toggle-sidebar')">
      <Icon
        v-if="sidebarOpen"
        name="panel-left-close"
        :size="16" />
      <Icon
        v-else
        name="panel-left-open"
        :size="16" />
    </button>
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
    <button
      class="icon-btn edge-toggle"
      aria-label="Open settings"
      title="Settings"
      @click="$emit('open-settings')">
      <Icon
        name="settings"
        :size="16" />
    </button>
    <NotificationToasts @open="$emit('open-notification', $event)" />
    <WindowControls
      :desktop="desktop"
      :maximised="maximised"
      @minimise="act('minimise')"
      @toggle-maximise="act('toggle-maximise')"
      @close="act('close')" />
    <ResizeFrame v-if="desktop && !maximised" />
  </header>
</template>

<script setup lang="ts">
  // the toggles drive app.vue's emits; app.vue routes each one on isMobile.
  import Icon from "../ui/Icon.vue";
  import NotificationToasts from "./NotificationToasts.vue";
  import NavCluster from "../nav/NavCluster.vue";
  import WindowControls from "./WindowControls.vue";
  import ResizeFrame from "./ResizeFrame.vue";
  import { useDesktopChrome } from "../../composables/desktopChrome";

  // inside the Wails window the header is the title bar: the lane renders and the
  // region publishing arms the native hit test. A browser tab gets neither.
  const { desktop, maximised, act } = useDesktopChrome();

  defineProps<{
    sidebarOpen: boolean;
    bottomOpen: boolean;
    inspectorOpen: boolean;
    canBack: boolean;
    canForward: boolean;
    // the palette's open state rides down to the cluster - the takeover hides the
    // arrows + the Ctrl+K toggle while the field is up (the cluster keeps their layout,
    // so the seat never moves).
    paletteOpen: boolean;
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