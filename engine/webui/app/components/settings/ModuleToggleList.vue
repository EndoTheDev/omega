<template>
  <!-- v0.12 E2: the live module toggle list. Data is the registry's
       introspection (core provides it, this panel renders it - the
       settings-toggle small law). Engine ring modules never appear:
       the server filters them before the wire (tier law). A host
       without the toggle seam (501) renders nothing - same fallback
       the settings pane uses for missing knobs.
       Card t_8ac225f1: modules is a FIRST-CLASS settings group now -
       the same group-head treatment as the ten knob groups (chevron
       button, aria-expanded, click-to-toggle, persisted collapse via
       the shared omega.settings-collapsed storage), and the search
       box narrows its rows like every other group's knobs. -->
  <section
    v-if="loaded && (!searching || visibleRows.length > 0)"
    ref="rootEl"
    class="settings-group modules-group">
    <!-- full-row clickable header, byte-for-byte the same interaction
         as SettingsTab's group loop: chevron prefix, whole header
         toggles, aria-expanded. A group is hidden when collapsed,
         EXCEPT while a search narrows the list - matching results
         must never hide the sections that found them. -->
    <button
      class="settings-group-head"
      type="button"
      :aria-expanded="!isCollapsed"
      @click="toggleCollapsed">
      <svg
        class="bucket-chevron"
        :class="{ open: !isCollapsed }"
        width="8"
        height="8"
        viewBox="0 0 8 8"
        aria-hidden="true">
        <path
          d="M2.5 1L6 4L2.5 7"
          fill="none"
          stroke="currentColor"
          stroke-width="1.2" />
      </svg>
      <span class="settings-group-title">modules</span>
    </button>
    <template v-if="!isCollapsed">
    <label
      v-for="m in visibleRows"
      :key="rowKey(m)"
      class="setting-row module-row"
      :class="{ 'module-off': m.state === 'disabled', narrow: narrow }">
      <input
        type="checkbox"
        :disabled="busy !== null || !m.toggleable"
        :checked="m.state === 'mounted'"
        @change="onToggle(m, ($event.target as HTMLInputElement).checked)" />
      <span class="module-label">{{ m.name }}</span>
      <span
        v-if="!m.toggleable"
        class="module-state module-locked"
        :title="m.description">always on</span>
      <span v-if="rowNote(m)" class="setting-help module-note">{{ rowNote(m) }}</span>
    </label>
    <p v-if="modules.length === 0" class="settings-empty">no modules reported</p>
    <p v-if="toggleError" class="settings-error">{{ toggleError }}</p>
    <!-- Q12: rejections visible as events - the refusal tail rides
         every response, so a rejected toggle shows its ledger entry
         right under the list it was refused from. -->
    <p v-for="r in refusals" :key="r.at + r.module" class="module-refusal">
      {{ r.module }}: {{ r.err }}
    </p>
    </template>
  </section>
</template>

