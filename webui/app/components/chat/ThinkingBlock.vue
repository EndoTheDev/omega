<template>
  <details
    ref="el"
    :class="['thinking', { live: streaming === true }]">
    <summary @click="onSummaryClick">
      <Icon
        name="brain"
        class="thinking-ic" />
      <span class="thinking-label">thinking</span>
      <span
        v-if="elapsedLabel"
        class="thinking-elapsed">{{ elapsedLabel }}</span
      >
      <span
        v-if="streaming"
        class="thinking-live"
        >…</span
      > </summary>
    <div class="thinking-body">{{ content }}</div>
    <!-- variant anchor: a THINKING row can be a branch's active head -->
    <VariantSwitcher
      v-if="variant"
      class="thinking-variant"
      :variant="variant"
      @select="emit('select', $event)" />
  </details>
</template>

<script setup lang="ts">
  import { nextTick, onMounted, ref, watch } from "vue";
  import { useElapsed } from "../../composables/useElapsed";
  import Icon from "../ui/Icon.vue";
  import { thinkingWantOpen, useThinkingDisplay } from "../../composables/thinkingDisplay";
  import VariantSwitcher from "./VariantSwitcher.vue";
  import type { VariantBlock } from "../../composables/omega";

  const props = defineProps<{
    content: string;
    streaming?: boolean;
    // server-measured duration from restored history - wins over the
    // live meter (restored items mount final and never start one)
    finalMs?: number;
    // elapsed-so-far of a reasoning phase still streaming here: a remounted open block
    // counts on from the server's measurement, never from the mount moment
    elapsedMs?: number;
    variant?: VariantBlock | null;
  }>();

  const emit = defineEmits<{
    (e: "select", index: number): void;
  }>();

  const elapsedLabel = useElapsed(
    () => props.finalMs,
    () => props.streaming ?? false,
    () => props.elapsedMs ?? 0,
  );

  // auto open/close The display knob decides what a block does on its own. A manual
  // summary click PINS the block - its open state is the user's, and mode flips
  // recompute around it, never over it.
  const el = ref<HTMLDetailsElement | null>(null);
  const display = useThinkingDisplay();
  const pinned = ref<boolean | null>(null);

  function applyAuto() {
    if (pinned.value !== null || !el.value) return;
    el.value.open = thinkingWantOpen(display.value);
  }

  // nextTick: the mode flip may ride a render pass with other updates.
  watch(display, () => void nextTick(applyAuto));
  onMounted(applyAuto);

  // Click fires BEFORE the browser's toggle default action, so !open is
  // the state the click is about to produce - pin that, and auto
  // never touches this block again this session.
  function onSummaryClick() {
    if (!el.value) return;
    pinned.value = !el.value.open;
  }
</script>
