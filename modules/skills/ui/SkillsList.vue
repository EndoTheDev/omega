<template>
  <section class="skills-list">
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
  import { onMounted, ref } from "vue";

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
</style>