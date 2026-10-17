<template>
  <details class="toolcall">
    <summary>
      <span
        class="tool-dot"
        :class="status" />
      <span class="tool-name">{{ name }}</span>
      <span
        v-if="elapsedLabel"
        class="tool-elapsed">{{ elapsedLabel }}</span
      >
    </summary>
    <div
      v-if="output"
      class="tool-section">
      <span class="tool-section-label">input</span>
      <pre class="tool-output">{{ output }}</pre>
    </div>
    <div
      v-if="result"
      class="tool-section">
      <span class="tool-section-label">output</span>
      <pre
        class="tool-output"
        :class="{ 'is-error': status === 'error' }"
        >{{ result }}</pre
      >
    </div>
  </details>
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from "vue";

  import { createElapsedMeter, formatDurationMs } from "../composables/omega";

  const props = defineProps<{
    name: string;
    status: string;
    interrupted?: boolean;
    output?: string;
    result?: string;
    // server-measured duration from restored history - wins over the
    // live meter (restored items mount final and never start one)
    finalMs?: number;
  }>();

  // Elapsed counter (tool_call event -> tool_result arrival): ticks while
  // the call is running, freezes at the final value once the result
  // lands - the finished line keeps showing how long the call took.
  // Restored history arrives already-final and renders nothing.
  const elapsed = ref("");
  const meter = createElapsedMeter((label) => {
    elapsed.value = label;
  });
  const elapsedLabel = computed(() =>
    props.finalMs != null ? formatDurationMs(props.finalMs) : elapsed.value,
  );

  // live = no result yet AND the stream survived it: a dead stream never
  // delivers the tool result, so the interrupted flag stops the meter -
  // without it the counter would tick forever over a call that cannot
  // finish. It stays honest: dot and status keep saying running.
  const live = computed(() => props.status === "running" && !props.interrupted);
  watch(
    live,
    (isLive) => {
      if (isLive) meter.start();
      else meter.stop();
    },
    { immediate: true },
  );
  onUnmounted(() => meter.stop());
</script>
