<template>
  <section
    ref="rootEl"
    class="skills-list"
    :class="{ narrow: narrow }">
    <!--
      the UNIVERSAL band (the shared TabBand).
      The band renders BEFORE the scroll body (structurally pinned, never sticky):
      scrolling the list never moves it.: the compact in-band search (TabBand's
      shared.band-search seat) - the query filters the LOADED inventory by name or
      description, client-side, hide-rows-only.
    -->
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
      <!-- the tab becomes a skill editor. -->
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
    <!--
      the editor panel: one at a time, above the rows (the CronJobs editor's place -
      scrolling never moves it mid-edit).
    -->
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
            <!-- the name IS the identity (the folder-name law). -->
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
      <!-- v0.14.1 registry-browser: the shared RegistryList (one pattern, three. -->
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
        <!-- the row's edit affordance. -->
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
  // the band search's filter (name / description), pure so the node suite pins it.
  import { filterSearchableRows } from "./search";
  // the skill editor chain (pure module - the cron edit.ts convention).
  import {
    deleteSkill,
    emptyDraft,
    fetchSkillDraft,
    saveSkill,
    validateDraft,
    type SkillDraft,
  } from "./edit";

  // Module-owned UI: the loaded skill inventory, from GET /skills, plus the per-skill.
  const skills = ref<SkillInfo[]>([]);
  const fold = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const busy = ref<string | null>(null);
  // the refresh button's lock.
  const refreshing = ref(false);
  const toggleError = ref<string | null>(null);
  // false when the host serves no toggle seam (501 on /disabled).
  const toggleable = ref(false);

  // rows are a computed (the AgentsPanel shape): RegistryList binds `rows` straight
  // into the template, where an unwrap gives the array - a plain function would make
  // the `v-if="rows.length"` gate read the ARITY (0), and the list would never render.
  const rows = computed(() => {
    const set = new Set(fold.value);
    return skills.value.map((s) => ({
      name: s.name,
      description: s.description,
      disabled: set.has(s.name),
    }));
  });

  // the in-band search - CLIENT-SIDE on the loaded inventory (no new backend route).
  const query = ref("");
  const visibleRows = computed(() => filterSearchableRows(rows.value, query.value));

  // -- the viewer open: a name click opens the skill in a main-pane viewer tab.
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

  // server truth lands on the tab: the skills list always (the caller only gets a.
  function applyInventory(u: Awaited<ReturnType<typeof refreshSkills>>): void {
    skills.value = u.skills;
    if (u.fold !== null) fold.value = u.fold;
    if (u.toggleable !== null) toggleable.value = u.toggleable;
  }

  // the refresh: re-run the fetch chain without a remount.
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

  // -- the skill editor ------------------------------- One editor at a time (the
  // CronJobs editor's shape).
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

  // the commit: validate against the mirrored Go law first (the editor never ships a.
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

  // -- narrow adaptation, the module-UI recipe.
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

  // the toggle: PUT answers the fresh fold.
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
      }
      busy.value = null;
    }
  }
</script>

<style scoped>
  /*
   * the root stops being a flat scroll child - the TabBand stays fixed, the body owns
   * the scroll (structurally, never sticky).
   */
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
  /* the editor panel - the CronJobs editor's shape (anchored under the band, above. */
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
  .skill-edit-ctrl {
    margin-right: 2px;
    flex: none;
    align-self: flex-start;
  }
  .skills-list.narrow .skills-body {
    padding: 4px 8px 10px;
  }
</style>