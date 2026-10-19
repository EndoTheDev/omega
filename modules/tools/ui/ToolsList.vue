<template>
  <section class="tools-list">
    <h3 class="panel-title">tools</h3>
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
      v-else-if="tools.length"
      class="tools-inventory">
      <li
        v-for="t in tools"
        :key="t.name"
        class="tools-row">
        <span class="tools-name">{{ t.name }}</span>
        <span class="tools-desc">{{ t.description }}</span>
      </li>
    </ul>
    <p
      v-else
      class="panel-status">
      no tools registered
    </p>
  </section>
</template>

<script setup lang="ts">
  import { onMounted, ref } from "vue";

  // Module-owned UI canary: the tool inventory, sourced from GET /tools
  // and shipped by the tools module itself (modules/tools/ui/) through
  // the module-UI composition. This is the seed of the future tools tab.
  // The fetch is deliberately self-contained - the shell's API client
  // is shell code; a module's UI carries its own wiring so it can ride
  // the desktop / VS Code builds alone.
  interface ToolInfo {
    name: string;
    description: string;
  }
  const tools = ref<ToolInfo[]>([]);
  const loaded = ref(false);
  const error = ref("");

  onMounted(async () => {
    try {
      const res = await fetch("/tools");
      if (!res.ok) throw new Error(`GET /tools: ${res.status}`);
      tools.value = await res.json();
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    }
    loaded.value = true;
  });
</script>

<style scoped>
  /* Module-owned look built on the shell's design tokens - the section
     is self-titled; the inspector mounts it as any other panel. */
  .tools-list {
    padding: 10px 12px 14px;
  }
  .panel-title {
    margin: 0 0 4px;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    opacity: 0.5;
  }
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
  .tools-inventory {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .tools-row {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 5px 0;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
  }
  .tools-row:last-child {
    border-bottom: none;
  }
  .tools-name {
    font-size: 12px;
    font-weight: 600;
    opacity: 0.9;
  }
  .tools-desc {
    font-size: 11px;
    opacity: 0.6;
  }
</style>