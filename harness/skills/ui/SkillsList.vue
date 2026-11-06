<template>
  <section
    ref="rootEl"
    class="skills-list"
    :class="{ narrow: narrow }">
    <!-- card t_308e6d9b: the UNIVERSAL band (the shared TabBand) - title
         left, the refresh action right, ONE row. The click re-runs the
         fetch chain: GET /skills is a live scan, so a skill added after
         mount appears - no restart. The band renders BEFORE the scroll
         body (structurally pinned, never sticky): scrolling the list
         never moves it. Card t_529628d5: the compact in-band search
         (TabBand's shared .band-search seat) - the query filters the
         LOADED inventory by name or description, client-side,
         hide-rows-only. -->
    <TabBand title="Skills">
      <div class="band-search">
        <SearchInput
          v-model="query"
          placeholder="Search skills" />
      </div>
      <button
        class="band-tool"
        title="refresh"
        aria-label="refresh skills"
        :disabled="refreshing"
        @click="refresh">
        <Icon
          name="refresh"
          :size="13" />
      </button>
      <!-- card t_0a108f16: the tab becomes a skill editor - the plus
           tool opens a new-skill form; each row carries its own pencil
           (the CronJobs editor's shape: panel above the rows, save
           through POST /skills/save, two-step delete armed inside the
           panel). -->
      <button
        class="band-tool"
        title="add skill"
        aria-label="add skill"
        @click="startNew">
        <Icon
          name="plus"
          :size="13" />
      </button>
    </TabBand>
    <!-- the editor panel: one at a time, above the rows (the CronJobs
         editor's place - scrolling never moves it mid-edit). -->
    <div
      v-if="editor"
      class="skill-editor">
      <p class="skill-editor-title">{{ editor.creating ? "add skill" : `edit ${editor.draft.name}` }}</p>
      <div
        v-if="editor.loadingDraft"
        class="skill-editor-loading">loading skill...</div>
      <template v-else>
        <div class="skill-editor-grid">
          <label class="skill-editor-field">
            <span>name</span>
            <!-- the name IS the identity (the folder-name law): an
                 editing row keeps its name, only a create names one. -->
            <input
              v-model="editor.draft.name"
              :disabled="!editor.creating"
              placeholder="my-skill" />
          </label>
          <label class="skill-editor-field">
            <span>description</span>
            <input
              v-model="editor.draft.description"
              placeholder="one line - the Available Skills entry" />
          </label>
          <label class="skill-editor-field skill-editor-wide">
            <span>body</span>
            <textarea
              v-model="editor.draft.content"
              rows="10"
              placeholder="the markdown under the frontmatter fence (no leading ---)" />
          </label>
        </div>
        <p
          v-if="editor.error"
          class="skill-editor-error">{{ editor.error }}</p>
        <div class="skill-editor-actions">
          <button
            class="skill-editor-btn primary"
            :disabled="editor.busy"
            @click="commitEditor">
            {{ editor.busy ? "saving..." : "save skill" }}
          </button>
          <button
            class="skill-editor-btn"
            :disabled="editor.busy"
            @click="cancelEditor">
            cancel
          </button>
          <button
            v-if="!editor.creating"
            class="skill-editor-btn danger"
            :disabled="editor.busy"
            @click="onDeleteFromEditor">
            {{ editor.armedDelete ? "confirm delete?" : "delete skill" }}
          </button>
        </div>
      </template>
    </div>
    <div class="skills-body">
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
      <!-- v0.14.1 registry-browser: the shared RegistryList (one pattern,
           three consumers). The toggle drives PUT /disabled/skills; a
           disabled skill stays in the inventory but leaves the session
           prompt build (config.yaml disabled.skills). -->
      <RegistryList
        v-else
        kind="skills"
        :rows="visibleRows"
        :busy="busy"
        :error="toggleError"
        :loaded="loaded"
        :toggleable="toggleable"
        openable
        :empty-text="query.trim() ? 'no matching skills' : 'no skills installed'"
        @toggle="onToggle"
        @open="openSkill">
        <!-- card t_0a108f16: the row's edit affordance - a pencil per
             row opening the editor pre-filled with the skill (the
             CronJobs pencil, on the shared row side seat). -->
        <template #side="{ row }">
          <button
            class="band-tool skill-edit-ctrl"
            type="button"
            :aria-label="`edit skill ${row.name}`"
            @click="startEdit(row.name)">
            <Icon
              name="pencil"
              :size="13" />
          </button>
        </template>
      </RegistryList>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  import { refreshSkills, type SkillInfo } from "./refresh";
  // card t_529628d5: the band search's filter (name / description),
  // pure so the node suite pins it without compiling the SFC.
  import { filterSearchableRows } from "./search";
  // card t_0a108f16: the skill editor chain (pure module - the cron
  // edit.ts convention). The SFC only wires the form state and the
  // buttons; the validation and the wire shape live in ./edit.ts.
  import {
    deleteSkill,
    emptyDraft,
    fetchSkillDraft,
    saveSkill,
    validateDraft,
    type SkillDraft,
  } from "./edit";

  // Module-owned UI (card t_db430fea): the loaded skill inventory,
  // from GET /skills, plus the per-skill enable/disable toggle
  // (v0.14.1) against the /disabled fold. Self-contained wiring: a
  // module's UI carries its own wiring so it can ride the desktop /
  // VS Code builds alone (ToolsList precedent) - the fetch chain
  // lives in ./refresh.ts (the reload.ts precedent, card t_d6e94e79)
  // and the SHARED thing is the RegistryList component.
  const skills = ref<SkillInfo[]>([]);
  const fold = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const busy = ref<string | null>(null);
  // the refresh button's lock: a second click while a chain is in
  // flight is dropped (the in-flight chain answers)
  const refreshing = ref(false);
  const toggleError = ref<string | null>(null);
  // false when the host serves no toggle seam (501 on /disabled): the
  // list stays, read-only.
  const toggleable = ref(false);

  // rows are a computed (the AgentsPanel shape): RegistryList binds
  // `rows` straight into the template, where an unwrap gives the array
  // - a plain function would make the `v-if="rows.length"` gate read
  // the ARITY (0), and the list would never render (the t_504a78ca
  // bug: data fetched, tabs empty). Structural RegistryRow: module UI
  // imports no shell code, so the type rides inference.
  const rows = computed(() => {
    const set = new Set(fold.value);
    return skills.value.map((s) => ({
      name: s.name,
      description: s.description,
      disabled: set.has(s.name),
    }));
  });

  // card t_529628d5: the in-band search - CLIENT-SIDE on the loaded
  // inventory (no new backend route), name or description,
  // hide-rows-only (the row anatomy law: the filtered survivors keep
  // their exact row shape).
  const query = ref("");
  const visibleRows = computed(() => filterSearchableRows(rows.value, query.value));

  // --- the viewer open (card t_36ceb99b): a name click opens the skill
  // in a main-pane viewer tab. Module UI imports no shell code (the
  // nuxt.config comment; the Board precedent), so the open rides a window
  // CustomEvent the shell app listens on. THE LITERAL IS COPIED from
  // composables/viewer.ts (VIEWER_OPEN_EVENT) - deliberately, like
  // narrow.ts; the viewer test pins both spellings identical.
  const VIEWER_OPEN_EVENT = "omega:open-viewer";

  interface SkillRow {
    name: string;
  }

  function openSkill(r: SkillRow): void {
    window.dispatchEvent(
      new CustomEvent(VIEWER_OPEN_EVENT, {
        detail: { kind: "skill", name: r.name },
      }),
    );
  }

  async function load(): Promise<void> {
    applyInventory(await refreshSkills());
  }

  // server truth lands on the tab: the skills list always (the caller
  // only gets a resolved update when GET /skills succeeded), the fold
  // and the toggle flag only when the /disabled read landed - a miss
  // (501, unreachable, refusal) keeps the shown fold stable.
  function applyInventory(u: Awaited<ReturnType<typeof refreshSkills>>): void {
    skills.value = u.skills;
    if (u.fold !== null) fold.value = u.fold;
    if (u.toggleable !== null) toggleable.value = u.toggleable;
  }

  // the refresh (card t_ec57d5b8): re-run the fetch chain without a
  // remount - GET /skills is a live scan, so a skill added after
  // mount appears. A failure lands on the panel's existing error line
  // and the current rows stand; the fold re-read (or the same miss
  // rules as the mount) keeps the enabled/disabled state stable.
  async function refresh(): Promise<void> {
    if (refreshing.value) return;
    refreshing.value = true;
    try {
      applyInventory(await refreshSkills());
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      refreshing.value = false;
    }
  }

  // --- the skill editor (card t_0a108f16) -------------------------------
  // One editor at a time (the CronJobs editor's shape): creating = a
  // new skill, else the row's name pre-fills the draft through GET
  // /skills/content. The name input locks while editing - the folder
  // name IS the identity. The delete rides the armed two-step mechanic
  // the toggle carries.
  const editor = ref<{
    creating: boolean;
    loadingDraft: boolean;
    draft: SkillDraft;
    error: string;
    busy: boolean;
    armedDelete: boolean;
  } | null>(null);

  function startNew(): void {
    editor.value = {
      creating: true,
      loadingDraft: false,
      draft: emptyDraft(),
      error: "",
      busy: false,
      armedDelete: false,
    };
  }

  async function startEdit(name: string): Promise<void> {
    editor.value = {
      creating: false,
      loadingDraft: true,
      draft: emptyDraft(),
      error: "",
      busy: false,
      armedDelete: false,
    };
    try {
      editor.value.draft = await fetchSkillDraft(name);
    } catch (e) {
      if (editor.value) {
        editor.value.error = e instanceof Error ? e.message : String(e);
      }
    } finally {
      if (editor.value) editor.value.loadingDraft = false;
    }
  }

  function cancelEditor(): void {
    editor.value = null;
  }

  // the commit: validate against the mirrored Go law first (the editor
  // never ships a skill the provider would reject), POST, and land the
  // response's fresh listing on the rows. A failure keeps the editor
  // open with the refusal in its error line.
  async function commitEditor(): Promise<void> {
    if (!editor.value || editor.value.busy) return;
    const msg = validateDraft(editor.value.draft);
    if (msg) {
      editor.value.error = msg;
      return;
    }
    editor.value.busy = true;
    editor.value.error = "";
    try {
      skills.value = await saveSkill(editor.value.draft);
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

  // the panel's delete: click 1 arms ("confirm delete?"), click 2
  // removes - backed up + receipted server-side, so the answer is the
  // listing the folder left. Armed clears whether the write lands.
  async function onDeleteFromEditor(): Promise<void> {
    if (!editor.value || editor.value.busy) return;
    if (!editor.value.armedDelete) {
      editor.value.armedDelete = true;
      return;
    }
    editor.value.busy = true;
    try {
      if (editor.value.draft.name) {
        skills.value = await deleteSkill(editor.value.draft.name);
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

  // --- narrow adaptation, the module-UI recipe (card t_a2fdc630): this
  // root measures its OWN rendered container and flips the class; the
  // scoped stylesheet below condenses. Same mechanism as the shell's
  // useNarrow - kept in-module because module UI imports no shell code
  // (Board precedent); the threshold is unit-covered in ./narrow.ts.
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

  onMounted(async () => {
    try {
      await load();
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
  });

  // the toggle: PUT answers the fresh fold - rows rebuild so the
  // persisted (server-owned) state is what the tab shows. A failed
  // toggle refreshes anyway: the error line stands over the truth.
  async function onToggle(name: string, enabled: boolean): Promise<void> {
    if (busy.value !== null) return;
    busy.value = name;
    toggleError.value = null;
    try {
      const res = await fetch("/disabled/skills", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, enabled }),
      });
      if (!res.ok) throw new Error(await res.text());
    } catch (e) {
      toggleError.value = e instanceof Error ? e.message : String(e);
    } finally {
      try {
        await load();
      } catch {
        // the rows stay; the error line stands
      }
      busy.value = null;
    }
  }
</script>

<style scoped>
  /* card t_308e6d9b: the root stops being a flat scroll child - the
     TabBand stays fixed, the body owns the scroll (structurally, never
     sticky). The old root padding rides the body now. */
  .skills-list {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .skills-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 4px 12px 14px;
  }
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
  /* card t_0a108f16: the editor panel - the CronJobs editor's shape
     (anchored under the band, above the scroll body, a 2-wide field
     grid the body textarea spans). */
  .skill-editor {
    padding: 8px 12px;
    border-bottom: 1px solid var(--border, rgba(128, 128, 128, 0.25));
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .skill-editor-title {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    opacity: 0.85;
  }
  .skill-editor-loading {
    font-size: 11px;
    opacity: 0.6;
  }
  .skill-editor-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px 10px;
  }
  .skill-editor-field {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    font-size: 11px;
  }
  .skill-editor-field.skill-editor-wide {
    grid-column: 1 / -1;
  }
  .skill-editor-field span {
    opacity: 0.6;
  }
  .skill-editor-field input,
  .skill-editor-field textarea {
    font: inherit;
    color: inherit;
    background: var(--bg-elev, rgba(128, 128, 128, 0.08));
    border: 1px solid var(--border, rgba(128, 128, 128, 0.25));
    border-radius: 4px;
    padding: 3px 6px;
    min-width: 0;
    box-sizing: border-box;
  }
  .skill-editor-field textarea {
    font-family: var(--mono, monospace);
    font-size: 11px;
    resize: vertical;
  }
  .skill-editor-error {
    margin: 0;
    font-size: 11px;
    color: var(--danger, #e5484d);
  }
  .skill-editor-actions {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .skill-editor-btn {
    font: inherit;
    font-size: 11px;
    color: inherit;
    background: none;
    border: 1px solid var(--border, rgba(128, 128, 128, 0.25));
    border-radius: 4px;
    padding: 3px 10px;
    cursor: pointer;
  }
  .skill-editor-btn:hover {
    border-color: var(--text, currentColor);
  }
  .skill-editor-btn.primary {
    background: var(--bg-elev, rgba(128, 128, 128, 0.12));
  }
  .skill-editor-btn.danger {
    color: var(--danger, #e5484d);
  }
  /* the row pencil sits beside the toggle pill on the right edge */
  .skill-edit-ctrl {
    margin-right: 2px;
    flex: none;
    align-self: flex-start;
  }
  .skills-list.narrow .skills-body {
    padding: 4px 8px 10px;
  }
</style>