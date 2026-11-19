<template>
  <!-- THE THEMED SELECT -->
  <div
    class="model-select band-select"
    :class="{ up: drop === 'up' }"
    ref="root">
    <button
      ref="chipEl"
      class="model-chip"
      type="button"
      :title="label"
      :aria-label="label"
      :aria-expanded="open"
      :aria-controls="open ? listId : undefined"
      @click="open ? close() : openNow()"
      @keydown.down.prevent="openNow()"
      @keydown.esc.stop="close()">
      <span class="chip-name">{{ current }}</span>
      <span
        class="chev"
        aria-hidden="true">▾</span>
    </button>
    <div
      v-if="open"
      :id="listId"
      class="model-pop"
      role="listbox"
      :aria-label="label">
      <ul class="model-list">
        <li
          v-for="(o, i) in options"
          :key="o.value"
          role="option"
          :tabindex="i === activeSeat ? 0 : -1"
          :aria-selected="o.value === modelValue"
          :class="[{ current: o.value === modelValue, active: i === activeSeat }, o.cls ?? '']"
          :ref="(el) => setRowEl(i, el)"
          @focus="seatOn(i)"
          @click="pick(o.value)"
          @keydown="onRowKey($event, o.value)">
          {{ o.label }}
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts" generic="V extends string | number">
  import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
  // the app's ONE keyboard arithmetic (the command palette's wrap).
  import { clampIndex } from "../../composables/commandRegistry";

  const props = withDefaults(
    defineProps<{
      options: { value: V; label: string; cls?: string }[];
      modelValue: V;
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
  const chipEl = ref<HTMLButtonElement | null>(null);

  const current = computed(
    () => props.options.find((o) => o.value === props.modelValue)?.label ?? String(props.modelValue),
  );

  // the KEYBOARD LAW: exactly ONE row is in the tab order (roving tabindex), arrows walk
  // the rows through clampIndex (the palette's wrap - never a second modulo), Home/End jump,
  // Enter/Space picks the focused row, Escape closes and hands focus back to the chip, Tab
  // leaves the popup without stealing the move.
  let pickerSeq = 0;
  const listId = `band-select-${++pickerSeq}-list`;

  const active = ref(0);

  /** the seat the roving tabindex sits on - clamped so a shrinking list keeps a tab stop */
  const activeSeat = computed(() => {
    const len = props.options.length;
    if (len === 0) return -1;
    return Math.min(Math.max(active.value, 0), len - 1);
  });

  const rowEls: (HTMLElement | null)[] = [];
  function setRowEl(i: number, el: unknown) {
    rowEls[i] = el instanceof HTMLElement ? el : null;
  }

  function seatOn(i: number) {
    active.value = i;
  }

  /** land the cursor on row `i` and move DOM focus there */
  function focusRow(i: number) {
    const len = props.options.length;
    if (len === 0) return;
    const seat = Math.max(0, Math.min(len - 1, i));
    active.value = seat;
    const el = rowEls[seat];
    el?.scrollIntoView({ block: "nearest" });
    el?.focus();
  }

  function moveActive(delta: number) {
    const next = clampIndex(activeSeat.value, delta, props.options.length);
    if (next >= 0) focusRow(next);
  }

  function onRowKey(e: KeyboardEvent, value: V) {
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        moveActive(1);
        break;
      case "ArrowUp":
        e.preventDefault();
        moveActive(-1);
        break;
      case "Home":
        e.preventDefault();
        focusRow(0);
        break;
      case "End":
        e.preventDefault();
        focusRow(props.options.length - 1);
        break;
      case "Enter":
      case " ":
        e.preventDefault();
        // Space is ALSO the shell's TYPE-TO-CAPTURE key (app.vue jumps into the chat
        // composer on any PRINTABLE key): activating an option is not typing, so the
        // keystroke must not bubble out.
        e.stopPropagation();
        pick(value);
        break;
      case "Escape":
        // stop here: a host drawer binds Escape to its own close, and this pop is the
        // inner surface
        e.preventDefault();
        e.stopPropagation();
        close();
        break;
      case "Tab":
        // leaving the list is leaving the popup - but never steal the Tab
        close(false);
        break;
    }
  }

  function openNow() {
    open.value = true;
    // seat on the CURRENT value's row
    const seat = props.options.findIndex((o) => o.value === props.modelValue);
    active.value = seat >= 0 ? seat : 0;
    void nextTick(() => {
      if (open.value) focusRow(activeSeat.value);
    });
  }

  function pick(value: V) {
    emit("update:modelValue", value);
    close();
  }

  function close(restoreFocus = true) {
    if (!open.value) return;
    const hadFocus = !!root.value?.contains(document.activeElement);
    open.value = false;
    active.value = 0;
    if (restoreFocus && hadFocus) void nextTick(() => chipEl.value?.focus());
  }

  // click-outside closes the list: the native select gave that for free, so the custom one
  // must match (the ModelSelect contract)
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
  .band-select:not(.up) .model-pop {
    bottom: auto;
    top: calc(100% + 6px);
  }
  /*
   * seats: the chip is inline (the composer's pattern) - a host can width the ROOT and the
   * chip follows, but never beyond its pane.
   */
  .band-select .model-chip {
    max-width: 100%;
  }
  /* the ACTIVE row is the one the roving tabindex and DOM focus sit on */
  .band-select .model-list li.active {
    background: var(--z1-raised);
    color: var(--text);
  }
  .band-select .model-list li:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: -1px;
  }
</style>