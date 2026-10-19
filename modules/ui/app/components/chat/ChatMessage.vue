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
      <!-- uploaded/attached images: thumbnails, click opens full size -->
      <div
        v-if="images?.length"
        class="msg-images">
        <img
          v-for="(img, i) in images"
          :key="i"
          class="msg-image"
          :src="dataUrl(img)"
          :alt="'attached image ' + (i + 1)"
          @click="openImage(img)" />
      </div>
      <span
        v-if="elapsedLabel"
        class="msg-elapsed">{{ elapsedLabel }}</span
      >
      <span
        v-if="streaming"
        class="cursor" />
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onUnmounted, ref, watch } from "vue";

  import { decorateCodeblocks } from "../../composables/codeblocks";
  import {
    createRenderScheduler,
    dataUrl,
    formatMsgTime,
    renderMarkdown,
    type OmegaImage,
  } from "../../composables/omega";
  import { useElapsed } from "../../composables/useElapsed";

  const props = defineProps<{
    role: "user" | "assistant";
    content: string;
    streaming?: boolean;
    time?: string;
    // image attachments (user messages; restored history carries them)
    images?: OmegaImage[];
    // server-measured duration from restored history - wins over the
    // live meter (restored items mount final and never start one)
    finalMs?: number;
  }>();

  // Click-to-open: a data URL in a new browser tab is the plain full
  // view (no lightbox infrastructure for v1).
  function openImage(img: OmegaImage) {
    window.open(dataUrl(img), "_blank");
  }

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
  });

  // Elapsed counter (first response chunk -> stream_end): starts when the
  // bubble mounts streaming and freezes at the final value when it ends -
  // the finished answer keeps showing how long it took. Bubbles that
  // arrive already-final (user messages, restored history) never start
  // and render nothing. The transcript v-for keys on item identity
  // (createItemKeySource), so a component instance always belongs to one
  // item - no label ever leaks across a history rebuild or retry.
  const elapsedLabel = useElapsed(
    () => props.finalMs,
    () => props.streaming ?? false,
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