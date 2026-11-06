<template>
  <!-- custom dropdown: a native <select> popup is OS chrome that can't be
       themed - this renders the listbox ourselves on the design tokens.
       Card t_1914a8c9: the flat list became the Hermes-desktop picker -
       one COLLAPSIBLE section per provider (the grouped /models payload,
       "default" first, named instances sorted after), searchable across
       sections, a listing failure shown inline in its section (never a
       silent drop of the provider), and footer actions: refresh models
       (re-fetch every provider's catalog), add custom model..., edit
       models... (custom models ride localStorage - picker-level extras
       merged into their section, no config machinery). A pick emits the
       slot grammar: "instance:model" for a named section (the resolved
       pin sticks server-side), "default:model" for the base provider
       (the explicit left-the-instance pick cancels the sticky pin). -->
  <div
    class="model-select"
    ref="root">
    <button
      class="model-chip"
      type="button"
      title="Model"
      :aria-expanded="open"
      @click="open ? close() : openNow()"
      @keydown.down.prevent="openNow()">
      <!-- ellipsis container: at narrow widths the chip gives up width
           BEFORE the toolbar wraps (t_357cbe9c squeeze, 320px floor) -->
      <span class="chip-name">{{ model }}</span>
      <span
        class="chev"
        aria-hidden="true"
        >▾</span
      >
    </button>
    <div
      v-if="open"
      class="model-pop">
      <input
        ref="searchEl"
        v-model="query"
        class="model-search"
        type="text"
        placeholder="filter models"
        spellcheck="false"
        @keydown.escape.stop="close" />
      <div class="model-body">
        <div
          v-for="g in visibleGroups"
          :key="g.name"
          class="model-section">
          <button
            type="button"
            class="model-sec-head"
            :aria-expanded="isExpanded(g)"
            :aria-label="`toggle provider section ${g.name}`"
            @click="toggle(g.name)">
            <span
              class="sec-chev"
              aria-hidden="true">{{ isExpanded(g) ? "▾" : "▸" }}</span>
            <span class="sec-name">{{ g.name }}</span>
            <span class="sec-count">{{
              g.error ? "!" : g.models.length
            }}</span>
          </button>
          <div v-if="isExpanded(g)">
            <p
              v-if="g.error"
              class="model-sec-err">
              {{ g.error }} - use refresh models to retry
            </p>
            <ul class="model-list">
              <li
                v-for="m in g.models"
                :key="`${g.name}:${m}`"
                role="option"
                :aria-selected="isCurrent(g, m)"
                :class="{ current: isCurrent(g, m) }"
                @click="pick(g, m)">
                {{ m }}
              </li>
              <li
                v-if="!g.error && g.models.length === 0"
                class="model-none">
                no models
              </li>
            </ul>
          </div>
        </div>
        <p
          v-if="visibleGroups.length === 0"
          class="model-none">
          no matches
        </p>
      </div>
      <div class="model-foot">
        <button
          type="button"
          class="model-foot-btn"
          title="re-fetch every provider's model catalog"
          @click="$emit('refresh-models')">
          refresh models
        </button>
        <button
          type="button"
          class="model-foot-btn"
          :class="{ on: adding }"
          :aria-expanded="adding"
          @click="togglePanel('add')">
          add custom model...
        </button>
        <button
          type="button"
          class="model-foot-btn"
          :class="{ on: editing }"
          :aria-expanded="editing"
          @click="togglePanel('edit')">
          edit models...
        </button>
      </div>
      <div
        v-if="adding"
        class="model-addform">
        <select
          v-model="addProvider"
          class="model-addprov"
          aria-label="provider for the new custom model">
          <option
            v-for="g in mergedGroups"
            :key="g.name"
            :value="g.name">
            {{ g.name }}
          </option>
        </select>
        <input
          v-model="addName"
          class="model-addname"
          type="text"
          placeholder="model name"
          autocomplete="off"
          spellcheck="false"
          @keydown.enter.prevent="addCustom"
          @keydown.escape.stop="close" />
        <button
          type="button"
          class="model-foot-btn"
          :disabled="!addName.trim()"
          @click="addCustom">
          add
        </button>
      </div>
      <div
        v-if="editing"
        class="model-editlist">
        <p
          v-if="customEntries.length === 0"
          class="model-none">
          no custom models
        </p>
        <div
          v-for="e in customEntries"
          :key="`${e.section}:${e.name}`"
          class="model-editrow">
          <span class="model-editname">{{ e.section }}:{{ e.name }}</span>
          <button
            type="button"
            class="model-editdel"
            :aria-label="`remove custom model ${e.section}:${e.name}`"
            @click="removeCustom(e.section, e.name)">
            ×
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
  import type { ModelProviderSection } from "../composables/omega";

  const props = defineProps<{
    model: string;
    /** the flat floor (the settings pickers' fallback list) - the chat
         picker renders it as the default section when no grouped
         catalog arrived yet */
    models: string[];
    /** the grouped catalog (card t_1914a8c9): one section per provider;
         empty falls back to the flat models prop */
    groups?: ModelProviderSection[];
  }>();

  const emit = defineEmits<{
    (e: "update:model", value: string): void;
    (e: "refresh-models"): void;
  }>();

  const open = ref(false);
  const query = ref("");
  const root = ref<HTMLElement | null>(null);
  const searchEl = ref<HTMLInputElement | null>(null);

  // ---- the custom models ("add custom model..." store) - localStorage:
  // picker-level extras merged into their section, no config machinery.
  const CUSTOMS_KEY = "omega.model-picker.customs";
  const customs = ref<Record<string, string[]>>(loadCustoms());

  function loadCustoms(): Record<string, string[]> {
    try {
      const raw = localStorage.getItem(CUSTOMS_KEY);
      if (!raw) return {};
      const parsed = JSON.parse(raw) as unknown;
      if (!parsed || typeof parsed !== "object") return {};
      const out: Record<string, string[]> = {};
      for (const [section, names] of Object.entries(
        parsed as Record<string, unknown>,
      )) {
        if (section === "" || !Array.isArray(names)) continue;
        const clean = names.filter(
          (n): n is string => typeof n === "string" && n.trim() !== "",
        );
        if (clean.length) out[section] = [...new Set(clean)];
      }
      return out;
    } catch {
      return {};
    }
  }

  function saveCustoms() {
    try {
      localStorage.setItem(CUSTOMS_KEY, JSON.stringify(customs.value));
    } catch {
      // a full quota or a closed storage is no reason to break the picker
    }
  }

  const mergedGroups = computed<ModelProviderSection[]>(() => {
    const base: ModelProviderSection[] =
      props.groups && props.groups.length
        ? props.groups
        : [{ name: "default", models: props.models }];
    return base.map((g) => ({
      ...g,
      models: [
        ...new Set([...(g.models ?? []), ...(customs.value[g.name] ?? [])]),
      ],
    }));
  });

  // collapsed provider sections (in-memory: the pop reopens expanded -
  // the Hermes picker behaves the same); searching forces every section
  // open so the query reads across providers.
  const collapsed = ref(new Set<string>());

  const visibleGroups = computed(() => {
    const q = query.value.trim().toLowerCase();
    return mergedGroups.value
      .map((g) => {
        if (!q) return g;
        if (g.error) return { ...g, models: [] };
        return {
          ...g,
          models: g.models.filter((m) => m.toLowerCase().includes(q)),
        };
      })
      .filter((g) =>
        q
          ? g.models.length > 0
          : g.error !== undefined ||
            g.models.length > 0 ||
            !props.groups?.length,
      );
  });

  function isExpanded(g: ModelProviderSection): boolean {
    return query.value.trim() !== "" || !collapsed.value.has(g.name);
  }

  function toggle(name: string) {
    const next = new Set(collapsed.value);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    collapsed.value = next;
  }

  // ---- the pick: the slot grammar (card t_1914a8c9). A named section
  // addresses its instance ("backup:llama3" - the server resolves the
  // instance and sticks the pin); the default section names the base
  // provider ("default:llama3" - the explicit left-the-instance pick
  // cancels a sticky pin). The flat floor also rides "default:" so a
  // pick from the default section never re-routes a pinned session.
  function pick(g: ModelProviderSection, m: string) {
    emit(
      "update:model",
      g.name === "default" ? `default:${m}` : `${g.name}:${m}`,
    );
    close();
  }

  // current-row highlight: the chip may carry the slot value OR the
  // legacy bare name (pre-reshape sessions) - both count for default.
  function isCurrent(g: ModelProviderSection, m: string): boolean {
    if (g.name === "default" && props.model === m) return true;
    return props.model === `${g.name}:${m}`;
  }

  const adding = ref(false);
  const editing = ref(false);
  const addProvider = ref("default");
  const addName = ref("");

  function togglePanel(which: "add" | "edit") {
    if (which === "add") {
      adding.value = !adding.value;
      editing.value = false;
      if (adding.value) {
        addName.value = "";
        addProvider.value = mergedGroups.value.some((g) => g.name === "default")
          ? "default"
          : (mergedGroups.value[0]?.name ?? "default");
      }
    } else {
      editing.value = !editing.value;
      if (editing.value) adding.value = false;
    }
  }

  function addCustom() {
    const name = addName.value.trim();
    if (!name) return;
    const section = addProvider.value;
    const next = { ...customs.value };
    next[section] = [...new Set([...(next[section] ?? []), name])];
    customs.value = next;
    saveCustoms();
    addName.value = "";
  }

  const customEntries = computed(() =>
    Object.entries(customs.value).flatMap(([section, names]) =>
      names.map((name) => ({ section, name })),
    ),
  );

  function removeCustom(section: string, name: string) {
    const next = { ...customs.value };
    const names = (next[section] ?? []).filter((n) => n !== name);
    if (names.length) next[section] = names;
    else delete next[section];
    customs.value = next;
    saveCustoms();
  }

  function openNow() {
    open.value = true;
    adding.value = false;
    editing.value = false;
    nextTick(() => searchEl.value?.focus());
  }

  function close() {
    open.value = false;
    query.value = "";
  }

  // click-outside closes the list - the native select gave us this for free,
  // so the custom one must match its behavior
  function onDocPointer(e: PointerEvent) {
    if (open.value && root.value && !root.value.contains(e.target as Node))
      close();
  }

  onMounted(() => document.addEventListener("pointerdown", onDocPointer));
  onBeforeUnmount(() =>
    document.removeEventListener("pointerdown", onDocPointer),
  );
</script>
