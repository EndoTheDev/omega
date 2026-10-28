<template>
  <aside
    ref="rootEl"
    class="aside pane inspector-panel"
    :class="{ open: open, narrow: narrow }">
    <div class="inspector-extensions">
      <p class="extensions-summary">{{ summary }}</p>
      <p
        v-if="stateNote"
        class="extensions-status extensions-state-note">
        {{ stateNote }}
      </p>
      <ul
        v-if="rows.length"
        class="extensions-list">
        <li
          v-for="row in rows"
          :key="rowKey(row)"
          class="extension-row"
          :class="{ 'module-off': row.state === 'disabled' }">
          <div class="extension-head">
            <button
              class="extension-expand"
              type="button"
              :aria-expanded="isExpanded(row.name)"
              :title="isExpanded(row.name) ? 'Collapse' : 'Expand'"
              @click="toggleExpand(row.name)">
              <Icon
                class="extension-chevron"
                :class="{ open: isExpanded(row.name) }"
                name="chevron-down"
                :size="12" />
            </button>
            <span class="extension-name">{{ row.name }}</span>
            <!-- the lifecycle cockpit (card t_210a0d19): the toggle rides
                 the row when the registry introspection knows it and the
                 module is toggleable. Engine ring rows carry the tier-law
                 badge instead (never in /modules); machine-critical rows
                 keep the always-on annotation - the same law the old
                 settings group spoke. -->
            <span class="extension-side">
              <label
                v-if="seamAvailable && row.toggleable"
                class="module-toggle">
                <input
                  type="checkbox"
                  :disabled="busy !== null"
                  :checked="row.state === 'mounted'"
                  @change="onToggle(row, ($event.target as HTMLInputElement).checked)" />
              </label>
              <span
                v-else-if="row.state === 'engine'"
                class="extension-state extension-engine">engine</span>
              <span
                v-else-if="!row.toggleable"
                class="extension-state module-locked"
                :title="row.description">always on</span>
              <span
                v-if="row.state === 'disabled'"
                class="extension-state module-off-note">disabled</span>
              <button
                v-if="row.hasUi"
                class="extension-open"
                title="Open this module's panel"
                @click="$emit('open-module', row.name)">open</button>
              <span
                v-else
                class="extension-noui">no UI</span>
            </span>
          </div>
          <p
            v-if="row.description"
            class="extension-desc">
            {{ row.description }}
          </p>
          <p class="extension-meta">
            {{ row.tools }} tool{{ row.tools === 1 ? "" : "s" }} - {{ row.commands }} command{{ row.commands === 1 ? "" : "s" }}
          </p>
          <p
            v-if="rowNote(row)"
            class="setting-help module-note">{{ rowNote(row) }}</p>
          <!-- the expanded detail: the row's own facts, verbatim - the
               tools it provides (registry-sorted names), the slash
               commands it registered (registration order), and the
               panels it ships per the composition manifest. A disabled
               module has nothing mounted: the sections say so. -->
          <div
            v-if="isExpanded(row.name)"
            class="extension-detail">
            <p class="detail-group">
              <span class="detail-label">tools</span>
              <span
                v-if="row.toolNames.length"
                class="detail-items">{{ row.toolNames.join(", ") }}</span>
              <span
                v-else
                class="detail-none">{{ row.state === "disabled" ? "none (disabled)" : "none" }}</span>
            </p>
            <p class="detail-group">
              <span class="detail-label">commands</span>
              <span
                v-if="row.commandNames.length"
                class="detail-items">{{ row.commandNames.join(", ") }}</span>
              <span
                v-else
                class="detail-none">{{ row.state === "disabled" ? "none (disabled)" : "none" }}</span>
            </p>
            <p class="detail-group">
              <span class="detail-label">panels</span>
              <span
                v-if="row.panels.length"
                class="detail-items">{{ row.panels.join(", ") }}</span>
              <span
                v-else
                class="detail-none">none</span>
            </p>
          </div>
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
      <p
        v-if="toggleError"
        class="extensions-status extensions-error">
        {{ toggleError }}
      </p>
      <!-- Q12: rejections visible as events - the refusal tail rides
           every response, so a rejected toggle shows its ledger entry
           right under the list it was refused from. -->
      <p
        v-for="r in refusals"
        :key="r.at + r.module"
        class="module-refusal">
        {{ r.module }}: {{ r.err }}
      </p>
    </div>
  </aside>
