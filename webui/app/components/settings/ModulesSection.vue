<template>
  <!-- the module lifecycle surface is CONFIGURATION, not content. -->
  <div class="settings-modules" :class="{ narrow: narrow }">
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
        :key="row.name"
        class="extension-row reg-row"
        :class="{ 'module-off': row.state === 'disabled' }">
        <div class="extension-head reg-row-head">
          <button
            class="extension-expand reg-row-lead"
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
          <div class="reg-row-main">
            <!-- a module with UI opens through its name. -->
            <button
              v-if="row.hasUi"
              class="extension-name extension-open-name reg-row-name"
              :title="`open ${row.name} panel`"
              @click="$emit('open-module', row.name)">{{ row.name }}</button>
            <span
              v-else
              class="extension-name reg-row-name"
              :title="row.description">{{ row.name }}</span>
            <span class="reg-row-sub">
              <span
                v-if="row.description"
                class="reg-row-sub-text extension-desc"
                :title="row.description">{{ row.description }}</span>
              <span class="extension-meta">{{ row.tools }} tool{{ row.tools === 1 ? "" : "s" }} - {{ row.commands }} command{{ row.commands === 1 ? "" : "s" }}</span>
              <span
                v-if="!row.hasUi"
                class="extension-noui">no UI</span>
            </span>
          </div>
          <!--
            the right control seat: the shared TogglePill for toggleable rows.
            ONE click applies.: the load-bearing pair (agent-loop + provider) renders a
            VISIBLE always-on CHIP, never a disabled control - the seam refuses that
            toggle (api/server_modules.go: "module <name> is always on"), so this row
            is only the mirror.
          -->
          <TogglePill
            v-if="seamAvailable && row.toggleable"
            class="module-toggle reg-row-ctrl"
            :state="row.state === 'mounted'"
            :disabled="busy !== null"
            :label="`${row.state === 'mounted' ? 'disable' : 'enable'} ${row.name}`"
            :hint="rowHint(row)"
            @click="onToggle(row, row.state !== 'mounted')" />
          <span
            v-else-if="row.state === 'shell'"
            class="extension-state extension-shell reg-row-ctrl">shell</span>
          <span
            v-else
            class="module-alwayson reg-row-ctrl"
            :title="`${row.name} is always on - the toggle endpoint refuses it`">always on</span>
        </div>
        <p
          v-if="rowNote(row)"
          class="setting-help module-note reg-row-note">{{ rowNote(row) }}</p>
        <!--
          the expanded detail: the row's own facts, verbatim - the tools it provides
          (registry-sorted names), the slash commands it registered (registration
          order), and the panels it ships per the composition manifest.
        -->
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
      no modules loaded
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
    <p
      v-for="r in moduleRefusals"
      :key="r.at + r.module"
      class="module-refusal">
      {{ r.module }}: {{ r.err }}
    </p>
  </div>
</template>

