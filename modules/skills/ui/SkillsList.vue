<template>
  <section
    ref="rootEl"
    class="skills-list"
    :class="{ narrow: narrow }">
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
      empty-text="no skills installed"
      @toggle="onToggle" />
  </section>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";

  // Module-owned UI (card t_db430fea): the loaded skill inventory,
  // from GET /skills, plus the per-skill enable/disable toggle
  // (v0.14.1) against the /disabled fold. Self-contained fetch: a
  // module's UI carries its own wiring so it can ride the desktop /
  // VS Code builds alone (ToolsList precedent) - the flag merge and
  // the toggle PUT are small deliberate copies (the narrow.ts copy
  // law), the SHARED thing is the RegistryList component.
  interface SkillInfo {
    name: string;
    description: string;
  }
  const skills = ref<SkillInfo[]>([]);
  const fold = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const busy = ref<string | null>(null);
  const toggleError = ref<string | null>(null);
  // false when the host serves no toggle seam (501 on /disabled): the
  // list stays, read-only.
  const toggleable = ref(false);

  function rows() {
    const set = new Set(fold.value);
    return skills.value.map((s) => ({ name: s.name, description: s.description, disabled: set.has(s.name) || false }));
  }

  async function load(): Promise<void> {
    const res = await fetch("/skills");
    if (!res.ok) throw new Error(`GET /skills: ${res.status}`);
    skills.value = await res.json();
    try {
      const dRes = await fetch("/disabled");
      if (dRes.status === 501) {
        toggleable.value = false; // no seam: read-only
      } else if (dRes.ok) {
        const foldBody = (await dRes.json())?.skills;
        fold.value = Array.isArray(foldBody)
          ? foldBody.filter((n): n is string => typeof n === "string")
          : [];
        toggleable.value = true;
      }
    } catch {
      // /disabled unreachable is not fatal to the LIST; stay read-only
      // rather than guessing the fold state
      toggleable.value = false;
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
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
  .skills-list.narrow {
    padding: 6px 8px 10px;
  }
</style>