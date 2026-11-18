<template>
  <header class="menubar">
    <!-- ONE brand - the omega glyph and the word in a SINGLE element, far left. -->
    <span class="brand">Ωmega</span>
    <NavCluster
      :can-back="canBack"
      :can-forward="canForward"
      :palette-open="paletteOpen"
      @back="$emit('back')"
      @forward="$emit('forward')"
      @open-palette="$emit('open-palette')" />
    <span class="menubar-spacer" />
    <!-- the desktop toggles are BACK. -->
    <button
      class="icon-btn edge-toggle"
      aria-label="Toggle left sidebar"
      :title="sidebarOpen ? 'Close left sidebar' : 'Open left sidebar'"
      @click="$emit('toggle-sidebar')">
      <!-- the six hand-rolled toggle SVGs collapse into the wrapper's. -->
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
    <!-- the settings gear is BACK. -->
    <button
      class="icon-btn edge-toggle"
      aria-label="Open settings"
      title="Settings"
      @click="$emit('open-settings')">
      <Icon
        name="settings"
        :size="16" />
    </button>
    <!-- the bell is GONE - the durable feed is the notifications dock tab. -->
    <NotificationToasts @open="$emit('open-notification', $event)" />
  </header>
</template>

<script setup lang="ts">
  // the toggles drive app.vue's emits; app.vue routes each one on isMobile.
  import Icon from "../ui/Icon.vue";
  // the notifications surface is the notifications DOCK TAB.
  import NotificationToasts from "./NotificationToasts.vue";
  import NavCluster from "../nav/NavCluster.vue";

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