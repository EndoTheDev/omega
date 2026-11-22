<template>
  <section
    ref="rootEl"
    class="mcp-list"
    :class="{ narrow: narrow }">
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
      <ul class="mcp-rows">
        <li
          v-for="s in visibleRows"
          :key="rowKey(s)"
          class="mcp-row reg-row"
          :class="{ 'mcp-off': !serverEnabled(s) }"
          @contextmenu="onRowMenu($event, s)">
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
          <!--
            THE ROW DETAIL (the menu's Open): the recorded failure in full - the state
            label ellipsizes it - and where the entry is authored. The panel writes the
            enabled flag and a delete only; the config's args/env/headers never reach the
            wire, so editing here would drop them.
          -->
          <div
            v-if="openName === s.name"
            class="mcp-detail">
            <p class="mcp-detail-note">{{ MCP_DETAIL_NOTE }}</p>
            <p
              v-if="s.error"
              class="mcp-detail-error">{{ s.error }}</p>
            <div
              v-if="confirming === s.name"
              class="mcp-confirm">
              <p class="mcp-confirm-text">
                delete {{ s.name }}? its mcp.yaml entry goes, and its connections close
              </p>
              <button
                type="button"
                class="mcp-confirm-go"
                :disabled="busy !== null"
                @click="removeServer(s)">delete</button>
              <button
                type="button"
                class="mcp-confirm-cancel"
                @click="confirming = ''">cancel</button>
            </div>
          </div>
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
  import { deleteMcpServer } from "./remove";
  import { setMcpServerEnabled } from "./toggle";
  import { openContextMenu } from "~/composables/contextMenu";
  import { mcpMenuItems } from "./mcpMenu";
  import {
    MCP_EMPTY_HINT,
    MCP_EMPTY_TEXT,
    MCP_DETAIL_NOTE,
    serverEnabled,
    serverRows,
    statusDot,
    statusLabel,
    type McpServerWire,
    type McpStatusWire,
  } from "./serverRows";
  import { filterMcpServers } from "./search";

  // Module-owned UI: the inspector's MCP tab content, sourced from GET /mcp/servers and
  // shipped by the mcp module itself (harness/mcp/ui/) through the module-UI composition.
  // Self-contained wiring (module UI carries its own wiring, the narrow.ts copy law). The
  // wire's `source` field is unread here; it stays for the helpers' contracts.
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

  // the per-server toggle POSTs /mcp/toggle: ONE click commits. busy holds the
  // committing row's name so a stacked double-click cannot overlap writes.
  const busy = ref<string | null>(null);

  // the row detail (the menu's Open) is one at a time; `confirming` is the delete arm
  // that detail carries, so a delete is never one click from a menu row.
  const openName = ref("");
  const confirming = ref("");

  function onToggle(s: McpServerWire) {
    if (busy.value !== null) return;
    apply(s);
  }

  /** THE ROW MENU: Open unfolds the detail, Enable/Disable IS the pill, Delete arms the
      detail's one confirm. No Edit row - see mcpMenu's header for why the panel cannot
      offer a lossless save. */
  function onRowMenu(e: MouseEvent, s: McpServerWire) {
    const items = mcpMenuItems(
      { enabled: serverEnabled(s) },
      {
        open: () => {
          openName.value = openName.value === s.name ? "" : s.name;
          confirming.value = "";
        },
        toggle: () => onToggle(s),
        remove: () => {
          openName.value = s.name;
          confirming.value = s.name;
        },
      },
    );
    if (items.length === 0) return;
    e.preventDefault();
    void openContextMenu(e.clientX, e.clientY, items);
  }

  // removeServer is the detail's confirm: POST /mcp/delete answers the fresh listing,
  // so the panel re-renders from server truth and never from its own guess.
  async function removeServer(s: McpServerWire): Promise<void> {
    if (busy.value !== null) return;
    busy.value = s.name;
    try {
      rows.value = serverRows(await deleteMcpServer(s.name));
      openName.value = "";
      confirming.value = "";
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      busy.value = null;
    }
  }

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
  /* the row detail: the same left gutter as the target/chips, one hairline above */
  .mcp-detail {
    margin-top: 4px;
    padding-top: 4px;
    border-top: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .mcp-detail-note {
    margin: 0;
    font-size: 10.5px;
    opacity: 0.55;
  }
  .mcp-detail-error {
    margin: 0;
    font-size: 10.5px;
    color: var(--err);
    overflow-wrap: anywhere;
  }
  .mcp-confirm {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .mcp-confirm-text {
    margin: 0;
    font-size: 10.5px;
    opacity: 0.75;
  }
  .mcp-confirm-go,
  .mcp-confirm-cancel {
    font: inherit;
    font-size: 10.5px;
    color: inherit;
    background: none;
    border: 1px solid color-mix(in srgb, currentcolor 25%, transparent);
    border-radius: var(--r-chip);
    padding: 1px 8px;
    cursor: pointer;
  }
  .mcp-confirm-go:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .mcp-confirm-go {
    color: var(--err);
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