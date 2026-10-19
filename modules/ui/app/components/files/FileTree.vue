<template>
  <ul class="files-tree">
    <li
      v-for="entry in entries"
      :key="entry.name">
      <div
        class="tree-row"
        :class="{ 'is-dir': entry.is_dir }"
        :style="{ paddingInlineStart: indent }"
        :title="entryPath(entry)"
        @click="activate(entry, $event)">
        <span
          v-if="entry.is_dir"
          class="chev"
          :class="{ open: isOpen(entryPath(entry)) }"
          role="presentation" />
        <span
          v-else
          class="chev file"
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
          class="size">{{ entry.size_bytes }}</span
        >
      </div>
      <p
        v-if="errorOf(entryPath(entry))"
        class="tree-error"
        :style="{ paddingInlineStart: `${depth * 14 + 24}px` }">
        {{ errorOf(entryPath(entry)) }}
      </p>
      <FileTree
        v-else-if="entry.is_dir && isOpen(entryPath(entry)) && !isLoading(entryPath(entry))"
        :entries="childrenOf(entryPath(entry))"
        :base="entryPath(entry)"
        :depth="depth + 1"
        @open="(p) => emit('open', p)" />
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
  import { computed, inject, provide, reactive, watch } from "vue";
  import {
    FILETREE_STATE_KEY,
    fetchFilesBrowse,
    joinRelPath,
    parseExpandedPaths,
    sortFileEntries,
    serializeExpandedPaths,
  } from "../../composables/omega";
  import type { FileEntry } from "../../composables/omega";

  // Inspector file tree (VS Code-style): one row per entry, chevron rows
  // expand in place, and expanding a directory lazy-loads exactly one
  // level via /files/browse. The tree IS the location - there is no
  // navigate-in and no separate listing pane. Clicking a file emits
  // `open` and RightSidebar keeps the read-only preview responsibility.
  const props = defineProps<{
    entries: FileEntry[];
    // path of the directory these entries belong to ("" = the root)
    base: string;
    depth: number;
  }>();

  const emit = defineEmits<{ open: [path: string] }>();

  interface TreeStore {
    isOpen(path: string): boolean;
    isLoading(path: string): boolean;
    errorOf(path: string): string;
    childrenOf(path: string): FileEntry[];
    toggle(path: string): void;
  }

  // State is created once by the root instance and shared down the
  // recursion via provide/inject - every rendered branch reads and
  // mutates the same expanded/children/error maps.
  let store: TreeStore | undefined = inject<TreeStore | undefined>(
    "omega-filetree",
    undefined,
  );
  if (!store) {
    const expanded = reactive(new Set<string>());
    const loading = reactive(new Set<string>());
    const kids: Record<string, FileEntry[]> = reactive({});
    const errs: Record<string, string> = reactive({});

    // ensureLoaded fetches one directory level for an expanded branch;
    // in-flight and already-loaded reads are no-ops. A failed read
    // collapses the branch and surfaces the error inline on the row.
    // Lives in this block: it closes over kids/loading/errs/expanded.
    function ensureLoaded(path: string) {
      if (kids[path] || loading.has(path)) return;
      kids[path] ??= [];
      loading.add(path);
      fetchFilesBrowse(path)
        .then((data) => {
          kids[path] = sortFileEntries(data.entries ?? []);
          delete errs[path];
        })
        .catch((e) => {
          delete kids[path];
          errs[path] = e instanceof Error ? e.message : String(e);
          expanded.delete(path);
        })
        .finally(() => loading.delete(path));
    }

    try {
      for (const p of parseExpandedPaths(
        localStorage.getItem(FILETREE_STATE_KEY) ?? "",
      ))
        expanded.add(p);
    } catch {
      // no localStorage (or blocked): state is per-session, fine
    }
    watch(
      expanded,
      () => {
        try {
          localStorage.setItem(
            FILETREE_STATE_KEY,
            serializeExpandedPaths(expanded),
          );
        } catch {
          // persistence is a courtesy, never a failure
        }
      },
    );

    store = {
      isOpen: (p) => expanded.has(p),
      isLoading: (p) => loading.has(p),
      errorOf: (p) => errs[p] ?? "",
      childrenOf: (p) => kids[p] ?? [],
      toggle(path: string) {
        if (expanded.has(path)) {
          expanded.delete(path);
          return;
        }
        expanded.add(path);
        ensureLoaded(path);
      },
    };

    // Restored branches have no toggle to fire their lazy load - fetch
    // every persisted level up front so a reloaded tree renders whole.
    for (const p of [...expanded]) ensureLoaded(p);
    provide("omega-filetree", store);
  }

  const indent = computed(() => `${props.depth * 14 + 10}px`);

  function entryPath(entry: FileEntry): string {
    return joinRelPath(props.base, entry.name);
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

  function errorOf(path: string): string {
    return store!.errorOf(path);
  }

  function activate(entry: FileEntry, ev: MouseEvent) {
    const path = entryPath(entry);
    if (entry.is_dir) {
      ev.stopPropagation();
      store!.toggle(path);
    } else emit("open", path);
  }
</script>