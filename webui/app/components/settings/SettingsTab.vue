<template>
    <!-- card t_1cfb080a (Endo, 2026-11-11) SUPERSEDES card t_308e6d9b FOR
         THIS PANE: the settings tab carries NO tab band. t_308e6d9b put the
         universal band here on purpose; a later pass removed it because the
         DOCK TAB already reads "Settings" - the in-pane band only repeated
         the name and spent 22px of row + 14px of lead on it. Do not add it
         back. The band's 14px lead was not lost with it: it is now the
         filter bar's own top padding (the bar is the pane's first row - see
         .settings-filterbar below). The other tabs keep the shared band
         (shell/ExtensionsTab.vue still composes it).
         card t_6bebff18 (Endo ruling 2026-11-10, option B): settings
         LEAVES the shared band-search roster (card t_529628d5, which now
         scopes to the four module tabs - cron/skills/tools/mcp) and the
         filter becomes a pane-top full-width bar, the VSCode settings
         shape. The filter's BEHAVIOR is untouched (filterSettingGroups
         on the loaded groups, v-model "query"); only its seat moved -
         one input, one query ref, no second filter. The dock tab bar
         still carries the chip and the tab lives pinned in the
         inspector (card t_49d0b308). The aside classes carry the mobile
         drawer shape (mirrors FilesTab); `narrow` is the width-driven
         adaptation class (a measured on the root: the inspector's
         ~300px column). -->
    <aside
      ref="rootEl"
      class="settings aside pane inspector-panel"
      :class="{ open: open, narrow: narrow }">

    <!-- card t_8b464090 (Endo, 2026-11-11) - the ONE-DOCUMENT law, which
         SUPERSEDES rulings 1 and 12 of the 2026-11-10 grill. The pane is a
         single scrollable document: EVERY category renders as a headed
         section, top to bottom, in nav order, and NOTHING is selected on
         open. The sidebar is not a selector any more - it is JUMP LINKS. A
         click scrolls the pane to that section (scrollIntoView, smooth
         unless prefers-reduced-motion asks otherwise) and marks it
         SELECTED; a second click on the selected entry DESELECTS: the
         highlight clears and the pane returns to the top. Scrolling moves
         the highlight (IntersectionObserver scroll-spy). Selection NEVER
         filters the document - it always shows every knob and its
         description.

         Expansion follows SELECTION (this REPLACES t_5656d52f's
         default-expanded + per-parent toggle): a category discloses its
         subsection jump links only while it, or one of its subsections, is
         selected - exactly one branch open, no remembered toggle set. A
         live query force-opens every parent that carries a hit, so hits
         stay reachable.

         Narrow panes are the SAME document with the nav as a drawer that
         jumps and closes - there is nothing to drill into, so the two-screen
         state is gone. -->
    <div class="settings-filterbar">
      <SearchInput
        v-model="query"
        placeholder="filter settings" />
      <!-- card t_a8063683 (Endo, 2026-11-11): the modified-only switch left
           the deleted heading bar and sits beside the other pane-wide
           control. It is a DOCUMENT-wide filter, so it belongs with the
           search, not with a heading. Same contract as before: a
           pressed-state chip that narrows the whole document. -->
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
        class="settings-jump"
        type="button"
        :aria-expanded="drawerOpen"
        aria-label="settings categories"
        @click="drawerOpen = !drawerOpen">
        <Icon class="settings-jump-icon" name="chevron-down" :size="12" />
        <span>categories</span>
      </button>
    </div>

    <div class="settings-split">
      <div
        v-if="narrow && drawerOpen"
        class="settings-scrim"
        @click="drawerOpen = false" />
      <nav
        v-if="!narrow || drawerOpen"
        class="settings-nav"
        :class="{ drawer: narrow }"
        aria-label="settings categories">
        <template v-for="cat in nav" :key="cat.id">
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
            <!-- a flat category keeps the chevron's column so labels align -->
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
              <span class="settings-nav-count">{{ cat.knobs.length }}</span>
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
      </nav>

      <section class="settings-content">
        <!-- card t_a8063683: the head keeps ONLY the scope strip. It is a
             flex child, so a payload with no second scope must render no
             head AT ALL - an empty wrapper would still hold a seat. -->
        <div v-if="scopeTabs.length > 1" class="settings-content-head">
    <!-- card t_04eb11d6 (settings tier 3b, R6/R8): the scope strip, the
         VSCode tab row at the head of the pane. R8 EMPTY WORLD: zero
         agent scopes in the payload renders NO strip at all, so the pane
         stays exactly the tier-2b shape; one scope or more materializes
         Main + one tab per agent name, and a tab switches the WHOLE pane
         (its groups, its search, the dot's base, the reset target). -->
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

    <!-- an agent scope whose layer file cannot be read/written: the
         endpoint refuses it too, so the pane renders the message and
         goes read-only (R4 - never a silent edit that cannot land) -->
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
      v-if="!loadError && totalCount === 0"
      class="settings-empty">
        {{ emptyText }}
      </p>

    <!--
      Two knob origins share one renderer:
      - the chat group is client state (props/emits, persisted under
        omega.theme-mode, panel prefs) - never sent to the server;
      - every other group is server config, edited in the draft map and
        saved through PUT /settings (diff-only).
      The document IS the content view (settingsNav.documentFor): EVERY
      category in nav order, each a headed section carrying its children
      as headed blocks. The selection never filters it; a live query and
      the modified-only switch filter it in place.
    -->
    <section
      v-for="section in docSections"
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
      <!-- the child heading: a multi-child category names its children;
           a flat list or a one-child domain would only repeat the title
           above it -->
      <h3 v-if="block.heading" class="settings-subgroup-title">{{ block.label }}</h3>
        <template v-for="knob in block.knobs" :key="knob.key">
        <!-- toggle (card t_c04c959e: the row is the label's wrapper now, so
             the modified dot + reset affordance can share the line
             without a click on them flipping the checkbox) -->
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
            <!-- the draft star keeps its pre-existing seat (the non-toggle
                 head below): toggle rows never carried it, and this card
                 does not move it (card t_c04c959e step 3) -->
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
            <!-- card t_c04c959e: two DIFFERENT markers coexist - the dot
                 means "differs from the knob default" (persistent state),
                 the star means "edited in the draft, not saved yet".
                 card t_04eb11d6: on an agent tab the dot's base is MAIN's
                 inherited value and reset removes the override, so the
                 labels name that base instead. -->
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
          <!-- select (card t_acad02ec, audit M1: the themed listbox - a
               native select popup is OS chrome over the pane) -->
          <BandSelect
            v-if="knob.type === 'select'"
            class="setting-select"
            :model-value="selectValue(owner(knob), knob)"
            :options="knobOptions(knob)"
            :label="knob.label"
            drop="down"
            @update:model-value="setSelect(owner(knob), knob, $event)" />
          <!-- model (card t_70c28128: the typed model picker - a
               BandSelect from the provider's live list, filtered by
               the knob's slot; free text only when the list has
               failed/filtered empty) -->
          <ModelField
            v-else-if="knob.type === 'model'"
            :knob="knob"
            :value="modelValue(knob)"
            @update="setServer(knob, $event)" />
          <!-- providers (card t_7dcd4280: the named instances list -
               one row per instance, name = the instance id, add and
               remove included; the value is a structural list, saved
               through the normal diff-only PUT) -->
          <ProvidersField
            v-else-if="knob.type === 'providers'"
            :knob="knob"
            :value="providersValue(knob)"
            @update="setServer(knob, $event)" />
          <!-- textarea -->
          <textarea
            v-else-if="knob.type === 'textarea'"
            class="setting-input setting-textarea"
            rows="3"
            :value="serverValue(knob)"
            @input="setServer(knob, ($event.target as HTMLTextAreaElement).value)" />
          <!-- text / secret / bindlist / number -->
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
          <!-- path + browse (t_a5f9d185): ONE row, the value rides the
               normal draft/save diff; the picker reuses the workspace
               chip's pop (WorkspaceSelect in browse-label mode). -->
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
    <!-- card t_210a0d19: the MODULES group is gone from settings - the
         module toggles live in the extensions tab now (the module
         lifecycle cockpit), and this pane carries config knobs only. -->
    </div>
    <!-- save row stays pinned below the scrollable body; a read-only
         scope (broken layer / a 409 the endpoint sent) has nothing to
         save, so the row stays out of the way -->
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
    fetchSettings,
    filterSettingGroups,
    knobDiffersFromDefault,
    knobHasDefault,
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
  // card t_70c28128: the typed model pickers ride the shared catalog
  // (one /models fetch per settings open) and the ModelField control
  import { refreshModelCatalog } from "~/composables/modelCatalog";
  import ModelField from "./ModelField.vue";
  // card t_7dcd4280: the named provider instances list editor
  import ProvidersField from "./ProvidersField.vue";
  // card t_8b464090: the pane is ONE DOCUMENT - the nav's nesting DATA,
  // the document builder, the anchor resolution and the grey-out set. The
  // master-detail selection algebra (fallbackSelection/effectiveSelection/
  // contentFor) is gone with the two-screen model.
  import {
    NAV_STORAGE_KEY,
    anchorSelection,
    buildNav,
    clampedRange,
    documentFor,
    expandedCategoryIds,
    matchingCategoryIds,
    parseSelection,
    resolveSelection,
    spyTargetIds,
    withSubgroups,
    type DocSection,
    type NavCategory,
    type NavSubgroup,
  } from "./settingsNav";
  import { useNarrow } from "~/composables/narrow";
  import { DEFAULT_TOOL_DISPLAY, type ToolDisplay } from "~/composables/toolDisplay";
  import { DEFAULT_THINKING_DISPLAY, type ThinkingDisplay } from "~/composables/thinkingDisplay";
  // card t_502b8e81: the iconset wrapper - the group collapser chevron
  import Icon from "../ui/Icon.vue";
  import WorkspaceSelect from "../chat/WorkspaceSelect.vue";
  // card t_d7fd3ad3 item 6: the dock group's client knob rides the shared
  // bottom-width mode ref (the applier in app.vue reads the same signal)
  import {
    DEFAULT_BOTTOM_WIDTH_MODE,
    setBottomWidthMode,
    useBottomWidthMode,
  } from "~/composables/bottomWidth";
  // card t_30137f67: the server.default_workspace value rides the same
  // shared-signal shape - this tab's reads keep it fresh, the chat host
  // reads it when a new session's draft is born
  import { DEFAULT_WORKSPACE_KNOB, setDefaultWorkspace } from "~/composables/defaultWorkspace";

  const props = defineProps<{
    showThinking: boolean;
    appearance: Appearance;
    toolDisplay: ToolDisplay;
    thinkingDisplay: ThinkingDisplay;
    colorMode: ColorMode;
    themeId: string;
    userThemes: UserTheme[];
    /** the mobile drawer's overlay state (desktop tabs never pass it
        as toggled; mirrors FilesTab's prop) */
    open?: boolean;
  }>();

  // width-driven adaptation (card t_49d0b308): the inspector column
  // measures ~240-320px - the class flips and the knobs stack (rules in
  // main.css, .settings.narrow)
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  const emit = defineEmits<{
    (e: "update:showThinking", value: boolean): void;
    (e: "update:appearance", value: Appearance): void;
    (e: "update:toolDisplay", value: ToolDisplay): void;
    (e: "update:thinkingDisplay", value: ThinkingDisplay): void;
    (e: "set-color-mode", mode: ColorMode): void;
    (e: "set-theme", id: string): void;
  }>();

  // --- server payload state ---

  const groups = ref<SettingGroup[]>([]);
  // loaded: values as the server handed them; draft: edited values.
  // Diff-only saves (settingsDiff) keep unrelated knobs untouched.
  const loaded = ref<Record<string, unknown>>({});
  const draft = ref<Record<string, unknown>>({});
  const loadError = ref<string | null>(null);
  const saveError = ref<string | null>(null);
  const saving = ref(false);

  // --- agent scopes (settings tier 3b) ---
  // The payload's scopes ARE the tab strip (R8: zero scopes = no strip).
  // activeScope null is the Main tab - the tier-2b pane untouched. Each
  // scope keeps its OWN loaded/draft/unset state, so switching tabs never
  // throws an unsaved edit away (and a main-tab save round-trip keeps an
  // untouched agent tab's draft, see applyPayload).
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
  // a 409 (or any refusal that means "this layer is not writable") locks
  // the scope read-only with the endpoint's own message (R4)
  const scopeLocks = reactive<Record<string, string>>({});

  const activeScopeData = computed<SettingsScope | null>(() =>
    activeScope.value === null
      ? null
      : scopes.value.find((s) => s.name === activeScope.value) ?? null,
  );
  const activeState = computed<ScopeState | null>(() =>
    activeScope.value === null ? null : scopeStates[activeScope.value] ?? null,
  );

  // the strip: Main first, then one tab per agent scope name
  const scopeTabs = computed<{ name: string | null; label: string }[]>(() => [
    { name: null, label: "Main" },
    ...scopes.value.map((s) => ({ name: s.name, label: s.name })),
  ]);

  // R7 per-scope search: each tab keeps its OWN query, so switching from
  // a broad Main search to an agent tab never lands on a stale "no
  // settings match" - and the filter itself already runs per tab, since
  // it filters that tab's group list (Main: the registry + client groups,
  // agent: the four overridable groups only - see allGroups).
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

  // the dot/reset voice names its BASE: main's inherited value on an
  // agent tab, the config default on the Main tab (R3/seam ruling)
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

  // Card 5: settings is a dock TAB, not an overlay - the tab opens once
  // and stays mounted, so the load-on-open watch dies with the drawer
  // shape. The tab mount loads; the save round-trip refreshes from the
  // server response the same way it did before.
  onMounted(() => {
    bindIntent(rootEl.value);
    void reload();
  });

  async function reload() {
    loadError.value = null;
    saveError.value = null;
    try {
      applyPayload(await fetchSettings());
      // card t_70c28128: the model pickers' list rides ONE request per
      // settings open - refreshed after the knob payload lands (the
      // fields exist only once the groups do; the composable shares
      // the single answer across every model field).
      refreshModelCatalog();
      // card t_8b464090: the document exists only once the payload does,
      // so the remembered anchor restores HERE - and the spy arms over the
      // rendered sections (a save round-trip re-arms it, never restores).
      void nextTick(() => {
        restoreAnchor();
        syncSpy();
      });
    } catch (err) {
      // a host without the settings seam (501) or an unreachable
      // server: keep the client-side settings usable, say why the
      // server list is missing
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
    // card t_30137f67: every read (mount reload + save round-trip)
    // keeps the shared signal fresh - a Settings edit applies to the
    // next new session's chip without a reload. "" clears it.
    setDefaultWorkspace(values[DEFAULT_WORKSPACE_KNOB]);
  }

  // applyScopes re-seeds the per-scope state from a fresh payload. A
  // scope whose file did NOT change keeps its draft + pending unset (a
  // main-tab save round-trip must not discard an agent tab's unsaved
  // edit); a changed file re-seeds, because the values just moved under
  // the draft. Scopes that vanished (agent removed) drop their state and
  // the tab falls back to Main.
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
      // a file that changed under us may have fixed what a 409 refused
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

  // --- client (chat) group: props in, emits out ---

  // --- theme picker: two knobs - color mode (built-in palettes) and
  // theme (the built-in default or a server-side user theme) ---

  // The theme knob's option list: "default" first, then every usable
  // theme file (option labels carry the yaml name, the value is the
  // file id). A broken theme (server-flagged error) never enters.
  const themeOptions = computed(() => themePickOptions(props.userThemes));

  // select options arrive as strings or {value,label}: one renderer each
  const optLabel = (opt: string | { value: string; label: string }) =>
    typeof opt === "string" ? opt : opt.label;
  const optValue = (opt: string | { value: string; label: string }) =>
    typeof opt === "string" ? opt : opt.value;

  // card t_acad02ec: the BandSelect listbox's options for a knob - the
  // same values the native select offered, rendered through the themed
  // component
  function knobOptions(knob: SettingKnob) {
    return (knob.options ?? []).map((opt) => ({
      value: optValue(opt),
      label: optLabel(opt),
    }));
  }

  // Card t_c04c959e: the client knobs carry their OWN defaults - no server
  // schema is involved, so the default lives beside the knob definition and
  // matches the value app.vue (or the shared composable) starts from. The
  // reset affordance writes it back through the same emit path the control
  // uses.
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

  // card t_d7fd3ad3 item 6: the dock's client knob - the bottom panel's
  // width mode. "aligned" (default) matches the panel to the main panes'
  // box (the VS Code panel read); "full" spans the dock area. Written
  // straight to the shared module ref (composables/bottomWidth.ts) - the
  // applier in app.vue re-measures on the same signal.
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

  // Client knobs write props/emits, server knobs write the draft map.
  // Card t_6bebff18: the Commonly Used group renders BOTH origins side
  // by side, so the routing is per KNOB (the key prefix names its
  // owner), never per group alone - a curated theme row read through
  // serverValue would render empty and swallow its edit into a
  // PUT /settings the server never accepts.
  const CLIENT_KNOB_PREFIXES = ["chat.", "dock."];
  const isClientKnob = (knob: SettingKnob) =>
    CLIENT_KNOB_PREFIXES.some((prefix) => knob.key.startsWith(prefix));
  const isClient = (group: SettingGroup, knob?: SettingKnob) =>
    group.key === "chat" ||
    group.key === "dock" ||
    (knob !== undefined && isClientKnob(knob));

  // t_a5f9d185: the path knob (default workspace) renders input + the
  // workspace browse chip; every other text knob keeps the plain input.
  const isPathKnob = (knob: SettingKnob) =>
    knob.key === "server.default_workspace";

  // --- the curated first group (card t_6bebff18) ---
  // Membership is DATA, one line to change: every model knob (the main
  // model plus the decision / vision / title / per-subsystem slots) and
  // the appearance pair (theme, colour mode). Each entry is picked BY
  // KEY out of the already-loaded registry - the SAME knob objects, no
  // clone and no second fetch - so an edit here writes the draft map /
  // emits the client event exactly as the origin row does.
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
    // a registry that carries none of the keys (an older server, a 501
    // seam) renders no empty section
    if (knobs.length === 0) return null;
    return { key: "common", label: "Commonly Used", knobs };
  });

  // --- the agent scope groups (settings tier 3b, R3) ---

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

  // R3: an agent tab renders the four overridable groups and nothing
  // else - the scope payload decides which groups/knobs exist.
  const SCOPE_GROUP_LABELS: Record<string, string> = {
    provider: "provider",
    system_prompt: "system prompt",
    description: "description",
    max_turns: "max turns",
  };
  // the control each scope knob renders through; the main registry's
  // knob answers the rest (labels, select options, number bounds)
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
      // the DIFF base on an agent tab is MAIN's value (the seam ruling):
      // the existing isModified test then means "differs from main"
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
    // R3: an agent tab's ONE source is the scope's four groups - no
    // Commonly Used, no chat/dock client state, no other registry group
    if (activeScope.value !== null) return scopeGroups.value;
    // card t_af7e1150: the curated group is ALWAYS in the list now - the
    // nav must keep its shape under a live query (ruling 6 greys, never
    // hides), so Commonly Used stays a category. The duplicate-row risk is
    // answered in documentFor: a live query SKIPS the curated section, so
    // a hit renders once, under the domain that owns it.
    return [
      ...(commonlyUsedGroup.value ? [commonlyUsedGroup.value] : []),
      chatGroup.value,
      dockGroup.value,
      ...groups.value,
    ];
  });

  // instanceRows was the model pickers' instance feed (card t_fc0b2f2d)
  // - card t_1914a8c9 moved the options to the grouped /models catalog
  // (ModelField reads it from composables/modelCatalog), so the pane
  // carries no rows plumbing anymore.

  // --- the jump-link nav (card t_8b464090, superseding t_af7e1150) ---

  // The knob's OWN domain, for the row plumbing below. A knob can be
  // rendered from the virtual Commonly Used category, but its diff base,
  // its client/server routing and its reset target belong to the domain
  // that DEFINED it - so the lookup is built from the real groups, never
  // from the nav (which mixes domains in the curated category).
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

  // The nav builds itself from the per-knob subgroup DATA (ruling 2):
  // withSubgroups stamps each knob, buildNav derives the two levels, and
  // an undeclared knob lands in the implicit "general" child (ruling 3).
  // Empty categories never enter (ruling 14: an unmounted module's domain
  // is absent from the payload, so it is absent from the nav - and from
  // the document below).
  const nav = computed<NavCategory[]>(() =>
    buildNav(withSubgroups(allGroups.value)),
  );

  // a domain whose knobs all land in ONE child shows no child jump link -
  // a nested copy of the parent's name is noise and there is nothing to
  // jump to.
  const navChildren = (cat: NavCategory): NavSubgroup[] =>
    cat.subgroups.length > 1 ? cat.subgroups : [];

  // --- the selection: jump links, not a selector (card t_8b464090) ---
  //
  // Selection is state the USER sets and clears - never a pure read of
  // the scroll. A pure read could not deselect: jumping back to the top
  // would make the observer instantly reselect whatever sits there, and
  // the deselect would look broken. A nav click sets it, a second click
  // on the same entry clears it, the observer moves it while scrolling.
  const selection = ref<string | null>(null);

  // the narrow drawer: the SAME jump links over the SAME document
  const drawerOpen = ref(false);

  // EXPANSION IS DERIVED FROM THE SELECTION. There is no collapsed set,
  // no per-parent toggle and nothing remembered (this supersedes
  // t_5656d52f's default-expanded model): exactly the branch the
  // selection lives in is open, every other branch shut - so "what is
  // open" and "where I am" can never disagree. A live query force-opens
  // every parent carrying a hit: hits stay reachable whatever is
  // selected.
  const expandedIds = computed<ReadonlySet<string>>(() =>
    expandedCategoryIds(
      selection.value,
      searching.value ? matchIds.value : null,
    ),
  );
  // the child jump links the nav renders for a category right now
  const openChildren = (cat: NavCategory): NavSubgroup[] =>
    expandedIds.value.has(cat.id) ? navChildren(cat) : [];

  // --- the scrollport, the jump, the anchor ---
  const bodyEl = ref<HTMLElement | null>(null);
  // id -> element for BOTH levels: a category section answers to its own
  // id, each block to "cat/sub". Rebuilt from the DOM by collectSpyTargets
  // whenever the document changes (a query, a scope), so a filtered-away
  // section can never leave a stale target behind.
  const spyNodes = new Map<string, HTMLElement>();
  // The observer starts SHUT: a fresh pane has nothing selected (card
  // t_8b464090) and the observer's first delivery would otherwise select
  // whatever heads the document. Real input is what opens it - the same
  // suppression a nav click and a deselect use.
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
      }, ms);
    }
  }
  function unlockSpy() {
    if (spyTimer !== undefined) clearTimeout(spyTimer);
    spyTimer = undefined;
    spyLocked = false;
  }

  function collectSpyTargets(root: HTMLElement) {
    spyNodes.clear();
    for (const node of root.querySelectorAll<HTMLElement>("[data-section]")) {
      const id = node.dataset.section;
      if (id) spyNodes.set(id, node);
    }
  }

  function reducedMotion(): boolean {
    return (
      typeof window !== "undefined" &&
      typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches
    );
  }

  /** The height of the heading a target sits UNDER (its own section's
      sticky heading). A block jump must clear it - the heading pins at the
      scrollport's top edge, so a block scrolled to exactly 0 would paint
      its first rows BEHIND it (card t_a8063683). */
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
      root.scrollTo({ top: 0, behavior });
      return;
    }
    // a SECTION lands with its own heading exactly at the top (the heading
    // pins there itself); a BLOCK lands just below that pinned heading, so
    // nothing it owns is hidden behind it.
    const isBlock = id !== null && node.dataset.section !== parseSelection(id).category;
    const rootTop = root.getBoundingClientRect().top;
    const delta = node.getBoundingClientRect().top - rootTop;
    const clear = isBlock ? pinnedHeadingHeight(node) : 0;
    root.scrollTo({ top: root.scrollTop + delta - clear, behavior });
  }

  function persistAnchor(id: string | null) {
    try {
      // DESELECTED persists as NO anchor: a reload must open at the top,
      // never resurrect a selection the user cleared
      if (id) localStorage.setItem(NAV_STORAGE_KEY, id);
      else localStorage.removeItem(NAV_STORAGE_KEY);
    } catch {
      // private mode / quota: the session still navigates, only the
      // memory is gone - never a broken click
    }
  }

  /** first click: select + expand + scroll. A second click on the SAME
      entry - or on the CATEGORY that currently HOLDS the scroll - deselects:
      one action in both directions. The second half matters for the clamp
      (card t_a6b794b5): the spy moves the highlight onto a block while the
      user scrolls INSIDE a selected category, and the promised "click
      again to release" must not depend on where the highlight landed. A
      subsection link still jumps (its parent holding the scroll is not a
      toggle-off). */
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
    // a REAL selection clamps the pane to its category (card t_a6b794b5);
    // the spy's highlight never does
    clampId.value = picked.category;
    persistAnchor(id);
    // the observer must not fight the smooth scroll it just started
    lockSpy(650);
    scrollToAnchor(id);
    if (narrow.value) drawerOpen.value = false;
  }

  function deselect() {
    selection.value = null;
    // the clamp releases in the SAME frame: free scroll returns before the
    // pane's return-to-top animation even starts
    clampId.value = null;
    persistAnchor(null);
    lockSpy(); // stays shut until the NEXT real scroll
    scrollToAnchor(null);
    if (narrow.value) drawerOpen.value = false;
  }

  // Any real input on the pane releases a deselect's suppression - that
  // IS the "next user scroll" the ruling names.
  const INTENT_EVENTS = ["wheel", "touchstart", "pointerdown", "keydown"];
  let intentTarget: HTMLElement | null = null;
  const noteUserIntent = () => unlockSpy();
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
    // the selected section bounds the pane (card t_a6b794b5)
    if (clampId.value) scheduleClamp();
  }

  // --- the SELECTED-SECTION scroll clamp (card t_a6b794b5) ----------------
  //
  // Endo, 2026-11-11: "when i select a category i should not be able to
  // scroll further than inside the category. until i deselected again and
  // nothing is selected." Option A: every section stays in the document
  // (selection never filters it); the clamp only bounds WHERE it may rest.
  //
  // Only a REAL user selection writes `clampId`. The spy writes `selection`
  // while the user scrolls - clamping on that would seize free scroll the
  // moment the user scrolled at all (the trap the card names). A RESTORED
  // anchor does not clamp either: the remembered id is written by the spy
  // too, so reopening the pane must never trap a scroll in a section the
  // user merely scrolled past.
  const clampId = ref<string | null>(null);
  let clampObserver: ResizeObserver | null = null;
  let clampFrame = 0;

  /** the section a selection clamps to - its CATEGORY section, which is
      the box carrying the sticky heading; a block jump inside it still
      lands clear of that heading (scrollToAnchor), so the clamp never
      undoes a subsection jump */
  function clampNode(id: string): HTMLElement | null {
    const root = bodyEl.value;
    if (!root) return null;
    const category = parseSelection(id).category;
    return (
      spyNodes.get(category) ??
      root.querySelector<HTMLElement>(`[data-section="${category}"]`)
    );
  }

  /** applyClamp holds scrollTop inside the selected section's range. The
      maths is the pure clampedRange (settingsNav.ts); the component only
      measures. The range is in DOCUMENT coordinates, so it does not move
      as the pane scrolls - only the pane's window does. */
  function applyClamp() {
    // a programmatic jump is intent, not the user's hand: the spy's
    // existing lock is the ONE suppression (step 5 - no second mechanism),
    // so the smooth scroll and a deselect's return-to-top are never fought
    if (spyLocked) return;
    const root = bodyEl.value;
    const id = clampId.value;
    if (!root || !id) return;
    // a section a live query or `modified only` filtered out of the
    // document has no range to hold: free scroll is the only truth left
    const node = clampNode(id);
    if (!node) return;
    const [min, max] = clampedRange({
      top: node.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop,
      height: node.offsetHeight,
      viewport: root.clientHeight,
      content: root.scrollHeight,
    });
    const now = root.scrollTop;
    const next = Math.min(Math.max(now, min), max);
    // COMPARE BEFORE ASSIGNING: a write fires another scroll event, and an
    // unguarded handler loops. Sub-pixel positions never settle exactly.
    if (Math.abs(next - now) < 1) return;
    root.scrollTop = next;
  }

  /** the clamp rides the frame AFTER the event: wheel and trackpad keep
      pushing for a frame or two after the snap, and clamping synchronously
      inside the scroll handler reads as the pane fighting the hand */
  function scheduleClamp() {
    if (clampFrame) return;
    clampFrame = requestAnimationFrame(() => {
      clampFrame = 0;
      applyClamp();
    });
  }

  /** observeClamp re-arms over the boxes the range is measured from. A
      section's height changes with NO scroll at all: a parent's jump links
      expand, `modified only` narrows the document, a scope tab swaps the
      knob set, a font or width reflow lands, the window resizes. Measuring
      whenever a box moves is what keeps the boundary from going stale (a
      stale max either locks the pane out of the section's bottom or lets
      it past - step 3). */
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
    void nextTick(() => {
      observeClamp();
      applyClamp();
    });
  });

  onBeforeUnmount(() => {
    unlockSpy();
    bindIntent(null);
    spyObserver?.disconnect();
    spyObserver = null;
    clampObserver?.disconnect();
    clampObserver = null;
    if (clampFrame) cancelAnimationFrame(clampFrame);
    clampFrame = 0;
  });

  // Ruling 8: the VSCode modified-only switch above the content list.
  const modifiedOnly = ref(false);


  // --- value plumbing ---

  // The pane reads ONE pair of maps: Main's loaded/draft on the Main tab,
  // the active scope's on an agent tab. Writing routes the same way, so
  // every control keeps its single set path (card t_6bebff18's per-knob
  // routing is untouched).
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
      // an edited key is no longer a pending override removal: the edit
      // is the new state (only an untouched reset stays pending)
      if (state.unset.includes(key))
        state.unset = state.unset.filter((k) => k !== key);
      return;
    }
    draft.value = { ...draft.value, [key]: value };
  }

  // clientValue reads the live chat/dock state: a knob the user edited
  // (dirty or not) always shows the client's own truth - never the
  // server draft map (card t_6bebff18's per-knob routing).
  const clientValue = (knob: SettingKnob): unknown => {
    if (knob.key === "chat.show_thinking") return props.showThinking;
    if (knob.key === "chat.appearance") return props.appearance;
    if (knob.key === "chat.tool_display") return props.toolDisplay;
    if (knob.key === "chat.thinking_display") return props.thinkingDisplay;
    if (knob.key === "dock.bottom_width") return bottomWidthMode.value;
    if (knob.key === "chat.color_mode") return props.colorMode;
    return props.themeId;
  };

  // serverValue reads draft-first: a knob the user edited (dirty or not)
  // always displays what will be saved - Main's maps on the Main tab, the
  // active scope's on an agent tab (card t_04eb11d6).
  const serverValue = (knob: SettingKnob): unknown =>
    paneDraft.value[knob.key] ?? paneLoaded.value[knob.key];

  // modelValue (card t_70c28128): the model pickers take strings -
  // a number-typed server value (never expected, tolerated) stringifies
  // instead of breaking the BandSelect generic.
  const modelValue = (knob: SettingKnob): string => {
    const v = serverValue(knob);
    return typeof v === "string" ? v : v == null ? "" : String(v);
  };

  // numberValue: number inputs must never render a half-state - an
  // empty draft shows "" so the field can be cleared; anything else is
  // whatever the last parse produced (see setServer).
  const numberValue = (knob: SettingKnob): unknown => {
    const v = serverValue(knob);
    return v === "" ? "" : v;
  };

  // providersValue (card t_7dcd4280, reshaped t_1914a8c9): the list
  // editor's rows - a structural value coerced to its named-endpoint
  // shape (no model column; tolerant: an unexpected kind degrades to
  // no rows, never a render crash).
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

  // knobValue is the value the row is SHOWING right now: draft-first for
  // server knobs, the live client state for the chat/dock groups.
  const knobValue = (group: SettingGroup, knob: SettingKnob): unknown =>
    isClient(group) ? clientValue(knob) : serverValue(knob);

  // isModified: the VS Code modified marker (card t_c04c959e) - the row's
  // current value differs from the knob's default. Distinct from the draft
  // star below: a row can be modified-but-saved (dot, no star) or
  // edited-back-to-default-then-unsaved-elsewhere (star, no dot). A knob the
  // server knows no default for (default: null - provider.model_name) never
  // shows the dot.
  //
  // card t_04eb11d6 (seam ruling): on an agent tab the SAME marker names a
  // different BASE - differs from MAIN, because main's inherited value is
  // that tab's default. The base is structural where the value cannot
  // answer (provider.api_key rides masked, so the override flag is the
  // only honest "this agent has its own").
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

  // canReset: Main - the tier-2b rule (a modified row resets to the config
  // default). Agent tab - reset is the OVERRIDE REMOVAL, so it exists
  // exactly while the row differs from main (a saved override to delete,
  // or a pending edit to undo).
  const canReset = (group: SettingGroup, knob: SettingKnob): boolean =>
    activeScope.value === null ? isModified(group, knob) : scopeDiffersFromMain(knob);

  // --- the content pane: search, hits, the rendered view (t_af7e1150) ---

  // The query's knob set comes from the EXISTING filter (tier 1's
  // filterSettingGroups: label + help substring), applied to this scope's
  // own group list - so ruling 7's per-scope search survives untouched.
  // null means NO query is live; an EMPTY set is a query that matched
  // nothing, and the two must stay distinguishable (everything vs. the
  // empty state).
  const searchHits = computed<ReadonlySet<string> | null>(() => {
    if (query.value.trim() === "") return null;
    const hits = new Set<string>();
    for (const group of filterSettingGroups(allGroups.value, query.value))
      for (const knob of group.knobs) hits.add(knob.key);
    return hits;
  });
  const searching = computed(() => searchHits.value !== null);

  // Ruling 8's predicate: the SAME modified test the rows carry, asked
  // through the knob's own domain (the dot and the switch can never
  // disagree about which knobs are modified).
  const knobModified = (knob: SettingKnob): boolean =>
    isModified(owner(knob), knob);

  // The document: EVERY category, in nav order, filtered IN PLACE by the
  // live query and the modified-only switch. This is the whole content
  // pane now - there is no per-selection view to swap in (card t_8b464090).
  const docSections = computed<DocSection[]>(() =>
    documentFor(nav.value, {
      hits: searchHits.value,
      modifiedOnly: modifiedOnly.value,
      isModified: knobModified,
    }),
  );
  const matchIds = computed<ReadonlySet<string>>(() =>
    matchingCategoryIds(nav.value, {
      hits: searchHits.value,
      modifiedOnly: modifiedOnly.value,
      isModified: knobModified,
    }),
  );

  // the visible knob total under the live query / modified-only switch -
  // the empty state's witness (card t_a8063683 deleted the heading bar
  // that also carried it; the section headings carry their own counts)
  const totalCount = computed(() =>
    docSections.value.reduce((n, section) => n + section.count, 0),
  );

  // --- the scroll-spy (the nav follows the document) ---
  const spyOrder = computed<string[]>(() => spyTargetIds(docSections.value));
  const inBand = new Set<string>();
  let spyObserver: IntersectionObserver | null = null;

  /** One thin band at the top of the scrollport: whichever heading sits
      nearest its top edge - a category or one of its blocks - is the one
      the pane is looking at. A generous band would let two neighbours
      fight over the highlight mid-scroll, which is exactly the flicker
      the root margin is chosen to avoid; the suppression window above is
      the other half (the observer never fights the click that started a
      smooth scroll, and a deselect stays put until the next real
      scroll). */
  function syncSpy() {
    const root = bodyEl.value;
    spyObserver?.disconnect();
    spyObserver = null;
    inBand.clear();
    if (!root || typeof IntersectionObserver === "undefined") return;
    collectSpyTargets(root);
    spyObserver = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const id = (entry.target as HTMLElement).dataset.section ?? "";
          if (!id) continue;
          if (entry.isIntersecting) inBand.add(id);
          else inBand.delete(id);
        }
        if (spyLocked) return;
        const rootTop = root.getBoundingClientRect().top;
        // card t_a8063683: the section headings are STICKY now, so several
        // of them can sit at the SAME top edge. "Nearest the top" would
        // pick a neighbour arbitrarily there; the honest signal is the
        // LAST heading in document order that has reached the scrollport's
        // top - that is the one currently PINNED at it, and its predecessor
        // has already been pushed out. `first` is the fallback for a pane
        // whose targets are all still below the edge.
        let best: string | null = null;
        let first: string | null = null;
        for (const id of spyOrder.value) {
          if (!inBand.has(id)) continue;
          const node = spyNodes.get(id);
          if (!node) continue;
          if (first === null) first = id;
          if (node.getBoundingClientRect().top - rootTop <= 1) best = id;
        }
        if (best === null) best = first;
        if (best === null || best === selection.value) return;
        selection.value = best;
        // the last place the user was IS the anchor a reopen restores
        persistAnchor(best);
      },
      { root, rootMargin: "0px 0px -80% 0px", threshold: 0 },
    );
    for (const node of spyNodes.values()) spyObserver.observe(node);
  }

  // Ruling 13 survives as an ANCHOR: on open the pane restores the scroll
  // to the remembered section and marks it. A remembered id that no
  // longer exists (module unmounted, an agent tab without that category)
  // opens at the top - no selection is invented, and a DESELECTED pane
  // stored no anchor at all, so it never resurrects one.
  let restored = false;
  function restoreAnchor() {
    if (restored || typeof window === "undefined") return;
    restored = true;
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

  // a scope that cannot show the selection (an agent tab carries no
  // Commonly Used) clears the HIGHLIGHT only - the stored anchor stays
  // raw for the next open. The clamp goes with it: a section that is not
  // in this pane's document cannot bound its scroll.
  watch(nav, (next) => {
    const id = selection.value;
    if (id && !resolveSelection(next, id)) {
      selection.value = null;
      clampId.value = null;
      lockSpy();
    }
  });

  // the document's targets move with the query/scope: re-arm the spy and
  // give a cleared query back the scroll position it interrupted. The
  // clamp's boxes moved with them, so re-observe and re-apply (card
  // t_a6b794b5 step 3: a box change is not always a scroll).
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
      if (node) node.scrollTop = lastDocScrollTop;
      syncSpy();
    });
  });

  // the empty state names the reason it is empty (a live query, the
  // modified-only switch, or a payload with no knobs at all)
  const emptyText = computed(() => {
    if (query.value.trim() !== "") return `no settings match "${query.value.trim()}"`;
    if (modifiedOnly.value) return "no modified settings";
    return "no settings here";
  });

  // resetKnob (card t_c04c959e): writes the knob default down the control's
  // OWN set path - the draft map for server knobs (so the normal diff-only
  // save ships it, and the star clears when the default is what was loaded)
  // and the client emit/ref for the chat and dock groups. A no-default knob
  // has no button, but guard anyway: the reset never invents a value.
  //
  // card t_04eb11d6: on an agent tab the reset target is MAIN, and the
  // verb is the wire's `unset` (delete the key), never a write of main's
  // value - that would pin a phantom override and, on provider.api_key,
  // persist the read mask as the agent's secret (card t_53523d5b).
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

  // saveScope ships one agent tab through the EXISTING agents edit
  // endpoint (R4: one write path, one call per save - the webui never
  // learns or cares which lane the server walks). The endpoint's answer
  // IS the error mapping: any refusal (409 locked default / a layer that
  // will not parse, 400 a bad value) pins its own words on the scope and
  // renders the tab read-only - never a silent edit that cannot land.
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
    // the save landed: adopt the draft as the local truth FIRST, so the
    // markers clear even when the refresh below cannot reach the host -
    // and no queued removal survives a save that already deleted the key
    // (a reset whose value happened to equal main's would otherwise stay
    // queued forever)
    const state = scopeStates[name];
    if (state) {
      state.loaded = { ...state.draft };
      state.unset = [];
    }
    try {
      applyScopes((await fetchSettings()).scopes);
    } catch (err) {
      saveError.value = err instanceof Error ? err.message : String(err);
    } finally {
      saving.value = false;
    }
  }
