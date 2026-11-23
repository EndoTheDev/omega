<template>
  <section
    ref="rootEl"
    class="cron-list"
    :class="{ narrow: narrow }">
    <!-- the UNIVERSAL band: rendered BEFORE the scroll body, so scrolling never moves it. -->
    <TabBand title="Cron">
      <div class="band-search">
        <SearchInput
          v-model="query"
          placeholder="Search cron jobs" />
      </div>
      <button
        class="band-tool"
        title="refresh"
        aria-label="refresh cron jobs"
        :disabled="refreshing"
        @click="refresh">
        <Icon
          name="refresh"
          :size="13" />
      </button>
      <button
        class="band-tool"
        title="add cron job"
        aria-label="add cron job"
        @click="startNew">
        <Icon
          name="plus"
          :size="13" />
      </button>
    </TabBand>
    <!-- the editor panel: one at a time, above the rows, so the scroll never moves it. -->
    <div
      v-if="editor"
      class="cron-editor">
      <p class="cron-editor-title">{{ editor.creating ? "add cron job" : `edit ${editor.draft.name}` }}</p>
      <div class="cron-editor-grid">
        <label class="cron-editor-field">
          <span>name</span>
          <input
            v-model="editor.draft.name"
            :disabled="!editor.creating"
            placeholder="my-job" />
        </label>
        <label class="cron-editor-field">
          <span>kind</span>
          <select v-model="editor.draft.kind">
            <option value="cron">cron (5-field)</option>
            <option value="interval">interval</option>
          </select>
        </label>
        <label
          v-if="editor.draft.kind === 'cron'"
          class="cron-editor-field">
          <span>schedule</span>
          <input
            v-model="editor.draft.schedule"
            placeholder="0 8 * * *" />
        </label>
        <label
          v-if="editor.draft.kind === 'interval'"
          class="cron-editor-field">
          <span>minutes</span>
          <input
            v-model="editor.draft.minutes"
            type="number"
            min="1"
            placeholder="5" />
        </label>
        <label class="cron-editor-field">
          <span>deliver</span>
          <select v-model="editor.draft.deliver">
            <option
              v-for="t in DELIVER_TARGETS"
              :key="t"
              :value="t">{{ t }}</option>
          </select>
        </label>
        <label
          v-if="editor.draft.deliver === 'append-to-file'"
          class="cron-editor-field">
          <span>file</span>
          <input
            v-model="editor.draft.file"
            placeholder="journal (empty = dated journal file)" />
        </label>
        <!--
          agent and model are COMBOBOXES: picking is the fast path, typing stays legal (a
          job may reference an agent home or a model that no longer exists), and an EMPTY
          field is the inheritance chain - agent from main, model from the agent's own.
        -->
        <div
          v-for="f in COMBO_FIELDS"
          :key="f.field"
          class="cron-editor-field">
          <div class="cron-field-head">
            <span>{{ f.label }}</span>
            <span
              v-if="chainOf(f.field).inherited"
              class="cron-inherited-chip"
              :class="{ blocked: chainOf(f.field).blocked }"
              :title="chainOf(f.field).note">inherited</span>
          </div>
          <div class="cron-combo">
            <input
              v-model="editor.draft[f.field]"
              role="combobox"
              :aria-label="f.label"
              :aria-expanded="combo.field === f.field"
              aria-autocomplete="list"
              :aria-controls="`cron-${f.field}-options`"
              autocomplete="off"
              spellcheck="false"
              :placeholder="f.placeholder"
              @focus="openCombo(f.field)"
              @input="onComboInput(f.field)"
              @keydown="onComboKeydown($event, f.field)"
              @blur="closeCombo" />
            <ul
              v-if="combo.field === f.field"
              :id="`cron-${f.field}-options`"
              class="cron-combo-list"
              role="listbox">
              <li
                v-for="(o, i) in comboRows(f.field)"
                :key="o.value"
                class="cron-combo-row"
                :class="{ active: i === combo.index }"
                role="option"
                :aria-selected="i === combo.index"
                @mousedown.prevent="pickCombo(f.field, o.value)">
                <span class="cron-combo-label">{{ o.label }}</span>
                <span
                  v-if="o.hint"
                  class="cron-combo-hint">{{ o.hint }}</span>
              </li>
              <li
                v-if="!comboRows(f.field).length"
                class="cron-combo-empty">
                {{ optionStatus(f.field) }}
              </li>
            </ul>
          </div>
          <p
            v-if="chainOf(f.field).inherited"
            class="cron-field-note cron-chain-note"
            :class="{ blocked: chainOf(f.field).blocked }">
            {{ chainOf(f.field).note }}
          </p>
          <p
            v-if="unknownNote(f.field)"
            class="cron-field-note">{{ unknownNote(f.field) }}</p>
        </div>
        <div class="cron-editor-field cron-editor-wide">
          <span>skills</span>
          <div
            class="cron-skills"
            :class="{ open: skillsOpen }">
            <div
              class="cron-skills-control"
              role="button"
              tabindex="0"
              aria-haspopup="listbox"
              :aria-expanded="skillsOpen"
              aria-label="choose skills"
              @click="toggleSkillsPanel"
              @keydown.enter.prevent="toggleSkillsPanel"
              @keydown.space.prevent="toggleSkillsPanel">
              <span
                v-if="!skillChips.length"
                class="cron-skills-placeholder">choose skills</span>
              <button
                v-for="s in skillChips"
                :key="s"
                type="button"
                class="cron-skill-chip"
                :aria-label="`remove skill ${s}`"
                @click.stop="removeSkill(s)">
                {{ s }}<span
                  class="cron-skill-x"
                  aria-hidden="true">&times;</span>
              </button>
              <Icon
                class="cron-skills-caret"
                name="chevron-down"
                :size="11" />
            </div>
            <div
              v-if="skillsOpen"
              class="cron-skills-panel">
              <input
                v-model="skillQuery"
                class="cron-skills-search"
                aria-label="search skills"
                placeholder="search skills"
                autocomplete="off"
                spellcheck="false" />
              <ul
                class="cron-skills-list"
                role="listbox"
                aria-multiselectable="true">
                <li
                  v-for="o in skillList"
                  :key="o.value">
                  <label
                    class="cron-skill-row"
                    :title="o.hint || o.value">
                    <input
                      type="checkbox"
                      :checked="skillChips.includes(o.value)"
                      @change="toggleSkill(o.value)" />
                    <span class="cron-skill-name">{{ o.value }}</span>
                    <span
                      v-if="o.hint"
                      class="cron-skill-hint">{{ o.hint }}</span>
                  </label>
                </li>
                <li
                  v-if="!skillList.length"
                  class="cron-combo-empty">
                  {{ optionStatus("skills") }}
                </li>
              </ul>
            </div>
          </div>
          <p
            v-if="skillsDropped.length"
            class="cron-field-note">
            dropped (not in the skills registry): {{ skillsDropped.join(", ") }}
          </p>
        </div>
        <label class="cron-editor-field cron-editor-wide">
          <span>prompt</span>
          <textarea
            v-model="editor.draft.prompt"
            rows="4"
            placeholder="the user message the fire sends" />
        </label>
      </div>
      <p
        v-if="editor.error"
        class="cron-editor-error">{{ editor.error }}</p>
      <div class="cron-editor-actions">
        <button
          class="cron-editor-btn primary"
          :disabled="editor.busy"
          @click="commitEditor">
          {{ editor.busy ? "saving..." : "save job" }}
        </button>
        <button
          class="cron-editor-btn"
          :disabled="editor.busy"
          @click="cancelEditor">
          cancel
        </button>
        <button
          v-if="!editor.creating"
          class="cron-editor-btn danger"
          :disabled="editor.busy"
          @click="onDeleteFromEditor">
          {{ editor.armedDelete ? "confirm delete?" : "delete job" }}
        </button>
      </div>
    </div>
    <div class="cron-body">
      <p
        v-if="error"
        class="panel-status">
        {{ error }}
      </p>
      <p
        v-else-if="!loaded"
        class="panel-status">
        loading
      </p>
      <ul
        v-else-if="filteredJobs.length"
        class="cron-jobs">
      <li
        v-for="j in filteredJobs"
        :key="cronKeyOf(j)"
        class="cron-row reg-row"
        :class="{ disabled: !j.enabled }"
        @contextmenu="onRowMenu($event, j)">
        <div class="cron-head reg-row-head">
          <div class="reg-row-main">
            <span class="cron-name reg-row-name">
              <i
                class="cron-dot"
                :class="[`cron-dot-${cronDotOf(j)}`, { live: j.running }]"
                :title="cronDotLabel(j)"
                :aria-label="cronDotLabel(j)"
                role="img"></i>{{ j.name }}</span>
            <span class="reg-row-sub">
              <span class="cron-meta reg-row-sub-text">
                {{ cadenceOf(j) }}<template v-if="j.next"> - next {{ shortTime(j.next) }}</template>{{ lastMeta(j) }}{{ jobMeta(j) }}
              </span>
            </span>
          </div>
          <button
            v-if="editableOf(j)"
            class="band-tool cron-edit-ctrl"
            :aria-label="`edit cron job ${j.name}`"
            @click="startEdit(j)">
            <Icon
              name="pencil"
              :size="13" />
          </button>
          <TogglePill
            class="cron-toggle reg-row-ctrl"
            :state="j.enabled"
            :disabled="busy !== null"
            :label="`${j.enabled ? 'disable' : 'enable'} ${j.name}`"
            @click="onToggle(j)" />
        </div>
      </li>
      </ul>
      <!-- the row anatomy law: the filter HIDES rows, it never changes their shape. -->
      <p
        v-else-if="query.trim()"
        class="panel-status">
        no matching jobs
      </p>
      <div
        v-else
        class="cron-empty">
        <Icon
          class="cron-empty-icon"
          name="cron"
          :size="20" />
        <p class="cron-empty-text">{{ CRON_EMPTY_TEXT }}</p>
        <p class="cron-empty-hint">{{ CRON_EMPTY_HINT }}</p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  import { openContextMenu } from "~/composables/contextMenu";
  import { cronMenuItems } from "./cronMenu";
  // the empty-state copy lives in its own PURE module.
  import { CRON_EMPTY_HINT, CRON_EMPTY_TEXT } from "./emptyState";
  import {
    cronKeyOf,
    runCronJobNow,
    setCronJobEnabled,
    stopCronRun,
    type CronJobWire,
  } from "./toggle";
  import { cronDotLabel, cronDotOf } from "./statusDot";
  import { filterCronJobs } from "./search";
  import {
    DELIVER_TARGETS,
    agentChain,
    cadenceOf,
    deleteCronJob,
    draftOf,
    editableOf,
    emptyDraft,
    fetchAgentOptions,
    fetchModelOptions,
    fetchSkillOptions,
    filterFieldOptions,
    jobMeta,
    lastMeta,
    mainAgentOf,
    modelChain,
    moveComboIndex,
    optionKnown,
    parseSkills,
    saveCronJob,
    skillsVoice,
    strictSkills,
    validateDraft,
    withSkill,
    withoutSkill,
    type ChainDraft,
    type ChainValue,
    type FieldOption,
    type JobDraft,
    type ModelCatalog,
  } from "./edit";

  const jobs = ref<(CronJobWire & { kind?: string; minutes?: number; deliver?: string; model?: string; file?: string })[]>([]);
  const loaded = ref(false);
  const error = ref("");

  // the in-band search - CLIENT-SIDE on the loaded listing.
  const query = ref("");
  const filteredJobs = computed(() => filterCronJobs(jobs.value, query.value));

  const refreshing = ref(false);

  const editor = ref<{
    creating: boolean;
    draft: JobDraft;
    error: string;
    busy: boolean;
    armedDelete: boolean;
  } | null>(null);
  // editorSeq invalidates an in-flight options callback when the open editor changes.
  let editorSeq = 0;

  // each option source loads once per mount (re-armed on a failed answer); a source that
  // is down degrades its ONE control, never the edit.
  type ComboField = "agent" | "model";

  const agentOptions = ref<FieldOption[]>([]);
  const modelOptions = ref<FieldOption[]>([]);
  const skillOptions = ref<FieldOption[]>([]);
  // the model this omega RUNS (GET /models' `current`); empty when the catalog never
  // answered, so the chain names the source without inventing a value.
  const modelCurrent = ref("");
  const optionErrors = ref<Record<"agent" | "model" | "skills", string>>({
    agent: "",
    model: "",
    skills: "",
  });
  const optionsReady = ref(false);
  let optionsInflight: Promise<void> | null = null;

  function applySource(
    settled: PromiseSettledResult<FieldOption[]>,
    target: typeof agentOptions,
    key: "agent" | "model" | "skills",
  ): boolean {
    if (settled.status === "fulfilled") {
      target.value = settled.value;
      optionErrors.value[key] = "";
      return true;
    }
    target.value = [];
    const reason = settled.reason;
    optionErrors.value[key] =
      reason instanceof Error ? reason.message : String(reason);
    return false;
  }

  function applyModelSource(settled: PromiseSettledResult<ModelCatalog>): boolean {
    if (settled.status === "fulfilled") {
      modelOptions.value = settled.value.options;
      modelCurrent.value = settled.value.current;
      optionErrors.value.model = "";
      return true;
    }
    modelOptions.value = [];
    modelCurrent.value = "";
    const reason = settled.reason;
    optionErrors.value.model =
      reason instanceof Error ? reason.message : String(reason);
    return false;
  }

  function ensureOptions(): Promise<void> {
    if (optionsInflight) return optionsInflight;
    if (optionsReady.value) return Promise.resolve();
    optionsInflight = Promise.allSettled([
      fetchAgentOptions(),
      fetchModelOptions(),
      fetchSkillOptions(),
    ])
      .then(([agents, models, skills]) => {
        const all =
          applySource(agents, agentOptions, "agent") &&
          applyModelSource(models) &&
          applySource(skills, skillOptions, "skills");
        optionsReady.value = all;
      })
      .finally(() => {
        optionsInflight = null;
      });
    return optionsInflight;
  }

  // one open control at a time; the typed text IS the field's value, so picking is only a
  // shortcut and a value the registry does not list still saves.
  const COMBO_FIELDS: { field: ComboField; label: string; placeholder: string }[] = [
    { field: "agent", label: "agent", placeholder: "the main agent" },
    {
      field: "model",
      label: "model",
      placeholder: "the agent's model",
    },
  ];

  const combo = ref<{ field: ComboField | null; index: number }>({
    field: null,
    index: -1,
  });
  const comboQuery = ref("");

  function comboRows(field: ComboField): FieldOption[] {
    const source = field === "agent" ? agentOptions.value : modelOptions.value;
    return filterFieldOptions(source, comboQuery.value);
  }

  function openCombo(field: ComboField) {
    combo.value = { field, index: -1 };
    comboQuery.value = ""; // opening shows the whole catalog
  }

  function closeCombo() {
    combo.value = { field: null, index: -1 };
  }

  function onComboInput(field: ComboField) {
    if (!editor.value) return;
    comboQuery.value =
      field === "agent" ? editor.value.draft.agent : editor.value.draft.model;
    if (combo.value.field !== field) combo.value = { field, index: -1 };
    else combo.value.index = -1;
  }

  function pickCombo(field: ComboField, value: string) {
    if (!editor.value) return;
    if (field === "agent") editor.value.draft.agent = value;
    else editor.value.draft.model = value;
    closeCombo();
  }

  function onComboKeydown(e: KeyboardEvent, field: ComboField) {
    const list = comboRows(field);
    if (combo.value.field !== field) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        openCombo(field);
      }
      return;
    }
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        combo.value.index = moveComboIndex(combo.value.index, 1, list.length);
        break;
      case "ArrowUp":
        e.preventDefault();
        combo.value.index = moveComboIndex(combo.value.index, -1, list.length);
        break;
      case "Enter": {
        const pick =
          combo.value.index >= 0 ? list[combo.value.index] : undefined;
        if (pick) {
          e.preventDefault();
          pickCombo(field, pick.value);
        }
        break;
      }
      case "Escape":
        e.preventDefault();
        closeCombo();
        break;
      case "Tab":
        closeCombo();
        break;
    }
  }

  function optionStatus(field: "agent" | "model" | "skills"): string {
    const err = optionErrors.value[field];
    if (err) {
      return field === "skills"
        ? `${err} - skill names come from the registry`
        : `${err} - type it instead`;
    }
    return "no match";
  }

  // free text is legal here, so a typed value the registry does not list is noted without
  // blocking the save.
  function unknownNote(field: ComboField): string {
    if (!editor.value || optionErrors.value[field]) return "";
    const value = (
      field === "agent" ? editor.value.draft.agent : editor.value.draft.model
    ).trim();
    const options = field === "agent" ? agentOptions.value : modelOptions.value;
    if (value === "" || options.length === 0) return "";
    if (optionKnown(options, value)) return "";
    return "not in the registry - kept as typed";
  }

  // the inheritance chain: one mechanism for both combo surfaces.
  const CHAIN_IDLE: ChainValue = {
    inherited: false,
    value: "",
    source: "",
    note: "",
    blocked: false,
  };

  function chainOf(field: ComboField): ChainValue {
    const draft = editor.value?.draft;
    if (!draft) return CHAIN_IDLE;
    const chain: ChainDraft = {
      agent: draft.agent,
      model: draft.model,
      deliver: draft.deliver,
    };
    return field === "agent"
      ? agentChain(chain, agentOptions.value)
      : modelChain(chain, agentOptions.value, modelCurrent.value);
  }

  // STRICT: a job's pinned skill the registry cannot serve is dropped from the draft and
  // NAMED on screen - visible, never silent.
  const skillsOpen = ref(false);
  const skillQuery = ref("");
  const skillsDropped = ref<string[]>([]);

  const skillChips = computed(() =>
    parseSkills(editor.value?.draft.skills ?? ""),
  );
  const skillList = computed(() =>
    filterFieldOptions(skillOptions.value, skillQuery.value),
  );

  function toggleSkillsPanel() {
    skillsOpen.value = !skillsOpen.value;
    if (skillsOpen.value) skillQuery.value = "";
  }

  function setChips(next: string[]) {
    if (!editor.value) return;
    editor.value.draft.skills = skillsVoice(next);
  }

  function toggleSkill(name: string) {
    const chips = skillChips.value;
    setChips(
      chips.includes(name)
        ? withoutSkill(chips, name)
        : withSkill(chips, name),
    );
  }

  function removeSkill(name: string) {
    setChips(withoutSkill(skillChips.value, name));
  }

  // runs once the registry answers, so a strict load never races the fetch; an unanswered
  // registry leaves the value exactly as loaded.
  function applyStrictSkills() {
    if (!editor.value || optionErrors.value.skills) return;
    const chips = parseSkills(editor.value.draft.skills);
    if (chips.length === 0) {
      skillsDropped.value = [];
      return;
    }
    const { kept, dropped } = strictSkills(chips, skillOptions.value);
    skillsDropped.value = dropped;
    if (dropped.length) editor.value.draft.skills = skillsVoice(kept);
  }

  function startNew() {
    editorSeq += 1;
    editor.value = {
      creating: true,
      draft: emptyDraft(),
      error: "",
      busy: false,
      armedDelete: false,
    };
    closeCombo();
    skillsOpen.value = false;
    skillsDropped.value = [];
    void ensureOptions();
  }

  function startEdit(j: CronJobWire & { kind?: string; minutes?: number; deliver?: string; model?: string; file?: string }) {
    // the strict-skills callback must land on THIS editor only: a sequence token, not an
    // object identity - editor is a ref, so its value is a reactive PROXY, never ===.
    editorSeq += 1;
    const token = editorSeq;
    editor.value = {
      creating: false,
      draft: draftOf(j),
      error: "",
      busy: false,
      armedDelete: false,
    };
    closeCombo();
    skillsOpen.value = false;
    skillsDropped.value = [];
    void ensureOptions().then(() => {
      if (editorSeq === token) applyStrictSkills();
    });
  }

  function cancelEditor() {
    editor.value = null;
    closeCombo();
    skillsOpen.value = false;
    skillsDropped.value = [];
  }

  async function commitEditor() {
    if (!editor.value || editor.value.busy) return;
    // the chain's law needs the roster's main agent (a wake may not lean on it).
    const msg = validateDraft(
      editor.value.draft,
      mainAgentOf(agentOptions.value),
    );
    if (msg) {
      editor.value.error = msg;
      return;
    }
    editor.value.busy = true;
    editor.value.error = "";
    try {
      jobs.value = await saveCronJob(editor.value.draft);
      editor.value = null;
      error.value = "";
    } catch (e) {
      if (editor.value) {
        editor.value.error = e instanceof Error ? e.message : String(e);
        editor.value.busy = false;
      }
    }
  }

  async function onDeleteFromEditor() {
    if (!editor.value || editor.value.busy) return;
    if (!editor.value.armedDelete) {
      editor.value.armedDelete = true; // two-step: the delete guard
      return;
    }
    editor.value.busy = true;
    try {
      if (editor.value.draft.name) {
        jobs.value = await deleteCronJob(editor.value.draft.name);
      }
      editor.value = null;
      error.value = "";
    } catch (e) {
      if (editor.value) {
        editor.value.error = e instanceof Error ? e.message : String(e);
        editor.value.busy = false;
        editor.value.armedDelete = false;
      }
    }
  }

  async function load(): Promise<void> {
    const res = await fetch("/cron/jobs");
    if (!res.ok) throw new Error(`GET /cron/jobs: ${res.status}`);
    jobs.value = (await res.json()) as CronJobWire[];
  }

  async function refresh(): Promise<void> {
    if (refreshing.value) return;
    refreshing.value = true;
    try {
      await load();
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      refreshing.value = false;
    }
  }

  // one click commits; busy holds the committing key so a stacked double-click cannot
  // overlap writes.
  const busy = ref<string | null>(null);

  function onToggle(j: CronJobWire) {
    if (busy.value !== null) return;
    apply(j);
  }

  // the row's right-click: Pause/Resume IS the pill, Delete opens the tab's own armed
  // confirm in the editor - the same guard the editor's delete button carries.
  function onRowMenu(e: MouseEvent, j: CronJobWire) {
    e.preventDefault();
    void openContextMenu(
      e.clientX,
      e.clientY,
      cronMenuItems(j, {
        runNow: () => void applyRun(j, runCronJobNow),
        stopRun: () => void applyRun(j, stopCronRun),
        toggle: () => onToggle(j),
        remove: () => {
          startEdit(j);
          if (editor.value) editor.value.armedDelete = true;
        },
      }),
    );
  }

  // applyRun commits one run-control verb and paints the fresh job set, then keeps the
  // live mark honest: while any fire is in flight the console re-reads on a short ladder,
  // so Stop run appears and disappears with the fire itself (never a stale row).
  let runPollTimer: ReturnType<typeof setTimeout> | undefined;
  function pollRunning(): void {
    if (runPollTimer) clearTimeout(runPollTimer);
    runPollTimer = undefined;
    if (!jobs.value.some((j) => j.running)) return;
    runPollTimer = setTimeout(async () => {
      runPollTimer = undefined;
      try {
        await load();
      } catch {
        return;
      }
      pollRunning();
    }, 2000);
  }
  onBeforeUnmount(() => {
    if (runPollTimer) clearTimeout(runPollTimer);
  });

  async function applyRun(j: CronJobWire, act: (key: string) => Promise<CronJobWire[]>): Promise<void> {
    if (busy.value !== null) return;
    busy.value = cronKeyOf(j);
    try {
      jobs.value = await act(cronKeyOf(j));
      error.value = "";
      pollRunning();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      busy.value = null;
    }
  }

  async function apply(j: CronJobWire) {
    const target = !j.enabled;
    busy.value = cronKeyOf(j);
    try {
      jobs.value = await setCronJobEnabled(cronKeyOf(j), target);
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      busy.value = null;
    }
  }

  // the module-UI narrow recipe: this component measures itself, then flips the flag.
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = ref(false);
  let resizeObs: ResizeObserver | null = null;
  onMounted(() => {
    if (typeof ResizeObserver === "undefined" || !rootEl.value) return;
    // rAF deferred: the flip changes the observed layout, so the callback must not
    // mutate this frame
    resizeObs = new ResizeObserver((entries) => {
      const pending = entries
        .map((e) => Math.round(e.contentRect.width))
        .filter((w) => w > 0);
      if (pending.length === 0) return;
      requestAnimationFrame(() => {
        if (resizeObs === null) return;
        for (const w of pending) narrow.value = isNarrowWidth(w);
      });
    });
    resizeObs.observe(rootEl.value);
    // mount measures NOW: an in-place remount produces no resize entry, so the class
    // cannot wait for a width change
    narrow.value = isNarrowWidth(Math.round(rootEl.value.getBoundingClientRect().width));
  });
  onBeforeUnmount(() => {
    resizeObs?.disconnect();
    resizeObs = null;
  });

  function shortTime(rfc3339: string): string {
    const t = new Date(rfc3339);
    if (Number.isNaN(t.getTime())) return rfc3339;
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${pad(t.getMonth() + 1)}/${pad(t.getDate())} ${pad(t.getHours())}:${pad(t.getMinutes())}`;
  }

  onMounted(async () => {
    try {
      await load();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
  });
</script>

<style scoped>
  .cron-list {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .cron-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 4px 12px 14px;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
  }
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
  .cron-empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    padding: 24px 12px;
    text-align: center;
  }
  .cron-empty-icon {
    color: var(--text-dim, #8a8a92);
    opacity: 0.8;
    margin-bottom: 6px;
  }
  .cron-empty-text {
    margin: 0;
    font-size: 12.5px;
    opacity: 0.75;
  }
  .cron-empty-hint {
    margin: 2px 0 0;
    font-size: 11px;
    color: var(--text-dim, #8a8a92);
  }
  .cron-jobs {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  /* the job editor panel: anchored under the band, above the scroll body, so edits never scroll away. */
  .cron-editor {
    padding: 8px 12px;
    border-bottom: 1px solid var(--border, rgba(128, 128, 128, 0.25));
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .cron-editor-title {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    opacity: 0.85;
  }
  .cron-editor-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px 10px;
  }
  .cron-editor-field {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    font-size: 11px;
  }
  .cron-editor-field.wide,
  .cron-editor-field.cron-editor-wide {
    grid-column: 1 / -1;
  }
  .cron-editor-field span {
    opacity: 0.6;
  }
  .cron-editor-field input,
  .cron-editor-field select,
  .cron-editor-field textarea {
    font: inherit;
    color: inherit;
    background: var(--bg-elev, rgba(128, 128, 128, 0.08));
    border: 1px solid var(--border, rgba(128, 128, 128, 0.25));
    border-radius: 4px;
    padding: 3px 6px;
    min-width: 0;
    box-sizing: border-box;
  }
  .cron-editor-field textarea {
    font-family: var(--mono, monospace);
    resize: vertical;
  }
  .cron-editor-error {
    margin: 0;
    font-size: 11px;
    color: var(--danger, #e5484d);
  }
  .cron-editor-actions {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .cron-editor-btn {
    font: inherit;
    font-size: 11px;
    color: inherit;
    background: none;
    border: 1px solid var(--border, rgba(128, 128, 128, 0.25));
    border-radius: 4px;
    padding: 3px 10px;
    cursor: pointer;
  }
  .cron-editor-btn:hover {
    border-color: var(--text, currentColor);
  }
  .cron-editor-btn.primary {
    background: var(--bg-elev, rgba(128, 128, 128, 0.12));
  }
  .cron-editor-btn.danger {
    color: var(--danger, #e5484d);
  }
  .cron-edit-ctrl {
    margin-right: 2px;
    flex: none;
    align-self: flex-start;
  }
  .cron-row.disabled {
    opacity: 0.5;
  }
  .cron-meta {
    font-family: var(--mono, monospace);
  }
  /*
   * THE STATUS DOT: the last fire's outcome, one word per color (green ok / red error /
   * amber never fired). A live fire pulses the same dot; the color still tells the last
   * outcome, the pulse tells the present.
   */
  .cron-dot {
    display: inline-block;
    width: 6px;
    height: 6px;
    margin-inline-end: 6px;
    border-radius: 50%;
    vertical-align: middle;
    background: var(--text-dim, #8a8a92);
  }
  .cron-dot-ok {
    background: var(--ok);
  }
  .cron-dot-error {
    background: var(--err);
  }
  .cron-dot-never {
    background: var(--accent);
  }
  .cron-dot.live {
    animation: cron-dot-pulse 1.1s ease-in-out infinite;
  }
  @keyframes cron-dot-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  .cron-field-note {
    margin: 2px 0 0;
    font-size: 10.5px;
    color: var(--text-dim, #8a8a92);
  }
  /* the inheritance chain's visible half: `blocked` is the chain the fire can never satisfy (a wake
     leaning on the main agent, which never wakes) - the same state save.go refuses. */
  .cron-field-head {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
  }
  .cron-editor-field .cron-inherited-chip {
    flex: none;
    font-size: 9.5px;
    font-family: var(--mono, monospace);
    line-height: 1.5;
    color: var(--text-dim, #8a8a92);
    background: var(--z1-raised, rgba(128, 128, 128, 0.14));
    border: 1px solid var(--stroke, rgba(128, 128, 128, 0.25));
    border-radius: var(--r-chip, 4px);
    padding: 0 4px;
    opacity: 1;
  }
  .cron-editor-field .cron-inherited-chip.blocked {
    color: var(--danger, #e5484d);
    border-color: var(--danger, #e5484d);
  }
  .cron-chain-note.blocked {
    color: var(--danger, #e5484d);
  }
  .cron-combo {
    position: relative;
    min-width: 0;
  }
  .cron-combo input {
    width: 100%;
  }
  .cron-combo-list {
    position: absolute;
    top: calc(100% + 2px);
    left: 0;
    right: 0;
    z-index: 30;
    margin: 0;
    padding: 2px;
    list-style: none;
    max-height: 180px;
    overflow-y: auto;
    background: var(--z2, #26262a);
    border: 1px solid var(--stroke, #2e2e31);
    border-radius: var(--r-chip, 4px);
    box-shadow: var(--shadow-2, 0 8px 30px rgba(0, 0, 0, 0.5));
  }
  .cron-combo-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    padding: 3px 6px;
    border-radius: 3px;
    cursor: pointer;
  }
  .cron-combo-row.active {
    background: color-mix(in srgb, var(--accent, #e8833a) 18%, transparent);
  }
  .cron-combo-label {
    flex: none;
    max-width: 60%;
    font-size: 11px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cron-combo-hint {
    flex: 1 1 auto;
    min-width: 0;
    font-size: 10.5px;
    color: var(--text-dim, #8a8a92);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cron-combo-empty {
    padding: 3px 6px;
    font-size: 10.5px;
    color: var(--text-dim, #8a8a92);
  }
  .cron-skills {
    position: relative;
  }
  .cron-skills-control {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
    min-height: 24px;
    padding: 2px 6px;
    font: inherit;
    font-size: 11px;
    color: inherit;
    background: var(--bg-elev, rgba(128, 128, 128, 0.08));
    border: 1px solid var(--border, rgba(128, 128, 128, 0.25));
    border-radius: 4px;
    cursor: pointer;
  }
  .cron-skills.open .cron-skills-control,
  .cron-skills-control:focus-visible {
    border-color: var(--accent, #e8833a);
    outline: none;
  }
  .cron-skills-placeholder {
    color: var(--text-dim, #8a8a92);
  }
  .cron-skill-chip {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font: inherit;
    font-size: 10.5px;
    font-family: var(--mono, monospace);
    color: inherit;
    background: var(--z1-raised, rgba(128, 128, 128, 0.14));
    border: 1px solid var(--stroke, rgba(128, 128, 128, 0.25));
    border-radius: var(--r-chip, 4px);
    padding: 0 4px;
    cursor: pointer;
  }
  .cron-skill-x {
    opacity: 0.55;
  }
  .cron-skill-chip:hover .cron-skill-x {
    opacity: 1;
    color: var(--accent, #e8833a);
  }
  .cron-skills-caret {
    margin-left: auto;
    opacity: 0.6;
  }
  .cron-skills-panel {
    position: absolute;
    top: calc(100% + 2px);
    left: 0;
    right: 0;
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 6px;
    background: var(--z2, #26262a);
    border: 1px solid var(--stroke, #2e2e31);
    border-radius: var(--r-chip, 4px);
    box-shadow: var(--shadow-2, 0 8px 30px rgba(0, 0, 0, 0.5));
  }
  .cron-skills-list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 180px;
    overflow-y: auto;
  }
  /* the row checkboxes are SELECTION boxes, not switches. */
  .cron-editor-field .cron-skill-row input {
    margin: 0;
    padding: 0;
    background: none;
    border: none;
    accent-color: var(--accent, #e8833a);
  }
  .cron-skill-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    padding: 2px 4px;
    border-radius: 3px;
    cursor: pointer;
  }
  .cron-skill-row:hover {
    background: rgba(128, 128, 128, 0.12);
  }
  .cron-skill-name {
    flex: 0 0 auto;
    font-size: 11px;
    font-family: var(--mono, monospace);
  }
  .cron-skill-hint {
    flex: 1 1 auto;
    min-width: 0;
    font-size: 10px;
    color: var(--text-dim, #8a8a92);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* narrow: the module-UI recipe. The rows are already single-column; what dies first at
     ~300px is horizontal breathing room - the head's name/sub pair wraps so a long schedule
     line never elides. */
  .cron-list.narrow .cron-body {
    padding: 4px 8px 10px;
  }
  .cron-list.narrow .cron-editor-grid {
    grid-template-columns: 1fr;
  }
</style>