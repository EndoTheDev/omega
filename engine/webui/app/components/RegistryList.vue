<template>
  <!-- v0.14.1: the registry-browser list. ONE component, three
       consumers (skills, tools, agents) - rows with a name, one
       toggle each, and an expandable detail section. The parent owns
       the data (fetches + fold state through composables/omega.ts);
       this renders the rows and drives the two-step toggle (the same
       arm/confirm mechanic the module list rides, no accidental
       first-click flips), rendered with the shared TogglePill
       (card t_d93864e7). A host without the toggle seam (501) renders
       the list read-only: no checkboxes, the state text only.
       v0.14.3: the insights tab joins as the read-only stat consumer -
       `showState=false` drops the on/disabled state text (insight rows
       have no fold), and the generic #side slot seats a row's right
       cluster (the CSS width bars the plain renderer can't express);
       existing consumers ride the defaults, byte-identical renders. -->
  <section class="registry-list">
    <p
      v-if="error"
      class="settings-error registry-error">{{ error }}</p>
    <ul
      v-if="rows.length"
      class="registry-rows">
      <li
        v-for="r in rows"
        :key="rowKey(r)"
        class="registry-row"
        :class="{ 'registry-off': r.disabled }">
        <span class="registry-main">
          <button
            v-if="openable"
            type="button"
            class="registry-name registry-open"
            :title="`open ${r.name}`"
            @click.stop="emit('open', r)">{{ r.name }}</button>
          <span
            v-else
            class="registry-name">{{ r.name }}</span>
          <span
            v-if="r.description"
            class="registry-desc">{{ r.description }}</span>
          <!-- the generic row side: numbers, bars, badges - things the
               plain name/desc line doesn't express -->
          <slot
            name="side"
            :row="r" />
          <button
            v-if="hasDetail(r)"
            class="registry-expander"
            type="button"
            :aria-expanded="isExpanded(r.name)"
            :aria-label="isExpanded(r.name) ? `collapse ${r.name}` : `expand ${r.name}`"
            @click.stop="flipExpand(r)">{{ isExpanded(r.name) ? "▾" : "▸" }}</button>
          <!-- the toggle is the shared TogglePill (card t_d93864e7): the
               MCP tab's pill look, now here too - same 26px pill, same
               dot, same armed aria. A button whose armed state comes from
               the reactive model carries no :checked prop for Vue's prop
               diff to skip (t_87303e8f), so the checkbox revision-remount
               machinery is gone with it. -->
          <TogglePill
            v-if="toggleable"
            class="registry-toggle"
            :state="!r.disabled"
            :disabled="busy !== null"
            :armed="armed === r.name"
            :label="`${r.disabled ? 'enable' : 'disable'} ${r.name}`"
            :confirm-label="`confirm apply to ${r.name}?`"
            @click="onToggle(r)" />
          <span
            v-else-if="showState"
            class="registry-state"
            :aria-label="`${r.disabled ? 'enable' : 'disable'} ${r.name}`">{{ r.disabled ? "disabled" : "on" }}</span>
        </span>
        <span
          v-if="armed === r.name"
          class="registry-confirm">{{ noteFor(r) }}</span>
        <!-- card t_c7222095: the optional under-row preview (the merged
             agent row shows the continuous last report under the facts;
             consumers that do not set it render nothing). -->
        <span
          v-if="r.preview"
          class="registry-preview">{{ r.preview }}</span>
        <template v-if="isExpanded(r.name)">
          <ul
            v-if="hasDetail(r)"
            class="registry-detail">
            <li
              v-for="line in r.detail"
              :key="line"
              class="registry-detail-line"><span>{{ line }}</span></li>
          </ul>
          <!-- richer consumers (the agents tab renders the config
               summary + session list) slot in here; plain consumers get
               the detail lines above only. -->
          <slot
            name="detail"
            :row="r" />
        </template>
      </li>
    </ul>
    <p
      v-else-if="loaded"
      class="settings-empty">{{ emptyText }}</p>
    <p
      v-if="busy"
      class="registry-busy">{{ busy }}…</p>
  </section>
</template>

<script setup lang="ts">
  import { ref } from "vue";
  import {
    registryNote,
    type RegistryKind,
    type RegistryRow,
  } from "~/composables/omega";

  // The rows the parent built (registry listing + fold flags merged
  // through withDisabledFlags). toggleable=false when the host
  // answered 501 (no seam): the list renders read-only.
  const props = withDefaults(
    defineProps<{
      kind: RegistryKind;
      rows: RegistryRow[];
      busy?: string | null;
      error?: string | null;
      loaded?: boolean;
      toggleable?: boolean;
      showState?: boolean;
      emptyText?: string;
      /** card t_36ceb99b: the name click opens the row's content in a
          main-pane viewer tab (the skills consumer only; everything else
          keeps the plain name span and byte-identical renders) */
      openable?: boolean;
    }>(),
    { busy: null, error: null, loaded: false, toggleable: false, showState: true, emptyText: "nothing registered", openable: false },
  );

  // toggle carries the CONFIRMED direction: the parent PUTs and
  // re-renders the rows from the response fold. expand tells the
  // parent a row opened (the agents tab lazily loads sessions). open
  // (card t_36ceb99b): the NAME click opens the row's content in a
  // main-pane viewer tab - opt-in via `openable` (the skills tab only);
  // the expander stays the detail toggle and keeps @click.stop.
  const emit = defineEmits<{
    (e: "toggle", name: string, enabled: boolean): void;
    (e: "expand", row: RegistryRow): void;
    (e: "open", row: RegistryRow): void;
  }>();

  const hasDetail = (r: RegistryRow) => !!(r.detail && r.detail.length);

  // one key per row identity - the registry owns the names, no remount
  // tricks since the toggle became the shared pill
  const rowKey = (r: RegistryRow) => r.name;

  // --- two-step toggle: arm, then apply (ModuleToggleList mechanic) ---
  // Card t_87303e8f's checkbox trap needed the per-row remount hack
  // when the toggle was an input: a :checked prop Vue could skip. The
  // shared TogglePill (card t_d93864e7) is a button whose armed state
  // comes from the reactive model - no DOM prop to miss - so the
  // remount machinery is gone with the checkbox.
  const armed = ref<string | null>(null);

  function onToggle(r: RegistryRow) {
    if (props.busy !== null) return;
    if (armed.value !== r.name) {
      // arm: the note below the row shows what the exclusion means
      armed.value = r.name;
      return;
    }
    armed.value = null;
    // the target direction IS the row's disabled flag: a disabled row
    // emits enabled=true (the fold write re-renders from the response)
    emit("toggle", r.name, r.disabled);
  }

  const noteFor = (r: RegistryRow) => registryNote(props.kind, r.name, r.disabled);

  // --- expandable detail: per-row, click-driven, one set per mount ---
  const expanded = ref<Record<string, boolean>>({});

  const isExpanded = (name: string) => !!expanded.value[name];
  function flipExpand(r: RegistryRow) {
    expanded.value = { ...expanded.value, [r.name]: !expanded.value[r.name] };
    if (isExpanded(r.name)) emit("expand", r);
  }

  defineExpose({ armed, flipExpand, isExpanded });
</script>

<style scoped>
  .registry-list {
    padding: 8px 12px 14px;
  }
  .registry-rows {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .registry-row {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 5px 0;
    border-bottom: 1px solid color-mix(in srgb, currentcolor 10%, transparent);
  }
  .registry-row:last-child {
    border-bottom: none;
  }
  .registry-main {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .registry-name {
    font-size: 12px;
    font-weight: 600;
    opacity: 0.9;
  }
  /* the openable name (t_36ceb99b): a button styled back into the row -
     same voice, the hover raises it like the other row affordances */
  .registry-open {
    border: none;
    background: none;
    color: inherit;
    padding: 0;
    cursor: pointer;
    font-family: inherit;
    text-align: left;
  }
  .registry-open:hover {
    text-decoration: underline;
  }
  .registry-off .registry-name {
    opacity: 0.55;
  }
  .registry-desc {
    font-size: 11px;
    opacity: 0.6;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .registry-expander {
    border: none;
    background: none;
    color: inherit;
    opacity: 0.55;
    cursor: pointer;
    font-size: 10px;
    padding: 0 2px;
  }
  .registry-expander:hover {
    opacity: 0.9;
  }
  .registry-state {
    font-size: 10px;
    opacity: 0.5;
  }
  /* the shared TogglePill (t_d93864e7): the row line speaks baseline,
     the pill sits centered on it - a 15px button baseline-aligned would
     park its top above the 12px name. The pill's own CSS never lives
     here (one component, zero copies). */
  .registry-main .toggle-pill {
    align-self: center;
  }
  .registry-confirm {
    font-size: 11px;
    opacity: 0.75;
  }
  /* card t_c7222095: the under-row preview - one dim line, ellipsed */
  .registry-preview {
    font-size: 11px;
    opacity: 0.6;
    overflow: hidden;
    padding-left: 10px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .registry-detail {
    list-style: none;
    margin: 2px 0 2px 10px;
    padding: 0;
  }
  .registry-detail-line {
    font-size: 11px;
    opacity: 0.6;
    padding: 1px 0;
  }
  .registry-detail-line span::before {
    content: "\00b7\2002";
    opacity: 0.4;
  }
  .registry-error {
    margin: 0 0 6px;
  }
  .registry-busy {
    margin: 4px 0 0;
    font-size: 11px;
    opacity: 0.5;
  }
</style>