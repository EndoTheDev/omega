<template>
  <section
    ref="rootEl"
    class="tools-list"
    :class="{ narrow: narrow }">
    <!--
      the UNIVERSAL band (the shared TabBand).
      The band renders BEFORE the scroll body: scrolling the list never moves it.: the
      compact in-band search (TabBand's shared.band-search seat) - the query filters
      the LOADED registry by name or description, client-side on the grouped rows
      (hide-rows-only), still inside the tab's native only slice.
    -->
    <TabBand title="Tools">
      <div class="band-search">
        <SearchInput
          v-model="query"
          placeholder="Search tools" />
      </div>
      <button
        class="band-tool"
        title="refresh"
        aria-label="refresh tools"
        :disabled="refreshing"
        @click="refresh">
        <Icon
          name="refresh"
          :size="13" />
      </button>
    </TabBand>
    <div class="tools-body">
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
    <!--
      v0.14.1 registry-browser: the shared RegistryList (one pattern, three consumers).
    -->
    <RegistryList
      v-else
      kind="tools"
      :rows="visibleRows"
      :busy="busy"
      :error="toggleError"
      :loaded="loaded"
      :toggleable="toggleable"
      :row-menu="rowMenu"
      :empty-text="query.trim() ? 'no matching tools' : 'no tools registered'"
      @toggle="onToggle" />
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  import { nativeTools, toggleTargets, toolRows, type ToolWire } from "./toolRows";
  // the band search's filter (name / description on the GROUPED rows), pure.
  import { filterToolRows } from "./search";
  import type { ContextMenuEntry } from "~/composables/contextMenu";
  import { toolMenuItems } from "./toolMenu";

  // Module-owned UI canary, now the inspector's Tools tab content.
  interface ToolInfo extends ToolWire {}
  const tools = ref<ToolInfo[]>([]);
  const fold = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const busy = ref<string | null>(null);
  // the refresh's lock: a second click while a chain is in flight is dropped.
  const refreshing = ref(false);
  const toggleError = ref<string | null>(null);
  const toggleable = ref(false);

  // the row model lives in./toolRows - the pure grouping the node suite pins. rows are
  // a computed (the AgentsPanel shape): RegistryList binds `rows` straight into the
  // template, where an unwrap gives the array - a plain function would make the
  // `v-if="rows.length"` gate read the ARITY (0), and the list would never render.
  const rows = computed(() => toolRows(tools.value, fold.value));

  // the in-band search - CLIENT-SIDE on the grouped rows (no new backend route), name.
  const query = ref("");
  const visibleRows = computed(() => filterToolRows(rows.value, query.value));

  // THE ROW MENU: the tab's only verb is the pill's write, so an untoggleable registry
  // registers nothing and the browser's own menu stays. The row face is local: module UI
  // imports no shell client.
  function rowMenu(row: { name: string; disabled: boolean; noToggle?: boolean }): ContextMenuEntry[] {
    return toolMenuItems(
      {
        disabled: !!row.disabled,
        toggleable: toggleable.value && !row.noToggle,
      },
      // the target direction IS the row's disabled flag (RegistryList's own emit)
      { toggle: () => void onToggle(row.name, !!row.disabled) },
    );
  }

  async function load(): Promise<void> {
    // the fold-aware management read. The bare GET /tools stays the executable registry
    // the chat loop consumes; this tab is the management surface and must show what is
    // off, or a disable becomes a one-way door ("i disabled web, and now its so gone").
    const res = await fetch("/tools?disabled=1");
    if (!res.ok) throw new Error(`GET /tools: ${res.status}`);
    // MCP-origin tools never reach this tab.
    tools.value = nativeTools(await res.json());
    try {
      const dRes = await fetch("/disabled");
      if (dRes.status === 501) {
        toggleable.value = false; // no seam: read-only
      } else if (dRes.ok) {
        const foldBody = (await dRes.json())?.tools;
        fold.value = Array.isArray(foldBody)
          ? foldBody.filter((n): n is string => typeof n === "string")
          : [];
        toggleable.value = true;
      }
    } catch {
      // /disabled unreachable is not fatal to the LIST.
      toggleable.value = false;
    }
  }

  async function refresh(): Promise<void> {
    if (refreshing.value) return;
    refreshing.value = true;
    toggleError.value = null;
    try {
      await load();
      error.value = "";
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      refreshing.value = false;
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

  // the toggle: the fold stores EXACT tool names, so a group parent's toggle fans out.
  async function onToggle(name: string, enabled: boolean): Promise<void> {
    if (busy.value !== null) return;
    busy.value = name;
    toggleError.value = null;
    try {
      for (const target of toggleTargets(name, tools.value)) {
        const res = await fetch("/disabled/tools", {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name: target, enabled }),
        });
        if (!res.ok) throw new Error(`${target}: ${await res.text()}`);
      }
    } catch (e) {
      toggleError.value = e instanceof Error ? e.message : String(e);
    } finally {
      try {
        await load();
      } catch {
      }
      busy.value = null;
    }
  }
</script>

<style scoped>
  /*
   * the root stops being a flat scroll child - the TabBand stays fixed, the body owns
   * the scroll (structurally, never sticky).
   */
  .tools-list {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .tools-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 4px 12px 14px;
  }
  /* narrow: the ROOT stays padding-free. */
  .tools-list.narrow .tools-body {
    padding: 4px 8px 10px;
  }
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
</style>
