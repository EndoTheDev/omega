<template>
  <aside
    ref="rootEl"
    class="aside pane inspector-panel"
    :class="{ open: open, narrow: narrow }">
    <div class="inspector-files">
      <!--
        the VS Code-style section header.
        Ruled with Endo: NO resolvable workspace = NO header (the cwd fallback renders
        a bare tree; removing the header removes the feature row - true deletability).:
        the band is the SHARED TabBand now (the universal top-bar contract this tab
        defined) - and it is structurally pinned: the band renders BEFORE the scroll
        body, so a long tree never scrolls it away.
      -->
      <TabBand
        v-if="headerName"
        :title="headerName"
        :title-attr="headerRoot">
        <button
          class="band-tool"
          title="add file"
          aria-label="add file"
          :disabled="toolsLocked"
          @click="startCreate('file')">
          <Icon name="file-plus" :size="13" />
        </button>
        <button
          class="band-tool"
          title="add folder"
          aria-label="add folder"
          :disabled="toolsLocked"
          @click="startCreate('folder')">
          <Icon name="folder-plus" :size="13" />
        </button>
        <button
          class="band-tool"
          title="refresh"
          aria-label="refresh files"
          :disabled="loadingRoot"
          @click="refreshFiles">
          <Icon name="refresh" :size="13" />
        </button>
        <button
          class="band-tool"
          title="collapse folders"
          aria-label="collapse folders"
          @click="collapseFiles">
          <Icon name="unfoldless" :size="13" />
        </button>
      </TabBand>
      <div class="files-scroll">
        <form
          v-if="creating"
          class="files-add"
          @submit.prevent="confirmCreate">
          <input
            v-model="createName"
            class="files-add-name"
            :placeholder="creating === 'file' ? 'new file name' : creating === 'folder' ? 'new folder name' : 'new name'"
            aria-label="new node name" />
          <button
            type="submit"
            class="files-add-go"
            :disabled="!createName.trim() || creatingBusy">
            {{ creating === "file" ? "create file" : creating === "folder" ? "create folder" : "rename" }}
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
          @select="onSelect"
          @create="onCreateNode"
          @rename="onRenameNode"
          @delete="onDeleteNode"
          @notice="(m) => (toolError = m)" />
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
  // the inspector's FILES tab. Lazy: the tab unmounts when the dock switches views, so
  // the root listing refetches on every re-entry (fresher than the old fetch-once
  // guard, and never a poll - nothing here loops).
  import { computed, inject, ref, watch } from "vue";
  import {
    createTargetDir,
    fetchFilesBrowse,
    fileTreeBus,
    filesHeaderVisible,
    filesRootName,
    joinRelPath,
    postFilesDelete,
    postFilesMkdir,
    postFilesRename,
    postFilesWrite,
    sortFileEntries,
  } from "~/composables/omega";
  import { latestWins } from "~/composables/latestfetch";
  import type { FileEntry, FilesBrowsePayload, SelectedNode } from "~/composables/omega";
  import { CHAT_HOST_KEY } from "~/composables/chat";
  import { useNarrow } from "~/composables/narrow";
  import { DOCK_VIEWER_OPEN_KEY, FOCUSED_SESSION_KEY } from "~/composables/viewer";
  import type { ViewerRef } from "~/composables/dockables";
  import FileTree from "./FileTree.vue";
  import Icon from "~/components/ui/Icon.vue";

  // `open` is the mobile drawer overlay state.
  defineProps<{
    open?: boolean;
  }>();

  // the narrow adaptation - the module-UI recipe.
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  const rootEntries = ref<FileEntry[]>([]);
  const loadingRoot = ref(false);
  const rootError = ref("");

  // root resolution law, client half --- the tab sends the focused chat tab's SESSION
  // ID (never a root) with every request; the server resolves session workspace ->
  // default_workspace -> cwd. The header shows exactly what the server resolved: name =
  // basename, hidden entirely when the cwd fallback stands.
  const focusedSession = inject(FOCUSED_SESSION_KEY, null);
  const sessionId = computed(() => focusedSession?.value ?? undefined);

  const headerRoot = ref("");
  const headerSource = ref<FilesBrowsePayload["source"]>(undefined);
  const headerName = computed(() =>
    filesHeaderVisible(headerSource.value) ? filesRootName(headerRoot.value) : "",
  );

  // the opener seam - a file click MINTS/FOCUSES a main-pane viewer tab.
  const openViewerRef = inject(DOCK_VIEWER_OPEN_KEY, null);

  function openFile(path: string) {
    openViewerRef?.({ kind: "file", name: path } satisfies ViewerRef);
  }

  // the boot fetch races its own re-root. The boot response can resolve AFTER the
  // refetch's and clobber it (last-writer-wins: the tab sat wrong on the fallback root
  // until a remount). A round counter keeps the LATEST-REQUESTED fetch as the only
  // writer: a stale response is dropped, never rendered.
  const rootRounds = latestWins();
  async function loadRoot() {
    const admitted = rootRounds.begin();
    loadingRoot.value = true;
    rootError.value = "";
    try {
      const data = await fetchFilesBrowse("", sessionId.value);
      if (!admitted()) return; // a newer root request owns the state
      rootEntries.value = sortFileEntries(data.entries ?? []);
      headerRoot.value = data.root ?? "";
      headerSource.value = data.source;
    } catch (e) {
      if (!admitted()) return;
      rootError.value = e instanceof Error ? e.message : String(e);
    } finally {
      if (admitted()) loadingRoot.value = false;
    }
  }

  const chatHost = inject(CHAT_HOST_KEY, null);
  const rowWorkspace = computed(() => {
    const id = focusedSession?.value ?? null;
    return id ? (chatHost?.sessionRow(id)?.workspace ?? null) : null;
  });
  watch([sessionId, rowWorkspace], () => {
    void refreshFiles();
  });


  function onSelect(sel: SelectedNode) {
    selected.value = sel;
  }

  // the tree row menu's New: the row IS the target, then the tab's one form opens (the
  // band's add-folder button flips the same form to POST /files/mkdir).
  function onCreateNode(node: SelectedNode) {
    selected.value = node;
    startCreate("file");
  }

  // create buttons only: a refused root listing is "no workspace under
  // this tab" - refresh must stay live to recover it, collapse is cheap
  const toolsLocked = computed(() => !!rootError.value || loadingRoot.value);

  type CreateKind = "file" | "folder" | "rename";
  const creating = ref<CreateKind | null>(null);
  const createName = ref("");
  const creatingBusy = ref(false);
  const toolError = ref("");
  const selected = ref<SelectedNode | null>(null);
  // the node a rename form is editing (the path the menu pointed at)
  const renameTarget = ref("");

  function startCreate(kind: "file" | "folder") {
    creating.value = kind;
    createName.value = "";
    renameTarget.value = "";
    toolError.value = "";
  }

  // Rename reuses the ONE form: the target stays where it is, only the leaf changes.
  function onRenameNode(path: string) {
    creating.value = "rename";
    renameTarget.value = path;
    createName.value = path.split("/").pop() ?? "";
    toolError.value = "";
  }

  async function onDeleteNode(path: string) {
    toolError.value = "";
    try {
      await postFilesDelete(path, sessionId.value);
    } catch (e) {
      toolError.value = e instanceof Error ? e.message : String(e);
      return;
    }
    await refreshFiles();
  }

  async function confirmCreate() {
    const kind = creating.value;
    if (!kind || creatingBusy.value) return;
    const name = createName.value.trim();
    if (!name) return;
    creatingBusy.value = true;
    toolError.value = "";
    if (kind === "rename") {
      try {
        await postFilesRename(renameTarget.value, name, sessionId.value);
      } catch (e) {
        toolError.value = e instanceof Error ? e.message : String(e);
        creatingBusy.value = false;
        return;
      }
      creatingBusy.value = false;
      creating.value = null;
      await refreshFiles();
      return;
    }
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
  /*
   * the band + scroll shape.
   * `.inspector-files` stops being the scrollport - it is the column that anchors the
   * band; the scroll rules moved to `.files-scroll` (overflow is the BODY's, the band
   * can never scroll away).
   */
  .inspector-files {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .files-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding-bottom: 8px;
  }
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

</style>