<template>
  <section
    ref="rootEl"
    class="tools-list"
    :class="{ narrow: narrow }">
    <!-- card t_308e6d9b: the UNIVERSAL band (the shared TabBand) - title
         left, the refresh action right, ONE row. Card t_f1a034dc: Tools
         was the only RegistryList tab whose band stayed bare - Skills got
         its refresh in the t_ec57d5b8 rollout and Tools missed it, so
         the bare band read as unaligned next to every other tab. The
         click re-runs the fetch chain: GET /tools is the live
         inventory, so a tool appearing after mount shows - no restart.
         The band renders BEFORE the scroll body: scrolling the list
         never moves it. -->
    <TabBand title="Tools">
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
    <!-- v0.14.1 registry-browser: the shared RegistryList (one pattern,
         three consumers). The toggle drives PUT /disabled/tools; a
         disabled tool is absent from every tool union - the built-in-
         wins merge law is untouched, the fold filters after it. -->
    <RegistryList
      v-else
      kind="tools"
      :rows="rows"
      :busy="busy"
      :error="toggleError"
      :loaded="loaded"
      :toggleable="toggleable"
      empty-text="no tools registered"
      @toggle="onToggle" />
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";
  import { toggleTargets, toolRows, type ToolWire } from "./toolRows";

  // Module-owned UI canary (t_d984b493), now the inspector's Tools tab
  // content (card t_db430fea): the tool inventory, sourced from
  // GET /tools and shipped by the tools module itself
  // (modules/tools/ui/) through the module-UI composition, plus the
  // per-tool enable/disable toggle (v0.14.1) against the /disabled
  // fold. A domain tool's actions[] become the row's expandable
  // detail: one metadata source feeds this panel, the system prompt,
  // and the row detail. Self-contained fetch (module UI carries its
  // own wiring, desktop / VS Code precedent) - the flag merge and the
  // toggle PUT are small deliberate copies (the narrow.ts copy law);
  // the SHARED thing is the RegistryList component.
  // the wire shape GET /tools serves (types live in ./toolRows)
  interface ToolInfo extends ToolWire {}
  const tools = ref<ToolInfo[]>([]);
  const fold = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const busy = ref<string | null>(null);
  // the refresh's lock (card t_ec57d5b8 pattern, card t_f1a034dc): a
  // second click while a chain is in flight is dropped (the in-flight
  // chain answers)
  const refreshing = ref(false);
  const toggleError = ref<string | null>(null);
  // false when the host serves no toggle seam (501 on /disabled): the
  // list stays, read-only.
  const toggleable = ref(false);

  // the row model (t_e06b38bc Part 1) lives in ./toolRows - the pure
  // grouping the node suite pins: domain tools as parent rows with
  // dotted action children, sibling-verb tools merged under their
  // prefix, lone dotted tools as their own rows. rows are a computed
  // (the AgentsPanel shape): RegistryList binds `rows` straight into
  // the template, where an unwrap gives the array - a plain function
  // would make the `v-if="rows.length"` gate read the ARITY (0), and
  // the list would never render (the t_504a78ca bug: data fetched,
  // tabs empty).
  const rows = computed(() => toolRows(tools.value, fold.value));

  async function load(): Promise<void> {
    const res = await fetch("/tools");
    if (!res.ok) throw new Error(`GET /tools: ${res.status}`);
    tools.value = await res.json();
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
      // /disabled unreachable is not fatal to the LIST; stay read-only
      // rather than guessing the fold state
      toggleable.value = false;
    }
  }

  // the refresh (card t_ec57d5b8 pattern, card t_f1a034dc): re-run the
  // fetch chain without a remount - GET /tools answers the live
  // inventory (the built-in-wins merge is server-side), so a tool
  // appearing after mount shows. A failure lands on the panel's
  // existing error line and the current rows stand.
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

  // the toggle: the fold stores EXACT tool names, so a group parent's
  // toggle fans out to its members (toggleTargets: an exact tool name
  // PUTs itself, a merged prefix PUTs each member). PUT answers the
  // fresh fold - rows rebuild from the reload so the persisted state
  // is what the tab shows. A failed toggle still refreshes: the error
  // line stands over the truth (404 = unknown name, 409 =
  // built-in-wins, both loud).
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
        // the rows stay; the error line stands
      }
      busy.value = null;
    }
  }
</script>

<style scoped>
  /* Module-owned look built on the shell's design tokens - the section
     is self-titled; the inspector mounts it as any other panel. */
  /* card t_308e6d9b: the root stops being a flat scroll child - the
     TabBand stays fixed, the body owns the scroll (structurally, never
     sticky). The old root padding rides the body now. */
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
  /* narrow (the module-UI recipe, card t_a2fdc630): the ROOT stays
     padding-free - the band mounts flush like Cron/Skills (the stray
     narrow root padding sat the band +6/+8px inside, card t_ffab9823);
     the body owns every inset. */
  .tools-list.narrow .tools-body {
    padding: 4px 8px 10px;
  }
  .panel-status {
    margin: 0;
    font-size: 11px;
    opacity: 0.6;
  }
</style>