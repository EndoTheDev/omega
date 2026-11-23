<template>
  <section
    ref="rootEl"
    class="skills-list"
    :class="{ narrow: narrow }">
    <!-- the UNIVERSAL band (shared TabBand): rendered before the scroll body, structurally pinned, never sticky; the in-band search filters the loaded inventory client-side. -->
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
    <!-- the editor panel: one at a time, above the rows, so scrolling never moves it mid-edit. -->
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
      <RegistryList
        v-else
        kind="skills"
        :rows="visibleRows"
        :busy="busy"
        :error="toggleError"
        :loaded="loaded"
        :toggleable="toggleable"
        openable
        :row-menu="rowMenu"
        :empty-text="query.trim() ? 'no matching skills' : 'no skills installed'"
        @toggle="onToggle"
        @open="openSkill">
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
  import { filterSearchableRows } from "./search";
  import type { ContextMenuEntry } from "~/composables/contextMenu";
  import { skillMenuItems } from "./skillMenu";
  // the editor chain is a pure module, like the cron tab's edit.ts.
  import {
    deleteSkill,
    emptyDraft,
    fetchSkillDraft,
    saveSkill,
    validateDraft,
    type SkillDraft,
  } from "./edit";

  const skills = ref<SkillInfo[]>([]);
  const fold = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const busy = ref<string | null>(null);
  const refreshing = ref(false);
  const toggleError = ref<string | null>(null);
  const toggleable = ref(false);

  // rows is a computed, not a function: RegistryList binds it into the template, where a
  // plain function would make `v-if="rows.length"` read the ARITY (0) and never render.
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

  // THE ROW MENU: Open is the row's own click (the viewer tab), Export downloads the
  // folder (GET /skills/export), Enable or Disable is the pill's own write, Archive is
  // POST /skills/archive (reversible: the folder moves into .archive), and Delete arms
  // the editor's one confirm - never a second confirm dialog beside it. The row face is
  // local: module UI imports no shell client.
  function rowMenu(row: { name: string; disabled: boolean; noToggle?: boolean }): ContextMenuEntry[] {
    return skillMenuItems(
      {
        disabled: !!row.disabled,
        toggleable: toggleable.value && !row.noToggle,
      },
      {
        open: () => openSkill(row),
        exportSkill: () => exportSkill(row.name),
        // the target direction IS the row's disabled flag (RegistryList's own emit)
        toggle: () => void onToggle(row.name, !!row.disabled),
        archive: () => void archiveSkill(row.name),
        remove: () => void startEdit(row.name, true),
      },
    );
  }

  // exportSkill is the browser's own download: an anchor to the route the tab's menu
  // names, never a fetch whose bytes the page would have to hold.
  function exportSkill(name: string): void {
    const a = document.createElement("a");
    a.href = `/skills/export?name=${encodeURIComponent(name)}`;
    a.download = `${name}.tar.gz`;
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  // archiveSkill moves the folder out of the listing; the answer is the fresh inventory
  // (the editor routes' one-response contract), so the row disappears without a refetch.
  async function archiveSkill(name: string): Promise<void> {
    if (busy.value !== null) return;
    busy.value = name;
    try {
      const res = await fetch("/skills/archive", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name }),
      });
      if (!res.ok) throw new Error(await res.text());
      if (editor.value && editor.value.draft.name === name) editor.value = null;
      await load();
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      busy.value = null;
    }
  }

  async function load(): Promise<void> {
    applyInventory(await refreshSkills());
  }

  // a null field means that read was not carried; keep the local value, never clear it.
  function applyInventory(u: Awaited<ReturnType<typeof refreshSkills>>): void {
    skills.value = u.skills;
    if (u.fold !== null) fold.value = u.fold;
    if (u.toggleable !== null) toggleable.value = u.toggleable;
  }

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

  async function startEdit(name: string, armedDelete = false): Promise<void> {
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
      // the row menu's Delete lands on the editor's own confirm: arm only once the
      // draft really loaded, so a failed fetch can never leave a delete holding ""
      if (armedDelete) editor.value.armedDelete = true;
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

  onMounted(async () => {
    try {
      await load();
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
  });

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
  /* the editor panel: anchored under the band, above the scrolling body. */
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