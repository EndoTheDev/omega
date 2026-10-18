<template>
  <aside
    class="aside pane"
    :class="{ open: open }">
    <div class="inspector-tabs">
      <button
        v-for="tab in tabs"
        :key="tab"
        class="inspector-tab"
        :class="{ active: tab === 'Files' }">
        {{ tab }}
      </button>
    </div>

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
  import { ref, watch } from "vue";
  import { fetchFilesBrowse, fetchFilesRead, sortFileEntries } from "../composables/omega";
  import type { FileEntry } from "../composables/omega";

  // Inspector pane: a tab shell with the Files tab live. Parked tabs
  // (git view, session details, SSE wire log) are future siblings in the
  // tabs array - the shell needs no rework when they land.
  const tabs = ["Files"];

  // `open` is the mobile overlay state; `visible` is the desktop
  // slot-occupancy (false while settings holds the slot or the group is
  // closed). Loading is lazy: the pane is always mounted, so the first
  // visible flag is the one moment the root listing gets fetched. Below
  // the root, FileTree owns the tree: the expanded directory set IS the
  // location, persisted under omega.filetree-expanded.
  const props = defineProps<{
    open?: boolean;
    visible?: boolean;
  }>();

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

  let rootLoaded = false;

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

  watch(
    () => props.visible,
    (visible) => {
      if (visible && !rootLoaded) {
        rootLoaded = true;
        loadRoot();
      }
    },
    { immediate: true },
  );
</script>