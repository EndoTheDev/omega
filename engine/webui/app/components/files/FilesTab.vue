<template>
  <aside
    ref="rootEl"
    class="aside pane inspector-panel"
    :class="{ open: open, narrow: narrow }">
    <div class="inspector-files">
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
        @open="openFile" />
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
  import { ref } from "vue";
  import { fetchFilesBrowse, fetchFilesRead, sortFileEntries } from "../../composables/omega";
  import type { FileEntry } from "../../composables/omega";
  import { useNarrow } from "~/composables/narrow";
  import FileTree from "./FileTree.vue";

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

  loadRoot();
</script>

<style scoped>
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
