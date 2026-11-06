<template>
  <details
    ref="el"
    class="thinking">
    <summary @click="onSummaryClick">
      <!-- card t_502b8e81: the thinking block wears its iconset glyph -->
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
    <!-- variant anchor (t_0df4efd8): a THINKING row can be a branch's
         active head (a multi-row replay without a stored text bubble)
         - the switcher mounts on the block's tail -->
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
  // card t_502b8e81: the iconset wrapper - brain on the thinking block
  import Icon from "../ui/Icon.vue";
  // card t_428ed4fb: the thinking display knob (auto | expanded |
  // collapsed) - the block's own auto state machine rides the shared
  // mode like the tool rows' (card t_868633b3)
  import { thinkingWantOpen, useThinkingDisplay } from "../../composables/thinkingDisplay";
  import VariantSwitcher from "./VariantSwitcher.vue";
  import type { VariantBlock } from "../../composables/omega";

  const props = defineProps<{
    content: string;
    // the run's reasoning is being streamed - drives the live "…"
    // marker and the elapsed meter (NOT the open state; auto stays
    // collapsed while streaming, per card t_428ed4fb)
    streaming?: boolean;
    // server-measured duration from restored history - wins over the
    // live meter (restored items mount final and never start one)
    finalMs?: number;
    // this row's branch block when the thinking row is the branch's
    // active head (a text-less multi-row replay) - resolved by ChatTab
    variant?: VariantBlock | null;
  }>();

  const emit = defineEmits<{
    (e: "select", index: number): void;
  }>();

  // Elapsed counter: ticks every second while the stream is live and
  // freezes at the final value when it ends - the collapsed summary keeps
  // showing how long the run thought.
  const elapsedLabel = useElapsed(
    () => props.finalMs,
    () => props.streaming ?? false,
  );

  // --- auto open/close (card t_428ed4fb, same shape as the tool rows)
  // The display knob decides what a block does on its own: auto and
  // collapsed keep the body shut (the one-line summary is all you get),
  // expanded forces it open. A manual summary click PINS the block -
  // its open state is the user's, and mode flips recompute around it,
  // never over it. The details element stays uncontrolled (no :open
  // binding): the browser owns the toggle, this side applies the mode
  // on mount and on mode change only.
  const el = ref<HTMLDetailsElement | null>(null);
  const display = useThinkingDisplay();
  const pinned = ref<boolean | null>(null);

  function applyAuto() {
    if (pinned.value !== null || !el.value) return;
    el.value.open = thinkingWantOpen(display.value);
  }

  // nextTick: the mode flip may ride a render pass with other updates -
  // apply after the DOM settles.
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
