<template>
  <footer class="statusbar">
    <span class="seg">
      <span
        class="dot"
        :class="healthy ? 'ok' : 'down'" />
      {{ healthy ? "online" : "offline" }}
    </span>
    <span
      v-if="tokens"
      class="seg hide-sm"
      title="tokens used, active session">
      {{ tokens }} tok
    </span>
    <span
      v-if="contextSize"
      class="seg hide-sm"
      title="model context window">
      {{ formatCtx }} ctx
    </span>
    <span
      v-if="streaming && liveChunksPerSecond"
      class="seg"
      title="chunks received per second (live)">
      {{ liveChunksPerSecond }} ch/s
    </span>
    <span
      v-else-if="lastChunksPerSecond"
      class="seg"
      title="chunks per second, last run">
      {{ lastChunksPerSecond }} ch/s
    </span>
    <span
      v-if="tokensPerSecond"
      class="seg"
      title="generation speed, last run">
      {{ tokensPerSecond }} tok/s
    </span>
    <span class="right">
      <span
        v-if="version"
        class="seg hide-sm"
        :title="`server build and uptime - ${version}`">
        {{ version }} · up {{ formatUp }}
      </span>
      <span class="seg">
        <span
          v-if="streaming"
          class="streaming-seg"
          >streaming</span
        >
        <span v-else>{{ messageCount }} messages</span>
      </span>
    </span>
  </footer>
</template>

<script setup lang="ts">
  import { computed } from "vue";
  import { formatDurationMs } from "../composables/omega";

  const props = defineProps<{
    healthy: boolean;
    streaming: boolean;
    messageCount: number;
    tokens: number;
    contextSize: number;
    tokensPerSecond: number;
    liveChunksPerSecond: number;
    // last-known chunks/s (persisted last_run block, or the run that
    // just ended) - shown while no stream is live
    lastChunksPerSecond: number;
    // server build tag + uptime from /health; empty version hides the seg
    version: string;
    upSeconds: number;
  }>();

  // 1048576 -> "1M", 32768 -> "32k"
  const formatCtx = computed(() => {
    const n = props.contextSize;
    if (n >= 1_000_000) {
      const m = n / 1_000_000;
      return `${Number.isInteger(m) ? m : m.toFixed(1)}M`;
    }
    if (n >= 1_000) return `${Math.round(n / 1000)}k`;
    return String(n);
  });

  // uptime reads in the same voice as the elapsed meters ("2m 05s",
  // "1h 02m"); a fresh server reads "0s"
  const formatUp = computed(() => {
    const total = Math.floor(props.upSeconds);
    if (total < 1) return "0s";
    return formatDurationMs(total * 1000);
  });
</script>
