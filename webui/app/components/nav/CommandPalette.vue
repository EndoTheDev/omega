<template>
  <!-- the command palette OVERLAY -->
  <div
    ref="rootEl"
    v-if="open"
    class="command-palette"
    role="dialog"
    aria-label="Command palette">
    <!--
      the seat swap: `disabled` is the mobile fallback - below 768px the nav cluster is
      display:none, so there is no seat to fill and the input renders here in the panel.
    -->
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
        :class="{ active: i === active, dimmed: c.dimmed }"
        role="option"
        :aria-selected="i === active"
        :aria-disabled="c.dimmed || undefined"
        @mouseenter="active = i"
        @click="run(c)">
        <span class="command-text">
          <span class="command-title">{{ c.title }}</span>
          <!-- description under the title; rows without one render title-only -->
          <span
            v-if="c.description"
            class="command-desc">{{ c.description }}</span>
        </span>
        <!-- the keybinding-hint column: the layout stays VS Code's two-column row, so a
             hint lands without churn -->
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
    // the top bar's palette seat is real estate this panel borrows: true on desktop, false
    // below the drawer breakpoint where the cluster is display:none and the field stays here
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

  const visible = computed(() =>
    props.open ? filterPaletteCommands(query.value, props.commands) : [],
  );

  watch(
    () => props.open,
    (o) => {
      if (!o) return;
      query.value = "";
      active.value = 0;
      void nextTick(() => inputEl.value?.focus());
    },
  );

  // crossing the drawer breakpoint moves the field between the bar's seat and the panel (the
  // Teleport re-mounts it), so the palette carries its caret across - a live resize must
  // never leave a field on screen that typing cannot reach.
  watch(
    () => props.inPlace,
    () => {
      if (!props.open) return;
      void nextTick(() => inputEl.value?.focus());
    },
  );

  function onInput(e: Event) {
    query.value = (e.target as HTMLInputElement).value;
    active.value = 0;
  }

  // the row under the selection stays visible - the list scrolls, the selection never.
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

  function run(c: PaletteCommand) {
    emit("close");
    void c.run();
  }

  // click anywhere OUTSIDE the palette closes it. The overlay has no scrim on purpose (the
  // panes stay readable behind it), so the "outside" click rides the document.
  // pointerdown, not click: it lands BEFORE the opening gesture's own click, so a click that
  // opens the palette can never close it on the way out. Exemptions: the shell's trigger
  // button (`data-palette-trigger`) and the TELEPORTED live field, which is the palette's own
  // surface even though it is not a rootEl descendant.
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