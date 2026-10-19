<template>
  <!-- no title row: the dock tab bar carries the "settings" title; the
       menubar gear OPENS this tab (singleton) - this is just content -->
  <aside class="settings">
    <!-- search: narrows every group live; Escape clears (shared look -
         SearchInput, same component as the sidebar's session search) -->
    <SearchInput
      v-model="query"
      class="settings-search"
      placeholder="filter settings" />

    <p v-if="loadError" class="settings-error">
      {{ loadError }}
    </p>
    <div class="settings-body">
      <p v-if="!loadError && visibleGroups.length === 0" class="settings-empty">
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
        <svg
          class="bucket-chevron"
          :class="{ open: !isCollapsed(group.key) }"
          width="8"
          height="8"
          viewBox="0 0 8 8"
          aria-hidden="true">
          <path
            d="M2.5 1L6 4L2.5 7"
            fill="none"
            stroke="currentColor"
            stroke-width="1.2" />
        </svg>
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
          <!-- select -->
          <select
            v-if="knob.type === 'select'"
            class="model-chip theme-select"
            :value="selectValue(group, knob)"
            @change="setSelect(group, knob, ($event.target as HTMLSelectElement).value)">
            <option v-for="opt in knob.options ?? []" :key="opt" :value="opt">
              {{ opt }}
            </option>
          </select>
          <!-- textarea -->
          <textarea
            v-else-if="knob.type === 'textarea'"
            class="setting-input setting-textarea"
            rows="3"
            :value="serverValue(knob)"
            @input="setServer(knob, ($event.target as HTMLTextAreaElement).value)" />
          <!-- text / secret / bindlist / number -->
          <input
            v-else
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
          <p v-if="knob.help" class="setting-help">{{ knob.help }}</p>
        </div>
        </template>
      </template>
    </section>
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
    parseCollapsedKeys,
    serializeCollapsedKeys,
    type SettingGroup,
    type SettingKnob,
    type SettingsPayload,
  } from "~/composables/omega";

  const props = defineProps<{
    showThinking: boolean;
    themeMode: "auto" | "light" | "dark";
  }>();

  const emit = defineEmits<{
    (e: "update:showThinking", value: boolean): void;
    (e: "set-theme", mode: "auto" | "light" | "dark"): void;
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
  }

  // --- client (chat) group: props in, emits out ---

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
        key: "chat.theme",
        label: "theme",
        type: "select",
        value: props.themeMode,
        options: ["auto", "light", "dark"],
        help: "interface colour theme; auto follows the system",
      },
    ],
  }));

  const isClient = (group: SettingGroup) => group.key === "chat";

  const allGroups = computed<SettingGroup[]>(() => [chatGroup.value, ...groups.value]);

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
    const next = new Set(collapsed.value);
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

  const clientValue = (knob: SettingKnob): unknown =>
    knob.key === "chat.show_thinking" ? props.showThinking : props.themeMode;

  // serverValue reads draft-first: a knob the user edited (dirty or not)
  // always displays what will be saved.
  const serverValue = (knob: SettingKnob): unknown =>
    draft.value[knob.key] ?? loaded.value[knob.key];

  // numberValue: number inputs must never render a half-state - an
  // empty draft shows "" so the field can be cleared; anything else is
  // whatever the last parse produced (see setServer).
  const numberValue = (knob: SettingKnob): unknown => {
    const v = serverValue(knob);
    return v === "" ? "" : v;
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
      if (knob.key === "chat.theme")
        emit("set-theme", value as "auto" | "light" | "dark");
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
    draft.value[knob.key] !== loaded.value[knob.key];

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