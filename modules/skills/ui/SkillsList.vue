<template>
  <section
    ref="rootEl"
    class="skills-list"
    :class="{ narrow: narrow }">
    <!-- card t_ec57d5b8: the toolbar row, the FilesTab pattern - the
         title flush LEFT, the refresh action flush RIGHT on ONE row
         (the same -8px dock-pane tuck the Files header clears, so the
         same 14px lead-in). The click re-runs the fetch chain: GET
         /skills is a live scan, so a skill added after mount appears -
         no restart. -->
    <div
      class="skills-header"
      role="toolbar"
      aria-label="skills actions">
      <h3 class="panel-title">skills</h3>
      <button
        class="skills-tool"
        title="refresh"
        aria-label="refresh skills"
        :disabled="refreshing"
        @click="refresh">
        <Icon
          name="refresh"
          :size="13" />
      </button>
    </div>
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
      :rows="rows"
      :busy="busy"
      :error="toggleError"
      :loaded="loaded"
      :toggleable="toggleable"
      openable
      empty-text="no skills installed"
      @toggle="onToggle"
      @open="openSkill" />
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  import { refreshSkills, type SkillInfo } from "./refresh";

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
  .skills-list {
    padding: 10px 12px 14px;
  }
  /* the toolbar row (card t_ec57d5b8): title left, refresh flush right,
     one row - the FilesTab header pattern with the module's own copy
     (module UI imports no shell code, the narrow.ts copy law). The
     14px lead-in clears the same -8px dock-pane tuck the Files header
     clears (card t_5c660a42). */
  .skills-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: 14px 0 4px;
  }
  .panel-title {
    margin: 0;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    opacity: 0.5;
  }
  .skills-tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 20px;
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--text-dim);
    border-radius: var(--r-chip);
    cursor: pointer;
  }
  .skills-tool:hover:not(:disabled) {
    opacity: 0.85;
  }
  .skills-tool:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
  .skills-list.narrow {
    padding: 6px 8px 10px;
  }
</style>