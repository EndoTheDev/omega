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
    <ul
      v-else-if="skills.length"
      class="skills-inventory">
      <li
        v-for="s in skills"
        :key="s.name"
        class="skills-row">
        <span class="skills-name">{{ s.name }}</span>
        <span class="skills-desc">{{ s.description }}</span>
      </li>
    </ul>
    <p
      v-else
      class="panel-status">
      no skills installed
    </p>
  </section>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";

  // Module-owned UI (card t_db430fea): the loaded skill inventory,
  // from GET /skills. READ-ONLY v1 - name + description rows only; the
  // reader tool (skills.read) remains the way into a skill's content.
  // Self-contained fetch: a module's UI carries its own wiring so it
  // can ride the desktop / VS Code builds alone (ToolsList precedent).
  interface SkillInfo {
    name: string;
    description: string;
  }
  const skills = ref<SkillInfo[]>([]);
  const loaded = ref(false);
  const error = ref("");

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
      const res = await fetch("/skills");
      if (!res.ok) throw new Error(`GET /skills: ${res.status}`);
      skills.value = await res.json();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
  });
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
  .skills-inventory {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .skills-row {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 5px 0;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
  }
  .skills-row:last-child {
    border-bottom: none;
  }
  .skills-name {
    font-size: 12px;
    font-weight: 600;
    opacity: 0.9;
  }
  .skills-desc {
    font-size: 11px;
    opacity: 0.6;
  }
  /* narrow: the module-UI recipe (card t_a2fdc630). Name + description
     already stack vertically; the adaptation is condensed chrome - at
     ~300px the wide row padding reads as dead space. */
  .skills-list.narrow {
    padding: 6px 8px 10px;
  }
  .skills-list.narrow .skills-row {
    padding: 4px 0;
  }
  .skills-list.narrow .skills-name {
    font-size: 11.5px;
    /* skill names are dotted paths (skills.read paths): long ones only
       ellipsize when the row is measured-narrow */
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>