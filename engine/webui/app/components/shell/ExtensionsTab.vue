<template>
  <aside
    ref="rootEl"
    class="aside pane inspector-panel"
    :class="{ open: open, narrow: narrow }">
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
  import { computed, ref } from "vue";
  import { fetchExtensions } from "../../composables/omega";
  import type { OmegaExtension } from "../../composables/omega";
  import { extensionsRows, extensionsSummary } from "./extensionsRows";
  import { modulePanels } from "../../composables/module-panels.generated";
  import { useNarrow } from "~/composables/narrow";

  // `open` is the mobile drawer overlay state (desktop tabs never pass
  // it up - the pane is always shown); matches FilesTab / SettingsTab.
  defineProps<{
    open?: boolean;
  }>();

  // open-module(moduleId): switch the inspector to that module's dock
  // tab (app.vue's outlet handler owns the dock call).
  defineEmits<{ (e: "open-module", moduleId: string): true }>();

  // the narrow adaptation - the module-UI recipe (card t_a2fdc630):
  // the root measures its own rendered container (this tab rides both
  // the inspector column and drawer mounts) and flips the class; the
  // scoped stylesheet below reflows the rows.
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  const extensions = ref<OmegaExtension[]>([]);
  const loading = ref(false);
  const error = ref("");

  const rows = computed(() =>
    extensionsRows(extensions.value, modulePanels.map((p) => p.module)),
  );
  const summary = computed(() => extensionsSummary(rows.value));

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

<style scoped>
  /* the inventory voice: same scrolling body as .inspector-files
     (main.css owns .aside / the shared shell chrome), this block is the
     tab's own rows. Rows are entry list items in the session-list voice:
     raised chip hover, mono meta on the edge. */
  .inspector-extensions {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 6px 8px 8px;
  }
  .extensions-summary {
    margin: 0 0 6px;
    padding: 0 2px;
    font-size: 11px;
    color: var(--text-dim);
    font-family: var(--mono);
  }
  .extensions-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .extension-row {
    padding: 6px 8px;
    border-radius: var(--r-chip);
    font-size: 12px;
  }
  .extension-row:hover { background: var(--z1-raised); }
  .extension-head {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }
  .extension-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--mono);
  }
  .extension-ui { margin-left: auto; flex: none; }
  .extension-open {
    border: none;
    background: none;
    color: var(--accent);
    font: inherit;
    font-size: 11px;
    cursor: pointer;
    padding: 0;
  }
  .extension-open:hover { text-decoration: underline; }
  .extension-noui {
    margin-left: auto;
    flex: none;
    font-size: 10.5px;
    color: var(--text-dim);
  }
  .extension-desc {
    margin: 2px 0 0;
    color: var(--text-dim);
    font-size: 11.5px;
    overflow-wrap: anywhere;
  }
  .extension-meta {
    margin: 2px 0 0;
    font-family: var(--mono);
    font-size: 10.5px;
    color: var(--text-dim);
  }
  .extensions-status {
    margin: 0;
    padding: 4px 2px;
    font-size: 12px;
    color: var(--text-dim);
  }
  .extensions-error { color: var(--err); }

  /* narrow: the module-UI recipe (card t_a2fdc630) - the name and its
     action share the head row tightly; the description still wraps. */
  .inspector-panel.narrow .extensions-summary {
    overflow-wrap: anywhere;
  }
  .inspector-panel.narrow .extension-open {
    font-size: 10.5px;
  }
</style>