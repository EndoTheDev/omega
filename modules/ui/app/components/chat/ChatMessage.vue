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
      <!-- the action row (t_19c100dd, Endo ruling 2): the variant
           switcher « n/n », fork and copy sit in ONE row at the
           BOTTOM of the message - never top-edge chips. The row hides
           while the bubble streams (a streaming bubble has no
           actions); the switcher mounts only on a branch block, fork
           only on historical rows. -->
      <div
        v-if="!streaming"
        class="msg-actions">
        <div
          v-if="variant"
          class="variant-switcher">
          <button
            class="v-arrow"
            type="button"
            title="previous variant"
            :disabled="vLabel.index <= 0"
            @click.stop="emit('select', vLabel.index - 1)">«</button>
          <span class="v-pos">{{ vLabel.index + 1 }}/{{ vLabel.count }}</span>
          <button
            class="v-arrow"
            type="button"
            title="next variant"
            :disabled="vLabel.index >= vLabel.count - 1"
            @click.stop="emit('select', vLabel.index + 1)">»</button>
        </div>
        <!-- fork affordance (t_a62eeb43): any HISTORICAL message carries
             it. Click anchors the composer - the next send continues
             from this message. -->
        <button
          v-if="itemId && !streaming"
          class="msg-fork"
          type="button"
          title="fork from here - the next send continues from this message"
          @click.stop="emit('fork')">fork</button>
        <!-- copy message (/copy parity): the click carries the RAW
             source, never the render -->
        <button
          class="msg-copy"
          type="button"
          :title="copied ? 'copied' : 'copy message'"
          @click.stop="copyMessage">{{ copied ? "copied" : "copy" }}</button>
      </div>
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
    variantLabel,
    type VariantBlock,
  } from "../../composables/omega";
  import { useElapsed } from "../../composables/useElapsed";

  const props = defineProps<{
    role: "user" | "assistant";
    content: string;
    streaming?: boolean;
    // store row id (restored history / post-run refetch) - the switcher
    // and fork affordance's address; live bubbles carry none
    itemId?: string;
    // this bubble's branch block (assistant responses with >=2 sibling
    // runs) - resolved by ChatTab, rendered as the « 2/3 » switcher
    variant?: VariantBlock | null;
    time?: string;
    // image attachments (user messages; restored history carries them)
    images?: OmegaImage[];
    // server-measured duration from restored history - wins over the
    // live meter (restored items mount final and never start one)
    finalMs?: number;
  }>();

  // the variant affordances surface upward: select carries the SIBLING
  // index clicked; fork carries this bubble's store id. ChatItem maps
  // select onto the branch point's id (this component does not know it).
  const emit = defineEmits<{
    (e: "select", index: number): void;
    (e: "fork"): void;
  }>();

  // the « 2/3 » read: THIS bubble's 0-based sibling position clamped
  // into range (variantLabel), one-indexed plus the total in the span.
  // Read only under the v-if="variant" branch; the fallback keeps the
  // computed total (a referrer outside that branch would have no slot).
  const vLabel = computed(() =>
    props.variant ?
      variantLabel(props.variant, props.itemId)
      : { index: 0, count: 1 },
  );

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

  // Copy message (/copy parity, card t_868633b3): the bubble's RAW text
  // reaches the clipboard - the markdown source for assistant bubbles,
  // plain text for user ones - never the rendered HTML (codeblock copy
  // already handles its own raw source). Same 1.2s copied affordance as
  // the codeblock header button.
  const copied = ref(false);
  let copyTimer: number | undefined;
  function copyMessage() {
    void navigator.clipboard.writeText(props.content);
    copied.value = true;
    window.clearTimeout(copyTimer);
    copyTimer = window.setTimeout(() => {
      copied.value = false;
    }, 1200);
  }
  onUnmounted(() => window.clearTimeout(copyTimer));

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