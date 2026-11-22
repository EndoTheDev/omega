<template>
  <!--
    omega webui: THE context menu surface. ONE instance for the window (app.vue mounts it);
    every surface feeds the store, so no component ever owns a private menu.
    The surface measures itself once per open: the placement math needs the real box, so it
    stays invisible for the frame before the measure lands (never a visible jump).
  -->
  <Teleport to="body">
    <div
      v-if="contextMenuOpen"
      ref="rootEl"
      class="context-menu"
      :class="{ measured: contextMenuMeasured }"
      role="menu"
      aria-label="context menu"
      :style="style"
      @contextmenu.prevent
      @pointerdown.stop>
      <template
        v-for="(entry, i) in contextMenuEntries"
        :key="entryKey(entry, i)">
        <div
          v-if="isSeparator(entry)"
          class="context-menu-sep"
          role="separator" />
        <button
          v-else
          type="button"
          class="context-menu-item"
          :class="{ active: i === contextMenuActive, danger: entry.danger }"
          :role="entry.checked === undefined ? 'menuitem' : 'menuitemcheckbox'"
          :aria-checked="entry.checked"
          :disabled="entry.disabled"
          @mouseenter="setContextMenuActive(i)"
          @click="runContextMenuItem(i)">
          <span
            v-if="entry.checked !== undefined"
            class="context-menu-check"
            aria-hidden="true">{{ entry.checked ? "✓" : "" }}</span>
          <span class="context-menu-label">{{ entry.label }}</span>
          <span
            v-if="entry.shortcut"
            class="context-menu-shortcut">{{ entry.shortcut }}</span>
        </button>
      </template>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
  import {
    contextMenuActive,
    contextMenuEntries,
    contextMenuMeasured,
    contextMenuOpen,
    contextMenuPlacement,
    handleContextMenuKey,
    isSeparator,
    runContextMenuItem,
    setContextMenuActive,
    setContextMenuSize,
    type ContextMenuEntry,
  } from "../../composables/contextMenu.ts";

  const rootEl = ref<HTMLElement | null>(null);

  const style = computed(() => {
    const p = contextMenuPlacement.value;
    if (!p) return {};
    return { left: p.left + "px", top: p.top + "px" };
  });

  function entryKey(entry: ContextMenuEntry, index: number): string {
    return isSeparator(entry) ? "sep-" + index : entry.id;
  }

  // the real box decides the flip, so it is read after the row set has a layout
  watch(contextMenuOpen, async (o) => {
    if (!o) return;
    await nextTick();
    const el = rootEl.value;
    if (!el) return;
    setContextMenuSize(el.offsetWidth, el.offsetHeight);
  });

  // Escape / arrows / Enter ride the window in the CAPTURE phase: a menu never steals the
  // caret from the composer, but a focused child that owns its keys - a terminal's xterm
  // forwards the key to the shell and stops it from bubbling - cannot swallow the menu's
  // own grammar on its way up. A consumed chord stops there.
  function onKey(e: KeyboardEvent) {
    if (!contextMenuOpen.value) return;
    if (!handleContextMenuKey(e.key)) return;
    e.preventDefault();
    e.stopPropagation();
  }

  // click anywhere outside closes. pointerdown, not click: it lands before the opening
  // gesture's own click, so the right-click that opened the menu cannot close it on the way
  // out. The surface stops its own pointerdown, so an in-menu press never reaches this.
  function onDocPointer(e: PointerEvent) {
    if (!contextMenuOpen.value) return;
    const target = e.target as Node | null;
    if (target && rootEl.value?.contains(target)) return;
    handleContextMenuKey("Escape");
  }

  onMounted(() => {
    window.addEventListener("keydown", onKey, true);
    document.addEventListener("pointerdown", onDocPointer);
  });
  onBeforeUnmount(() => {
    window.removeEventListener("keydown", onKey, true);
    document.removeEventListener("pointerdown", onDocPointer);
  });
</script>

<style scoped>
  .context-menu {
    position: fixed;
    z-index: 250;
    min-width: 170px;
    max-width: 280px;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    background: var(--z2);
    border: 1px solid var(--stroke);
    border-radius: var(--r-card);
    box-shadow: var(--shadow-2);
    user-select: none;
    visibility: hidden;
  }
  .context-menu.measured {
    visibility: visible;
  }
  .context-menu-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 18px;
    padding: 6px 10px;
    background: none;
    border: none;
    border-radius: var(--r-chip);
    color: var(--text);
    font-family: var(--sans);
    font-size: 13px;
    text-align: left;
    cursor: pointer;
    white-space: nowrap;
  }
  .context-menu-item:hover:not(:disabled),
  .context-menu-item.active {
    background: var(--z1-raised);
  }
  .context-menu-item.danger {
    color: var(--err);
  }
  /* the state column: an empty cell still holds its width, so the labels align */
  .context-menu-check {
    flex: none;
    width: 11px;
    color: var(--accent);
    font-size: 12px;
    text-align: center;
  }
  .context-menu-label {
    flex: 1 1 auto;
  }
  .context-menu-item:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .context-menu-shortcut {
    color: var(--text-dim);
    font-family: var(--mono);
    font-size: 11px;
  }
  .context-menu-sep {
    height: 1px;
    margin: 4px 2px;
    background: var(--stroke);
  }
</style>
