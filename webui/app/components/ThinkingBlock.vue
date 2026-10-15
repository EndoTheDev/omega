<template>
  <details
    class="thinking"
    :open="streaming">
    <summary>
      <span class="thinking-label">thinking</span>
      <span
        v-if="streaming"
        class="thinking-live"
        >…</span
      >
    </summary>
    <div class="thinking-body">{{ content }}</div>
  </details>
</template>

<script setup lang="ts">
  // Auto-expand while streaming, collapse when the answer starts: the :open
  // binding tracks `streaming` — true keeps the block open (reasoning visible),
  // false removes the attribute and the details element folds shut.
  defineProps<{
    content: string;
    streaming?: boolean;
  }>();
</script>
