<template>
  <div :class="['msg', role]">
    <div
      class="bubble"
      :title="timeTitle" >
      <div
        v-if="role === 'assistant'"
        ref="mdEl"
        class="md"
        v-html="rendered" />
      <template v-else>{{ content }}</template>
      <span
        v-if="elapsed"
        class="msg-elapsed">{{ elapsed }}</span
      >
      <span
        v-if="streaming"
        class="cursor" />
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onUnmounted, ref, watch } from "vue";

  import { decorateCodeblocks } from "../composables/codeblocks";
  import { createRenderScheduler, createElapsedMeter, formatMsgTime, renderMarkdown } from "../composables/omega";

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

  onUnmounted(() => {
    scheduler.cancel();
    meter.stop();
  });

  // Elapsed counter (first response chunk -> stream_end): starts when the
  // bubble mounts streaming and freezes at the final value when it ends -
  // the finished answer keeps showing how long it took. Bubbles that
  // arrive already-final (user messages, restored history) never start
  // and render nothing. The transcript v-for keys on item identity
  // (createItemKeySource), so a component instance always belongs to one
  // item - no label ever leaks across a history rebuild or retry.
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

  // Hover timestamp: an empty ref removes the title attribute entirely
  // (malformed or absent times render nothing, not a blank tooltip).
  const timeTitle = computed(() => (props.time ? formatMsgTime(props.time) : null));

  // Codeblock UX (header bar, copy, collapse, 60vh cap). The markdown
  // renderer owns the <pre><code> markup; the wrapper is added after each
  // render so it survives streaming. v-html replacement tears the wrapper
  // down on every tick - decorate re-adds it, keyed collapse state keeps
  // the user's choice across re-renders.
  const mdEl = ref<HTMLElement | null>(null);
  const collapsed = new Set<number>();
  watch(rendered, () => {
    void nextTick(() => decorateCodeblocks(mdEl.value, collapsed));
  }, { immediate: true });
</script>