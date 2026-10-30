<template>
  <section
    ref="rootEl"
    class="mcp-list"
    :class="{ narrow: narrow }">
    <!-- card t_d6e94e79: the toolbar row, the FilesTab pattern - the
         title flush LEFT, the refresh action flush RIGHT on ONE row.
         card t_308e6d9b: the row is the UNIVERSAL band now (the shared
         TabBand). The click makes mcp.yaml edits live (POST
         /mcp/reload + the re-fetch), no restart, no file watcher - the
         button is the whole trigger, and it lives in the band. -->
    <TabBand title="MCP">
      <button
        class="band-tool"
        title="refresh"
        aria-label="refresh mcp servers"
        :disabled="reloading"
        @click="reload">
        <Icon
          name="refresh"
          :size="13" />
      </button>
    </TabBand>
    <div class="mcp-body">
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
    <!-- card t_c7136c50: the empty state - the standard pattern, never
         a blank pane. Zero servers configured reads as WHAT is unset
         plus WHERE to set it (mcp.yaml's servers: array / MCP_SERVERS). -->
    <div
      v-else-if="rows.length === 0"
      class="mcp-empty">
      <p class="mcp-empty-text">{{ MCP_EMPTY_TEXT }}</p>
      <p class="mcp-empty-hint">{{ MCP_EMPTY_HINT }}</p>
    </div>
    <template v-else>
      <p class="panel-status">
        from {{ sourceLabel(source) }}
      </p>
      <ul class="mcp-servers">
        <li
          v-for="s in rows"
          :key="s.name"
          class="mcp-server">
          <div class="mcp-server-head">
            <span class="mcp-server-name">{{ s.name }}</span>
            <span
              class="mcp-transport"
              title="transport">{{ s.transport || "unconfigured" }}</span>
            <span
              class="mcp-status"
              :class="{ ok: s.connected && !s.error }">{{ statusLabel(s) }}</span>
          </div>
          <p
            v-if="s.url || s.command"
            class="mcp-target">
            {{ s.url || s.command }}
          </p>
          <ul
            v-if="s.tools && s.tools.length"
            class="mcp-tools">
            <li
              v-for="t in s.tools"
              :key="t"
              class="mcp-tool">
              {{ t }}
            </li>
          </ul>
          <p
            v-else
            class="panel-status">
            no tools discovered
          </p>
        </li>
      </ul>
    </template>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  import { reloadMcpServers } from "./reload";
  import {
    MCP_EMPTY_HINT,
    MCP_EMPTY_TEXT,
    serverRows,
    sourceLabel,
    statusLabel,
    type McpStatusWire,
  } from "./serverRows";

  // Module-owned UI (card t_c7136c50): the inspector's MCP tab content,
  // sourced from GET /mcp/servers and shipped by the mcp module itself
  // (modules/mcp/ui/) through the module-UI composition - the same
  // shape the tools/skills/cron panels ride. Read-only listing; the
  // one write path is the refresh button (card t_d6e94e79): POST
  // /mcp/reload then the re-fetch, so mcp.yaml edits go live with no
  // server restart. Self-contained wiring (module UI carries its own
  // wiring, the narrow.ts copy law).
  const source = ref<string | undefined>(undefined);
  const rows = ref(serverRows({}));
  const loaded = ref(false);
  const error = ref("");
  const reloading = ref(false);

  async function load(): Promise<void> {
    const res = await fetch("/mcp/servers");
    if (!res.ok) throw new Error(`GET /mcp/servers: ${res.status}`);
    const rep: McpStatusWire = await res.json();
    source.value = rep.source;
    rows.value = serverRows(rep);
  }

  // the refresh (card t_d6e94e79): POST the reload route, then show
  // the fresh listing. A failure lands on the panel's existing error
  // line; the previous rows stand (server truth never fakes a swap).
  async function reload(): Promise<void> {
    if (reloading.value) return;
    reloading.value = true;
    try {
      const rep = await reloadMcpServers();
      source.value = rep.source;
      rows.value = serverRows(rep);
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      reloading.value = false;
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
</script>

<style scoped>
  /* Module-owned look built on the shell's design tokens - the section
     is self-titled; the inspector mounts it as any other panel. No
     invented vars (the t_11a9e160 lesson): opacity + the type scale
     the sibling panels (ToolsList, SkillsList) already speak. */
  /* card t_308e6d9b: the root stops being a flat scroll child - the
     TabBand stays fixed, the body owns the scroll (structurally, never
     sticky). The old root padding rides the body now. */
  .mcp-list {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .mcp-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 4px 12px 14px;
  }
  .panel-status {
    font-size: 11px;
    opacity: 0.6;
  }
  .mcp-tool {
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
  .mcp-tool:hover:not(:disabled) {
    opacity: 0.85;
  }
  .mcp-tool:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .mcp-empty {
    margin-top: 6px;
  }
  .mcp-empty-text {
    margin: 0;
    font-size: 12px;
    opacity: 0.75;
  }
  .mcp-empty-hint {
    margin: 2px 0 0;
    font-size: 11px;
    opacity: 0.55;
  }
  .mcp-servers {
    list-style: none;
    margin: 6px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .mcp-server {
    border: 1px solid currentColor;
    border-radius: 6px;
    padding: 6px 8px;
    opacity: 0.9;
  }
  .mcp-server-head {
    display: flex;
    align-items: baseline;
    gap: 6px;
    flex-wrap: wrap;
  }
  .mcp-server-name {
    font-size: 12px;
    font-weight: 600;
  }
  .mcp-transport {
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    opacity: 0.55;
  }
  .mcp-status {
    margin-left: auto;
    font-size: 10px;
    opacity: 0.7;
    text-align: right;
    overflow-wrap: anywhere;
  }
  .mcp-status.ok {
    opacity: 0.9;
  }
  .mcp-target {
    margin: 2px 0 0;
    font-size: 10px;
    opacity: 0.5;
    overflow-wrap: anywhere;
  }
  .mcp-tools {
    list-style: none;
    margin: 4px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .mcp-tool {
    font-size: 11px;
    opacity: 0.75;
    overflow-wrap: anywhere;
  }
  /* narrow: the module-UI recipe (card t_a2fdc630) - condensed chrome:
     the status line drops under the name instead of floating right. */
  .mcp-list.narrow {
    padding: 0;
  }
  .mcp-list.narrow .mcp-body {
    padding: 4px 8px 10px;
  }
  .mcp-list.narrow .mcp-server-head {
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
  }
  .mcp-list.narrow .mcp-status {
    margin-left: 0;
    text-align: left;
  }
</style>