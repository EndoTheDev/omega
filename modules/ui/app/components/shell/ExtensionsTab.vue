<template>
  <aside
    class="aside pane inspector-panel"
    :class="{ open: open }">
    <div class="inspector-extensions">
      <p class="extensions-summary">{{ summary }}</p>
      <ul
        v-if="rows.length"
        class="extensions-list">
        <li
          v-for="row in rows"
          :key="row.name"
          class="extension-row">
          <div class="extension-head">
            <span class="extension-name">{{ row.name }}</span>
            <span
              v-if="row.hasUi"
              class="extension-ui"
            ><button
              class="extension-open"
              title="Open this module's panel"
              @click="$emit('open-module', row.name)">open</button></span>
            <span
              v-else
              class="extension-noui">no UI</span>
          </div>
          <p
            v-if="row.description"
            class="extension-desc">
            {{ row.description }}
          </p>
          <p class="extension-meta">
            {{ row.tools }} tool{{ row.tools === 1 ? "" : "s" }} - {{ row.commands }} command{{ row.commands === 1 ? "" : "s" }}
          </p>
        </li>
      </ul>
      <p
        v-else-if="!loading"
        class="extensions-status">
        no extensions loaded
      </p>
      <p
        v-else
        class="extensions-status">
        loading
      </p>
      <p
        v-if="error"
        class="extensions-status extensions-error">
        {{ error }}
      </p>
    </div>
  </aside>
</template>

<script setup lang="ts">
  // the inspector's EXTENSIONS tab (card t_14f73742): the webui parity of
  // the TUI /extensions command. A SHELL tab over GET /extensions (the
  // composition-contract ruling lives in extensionsRows.ts): the shell
  // owns the listing because it aggregates every module. has-ui derives
  // from the build-time modulePanels manifest; a has-ui module row emits
  // open-module(moduleId) so the shell switches to that module's dock
  // tab - the module-panels machinery does the mounting, this tab only
  // routes. /help parity: the webui has no slash-command bar, so this
  // tab IS the /extensions surface.
  // Lazy like the other inspector tabs: the load runs on mount, the dock
  // only activates a tab's content when the tab IS active.
  import { ref } from "vue";
  import { fetchExtensions } from "../../composables/omega";
  import type { OmegaExtension } from "../../composables/omega";
  import { extensionsRows, extensionsSummary } from "./extensionsRows";
  import { modulePanels } from "../../composables/module-panels.generated";

  // `open` is the mobile drawer overlay state (desktop tabs never pass
  // it up - the pane is always shown); matches FilesTab / SettingsTab.
  defineProps<{
    open?: boolean;
  }>();

  // open-module(moduleId): switch the inspector to that module's dock
  // tab (app.vue's outlet handler owns the dock call).
  defineEmits<{ (e: "open-module", moduleId: string): true }>();

  const extensions = ref<OmegaExtension[]>([]);
  const loading = ref(false);
  const error = ref("");

  const rows = () => extensionsRows(extensions.value, modulePanels.map((p) => p.module));
  const summary = () => extensionsSummary(rows());

  async function load() {
    loading.value = true;
    error.value = "";
    try {
      extensions.value = await fetchExtensions();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
  }

  load();
</script>