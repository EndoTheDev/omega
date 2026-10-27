<template>
  <details
    class="thinking"
    :open="streaming">
    <summary>
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
  import { useElapsed } from "../../composables/useElapsed";
  import VariantSwitcher from "./VariantSwitcher.vue";
  import type { VariantBlock } from "../../composables/omega";

  // Auto-expand while streaming, collapse when the answer starts: the :open
  // binding tracks `streaming` — true keeps the block open (reasoning visible),
  // false removes the attribute and the details element folds shut.
  const props = defineProps<{
    content: string;
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
</script>