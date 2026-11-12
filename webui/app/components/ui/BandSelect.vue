<template>
  <!-- omega webui: THE THEMED SELECT (card t_acad02ec). A native
       <select> popup is OS chrome over the themed panes (audit M1); this
       renders the listbox ourselves on the tokens - the ModelSelect/
       ThinkingSelect dropdown grammar (chip button + role="listbox" pop,
       pointerdown-outside closes) generalized over short option ladders
       that need no search field: band toggles, the board switcher, the
       drawer's status, settings knobs. The site keeps the behavior via
       v-model/update; this owns ONLY the look + open/close.
       Card t_2daae1aa: the rows were keyboard-DEAD (role="option" and
       nothing else) - they now ride the same keyboard law as ModelSelect:
       roving tabindex, arrows/Home/End on the app's one clampIndex wrap,
       Enter/Space picks, Escape closes and hands focus back to the chip. -->
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
          :class="{ current: o.value === modelValue, active: i === activeSeat }"
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
  // the app's ONE keyboard arithmetic (the command palette's wrap) - the
  // rows ride the same grammar as the model picker instead of inventing a
  // second one (cards t_a990f422, t_2daae1aa)
  import { clampIndex } from "../../composables/commandRegistry";

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
  const chipEl = ref<HTMLButtonElement | null>(null);

  // the chip face: the current option's label - or the raw value when no
  // option matches (a not-yet-fetched registry edge shows the truth).
  const current = computed(
    () => props.options.find((o) => o.value === props.modelValue)?.label ?? String(props.modelValue),
  );

  // --- the KEYBOARD LAW (card t_2daae1aa, the ModelSelect grammar) --------
  //
  // Exactly ONE row is in the tab order (roving tabindex), arrows walk the
  // rows through clampIndex (the palette's wrap - never a second modulo),
  // Home/End jump, Enter/Space picks the focused row, Escape closes and
  // hands focus back to the chip, Tab leaves the popup without stealing the
  // move. The ACTIVE row is highlighted so the visible cursor follows
  // focus - the same affordance a mouse hover gives.
  //
  // The id counter restarts per instance, which is safe HERE: the attribute
  // and the pop exist only while the list is open, and a second chip's
  // pointerdown closes the first before it can open (one pop in the DOM).
  let pickerSeq = 0;
  const listId = `band-select-${++pickerSeq}-list`;

  /** the keyboard cursor - an INDEX. The ladder is one flat list of
      primitive values (no groups, no query), so the visual order the user
      sees IS the index order, always. */
  const active = ref(0);

  /** the seat the roving tabindex sits on - clamped, so a shrinking options
      list can never leave the popup with zero tab stops. -1 = empty list
      (no row is tabbable, and there is nothing to walk). */
  const activeSeat = computed(() => {
    const len = props.options.length;
    if (len === 0) return -1;
    return Math.min(Math.max(active.value, 0), len - 1);
  });

  const rowEls: (HTMLElement | null)[] = [];
  function setRowEl(i: number, el: unknown) {
    rowEls[i] = el instanceof HTMLElement ? el : null;
  }

  /** the cursor FOLLOWS focus: whatever row the user lands on (Tab, a
      pointer click, an arrow move) is the one highlight and tab stop name */
  function seatOn(i: number) {
    active.value = i;
  }

  /** land the cursor on row `i` and move DOM focus there - the highlight
      and the focus ring are the same cursor. The guard is the empty list. */
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
        // Space is ALSO the shell's TYPE-TO-CAPTURE key (app.vue's
        // onKeyCapture jumps into the chat composer on any PRINTABLE key):
        // activating an option is not typing, so the keystroke must not
        // bubble out and steal the focus this pop is about to hand back to
        // the chip. Enter is not printable - the pair rides one line so the
        // focus-return law holds for BOTH pick keys.
        e.stopPropagation();
        pick(value);
        break;
      case "Escape":
        // stop here: a host drawer binds Escape to its own close, and
        // closing the pop is the nearer intent (the ModelSelect rule)
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
    // seat on the CURRENT value's row: opening lands the cursor where the
    // knob already points, not on row one (the ModelSelect rule)
    const seat = props.options.findIndex((o) => o.value === props.modelValue);
    active.value = seat >= 0 ? seat : 0;
    // the pop mounts on the NEXT tick - the chip has to land the cursor
    // itself, because the roving tabindex lives inside a pop that does not
    // exist yet: while closed, the chip is the only tab stop
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
    // the popup is about to unmount: if the keyboard was inside it, focus
    // would fall to <body> and the user's place in the page would be lost.
    // Hand it back to the chip that opened the list.
    const hadFocus = !!root.value?.contains(document.activeElement);
    open.value = false;
    active.value = 0;
    if (restoreFocus && hadFocus) void nextTick(() => chipEl.value?.focus());
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
  /* the KEYBOARD CURSOR (card t_2daae1aa): the ACTIVE row is the one the
     roving tabindex and DOM focus sit on. It reads as the hover row, so a
     keyboard user sees the same affordance a mouse user gets. */
  .band-select .model-list li.active {
    background: var(--z1-raised);
    color: var(--text);
  }
  .band-select .model-list li:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: -1px;
  }
</style>