</template>

<script setup lang="ts">
  // the inspector's EXTENSIONS tab (card t_14f73742): the webui parity of
  // the TUI /extensions command. A SHELL tab over GET /extensions (the
  // composition-contract ruling lives in extensionsRows.ts): the shell
  // owns the listing because it aggregates every module.
  // Card t_210a0d19 makes it the module lifecycle cockpit: the row model
  // merges the registry introspection (GET /modules - live state,
  // toggleable, dependents) into the inventory, every toggleable row
  // gets the two-step toggle (arm, then apply - the same law the old
  // settings group obeyed), expansion reveals the module's tools,
  // commands and panels, and the MODULES settings group is gone - this
  // tab is the only toggle surface; the settings pane keeps config
  // knobs only. /help parity: the webui has no slash-command bar, so
  // this tab IS the /extensions surface.
  // Lazy like the other inspector tabs: the load runs on mount, the dock
  // only activates a tab's content when the tab IS active.
  import { computed, ref } from "vue";
  import {
    fetchExtensions,
    fetchModules,
    toggleModule,
    toggleNote,
    type ModuleRefusal,
    type ModuleRow,
    type OmegaExtension,
  } from "../../composables/omega";
  import {
    extensionsRows,
    extensionsSummary,
    type PanelRef,
  } from "./extensionsRows";
  import { modulePanels } from "../../composables/module-panels.generated";
  // card t_502b8e81: the iconset wrapper - the row expander chevron
  import Icon from "../ui/Icon.vue";
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
  const modules = ref<ModuleRow[]>([]);
  const refusals = ref<ModuleRefusal[]>([]);
  const loading = ref(false);
  const error = ref("");
  const toggleError = ref<string | null>(null);
  const busy = ref<string | null>(null);
  // false when the host serves no toggle seam (501) - the rows render
  // from the inventory alone, no checkbox, the state note explains.
  const seamAvailable = ref(true);
  const stateNote = ref("");

  // the shell's build-time composition facts, reshaped for the row
  // model: (module, dockable title) pairs from the panel manifest - a
  // manifest entry without a dockable declaration titles by module name.
  const panelRefs = computed<PanelRef[]>(() =>
    modulePanels.map((p) => ({
      module: p.module,
      title: p.dockable?.title ?? p.module,
    })),
  );

  const rows = computed(() =>
    extensionsRows(extensions.value, panelRefs.value, modules.value),
  );
  const summary = computed(() => extensionsSummary(rows.value));

  // --- expansion: ephemeral list state (no persistence law - the
  // inspector reopens fresh, unlike the settings collapse storage) ---
  const expanded = ref<Set<string>>(new Set());
  const isExpanded = (name: string) => expanded.value.has(name);
  function toggleExpand(name: string) {
    const next = new Set(expanded.value);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    expanded.value = next;
  }

  // --- the two-step toggle (ported from the settings group, card
  // t_210a0d19): the first click arms the row - the note names what
  // goes down with it (the dependents law, spoken by toggleNote) or
  // asks plain; the second click applies. A cancel bumps the row key
  // so the checkbox re-renders from server state. ---
  const armed = ref<string | null>(null);
  const revisions = ref<Record<string, number>>({});
  const rowKey = (row: { name: string }) =>
    `${row.name}-${revisions.value[row.name] ?? 0}`;
  const armedId = (name: string, enabled: boolean) =>
    `${name}:${String(enabled)}`;

  // toggleNote speaks the registry introspection's dialect (ModuleRow):
  // shrink the merged row to just the fields the note reads.
  const asModuleRow = (row: {
    name: string;
    toggleable: boolean;
    dependents: string[];
  }): ModuleRow => ({
    name: row.name,
    state: "mounted",
    toggleable: row.toggleable,
    dependents: row.dependents,
  });

  const rowNote = (row: {
    name: string;
    state: string;
    toggleable: boolean;
    dependents: string[];
  }): string => {
    if (armed.value === armedId(row.name, false)) {
      return toggleNote(asModuleRow(row), false) || "confirm: apply disable?";
    }
    if (armed.value === armedId(row.name, true)) {
      return toggleNote(asModuleRow(row), true) || "confirm: apply enable?";
    }
    // resting state: name the dependents while the module runs (the Q3
    // facts stay visible in the cockpit, not only at arm time)
    if (row.toggleable && row.dependents.length > 0) {
      return `dependents: ${row.dependents.join(", ")}`;
    }
    return "";
  };

  function apply(payload: { modules: ModuleRow[]; refusals: ModuleRefusal[] }) {
    modules.value = payload.modules;
    refusals.value = payload.refusals;
  }

  function bump(name: string) {
    revisions.value = {
      ...revisions.value,
      [name]: (revisions.value[name] ?? 0) + 1,
    };
  }

  type ToggleRow = {
    name: string;
    state: string;
    toggleable: boolean;
    dependents: string[];
  };

  async function onToggle(row: ToggleRow, checked: boolean) {
    if (busy.value !== null) return;
    const id = armedId(row.name, checked);
    if (armed.value !== id) {
      // arm: the first click never applies - the confirm note shows in
      // the row; the checkbox re-renders back to server state.
      armed.value = id;
      bump(row.name);
      return;
    }
    armed.value = null;
    busy.value = row.name;
    toggleError.value = null;
    try {
      apply(await toggleModule(row.name, checked));
      bump(row.name);
    } catch (err) {
      toggleError.value = err instanceof Error ? err.message : String(err);
      // Refresh anyway: the refusal ledger is the visible event (Q12)
      // even when the response body carried no fresh list.
      try {
        apply(await fetchModules());
      } catch {
        // the list stays; the error line stands
      }
      bump(row.name);
    } finally {
      busy.value = null;
    }
  }

  async function load() {
    // the inventory first (the tab's spine), then the introspection;
    // a toggle seam this host lacks degrades to the bare inventory.
    loading.value = true;
    error.value = "";
    try {
      extensions.value = await fetchExtensions();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
    try {
      apply(await fetchModules());
      seamAvailable.value = true;
      stateNote.value = "";
    } catch (e) {
      seamAvailable.value = false;
      stateNote.value = e instanceof Error && e.message.includes("501")
        ? "module state unavailable - no toggle seam"
        : `module state unavailable: ${
            e instanceof Error ? e.message : String(e)
          }`;
      if (!(e instanceof Error && e.message.includes("501"))) {
        console.error("modules load failed:", e);
      }
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
  .extensions-state-note {
    padding: 0 2px;
    color: var(--text-dim);
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
  .extension-row.module-off .extension-name { color: var(--text-dim); }
  .extension-head {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }
  /* the right-side cluster: toggle, badges, panel action - pushed as
     ONE group (a single auto margin keeps them together at the edge) */
  .extension-side {
    margin-left: auto;
    flex: none;
    display: flex;
    align-items: baseline;
    gap: 5px;
  }
  .extension-expand {
    flex: none;
    border: none;
    background: none;
    color: var(--text-dim);
    font: inherit;
    cursor: pointer;
    padding: 0;
    display: flex;
    align-items: baseline;
  }
  .extension-expand:hover { color: var(--text); }
  .extension-chevron { transition: transform 0.15s ease; }
  .extension-chevron.open { transform: rotate(90deg); }
  .extension-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--mono);
  }
  .extension-state {
    flex: none;
    font-size: 10.5px;
    color: var(--text-dim);
  }
  .extension-engine { font-family: var(--mono); }
  .module-toggle { flex: none; display: flex; align-items: baseline; }
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
  .module-note { margin: 2px 0 0; }
  .extension-detail {
    margin: 6px 0 0;
    padding: 2px 0 2px 10px;
    border-left: 1px solid var(--text-dim);
  }
  .detail-group {
    margin: 0 0 2px;
    display: flex;
    gap: 6px;
    align-items: baseline;
    min-width: 0;
  }
  .detail-label {
    flex: none;
    font-family: var(--mono);
    font-size: 10px;
    color: var(--text-dim);
    min-width: 48px;
  }
  .detail-items {
    min-width: 0;
    font-family: var(--mono);
    font-size: 10.5px;
    overflow-wrap: anywhere;
  }
  .detail-none {
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
  .inspector-panel.narrow .extension-desc {
    overflow-wrap: anywhere;
  }
</style>