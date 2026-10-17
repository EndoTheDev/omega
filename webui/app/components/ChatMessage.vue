<template>
  <div :class="['msg', role]">
    <div
      class="bubble"
      :title="timeTitle" >
      <div
        v-if="role === 'assistant'"
        class="md"
        v-html="rendered" />
      <template v-else>{{ content }}</template>
      <span
        v-if="streaming"
        class="cursor" />
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from "vue";

  import { createRenderScheduler, formatMsgTime, renderMarkdown } from "../composables/omega";

  const props = defineProps<{
    role: "user" | "assistant";
    content: string;
    streaming?: boolean;
    time?: string;
  }>();

  // Assistant bubbles render markdown. Stream chunks land at one render
  // per tick (createRenderScheduler), so partial markdown does not
  // re-parse per chunk; the stream end re-renders the full clean HTML
  // directly. User bubbles stay raw text.
  const rendered = ref("");
  const scheduler = createRenderScheduler((src) => {
    rendered.value = renderMarkdown(src);
  });

  watch(
    () => [props.content, props.streaming] as const,
    ([content, streaming]) => {
      if (!streaming) {
        rendered.value = renderMarkdown(content);
        scheduler.cancel();
        return;
      }
      scheduler.schedule(content);
    },
    { immediate: true },
  );

  onUnmounted(() => scheduler.cancel());

  // Hover timestamp: an empty ref removes the title attribute entirely
  // (malformed or absent times render nothing, not a blank tooltip).
  const timeTitle = computed(() => (props.time ? formatMsgTime(props.time) : null));
</script>