<template>
  <!-- card t_63582d1f: chevronless rows - ZERO triangle glyphs in the
       tree. The folder icon CARRIES the expand state (closed glyph =
       collapsed, open glyph = expanded, rowicon.ts folderIcon) and the
       row's aria-expanded + keyboard activation replace every affordance
       the chevron used to carry. -->
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
        <!-- VS Code-style row glyph (card t_467a0386, iconified card
             t_502b8e81): folder open/closed by expand state, files by
             extension, unknown = generic. An iconset NAME from the pure
             table - currentColor only, the row's dim/hover state carries
             the color. Since t_63582d1f the icon IS the expand affordance
             (no chevron rides beside it). -->
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
      <!-- card t_ccd4a000: a failed expansion renders NOTHING in the
           tree body - no error row, no backend string. The branch
           collapses and the store hints via the local toast funnel;
           clicking the folder row again retries (the failed cache was
           dropped, so the refetch fires). -->
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
        :style="{ paddingInlineStart: `${depth * 14 + 24}px` }">
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
  } from "../../composables/filetree";
  import { joinRelPath } from "../../composables/omega";
  import type { FileEntry } from "../../composables/omega";
  import { FOCUSED_SESSION_KEY } from "../../composables/viewer";
  import { folderIcon, glyphKindFor, glyphIcon } from "./rowicon";
  // card t_502b8e81: the iconset wrapper - the tree's row glyphs
  import Icon from "../ui/Icon.vue";

  // card t_a5f9d185: every branch expansion roots on the focused
  // session's RESOLVED root - the id is read live at fetch time, so a
  // re-rooted tab fetches branch listings under the new root. The app
  // always provides; a null fallback keeps the tree harmless.
  const focusedSession = inject(FOCUSED_SESSION_KEY, null);

  // File tree (VS Code-style, chevronless since t_63582d1f): one row per
  // entry, the folder icon toggles expand in place, and expanding a
  // directory lazy-loads exactly one level via /files/browse. The tree
  // IS the location - there is no navigate-in and no separate listing
  // pane. Clicking (or Enter/Space on a focused row for) a file emits
  // `open` (card t_36ceb99b: the host opens it as a main-pane viewer
  // tab - the inline preview is gone).
  const props = defineProps<{
    entries: FileEntry[];
    // path of the directory these entries belong to ("" = the root)
    base: string;
    depth: number;
    // the module-UI narrow flag (card t_a2fdc630): passed DOWN by the
    // measuring host (FilesTab) - a recursive child never observes its
    // own width (each instance would measure its indent, not the pane);
    // one measurable root owns the flip, descendants inherit it.
    narrow?: boolean;
  }>();

  const emit = defineEmits<{
    open: [path: string];
    // every row click, files and folders alike - the toolbar creates at
    // the selected folder (or a selected file's folder)
    select: [sel: { path: string; isDir: boolean }];
  }>();

  // State lives in the filetree store (card t_ccd4a000: extracted to
  // app/composables/filetree.ts so the node suite drives expansion,
  // failures and restores without a DOM). Created once by the root
  // instance and shared down the recursion via provide/inject - every
  // rendered branch reads and mutates the same maps.
  let store = inject<FileTreeStore | undefined>("omega-filetree", undefined);
  if (!store) {
    store = createFileTreeStore({
      session: () => focusedSession?.value ?? undefined,
    });
    provide("omega-filetree", store);
  }

  const indent = computed(() => `${props.depth * 14 + 10}px`);

  // t_63582d1f hover indent guides: every level ul carries ONE hairline
  // as an absolutely-positioned ::before overlay (never flow content -
  // the reveal is opacity-only, zero layout shift). The per-level ul
  // padding (8px x nesting depth, main.css) already sits inside the
  // containing block, so depth*14 + 12px from the level ul's edge puts
  // the line 6px before that level's first glyph.
  const guideStyle = computed(() =>
    props.depth > 0
      ? ({ "--guide-x": `${props.depth * 14 + 12}px` } as Record<string, string>)
      : undefined,
  );

  function entryPath(entry: FileEntry): string {
    return joinRelPath(props.base, entry.name);
  }

  // the row's iconset name: folders by open state, files by extension
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

  // One activation path for the pointer AND the keyboard: select always;
  // folders toggle their branch, files open the viewer tab. Clicks keep
  // the old stopPropagation semantics (ev optional - the keyboard has
  // none to stop).
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
  /* narrow (card t_a2fdc630): measured by the host (FilesTab passes the
     flag down - never a per-instance observer, see the prop comment).
     The size column is the first thing that stops fitting: gone under
     narrow, the name keeps the full row. Icon-state + hover guides are
     pixel-level truths - narrow mode keeps BOTH laws (t_63582d1f ships
     no narrow exemption). */
  .files-tree.narrow .size {
    display: none;
  }

  /* t_63582d1f hover indent guides: a hairline per nesting level, an
     absolute ::before OVERLAY on the branch (never flow content - the
     reveal cannot shift layout), shown purely by CSS :hover on the tree
     containers (t_e539ba29's invisible-lines law: chrome reveals on
     intent; zero listeners, zero JS). Hovering anything inside the tree
     makes every ancestor ul :hover, so the descendant-of-hovered rule
     shows ALL level lines at once under one fast subtle fade. Depth-0
     carries no line (there is no level above the root to mark; the
     root ul is the one without the guided class). */
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