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
  </section>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import { isNarrowWidth } from "./narrow";

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
  const fold = ref<string[]>([]);
  const loaded = ref(false);
  const error = ref("");
  const busy = ref<string | null>(null);
  const toggleError = ref<string | null>(null);
  // false when the host serves no toggle seam (501 on /disabled): the
  // list stays, read-only.
  const toggleable = ref(false);

  // the row model: the domain context stays the description, the
  // actions collapse into dotted detail lines (the row expands, the
  // same mechanism every registry-browser row rides).
  function rows() {
    const set = new Set(fold.value);
    return tools.value.map((t) => ({
      name: t.name,
      description: t.description,
      detail: (t.actions ?? []).map(
        (a) => `${t.name}.${a.name} - ${a.description}`,
      ),
      disabled: set.has(t.name) || false,
    }));
  }

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

  // the toggle: PUT answers the fresh fold - rows rebuild from the
  // reload so the persisted state is what the tab shows. A failed
  // toggle still refreshes: the error line stands over the truth
  // (404 = unknown name, 409 = built-in-wins, both loud).
  async function onToggle(name: string, enabled: boolean): Promise<void> {
    if (busy.value !== null) return;
    busy.value = name;
    toggleError.value = null;
    try {
      const res = await fetch("/disabled/tools", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, enabled }),
      });
      if (!res.ok) throw new Error(await res.text());
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
  /* narrow: the module-UI recipe (card t_a2fdc630) - condensed chrome:
     the action detail lines live inside the rows now. */
  .tools-list.narrow {
    padding: 6px 8px 10px;
  }
</style>