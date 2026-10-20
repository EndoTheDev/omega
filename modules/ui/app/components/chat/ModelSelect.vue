<template>
  <!-- custom dropdown: a native <select> popup is OS chrome that can't be
       themed - this renders the listbox ourselves on the design tokens -->
  <div
    class="model-select"
    ref="root">
    <button
      class="model-chip"
      type="button"
      title="Model"
      :aria-expanded="open"
      @click="open ? close() : openNow()"
      @keydown.down.prevent="openNow()">
      <!-- ellipsis container: at narrow widths the chip gives up width
           BEFORE the toolbar wraps (t_357cbe9c squeeze, 320px floor) -->
      <span class="chip-name">{{ model }}</span>
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
      <input
        ref="searchEl"
        v-model="query"
        class="model-search"
        type="text"
        placeholder="filter models"
        spellcheck="false"
        @keydown.escape.stop="close" />
      <ul class="model-list">
        <li
          v-for="m in filtered"
          :key="m"
          role="option"
          :aria-selected="m === model"
          :class="{ current: m === model }"
          @click="
            $emit('update:model', m);
            close();
          ">
          {{ m }}
        </li>
        <li
          v-if="filtered.length === 0"
          class="model-none">
          no matches
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";

  const props = defineProps<{
    model: string;
    models: string[];
  }>();

  defineEmits<{
    (e: "update:model", value: string): void;
  }>();

  const open = ref(false);
  const query = ref("");
  const root = ref<HTMLElement | null>(null);
  const searchEl = ref<HTMLInputElement | null>(null);

  const filtered = computed(() => {
    const q = query.value.trim().toLowerCase();
    if (!q) return props.models;
    return props.models.filter((m) => m.toLowerCase().includes(q));
  });

  function openNow() {
    open.value = true;
    nextTick(() => searchEl.value?.focus());
  }

  function close() {
    open.value = false;
    query.value = "";
  }

  // click-outside closes the list - the native select gave us this for free,
  // so the custom one must match its behavior
  function onDocPointer(e: PointerEvent) {
    if (open.value && root.value && !root.value.contains(e.target as Node))
      close();
  }

  onMounted(() => document.addEventListener("pointerdown", onDocPointer));
  onBeforeUnmount(() =>
    document.removeEventListener("pointerdown", onDocPointer),
  );
</script>
