<template>
  <!-- v0.12 E2: the live module toggle list. Data is the registry's
       introspection (core provides it, this panel renders it - the
       settings-toggle small law). Engine ring modules never appear:
       the server filters them before the wire (tier law). A host
       without the toggle seam (501) renders nothing - same fallback
       the settings pane uses for missing knobs. -->
  <section v-if="loaded" ref="rootEl" class="settings-group modules-group">
    <div class="settings-group-head modules-head">
      <span class="settings-group-title">modules</span>
    </div>
    <label
      v-for="m in modules"
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
  </section>
</template>

<script setup lang="ts">
  import { onMounted, ref } from "vue";
  import {
    fetchModules,
    toggleModule,
    type ModuleRow,
    type ModuleRefusal,
  } from "~/composables/omega";
  import { useNarrow } from "~/composables/narrow";

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