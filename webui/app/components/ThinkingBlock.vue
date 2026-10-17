<template>
  <details
    class="thinking"
    :open="streaming">
    <summary>
      <span class="thinking-label">thinking</span>
      <span
        v-if="elapsed"
        class="thinking-elapsed">{{ elapsed }}</span
      >
      <span
        v-if="streaming"
        class="thinking-live"
        >…</span
      > </summary>
    <div class="thinking-body">{{ content }}</div>
  </details>
</template>

<script setup lang="ts">
  import { onUnmounted, ref, watch } from "vue";

  import { createElapsedMeter } from "../composables/omega";

  // Auto-expand while streaming, collapse when the answer starts: the :open
  // binding tracks `streaming` — true keeps the block open (reasoning visible),
  // false removes the attribute and the details element folds shut.
  const props = defineProps<{
    content: string;
    streaming?: boolean;
  }>();

  // Elapsed counter: ticks every second while the stream is live and
  // freezes at the final value when it ends - the collapsed summary keeps
  // showing how long the run thought. The meter carries no interval of its
  // own across unmount: stop() is called on unmount too (clears the timer,
  // keeps the last label).
  const elapsed = ref("");
  const meter = createElapsedMeter((label) => {
    elapsed.value = label;
  });

  watch(
    () => props.streaming ?? false,
    (live) => {
      if (live) meter.start();
      else meter.stop();
    },
    { immediate: true },
  );

  onUnmounted(() => meter.stop());
</script>