<template>
  <!-- omega webui: THE THEMED SELECT (card t_acad02ec). A native
       <select> popup is OS chrome over the themed panes (audit M1); this
       renders the listbox ourselves on the tokens - the ModelSelect/
       ThinkingSelect dropdown grammar (chip button + role="listbox" pop,
       pointerdown-outside closes) generalized over short option ladders
       that need no search field: band toggles, the board switcher, the
       drawer's status, settings knobs. The site keeps the behavior via
       v-model/update; this owns ONLY the look + open/close. -->
  <div
    class="model-select band-select"
    :class="{ up: drop === 'up' }"
    ref="root">
    <button
      class="model-chip"
      type="button"
      :title="label"
      :aria-label="label"
      :aria-expanded="open"
      @click="open ? close() : openNow()"
      @keydown.down.prevent="openNow()"
      @keydown.esc.stop="close">
      <span class="chip-name">{{ current }}</span>
      <span
        class="chev"
        aria-hidden="true">▾</span>
    </button>
    <div
      v-if="open"
      class="model-pop"
      role="listbox"
      :aria-label="label">
      <ul class="model-list">
        <li
          v-for="o in options"
          :key="o.value"
          role="option"
          :aria-selected="o.value === modelValue"
          :class="{ current: o.value === modelValue }"
          @click="pick(o.value)">
          {{ o.label }}
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts" generic="V extends string | number">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";

  // The pop opens DOWN by default (a band/drawer head sits at the top of
  // its pane - downward is where the room is); drop="up" flips it for
  // controls seated above their pane's floor. The shared .model-pop CSS
  // encodes the up case (the composer's grammar); this overrides down.
  const props = withDefaults(
    defineProps<{
      options: { value: V; label: string }[];
      modelValue: V;
      /** the chip's tooltip AND the listbox's aria-label: what this picks */
      label: string;
      drop?: "down" | "up";
    }>(),
    { drop: "down" },
  );

  const emit = defineEmits<{
    (e: "update:modelValue", value: V): void;
  }>();

  const open = ref(false);
  const root = ref<HTMLElement | null>(null);

  // the chip face: the current option's label - or the raw value when no
  // option matches (a not-yet-fetched registry edge shows the truth).
  const current = computed(
    () => props.options.find((o) => o.value === props.modelValue)?.label ?? String(props.modelValue),
  );

  function openNow() {
    open.value = true;
  }

  function close() {
    open.value = false;
  }

  function pick(value: V) {
    emit("update:modelValue", value);
    close();
  }

  // click-outside closes the list - the native select gave us this for
  // free, so the custom one must match its behavior (the ModelSelect
  // contract)
  function onDocPointer(e: PointerEvent) {
    if (open.value && root.value && !root.value.contains(e.target as Node))
      close();
  }

  onMounted(() => document.addEventListener("pointerdown", onDocPointer));
  onBeforeUnmount(() =>
    document.removeEventListener("pointerdown", onDocPointer),
  );
</script>

<style scoped>
  /* downward pop: the shared .model-pop anchors bottom: calc(100%+6px);
     the down case flips to the chip's floor. */
  .band-select:not(.up) .model-pop {
    bottom: auto;
    top: calc(100% + 6px);
  }
  /* seats: the chip is inline (the composer's pattern) - a host can
     width the ROOT and the chip follows, but never beyond its pane. */
  .band-select .model-chip {
    max-width: 100%;
  }
</style>