</script>

<style scoped>
  /* card t_04eb11d6 (R6): the scope strip - the pane's own tab row at the
     head, [Main | agent names...]. It scrolls sideways when the roster is
     long (the .dock-tabs shape) and the active tab carries the accent's
     inset top bar - the dock tab language at settings size. The strip
     only exists when the payload carries scopes (R8 empty world renders
     no strip at all; v-if lives in the template). */
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
  /* a read-only scope (a layer file that will not parse, or any refusal
     the endpoint sent): the values stay visible behind the message, but
     nothing can be edited and no save row exists (R4) */
  .settings-readonly {
    opacity: 0.7;
  }
  .settings-readonly :deep(input),
  .settings-readonly :deep(textarea),
  .settings-readonly :deep(.setting-select) {
    pointer-events: none;
  }

  /* card t_6bebff18 (Endo ruling 2026-11-10): the filter left the shared
     128px band seat and became the pane-top full-width bar - the VSCode
     settings-search shape. Placement only: the input's look stays the ONE
     shared .search-input sheet (main.css), no second copy here. The bar is
     a flex child ABOVE .settings-body, so it never scrolls.
     card t_1cfb080a (Endo, 2026-11-11): the band above it is GONE, so the
     bar is now the pane's FIRST row and carries the top seat the band used
     to supply - 14px, the band's own lead-in (TabBand's margin-top), NOT
     its 22px title row. The sides stay 10px, matching the body's inset, so
     the pane's horizontal geometry is unchanged. */
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
  /* narrow only: the jump-links drawer's toggle (card t_8b464090). The
     document never leaves the screen - the nav arrives over it. */
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
  /* the knob select is the shared BandSelect chip - inline (the
     composer's listbox pattern), never wider than its row */
  .setting-select {
    max-width: 100%;
  }

  /* card t_c04c959e: the modified marker cluster. The dot is the VS Code
     "differs from the default" state (persistent, survives reload); the
     star (main.css .setting-dirty) is the unsaved draft edit - two
     different meanings, so they never share a shape: accent filled dot
     vs. accent asterisk. The reset button exists only on modified rows. */
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
  /* the toggle row keeps its full-row click target on the label; the
     badges sit outside it so a reset click never flips the checkbox */
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

  /* --- card t_8b464090: the one-document bones ---
     LEFT is the JUMP-LINK nav, RIGHT is the single scrolling document of
     every category (ruling 11 survives only in placement: the scope strip
     lives in the content head, the nav stays categories). The rows inside
     keep their tier-2b styles untouched (ruling 10). */
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
  /* the category row: the disclosure chevron and the category button share
     one line (card t_5656d52f). TWO hit targets, never one - the chevron
     toggles the children, the label still selects, so a parent can be
     picked without being expanded. */
  .settings-nav-row {
    display: flex;
    align-items: center;
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
  /* collapsed points right (the iconset's down glyph, rotated); a flat
     category keeps the same column so every label starts on one x */
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
  /* card t_5656d52f: children are INDENTED under their parent and hidden
     while the parent is collapsed (the chevron toggles) - the indent
     clears the chevron column so both levels share one label axis */
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
  /* ruling 6: a live query GREYS the categories with no hit - the nav
     keeps its shape, so you can see where the hits live */
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
  /* a category heading inside the ONE document (card t_8b464090): the
     section's own title, carrying its knob count. The nav's jump links are
     styled above; these are the document's furniture.
     card t_a8063683 (Endo, 2026-11-11): this heading is the pane's ONLY
     heading, and it STICKS - it holds at the scrollport's top edge until
     the next section's heading pushes it out. Two traps this rule must
     not fall into:
     - a sticky box pins its BORDER box at top: 0, so the old
       `margin: 14px 0 2px` left a sliver ABOVE the pinned heading through
       which rows scrolled. The 14px now rides padding-top (with the 6px
       the label always had), so the pinned block is solid.
     - the heading must be OPAQUE or rows show through it. --z1 is the
       pane's own background (main.css .settings), so every theme agrees. */
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
  /* a child heading inside a multi-child category */
  .settings-subgroup-title {
    margin: 8px 0 0;
    font-size: 10.5px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--text-dim);
  }

  /* narrow (card t_8b464090): the SAME document, the nav as a drawer over
     it - it jumps, then closes. There is nothing to drill into, so no
     second screen and no back control. The pane root is the drawer's
     positioning context. */
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