<script setup lang="ts">
  import { computed, onMounted, ref, watch } from "vue";
  import {
    fetchModules,
    filterModuleRows,
    toggleModule,
    parseCollapsedKeys,
    serializeCollapsedKeys,
    type ModuleRow,
    type ModuleRefusal,
  } from "~/composables/omega";
  import { useNarrow } from "~/composables/narrow";

  // the settings search narrows this group's rows too: the parent
  // hands its live query down (empty = no narrowing).
  const props = defineProps<{ query?: string }>();

  // the parent tracks whether this section currently renders at all
  // (the seam may be absent, or a search matched no rows) so the
  // pane's "no settings match" empty state stays truthful.
  const emit = defineEmits<{
    (e: "visible", value: boolean): void;
  }>();

  // width-driven adaptation: the inspector column measures ~240-320px,
  // the rows stack below the shared threshold (same mechanic as
  // SettingsTab's own narrow).
  const rootEl = ref<HTMLElement | null>(null);
  const narrow = useNarrow(rootEl);

  const loaded = ref(false);
  const modules = ref<ModuleRow[]>([]);
  const refusals = ref<ModuleRefusal[]>([]);
  const toggleError = ref<string | null>(null);
  const busy = ref<string | null>(null);

  // --- collapse + search: the same mechanic as the knob groups ---
  // collapsed follows the SHARED settings persistence (the same
  // localStorage key and parse helpers SettingsTab uses; the group
  // key is "modules"). Search wins over collapse: while a query
  // narrows the list, the group shows its matches.
  const COLLAPSED_KEY = "omega.settings-collapsed";
  const collapsed = ref(false);

  function restoreCollapsed() {
    if (typeof window === "undefined") return;
    // a missing persisted set (or a stale payload) restores expanded -
    // parseCollapsedKeys' tolerance covers unknown keys and junk.
    collapsed.value = parseCollapsedKeys(
      localStorage.getItem(COLLAPSED_KEY),
    ).has("modules");
  }

  function toggleCollapsed() {
    collapsed.value = !collapsed.value;
    if (typeof window !== "undefined") {
      // read-modify-write the shared set: the other groups' keys must
      // survive this group's toggle (and vice versa, via SettingsTab).
      const next = parseCollapsedKeys(localStorage.getItem(COLLAPSED_KEY));
      if (collapsed.value) next.add("modules");
      else next.delete("modules");
      localStorage.setItem(COLLAPSED_KEY, serializeCollapsedKeys(next));
    }
  }

  const searching = computed(() => (props.query ?? "").trim().length > 0);
  const visibleRows = computed<ModuleRow[]>(() =>
    filterModuleRows(modules.value, props.query ?? ""),
  );
  // a search that narrows the groups must not also swallow their rows
  const isCollapsed = computed(
    () => collapsed.value && !searching.value,
  );

  restoreCollapsed();

  // the parent's empty-state needs the live visibility, not a stale one
  const sectionVisible = computed(
    () => loaded.value && (!searching.value || visibleRows.value.length > 0),
  );
  watch(
    sectionVisible,
    (v) => emit("visible", v),
    { immediate: true },
  );

  // The two-step confirm (no modal in the inspector column): the first
  // click arms the row - the note names what goes down with it (the
  // Q3 dependents) or asks plain; the second click applies. A cancel
  // bumps the row key so the checkbox re-renders from server state.
  const armed = ref<string | null>(null);
  const revisions = ref<Record<string, number>>({});

  const rowKey = (m: ModuleRow) => `${m.name}-${revisions.value[m.name] ?? 0}`;
  const armedId = (m: ModuleRow, enabled: boolean) => `${m.name}:${String(enabled)}`;

  const rowNote = (m: ModuleRow): string => {
    if (armed.value === armedId(m, false)) return "confirm: apply disable?";
    if (armed.value === armedId(m, true)) return "confirm: apply enable?";
    if (!m.toggleable) return "always on";
    if (m.dependents && m.dependents.length > 0) return `dependents: ${m.dependents.join(", ")}`;
    return "";
  };

  onMounted(() => {
    void load();
  });

  async function load() {
    toggleError.value = null;
    try {
      apply(await fetchModules());
      loaded.value = true;
    } catch (err) {
      // 501 (no toggle seam) or an unreachable server: the section
      // stays hidden, the settings pane keeps working without it.
      loaded.value = false;
      if (!(err instanceof Error && err.message.includes("501"))) {
        console.error("modules load failed:", err);
      }
    }
  }

  function apply(payload: { modules: ModuleRow[]; refusals: ModuleRefusal[] }) {
    modules.value = payload.modules;
    refusals.value = payload.refusals;
  }

  async function onToggle(m: ModuleRow, checked: boolean) {
    if (busy.value !== null) return;
    const id = armedId(m, checked);
    if (armed.value !== id) {
      // arm: the first click never applies - the confirm note shows in
      // the row; the checkbox re-renders back to server state.
      armed.value = id;
      bump(m);
      return;
    }
    armed.value = null;
    busy.value = m.name;
    toggleError.value = null;
    try {
      apply(await toggleModule(m.name, checked));
      bump(m);
    } catch (err) {
      toggleError.value = err instanceof Error ? err.message : String(err);
      // Refresh anyway: the refusal ledger is the visible event (Q12)
      // even when the response body carried no fresh list.
      try {
        apply(await fetchModules());
      } catch {
        // the list stays; the error line stands
      }
      bump(m);
    } finally {
      busy.value = null;
    }
  }

  function bump(m: ModuleRow) {
    revisions.value = { ...revisions.value, [m.name]: (revisions.value[m.name] ?? 0) + 1 };
  }
</script>