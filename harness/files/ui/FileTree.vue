<template>
  <!-- chevronless rows - ZERO triangle glyphs in the tree. -->
  <ul
    class="files-tree"
    :class="{ narrow: narrow ?? false, guided: depth > 0 }"
    :style="guideStyle"
    :role="depth > 0 ? 'group' : 'tree'">
    <li
      v-for="entry in entries"
      :key="entry.name">
      <div
        class="tree-row"
        :class="{ 'is-dir': entry.is_dir }"
        :style="{ paddingInlineStart: indent }"
        :title="entryPath(entry)"
        role="treeitem"
        :tabindex="0"
        :aria-expanded="entry.is_dir ? isOpen(entryPath(entry)) : undefined"
        @click="activate(entry, $event)"
        @keydown.enter.prevent="activate(entry)"
        @keydown.space.prevent="activate(entry)">
        <!--
          VS Code-style row glyph: folder open/closed by expand state, files by.
        -->
        <Icon
          class="glyph"
          :name="rowGlyphIcon(entry)"
          :size="13"
          role="presentation" />
        <span class="name"
          >{{ entry.name
          }}<span
            v-if="entry.is_dir"
            class="dir-mark">/</span
          ></span
        >
        <span
          v-if="!entry.is_dir"
          class="size">{{ entry.size_bytes }}</span>
      </div>
      <!-- a failed expansion renders NOTHING in the tree body. -->
      <FileTree
        v-if="entry.is_dir && isOpen(entryPath(entry)) && !isLoading(entryPath(entry))"
        :entries="childrenOf(entryPath(entry))"
        :base="entryPath(entry)"
        :depth="depth + 1"
        :narrow="narrow"
        @open="(p) => emit('open', p)"
        @select="(s) => emit('select', s)" />
      <p
        v-else-if="entry.is_dir && isLoading(entryPath(entry))"
        class="tree-error"
        :style="{ paddingInlineStart: `${depth * 4 + 22}px` }">
        loading
      </p>
    </li>
  </ul>
</template>

<script setup lang="ts">
  import { computed, inject, provide } from "vue";
  import {
    createFileTreeStore,
    type FileTreeStore,
  } from "~/composables/filetree";
  import { joinRelPath } from "~/composables/omega";
  import type { FileEntry } from "~/composables/omega";
  import { FOCUSED_SESSION_KEY } from "~/composables/viewer";
  import { folderIcon, glyphKindFor, glyphIcon } from "./rowicon";
  import Icon from "~/components/ui/Icon.vue";

  // every branch expansion roots on the focused session's RESOLVED root.
  const focusedSession = inject(FOCUSED_SESSION_KEY, null);

  // File tree: one row per entry, the folder icon toggles expand in place, and
  // expanding a directory lazy-loads exactly one level via /files/browse.
  const props = defineProps<{
    entries: FileEntry[];
    base: string;
    depth: number;
    // the module-UI narrow flag: passed DOWN by the measuring host (FilesTab) - a
    // recursive child never observes its own width (each instance would measure its
    // indent, not the pane); one measurable root owns the flip, descendants inherit it.
    narrow?: boolean;
  }>();

  const emit = defineEmits<{
    open: [path: string];
    select: [sel: { path: string; isDir: boolean }];
  }>();

  let store = inject<FileTreeStore | undefined>("omega-filetree", undefined);
  if (!store) {
    store = createFileTreeStore({
      session: () => focusedSession?.value ?? undefined,
    });
    provide("omega-filetree", store);
  }

  const indent = computed(() => `${props.depth * 4 + 8}px`);

  // hover indent guides: every level ul carries ONE hairline as an
  // absolutely-positioned:before overlay (never flow content - the reveal is
  // opacity-only, zero layout shift).
  const guideStyle = computed(() =>
    props.depth > 0
      ? ({ "--guide-x": `${props.depth * 4 + 6}px` } as Record<string, string>)
      : undefined,
  );

  function entryPath(entry: FileEntry): string {
    return joinRelPath(props.base, entry.name);
  }

  function rowGlyphIcon(entry: FileEntry): string {
    if (entry.is_dir) return folderIcon(isOpen(entryPath(entry)));
    return glyphIcon(glyphKindFor(entry.name));
  }

  function childrenOf(path: string): FileEntry[] {
    return store!.childrenOf(path);
  }

  function isOpen(path: string): boolean {
    return store!.isOpen(path);
  }

  function isLoading(path: string): boolean {
    return store!.isLoading(path);
  }

  // One activation path for the pointer AND the keyboard.
  function activate(entry: FileEntry, ev?: MouseEvent) {
    const path = entryPath(entry);
    emit("select", { path, isDir: entry.is_dir });
    if (entry.is_dir) {
      ev?.stopPropagation();
      store!.toggle(path);
    } else emit("open", path);
  }
</script>

<style scoped>
  /* narrow: measured by the host (FilesTab passes the flag down - never a. */
  .files-tree.narrow .size {
    display: none;
  }

  /*
   * hover indent guides: a hairline per nesting level, an absolute:before OVERLAY on
   * the branch (never flow content - the reveal cannot shift layout), shown purely by
   * CSS:hover on the tree containers.
   */
  .files-tree {
    position: relative;
  }
  .files-tree.guided::before {
    content: "";
    position: absolute;
    top: 0;
    bottom: 0;
    left: var(--guide-x, 0px);
    width: 1px;
    background: var(--stroke);
    opacity: 0;
    transition: opacity 0.15s ease;
    pointer-events: none;
  }
  .files-tree:hover::before,
  .files-tree:hover .files-tree::before {
    opacity: 1;
  }
</style>