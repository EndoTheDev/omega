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
      <nav class="files-crumbs">
        <template
          v-for="(crumb, i) in crumbs"
          :key="crumb.path">
          <span
            v-if="i"
            class="crumb-sep">/</span>
          <button
            :disabled="i === crumbs.length - 1"
            @click="openDir(crumb.path)">
            {{ crumb.name }}
          </button>
        </template>
      </nav>
      <ul class="files-list">
        <li
          v-if="errorMsg"
          class="files-status">
          {{ errorMsg }}
        </li>
        <li
          v-else-if="loading"
          class="files-status">
          loading
        </li>
        <li
          v-else-if="!entries.length"
          class="files-status">
          empty directory
        </li>
        <li
          v-for="entry in entries"
          :key="entry.name"
          class="file-row"
          @click="openEntry(entry)">
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
        </li>
      </ul>
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
  import { computed, ref, watch } from "vue";
  import {
    fileCrumbs,
    fetchFilesBrowse,
    fetchFilesRead,
    joinRelPath,
    sortFileEntries,
  } from "../composables/omega";
  import type { FileEntry } from "../composables/omega";

  // Inspector pane: a tab shell with the Files tab live. Parked tabs
  // (git view, session details, SSE wire log) are future siblings in the
  // tabs array - the shell needs no rework when they land.
  const tabs = ["Files"];

  // `open` is the mobile overlay state; `visible` is the desktop
  // slot-occupancy (false while settings holds the slot or the group is
  // closed). Loading is lazy: the pane is always mounted, so the first
  // visible flag is the one moment the root listing gets fetched.
  const props = defineProps<{
    open?: boolean;
    visible?: boolean;
  }>();

  const entries = ref<FileEntry[]>([]);
  const dirPath = ref("");
  const loading = ref(false);
  const errorMsg = ref("");

  interface Preview {
    path: string;
    content: string;
    truncated: boolean;
    binary: boolean;
    error?: string;
  }
  const preview = ref<Preview | null>(null);

  const crumbs = computed(() => fileCrumbs(dirPath.value));

  let rootLoaded = false;

  async function openDir(path: string) {
    loading.value = true;
    errorMsg.value = "";
    try {
      const data = await fetchFilesBrowse(path);
      entries.value = sortFileEntries(data.entries ?? []);
      dirPath.value = path;
    } catch (e) {
      errorMsg.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
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

  function openEntry(entry: FileEntry) {
    const path = joinRelPath(dirPath.value, entry.name);
    if (entry.is_dir) openDir(path);
    else openFile(path);
  }

  watch(
    () => props.visible,
    (visible) => {
      if (visible && !rootLoaded) {
        rootLoaded = true;
        openDir("");
      }
    },
    { immediate: true },
  );
</script>