<template>
  <!-- card t_6215474b: the nav cluster - back / forward / command palette,
       CENTERED in the TOPMOST app bar (Endo's correction, 2026-11-02: the
       pane-toggle bar, NOT the frame rail). VS Code states: the ends DIM
       via the disabled attribute - a real disabled button, never a
       click-swallowing lookalike. Buttons and the Alt+Left / Alt+Right
       keys share ONE path (the nav composable's back/forward). -->
  <div class="nav-cluster" aria-label="navigation">
    <button
      class="icon-btn nav-btn"
      aria-label="Back"
      title="Back (Alt+Left)"
      :disabled="!canBack"
      @click="$emit('back')">
      <Icon
        name="nav-back"
        :size="15" />
    </button>
    <button
      class="icon-btn nav-btn"
      aria-label="Forward"
      title="Forward (Alt+Right)"
      :disabled="!canForward"
      @click="$emit('forward')">
      <Icon
        name="nav-forward"
        :size="15" />
    </button>
    <button
      class="icon-btn nav-btn"
      aria-label="Command palette"
      title="Command palette (Ctrl+K)"
      @click="$emit('open-palette')">
      <Icon
        name="palette"
        :size="15" />
    </button>
  </div>
</template>

<script setup lang="ts">
  import Icon from "../ui/Icon.vue";

  // the dims ride the nav history's canBack/canForward (app.vue passes
  // the computed's values down - cluster stays a dumb three-button strip)
  defineProps<{
    canBack: boolean;
    canForward: boolean;
  }>();

  defineEmits<{
    (e: "back"): void;
    (e: "forward"): void;
    (e: "open-palette"): void;
  }>();
</script>