<script setup lang="ts">
  // the settings tab's MODULES section.
  import { computed, ref, watch } from "vue";
  import {
    fetchExtensions,
    toggleNote,
    type ModuleRow,
    type OmegaExtension,
  } from "../../composables/omega";
  // the live module state is the SHARED store now - the dock's runtime gate reads the
  // same copy, so a row's flip and the strip's chip can never disagree (one GET at boot
  // + the toggle responses; this section neither owns nor re-fetches the state).
  import {
    commitModuleToggle,
    loadModules,
    moduleRefusals,
    moduleRows,
    modulesKnown,
  } from "../../composables/moduleState";
  import {
    extensionsRows,
    extensionsSummary,
    type PanelRef,
  } from "../shell/extensionsRows";
  import { modulePanels } from "../../composables/module-panels.generated";
  import Icon from "../ui/Icon.vue";
  import TogglePill from "../ui/TogglePill.vue";

  const props = defineProps<{
    narrow?: boolean;
  }>();

  // open-module(moduleId): open that module's dock tab.
  const emit = defineEmits<{
    (e: "open-module", moduleId: string): void;
    (e: "count", count: number): void;
  }>();

  const extensions = ref<OmegaExtension[]>([]);
  const loading = ref(false);
  const error = ref("");
  const toggleError = ref<string | null>(null);
  const busy = ref<string | null>(null);
  const seamAvailable = ref(true);
  const stateNote = ref("");

  const panelRefs = computed<PanelRef[]>(() =>
    modulePanels.map((p) => ({
      module: p.module,
      title: p.dockable?.title ?? p.module,
    })),
  );

  const rows = computed(() =>
    extensionsRows(extensions.value, panelRefs.value, moduleRows.value),
  );
  const summary = computed(() => extensionsSummary(rows.value));
  watch(
    rows,
    (list) => emit("count", list.length),
    { immediate: true },
  );

  const expanded = ref<Set<string>>(new Set());
  const isExpanded = (name: string) => expanded.value.has(name);
  function toggleExpand(name: string) {
    const next = new Set(expanded.value);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    expanded.value = next;
  }

  // the one-click toggle ------------------------ One activation commits; busy names
  // the in-flight row so a second click cannot overlap the write.

  // toggleNote speaks the registry introspection's dialect (ModuleRow).
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

  // rowHint: the consequence line ("this also disables. It now rides the pill's title,
  // where it informs without ever blocking the switch.
  const rowHint = (row: {
    name: string;
    state: string;
    toggleable: boolean;
    dependents: string[];
  }): string => toggleNote(asModuleRow(row), row.state !== "mounted");

  const rowNote = (row: {
    name: string;
    state: string;
    toggleable: boolean;
    dependents: string[];
  }): string => {
    if (row.toggleable && row.dependents.length > 0) {
      return `dependents: ${row.dependents.join(", ")}`;
    }
    return "";
  };

  type ToggleRow = {
    name: string;
    state: string;
    toggleable: boolean;
    dependents: string[];
  };

  async function onToggle(row: ToggleRow, checked: boolean) {
    if (busy.value !== null) return;
    busy.value = row.name;
    toggleError.value = null;
    try {
      await commitModuleToggle(row.name, checked);
    } catch (err) {
      toggleError.value = err instanceof Error ? err.message : String(err);
      try {
        await loadModules();
      } catch {
      }
    } finally {
      busy.value = null;
    }
  }

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
    try {
      await loadModules();
      seamAvailable.value = true;
      stateNote.value = "";
    } catch (e) {
      seamAvailable.value = modulesKnown.value;
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
  .settings-modules {
    padding: 2px 0 4px;
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
    border-radius: var(--r-chip);
  }
  .extension-row:hover { background: var(--z1-raised); }
  .extension-row.module-off .extension-name { color: var(--text-dim); }
  .extension-head {
    min-width: 0;
  }
  .extension-expand {
    border: none;
    background: none;
    color: var(--text-dim);
    font: inherit;
    cursor: pointer;
    padding: 0;
  }
  .extension-expand:hover { color: var(--text); }
  .extension-chevron { transition: transform 0.15s ease; }
  .extension-chevron.open { transform: rotate(90deg); }
  /* the name line: the row law's typography rides.reg-row-name. */
  .extension-name {
    font-family: var(--mono);
  }
  .extension-open-name {
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    padding: 0;
    text-align: left;
  }
  .extension-open-name:hover { text-decoration: underline; }
  .extension-state {
    font-size: 10.5px;
  }
  .extension-shell { font-family: var(--mono); }
  /*
   * the always-on CHIP - the visible mirror of the seam's refusal.
   * A chip, never a disabled switch.
   */
  .module-alwayson {
    flex: none;
    padding: 1px 6px;
    border: 1px solid var(--text-dim);
    border-radius: var(--r-chip);
    font-size: 10px;
    font-family: var(--mono);
    color: var(--text-dim);
    white-space: nowrap;
  }
  .extension-meta {
    font-family: var(--mono);
    font-size: 10.5px;
  }
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

  .settings-modules.narrow .extensions-summary {
    overflow-wrap: anywhere;
  }
  .settings-modules.narrow .extension-desc {
    overflow-wrap: anywhere;
  }
</style>
