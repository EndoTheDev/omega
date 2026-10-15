<template>
  <details class="toolcall">
    <summary>
      <span
        class="tool-dot"
        :class="status" />
      <span class="tool-name">{{ name }}</span>
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
  defineProps<{
    name: string;
    status: string;
    output?: string;
    result?: string;
  }>();
</script>
