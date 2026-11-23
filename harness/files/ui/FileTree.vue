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
        @contextmenu="onRowMenu($event, entry)"
        @keydown.enter.prevent="activate(entry)"
        @keydown.space.prevent="activate(entry)">
        <!-- VS Code-style row glyph: folders by expand state, files by kind. -->
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
      <FileTree
        v-if="entry.is_dir && isOpen(entryPath(entry)) && !isLoading(entryPath(entry))"
        :entries="childrenOf(entryPath(entry))"
        :base="entryPath(entry)"
        :depth="depth + 1"
        :narrow="narrow"
        @open="(p) => emit('open', p)"
        @select="(s) => emit('select', s)"
        @create="(n) => emit('create', n)"
        @rename="(p) => emit('rename', p)"
        @delete="(p) => emit('delete', p)"
        @notice="(m) => emit('notice', m)" />
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
  import { computed, inject, provide, ref } from "vue";
  import {
    createFileTreeStore,
    type FileTreeStore,
  } from "~/composables/filetree";
  import { fetchFilesRead, filesRawUrl, joinRelPath } from "~/composables/omega";
  import type { FileEntry } from "~/composables/omega";
  import { FOCUSED_SESSION_KEY } from "~/composables/viewer";
  import { CHAT_HOST_KEY } from "~/composables/chat";
  import { openContextMenu } from "~/composables/contextMenu";
  import { copyText } from "~/composables/clipboard";
  import { folderIcon, glyphKindFor, glyphIcon } from "./rowicon";
  import { fileMenuItems } from "./fileMenu";
  import Icon from "~/components/ui/Icon.vue";

  // every branch expansion roots on the focused session's RESOLVED root.
  const focusedSession = inject(FOCUSED_SESSION_KEY, null);
  // the chat lanes (Add file to chat) land in the focused session's own tab.
  const chatHost = inject(CHAT_HOST_KEY, null);

  const props = defineProps<{
    entries: FileEntry[];
    base: string;
    depth: number;
    // the narrow flag is passed DOWN by the measuring host (FilesTab): a recursive child
    // would measure its own indent, not the pane.
    narrow?: boolean;
  }>();

  const emit = defineEmits<{
    open: [path: string];
    select: [sel: { path: string; isDir: boolean }];
    // New: the host owns the one new-node form (POST /files/write + /files/mkdir).
    create: [node: { path: string; isDir: boolean }];
    // Rename: the host's one form opens in rename mode with this node as the target.
    rename: [path: string];
    // Delete: the host runs POST /files/delete (the menu already armed the second pick).
    delete: [path: string];
    // the tab's status line: a copy or an attach answers through it, never a silent no-op.
    notice: [text: string];
  }>();

  let store = inject<FileTreeStore | undefined>("omega-filetree", undefined);
  if (!store) {
    store = createFileTreeStore({
      session: () => focusedSession?.value ?? undefined,
    });
    provide("omega-filetree", store);
  }

  const indent = computed(() => `${props.depth * 4 + 8}px`);

  // hover indent guides: one hairline per level, an absolutely-positioned :before overlay
  // (never flow content, so the reveal cannot shift layout).
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

  /** the focused session's chat tab: the tab the composer's attach lane writes into. */
  function chatKey(): string | null {
    const session = focusedSession?.value ?? null;
    if (!chatHost || !session) return null;
    for (const [key, st] of chatHost.states) {
      if (st.sessionId === session) return key;
    }
    return null;
  }

  function baseName(path: string): string {
    return path.split("/").pop() || path;
  }

  /** the menu's New: the host's own form, with this row as the target directory. */
  function createIn(entry: FileEntry) {
    const node = { path: entryPath(entry), isDir: entry.is_dir };
    emit("select", node);
    emit("create", node);
  }

  /**
   * Copy takes the file's TEXT (the one read route). A binary or a truncated read has
   * nothing the clipboard can carry, so the notice says so instead of copying garbage.
   */
  async function copyFile(path: string): Promise<void> {
    const name = baseName(path);
    try {
      const data = await fetchFilesRead(path, focusedSession?.value ?? undefined);
      if (data.binary) {
        emit("notice", `copy refused: ${name} is binary`);
        return;
      }
      if (data.truncated) {
        emit("notice", `copy refused: ${name} is past the 64 KiB read window`);
        return;
      }
      emit(
        "notice",
        (await copyText(data.content))
          ? `copied ${name}`
          : "copy refused: the clipboard is unavailable on this page",
      );
    } catch (e) {
      emit("notice", `copy failed: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  async function copyPath(path: string): Promise<void> {
    emit(
      "notice",
      (await copyText(path))
        ? `copied path ${path}`
        : "copy refused: the clipboard is unavailable on this page",
    );
  }

  /**
   * Add file to chat rides the composer's own attach lane: raw bytes into a File, then
   * the host's /attach-file upload parks the reference on the focused tab.
   */
  async function addToChat(path: string): Promise<void> {
    const key = chatKey();
    if (!chatHost || !key) return;
    const name = baseName(path);
    try {
      // the download lane: /files/raw renders image/* and pdf only, so any other file
      // needs ?download=1 to serve its bytes at all.
      const res = await fetch(
        filesRawUrl(path, focusedSession?.value ?? undefined, { download: true }),
      );
      if (!res.ok) throw new Error(`read: ${res.status} ${(await res.text()).trim()}`);
      const blob = await res.blob();
      const file = new File([blob], name, { type: blob.type || "application/octet-stream" });
      await chatHost.attachFileRefs(key, [file]);
      emit("notice", `attached ${name} to the focused chat`);
    } catch (e) {
      emit("notice", `attach failed: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  /**
   * THE ROW MENU: New targets this row, Rename hands the path to the tab's one form,
   * Open/Copy/Add file to chat need a FILE row, Copy path always works, and the chat rows
   * are absent without a bound chat tab. Delete arms on the first pick (the row re-reads
   * "Confirm delete") and fires on the second - the board card's one shape.
   */
  const armedDelete = ref("");
  let armedTimer: ReturnType<typeof setTimeout> | undefined;
  const DELETE_ARM_MS = 6000;

  function disarmDelete() {
    armedDelete.value = "";
    if (armedTimer) {
      clearTimeout(armedTimer);
      armedTimer = undefined;
    }
  }

  function onDeleteRow(path: string) {
    if (armedDelete.value !== path) {
      disarmDelete();
      armedDelete.value = path;
      armedTimer = setTimeout(disarmDelete, DELETE_ARM_MS);
      emit("notice", `right-click ${path.split("/").pop()} again and pick "Confirm delete"`);
      return;
    }
    disarmDelete();
    emit("delete", path);
  }

  function onRowMenu(e: MouseEvent, entry: FileEntry) {
    const path = entryPath(entry);
    // a right-click selects what it menus (the shared menu's own law)
    emit("select", { path, isDir: entry.is_dir });
    const items = fileMenuItems(
      {
        isDir: entry.is_dir,
        chatBound: chatKey() !== null,
        armedDelete: armedDelete.value === path,
      },
      {
        create: () => createIn(entry),
        rename: () => emit("rename", path),
        open: () => emit("open", path),
        copy: () => void copyFile(path),
        copyPath: () => void copyPath(path),
        addToChat: () => void addToChat(path),
        remove: () => onDeleteRow(path),
      },
    );
    if (items.length === 0) return;
    e.preventDefault();
    void openContextMenu(e.clientX, e.clientY, items);
  }
</script>

<style scoped>
  /* narrow: measured by the host (FilesTab), never by the tree itself. */
  .files-tree.narrow .size {
    display: none;
  }

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