<template>
  <aside
    ref="rootEl"
    class="aside pane inspector-panel"
    :class="{ open: open, narrow: narrow }">
    <div class="inspector-files">
      <!-- card t_a5f9d185: the VS Code-style section header - workspace
           name flush LEFT, the 4 action icons (t_a4504a25: add file /
           add folder / refresh / collapse) flush RIGHT on ONE row, tree
           rows' rhythm (~22px), no container box, tree starts directly
           below. Ruled with Endo: NO resolvable workspace = NO header
           (the cwd fallback renders a bare tree; removing the header
           removes the feature row - true deletability). -->
      <div
        v-if="headerName"
        class="files-header">
        <span
          class="files-header-name"
          :title="headerRoot">{{ headerName }}</span>
        <div
          class="files-header-actions"
          role="toolbar"
          aria-label="files actions">
          <button
            class="files-tool"
            title="add file"
            aria-label="add file"
            :disabled="toolsLocked"
            @click="startCreate('file')">
            <Icon name="file-plus" :size="13" />
          </button>
          <button
            class="files-tool"
            title="add folder"
            aria-label="add folder"
            :disabled="toolsLocked"
            @click="startCreate('folder')">
            <Icon name="folder-plus" :size="13" />
          </button>
          <button
            class="files-tool"
            title="refresh"
            aria-label="refresh files"
            :disabled="loadingRoot"
            @click="refreshFiles">
            <Icon name="refresh" :size="13" />
          </button>
          <button
            class="files-tool"
            title="collapse folders"
            aria-label="collapse folders"
            @click="collapseFiles">
            <Icon name="unfoldless" :size="13" />
          </button>
        </div>
      </div>
      <form
        v-if="creating"
        class="files-add"
        @submit.prevent="confirmCreate">
        <input
          v-model="createName"
          class="files-add-name"
          :placeholder="creating === 'file' ? 'new file name' : 'new folder name'"
          aria-label="new node name" />
        <button
          type="submit"
          class="files-add-go"
          :disabled="!createName.trim() || creatingBusy">
          {{ creating === "file" ? "create file" : "create folder" }}
        </button>
      </form>
      <p
        v-if="toolError"
        class="files-status files-tool-error"
        role="alert">
        {{ toolError }}
      </p>
      <ul class="files-list">
        <li
          v-if="rootError"
          class="files-status">
          {{ rootError }}
        </li>
        <li
          v-else-if="loadingRoot"
          class="files-status">
          loading
        </li>
        <li
          v-else-if="!rootEntries.length"
          class="files-status">
          empty directory
        </li>
      </ul>
      <FileTree
        v-if="rootEntries.length"
        :entries="rootEntries"
        base=""
        :depth="0"
        :narrow="narrow"
        @open="openFile"
        @select="onSelect" />
    </div>
  </aside>
</template>

