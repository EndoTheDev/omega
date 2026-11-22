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
      <template v-else-if="msgParts">
        <template
          v-for="(seg, i) in msgParts"
          :key="i">
          <template v-if="seg.text">{{ seg.text }}</template>
          <ReferencePill
            v-else-if="seg.token"
            :pill="seg.token"
            @open="openRef(seg.token!)" />
        </template>
      </template>
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
      <!-- attachments: type glyph + name + size, never an inline render -->
      <div
        v-if="attachments?.length"
        class="msg-files">
        <span
          v-for="(f, i) in attachments"
          :key="'file-' + i"
          class="msg-file"
          :title="f.name + ' - ' + attachmentSizeLabel(f.size)">
          <Icon
            :name="attachmentIcon(f.kind)"
            :size="14" />
          <span class="msg-file-name">{{ f.name }}</span>
          <span class="msg-file-size">{{ attachmentSizeLabel(f.size) }}</span>
        </span>
      </div>
      <span
        v-if="role === 'assistant' && streaming && elapsedLabel"
        class="msg-elapsed">{{ elapsedLabel }}</span
      >
      <span
        v-if="streaming"
        class="cursor" />
      <div
        v-if="!streaming && role === 'assistant'"
        class="msg-actions">
        <VariantSwitcher
          v-if="variant"
          :variant="variant"
          :item-id="itemId"
          @select="$emit('select', $event)" />
        <!-- fork: carried by any HISTORICAL message (itemId), never a streaming one -->
        <button
          v-if="itemId && !streaming"
          class="msg-fork"
          type="button"
          title="fork from here - the next send continues from this message"
          @click.stop="emit('fork')">fork</button>
        <button
          class="msg-copy"
          type="button"
          :title="copyFailed ? 'copy failed' : copied ? 'copied' : 'copy message'"
          @click.stop="copyMessage">{{ copyFailed ? "failed" : copied ? "copied" : "copy" }}</button>
        <!-- retry rides only the NEWEST assistant response (the parent sets retryable) -->
        <button
          v-if="retryable && itemId"
          class="msg-retry"
          type="button"
          title="retry - re-run the last user message, the fresh response becomes a sibling variant"
          @click.stop="emit('retry')">retry</button>
        <span
          v-if="elapsedLabel"
          class="msg-elapsed">{{ elapsedLabel }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, inject, nextTick, onUnmounted, ref, watch } from "vue";

  import { decorateCodeblocks } from "../../composables/codeblocks";
  // the ONE copy path - insecure-context-proof
  import { copyText } from "../../composables/clipboard";
  import VariantSwitcher from "./VariantSwitcher.vue";
  import ReferencePill from "./ReferencePill.vue";
  import Icon from "../ui/Icon.vue";
  import {
    splitRefSegments,
    type RefToken,
  } from "../../composables/pills";
  import {
    attachmentIcon,
    attachmentSizeLabel,
  } from "../../composables/attachments";
  import { DOCK_VIEWER_OPEN_KEY } from "../../composables/viewer";
  import {
    createRenderScheduler,
    dataUrl,
    formatMsgTime,
    renderMarkdown,
    type OmegaAttachment,
    type OmegaImage,
    type VariantBlock,
  } from "../../composables/omega";
  import { useElapsed } from "../../composables/useElapsed";

  const props = defineProps<{
    role: "user" | "assistant";
    content: string;
    streaming?: boolean;
    itemId?: string;
    variant?: VariantBlock | null;
    time?: string;
    images?: OmegaImage[];
    // non-image attachments: type-glyph chips, never the bytes
    attachments?: OmegaAttachment[];
    // server-measured, from restored history: beats the live meter
    finalMs?: number;
    turnMs?: number;
    elapsedMs?: number;
    retryable?: boolean;
  }>();

  const emit = defineEmits<{
    (e: "select", index: number): void;
    (e: "fork"): void;
    (e: "retry"): void;
  }>();

  // Pills are USER-only, and the scan is the shared token model (pills.ts), not
  // a transcript heuristic: pills exist exactly where the invocation grammar is.
  const msgParts = computed(() =>
    props.role === "user" ? splitRefSegments(props.content) : null,
  );
  // the pill's open: the app always provides the viewer opener (FilesTab rides it).
  const openViewerRef = inject(DOCK_VIEWER_OPEN_KEY, null);
  function openRef(t: RefToken) {
    openViewerRef?.({ kind: t.kind, name: t.name });
  }

  function openImage(img: OmegaImage) {
    window.open(dataUrl(img), "_blank");
  }

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

  // Bubbles that arrive already-final (user messages, restored history) never
  // start a meter and render nothing.
  const elapsedLabel = useElapsed(
    () => props.turnMs ?? props.finalMs,
    () => props.streaming ?? false,
    () => props.elapsedMs ?? 0,
  );

  const timeTitle = computed(() => (props.time ? formatMsgTime(props.time) : null));

  // The bubble's RAW text reaches the clipboard - markdown source for assistant
  // bubbles, plain text for user ones - never the rendered HTML (codeblock copy
  // already handles its own raw source).
  const copied = ref(false);
  const copyFailed = ref(false);
  let copyTimer: number | undefined;
  async function copyMessage() {
    // the ONE copy path: report the real verdict, never a blind "copied"
    const raw = await copyText(props.content);
    if (!raw) {
      copyFailed.value = true;
      window.clearTimeout(copyTimer);
      copyTimer = window.setTimeout(() => {
        copyFailed.value = false;
      }, 1200);
      return;
    }
    copied.value = true;
    window.clearTimeout(copyTimer);
    copyTimer = window.setTimeout(() => {
      copied.value = false;
    }, 1200);
  }
  onUnmounted(() => window.clearTimeout(copyTimer));

  const mdEl = ref<HTMLElement | null>(null);
  const collapsed = new Set<number>();
  watch(rendered, () => {
    void nextTick(() => decorateCodeblocks(mdEl.value, collapsed));
  }, { immediate: true });
</script>