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
      <button
        class="send"
        :disabled="busy || !draft.trim()"
        @click="$emit('send')">
        send
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
  const props = defineProps<{
    draft: string;
    model: string;
    models: string[];
    busy: boolean;
  }>();

  defineEmits<{
    (e: "update:draft", value: string): void;
    (e: "update:model", value: string): void;
    (e: "send"): void;
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