<script setup lang="ts">
  // the inspector's FILES tab (card t_4ae27e0d): the pre-reset
  // RightSidebar's chip strip dissolved - files is a REAL dock tab whose
  // content the dock tabbar hosts (app.vue's pane-files outlet). The
  // module views (cron / logs / skills / tools) live beside it through
  // InspectorPanel.vue; module-owned components stayed untouched.
  // Lazy: the tab unmounts when the dock switches views, so the root
  // listing refetches on every re-entry (fresher than the old
  // fetch-once guard, and never a poll - nothing here loops).
  import { computed, inject, ref, watch } from "vue";
  import {
    createTargetDir,
    fetchFilesBrowse,
    fileTreeBus,
    filesHeaderVisible,
    filesRootName,
    joinRelPath,
    postFilesMkdir,
    postFilesWrite,
    sortFileEntries,
  } from "../../composables/omega";
  import type { FileEntry, FilesBrowsePayload, SelectedNode } from "../../composables/omega";
  import { CHAT_HOST_KEY } from "../../composables/chat";
  import { useNarrow } from "~/composables/narrow";
  import { DOCK_VIEWER_OPEN_KEY, FOCUSED_SESSION_KEY } from "../../composables/viewer";
  import type { ViewerRef } from "../../composables/dockables";
  import FileTree from "./FileTree.vue";
  // card t_a4504a25: the toolbar's glyph set (icons.ts vocabulary)
  import Icon from "../ui/Icon.vue";

  // `open` is the mobile drawer overlay state (desktop tabs never pass it
  // up - the pane is always shown). Root listing loads on mount: the dock
  // only activates a tab's content when the tab IS active.
  defineProps<{
    open?: boolean;
  }>();

  // the narrow adaptation - the module-UI recipe (card t_a2fdc630):
  // the root measures its OWN rendered container (this tab rides both
  // the inspector column and main-pane-width mounts) and flips the
  // class; the tree gets it passed down (the size-column collapse is
  // the flip's only consumer now).
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  const rootEntries = ref<FileEntry[]>([]);
  const loadingRoot = ref(false);
  const rootError = ref("");

  // --- root resolution law, client half (card t_a5f9d185) ---
  // the tab sends the focused chat tab's SESSION ID (never a root) with
  // every request; the server resolves session workspace ->
  // default_workspace -> cwd. The watch re-roots when the focus moves to
  // another session - and when the focused session's row workspace
  // changes (the workspace chip writes the row + refreshSessions feeds
  // it back). The header shows exactly what the server resolved: name
  // = basename, hidden entirely when the cwd fallback stands.
  const focusedSession = inject(FOCUSED_SESSION_KEY, null);
  const sessionId = computed(() => focusedSession?.value ?? undefined);

  const headerRoot = ref("");
  const headerSource = ref<FilesBrowsePayload["source"]>(undefined);
  const headerName = computed(() =>
    filesHeaderVisible(headerSource.value) ? filesRootName(headerRoot.value) : "",
  );

  // card t_36ceb99b: the opener seam - a file click MINTS/FOCUSES a
  // main-pane viewer tab; the app provides always (mobile drawer mounts
  // included). No inline preview survives here.
  const openViewerRef = inject(DOCK_VIEWER_OPEN_KEY, null);

  function openFile(path: string) {
    openViewerRef?.({ kind: "file", name: path } satisfies ViewerRef);
  }

  async function loadRoot() {
    loadingRoot.value = true;
    rootError.value = "";
    try {
      const data = await fetchFilesBrowse("", sessionId.value);
      rootEntries.value = sortFileEntries(data.entries ?? []);
      headerRoot.value = data.root ?? "";
      headerSource.value = data.source;
    } catch (e) {
      rootError.value = e instanceof Error ? e.message : String(e);
    } finally {
      loadingRoot.value = false;
    }
  }

  // re-root triggers: a different focused session, or the focused
  // session's row workspace changing (the chip's write + the refreshed
  // row both flow through here). refreshFiles (not loadRoot) so the
  // bus also drops the tree's cached branch listings - a relative path
  // means a different directory under the new root. No immediate
  // watcher - the mount call below loads once already.
  const chatHost = inject(CHAT_HOST_KEY, null);
  const rowWorkspace = computed(() => {
    const id = focusedSession?.value ?? null;
    return id ? (chatHost?.sessionRow(id)?.workspace ?? null) : null;
  });
  watch([sessionId, rowWorkspace], () => {
    void refreshFiles();
  });

  // --- toolbar (card t_a4504a25) ---

  function onSelect(sel: SelectedNode) {
    selected.value = sel;
  }

  // create buttons only: a refused root listing is "no workspace under
  // this tab" - refresh must stay live to recover it, collapse is cheap
  const toolsLocked = computed(() => !!rootError.value || loadingRoot.value);

  type CreateKind = "file" | "folder";
  const creating = ref<CreateKind | null>(null);
  const createName = ref("");
  const creatingBusy = ref(false);
  const toolError = ref("");
  const selected = ref<SelectedNode | null>(null);

  function startCreate(kind: CreateKind) {
    creating.value = kind;
    createName.value = "";
    toolError.value = "";
  }

  // confirmCreate POSTs the validated name at the selected folder (or
  // the tree root): server refusals land in the inline error, successes
  // refresh the root + every expanded branch, reveal the new node, and
  // select it (files open their viewer tab, folders become the target).
  async function confirmCreate() {
    const kind = creating.value;
    if (!kind || creatingBusy.value) return;
    const name = createName.value.trim();
    if (!name) return;
    creatingBusy.value = true;
    toolError.value = "";
    const path = joinRelPath(createTargetDir(selected.value), name);
    try {
      if (kind === "file") {
        await postFilesWrite(path, "", sessionId.value);
      } else {
        await postFilesMkdir(path, sessionId.value);
      }
    } catch (e) {
      toolError.value = e instanceof Error ? e.message : String(e);
      creatingBusy.value = false;
      return;
    }
    creatingBusy.value = false;
    creating.value = null;
    await refreshFiles();
    fileTreeBus.reveal = path;
    if (kind === "file") {
      await openFile(path);
    } else {
      selected.value = { path, isDir: true };
    }
  }

  async function refreshFiles() {
    await loadRoot();
    fileTreeBus.refresh++;
  }

  function collapseFiles() {
    fileTreeBus.collapse++;
  }

  loadRoot();
</script>

<style scoped>
  /* the header (card t_a5f9d185): ONE VS Code-style row - workspace
     name flush left, the 4 action icons flush right on the same line -
     at the tree rows' rhythm (~22px), no container box, nothing docked
     above the tree. Dimmed icons at rest, the dock-tab hover raises. */
  .files-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 4px;
    height: 22px;
    padding: 0 8px;
  }
  .files-header-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--mono);
    font-size: 12px;
    color: var(--text);
  }
  .files-header-actions {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-shrink: 0;
  }
  .files-tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 20px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    border-radius: var(--r-chip);
    cursor: pointer;
  }
  .files-tool:hover:not(:disabled) {
    background: var(--z2);
    color: var(--text);
  }
  .files-tool:disabled {
    opacity: 0.4;
    cursor: default;
  }

  /* the inline create form - the agents panel's create shape, one row */
  .files-add {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 2px 8px 6px;
  }
  .files-add-name {
    flex: 1;
    min-width: 0;
    font-family: var(--mono);
    font-size: 11px;
    padding: 3px 6px;
    background: var(--z1-raised);
    color: var(--text);
    border: 0;
    border-radius: var(--r-chip);
  }
  .files-add-go {
    flex-shrink: 0;
    border: 0;
    background: transparent;
    font-family: var(--mono);
    font-size: 10.5px;
    color: var(--text-dim);
    cursor: pointer;
  }
  .files-add-go:hover:not(:disabled) {
    color: var(--text);
  }
  .files-add-go:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .files-tool-error {
    color: var(--text-dim);
  }

  /* narrow (card t_a2fdc630): the tree gets the flag passed down - once
       the preview left (t_36ceb99b), the tab is just the tree; the
       recipe's measuring root stays (the flip is the tree's size-column
       collapse trigger). */
</style>
