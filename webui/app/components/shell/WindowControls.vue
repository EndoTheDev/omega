<template>
  <div
    v-if="desktop"
    class="window-controls"
    role="group"
    aria-label="Window">
    <button
      type="button"
      tabindex="-1"
      class="win-cell win-min"
      data-window-control="minimize"
      aria-label="Minimise"
      title="Minimise"
      @click="$emit('minimise')">
      <Icon
        name="minus"
        :size="16" />
    </button>
    <button
      type="button"
      tabindex="-1"
      class="win-cell win-max"
      data-window-control="maximize"
      :aria-label="maximised ? 'Restore' : 'Maximise'"
      :title="maximised ? 'Restore' : 'Maximise'"
      @click="$emit('toggle-maximise')">
      <Icon
        :name="maximised ? 'copy' : 'square'"
        :size="16" />
    </button>
    <button
      type="button"
      tabindex="-1"
      class="win-cell win-close"
      data-window-control="close"
      aria-label="Close"
      title="Close"
      @click="$emit('close')">
      <Icon
        name="x"
        :size="16" />
    </button>
  </div>
</template>

<script setup lang="ts">
  // The window is windowed-hosted, so the cells are no-drag client elements and a press
  // goes through the page; the ACTION is named to the host, which performs it. No state
  // is guessed here: the restore glyph follows the prop.
  import Icon from "../ui/Icon.vue";

  defineProps<{
    desktop: boolean;
    maximised: boolean;
  }>();

  defineEmits<{
    (e: "minimise"): void;
    (e: "toggle-maximise"): void;
    (e: "close"): void;
  }>();
</script>
