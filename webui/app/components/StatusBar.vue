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
      v-if="tokensPerSecond"
      class="seg"
      title="generation speed">
      {{ tokensPerSecond }} tok/s
    </span>
    <span class="right">
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

  const props = defineProps<{
    healthy: boolean;
    streaming: boolean;
    messageCount: number;
    tokens: number;
    contextSize: number;
    tokensPerSecond: number;
    liveChunksPerSecond: number;
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
</script>
