<template>
    <!-- card t_308e6d9b: the tab carries the universal band ("Settings");
         card t_529628d5: the EXISTING filter input SEATS IN the band
         (the t_7ee0a673 sessions card's move, the shared .band-search
         seat in TabBand's band-tool slot) - the filter's BEHAVIOR is
         untouched (filterSettingGroups on the loaded groups, v-model
         "query"); only its seat moved. The old filter row below the
         band is GONE (card t_f1a034dc's own-row shape died with this
         ruling). The dock tab bar still carries the chip and the tab
         lives pinned in the inspector (card t_49d0b308). The aside
         classes carry the mobile drawer shape (mirrors FilesTab);
         `narrow` is the width-driven adaptation class (a measured on
         the root: the inspector's ~300px column). -->
    <aside
      ref="rootEl"
      class="settings aside pane inspector-panel"
      :class="{ open: open, narrow: narrow }">
      <TabBand title="Settings">
        <div class="band-search">
          <SearchInput
            v-model="query"
            placeholder="filter settings" />
        </div>
      </TabBand>

    <p v-if="loadError" class="settings-error">
      {{ loadError }}
    </p>
    <div class="settings-body">
      <p
      v-if="!loadError && visibleGroups.length === 0"
      class="settings-empty">
        no settings match "{{ query.trim() }}"
      </p>

    <!--
      Two knob origins share one renderer:
      - the chat group is client state (props/emits, persisted under
        omega.theme-mode, panel prefs) - never sent to the server;
      - every other group is server config, edited in the draft map and
        saved through PUT /settings (diff-only).
    -->
    <section
      v-for="group in visibleGroups"
      :key="group.key"
      class="settings-group">
      <!-- full-row clickable header, same interaction feel as the sidebar
           day groups: chevron prefix, whole header toggles, aria-expanded.
           A group is hidden when collapsed, EXCEPT while a search narrows
           the list - matching results must never hide the sections that
           found them. -->
      <button
        class="settings-group-head"
        type="button"
        :aria-expanded="!isCollapsed(group.key)"
        @click="toggleCollapsed(group.key)">
        <Icon
          class="bucket-chevron"
          :class="{ open: !isCollapsed(group.key) }"
          name="chevron-down"
          :size="12" />
        <span class="settings-group-title">{{ group.label }}</span>
      </button>
      <template v-if="!isCollapsed(group.key)">
        <template v-for="knob in group.knobs" :key="knob.key">
        <!-- toggle -->
        <label v-if="knob.type === 'toggle'" class="setting-row">
          <input
            type="checkbox"
            :checked="toggleValue(group, knob)"
            @change="
              setToggle(group, knob, ($event.target as HTMLInputElement).checked)
            " />
          <span>{{ knob.label }}</span>
        </label>
        <div v-else class="setting-item">
          <div class="setting-head">
            <span>{{ knob.label }}</span>
            <span v-if="serverKnobDirty(knob)" class="setting-dirty">*</span>
          </div>
          <!-- select (card t_acad02ec, audit M1: the themed listbox - a
               native select popup is OS chrome over the pane) -->
          <BandSelect
            v-if="knob.type === 'select'"
            class="setting-select"
            :model-value="selectValue(group, knob)"
            :options="knobOptions(knob)"
            :label="knob.label"
            drop="down"
            @update:model-value="setSelect(group, knob, $event)" />
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
      </template>
    </section>
    <!-- card t_210a0d19: the MODULES group is gone from settings - the
         module toggles live in the extensions tab now (the module
         lifecycle cockpit), and this pane carries config knobs only. -->
    </div>
    <!-- save row stays pinned below the scrollable body -->
    <div v-if="dirtyCount > 0" class="settings-save-row">
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
      knobs marked live apply to the running server; the rest take effect
      on restart.
    </p>
  </aside>
</template>

<script setup lang="ts">
  import {
    computed,
    onMounted,
    ref,
  } from "vue";
  import {
    fetchSettings,
    filterSettingGroups,
    saveSettings,
    settingsDiff,
    valuesEqual,
    parseCollapsedKeys,
    serializeCollapsedKeys,
    themePickOptions,
    colorModeOptions,
    type SettingGroup,
    type SettingKnob,
    type SettingsPayload,
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
  import { useNarrow } from "~/composables/narrow";
  import type { ToolDisplay } from "~/composables/toolDisplay";
  import type { ThinkingDisplay } from "~/composables/thinkingDisplay";
  // card t_502b8e81: the iconset wrapper - the group collapser chevron
  import Icon from "../ui/Icon.vue";
  import WorkspaceSelect from "../chat/WorkspaceSelect.vue";
  // card t_d7fd3ad3 item 6: the dock group's client knob rides the shared
  // bottom-width mode ref (the applier in app.vue reads the same signal)
  import {
    setBottomWidthMode,
    useBottomWidthMode,
  } from "~/composables/bottomWidth";
  // card t_30137f67: the server.default_workspace value rides the same
  // shared-signal shape - this tab's reads keep it fresh, the chat host
  // reads it when a new session's draft is born
  import { DEFAULT_WORKSPACE_KNOB, setDefaultWorkspace } from "~/composables/defaultWorkspace";
  // card t_5752861e: the composer's display mode knob rides the shared
  // signal shape (bottomWidth's pattern) - this tab writes it, the
  // composer's class binding reads it
  import {
    setComposerMode,
    useComposerMode,
    type ComposerMode,
  } from "~/composables/composerMode";

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
  const query = ref("");

  // Card 5: settings is a dock TAB, not an overlay - the tab opens once
  // and stays mounted, so the load-on-open watch dies with the drawer
  // shape. The tab mount loads; the save round-trip refreshes from the
  // server response the same way it did before.
  onMounted(() => {
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
    // card t_30137f67: every read (mount reload + save round-trip)
    // keeps the shared signal fresh - a Settings edit applies to the
    // next new session's chip without a reload. "" clears it.
    setDefaultWorkspace(values[DEFAULT_WORKSPACE_KNOB]);
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

  const chatGroup = computed<SettingGroup>(() => ({
    key: "chat",
    label: "chat",
    knobs: [
      {
        key: "chat.show_thinking",
        label: "show thinking blocks in chat",
        type: "toggle",
        value: props.showThinking,
      },
      {
        key: "chat.color_mode",
        label: "color mode",
        type: "select",
        value: props.colorMode,
        options: [...colorModeOptions],
        help: "interface colour palette; auto follows the system preference live",
      },
      {
        key: "chat.theme",
        label: "theme",
        type: "select",
        value: props.themeId,
        options: themeOptions.value,
        help: "colour theme; default is the built-in palette, the rest are the server's themes/ folder files",
      },
      {
        key: "chat.appearance",
        label: "chat appearance",
        type: "select",
        value: props.appearance,
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
        options: ["auto", "expanded", "collapsed"],
        help:
          "auto keeps thinking blocks collapsed behind their one-line " +
          "summary and a manual click pins one open; expanded/collapsed force one state",
      },
      {
        key: "chat.composer_mode",
        label: "composer display mode",
        type: "select",
        value: composerMode.value,
        options: ["compact", "wide", "full"],
        help:
          "compact is the card shape with side insets; wide tightens the " +
          "insets so the composer grows wider; full spans the pane edge to edge",
      },
    ],
  }));

  // card t_d7fd3ad3 item 6: the dock's client knob - the bottom panel's
  // width mode. "aligned" (default) matches the panel to the main panes'
  // box (the VS Code panel read); "full" spans the dock area. Written
  // straight to the shared module ref (composables/bottomWidth.ts) - the
  // applier in app.vue re-measures on the same signal.
  const bottomWidthMode = useBottomWidthMode();
  // card t_5752861e: the composer mode knob - same shared-signal shape
  const composerMode = useComposerMode();
  const dockGroup = computed<SettingGroup>(() => ({
    key: "dock",
    label: "dock",
    knobs: [
      {
        key: "dock.bottom_width",
        label: "bottom panel width",
        type: "select",
        value: bottomWidthMode.value,
        options: ["aligned", "full"],
        help:
          "aligned matches the bottom panel to the main pane's width " +
          "(the sidebars stand clear, like the VS Code panel); full " +
          "spans the whole window",
      },
    ],
  }));

  const isClient = (group: SettingGroup) =>
    group.key === "chat" || group.key === "dock";

  // t_a5f9d185: the path knob (default workspace) renders input + the
  // workspace browse chip; every other text knob keeps the plain input.
  const isPathKnob = (knob: SettingKnob) =>
    knob.key === "server.default_workspace";

  const allGroups = computed<SettingGroup[]>(() => [
    chatGroup.value,
    dockGroup.value,
    ...groups.value,
  ]);

  const visibleGroups = computed<SettingGroup[]>(() =>
    filterSettingGroups(allGroups.value, query.value),
  );

  // --- collapsible groups (same mechanic as the sidebar day groups) ---
  // collapsed holds the group KEYS whose knobs are hidden. Restored from
  // localStorage on setup; every group defaults expanded (unknown data
  // restores expanded too, via parseCollapsedKeys' tolerance). Search
  // wins over collapse: while a query narrows the list, every surviving
  // group shows its matches (see isCollapsed). Groups are dynamic server
  // data, so the persisted keys are parsed without a bucket allowlist -
  // stale keys are inert (no section renders under that key).
  const COLLAPSED_KEY = "omega.settings-collapsed";
  const collapsed = ref<Set<string>>(new Set());

  function restoreCollapsed() {
    if (typeof window === "undefined") return;
    collapsed.value = parseCollapsedKeys(
      localStorage.getItem(COLLAPSED_KEY),
    );
  }

  function toggleCollapsed(key: string) {
    // The persisted set is shared storage, so every toggle starts from
    // the storage set plus this pane's own view. A blind in-memory
    // write clobbered a concurrent writer's keys between two toggles
    // (found in the verify lane, card t_8ac225f1). Card t_210a0d19
    // moved the modules group (its second writer) to the extensions
    // tab, but the read-modify-write stays: any writer that reappears
    // must not lose the other groups' keys.
    const stored =
      typeof window === "undefined"
        ? collapsed.value
        : parseCollapsedKeys(localStorage.getItem(COLLAPSED_KEY));
    const next = new Set([...stored, ...collapsed.value]);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    collapsed.value = next;
    if (typeof window !== "undefined")
      localStorage.setItem(COLLAPSED_KEY, serializeCollapsedKeys(next));
  }

  // a search that narrows the groups must not also swallow their knobs
  const isCollapsed = (key: string) =>
    collapsed.value.has(key) && query.value.trim().length === 0;

  restoreCollapsed();

  // --- value plumbing ---

  const clientValue = (knob: SettingKnob): unknown => {
    if (knob.key === "chat.show_thinking") return props.showThinking;
    if (knob.key === "chat.appearance") return props.appearance;
    if (knob.key === "chat.tool_display") return props.toolDisplay;
    if (knob.key === "chat.thinking_display") return props.thinkingDisplay;
    if (knob.key === "dock.bottom_width") return bottomWidthMode.value;
    if (knob.key === "chat.composer_mode") return composerMode.value;
    if (knob.key === "chat.color_mode") return props.colorMode;
    return props.themeId;
  };

  // serverValue reads draft-first: a knob the user edited (dirty or not)
  // always displays what will be saved.
  const serverValue = (knob: SettingKnob): unknown =>
    draft.value[knob.key] ?? loaded.value[knob.key];

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

  // providersValue (card t_7dcd4280): the list editor's rows - a
  // structural value coerced to its shape (tolerant: an unexpected
  // kind degrades to no rows, never a render crash).
  const providersValue = (knob: SettingKnob): ProviderInstanceRow[] => {
    const v = serverValue(knob);
    if (!Array.isArray(v)) return [];
    return v
      .filter((r): r is ProviderInstanceRow => !!r && typeof r === "object")
      .map((r) => ({
        name: typeof r["name"] === "string" ? r["name"] : "",
        type: typeof r["type"] === "string" ? r["type"] : "",
        model_name: typeof r["model_name"] === "string" ? r["model_name"] : "",
        host: typeof r["host"] === "string" ? r["host"] : "",
        api_key: typeof r["api_key"] === "string" ? r["api_key"] : "",
      }));
  };

  function toggleValue(group: SettingGroup, knob: SettingKnob): boolean {
    if (isClient(group)) return clientValue(knob) === true;
    return serverValue(knob) === true;
  }

  function selectValue(group: SettingGroup, knob: SettingKnob): string {
    const v = isClient(group) ? clientValue(knob) : serverValue(knob);
    return typeof v === "string" ? v : String(v);
  }

  function setToggle(group: SettingGroup, knob: SettingKnob, on: boolean) {
    if (isClient(group)) {
      if (knob.key === "chat.show_thinking") emit("update:showThinking", on);
      return;
    }
    draft.value = { ...draft.value, [knob.key]: on };
  }

  function setSelect(group: SettingGroup, knob: SettingKnob, value: string) {
    if (isClient(group)) {
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
      else if (knob.key === "chat.composer_mode")
        setComposerMode(value as ComposerMode);
      return;
    }
    draft.value = { ...draft.value, [knob.key]: value };
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
    draft.value = { ...draft.value, [knob.key]: value };
  }

  const serverKnobDirty = (knob: SettingKnob): boolean =>
    !valuesEqual(draft.value[knob.key], loaded.value[knob.key]);

  const dirtyCount = computed(
    () => Object.keys(settingsDiff(loaded.value, draft.value)).length,
  );

  function resetDraft() {
    saveError.value = null;
    draft.value = { ...loaded.value };
  }

  async function save() {
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
</script>

<style scoped>
  /* card t_529628d5: the filter rides TabBand's shared .band-search
     seat (the t_f1a034dc own-row shape died with this ruling). The
     knob select is the shared BandSelect chip - inline (the
     composer's listbox pattern), never wider than its row */
  .setting-select {
    max-width: 100%;
  }
</style>