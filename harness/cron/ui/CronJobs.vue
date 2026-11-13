<template>
  <section
    ref="rootEl"
    class="cron-list"
    :class="{ narrow: narrow }">
    <!-- card t_308e6d9b: the UNIVERSAL band - the Files pattern lands on
         every tab. Card t_529628d5: the band carries the compact in-band
         search seat (the t_7ee0a673 sessions seat, TabBand's shared
         .band-search) - the query filters the LOADED jobs by name or
         schedule, client-side, hide-rows-only - and the REFRESH tool
         (the Skills/Tools/MCP band-tool look): the click re-fetches GET
         /cron/jobs. Rendered BEFORE the scroll body: scrolling never
         moves it. -->
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
      <!-- card t_ecec864e: the job editor lives here - the tab SHOWS and
           EDITS all job fields (kind, cadence, agent, model, prompt,
           deliver target, enabled). The plus tool opens a new-job form;
           each main-home row carries its own pencil. -->
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
    <!-- the editor panel: one at a time, above the rows so the scroll
         never moves it mid-edit (the band-before-body law's sibling). -->
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
        <!-- card t_60e0651e, rulings 1-2: agent and model are COMBOBOXES.
             Agent reads the agents registry (GET /agents), model reads
             the same GET /models catalog the settings model knobs use.
             ONE block, driven by COMBO_FIELDS: the two controls differ
             only in their source, placeholder and the note they carry.
             Picking is the fast path; TYPING stays legal both ways - a
             job may reference an agent home or a model that no longer
             exists, and the editor never strands that edit. -->
        <div
          v-for="f in COMBO_FIELDS"
          :key="f.field"
          class="cron-editor-field">
          <span>{{ f.label }}</span>
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
            v-if="unknownNote(f.field)"
            class="cron-field-note">{{ unknownNote(f.field) }}</p>
        </div>
        <!-- t_3885c975: the fire-pinned skills - the bodies ride the fire
             prompt ahead of the prompt. card t_60e0651e, ruling 3: a
             STRICT multi-select - a checkbox dropdown with its own
             search, chosen names riding the closed control as chips.
             Strict: only names the skills registry (GET /skills) serves
             can be chosen or loaded. -->
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
        :class="{ disabled: !j.enabled }">
        <div class="cron-head reg-row-head">
          <div class="reg-row-main">
            <span class="cron-name reg-row-name">{{ j.name }}</span>
            <!-- the row law's subtitle: the mono cadence (kind-aware),
                 with the next fire + the deliver/agent/model meta riding
                 the same dim line - the two-line reference anatomy,
                 uniform row heights. -->
            <span class="reg-row-sub">
              <span class="cron-meta reg-row-sub-text">
                {{ cadenceOf(j) }}<template v-if="j.next"> - next {{ shortTime(j.next) }}</template>{{ lastMeta(j) }}{{ jobMeta(j) }}
              </span>
            </span>
          </div>
          <!-- card t_ecec864e: the row's edit affordance - a pencil per
               MAIN-home row opening the editor pre-filled with the job
               (agent-home wake rows carry no pencil: their file belongs
               to a different home, the LoadAllConfigs law). -->
          <button
            v-if="editableOf(j)"
            class="band-tool cron-edit-ctrl"
            :aria-label="`edit cron job ${j.name}`"
            @click="startEdit(j)">
            <Icon
              name="pencil"
              :size="13" />
          </button>
          <!-- card t_561dc4e0: the "enabled"/"disabled" text dies - the
               one right control is the SHARED TogglePill (card
               t_d93864e7, the MCP pill), ONE click applies (card
               t_5051d523: the arm/confirm two-step and its "click again
               to confirm" note are dead - they made every job need a
               double click), top-aligned with the name. The click POSTs
               /cron/toggle (harness/cron/ui/toggle.ts): the write path
               is the per-job `enabled:` flag in the crons.yaml that owns
               the job, hot-reloaded - the same config-file law the MCP
               tab's own toggle rides. -->
          <TogglePill
            class="cron-toggle reg-row-ctrl"
            :state="j.enabled"
            :disabled="busy !== null"
            :label="`${j.enabled ? 'disable' : 'enable'} ${j.name}`"
            @click="onToggle(j)" />
        </div>
      </li>
      </ul>
      <!-- card t_529628d5: the row anatomy law - the filter HIDES rows,
           it never changes their shape; a query that hides all of them
           reads as a search miss, NOT as the "no jobs configured" empty
           state (that state is the crons.yaml activation story and only
           speaks when the listing itself is empty). -->
      <p
        v-else-if="query.trim()"
        class="panel-status">
        no matching jobs
      </p>
      <!-- card t_6072e718: the empty state is the designed pattern, not a
           bare corner status line - centered in the panel body, the cron
           clock glyph above the headline, and the crons.yaml activation
           hint below it. Activation is a file action, so there is no
           call-to-action button. The constants live in emptyState.ts (the
           composition-test convention: the node suite pins them without
           compiling this SFC). -->
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
  // the empty-state copy lives in its own PURE module (card t_6072e718,
  // the serverRows.ts / boardLogic.ts convention): the node suite pins
  // it without compiling this SFC.
  import { CRON_EMPTY_HINT, CRON_EMPTY_TEXT } from "./emptyState";
  import {
    cronKeyOf,
    setCronJobEnabled,
    type CronJobWire,
  } from "./toggle";
  // card t_529628d5: the band search's filter (name / schedule),
  // pure so the node suite pins it without compiling the SFC.
  import { filterCronJobs } from "./search";
  // card t_ecec864e: the job editor chain (pure module - the toggle
  // convention). The SFC only wires the form state and the buttons.
  import {
    DELIVER_TARGETS,
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
    moveComboIndex,
    optionKnown,
    parseSkills,
    saveCronJob,
    skillsVoice,
    strictSkills,
    validateDraft,
    withSkill,
    withoutSkill,
    type FieldOption,
    type JobDraft,
  } from "./edit";

  // Module-owned UI (card t_db430fea): the cron scheduler's job set,
  // from GET /cron/jobs. v1 shipped read-only (names, schedules,
  // enablement and next fire time); card t_561dc4e0 adds the per-job
  // toggle; card t_ecec864e exposes ALL job definition fields - the
  // editor POSTs the full job (upsert) into the main crons.yaml, the
  // write validated through the loader's own schema law.
  const jobs = ref<(CronJobWire & { kind?: string; minutes?: number; deliver?: string; model?: string; file?: string })[]>([]);
  const loaded = ref(false);
  const error = ref("");

  // card t_529628d5: the in-band search - CLIENT-SIDE on the loaded
  // listing (the filter rides the already-fetched jobs; no new
  // backend route), name or schedule, hide-rows-only.
  const query = ref("");
  const filteredJobs = computed(() => filterCronJobs(jobs.value, query.value));

  // --- the fetch (shared by mount + the band's refresh tool) ---------
  // card t_529628d5: the refresh is GET /cron/jobs re-run - the same
  // fetch chain the mount drove (cron keeps its fetch semantics; the
  // band tool is the Skills/Tools/MCP pattern). A failure lands on the
  // panel's existing error line and the current rows stand (the
  // reload.ts ruling); the refreshing lock drops a second click while
  // a chain is in flight (the in-flight chain answers).
  const refreshing = ref(false);

  // --- the job editor (card t_ecec864e) -------------------------------
  // One editor at a time, keyed by the row it edits (creating = a new
  // job, the name is the only identity). draftOf pre-fills from the
  // wire row; emptyDraft seeds a new one. The two-step delete is its
  // own guard (a DELETE, not a switch: card t_5051d523 killed the
  // toggle's arm step, not the delete's).
  const editor = ref<{
    creating: boolean;
    draft: JobDraft;
    error: string;
    busy: boolean;
    armedDelete: boolean;
  } | null>(null);
  // editorSeq invalidates an in-flight options callback when the open
  // editor changes (a ref's value is a proxy - identity compares lie)
  let editorSeq = 0;

  // --- the field option sources (card t_60e0651e) ---------------------
  // Rulings 1-2: agent and model are COMBOBOXES fed by the agents
  // registry (GET /agents) and the model catalog the settings knobs
  // read (GET /models). Ruling 3: skills is a STRICT multi-select fed
  // by the skills registry (GET /skills). Each source loads once per
  // panel mount (re-armed on a failed answer); a source that is down
  // degrades its ONE control, never the edit.
  type ComboField = "agent" | "model";

  const agentOptions = ref<FieldOption[]>([]);
  const modelOptions = ref<FieldOption[]>([]);
  const skillOptions = ref<FieldOption[]>([]);
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

  // ensureOptions loads the three sources once. A partial failure stays
  // degraded for that ONE control and is retried on the next editor
  // open: optionsReady latches only when every source answered.
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
          applySource(models, modelOptions, "model") &&
          applySource(skills, skillOptions, "skills");
        optionsReady.value = all;
      })
      .finally(() => {
        optionsInflight = null;
      });
    return optionsInflight;
  }

  // --- the comboboxes (rulings 1-2) -----------------------------------
  // One open control at a time; the typed text IS the field's value, so
  // picking is only a shortcut and a value the registry does not list
  // still saves (the never-strand-an-edit law).
  // COMBO_FIELDS drives the ONE combobox block: the agent field (the
  // agents registry) and the model field (the settings knobs' catalog)
  // differ only in source, label and placeholder.
  const COMBO_FIELDS: { field: ComboField; label: string; placeholder: string }[] = [
    { field: "agent", label: "agent", placeholder: "default when empty" },
    {
      field: "model",
      label: "model",
      placeholder: "session model when empty",
    },
  ];

  const combo = ref<{ field: ComboField | null; index: number }>({
    field: null,
    index: -1,
  });
  const comboQuery = ref("");

  // comboRows is the open control's list: its own source, narrowed by
  // what the operator typed into it.
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

  // optionStatus is the empty-list line: a failed fetch names its own
  // error, an honest zero says the source answered with nothing.
  function optionStatus(field: "agent" | "model" | "skills"): string {
    const err = optionErrors.value[field];
    if (err) {
      return field === "skills"
        ? `${err} - skill names come from the registry`
        : `${err} - type it instead`;
    }
    return "no match";
  }

  // unknownNote: a typed value the registry does not list. Free text is
  // legal here, so the note informs without blocking the save.
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

  // --- the skills multi-select (ruling 3) ------------------------------
  // STRICT: options come only from the skills registry, the closed
  // control renders the chosen names as chips, and the panel carries
  // its own search. A job's pinned name the registry cannot serve is
  // dropped from the draft and NAMED on screen - visible, never silent.
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

  // setChips writes the chip list back into the draft's comma voice -
  // the one string saveCronJob parses (the upsert path is untouched).
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

  // applyStrictSkills trims a loaded job to the registry's own set. It
  // runs once the registry answers, so a strict load never races the
  // fetch; an unanswered registry leaves the value exactly as loaded.
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
    // the strict-skills callback below must land on THIS editor only: a
    // sequence token, not an object identity - editor is a ref, so its
    // value is a reactive PROXY and never === the raw object assigned
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
    // the strict skills load needs the registry: trim once it lands,
    // and only while THIS editor is still the open one
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
    const msg = validateDraft(editor.value.draft);
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

  // --- the per-job toggle (card t_561dc4e0) ----------------------------
  // ONE click commits (card t_5051d523: the two-step arm/confirm the
  // mcp tab used to ride is dead - click one armed the row, click two
  // applied, i.e. every job needed a double click). busy holds the
  // committing key so a stacked double-click cannot overlap writes.
  const busy = ref<string | null>(null);

  function onToggle(j: CronJobWire) {
    if (busy.value !== null) return;
    apply(j);
  }

  // the commit: POST the toggle (the `enabled:` flag write lands and
  // the runner hot-reloads), then re-render from the fresh listing.
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

  // --- narrow adaptation, the module-UI recipe (card t_a2fdc630): this
  // root measures its OWN rendered container (the inspector column it
  // currently rides in) and flips the class; the scoped stylesheet
  // below condenses. Same mechanism as the shell's useNarrow - kept
  // in-module because module UI imports no shell code (Board
  // precedent); the threshold is unit-covered in ./narrow.ts.
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = ref(false);
  let resizeObs: ResizeObserver | null = null;
  onMounted(() => {
    if (typeof ResizeObserver === "undefined" || !rootEl.value) return;
    // (same rAF deferral as the shell's useNarrow: the flip changes the
    // observed layout itself, so the callback must not mutate this frame)
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
    // mount measures NOW: a tab remount in place produces no further
    // resize entry, so the class must not wait for the next width
    // change (the shell's useNarrow does the same on mount)
    narrow.value = isNarrowWidth(Math.round(rootEl.value.getBoundingClientRect().width));
  });
  onBeforeUnmount(() => {
    resizeObs?.disconnect();
    resizeObs = null;
  });

  // shortTime renders the RFC3339 next fire as a local, compact
  // date-time ("10/20 08:00") - the raw RFC string is noise in a row
  // that already carries the schedule.
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
  /* card t_308e6d9b: the band anchors the top, the body owns the scroll.
     The old root padding (10px 12px 14px) rides the body now; the
     empty state still centers in the body's full height (card
     t_6072e718). */
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
  /* card t_6072e718: the designed empty state. Flexes to the body's full
     height and centers its glyph + copy both axes; the colors ride the
     shell variables so both themes read right (module UI carries its own
     scoped CSS, no main.css edit). */
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
  /* card t_ecec864e: the job editor panel. It anchors under the band
     (above the scroll body, the band's own law) so edits never scroll
     away; the fields ride a 2-wide grid, the textarea spans both. */
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
  /* the row pencil sits beside the toggle pill on the right edge */
  .cron-edit-ctrl {
    margin-right: 2px;
    flex: none;
    align-self: flex-start;
  }
  /* card t_561dc4e0: the row geometry (paddings, separators, name/sub
     typography, the right-control seat, the confirm note line) lives
     in the shared resources css/registry-row.css anatomy - this block
     keeps only the cron specifics: the disabled dim, the mono
     schedule voice (the row law keeps the expression mono), the
     narrow condensation. */
  .cron-row.disabled {
    opacity: 0.5;
  }
  .cron-meta {
    font-family: var(--mono, monospace);
  }
  /* card t_60e0651e: the three option controls. Hand-rolled (no styling
     library, docs/webui.md); the combobox lists and the skills panel
     float on the z2 layer with the layer shadow, and the chosen skills
     ride the CLOSED control as chips. Tokens only - main.css owns the
     values. */
  .cron-field-note {
    margin: 2px 0 0;
    font-size: 10.5px;
    color: var(--text-dim, #8a8a92);
  }
  .cron-combo {
    position: relative;
    min-width: 0;
  }
  /* the control fills its field (a bare input keeps its intrinsic ~20ch
     width and overflows a narrow grid column) */
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
  /* the skills multi-select: a closed control carrying the chips, and a
     searchable checkbox panel under it */
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
  /* the row checkboxes are SELECTION boxes, not switches (the one-switch
     law covers on/off toggles) - they keep the browser's own box */
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
  /* narrow: the module-UI recipe (card t_a2fdc630). The rows are
     already single-column; what dies first at ~300px is horizontal
     breathing room - the head's name/sub pair wraps and the chrome
     condenses so a long schedule line never elides. */
  .cron-list.narrow .cron-body {
    padding: 4px 8px 10px;
  }
  /* card t_60e0651e: the editor's 2-wide grid collapses at the narrow
     threshold - a combobox list floating inside a half-width column is
     unreadable at ~300px */
  .cron-list.narrow .cron-editor-grid {
    grid-template-columns: 1fr;
  }
  /* card t_561dc4e0: the narrow name shrunk to 11.5px under the old
     bespoke anatomy - the row law's uniform heights carry narrow (the
     shared .narrow .reg-row rule), the name keeps its one size. */
</style>