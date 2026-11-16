<template>
  <!-- card t_6215474b: the command palette OVERLAY - top-center, no
       scrim (the user keeps reading the panes while the palette is up;
       the whole point is reaching a pane WITHOUT closing anything).
       Input + filtered list + keybinding-hint column. Hand-rolled: the
       three shipped search boxes are the pattern, keyboard nav rides
       clampIndex (the composer palette's arithmetic).
       card t_cc6d0920 (the TAKEOVER): the field no longer lives in this
       panel - it TELEPORTS into the top bar's palette seat
       (`#nav-palette-slot`, the box the clock button fills), so opening
       the palette turns the time field into the input IN PLACE. The
       panel keeps the merged anchor geometry (card t_d83b54b8: top =
       --menubar-h, 420 wide, centred) and now hangs BELOW the bar as
       the results dropdown alone. -->
  <div
    ref="rootEl"
    v-if="open"
    class="command-palette"
    role="dialog"
    aria-label="Command palette">
    <!-- the seat swap. `disabled` is the mobile fallback: below 768px the
         nav cluster is display:none (the drawer handles rule, nav.test),
         so there is no seat to fill - the input renders here, in the
         panel, exactly as it did before the takeover. -->
    <Teleport
      to="#nav-palette-slot"
      :disabled="!inPlace">
      <input
        ref="inputEl"
        :class="inPlace ? 'nav-palette-input is-live' : 'command-palette-input'"
        :value="query"
        type="text"
        placeholder="Type a command..."
        autocomplete="off"
        aria-label="Command palette search"
        @input="onInput"
        @keydown="onKey" />
    </Teleport>
    <ul
      ref="listEl"
      class="command-palette-list"
      role="listbox"
      aria-label="commands">
      <li
        v-for="(c, i) in visible"
        :key="c.id"
        :class="{ active: i === active }"
        role="option"
        :aria-selected="i === active"
        @mouseenter="active = i"
        @click="run(c)">
        <span class="command-title">{{ c.title }}</span>
        <!-- the keybinding-hint column: present where a binding exists
             (none in v1 - the keybindings card owns them), the layout is
             still VS Code's two-column row so hints land without churn -->
        <span
          v-if="c.hint"
          class="command-hint">{{ c.hint }}</span>
      </li>
      <li
        v-if="visible.length === 0"
        class="command-none">
        no matching command
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
  import {
    Teleport,
    computed,
    nextTick,
    onBeforeUnmount,
    onMounted,
    ref,
    watch,
  } from "vue";
  import {
    clampIndex,
    filterPaletteCommands,
    type PaletteCommand,
  } from "../../composables/commandRegistry";

  const props = defineProps<{
    open: boolean;
    commands: readonly PaletteCommand[];
    // card t_cc6d0920: the top bar's palette seat is real estate this panel
    // borrows for its field. True on desktop (the cluster renders there);
    // false below the drawer breakpoint, where the cluster is display:none
    // and the field must stay in the panel.
    inPlace: boolean;
  }>();

  const emit = defineEmits<{
    (e: "close"): void;
  }>();

  const query = ref("");
  const active = ref(0);
  const rootEl = ref<HTMLElement | null>(null);
  const inputEl = ref<HTMLInputElement | null>(null);
  const listEl = ref<HTMLUListElement | null>(null);

  // substring over title + id + keywords (the settled lazy default)
  const visible = computed(() =>
    props.open ? filterPaletteCommands(query.value, props.commands) : [],
  );

  // open: reset the query, land the selection on the first row, autofocus
  // (the settled ruling). The focus lands after the v-if renders.
  watch(
    () => props.open,
    (o) => {
      if (!o) return;
      query.value = "";
      active.value = 0;
      void nextTick(() => inputEl.value?.focus());
    },
  );

  // crossing the drawer breakpoint moves the field between the bar's seat
  // and the panel (the Teleport's `disabled` flips, re-mounting the input),
  // so the palette carries its caret across with it - a live resize must
  // never leave a field on screen that typing cannot reach.
  watch(
    () => props.inPlace,
    () => {
      if (!props.open) return;
      void nextTick(() => inputEl.value?.focus());
    },
  );

  // a fresh query re-seats the selection at the top
  function onInput(e: Event) {
    query.value = (e.target as HTMLInputElement).value;
    active.value = 0;
  }

  // the row under the selection stays visible - the list scrolls, the
  // selection never escapes the viewport window
  watch(active, () => {
    const list = listEl.value;
    if (!list) return;
    list.children[active.value]?.scrollIntoView({ block: "nearest" });
  });

  function onKey(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      active.value = clampIndex(active.value, 1, visible.value.length);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      active.value = clampIndex(active.value, -1, visible.value.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      const c = visible.value[active.value];
      if (c) run(c);
    } else if (e.key === "Escape") {
      e.preventDefault();
      emit("close");
    }
  }

  // one run closes the palette first, then acts - the shell returns to
  // the panes whatever the command does with them
  function run(c: PaletteCommand) {
    emit("close");
    void c.run();
  }

  // click anywhere OUTSIDE the palette closes it. The overlay has no scrim
  // on purpose (the panes stay readable behind it), so the "outside" click
  // cannot be caught by an overlay element - it rides the document, the
  // same seam the settings pickers use. pointerdown, not click: it lands
  // BEFORE the opening gesture's own click, so a click that opens the
  // palette can never close it on the way out. The ONE exemption is the
  // shell's trigger button (`data-palette-trigger`, the nav cluster's
  // Ctrl+K button): its own click toggles the palette, so closing it here
  // on pointerdown would flicker closed-then-reopened.
  // card t_cc6d0920: the live field is TELEPORTED into the bar, so it is not
  // a descendant of rootEl - it is the palette's own surface all the same,
  // and a pointerdown landing in it (a caret grab, a selection drag) must
  // never close the palette it belongs to.
  function onDocPointer(e: PointerEvent) {
    if (!props.open) return;
    const target = e.target as Element | null;
    if (target instanceof Element && target.closest("[data-palette-trigger]"))
      return;
    if (target instanceof Element && inputEl.value?.contains(target)) return;
    if (rootEl.value?.contains(target as Node)) return;
    emit("close");
  }

  onMounted(() => document.addEventListener("pointerdown", onDocPointer));
  onBeforeUnmount(() =>
    document.removeEventListener("pointerdown", onDocPointer),
  );
</script>