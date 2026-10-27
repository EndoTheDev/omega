<template>
  <!-- reasoning-effort chip: the ModelSelect chip's sibling, same
       dropdown pattern without the search field (the ladder is short) -->
  <div
    class="model-select thinking-select"
    ref="root">
    <button
      class="model-chip"
      type="button"
      title="Reasoning effort"
      :aria-expanded="open"
      @click="open ? close() : (open = true)"
      @keydown.down.prevent="open = true">
      {{ level }}
      <span
        class="chev"
        aria-hidden="true"
        >▾</span
      >
    </button>
    <div
      v-if="open"
      class="model-pop"
      role="listbox">
      <ul class="model-list">
        <li
          v-for="option in options"
          :key="option"
          role="option"
          :aria-selected="option === level"
          :class="{ current: option === level }"
          @click="
            $emit('update:level', option);
            close();
          ">
          {{ option }}
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import { THINKING_UI_LEVELS } from "../../composables/omega.ts";

  defineProps<{
    level: string;
  }>();

  defineEmits<{
    (e: "update:level", value: string): void;
  }>();

  const options = THINKING_UI_LEVELS;
  const open = ref(false);
  const root = ref<HTMLElement | null>(null);

  function close() {
    open.value = false;
  }

  // click-outside closes the list - the ModelSelect dropdown rule
  function onDocPointer(e: PointerEvent) {
    if (open.value && root.value && !root.value.contains(e.target as Node))
      close();
  }

  onMounted(() => document.addEventListener("pointerdown", onDocPointer));
  onBeforeUnmount(() =>
    document.removeEventListener("pointerdown", onDocPointer),
  );
</script>

<style>
  .thinking-select .model-pop {
    min-width: 130px;
  }
</style>