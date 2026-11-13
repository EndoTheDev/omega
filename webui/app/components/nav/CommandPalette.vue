<template>
  <!-- card t_6215474b: the command palette OVERLAY - top-center, no
       scrim (the user keeps reading the panes while the palette is up;
       the whole point is reaching a pane WITHOUT closing anything).
       Input + filtered list + keybinding-hint column. Hand-rolled: the
       three shipped search boxes are the pattern, keyboard nav rides
       clampIndex (the composer palette's arithmetic). -->
  <div
    ref="rootEl"
    v-if="open"
    class="command-palette"
    role="dialog"
    aria-label="Command palette">
    <input
      ref="inputEl"
      class="command-palette-input"
      :value="query"
      type="text"
      placeholder="Type a command..."
      autocomplete="off"
      aria-label="Command palette search"
      @input="onInput"
      @keydown="onKey" />
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
  import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import {
    clampIndex,
    filterPaletteCommands,
    type PaletteCommand,
  } from "../../composables/commandRegistry";

  const props = defineProps<{
    open: boolean;
    commands: readonly PaletteCommand[];
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
  function onDocPointer(e: PointerEvent) {
    if (!props.open || !rootEl.value) return;
    const target = e.target as Element | null;
    if (target instanceof Element && target.closest("[data-palette-trigger]"))
      return;
    if (rootEl.value.contains(target as Node)) return;
    emit("close");
  }

  onMounted(() => document.addEventListener("pointerdown", onDocPointer));
  onBeforeUnmount(() =>
    document.removeEventListener("pointerdown", onDocPointer),
  );
</script>