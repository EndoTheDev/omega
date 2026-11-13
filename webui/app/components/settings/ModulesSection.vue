<template>
  <!-- card t_81d0901b: the module lifecycle surface is CONFIGURATION, not
       content - it lives in the settings tab's MODULES section now (the
       old secondary-pane tab is gone). This component is that section's
       body: the rows, their live toggles, the expander detail and the
       refusal tail. It carries NO settings-native row semantics (no
       modified dot, no per-row reset, no scaffold-set override flag):
       a module row is a plain toggle + description over persisted config
       writes, which is why the row law below is the shared registry row
       (.reg-row), not the settings knob row (.setting-row). -->
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
            <!-- card t_561dc4e0: a module with UI opens through its name -
                 the RegistryList openable-name precedent (t_36ceb99b); the
                 right control seat stays one wide for the toggle/state,
                 per the one-control-right law. -->
            <button
              v-if="row.hasUi"
              class="extension-name extension-open-name reg-row-name"
              :title="`open ${row.name} panel`"
              @click="$emit('open-module', row.name)">{{ row.name }}</button>
            <span
              v-else
              class="extension-name reg-row-name"
              :title="row.description">{{ row.name }}</span>
            <!-- the row law's subtitle line: the description (ellipsis),
                 the tool/command counts and the no-UI fact riding the same
                 dim line - the collapsed row reads two lines, the detail
                 stays in the expander. -->
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
          <!-- the right control seat: the shared TogglePill
               (card t_d93864e7) for toggleable rows - one switch, top-
               aligned with the name, no "disabled" text beside it (the
               pill IS the state, the row dims). ONE click applies
               (card t_5051d523: the arm step is dead; the consequence text
               rides the pill's title).

               card t_81d0901b: the load-bearing pair (agent-loop +
               provider) renders a VISIBLE always-on CHIP, never a disabled
               control - the seam refuses that toggle
               (api/server_modules.go: "module <name> is always on"), so
               this row is only the mirror. Any row the registry reports
               non-toggleable takes the same chip; the shell ring keeps its
               own tier-law badge. -->
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
        <!-- the expanded detail: the row's own facts, verbatim - the tools
             it provides (registry-sorted names), the slash commands it
             registered (registration order), and the panels it ships per
             the composition manifest. A disabled module has nothing
             mounted: the sections say so. -->
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
    <!-- Q12: rejections visible as events - the refusal tail rides every
         response, so a rejected toggle shows its ledger entry right under
         the list it was refused from. -->
    <p
      v-for="r in refusals"
      :key="r.at + r.module"
      class="module-refusal">
      {{ r.module }}: {{ r.err }}
    </p>
  </div>
</template>

<script setup lang="ts">
  // the settings tab's MODULES section (card t_81d0901b, which SUPERSEDES
  // the t_210a0d19 placement - module toggles are configuration, not
  // content, so the surface lives here instead of the secondary-pane tab
  // that card introduced). The listing still rides GET /extensions (the
  // composition-contract ruling in ../shell/extensionsRows.ts: the shell
  // owns the listing because it aggregates every module) and the live
  // state + toggles still ride GET/PUT /modules - the WIRE endpoints are
  // untouched by the move; only the surface changed.
  //
  // The row model merges the registry introspection (live state,
  // toggleable, dependents) into the inventory; expansion reveals each
  // module's tools, commands and panels, and every toggleable row gets the
  // shared one-click toggle (card t_5051d523 killed the old two-step
  // arm/confirm). /help parity: the webui has no slash-command bar, so
  // this section plus the /modules palette command IS the /extensions
  // surface.
  import { computed, ref, watch } from "vue";
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
  } from "../shell/extensionsRows";
  import { modulePanels } from "../../composables/module-panels.generated";
  // card t_502b8e81: the iconset wrapper - the row expander chevron
  import Icon from "../ui/Icon.vue";
  import TogglePill from "../ui/TogglePill.vue";

  const props = defineProps<{
    /** the settings pane's width-driven adaptation: the section follows its
        host's measurement (no second resize observer here) */
    narrow?: boolean;
  }>();

  // open-module(moduleId): open that module's dock tab - the settings host
  // owns the dock call. count: the row total, so the section's nav entry
  // and heading print it without a second fetch.
  const emit = defineEmits<{
    (e: "open-module", moduleId: string): void;
    (e: "count", count: number): void;
  }>();

  const extensions = ref<OmegaExtension[]>([]);
  const modules = ref<ModuleRow[]>([]);
  const refusals = ref<ModuleRefusal[]>([]);
  const loading = ref(false);
  const error = ref("");
  const toggleError = ref<string | null>(null);
  const busy = ref<string | null>(null);
  // false when the host serves no toggle seam (501) - the rows render
  // from the inventory alone, no switch, the state note explains.
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
  watch(
    rows,
    (list) => emit("count", list.length),
    { immediate: true },
  );

  // --- expansion: ephemeral list state (no persistence law - the section
  // reopens fresh with the pane) ---
  const expanded = ref<Set<string>>(new Set());
  const isExpanded = (name: string) => expanded.value.has(name);
  function toggleExpand(name: string) {
    const next = new Set(expanded.value);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    expanded.value = next;
  }

  // --- the one-click toggle (card t_5051d523) ------------------------
  // One activation commits; busy names the in-flight row so a second
  // click cannot overlap the write.

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

  // rowHint (card t_5051d523): the consequence line ("this also
  // disables: cron") used to appear only after the first click, as the
  // arm gate. It now rides the pill's title, where it informs without
  // ever blocking the switch.
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
    // resting state: name the dependents while the module runs (the Q3
    // facts stay visible, not only on hover)
    if (row.toggleable && row.dependents.length > 0) {
      return `dependents: ${row.dependents.join(", ")}`;
    }
    return "";
  };

  function apply(payload: { modules: ModuleRow[]; refusals: ModuleRefusal[] }) {
    modules.value = payload.modules;
    refusals.value = payload.refusals;
  }

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
      apply(await toggleModule(row.name, checked));
    } catch (err) {
      toggleError.value = err instanceof Error ? err.message : String(err);
      // Refresh anyway: the refusal ledger is the visible event (Q12)
      // even when the response body carried no fresh list.
      try {
        apply(await fetchModules());
      } catch {
        // the list stays; the error line stands
      }
    } finally {
      busy.value = null;
    }
  }

  async function load() {
    // the inventory first (the section's spine), then the introspection;
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
  /* the section body. The row geometry (paddings, separators, name and
     subtitle typography, the right-control seat, the note line) is the
     shared assets/css/registry-row.css (card t_561dc4e0) - this block
     keeps the section's own voice only (the summary line, the mono meta,
     the dim states, the expanded detail). */
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
  /* the name line: the row law's typography rides .reg-row-name; this
     section keeps its mono voice, and the openable name (t_561dc4e0) is a
     button styled back into the row - hover raises it */
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
  /* card t_81d0901b: the always-on CHIP - the visible mirror of the seam's
     refusal (agent-loop, provider, and every row the registry reports as
     non-toggleable). A chip, never a disabled switch. */
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
  /* the subtitle specifics: the counts speak the section's mono meta voice;
     the dim voice is the row law's .reg-row-sub */
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

  /* narrow: the section follows the settings pane's own measurement; the
     description still ellipses within the subtitle line. */
  .settings-modules.narrow .extensions-summary {
    overflow-wrap: anywhere;
  }
  .settings-modules.narrow .extension-desc {
    overflow-wrap: anywhere;
  }
</style>
