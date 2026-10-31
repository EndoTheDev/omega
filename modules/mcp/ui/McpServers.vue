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
      <!-- card t_83f37525: the source echo is a proper muted caption -
           its own line, its own margin, never overlapped by the rows -->
      <p class="mcp-source">from {{ sourceLabel(source) }}</p>
      <!-- the server rows, the RegistryList rhythm (border-separated,
           breathing padding - not boxed debug cards in dead space).
           The write path is the per-row toggle (card t_83f37525): the
           arm/confirm two-step, POST /mcp/toggle riding toggle.ts. -->
      <ul class="mcp-rows">
        <li
          v-for="s in rows"
          :key="rowKey(s)"
          class="mcp-row"
          :class="{ 'mcp-off': !serverEnabled(s) }">
          <div class="mcp-row-main">
            <!-- the toggle is a BUTTON, not a checkbox: the RegistryList
                 arm-branch lesson (t_87303e8f) is the :checked prop trap -
                 a button whose armed state comes from the reactive model
                 carries no DOM prop Vue could skip, so the two-step is
                 honest without the per-row revision remount. -->
            <button
              type="button"
              class="mcp-toggle"
              :class="{ on: serverEnabled(s) }"
              :disabled="busy !== null"
              :aria-label="toggleAria(s)"
              :title="toggleAria(s)"
              @click="onToggle(s)">
              <span class="mcp-toggle-thumb"></span>
            </button>
            <span
              class="mcp-name"
              :title="s.name">{{ s.name }}</span>
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
          </div>
          <p
            v-if="s.url || s.command"
            class="mcp-target"
            :title="s.url || s.command">
            {{ s.url || s.command }}
          </p>
          <!-- tool CHIPS: wrapped inline pills - never the shredded
               zero-width columns the old fixed-width list rendered.
               Each chip ellipses its own text (long names read whole
               on hover); the container wraps to new lines, so at any
               pane width nothing clips vertically. -->
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
          <p
            v-if="armed === s.name"
            class="mcp-confirm">
            {{ noteFor(s) }}
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
  import { setMcpServerEnabled } from "./toggle";
  import {
    MCP_EMPTY_HINT,
    MCP_EMPTY_TEXT,
    serverEnabled,
    serverRows,
    sourceLabel,
    statusDot,
    statusLabel,
    type McpServerWire,
    type McpStatusWire,
  } from "./serverRows";

  // Module-owned UI (card t_c7136c50): the inspector's MCP tab content,
  // sourced from GET /mcp/servers and shipped by the mcp module itself
  // (modules/mcp/ui/) through the module-UI composition - the same
  // shape the tools/skills/cron panels ride. The write paths (card
  // t_d6e94e79 + t_83f37525): the band refresh POSTs /mcp/reload; the
  // per-row toggle POSTs /mcp/toggle - each re-fetches the listing
  // through its helper, and a failure lands on the panel's error line
  // with the prior rows standing (server truth never fakes a swap).
  // Self-contained wiring (module UI carries its own wiring, the
  // narrow.ts copy law).
  const source = ref<string | undefined>(undefined);
  const rows = ref<McpServerWire[]>(serverRows({}));
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

  // --- the per-server toggle (card t_83f37525) ----------------------------
  // The two-step RegistryList mechanic, armed locally: click 1 arms the
  // row (the confirm note states the consequence), click 2 commits -
  // no accidental first-click flips. busy holds the committing row's
  // name so a stacked double-click cannot overlap writes.
  const armed = ref<string | null>(null);
  const busy = ref<string | null>(null);

  function onToggle(s: McpServerWire) {
    if (busy.value !== null) return;
    if (armed.value !== s.name) {
      armed.value = s.name;
      return;
    }
    apply(s);
  }

  // the commit: POST the toggle chain (toggle.ts), then re-render from
  // the fresh listing. Armed clears whether the write lands or throws.
  async function apply(s: McpServerWire) {
    const target = !serverEnabled(s);
    armed.value = null;
    busy.value = s.name;
    try {
      const rep = await setMcpServerEnabled(s.name, target);
      source.value = rep.source;
      rows.value = serverRows(rep);
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      busy.value = null;
    }
  }

  function noteFor(s: McpServerWire): string {
    return serverEnabled(s)
      ? `disable ${s.name}? click again to confirm`
      : `enable ${s.name}? click again to confirm`;
  }

  function toggleAria(s: McpServerWire): string {
    return armed.value === s.name
      ? `confirm apply to ${s.name}?`
      : (serverEnabled(s) ? "disable " : "enable ") + s.name;
  }

  // one key per row identity (the config file owns the names)
  function rowKey(s: McpServerWire): string {
    return s.name;
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
     the sibling panels (ToolsList, SkillsList) already speak. The row
     vocabulary is the RegistryList's: border-separated rows, a semibold
     name over muted metas, chips for the enumerable bits. */
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
  /* the source echo (card t_83f37525): a muted caption with its own
     rhythm - never the half-covered line the row cards used to ride. */
  .mcp-source {
    margin: 2px 0 4px;
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
  /* the server rows: the RegistryList rhythm - a hairline between rows,
     7px of breath, no boxed card floating in dead space. */
  .mcp-rows {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }
  .mcp-row {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    padding: 7px 0;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
  }
  .mcp-row:last-child {
    border-bottom: none;
  }
  .mcp-row-main {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    flex-wrap: wrap;
  }
  .mcp-name {
    font-size: 12px;
    font-weight: 600;
    opacity: 0.9;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* the transport badge: a small mono pill - the chip voice, not a
     bare uppercase string floating in the row. */
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
  /* the status dot: --ok for a live connection, --err for a recorded
     failure, dim for disabled/not connected - never color-only (the
     label rides beside it, the status glyph law). */
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
  /* the toggle: a track/thumb switch on a BUTTON element so the arm/
     confirm two-step carries no :checked prop to miss (t_87303e8f). */
  .mcp-toggle {
    width: 26px;
    height: 15px;
    padding: 0;
    border: 1px solid color-mix(in srgb, currentcolor 30%, transparent);
    border-radius: 8px;
    background: transparent;
    position: relative;
    cursor: pointer;
    flex-shrink: 0;
  }
  .mcp-toggle:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .mcp-toggle-thumb {
    position: absolute;
    top: 1px;
    left: 1px;
    width: 11px;
    height: 11px;
    border-radius: 50%;
    background: var(--text-dim);
  }
  .mcp-toggle.on {
    border-color: var(--ok);
  }
  .mcp-toggle.on .mcp-toggle-thumb {
    left: 12px;
    background: var(--ok);
  }
  /* the meta lines keep the toggle's indent so the row reads as one
     column under the name */
  .mcp-target {
    margin: 0;
    font-size: 10.5px;
    opacity: 0.5;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding-left: 34px;
  }
  /* the tool chips: wrapped inline pills - every chip ellipses its own
     text at the pane's edge and the container wraps to the next line,
     so NOTHING clips vertically at any width (the old fixed-width
     list shredded names into four-char columns; that shape is dead). */
  .mcp-tools {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    min-width: 0;
    margin-top: 3px;
    padding-left: 34px;
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
    padding-left: 34px;
  }
  .mcp-confirm {
    margin: 0;
    font-size: 11px;
    opacity: 0.75;
    padding-left: 34px;
  }
  /* the disabled row stays visible, muted (the skills list law) */
  .mcp-off .mcp-name,
  .mcp-off .mcp-state {
    opacity: 0.55;
  }
  .mcp-off .mcp-tools {
    opacity: 0.45;
  }
  /* narrow: the module-UI recipe (card t_a2fdc630) - condensed chrome:
     the state drops to its own line instead of floating right; the
     chips keep their wrap+ellipsis law at any width. */
  .mcp-list.narrow {
    padding: 0;
  }
  .mcp-list.narrow .mcp-body {
    padding: 4px 8px 10px;
  }
  .mcp-list.narrow .mcp-state {
    margin-left: 0;
    width: 100%;
  }
</style>