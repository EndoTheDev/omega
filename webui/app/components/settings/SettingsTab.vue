<template>
    <!--
      SUPERSEDES FOR THIS PANE: the settings tab carries NO tab band.
      Do not add it back.
    -->
    <aside
      ref="rootEl"
      class="settings aside pane inspector-panel"
      :class="{ open: open, narrow: narrow }"
      @keydown="onDrawerKey">

    <!--
      the ONE-DOCUMENT law, which SUPERSEDES rulings 1 and 12 of the grill.
      Selection NEVER filters the document - it always shows every knob and its
      description.
      Expansion follows SELECTION: a category discloses its subsection jump links only
      while it, or one of its subsections, is selected - exactly one branch open, no
      remembered toggle set.
      It used to be exempt (the curated all-view) and rendered the whole document while
      every other category narrowed, which read as the filter being broken exactly
      there.
    -->
    <div class="settings-filterbar">
      <SearchInput
        v-model="query"
        placeholder="filter settings" />
      <!--
        the modified-only switch left the deleted heading bar and sits beside the.
        Same contract as before: a pressed-state chip that narrows the whole document.
      -->
      <button
        class="settings-modified"
        :class="{ on: modifiedOnly }"
        type="button"
        :aria-pressed="modifiedOnly"
        title="show only knobs that differ from the default"
        @click="modifiedOnly = !modifiedOnly">
        modified only
      </button>
      <button
        v-if="narrow"
        ref="jumpEl"
        class="settings-jump"
        type="button"
        :aria-expanded="drawerOpen"
        aria-label="settings categories"
        @click="drawerOpen ? closeDrawer() : openDrawer()">
        <Icon class="settings-jump-icon" name="chevron-down" :size="12" />
        <span>categories</span>
      </button>
      <!--
        the three config-file actions (reset to default, export to yaml, edit as yaml)
        live in the BAND - the pane's first row - never in the scroll body.
      -->
      <div ref="configMenuEl" class="settings-config">
        <button
          class="settings-config-btn"
          type="button"
          :aria-expanded="configMenuOpen"
          aria-haspopup="menu"
          aria-label="config file actions"
          :title="configChipHint"
          :disabled="configBusy"
          @click="toggleConfigMenu()">
          <span>config</span>
          <Icon class="settings-config-caret" name="chevron-down" :size="10" />
        </button>
        <div
          v-if="configMenuOpen"
          class="settings-config-menu"
          @click.stop>
          <!--
            round 2: NAME the target, inside the menu and in the chip's tooltip.
            Without the line, a chip opened from a scope tab looked like it would
            change the visible tab and silently did not.
          -->
          <p class="settings-config-target">{{ configTargetLabel }}</p>
          <div
            class="settings-config-items"
            role="menu"
            aria-label="config file actions">
            <button
              class="settings-config-item"
              role="menuitem"
              type="button"
              :disabled="configBusy"
              :title="resetHint"
              @click="onResetToDefaults()">
              {{ resetArmed ? "confirm: reset all?" : "reset to default" }}
            </button>
            <button
              class="settings-config-item"
              role="menuitem"
              type="button"
              :disabled="configBusy"
              title="download the config as a yaml file"
              @click="onExportYaml()">
              export to yaml
            </button>
            <button
              class="settings-config-item"
              role="menuitem"
              type="button"
              :disabled="configBusy"
              title="edit the config file as yaml"
              @click="onEditYaml()">
              edit as yaml
            </button>
          </div>
        </div>
      </div>
    </div>

    <div class="settings-split">
      <!-- the scrim is DECORATION: it carries no name, no state and no focus. -->
      <div
        v-if="narrow && drawerOpen"
        class="settings-scrim"
        role="presentation"
        @click="closeDrawer()" />
      <nav
        v-if="!narrow || drawerOpen"
        ref="drawerEl"
        class="settings-nav"
        :class="{ drawer: narrow }"
        aria-label="settings categories">
        <!--
          the jump links are GROUPED by surface class - frontend above backend, each
          under its own header - and the groups are the ORDER (SURFACE_ORDER), so this
          list and the document below can never disagree about where a section sits.
        -->
        <template v-for="group in navGroups" :key="'surface:' + group.surface">
          <div v-if="group.header" class="settings-nav-surface">
            {{ group.label }}
          </div>
          <template v-for="cat in group.items" :key="cat.id">
          <div class="settings-nav-row">
            <button
              v-if="navChildren(cat).length"
              class="settings-nav-chev"
              type="button"
              :aria-expanded="expandedIds.has(cat.id)"
              :aria-label="`${expandedIds.has(cat.id) ? 'collapse' : 'expand'} ${cat.label}`"
              @click="selectCategory(cat.id)">
              <Icon class="settings-nav-chevron" name="chevron-down" :size="10" />
            </button>
            <span
              v-else
              class="settings-nav-chev-gap"
              aria-hidden="true" />
            <button
              class="settings-nav-cat"
              :class="{ active: selection === cat.id, dim: searching && !matchIds.has(cat.id) }"
              type="button"
              :data-category="cat.id"
              :aria-current="selection === cat.id ? 'true' : undefined"
              @click="selectCategory(cat.id)">
              <span class="settings-nav-label">{{ cat.label }}</span>
              <span class="settings-nav-count">{{ categoryCount(cat) }}</span>
            </button>
          </div>
          <button
            v-for="sub in openChildren(cat)"
            :key="cat.id + '/' + sub.id"
            class="settings-nav-sub"
            :class="{ active: selection === cat.id + '/' + sub.id, dim: searching && !matchIds.has(cat.id) }"
            type="button"
            :data-category="cat.id + '/' + sub.id"
            :aria-current="selection === cat.id + '/' + sub.id ? 'true' : undefined"
            @click="selectCategory(cat.id + '/' + sub.id)">
            <span class="settings-nav-label">{{ sub.label }}</span>
            <span class="settings-nav-count">{{ sub.knobs.length }}</span>
          </button>
          </template>
        </template>
      </nav>

      <section class="settings-content">
        <!--
          the head keeps ONLY the scope strip.
          It is a flex child, so a payload with no second scope must render no head AT
          ALL - an empty wrapper would still hold a seat.
        -->
        <div v-if="scopeTabs.length > 1" class="settings-content-head">
    <!--
      (settings tier 3b, R6/R8).
      R8 EMPTY WORLD: zero agent scopes in the payload renders NO strip at all, so the
      pane stays exactly the tier-2b shape; one scope or more materializes Main + one
      tab per agent name, and a tab switches the WHOLE pane (its groups, its search,
      the dot's base, the reset target).
    -->
    <div
      class="settings-scopes"
      role="tablist"
      aria-label="settings scope">
      <button
        v-for="tab in scopeTabs"
        :key="tab.name ?? 'main'"
        class="settings-scope-tab"
        :class="{ active: tab.name === activeScope }"
        type="button"
        role="tab"
        :aria-selected="tab.name === activeScope"
        @click="selectScope(tab.name)">
        {{ tab.label }}
      </button>
    </div>
        </div>

    <!--
      an agent scope whose layer file cannot be read/written: the endpoint refuses it
      too, so the pane renders the message and goes read-only (R4 - never a silent edit
      that cannot land)
    -->
    <p v-if="scopeMessage" class="settings-error">
      {{ scopeMessage }}
    </p>

    <p v-if="loadError" class="settings-error">
      {{ loadError }}
    </p>
    <div
      ref="bodyEl"
      class="settings-body"
      :class="{ 'settings-readonly': scopeReadOnly }"
      @scroll.passive="onBodyScroll">
      <p
      v-if="!loadError && totalCount === 0 && !modulesShown"
      class="settings-empty">
        {{ emptyText }}
      </p>

    <!--
      Two knob origins share one renderer: - the chat group is client state
      (props/emits, persisted under omega.theme-mode, panel prefs) - never sent to the
      server; - every other group is server config, edited in the draft map and saved
      through PUT /settings (diff-only).
    -->
    <!--
      the document paints its sections in SURFACE GROUPS.
      The band is a WAYFINDING device for the long list, so a SECTION FILTER drops it:
      the pane is one section the nav (still printed, still banded) has just named, and
      law - the selected section HEADS the document, at the top edge - must hold
      without a band above it.
    -->
    <template v-for="group in docGroups" :key="'surface:' + group.surface">
      <h2
        v-if="group.header && filterSection === null"
        class="settings-surface-title">{{ group.label }}</h2>
      <section
        v-for="section in group.items"
        :key="section.id"
        class="settings-category"
        :data-category="section.id"
        :data-section="section.id">
      <h2 class="settings-section-title">
        {{ section.label }}
        <span class="settings-knob-count">{{ section.count }}</span>
      </h2>
      <div
        v-for="block in section.blocks"
        :key="block.id"
        class="settings-block"
        :data-category="block.id"
        :data-section="block.id">
      <h3 v-if="block.heading" class="settings-subgroup-title">{{ block.label }}</h3>
        <template v-for="knob in block.knobs" :key="knob.key">
        <div v-if="knob.type === 'toggle'" class="setting-row setting-toggle">
          <label class="setting-toggle-main">
            <input
              type="checkbox"
              :checked="toggleValue(owner(knob), knob)"
              @change="
                setToggle(owner(knob), knob, ($event.target as HTMLInputElement).checked)
              " />
            <span>{{ knob.label }}</span>
          </label>
          <span class="setting-badges">
            <span
              v-if="isModified(owner(knob), knob)"
              class="setting-dot"
              role="img"
              :aria-label="dotLabel"
              :title="dotLabel" />
            <!--
              the draft star keeps its pre-existing seat (the non-toggle head below):
              toggle rows never carried it, and this card does not move it
            -->
            <button
              v-if="canReset(owner(knob), knob)"
              class="setting-reset"
              type="button"
              :title="resetLabel"
              @click.stop.prevent="resetKnob(owner(knob), knob)">
              reset
            </button>
          </span>
        </div>
        <div v-else class="setting-item">
          <div class="setting-head">
            <span>{{ knob.label }}</span>
            <!-- two DIFFERENT markers coexist. -->
            <span class="setting-badges">
              <span
                v-if="isModified(owner(knob), knob)"
                class="setting-dot"
                role="img"
                :aria-label="dotLabel"
                :title="dotLabel" />
              <span
                v-if="serverKnobDirty(knob)"
                class="setting-dirty"
                title="edited, not saved">*</span>
              <button
                v-if="canReset(owner(knob), knob)"
                class="setting-reset"
                type="button"
                :title="resetLabel"
                @click.stop.prevent="resetKnob(owner(knob), knob)">
                reset
              </button>
            </span>
          </div>
          <BandSelect
            v-if="knob.type === 'select'"
            class="setting-select"
            :model-value="selectValue(owner(knob), knob)"
            :options="knobOptions(knob)"
            :label="knob.label"
            drop="down"
            @update:model-value="setSelect(owner(knob), knob, $event)" />
          <ModelField
            v-else-if="knob.type === 'model'"
            :knob="knob"
            :value="modelValue(knob)"
            @update="setServer(knob, $event)" />
          <ProvidersField
            v-else-if="knob.type === 'providers'"
            :knob="knob"
            :value="providersValue(knob)"
            @update="setServer(knob, $event)" />
          <textarea
            v-else-if="knob.type === 'textarea'"
            class="setting-input setting-textarea"
            rows="3"
            :value="serverValue(knob)"
            @input="setServer(knob, ($event.target as HTMLTextAreaElement).value)" />
          <input
            v-else-if="knob.type === 'number' || knob.type === 'secret' || !isPathKnob(knob)"
            class="setting-input"
            :type="knob.type === 'secret' ? 'password' : knob.type === 'number' ? 'number' : 'text'"
            :min="knob.min"
            :max="knob.max"
            :step="knob.step ?? (knob.type === 'number' ? 1 : undefined)"
            autocomplete="off"
            spellcheck="false"
            :value="knob.type === 'number' ? numberValue(knob) : serverValue(knob)"
            @input="
              setServer(
                knob,
                ($event.target as HTMLInputElement).value,
                knob.type === 'number',
              )
            " />
          <!-- path + browse: ONE row, the value rides the normal draft/save diff. -->
          <div
            v-else
            class="setting-path">
            <input
              class="setting-input"
              type="text"
              autocomplete="off"
              spellcheck="false"
              :value="serverValue(knob)"
              @input="setServer(knob, ($event.target as HTMLInputElement).value)" />
            <WorkspaceSelect
              browse-label="browse"
              @update:workspace="(v: string) => setServer(knob, v)" />
          </div>
          <p v-if="knob.help" class="setting-help">{{ knob.help }}</p>
        </div>
        </template>
      </div>
    </section>
    </template>
    <!--
      the MODULES section.
      The nav entry (settingsNav.modulesNavCategory) is a jump link like any other
      category, and the jump/anchor/spy machinery treats this section exactly like a
      knob section (it carries.settings-category + data-section).
      The knob filters never apply here: a module row is not a knob, so a live query or
      "modified only" hides the section rather than pretending a module could be a
      search hit.
      item 1: the section is the pane's TAIL - it is rendered AFTER the surface groups,
      which is where SURFACE_ORDER (modules last) puts its nav entry, so the jump link
      and the document cannot disagree.
    -->
    <section
      v-if="modulesShown"
      class="settings-category settings-modules-category"
      data-category="modules"
      data-section="modules">
      <h2 class="settings-section-title">
        modules
        <span class="settings-knob-count">{{ moduleCount }}</span>
      </h2>
      <ModulesSection
        :narrow="narrow"
        @count="moduleCount = $event"
        @open-module="(moduleId: string) => emit('open-module', moduleId)" />
    </section>
    </div>
    <div v-if="dirtyCount > 0 && !scopeReadOnly" class="settings-save-row">
      <button
        class="settings-save"
        type="button"
        :disabled="saving"
        @click="save">
        {{ saving ? "saving…" : `save ${dirtyCount} change${dirtyCount === 1 ? "" : "s"}` }}
      </button>
      <button class="settings-revert" type="button" :disabled="saving" @click="resetDraft">
        discard
      </button>
    </div>
    <p v-if="saveError" class="settings-error">{{ saveError }}</p>
    <p class="settings-note">
      {{ noteText }}
    </p>
      </section>
    </div>

    <dialog
      ref="yamlDialogEl"
      class="settings-yaml"
      aria-label="edit config yaml"
      @close="yamlOpen = false"
      @keydown.ctrl.enter.prevent="saveYaml()"
      @keydown.meta.enter.prevent="saveYaml()">
      <div class="settings-yaml-head">
        <span class="settings-yaml-title">config.yaml</span>
        <button
          class="settings-yaml-close"
          type="button"
          aria-label="close the config editor"
          @click="closeYamlEditor()">
          close
        </button>
      </div>
      <textarea
        v-model="yamlText"
        class="settings-yaml-editor"
        aria-label="config yaml"
        spellcheck="false"
        autocomplete="off" />
      <p v-if="yamlError" class="settings-yaml-error">{{ yamlError }}</p>
      <div class="settings-yaml-actions">
        <button
          class="settings-save"
          type="button"
          :disabled="yamlSaving || !yamlDirty"
          @click="saveYaml()">
          {{ yamlSaving ? "saving…" : "save" }}
        </button>
        <button
          class="settings-revert"
          type="button"
          :disabled="yamlSaving || !yamlDirty"
          @click="yamlText = yamlLoaded">
          revert
        </button>
        <span class="settings-yaml-hint">ctrl+enter saves (cmd+enter on mac)</span>
      </div>
    </dialog>
  </aside>
</template>

<script setup lang="ts">
  import {
    computed,
    nextTick,
    onBeforeUnmount,
    onMounted,
    reactive,
    ref,
    watch,
  } from "vue";
  import {
    DEFAULT_THEME_ID,
    configExportFilename,
    fetchConfigYaml,
    fetchSettings,
    fetchSettingsScopes,
    filterSettingGroups,
    knobDiffersFromDefault,
    knobHasDefault,
    resetSettings,
    saveConfigYaml,
    saveSettings,
    scopeConfigDiff,
    scopeKnobDiffersFromMain,
    settingsDiff,
    updateAgentConfig,
    valuesEqual,
    themePickOptions,
    colorModeOptions,
    type SettingGroup,
    type SettingKnob,
    type SettingsPayload,
    type SettingsScope,
    type SettingsScopeKnob,
    type Appearance,
    type ColorMode,
    type UserTheme,
    type ProviderInstanceRow,
  } from "~/composables/omega";
  import { refreshModelCatalog } from "~/composables/modelCatalog";
  import ModelField from "./ModelField.vue";
  import ProvidersField from "./ProvidersField.vue";
  // the pane is ONE DOCUMENT.
  import {
    NAV_STORAGE_KEY,
    MODULES_CATEGORY,
    anchorSelection,
    buildNav,
    categoryCount,
    clampEndFollow,
    clampedRange,
    documentFor,
    expandedCategoryIds,
    groupBySurface,
    matchingCategoryIds,
    modulesNavCategory,
    paneScrollIsUser,
    paneScrollOwns,
    paneScrollSettle,
    paneScrollSettled,
    parseSelection,
    parseSettingsSection,
    resolveSelection,
    sectionFilter,
    selectionId,
    selectionLost,
    spyAnchorMayWrite,
    spyTargetIds,
    buildSpyTargets,
    targetSection,
    withSettingsSection,
    withSubgroups,
    type ClampHold,
    type DocSection,
    type NavCategory,
    type NavSubgroup,
    type PaneScrollState,
    type SpyAnchorTrace,
    type SurfaceGroup,
  } from "./settingsNav";
  // the MODULES section's body.
  import ModulesSection from "./ModulesSection.vue";
  import { useSettingsJump } from "~/composables/settingsJump";
  import { useNarrow } from "~/composables/narrow";
  import { DEFAULT_TOOL_DISPLAY, type ToolDisplay } from "~/composables/toolDisplay";
  import { DEFAULT_THINKING_DISPLAY, type ThinkingDisplay } from "~/composables/thinkingDisplay";
  import Icon from "../ui/Icon.vue";
  import WorkspaceSelect from "../chat/WorkspaceSelect.vue";
  import {
    DEFAULT_BOTTOM_WIDTH_MODE,
    setBottomWidthMode,
    useBottomWidthMode,
  } from "~/composables/bottomWidth";
  // the server.default_workspace value rides the same shared-signal shape.
  import { DEFAULT_WORKSPACE_KNOB, setDefaultWorkspace } from "~/composables/defaultWorkspace";

  const props = defineProps<{
    showThinking: boolean;
    appearance: Appearance;
    toolDisplay: ToolDisplay;
    thinkingDisplay: ThinkingDisplay;
    colorMode: ColorMode;
    themeId: string;
    userThemes: UserTheme[];
    /** the mobile drawer's overlay state (desktop tabs never pass it as toggled. */
    open?: boolean;
  }>();

  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  const emit = defineEmits<{
    (e: "update:showThinking", value: boolean): void;
    (e: "update:appearance", value: Appearance): void;
    (e: "update:toolDisplay", value: ToolDisplay): void;
    (e: "update:thinkingDisplay", value: ThinkingDisplay): void;
    (e: "set-color-mode", mode: ColorMode): void;
    (e: "set-theme", id: string): void;
    /** a MODULES row with UI opens its module's dock tab. */
    (e: "open-module", moduleId: string): void;
  }>();


  const groups = ref<SettingGroup[]>([]);
  const loaded = ref<Record<string, unknown>>({});
  const draft = ref<Record<string, unknown>>({});
  const loadError = ref<string | null>(null);
  const saveError = ref<string | null>(null);
  const saving = ref(false);

  // agent scopes (settings tier 3b) --- The payload's scopes ARE the tab strip.
  // Each scope keeps its OWN loaded/draft/unset state, so switching tabs never throws
  // an unsaved edit away (and a main-tab save round-trip keeps an untouched agent tab's
  // draft, see applyPayload).
  interface ScopeState {
    loaded: Record<string, unknown>;
    draft: Record<string, unknown>;
    // the dotted keys whose override the user asked to REMOVE: the wire
    // verb is `unset` (a key deletion), never a write of main's value
    unset: string[];
  }
  const scopes = ref<SettingsScope[]>([]);
  const activeScope = ref<string | null>(null);
  const scopeStates = reactive<Record<string, ScopeState>>({});
  // a 409 (or any refusal that means "this layer is not writable") locks the scope.
  const scopeLocks = reactive<Record<string, string>>({});

  const activeScopeData = computed<SettingsScope | null>(() =>
    activeScope.value === null
      ? null
      : scopes.value.find((s) => s.name === activeScope.value) ?? null,
  );
  const activeState = computed<ScopeState | null>(() =>
    activeScope.value === null ? null : scopeStates[activeScope.value] ?? null,
  );

  const scopeTabs = computed<{ name: string | null; label: string }[]>(() => [
    { name: null, label: "Main" },
    ...scopes.value.map((s) => ({ name: s.name, label: s.name })),
  ]);

  // R7 per-scope search: each tab keeps its OWN query, so switching from a broad Main
  // search to an agent tab never lands on a stale "no settings match" - and the filter
  // itself already runs per tab, since it filters that tab's group list (Main: the
  // registry + client groups, agent: the four overridable groups only - see allGroups).
  const queries = reactive<Record<string, string>>({});
  const query = computed<string>({
    get: () => queries[activeScope.value ?? "main"] ?? "",
    set: (value: string) => {
      queries[activeScope.value ?? "main"] = value;
    },
  });

  function selectScope(name: string | null) {
    if (activeScope.value === name) return;
    activeScope.value = name;
    saveError.value = null;
  }

  const scopeMessage = computed(() => {
    const scope = activeScopeData.value;
    if (!scope) return "";
    return scope.error ?? (activeScope.value ? scopeLocks[activeScope.value] ?? "" : "");
  });
  const scopeReadOnly = computed(() => scopeMessage.value !== "");

  // the dot/reset voice names its BASE.
  const dotLabel = computed(() =>
    activeScope.value === null ? "differs from default" : "differs from main",
  );
  const resetLabel = computed(() =>
    activeScope.value === null ? "reset to default" : "reset to main",
  );
  const noteText = computed(() =>
    activeScope.value === null
      ? "knobs marked live apply to the running server; the rest take effect on restart."
      : "agent knobs override main; a knob without a dot inherits main. reset removes the override and falls back to main. changes apply to this agent's next run.",
  );

  // Card 5: settings is a dock TAB, not an overlay.
  onMounted(() => {
    bindIntent(rootEl.value);
    // the config menu closes on any click outside its own chip - one always-attached
    // listener, guarded by the open state, so opening/closing never juggles
    // registrations.
    document.addEventListener("click", onDocumentClick);
    void reload();
  });

  async function reload() {
    loadError.value = null;
    saveError.value = null;
    try {
      applyPayload(await fetchSettings());
      // the model pickers' list rides ONE request per settings open.
      refreshModelCatalog();
      // the document exists only once the payload does, so the remembered anchor
      // restores HERE - and the spy arms over the rendered sections (a save round-trip
      // re-arms it, never restores).
      void nextTick(() => {
        restoreAnchor();
        syncSpy();
        // a palette jump that arrived before this payload landed lands HERE,.
        applyJump();
      });
    } catch (err) {
      groups.value = [];
      loaded.value = {};
      draft.value = {};
      loadError.value = err instanceof Error ? err.message : String(err);
    }
  }

  function applyPayload(payload: SettingsPayload) {
    groups.value = payload.groups;
    const values: Record<string, unknown> = {};
    for (const g of payload.groups)
      for (const k of g.knobs) values[k.key] = k.value;
    loaded.value = values;
    draft.value = { ...values };
    applyScopes(payload.scopes);
    setDefaultWorkspace(values[DEFAULT_WORKSPACE_KNOB]);
  }

  // applyScopes re-seeds the per-scope state from a fresh payload.
  function applyScopes(next: SettingsScope[]) {
    scopes.value = next;
    const seen = new Set<string>();
    for (const scope of next) {
      seen.add(scope.name);
      const values: Record<string, unknown> = {};
      for (const g of scope.groups)
        for (const k of g.knobs) values[k.key] = k.value;
      const prev = scopeStates[scope.name];
      if (prev && valuesEqual(prev.loaded, values)) {
        prev.loaded = values; // same truth: draft + unset survive
        continue;
      }
      scopeStates[scope.name] = { loaded: values, draft: { ...values }, unset: [] };
      delete scopeLocks[scope.name];
    }
    for (const name of Object.keys(scopeStates))
      if (!seen.has(name)) {
        delete scopeStates[name];
        delete scopeLocks[name];
      }
    if (activeScope.value !== null && !seen.has(activeScope.value))
      activeScope.value = null;
  }


  // The theme knob's option list. A broken theme (server-flagged error) never enters.
  const themeOptions = computed(() => themePickOptions(props.userThemes));

  const optLabel = (opt: string | { value: string; label: string }) =>
    typeof opt === "string" ? opt : opt.label;
  const optValue = (opt: string | { value: string; label: string }) =>
    typeof opt === "string" ? opt : opt.value;

  // the BandSelect listbox's options for a knob.
  function knobOptions(knob: SettingKnob) {
    return (knob.options ?? []).map((opt) => ({
      value: optValue(opt),
      label: optLabel(opt),
    }));
  }

  // the client knobs carry their OWN defaults.
  const chatGroup = computed<SettingGroup>(() => ({
    key: "chat",
    label: "chat",
    knobs: [
      {
        key: "chat.show_thinking",
        label: "show thinking blocks in chat",
        type: "toggle",
        value: props.showThinking,
        default: true,
      },
      {
        key: "chat.color_mode",
        label: "color mode",
        type: "select",
        value: props.colorMode,
        default: "auto",
        options: [...colorModeOptions],
        help: "interface colour palette; auto follows the system preference live",
      },
      {
        key: "chat.theme",
        label: "theme",
        type: "select",
        value: props.themeId,
        default: DEFAULT_THEME_ID,
        options: themeOptions.value,
        help: "colour theme; default is the built-in palette, the rest are the server's themes/ folder files",
      },
      {
        key: "chat.appearance",
        label: "chat appearance",
        type: "select",
        value: props.appearance,
        default: "chat",
        options: ["chat", "stacked"],
        help:
          "stacked renders each turn as one block and pins it to the top " +
          "of the pane while the answer streams; chat is the flat chronolog",
      },
      {
        key: "chat.tool_display",
        label: "tool result display",
        type: "select",
        value: props.toolDisplay,
        default: DEFAULT_TOOL_DISPLAY,
        options: ["auto", "expanded", "collapsed"],
        help:
          "auto expands tool rows while a call runs and collapses them " +
          "once it settles; a manual toggle pins a row; expanded/collapsed force one state",
      },
      {
        key: "chat.thinking_display",
        label: "thinking display",
        type: "select",
        value: props.thinkingDisplay,
        default: DEFAULT_THINKING_DISPLAY,
        options: ["auto", "expanded", "collapsed"],
        help:
          "auto keeps thinking blocks collapsed behind their one-line " +
          "summary and a manual click pins one open; expanded/collapsed force one state",
      },
    ],
  }));

  const bottomWidthMode = useBottomWidthMode();
  const dockGroup = computed<SettingGroup>(() => ({
    key: "dock",
    label: "dock",
    knobs: [
      {
        key: "dock.bottom_width",
        label: "bottom panel width",
        type: "select",
        value: bottomWidthMode.value,
        default: DEFAULT_BOTTOM_WIDTH_MODE,
        options: ["aligned", "full"],
        help:
          "aligned matches the bottom panel to the main pane's width " +
          "(the sidebars stand clear, like the VS Code panel); full " +
          "spans the whole window",
      },
    ],
  }));

  // Client knobs write props/emits, server knobs write the draft map.: the Commonly
  // Used group renders BOTH origins side by side, so the routing is per KNOB (the key
  // prefix names its owner), never per group alone - a curated theme row read through
  // serverValue would render empty and swallow its edit into a PUT /settings the server
  // never accepts.
  const CLIENT_KNOB_PREFIXES = ["chat.", "dock."];
  const isClientKnob = (knob: SettingKnob) =>
    CLIENT_KNOB_PREFIXES.some((prefix) => knob.key.startsWith(prefix));
  const isClient = (group: SettingGroup, knob?: SettingKnob) =>
    group.key === "chat" ||
    group.key === "dock" ||
    (knob !== undefined && isClientKnob(knob));

  const isPathKnob = (knob: SettingKnob) =>
    knob.key === "server.default_workspace";

  // the curated first group --- Membership is DATA, one line to change. Each entry is
  // picked BY KEY out of the already-loaded registry - the SAME knob objects, no clone
  // and no second fetch - so an edit here writes the draft map / emits the client event
  // exactly as the origin row does.
  const COMMONLY_USED_KEYS = [
    "provider.model_name",
    "provider.vision_model",
    "provider.title_model",
    "provider.approvals_model",
    "provider.compaction_model",
    "provider.summarizer_model",
    "provider.skills_model",
    "chat.theme",
    "chat.color_mode",
  ];

  const commonlyUsedGroup = computed<SettingGroup | null>(() => {
    // the chat group's knobs are rebuilt per render, so the lookup map
    // is built from the live groups every time - never a stale copy
    const byKey = new Map<string, SettingKnob>();
    for (const group of [chatGroup.value, dockGroup.value, ...groups.value])
      for (const knob of group.knobs) byKey.set(knob.key, knob);
    const knobs = COMMONLY_USED_KEYS.map((key) => byKey.get(key)).filter(
      (knob): knob is SettingKnob => knob !== undefined,
    );
    // a registry that carries none of the keys (an older server, a 501 seam) renders.
    if (knobs.length === 0) return null;
    return { key: "common", label: "Commonly Used", knobs };
  });


  // mainValues / mainKnobByKey read the MAIN registry (the server groups
  // only - chat/dock are client state and can never be an agent knob):
  // an agent row's inherited base and control metadata both come from
  // there, so one vocabulary serves both tabs.
  const mainValues = computed<Record<string, unknown>>(() => {
    const out: Record<string, unknown> = {};
    for (const group of groups.value)
      for (const knob of group.knobs) out[knob.key] = knob.value;
    return out;
  });
  const mainKnobByKey = computed(() => {
    const out = new Map<string, SettingKnob>();
    for (const group of groups.value)
      for (const knob of group.knobs) out.set(knob.key, knob);
    return out;
  });

  const SCOPE_GROUP_LABELS: Record<string, string> = {
    provider: "provider",
    system_prompt: "system prompt",
    description: "description",
    max_turns: "max turns",
  };
  const SCOPE_KNOB_TYPES: Record<string, SettingKnob["type"]> = {
    "provider.type": "select",
    "provider.host": "text",
    "provider.api_key": "secret",
    "provider.model_name": "model",
    system_prompt: "textarea",
    description: "text",
    max_turns: "number",
  };
  // the help text names what the row MEANS on this tab: the main
  // registry's text talks about config.yaml and restarts, which is not
  // this surface's contract (one PUT /agents/{name}, reset falls back)
  const SCOPE_KNOB_HELP: Record<string, string> = {
    "provider.type": "provider flavor for this agent; empty defaults to ollama; mock answers deterministically",
    "provider.host": "provider base URL for this agent; empty inherits main's",
    "provider.api_key": "masked; a key here overrides main's for this agent - reset falls back to main's key",
    "provider.model_name": "model this agent serves; reset falls back to the main model",
    system_prompt: "this agent's system prompt; reset falls back to the main system prompt",
    description: "one-line description for this agent (the roster subtitle)",
    max_turns: "tool-loop turn cap for this agent's runs; empty inherits main (0 = the 100 default)",
  };

  function scopeKnobDef(knob: SettingsScopeKnob): SettingKnob {
    const base = mainKnobByKey.value.get(knob.key);
    return {
      key: knob.key,
      label: base?.label ?? SCOPE_GROUP_LABELS[knob.key] ?? knob.key,
      type: SCOPE_KNOB_TYPES[knob.key] ?? base?.type ?? "text",
      value: knob.value,
      // the DIFF base on an agent tab is MAIN's value.
      default: mainValues.value[knob.key] ?? "",
      options: base?.options,
      min: base?.min,
      max: base?.max,
      step: base?.step,
      help: SCOPE_KNOB_HELP[knob.key],
    };
  }

  const scopeGroups = computed<SettingGroup[]>(() => {
    const scope = activeScopeData.value;
    if (!scope) return [];
    const out: SettingGroup[] = [];
    for (const group of scope.groups) {
      const knobs = group.knobs.map(scopeKnobDef);
      if (knobs.length === 0) continue;
      out.push({
        key: group.key,
        label: SCOPE_GROUP_LABELS[group.key] ?? group.key,
        knobs,
      });
    }
    return out;
  });

  const allGroups = computed<SettingGroup[]>(() => {
    // R3: an agent tab's ONE source is the scope's four groups.
    if (activeScope.value !== null) return scopeGroups.value;
    // the curated group is ALWAYS in the list now - the nav must keep its shape.
    // The duplicate-row risk is answered in documentFor: a live query SKIPS the curated
    // section, so a hit renders once, under the domain that owns it.
    return [
      ...(commonlyUsedGroup.value ? [commonlyUsedGroup.value] : []),
      chatGroup.value,
      dockGroup.value,
      ...groups.value,
    ];
  });

  // instanceRows was the model pickers' instance feed.


  // The knob's OWN domain, for the row plumbing below. A knob can be rendered from the
  // virtual Commonly Used category, but its diff base, its client/server routing and
  // its reset target belong to the domain that DEFINED it - so the lookup is built from
  // the real groups, never from the nav (which mixes domains in the curated category).
  const ownerGroups = computed<SettingGroup[]>(() =>
    activeScope.value === null
      ? [chatGroup.value, dockGroup.value, ...groups.value]
      : scopeGroups.value,
  );
  const ownerMap = computed(() => {
    const map = new Map<string, SettingGroup>();
    for (const group of ownerGroups.value)
      for (const knob of group.knobs) if (!map.has(knob.key)) map.set(knob.key, group);
    return map;
  });
  const owner = (knob: SettingKnob): SettingGroup =>
    ownerMap.value.get(knob.key) ?? { key: "", label: "", knobs: [] };

  // The nav builds itself from the per-knob subgroup DATA. Empty categories never
  // enter.: the MODULES entry is APPENDED - it is not a knob domain, it is the module
  // lifecycle surface, and pins it at the pane's TAIL (the list of what is switched on
  // is the last thing you read, not the first). Its count is the live module-row total
  // (reported by the section), never a knob count.: the sections are GROUPED by surface
  // class, and the groups decide the ORDER as well as the headers - `nav` is the
  // flattened group list, so the jump links, the document and the spy all walk one
  // order. The pane never sorts: groupBySurface buckets in SURFACE_ORDER (curated,
  // frontend, backend, modules) and keeps each bucket's wire order.
  const moduleCount = ref(0);
  const navGroups = computed<SurfaceGroup<NavCategory>[]>(() =>
    groupBySurface(
      [...buildNav(withSubgroups(allGroups.value)), modulesNavCategory(moduleCount.value)],
      (cat) => cat.surface,
    ),
  );
  const nav = computed<NavCategory[]>(() =>
    navGroups.value.flatMap((group) => group.items),
  );

  // a domain whose knobs all land in ONE child shows no child jump link.
  const navChildren = (cat: NavCategory): NavSubgroup[] =>
    cat.subgroups.length > 1 ? cat.subgroups : [];

  // the selection: jump links, not a selector --- Selection is state the USER sets and
  // clears - never a pure read of the scroll.
  const selection = ref<string | null>(null);

  // the narrow drawer: the SAME jump links over the SAME document
  const drawerOpen = ref(false);

  // the DRAWER's focus law --------------------------- The narrow pane's nav is a MODAL
  // drawer: it covers the document behind a scrim, so while it is open the keyboard may
  // not wander into that document, Escape must close it, and closing must hand focus
  // back to the toggle that opened it.
  const drawerEl = ref<HTMLElement | null>(null);
  const jumpEl = ref<HTMLButtonElement | null>(null);

  const FOCUSABLE =
    'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  function drawerFocusables(): HTMLElement[] {
    const nav = drawerEl.value;
    if (!nav) return [];
    return Array.from(nav.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
      (el) => el.offsetParent !== null,
    );
  }

  function openDrawer() {
    drawerOpen.value = true;
    // a modal drawer holds focus, so focus enters it - the trap has nothing to hold.
    void nextTick(() => drawerFocusables()[0]?.focus());
  }

  function closeDrawer() {
    if (!drawerOpen.value) return;
    drawerOpen.value = false;
    void nextTick(() => jumpEl.value?.focus());
  }

  /**
   * Escape closes; Tab and Shift+Tab WRAP inside the drawer.
   * The trap is a boundary check on the pane, not a body-wide listener: focus starts
   * inside the drawer (openDrawer) and every wrap returns it there.
   */
  function onDrawerKey(e: KeyboardEvent) {
    if (!narrow.value || !drawerOpen.value) return;
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      closeDrawer();
      return;
    }
    if (e.key !== "Tab") return;
    const items = drawerFocusables();
    if (!items.length) return;
    const first = items[0]!;
    const last = items[items.length - 1]!;
    const active = document.activeElement as HTMLElement | null;
    if (!active || !drawerEl.value?.contains(active)) {
      e.preventDefault();
      (e.shiftKey ? last : first).focus();
      return;
    }
    if (e.shiftKey && active === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  }

  // EXPANSION IS DERIVED FROM THE SELECTION. There is no collapsed set, no per-parent
  // toggle and nothing remembered: exactly the branch the selection lives in is open,
  // every other branch shut - so "what is open" and "where I am" can never disagree.
  const expandedIds = computed<ReadonlySet<string>>(() =>
    expandedCategoryIds(
      selection.value,
      searching.value ? matchIds.value : null,
    ),
  );
  const openChildren = (cat: NavCategory): NavSubgroup[] =>
    expandedIds.value.has(cat.id) ? navChildren(cat) : [];

  const bodyEl = ref<HTMLElement | null>(null);
  // id -> element for BOTH levels. Rebuilt from the DOM by collectSpyTargets whenever
  // the document changes (a query, a scope), so a filtered-away section can never leave
  // a stale target behind.
  const spyNodes = new Map<string, HTMLElement>();
  // The observer starts SHUT.
  let spyLocked = true;
  let spyTimer: ReturnType<typeof setTimeout> | undefined;

  function lockSpy(ms?: number) {
    spyLocked = true;
    if (spyTimer !== undefined) clearTimeout(spyTimer);
    spyTimer = undefined;
    if (ms !== undefined) {
      spyTimer = setTimeout(() => {
        spyLocked = false;
        spyTimer = undefined;
        clampOnLockEnd();
        settleSpyIfIdle();
      }, ms);
    }
  }
  function unlockSpy() {
    if (spyTimer !== undefined) clearTimeout(spyTimer);
    spyTimer = undefined;
    spyLocked = false;
    clampOnLockEnd();
  }

  // the pane's OWN scrolls --------------------------- The suppression above exists.
  // Gating the release on the INTENT_EVENTS list alone was the defect: a scrollbar
  // drag, an assistive-technology scroll and a programmatic scrollTop dispatch none of
  // wheel/touchstart/pointerdown/keydown in Chromium, so a scroll that arrived while
  // the lock was up was never clamped AND never moved the spy - the pane simply stayed
  // wherever it landed (the nightly landed it on the document's end, 5709, where the
  // selected section's bottom was 2509). A duration guess would be wrong twice - Chrome
  // runs a long smooth jump ~1.1s, and an animation that gets OVERRIDDEN (a
  // programmatic scrollTop, a scrollbar drag) lands nowhere at all - while the gap
  // timer answers both: the events stop, the pane's scroll is over, and the clamp
  // re-measures (a scroll that interrupted a jump can never leave the pane outside its
  // section).
  const PANE_SCROLL_GAP_MS = 400;
  // a frame or two of settle: the animation's own trailing repeats must not release.
  const PANE_SETTLE_GRACE_MS = 200;
  let paneScroll: PaneScrollState = { target: null, graceUntil: 0, intent: false };
  let paneScrollTimer: ReturnType<typeof setTimeout> | undefined;
  function nowMs(): number {
    return typeof performance !== "undefined" ? performance.now() : Date.now();
  }
  /** armPaneSettle: the pane's own scroll is still producing events */
  function armPaneSettle() {
    if (paneScrollTimer !== undefined) clearTimeout(paneScrollTimer);
    paneScrollTimer = setTimeout(() => {
      paneScrollTimer = undefined;
      if (paneScroll.target === null) return;
      paneScroll = paneScrollSettle(nowMs(), PANE_SETTLE_GRACE_MS);
      // the pane's own scroll is over: hold it to its section again, so an
      // interrupted animation cannot strand it outside the range
      if (clampId.value) scheduleClamp();
      settleSpyIfIdle();
    }, PANE_SCROLL_GAP_MS);
  }
  function endPaneScroll() {
    paneScroll = { target: null, graceUntil: 0, intent: false };
    if (paneScrollTimer !== undefined) clearTimeout(paneScrollTimer);
    paneScrollTimer = undefined;
  }
  /** notePaneScroll: the pane is about to scroll ITSELF to `top`. */
  function notePaneScroll(root: HTMLElement, top: number, intent = false) {
    const limit = Math.max(root.scrollHeight - root.clientHeight, 0);
    paneScroll = { target: Math.min(Math.max(top, 0), limit), graceUntil: 0, intent };
    armPaneSettle();
  }
  /** paneOwnScroll: true while a scroll event still belongs to a scroll the PANE. */
  function paneOwnScroll(): boolean {
    const now = nowMs();
    if (paneScrollIsUser(paneScroll, now)) return false;
    if (paneScroll.target !== null) armPaneSettle();
    return true;
  }

  // the anchor is the USER's ------------------------- The spy may move the HIGHLIGHT
  // on any delivery, but the remembered anchor is "the section the user actually
  // scrolled to": a delivery armed by a geometry change (a viewport resize, a reflow,
  // the clamp's own re-snap) resolves a different id at new boxes and must not overrule
  // it - the nightly widened the pane after a drawer jump and the stored anchor read
  // back as the neighbouring section.
  let anchorTrace: SpyAnchorTrace = { userScrollAt: 0, geometryAt: 0, wroteAt: 0 };

  function collectSpyTargets(root: HTMLElement) {
    spyNodes.clear();
    // SECTIONS answer first: a flat or one-child category renders one unheaded block.
    const targets = buildSpyTargets<HTMLElement>({
      querySelectorAll: (selector) => root.querySelectorAll<HTMLElement>(selector),
    });
    for (const [id, node] of targets) spyNodes.set(id, node);
  }

  function reducedMotion(): boolean {
    return (
      typeof window !== "undefined" &&
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    );
  }

  /**
   * The height of the heading a target sits UNDER (its own section's sticky heading).
   * A block jump must clear it - the heading pins at the scrollport's top edge, so a
   * block scrolled to exactly 0 would paint its first rows BEHIND it.
   */
  function pinnedHeadingHeight(node: HTMLElement): number {
    const section = node.closest(".settings-category");
    const heading = section?.querySelector<HTMLElement>(".settings-section-title");
    return heading ? heading.offsetHeight : 0;
  }

  function scrollToAnchor(id: string | null) {
    const root = bodyEl.value;
    if (!root) return;
    const behavior: ScrollBehavior = reducedMotion() ? "auto" : "smooth";
    const node = id
      ? spyNodes.get(id) ?? spyNodes.get(parseSelection(id).category)
      : null;
    if (!node) {
      notePaneScroll(root, 0, true);
      root.scrollTo({ top: 0, behavior });
      return;
    }
    // a SECTION lands with its own heading exactly at the top.
    // The section box resolves through closest, so the clear is measured from the SAME
    // box the jump moves - a target answering for its own section can never subtract
    // the heading it is meant to sit.
    const section = targetSection(node);
    const isBlock = node !== section;
    const rootTop = root.getBoundingClientRect().top;
    const delta = node.getBoundingClientRect().top - rootTop;
    const clear = isBlock ? pinnedHeadingHeight(node) : 0;
    notePaneScroll(root, root.scrollTop + delta - clear, true);
    root.scrollTo({ top: root.scrollTop + delta - clear, behavior });
  }

  function persistAnchor(id: string | null) {
    try {
      // DESELECTED persists as NO anchor: a reload must open at the top, never.
      if (id) localStorage.setItem(NAV_STORAGE_KEY, id);
      else localStorage.removeItem(NAV_STORAGE_KEY);
    } catch {
      // private mode / quota: the session still navigates, only the
      // memory is gone - never a broken click
    }
  }

  /**
   * first click: select + expand + jump.
   * The second half matters for the clamp: the spy moves the highlight onto a block
   * while the user scrolls INSIDE a selected category, and the promised "click again to
   * release" must not depend on where the highlight landed.
   */
  function selectCategory(id: string) {
    const picked = parseSelection(id);
    if (
      selection.value === id ||
      (picked.subgroup === null && clampId.value === picked.category)
    ) {
      deselect();
      return;
    }
    selection.value = id;
    // a REAL selection clamps the pane to its category; the spy's highlight never does.
    clampId.value = picked.category;
    persistAnchor(id);
    // the observer must not fight the smooth scroll it just started
    lockSpy(650);
    void nextTick(() => scrollToAnchor(id));
    if (narrow.value) closeDrawer();
  }

  function deselect() {
    selection.value = null;
    // the clamp releases in the SAME frame.
    clampId.value = null;
    persistAnchor(null);
    lockSpy(); // stays shut until the NEXT real scroll
    scrollToAnchor(null);
    if (narrow.value) closeDrawer();
  }

  const pendingJump = useSettingsJump();

  /** applyJump consumes a pending request. */
  function applyJump(): boolean {
    const id = pendingJump.value;
    if (!id) return false;
    // a SECTION FILTER from an earlier click can be hiding the palette's target.
    const blocking = filterSection.value;
    if (blocking !== null && blocking !== id) {
      selection.value = null;
      clampId.value = null; // the URL drops its section with it
      lockSpy();
      void nextTick(() => {
        applyJump();
      });
      return false;
    }
    if (!modulesShown.value) return false;
    const root = bodyEl.value;
    if (!root || !root.querySelector(`.settings-category[data-section="${id}"]`)) {
      return false;
    }
    pendingJump.value = null;
    selectCategory(id);
    return true;
  }

  watch(pendingJump, (id) => {
    if (!id) return;
    void nextTick(() => {
      applyJump();
    });
  });

  const INTENT_EVENTS = ["wheel", "touchstart", "pointerdown", "keydown"];
  let intentTarget: HTMLElement | null = null;
  const noteUserIntent = () => {
    // input INTERRUPTS the pane's own animation.
    endPaneScroll();
    unlockSpy();
    // the gesture is ALSO the trigger a scroll EVENT cannot give.
    if (clampId.value) scheduleClamp();
  };
  function bindIntent(el: HTMLElement | null) {
    if (intentTarget === el) return;
    if (intentTarget) {
      for (const type of INTENT_EVENTS)
        intentTarget.removeEventListener(type, noteUserIntent);
      intentTarget = null;
    }
    if (!el) return;
    intentTarget = el;
    for (const type of INTENT_EVENTS)
      el.addEventListener(type, noteUserIntent, { passive: true });
  }

  // the document scroll position a cleared query must give back
  let lastDocScrollTop = 0;
  function onBodyScroll() {
    if (!searching.value && bodyEl.value) lastDocScrollTop = bodyEl.value.scrollTop;
    // a scroll the pane did not issue IS the user's hand, whatever event started it:
    // the suppression ends here, so the spy answers a scrollbar drag or an AT scroll
    // exactly like a wheel
    if (bodyEl.value && !paneOwnScroll()) {
      unlockSpy();
      // and it is the ONLY kind of scroll that may rewrite the remembered anchor
      anchorTrace.userScrollAt = nowMs();
    }
    if (clampId.value) scheduleClamp();
  }

  // the SELECTED-SECTION scroll clamp ---------------- Endo, "when i select a. A
  // RESTORED anchor does not clamp either: the remembered id is written by the spy too,
  // so reopening the pane must never trap a scroll in a section the user merely
  // scrolled past; a URL section is the one restore that DOES clamp, because the user
  // named it.
  const clampId = ref<string | null>(null);
  let clampObserver: ResizeObserver | null = null;
  let clampFrame = 0;
  // the end the clamp last left the pane ON (and where it left it).
  let clampHold: ClampHold | null = null;

  /**
   * the section a selection clamps to - its CATEGORY section, which is the box carrying
   * the sticky heading; a block jump inside it still lands clear of that heading
   * (scrollToAnchor), so the clamp never undoes a subsection jump / /
   */
  function clampNode(id: string): HTMLElement | null {
    const root = bodyEl.value;
    if (!root) return null;
    const category = parseSelection(id).category;
    return (
      spyNodes.get(category) ??
      // SECTIONS only: a flat or one-child category's single block repeats the id.
      root.querySelector<HTMLElement>(
        `.settings-category[data-section="${category}"]`,
      )
    );
  }

  /**
   * applyClamp holds scrollTop inside the selected section's range.
   * A dropped pass never re-runs, so a window computed from a box that was still moving
   * used to be the last word the pane was held to (the nightly's 816 against the
   * settled 814), and a gesture that produced no scroll event at all left nothing
   * holding it (noteUserIntent is the other trigger).
   */
  function applyClamp() {
    // a programmatic jump is intent, not the user's hand: the pane's OWN scroll
    // (tracked by its landing position) is the ONE suppression (step 5 - no second
    // mechanism), so the smooth scroll and a deselect's return-to-top are never fought.
    const root = bodyEl.value;
    if (!root) return;
    const id = clampId.value;
    if (!id) return;
    // a section a live query or `modified only` filtered out of the document has no.
    const node = clampNode(id);
    if (!node) return;
    const [min, max] = clampedRange({
      top: node.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop,
      height: node.offsetHeight,
      viewport: root.clientHeight,
      content: root.scrollHeight,
    });
    const now = root.scrollTop;
    // the suppression, asked AFTER the live measurement: only the pane's own DELIBERATE
    // landing is exempt - never the clamp's own re-snap, whose dropped pass would leave
    // a stale window in place
    if (paneScrollOwns(paneScroll)) return;
    // the end is the ONE side of the window the bound cannot follow BOTH ways.
    const follow = clampEndFollow(now, clampHold, max);
    const next = follow ?? Math.min(Math.max(now, min), max);
    // the pane rests ON the end (or was put there by this pass). Any other resting
    // place clears the record - a parked pane never follows.
    clampHold = Math.abs(next - max) < 1 ? { at: next, max } : null;
    // COMPARE BEFORE ASSIGNING. Sub-pixel positions never settle exactly.
    if (Math.abs(next - now) < 1) return;
    // the re-snap is the PANE's own scroll, and noting it is what keeps it out of the
    // spy's anchor gate: a clamp is not the user's hand, so the scroll event it fires
    // must not arm an anchor write.
    notePaneScroll(root, next);
    root.scrollTop = next;
  }

  /**
   * the clamp lands on the EVENT, and again on the frame.
   * The event pass is NOT optional - a hidden or occluded tab throttles
   * requestAnimationFrame to nothing, and the pane then keeps a scroll it was never
   * allowed to rest.
   */
  function scheduleClamp() {
    applyClamp();
    if (clampFrame) return;
    clampFrame = requestAnimationFrame(() => {
      clampFrame = 0;
      applyClamp();
    });
  }

  /**
   * clampOnLockEnd: a lock LIFTING is not the same thing as free scroll ard ).
   * A scroll request that produces NO event therefore slips between both: a same-value
   * instant request (`scrollTop = 1e6` while the pane already rests at the document's
   * end, which is exactly where a previous free scroll left it) fires nothing, and the
   * very request that ends the pane's own scroll tracking (a wheel, an intent event)
   * clears the settle timer that would have re-measured 400ms later.
   * The probe that opened the card, on the dev lane's settings document (13 categories,
   * pane clientHeight 423, document end 6247, provider range [1813, 2722]): nav-click
   * provider, wheel + `scrollTop = 1e6` in the NEXT task, side the same frame -> 6247,
   * and it STAYED the same wheel + assignment 2s after the click -> 2722 (clamped) e
   * same, 3s after (no lock, no suppression) -> 2722 (clamped) e same frame WITHOUT the
   * wheel -> 2722 (clamped) So the swallow is not the spy lock refusing the clamp (the
   * clamp never read that lock - the un-wheeled row above is inside the same lock and
   * ill clamps) and not the merged event path (the 3s row): it is a scroll nobody ever
   * measures.
   * A swallowed scroll must not become a forgotten one, so the moment a lock lifts, ONE
   * pass runs against the position the pane actually sits at - the same applyClamp, the
   * same clampedRange, no new primitive.
   * Do NOT shorten the timed lock: it is what keeps the observer from fighting the
   * smooth scroll it started.
   */
  function clampOnLockEnd() {
    if (clampId.value) scheduleClamp();
  }

  /** observeClamp re-arms over the boxes the range is measured from. */
  function observeClamp() {
    if (typeof ResizeObserver === "undefined") return;
    if (!clampObserver) clampObserver = new ResizeObserver(() => applyClamp());
    clampObserver.disconnect();
    const root = bodyEl.value;
    const id = clampId.value;
    if (!root || !id) return;
    clampObserver.observe(root);
    const node = clampNode(id);
    if (node) clampObserver.observe(node);
  }

  watch(clampId, () => {
    clampHold = null;
    void nextTick(() => {
      observeClamp();
      applyClamp();
    });
  });

  onBeforeUnmount(() => {
    unlockSpy();
    endPaneScroll();
    bindIntent(null);
    document.removeEventListener("click", onDocumentClick);
    spyObserver?.disconnect();
    spyObserver = null;
    paneBoxObserver?.disconnect();
    paneBoxObserver = null;
    clampObserver?.disconnect();
    clampObserver = null;
    if (clampFrame) cancelAnimationFrame(clampFrame);
    clampFrame = 0;
  });

  // the VSCode modified-only switch above the content list.
  const modifiedOnly = ref(false);


  // The pane reads ONE pair of maps.
  const paneLoaded = computed<Record<string, unknown>>(() =>
    activeScope.value === null ? loaded.value : activeState.value?.loaded ?? {},
  );
  const paneDraft = computed<Record<string, unknown>>(() =>
    activeScope.value === null ? draft.value : activeState.value?.draft ?? {},
  );

  function writeDraft(key: string, value: unknown) {
    const name = activeScope.value;
    if (name !== null) {
      const state = scopeStates[name];
      if (!state) return;
      state.draft = { ...state.draft, [key]: value };
      if (state.unset.includes(key))
        state.unset = state.unset.filter((k) => k !== key);
      return;
    }
    draft.value = { ...draft.value, [key]: value };
  }

  // clientValue reads the live chat/dock state: a knob the user edited (dirty or not)
  // always shows the client's own truth - never the server draft map.
  const clientValue = (knob: SettingKnob): unknown => {
    if (knob.key === "chat.show_thinking") return props.showThinking;
    if (knob.key === "chat.appearance") return props.appearance;
    if (knob.key === "chat.tool_display") return props.toolDisplay;
    if (knob.key === "chat.thinking_display") return props.thinkingDisplay;
    if (knob.key === "dock.bottom_width") return bottomWidthMode.value;
    if (knob.key === "chat.color_mode") return props.colorMode;
    return props.themeId;
  };

  // serverValue reads draft-first.
  const serverValue = (knob: SettingKnob): unknown =>
    paneDraft.value[knob.key] ?? paneLoaded.value[knob.key];

  // modelValue: the model pickers take strings - a number-typed server value.
  const modelValue = (knob: SettingKnob): string => {
    const v = serverValue(knob);
    return typeof v === "string" ? v : v == null ? "" : String(v);
  };

  // numberValue: number inputs must never render a half-state.
  const numberValue = (knob: SettingKnob): unknown => {
    const v = serverValue(knob);
    return v === "" ? "" : v;
  };

  // providersValue: the list editor's rows - a structural value coerced to its
  // named-endpoint shape (no model column; tolerant: an unexpected kind degrades to no
  // rows, never a render crash).
  const providersValue = (knob: SettingKnob): ProviderInstanceRow[] => {
    const v = serverValue(knob);
    if (!Array.isArray(v)) return [];
    return v
      .filter((r): r is ProviderInstanceRow => !!r && typeof r === "object")
      .map((r) => ({
        name: typeof r["name"] === "string" ? r["name"] : "",
        type: typeof r["type"] === "string" ? r["type"] : "",
        host: typeof r["host"] === "string" ? r["host"] : "",
        api_key: typeof r["api_key"] === "string" ? r["api_key"] : "",
      }));
  };

  function toggleValue(group: SettingGroup, knob: SettingKnob): boolean {
    if (isClient(group, knob)) return clientValue(knob) === true;
    return serverValue(knob) === true;
  }

  function selectValue(group: SettingGroup, knob: SettingKnob): string {
    const v = isClient(group, knob) ? clientValue(knob) : serverValue(knob);
    return typeof v === "string" ? v : String(v);
  }

  function setToggle(group: SettingGroup, knob: SettingKnob, on: boolean) {
    if (isClient(group, knob)) {
      if (knob.key === "chat.show_thinking") emit("update:showThinking", on);
      return;
    }
    writeDraft(knob.key, on);
  }

  function setSelect(group: SettingGroup, knob: SettingKnob, value: string) {
    if (isClient(group, knob)) {
      if (knob.key === "chat.color_mode")
        emit("set-color-mode", value as ColorMode);
      else if (knob.key === "chat.theme") emit("set-theme", value);
      else if (knob.key === "chat.appearance")
        emit("update:appearance", value as Appearance);
      else if (knob.key === "chat.tool_display")
        emit("update:toolDisplay", value as ToolDisplay);
      else if (knob.key === "chat.thinking_display")
        emit("update:thinkingDisplay", value as ThinkingDisplay);
      else if (knob.key === "dock.bottom_width")
        setBottomWidthMode(value === "full" ? "full" : "aligned");
      return;
    }
    writeDraft(knob.key, value);
  }

  // setServer stores text verbatim; numeric inputs parse what the
  // number field yields ("12" -> 12, "12abc" stays a string so the
  // server validation surfaces it instead of silently rounding).
  function setServer(knob: SettingKnob, raw: string, numeric = false) {
    let value: unknown = raw;
    if (numeric) {
      const n = Number(raw);
      value = raw === "" ? "" : Number.isFinite(n) ? n : raw;
    }
    writeDraft(knob.key, value);
  }

  // knobValue is the value the row is SHOWING right now.
  const knobValue = (group: SettingGroup, knob: SettingKnob): unknown =>
    isClient(group) ? clientValue(knob) : serverValue(knob);

  // isModified: the VS Code modified marker. A knob the server knows no default for
  // (default: null - provider.model_name) never shows the dot.: on an agent tab the
  // SAME marker names a different BASE - differs from MAIN, because main's inherited
  // value is that tab's default. The base is structural where the value cannot answer
  // (provider.api_key rides masked, so the override flag is the only honest "this agent
  // has its own").
  const scopeOverriddenKeys = computed<Set<string>>(() => {
    const out = new Set<string>();
    const scope = activeScopeData.value;
    if (!scope) return out;
    for (const group of scope.groups)
      for (const knob of group.knobs) if (knob.overridden) out.add(knob.key);
    return out;
  });

  const scopeDiffersFromMain = (knob: SettingKnob): boolean => {
    const state = activeState.value;
    if (!state) return false;
    return scopeKnobDiffersFromMain(
      knob.key,
      state.draft[knob.key],
      mainValues.value[knob.key],
      scopeOverriddenKeys.value.has(knob.key),
      state.unset,
    );
  };

  const isModified = (group: SettingGroup, knob: SettingKnob): boolean =>
    activeScope.value === null
      ? knobDiffersFromDefault(knobValue(group, knob), knob.default)
      : scopeDiffersFromMain(knob);

  // canReset: Main - the tier-2b rule (a modified row resets to the config default).
  // Agent tab - reset is the OVERRIDE REMOVAL, so it exists exactly while the row
  // differs from main (a saved override to delete, or a pending edit to undo).
  const canReset = (group: SettingGroup, knob: SettingKnob): boolean =>
    activeScope.value === null ? isModified(group, knob) : scopeDiffersFromMain(knob);


  // The query's knob set comes from the EXISTING filter.
  // null means NO query is live; an EMPTY set is a query that matched nothing, and the
  // two must stay distinguishable (everything vs. the empty state).
  const searchHits = computed<ReadonlySet<string> | null>(() => {
    if (query.value.trim() === "") return null;
    const hits = new Set<string>();
    for (const group of filterSettingGroups(allGroups.value, query.value))
      for (const knob of group.knobs) hits.add(knob.key);
    return hits;
  });
  const searching = computed(() => searchHits.value !== null);

  // The SECTION FILTER: the category the document is narrowed to. It reads the REAL
  // selection (clampId, the pane's own record of a user click) and never the spy's
  // highlight - a scroll must not collapse the document under the hand that is
  // scrolling it.
  const filterSection = computed<string | null>(() =>
    sectionFilter(clampId.value, searching.value),
  );

  // the MODULES section is CONFIGURATION over persisted module state, not a knob.
  const modulesShown = computed(
    () =>
      !searching.value &&
      !modifiedOnly.value &&
      (filterSection.value === null || filterSection.value === MODULES_CATEGORY),
  );

  // predicate: the SAME modified test the rows carry, asked through the knob's own
  // domain (the dot and the switch can never disagree about which knobs are modified).
  const knobModified = (knob: SettingKnob): boolean =>
    isModified(owner(knob), knob);

  const docSections = computed<DocSection[]>(() =>
    documentFor(nav.value, {
      hits: searchHits.value,
      modifiedOnly: modifiedOnly.value,
      isModified: knobModified,
      only: filterSection.value,
    }),
  );
  // the document's SURFACE GROUPS.
  const docGroups = computed<SurfaceGroup<DocSection>[]>(() =>
    groupBySurface(docSections.value, (section) => section.surface),
  );
  const matchIds = computed<ReadonlySet<string>>(() =>
    matchingCategoryIds(nav.value, {
      hits: searchHits.value,
      modifiedOnly: modifiedOnly.value,
      isModified: knobModified,
    }),
  );

  const totalCount = computed(() =>
    docSections.value.reduce((n, section) => n + section.count, 0),
  );

  // the scroll-spy (the nav follows the document) --- / added the MODULES section.
  const spyOrder = computed<string[]>(() => [
    ...spyTargetIds(docSections.value),
    ...(modulesShown.value ? [MODULES_CATEGORY] : []),
  ]);
  let spyObserver: IntersectionObserver | null = null;
  let paneBoxObserver: ResizeObserver | null = null;

  /** the band membership, MEASURED. */
  function spyBand(): (id: string) => boolean {
    const root = bodyEl.value;
    if (!root) return () => false;
    const rootTop = root.getBoundingClientRect().top;
    const bandBottom = rootTop + root.clientHeight * 0.2;
    return (id) => {
      const node = spyNodes.get(id);
      if (!node) return false;
      const box = node.getBoundingClientRect();
      return box.bottom > rootTop && box.top < bandBottom;
    };
  }

  /**
   * observePaneBox: the scrollport's OWN box moving is a geometry change, never a
   * scroll - a resize re-resolves every target at new heights and the delivery it arms
   * must not rewrite the anchor.
   */
  function observePaneBox(root: HTMLElement | null) {
    if (typeof ResizeObserver === "undefined") return;
    if (!paneBoxObserver)
      paneBoxObserver = new ResizeObserver(() => {
        anchorTrace.geometryAt = nowMs();
      });
    paneBoxObserver.disconnect();
    if (root) paneBoxObserver.observe(root);
  }

  /**
   * One thin band at the top of the scrollport.
   * A generous band would let two neighbours fight over the highlight mid-scroll, which
   * is exactly the flicker the root margin is chosen to avoid; the suppression window
   * above is the other half (the observer never fights the click that started a smooth
   * scroll, and a deselect stays put until the next real scroll).
   */
  /** pickSpySection is THE selection rule, shared by both membership sources. */
  function pickSpySection(inBandId: (id: string) => boolean): string | null {
    const root = bodyEl.value;
    if (!root) return null;
    const rootTop = root.getBoundingClientRect().top;
    let best: string | null = null;
    let first: string | null = null;
    for (const id of spyOrder.value) {
      if (!inBandId(id)) continue;
      const node = spyNodes.get(id);
      if (!node) continue;
      if (first === null) first = id;
      // a BLOCK lands just BELOW the section's pinned heading, so its own top is the
      // heading height away from the scrollport's - the edge it must reach is the
      // heading's bottom, or a subsection jump would never be the section the pane is
      // looking at and the re-resolve put the highlight back on the block above it
      const pinned = node.classList.contains("settings-block")
        ? pinnedHeadingHeight(node)
        : 0;
      if (node.getBoundingClientRect().top - rootTop - pinned <= 1) best = id;
    }
    return best ?? first;
  }

  function applySpySelection(best: string | null) {
    if (best === null || best === selection.value) return;
    selection.value = best;
    // the last place the USER was IS the anchor a reopen restores - a delivery a
    // geometry change armed resolves a new id at new boxes and must not write it
    if (!spyAnchorMayWrite(anchorTrace)) return;
    anchorTrace.wroteAt = anchorTrace.userScrollAt;
    persistAnchor(best);
  }

  /**
   * resolveSpyAtRest re-resolves the highlight from the boxes the pane RESTS.
   * It never writes the anchor: this is a programmatic jump, and the click already
   * persisted it.
   */
  function resolveSpyAtRest() {
    if (!bodyEl.value) return;
    applySpySelection(pickSpySection(spyBand()));
  }

  /**
   * settleSpyIfIdle re-resolves the highlight ONCE the pane has actually come to rest.
   */
  function settleSpyIfIdle() {
    if (spyLocked) return; // the lock's release calls us back
    if (!paneScrollSettled(paneScroll)) return; // the settle timer calls us back
    resolveSpyAtRest();
  }

  function syncSpy() {
    const root = bodyEl.value;
    spyObserver?.disconnect();
    spyObserver = null;
    // a re-arm is a GEOMETRY change, not a scroll: the boxes below are new
    anchorTrace.geometryAt = nowMs();
    observePaneBox(root);
    if (!root || typeof IntersectionObserver === "undefined") return;
    collectSpyTargets(root);
    spyObserver = new IntersectionObserver(
      () => {
        if (spyLocked) return;
        // a scroll the PANE issued is still producing events.
        if (!paneScrollSettled(paneScroll)) return;
        // the delivery is the TRIGGER only: resolve from the measured band
        applySpySelection(pickSpySection(spyBand()));
      },
      { root, rootMargin: "0px 0px -80% 0px", threshold: 0 },
    );
    for (const node of spyNodes.values()) spyObserver.observe(node);
  }

  // survives as an ANCHOR: on open the pane restores the scroll to the remembered. A
  // remembered id that no longer exists (module unmounted, an agent tab without that
  // category) opens at the top - no selection is invented, and a DESELECTED pane stored
  // no anchor at all, so it never resurrects one.
  let restored = false;
  function restoreAnchor() {
    if (restored || typeof window === "undefined") return;
    restored = true;
    // part 3: an in-app deep link (`?settings=<section>`) is the EXPLICIT door.
    // Unlike the anchor (a spy highlight, never clamped), a URL section IS a selection,
    // so it clamps too.
    const fromUrl = urlSectionSelection();
    if (fromUrl) {
      selection.value = fromUrl;
      clampId.value = parseSelection(fromUrl).category;
      persistAnchor(fromUrl);
      lockSpy(); // a restore is not a scroll: hold it until real input
      void nextTick(() => scrollToAnchor(fromUrl));
      return;
    }
    let stored: string | null = null;
    try {
      stored = localStorage.getItem(NAV_STORAGE_KEY);
    } catch {
      return;
    }
    const id = anchorSelection(nav.value, stored);
    if (!id) return;
    selection.value = id;
    lockSpy(); // a restore is not a scroll: hold it until real input
    void nextTick(() => scrollToAnchor(id));
  }

  // the URL carries the section --------------- The pane keeps ONE thing in the URL:
  // the section it is filtered to, on the QUERY axis (`?settings=<section>`), because
  // the PATH already owns the focused chat session and a settings path is the
  // settings-path card's lane - one scheme, two axes, replaceState only (the app never
  // grows the history stack). The write follows `clampId` - a REAL selection - and
  // never the spy's highlight: a scroll must not rewrite the URL that a reload then
  // lands.

  /**
   * urlSectionSelection resolves the section a deep link names against the LIVE nav.
   */
  function urlSectionSelection(): string | null {
    const id = parseSettingsSection(window.location.href);
    if (!id) return null;
    const resolved = resolveSelection(nav.value, id);
    if (!resolved) return null;
    return selectionId(resolved.category.id, resolved.subgroup?.id ?? null);
  }

  function writeUrlSection(section: string | null) {
    try {
      history.replaceState(
        {},
        "",
        withSettingsSection(window.location.href, section),
      );
    } catch {
      // a sandboxed host can refuse replaceState.
    }
  }

  watch(clampId, (section) => writeUrlSection(section));

  // a scope that cannot show the selection (an agent tab carries no Commonly Used). The
  // clamp goes with it: a section that is not in this pane's document cannot bound its
  // scroll. Reading `selection` alone resolved, cleared nothing, and left the filter
  // standing on a category the live nav cannot render - the whole document emptied ("no
  // settings here") and the URL kept a section the pane was not showing.
  watch(nav, (next) => {
    if (!selectionLost(next, [selection.value, clampId.value])) return;
    selection.value = null;
    clampId.value = null;
    lockSpy();
  });

  watch(docSections, () => {
    void nextTick(() => {
      syncSpy();
      observeClamp();
      applyClamp();
    });
  });
  watch(searching, (now, was) => {
    if (now === was) return;
    lockSpy(400);
    if (now) return;
    void nextTick(() => {
      const node = bodyEl.value;
      if (node) {
        // when the query clears back into a SECTION FILTER the returning document is.
        const top = filterSection.value === null ? lastDocScrollTop : 0;
        notePaneScroll(node, top, true);
        node.scrollTop = top;
      }
      syncSpy();
    });
  });

  const emptyText = computed(() => {
    if (query.value.trim() !== "") return `no settings match "${query.value.trim()}"`;
    if (modifiedOnly.value) return "no modified settings";
    return "no settings here";
  });

  // resetKnob: writes the knob default down the control's OWN set path. A no-default
  // knob has no button, but guard anyway: the reset never invents a value.: on an agent
  // tab the reset target is MAIN, and the verb is the wire's `unset` (delete the key),
  // never a write of main's value - that would pin a phantom override and, on
  // provider.api_key, persist the read mask as the agent's secret.
  function resetKnob(group: SettingGroup, knob: SettingKnob) {
    if (activeScope.value !== null) {
      const state = activeState.value;
      if (!state) return;
      const main = mainValues.value[knob.key];
      state.draft = { ...state.draft, [knob.key]: main === undefined ? "" : main };
      if (
        scopeOverriddenKeys.value.has(knob.key) &&
        !state.unset.includes(knob.key)
      )
        state.unset = [...state.unset, knob.key];
      return;
    }
    if (!knobHasDefault(knob.default)) return;
    const def = knob.default;
    if (isClient(group)) {
      if (knob.type === "toggle") setToggle(group, knob, def === true);
      else setSelect(group, knob, String(def));
      return;
    }
    writeDraft(knob.key, def);
  }

  const serverKnobDirty = (knob: SettingKnob): boolean => {
    if (activeScope.value !== null) {
      const state = activeState.value;
      if (!state) return false;
      return (
        state.unset.includes(knob.key) ||
        !valuesEqual(state.draft[knob.key], state.loaded[knob.key])
      );
    }
    return !valuesEqual(draft.value[knob.key], loaded.value[knob.key]);
  };

  // the agent tab's pending save: ONE derivation (the pure helper the
  // save ships) so the button's count can never drift from the wire.
  const scopePending = computed(() => {
    const state = activeState.value;
    if (!state)
      return { body: {} as Record<string, unknown>, invalid: undefined as string | undefined, count: 0 };
    const diff = scopeConfigDiff(
      state.loaded,
      state.draft,
      state.unset,
      scopeOverriddenKeys.value,
    );
    const unset = (diff.body.unset as string[] | undefined) ?? [];
    const count = Object.keys(diff.body).filter((k) => k !== "unset").length + unset.length;
    return { ...diff, count };
  });

  const dirtyCount = computed(() => {
    if (activeScope.value !== null) return scopePending.value.count;
    return Object.keys(settingsDiff(loaded.value, draft.value)).length;
  });

  function resetDraft() {
    saveError.value = null;
    if (activeScope.value !== null) {
      const state = activeState.value;
      if (state) {
        state.draft = { ...state.loaded };
        state.unset = [];
      }
      return;
    }
    draft.value = { ...loaded.value };
  }

  async function save() {
    if (activeScope.value !== null) {
      await saveScope(activeScope.value);
      return;
    }
    const changes = settingsDiff(loaded.value, draft.value);
    if (Object.keys(changes).length === 0) return;
    saving.value = true;
    saveError.value = null;
    try {
      applyPayload(await saveSettings(changes));
    } catch (err) {
      saveError.value = err instanceof Error ? err.message : String(err);
    } finally {
      saving.value = false;
    }
  }

  // saveScope ships one agent tab through the EXISTING agents edit endpoint (R4: one
  // write path, one call per save - the webui never learns or cares which lane the
  // server walks). The endpoint's answer IS the error mapping: any refusal (409 locked
  // default / a layer that will not parse, 400 a bad value) pins its own words on the
  // scope and renders the tab read-only - never a silent edit that cannot land.
  async function saveScope(name: string) {
    const diff = scopePending.value;
    if (diff.invalid) {
      saveError.value = diff.invalid;
      return;
    }
    if (diff.count === 0) return;
    saving.value = true;
    saveError.value = null;
    try {
      await updateAgentConfig(name, diff.body);
    } catch (err) {
      scopeLocks[name] = err instanceof Error ? err.message : String(err);
      saving.value = false;
      return;
    }
    // the save landed: adopt the draft as the local truth FIRST, so the markers clear
    // even when the refresh below cannot reach the host - and no queued removal
    // survives a save that already deleted the key (a reset whose value happened to
    // equal main's would otherwise stay queued forever)
    const state = scopeStates[name];
    if (state) {
      state.loaded = { ...state.draft };
      state.unset = [];
    }
    try {
      applyScopes(await fetchSettingsScopes());
    } catch (err) {
      saveError.value = err instanceof Error ? err.message : String(err);
    } finally {
      saving.value = false;
    }
  }


  const configMenuEl = ref<HTMLElement | null>(null);
  const configMenuOpen = ref(false);
  /** the reset action's arm: the FIRST click asks, the second applies (the. */
  const resetArmed = ref(false);
  const configBusy = ref(false);

  const resetHint = computed(() =>
    resetArmed.value
      ? "click again to restore every knob that has a default"
      : "restore every knob that has a default; the api key, bind addresses and named instances are kept",
  );

  // The chip and all three actions act on the MAIN config.yaml. An agent
  // scope tab edits its OWN layer file through the agent editor, so a
  // menu opened from that tab must say which document it is about -
  // otherwise the reset/edit visibly does nothing to the tab on screen.
  const configTargetLabel = computed(() =>
    activeScope.value
      ? `main config.yaml (not the ${activeScope.value} scope)`
      : "main config.yaml",
  );
  const configChipHint = computed(() => `config file actions - ${configTargetLabel.value}`);

  function toggleConfigMenu() {
    configMenuOpen.value = !configMenuOpen.value;
    resetArmed.value = false;
  }

  function closeConfigMenu() {
    configMenuOpen.value = false;
    resetArmed.value = false;
  }

  /** onDocumentClick closes the menu on any click outside it. */
  function onDocumentClick(event: MouseEvent) {
    if (!configMenuOpen.value) return;
    const el = configMenuEl.value;
    if (el && event.target instanceof Node && el.contains(event.target)) return;
    closeConfigMenu();
  }

  /**
   * onResetToDefaults restores the host's defaults.
   * The client ships NO values: the defaults are the host's (the same `default` field
   * the per-knob reset already compares against), so a reset can never drift from them.
   */
  async function onResetToDefaults() {
    if (!resetArmed.value) {
      resetArmed.value = true;
      return;
    }
    resetArmed.value = false;
    configBusy.value = true;
    saveError.value = null;
    try {
      applyPayload(await resetSettings());
      closeConfigMenu();
    } catch (err) {
      saveError.value = err instanceof Error ? err.message : String(err);
    } finally {
      configBusy.value = false;
    }
  }

  /** onExportYaml downloads the config document. */
  async function onExportYaml() {
    closeConfigMenu();
    configBusy.value = true;
    saveError.value = null;
    try {
      downloadText(configExportFilename(), await fetchConfigYaml());
    } catch (err) {
      saveError.value = err instanceof Error ? err.message : String(err);
    } finally {
      configBusy.value = false;
    }
  }

  /** downloadText hands text to the browser as a file download. */
  function downloadText(name: string, text: string) {
    const url = URL.createObjectURL(new Blob([text], { type: "application/yaml" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    a.style.display = "none";
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }


  const yamlDialogEl = ref<HTMLDialogElement | null>(null);
  const yamlOpen = ref(false);
  const yamlText = ref("");
  const yamlLoaded = ref("");
  const yamlSaving = ref(false);
  const yamlError = ref<string | null>(null);
  const yamlDirty = computed(() => yamlText.value !== yamlLoaded.value);

  /** onEditYaml opens the document editor on the file's own text. */
  async function onEditYaml() {
    closeConfigMenu();
    saveError.value = null;
    yamlError.value = null;
    configBusy.value = true;
    try {
      const text = await fetchConfigYaml();
      yamlText.value = text;
      yamlLoaded.value = text;
    } catch (err) {
      saveError.value = err instanceof Error ? err.message : String(err);
      return;
    } finally {
      configBusy.value = false;
    }
    yamlOpen.value = true;
    await nextTick();
    yamlDialogEl.value?.showModal();
  }

  function closeYamlEditor() {
    yamlDialogEl.value?.close();
    yamlOpen.value = false;
    yamlError.value = null;
  }

  /**
   * saveYaml writes the document through the server's config path, then re-reads it.
   */
  async function saveYaml() {
    if (yamlSaving.value || !yamlDirty.value) return;
    yamlSaving.value = true;
    yamlError.value = null;
    try {
      applyPayload(await saveConfigYaml(yamlText.value));
      const persisted = await fetchConfigYaml();
      yamlText.value = persisted;
      yamlLoaded.value = persisted;
    } catch (err) {
      yamlError.value = err instanceof Error ? err.message : String(err);
    } finally {
      yamlSaving.value = false;
    }
  }
</script>

<style scoped>
  /*
   * (R6): the scope strip - the pane's own tab row at the head, [Main | agent names.].
   */
  .settings-scopes {
    flex: none;
    display: flex;
    align-items: stretch;
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none;
    border-bottom: 1px solid var(--stroke);
  }
  .settings-scopes::-webkit-scrollbar { display: none; }
  .settings-scope-tab {
    flex: none;
    max-width: 160px;
    padding: 5px 10px;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    font: inherit;
    font-size: 12px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: pointer;
  }
  .settings-scope-tab:hover { color: var(--text); }
  .settings-scope-tab.active {
    color: var(--text);
    background: var(--z1);
    box-shadow: inset 0 2px 0 var(--accent);
  }
  .settings-scope-tab:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: -1px;
  }
  /*
   * a read-only scope.
   */
  .settings-readonly {
    opacity: 0.7;
  }
  .settings-readonly :deep(input),
  .settings-readonly :deep(textarea),
  .settings-readonly :deep(.setting-select) {
    pointer-events: none;
  }

  /*
   * the filter left the shared 128px band seat and became the pane-top full-width bar.
   * The bar is a flex child ABOVE.settings-body, so it never scrolls.: the band above
   * it is GONE, so the bar is now the pane's FIRST row and carries the top seat the
   * band used to supply - 14px, the band's own lead-in (TabBand's margin-top), NOT its
   * 22px title row.
   */
  .settings-filterbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 14px 10px 6px;
    border-bottom: 1px solid var(--stroke);
  }
  .settings-filterbar > :first-child {
    flex: 1;
    min-width: 0;
  }
  /*
   * narrow only: the jump-links drawer's toggle.
   * The document never leaves the screen - the nav arrives over it.
   */
  .settings-jump {
    flex: none;
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    background: var(--z0);
    color: var(--text-dim);
    font: inherit;
    font-size: 11px;
    white-space: nowrap;
    cursor: pointer;
  }
  .settings-jump:hover,
  .settings-jump[aria-expanded="true"] {
    color: var(--text);
    border-color: var(--accent);
  }
  .settings-jump-icon {
    transform: rotate(90deg);
  }
  /*
   * the config-file chip and its menu.
   * The chip wears the band's own chip shape (the settings-jump voice); the menu rides
   * under it - the.dock-menu geometry, this pane's tokens - so the three actions never
   * push the filter row wider than the column.
   */
  .settings-config {
    flex: none;
    position: relative;
  }
  .settings-config-btn {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 2px 6px;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    background: var(--z0);
    color: var(--text-dim);
    font: inherit;
    font-size: 10.5px;
    white-space: nowrap;
    cursor: pointer;
  }
  .settings-config-btn:hover,
  .settings-config-btn[aria-expanded="true"] {
    color: var(--text);
    border-color: var(--accent);
  }
  .settings-config-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .settings-config-caret {
    transform: rotate(0deg);
  }
  .settings-config-btn[aria-expanded="true"] .settings-config-caret {
    transform: rotate(180deg);
  }
  .settings-config-menu {
    position: absolute;
    top: calc(100% + 4px);
    right: 0;
    z-index: 40;
    min-width: 150px;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    background: var(--z1-raised);
    border: 1px solid var(--stroke);
    border-radius: 6px;
    box-shadow: var(--shadow-1);
  }
  /*
   * the target line: the chip always acts on the MAIN config.yaml, so the menu names.
   */
  .settings-config-target {
    margin: 0;
    padding: 2px 8px 4px;
    border-bottom: 1px solid var(--stroke);
    color: var(--text-dim);
    font-size: 10px;
    white-space: nowrap;
  }
  .settings-config-items {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .settings-config-item {
    display: flex;
    align-items: center;
    padding: 6px 8px;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 11.5px;
    text-align: left;
    white-space: nowrap;
    cursor: pointer;
  }
  .settings-config-item:hover {
    background: var(--z2);
  }
  .settings-config-item:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .settings-yaml {
    width: min(760px, 92vw);
    max-height: 86vh;
    padding: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--stroke);
    border-radius: 8px;
    background: var(--z1-raised);
    color: var(--text);
    box-shadow: var(--shadow-1);
  }
  .settings-yaml:not([open]) {
    display: none;
  }
  .settings-yaml::backdrop {
    background: rgba(0, 0, 0, 0.5);
  }
  .settings-yaml-head {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    border-bottom: 1px solid var(--stroke);
  }
  .settings-yaml-title {
    flex: 1;
    font-family: var(--mono);
    font-size: 12px;
  }
  .settings-yaml-close {
    flex: none;
    padding: 2px 6px;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    background: var(--z0);
    color: var(--text-dim);
    font: inherit;
    font-size: 10.5px;
    cursor: pointer;
  }
  .settings-yaml-close:hover {
    color: var(--text);
  }
  .settings-yaml-editor {
    flex: 1;
    min-height: 320px;
    margin: 8px 10px;
    padding: 8px;
    border: 1px solid var(--stroke);
    border-radius: 6px;
    background: var(--z0);
    color: var(--text);
    font-family: var(--mono);
    font-size: 12px;
    line-height: 1.45;
    tab-size: 2;
    resize: vertical;
  }
  .settings-yaml-editor:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: -1px;
  }
  .settings-yaml-error {
    flex: none;
    margin: 0 10px 6px;
    color: var(--err, var(--accent));
    font-size: 11px;
  }
  .settings-yaml-actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 10px 10px;
  }
  /*
   * the pane's.settings-save/.settings-revert are flex:1 (they fill the save row);
   * inside the dialog's action row they must keep their own width instead of stretching
   * across it / /
   */
  .settings-yaml-actions > button {
    flex: none;
    min-width: 88px;
  }
  .settings-yaml-hint {
    margin-left: auto;
    color: var(--text-dim);
    font-size: 10.5px;
  }
  /*
   * the knob select is the shared BandSelect chip - inline (the composer's listbox
   * pattern), never wider than its row / /
   */
  .setting-select {
    max-width: 100%;
  }

  /*
   * the modified marker cluster.
   * The dot is the VS Code "differs from the default" state (persistent, survives
   * reload); the star (main.css.setting-dirty) is the unsaved draft edit - two
   * different meanings, so they never share a shape: accent filled dot vs. accent
   * asterisk.
   */
  .setting-badges {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: none;
  }
  .setting-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    display: inline-block;
  }
  .setting-reset {
    background: none;
    border: 0;
    padding: 0;
    font: inherit;
    font-size: 11px;
    line-height: 1.2;
    color: var(--text-dim);
    cursor: pointer;
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .setting-reset:hover,
  .setting-reset:focus-visible {
    color: var(--accent);
  }
  /*
   * the toggle row keeps its full-row click target on the label; the badges sit outside
   * it so a reset click never flips the checkbox / /
   */
  .setting-toggle {
    cursor: default;
    gap: 8px;
  }
  .setting-toggle-main {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: 1;
    min-width: 0;
    cursor: pointer;
  }

  /* the one-document bones --- LEFT is the JUMP-LINK nav, RIGHT is the single. */
  .settings-split {
    flex: 1;
    min-height: 0;
    display: flex;
  }
  .settings-nav {
    flex: none;
    width: 200px;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 4px 0 6px;
    border-right: 1px solid var(--stroke);
  }
  /*
   * the category row: the disclosure chevron and the category button share one line.
   * TWO hit targets, never one - the chevron toggles the children, the label still
   * selects, so a parent can be picked without being expanded.
   */
  .settings-nav-row {
    display: flex;
    align-items: center;
  }
  /* a SURFACE GROUP header in the nav. */
  .settings-nav-surface {
    margin: 10px 0 2px;
    padding: 6px 10px 2px;
    border-top: 1px solid var(--stroke);
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--text-dim);
  }
  .settings-nav-chev {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    padding: 4px 0;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    cursor: pointer;
  }
  .settings-nav-chev:hover {
    color: var(--text);
  }
  .settings-nav-chev[aria-expanded="false"] .settings-nav-chevron {
    transform: rotate(-90deg);
  }
  .settings-nav-chev-gap {
    flex: none;
    width: 20px;
  }
  .settings-nav-row .settings-nav-cat {
    flex: 1;
    min-width: 0;
  }
  .settings-nav-cat,
  .settings-nav-sub {
    display: flex;
    align-items: baseline;
    gap: 6px;
    width: 100%;
    padding: 4px 10px;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    font: inherit;
    font-size: 12px;
    text-align: left;
    cursor: pointer;
  }
  /*
   * children are INDENTED under their parent and hidden while the parent is collapsed.
   */
  .settings-nav-sub {
    padding-left: 30px;
    font-size: 11.5px;
  }
  .settings-nav-cat:hover,
  .settings-nav-sub:hover,
  .settings-nav-cat.active,
  .settings-nav-sub.active {
    color: var(--text);
    background: var(--z1-raised);
  }
  .settings-nav-cat.active,
  .settings-nav-sub.active {
    box-shadow: inset 2px 0 0 var(--accent);
  }
  /* a live query GREYS the categories with no hit. */
  .settings-nav-cat.dim,
  .settings-nav-sub.dim {
    opacity: 0.38;
  }
  .settings-nav-label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .settings-nav-count {
    flex: none;
    font-family: var(--mono);
    font-size: 10px;
  }
  .settings-nav-cat:focus-visible,
  .settings-nav-sub:focus-visible,
  .settings-nav-chev:focus-visible,
  .settings-modified:focus-visible,
  .settings-jump:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: -1px;
  }

  .settings-content {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .settings-content-head {
    flex: none;
  }
  .settings-knob-count {
    margin-left: 6px;
    font-family: var(--mono);
    font-size: 10px;
    font-weight: 400;
    color: var(--text-dim);
  }
  .settings-modified {
    flex: none;
    border: 1px solid var(--stroke);
    border-radius: var(--r-chip);
    background: var(--z0);
    color: var(--text-dim);
    font: inherit;
    font-size: 10.5px;
    padding: 2px 6px;
    cursor: pointer;
  }
  .settings-modified:hover {
    color: var(--text);
    border-color: var(--accent);
  }
  .settings-modified.on {
    color: var(--accent);
    border-color: var(--accent);
  }
  /*
   * a category heading inside the ONE document.
   * Two traps this rule must not fall into: - a sticky box pins its BORDER box at top:
   * 0, so the old `margin: 14px 0 2px` left a sliver ABOVE the pinned heading through
   * which rows scrolled.
   * - the heading must be OPAQUE or rows show through it.
   */
  .settings-section-title {
    position: sticky;
    top: 0;
    z-index: 1;
    margin: 0 0 2px;
    padding: 20px 0 6px;
    border-top: 1px solid var(--stroke);
    background: var(--z1);
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--text);
  }
  .settings-category:first-of-type .settings-section-title {
    margin-top: 0;
    border-top: 0;
    padding-top: 4px;
  }
  /*
   * a SURFACE GROUP header in the document - the band between "frontend" and "backend".
   */
  .settings-surface-title {
    margin: 16px 0 0;
    padding: 0;
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1.4px;
    color: var(--text-dim);
  }
  .settings-subgroup-title {
    margin: 8px 0 0;
    font-size: 10.5px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--text-dim);
  }

  /*
   * (WCAG 2.2 SC 2.4.11): the pane's ONE heading STICKS, so a scroll that lands a box.
   * The pinned block measures 42px on the lane (20px padding-top + 6px padding-bottom +
   * the 11px heading's 15px line box + the 1px rule), so 44px lands a target TWO pixels
   * clear of it instead of exactly under it.
   */
  .settings-body {
    scroll-padding-top: 44px;
  }

  /* narrow: the SAME document, the nav as a drawer over it - it jumps, then closes. */
  .settings {
    position: relative;
  }
  .settings-scrim {
    position: absolute;
    inset: 0;
    z-index: 4;
    background: rgba(0, 0, 0, 0.45);
  }
  .settings-nav.drawer {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    z-index: 5;
    width: 220px;
    max-width: 82%;
    background: var(--z1);
    border-right: 1px solid var(--stroke);
    box-shadow: 4px 0 12px rgba(0, 0, 0, 0.35);
  }
  .settings.narrow .settings-split {
    flex-direction: column;
  }
  .settings.narrow .settings-content {
    flex: 1;
  }
</style>