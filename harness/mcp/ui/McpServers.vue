<template>
  <section
    ref="rootEl"
    class="mcp-list"
    :class="{ narrow: narrow }">
    <!-- the toolbar row, the FilesTab pattern. -->
    <TabBand title="MCP">
      <div class="band-search">
        <SearchInput
          v-model="query"
          placeholder="Search MCP servers" />
      </div>
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
    <!-- the empty state - the standard pattern, never a blank pane. -->
    <div
      v-else-if="rows.length === 0 && !query.trim()"
      class="mcp-empty">
      <p class="mcp-empty-text">{{ MCP_EMPTY_TEXT }}</p>
      <p class="mcp-empty-hint">{{ MCP_EMPTY_HINT }}</p>
    </div>
    <template v-else>
      <!--
        the row anatomy law - the filter HIDES rows, it never changes their shape.
      -->
      <p
        v-if="visibleRows.length === 0"
        class="panel-status mcp-none-matches">
        no matching servers
      </p>
      <!-- the server rows, THE ROW LAW. -->
      <ul class="mcp-rows">
        <li
          v-for="s in visibleRows"
          :key="rowKey(s)"
          class="mcp-row reg-row"
          :class="{ 'mcp-off': !serverEnabled(s) }">
          <div class="mcp-row-main reg-row-head">
            <div class="reg-row-main">
              <span
                class="mcp-name reg-row-name"
                :title="s.name">{{ s.name }}</span>
              <span class="mcp-sub reg-row-sub">
                <span
                  v-if="s.transport"
                  class="mcp-pill"
                  title="transport">{{ s.transport }}</span>
                <span
                  class="mcp-state"
                  :title="statusLabel(s)">
                  <span
                    class="mcp-dot"
                    :class="`mcp-dot-${statusDot(s)}`"></span>
                  <span class="mcp-state-label">{{ statusLabel(s) }}</span>
                </span>
              </span>
            </div>
            <!-- the toggle is the shared TogglePill. -->
            <TogglePill
              class="reg-row-ctrl"
              :state="serverEnabled(s)"
              :disabled="busy !== null"
              :label="`${serverEnabled(s) ? 'disable' : 'enable'} ${s.name}`"
              @click="onToggle(s)" />
          </div>
          <p
            v-if="s.url || s.command"
            class="mcp-target"
            :title="s.url || s.command">
            {{ s.url || s.command }}
          </p>
          <!--
            tool CHIPS: wrapped inline pills - never the shredded zero-width columns.
          -->
          <div
            v-if="s.tools && s.tools.length"
            class="mcp-tools">
            <span
              v-for="t in s.tools"
              :key="t"
              class="mcp-tool"
              :title="t">{{ t }}</span>
          </div>
          <p
            v-else-if="serverEnabled(s)"
            class="mcp-none">
            no tools discovered
          </p>
        </li>
      </ul>
    </template>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  import { reloadMcpServers } from "./reload";
  import { setMcpServerEnabled } from "./toggle";
  import {
    MCP_EMPTY_HINT,
    MCP_EMPTY_TEXT,
    serverEnabled,
    serverRows,
    statusDot,
    statusLabel,
    type McpServerWire,
    type McpStatusWire,
  } from "./serverRows";
  import { filterMcpServers } from "./search";

  // Module-owned UI: the inspector's MCP tab content,
  // sourced from GET /mcp/servers and shipped by the mcp module itself
  // (harness/mcp/ui/) through the module-UI composition - the same
  // shape the tools/skills/cron panels ride. The write paths
  // + ): the band refresh POSTs /mcp/reload; the
  // per-row toggle POSTs /mcp/toggle - each re-fetches the listing
  //   through its helper, and a failure lands on the panel's error line
  //   with the prior rows standing (server truth never fakes a swap).
  // Self-contained wiring (module UI carries its own wiring, the
  // narrow.ts copy law). The wire's `source` field is read nowhere in
  // the panel since (the from-mcp.yaml echo died with
  // its caption) - the wire keeps the field for the helpers' contracts.
  const rows = ref<McpServerWire[]>(serverRows({}));
  const loaded = ref(false);
  const error = ref("");
  const reloading = ref(false);

  // the in-band search - CLIENT-SIDE on the loaded listing (no new backend route). The
  // toggle still writes by exact server name, so a row hidden by the query keeps its
  // write target intact (the write path never routes through the filter).
  const query = ref("");
  const visibleRows = computed(() => filterMcpServers(rows.value, query.value));

  async function load(): Promise<void> {
    const res = await fetch("/mcp/servers");
    if (!res.ok) throw new Error(`GET /mcp/servers: ${res.status}`);
    const rep: McpStatusWire = await res.json();
    rows.value = serverRows(rep);
  }

  // the per-server toggle ---------------------------- ONE click commits. busy holds
  // the committing row's name so a stacked double-click cannot overlap writes.
  const busy = ref<string | null>(null);

  function onToggle(s: McpServerWire) {
    if (busy.value !== null) return;
    apply(s);
  }

  // the commit: POST the toggle chain (toggle.ts), then re-render from the fresh.
  async function apply(s: McpServerWire) {
    const target = !serverEnabled(s);
    busy.value = s.name;
    try {
      const rep = await setMcpServerEnabled(s.name, target);
      rows.value = serverRows(rep);
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      busy.value = null;
    }
  }

  function rowKey(s: McpServerWire): string {
    return s.name;
  }

  // the refresh: POST the reload route, then show the fresh listing. A failure lands on
  // the panel's existing error line; the previous rows stand (server truth never fakes
  // a swap).
  async function reload(): Promise<void> {
    if (reloading.value) return;
    reloading.value = true;
    try {
      const rep = await reloadMcpServers();
      rows.value = serverRows(rep);
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      reloading.value = false;
    }
  }

  // narrow adaptation, the module-UI recipe.
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
  /*
   * the root stops being a flat scroll child - the TabBand stays fixed, the body owns
   * the scroll (structurally, never sticky).
   */
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
  /* the server rows: THE ROW LAW. */
  .mcp-rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }
  .mcp-name {
    min-width: 0;
  }
  .mcp-pill {
    font-family: var(--mono);
    font-size: 9px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    line-height: 1.3;
    padding: 1px 6px;
    border: 1px solid color-mix(in srgb, currentcolor 25%, transparent);
    border-radius: var(--r-chip);
    opacity: 0.75;
    flex-shrink: 0;
  }
  .mcp-state {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    max-width: 100%;
    font-size: 11px;
    opacity: 0.7;
  }
  .mcp-state-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /*
   * the status dot: --ok for a live connection, --err for a recorded failure, dim for
   * disabled/not connected - never color-only (the label rides beside it, the status
   * glyph law).
   */
  .mcp-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .mcp-dot-ok {
    background: var(--ok);
  }
  .mcp-dot-err {
    background: var(--err);
  }
  .mcp-dot-off {
    background: var(--text-dim);
  }
  /* the toggle has no local CSS. */
  .mcp-target {
    margin: 0;
    font-size: 10.5px;
    opacity: 0.5;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mcp-tools {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    min-width: 0;
    margin-top: 3px;
  }
  .mcp-tool {
    font-family: var(--mono);
    font-size: 10.5px;
    line-height: 1.4;
    color: var(--text-dim);
    padding: 2px 7px;
    border: 1px solid color-mix(in srgb, currentcolor 15%, transparent);
    border-radius: var(--r-chip);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mcp-none {
    margin: 0;
    font-size: 10.5px;
    opacity: 0.5;
  }
  .mcp-off .mcp-name,
  .mcp-off .mcp-state {
    opacity: 0.55;
  }
  .mcp-off .mcp-tools {
    opacity: 0.45;
  }
  /*
   * narrow: the module-UI recipe - condensed chrome; the subtitle line truncates as one
   * (the state never wraps off the row); the chips keep their wrap+ellipsis law at any
   * width.
   */
  .mcp-list.narrow {
    padding: 0;
  }
  .mcp-list.narrow .mcp-body {
    padding: 4px 8px 10px;
  }
</style>