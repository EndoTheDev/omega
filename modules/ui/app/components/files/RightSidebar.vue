<template>
  <aside
    class="aside pane"
    :class="{ open: open }">
    <div class="inspector-tabs">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        class="inspector-tab"
        :class="{ active: tab.id === activeTab }"
        @click="activeTab = tab.id">
        {{ tab.title }}
      </button>
    </div>

    <div
      v-show="activeTab === 'files'"
      class="inspector-files">
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
      v-if="activeTab === 'files' && preview"
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
    <!-- module-panels: the composition mount (see composables/
         module-panels.generated.ts). The inspector tab card
         (t_db430fea) gave the panels real tabs: exactly the ACTIVE
         panel renders, self-titled, under its own tab - read-only,
         lazy (fetch/poll only while the tab is open). -->
    <div
      v-if="activePanel"
      class="module-panels">
      <component
        :is="activePanel.component"
        :key="activePanel.module" />
    </div>
  </aside>
</template>

<script setup lang="ts">
  import { computed, ref, watch } from "vue";
  import { fetchFilesBrowse, fetchFilesRead, sortFileEntries, mainViewModules } from "../../composables/omega";
  import type { FileEntry } from "../../composables/omega";
  // The inspector sees module PANELS only: modules that ship a main-pane
  // view (kanban's board) are not inspector content (see mainViewModules).
  import { modulePanels } from "../../composables/module-panels.generated";
  import { inspectorPanelTabs } from "../../composables/compose";

  // Inspector pane: a tab shell (card t_db430fea). The Files tab is the
  // shell's own content; every mounted module panel is a tab of its
  // own, labeled by its ui/dockable.json title (alphabetical, the
  // generated list's order). Only the ACTIVE panel renders - a panel
  // fetches (or polls, for logs) only while its tab is up. Future
  // siblings need no shell rework: ship modules/<name>/ui/ + a
  // dockable.json title and the tab exists.
  const tabs = [
    { id: "files", title: "Files" },
    ...inspectorPanelTabs(modulePanels, mainViewModules).map((t) => ({
      id: t.module,
      title: t.title,
    })),
  ];
  const activeTab = ref(tabs[0].id);
  const activePanel = computed(() =>
    modulePanels.find((p) => p.module === activeTab.value),
  );

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