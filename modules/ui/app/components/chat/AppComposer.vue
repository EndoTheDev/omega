<template>
  <div
    class="composer"
    :class="{ expanded: expanded }">
    <textarea
      ref="ta"
      :value="draft"
      placeholder="message omega"
      rows="1"
      @input="
        $emit('update:draft', ($event.target as HTMLTextAreaElement).value);
        resize();
      "
      @keydown.enter.exact.prevent="$emit('send')" />
    <button
      v-if="overflowing"
      class="expand-btn"
      type="button"
      :title="expanded ? 'collapse composer' : 'expand composer'"
      @click="toggleExpand">
      {{ expanded ? "▾" : "▴" }}
    </button>
    <div class="toolbar">
      <ModelSelect
        :model="model"
        :models="models"
        @update:model="$emit('update:model', $event)" />
      <ThinkingSelect
        :level="thinking"
        @update:level="$emit('update:thinking', $event)" />
      <!-- while this session's run streams, send becomes CANCEL (stop
           icon); idle, a retry affordance re-sends the last user message
           when there is one - next to the plain send button -->
=======
      <ThinkingSelect
        :level="thinking"
        @update:level="$emit('update:thinking', $event)" />
>>>>>>> Stashed changes:modules/ui/app/components/AppComposer.vue
      <button
        v-if="canRetry && !busy"
        class="retry"
        type="button"
        title="re-send the last user message as a fresh turn"
        @click="$emit('retry')">
        ↻ retry
      </button>
      <button
        v-if="busy"
        class="send cancel"
        type="button"
        title="cancel the running response (what streamed stays)"
        @click="$emit('cancel')">
        ■ stop
      </button>
      <button
        v-else
        class="send"
        :disabled="!draft.trim()"
        @click="$emit('send')">
        send
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
  import ModelSelect from "./ModelSelect.vue";
  import ThinkingSelect from "./ThinkingSelect.vue";

  const props = defineProps<{
    draft: string;
    model: string;
    models: string[];
    // THIS session's run is streaming: the send button is CANCEL now
    busy: boolean;
    // idle with a last user message in the transcript: retry shows
    canRetry: boolean;
    thinking: string;
  }>();

  defineEmits<{
    (e: "update:draft", value: string): void;
    (e: "update:model", value: string): void;
    (e: "update:thinking", value: string): void;
    (e: "send"): void;
    (e: "cancel"): void;
    (e: "retry"): void;
  }>();

  // grow with content: 1 row at rest, up to 4 rows (COMPOSER_MAX), then the
  // textarea scrolls. Past the cap an expand button appears (top right) to
  // grow the composer beyond it, up to 60% of the viewport.
  // Explicit 21px line-height in CSS makes the row math deterministic:
  // 4 rows = 84px of content height.
  const COMPOSER_MAX = 84;

  const ta = ref<HTMLTextAreaElement | null>(null);
  const expanded = ref(false);
  const overflowing = ref(false);

  function resize() {
    const el = ta.value;
    if (!el) return;
    el.style.height = "auto";
    const full = el.scrollHeight;
    overflowing.value = full > COMPOSER_MAX + 1;
    if (expanded.value) {
      // expanded: the composer fills the chat pane (CSS flex) - no inline
      // height, the textarea stretches and scrolls
      el.style.height = "";
      return;
    }
    el.style.height = `${Math.min(full, COMPOSER_MAX)}px`;
  }

  function toggleExpand() {
    expanded.value = !expanded.value;
    resize();
  }

  // draft clears after send (parent) - shrink back on the next tick
  watch(
    () => props.draft,
    () => nextTick(resize),
  );
</script>