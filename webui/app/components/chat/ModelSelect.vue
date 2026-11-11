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
    :class="{ down: flipDown, measured: measured }"
    :style="popRoom"
    ref="root">
    <button
      class="model-chip"
      type="button"
      :title="chipTitle"
      :aria-expanded="open"
      @click="open ? close() : openNow()"
      @keydown.down.prevent="openNow()">
      <!-- ellipsis container: at narrow widths the chip gives up width
           BEFORE the toolbar wraps (t_357cbe9c squeeze, 320px floor) -->
      <span class="chip-name">{{ chipText }}</span>
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
      <!-- the aux INHERIT row (card t_dac84a33, Endo's ruling 1): PINNED
           above the provider sections - never folded into one, because the
           word "default" here means "follow the chat's model", NOT the base
           provider's section. It sits OUTSIDE .model-body so it stays put
           while the sections scroll. Only a settings knob marked
           default_option passes inheritRow; the composer never does. -->
      <button
        v-if="inheritRow"
        type="button"
        class="model-inherit"
        role="option"
        :aria-selected="model === AUX_DEFAULT_VALUE"
        :class="{ current: model === AUX_DEFAULT_VALUE }"
        title="follow the model the current chat runs"
        @click="pickInherit()">
        {{ INHERIT_LABEL }}
      </button>
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
                :aria-disabled="reasonFor(g.name, m) !== null || undefined"
                :title="reasonFor(g.name, m) ?? undefined"
                :class="{
                  current: isCurrent(g, m),
                  off: reasonFor(g.name, m) !== null,
                }"
                @click="pick(g, m)">
                {{ m }}
                <span
                  v-if="reasonFor(g.name, m)"
                  class="row-reason"
                  >{{ reasonFor(g.name, m) }}</span
                >
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
  import type { ModelProviderSection } from "../../composables/omega";
  import { AUX_DEFAULT_VALUE } from "../../composables/modelCatalog";

  const props = defineProps<{
    model: string;
    /** the flat floor (the settings pickers' fallback list) - the chat
         picker renders it as the default section when no grouped
         catalog arrived yet */
    models: string[];
    /** the grouped catalog (card t_1914a8c9): one section per provider;
         empty falls back to the flat models prop */
    groups?: ModelProviderSection[];
    /** the chip's tooltip (card t_dac84a33: a settings row must say WHICH
         knob it sets; the composer leaves it and renders "Model") */
    label?: string;
    /** the aux INHERIT row (card t_dac84a33): pins a TOP row above the
         provider sections that emits the literal "default" - "follow the
         model the current chat runs". Off by default, so the composer
         (which has no default to follow) renders exactly as before. */
    inheritRow?: boolean;
    /** qualify marks a model row unusable and says WHY ("not a vision
         model"): a rejected row renders DISABLED with the reason as its
         title and never emits a pick - it is shown, never hidden, so the
         catalog reads complete. Off by default: every composer row is
         pickable. */
    qualify?: (section: string, model: string) => string | null;
    /** where the pop opens (card t_dac84a33): the composer's chip sits on
         the floor, so "up" is the default and the geometry is unchanged;
         a settings row sits mid-pane and passes "down" */
    drop?: "up" | "down";
  }>();

  const emit = defineEmits<{
    (e: "update:model", value: string): void;
    (e: "refresh-models"): void;
  }>();

  // The inherit row's exact label (Endo's ruling 1, card t_dac84a33). It
  // is NOT a provider section and must never read as one: the same word
  // spells the composer grammar's base-provider prefix ("default:model").
  const INHERIT_LABEL = "default (inherit from the main model)";
  const INHERIT_CHIP = "default (inherit)";

  const chipTitle = computed(() => props.label ?? "Model");
  // the chip face: the stored value, except that an INHERITED knob reads
  // as the word's meaning rather than dropping a bare "default" beside a
  // slot knob's "default:llama3" (two different defaults would look alike)
  const chipText = computed(() =>
    props.inheritRow && props.model === AUX_DEFAULT_VALUE
      ? INHERIT_CHIP
      : props.model,
  );

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
    // a DISABLED-with-reason row (card t_dac84a33) never stores anything:
    // the reason is information, not an override of the slot's rule
    if (reasonFor(g.name, m) !== null) return;
    emit(
      "update:model",
      g.name === "default" ? `default:${m}` : `${g.name}:${m}`,
    );
    close();
  }

  // the inherit row emits THE LITERAL WORD - never a section prefix. It is
  // the aux lane's one magic value ("follow the chat's model"), not the
  // composer grammar's "default:" section address (card t_dac84a33).
  function pickInherit() {
    emit("update:model", AUX_DEFAULT_VALUE);
    close();
  }

  // the row's disqualification reason, or null when it is pickable. No
  // qualify hook = no rejected rows (the composer's behavior exactly).
  function reasonFor(section: string, model: string): string | null {
    return props.qualify ? props.qualify(section, model) : null;
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

  // Where the pop opens (card t_dac84a33). The composer's chip sits on the
  // floor, so the shared .model-pop anchors UP and nothing is measured. A
  // settings row asks for "down" - but the settings body CLIPS its
  // overflow, so a pop poking past a floor is simply invisible. At open the
  // chip measures its nearest scrolling container: the pop takes the roomier
  // side and the section body is capped to what is left, so the list scrolls
  // inside the pop instead of disappearing behind the clip.
  const flipDown = ref(false);
  const measured = ref(false);
  const popRoom = ref<Record<string, string>>({});

  function placePop() {
    measured.value = false;
    popRoom.value = {};
    if (props.drop !== "down") return;
    const chip = root.value?.getBoundingClientRect();
    if (!chip) return;
    let floor = window.innerHeight;
    let ceil = 0;
    for (let el = root.value?.parentElement ?? null; el; el = el.parentElement) {
      if (getComputedStyle(el).overflowY !== "visible") {
        const r = el.getBoundingClientRect();
        floor = r.bottom;
        ceil = r.top;
        break;
      }
    }
    const below = floor - chip.bottom - 12;
    const above = chip.top - ceil - 12;
    flipDown.value = below >= above;
    // search + footer + padding + the inherit row: the body keeps the rest,
    // never more than the grouped design's own 260px cap
    const room = (flipDown.value ? below : above) - 106;
    popRoom.value = {
      "--model-pop-body": `${Math.round(Math.max(120, Math.min(260, room)))}px`,
    };
    measured.value = true;
  }

  function openNow() {
    open.value = true;
    adding.value = false;
    editing.value = false;
    placePop();
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

<style scoped>
  /* the settings pickers open DOWN (their row sits mid-pane); the shared
     .model-pop anchors upward for the composer's floor chip - same flip
     BandSelect makes for its drop="down" seats */
  .model-select.down .model-pop {
    bottom: auto;
    top: calc(100% + 6px);
  }
  /* a MEASURED seat (settings) caps the section body to the room its own
     scrolling container leaves, so the list scrolls inside the pop instead
     of being clipped by the pane. The composer never sets the var. */
  .model-select.measured .model-body {
    max-height: var(--model-pop-body, 260px);
  }
</style>
