<template>
  <aside
    ref="rootEl"
    class="aside pane inspector-panel"
    :class="{ open: open, narrow: narrow }">
    <div class="inspector-files">
      <!-- card t_a4504a25: the tab's toolbar - add file / add folder /
           refresh / collapse, icons.ts vocabulary. Rendered even in the
           empty-root state (the first file of an empty workspace is
           created right here); the create buttons alone disable when
           the root listing refused to load. -->
      <div
        class="files-toolbar"
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

    <div
      v-if="preview"
      class="file-preview">
      <div class="preview-head">
        <span class="preview-path">{{ preview.path }}</span>
        <button
          class="preview-close"
          title="Close preview"
          @click="preview = null">
          ×
        </button>
      </div>
      <p
        v-if="preview.error"
        class="preview-notice">
        {{ preview.error }}
      </p>
      <p
        v-else-if="preview.binary"
        class="preview-notice">
        binary file - no preview
      </p>
      <template v-else>
        <p
          v-if="preview.truncated"
          class="preview-notice">
          truncated at 64 KiB
        </p>
        <pre class="preview-body">{{ preview.content }}</pre>
      </template>
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
  import { computed, ref } from "vue";
  import {
    createTargetDir,
    fetchFilesBrowse,
    fetchFilesRead,
    fileTreeBus,
    joinRelPath,
    postFilesMkdir,
    postFilesWrite,
    sortFileEntries,
  } from "../../composables/omega";
  import type { FileEntry, SelectedNode } from "../../composables/omega";
  import { useNarrow } from "~/composables/narrow";
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
  // class; the tree gets it passed down, the scoped stylesheet below
  // rebalances the preview share.
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  const rootEntries = ref<FileEntry[]>([]);
  const loadingRoot = ref(false);
  const rootError = ref("");

  interface Preview {
    path: string;
    content: string;
    truncated: boolean;
    binary: boolean;
    error?: string;
  }
  const preview = ref<Preview | null>(null);

  async function loadRoot() {
    loadingRoot.value = true;
    rootError.value = "";
    try {
      const data = await fetchFilesBrowse("");
      rootEntries.value = sortFileEntries(data.entries ?? []);
    } catch (e) {
      rootError.value = e instanceof Error ? e.message : String(e);
    } finally {
      loadingRoot.value = false;
    }
  }

  async function openFile(path: string) {
    preview.value = { path, content: "", truncated: false, binary: false };
    try {
      const data = await fetchFilesRead(path);
      preview.value = {
        path,
        content: data.content,
        truncated: data.truncated,
        binary: data.binary,
      };
    } catch (e) {
      preview.value = {
        path,
        content: "",
        truncated: false,
        binary: false,
        error: e instanceof Error ? e.message : String(e),
      };
    }
  }

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
  // select it (files open their preview, folders become the target).
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
        await postFilesWrite(path, "");
      } else {
        await postFilesMkdir(path);
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
  /* the toolbar (card t_a4504a25): one row of icon buttons above the
     tree, the same mono-dim voice as the rows below - currentColor
     glyphs, hover raises, disabled dims. */
  .files-toolbar {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 0 8px 4px;
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
    background: var(--z1-raised);
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

  /* narrow: the module-UI recipe (card t_a2fdc630). The tab is already
     one column (tree above, preview below); narrow rebalances the
     vertical split - at ~300px a 45%-capped preview shows a header and
     two lines, so it takes the larger share instead (the tree scrolls,
     code wants lines). Scoped here, not in main.css: these rules are
     this tab's narrow view, not shell geometry. */
  .inspector-panel.narrow .file-preview {
    max-height: 64%;
    padding: 6px 8px 8px;
  }
  .inspector-panel.narrow .preview-body {
    font-size: 10.5px;
    padding: 6px 8px;
  }
  .inspector-panel.narrow .preview-head {
    margin-bottom: 4px;
  }
</style>
