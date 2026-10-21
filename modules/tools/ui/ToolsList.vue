<template>
  <section
    ref="rootEl"
    class="tools-list"
    :class="{ narrow: narrow }">
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
        <!-- Domain tool: dotted sub-rows, each action with its own
             one-liner (memory.add / files.write / task.create, ...) -->
        <ul
          v-if="t.actions && t.actions.length"
          class="tools-actions">
          <li
            v-for="a in t.actions"
            :key="a.name"
            class="tools-action">
            <span class="tools-action-name">{{ t.name }}.{{ a.name }}</span>
            <span class="tools-desc">{{ a.description }}</span>
          </li>
        </ul>
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
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";

  // Module-owned UI canary (t_d984b493), now the inspector's Tools tab
  // content (card t_db430fea): the tool inventory, sourced from
  // GET /tools and shipped by the tools module itself
  // (modules/tools/ui/) through the module-UI composition. A tool with
  // an actions[] array is a domain tool: its row is the domain context,
  // and each action renders as a dotted sub-row (t_73994e55 action
  // protocol) with its own description - one metadata source feeds
  // both this panel and the system prompt.
  // The fetch is deliberately self-contained - the shell's API client
  // is shell code; a module's UI carries its own wiring so it can ride
  // the desktop / VS Code builds alone.
  interface ToolAction {
    name: string;
    description: string;
  }
  interface ToolInfo {
    name: string;
    description: string;
    actions?: ToolAction[];
  }
  const tools = ref<ToolInfo[]>([]);
  const loaded = ref(false);
  const error = ref("");

  // --- narrow adaptation, the module-UI recipe (card t_a2fdc630): this
  // root measures its OWN rendered container and flips the class; the
  // scoped stylesheet below condenses the domain-tool sub-row
  // indentation too (it is the widest thing here at ~300px). Same
  // mechanism as the shell's useNarrow - kept in-module because module
  // UI imports no shell code (Board precedent); the threshold is
  // unit-covered in ./narrow.ts.
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
  });
  onBeforeUnmount(() => {
    resizeObs?.disconnect();
    resizeObs = null;
  });

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
  .tools-actions {
    list-style: none;
    margin: 2px 0 0 10px;
    padding: 0;
  }
  .tools-action {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 2px 0;
  }
  .tools-action-name {
    font-size: 11px;
    font-weight: 600;
    opacity: 0.85;
  }
  .tools-action-name::before {
    content: "\00b7\2002";
    font-weight: 400;
    opacity: 0.4;
  }
  /* narrow: the module-UI recipe (card t_a2fdc630) - condensed chrome:
     the action sub-rows lose most of their indent (memory.add style
     dotted paths keep the domain readable without it) and the title
     drops a step. */
  .tools-list.narrow {
    padding: 6px 8px 10px;
  }
  .tools-list.narrow .tools-actions {
    margin-left: 4px;
  }
  .tools-list.narrow .tools-name {
    font-size: 11.5px;
  }
</style>