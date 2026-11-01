<template>
  <!-- v0.14.1: the registry-browser list. ONE component, three
       consumers (skills, tools, agents) - rows with a name, one
       toggle each, and an expandable detail section. The parent owns
       the data (fetches + fold state through composables/omega.ts);
       this renders the rows and drives the two-step toggle (the same
       arm/confirm mechanic the module list rides, no accidental
       first-click flips), rendered with the shared TogglePill
       (card t_d93864e7). A host without the toggle seam (501) renders
       the list read-only: no pills, the state text only.
       v0.14.3: the insights tab joins as the read-only stat consumer -
       `showState=false` drops the right state text (insight rows
       have no fold), and the generic #side slot seats a row's right
       cluster (the CSS width bars the plain renderer can't express).
       card t_561dc4e0: the rows ride THE ROW LAW - the shared
       assets/css/registry-row.css anatomy (name / subtitle stack, one
       right control, hairline separators). The left stack is NAME on
       line 1, the description dim on line 2; the expander becomes the
       row's LEAD chevron (the Modules-tab reading); the pill stays
       the one right control (journey 92's button.registry-toggle
       selector). The row's own geometry lives in the anatomy file --
       this stylesheet keeps only the row's specifics. -->
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
        class="reg-row registry-row"
        :class="{ 'registry-off': r.disabled }">
        <div class="reg-row-head">
          <button
            v-if="hasDetail(r)"
            class="registry-expander reg-row-lead"
            type="button"
            :aria-expanded="isExpanded(r.name)"
            :aria-label="isExpanded(r.name) ? `collapse ${r.name}` : `expand ${r.name}`"
            @click.stop="flipExpand(r)">{{ isExpanded(r.name) ? "▾" : "▸" }}</button>
          <div class="reg-row-main">
            <button
              v-if="openable"
              type="button"
              class="reg-row-name registry-open"
              :title="`open ${r.name}`"
              @click.stop="emit('open', r)">{{ r.name }}</button>
            <span
              v-else
              class="reg-row-name">{{ r.name }}</span>
            <!-- the row law's subtitle: a flex line so bars/badges can
                 ride it; the dim 11px rides the .reg-row-sub anatomy
                 class (the consumer keeps no row typography). -->
            <span
              v-if="r.description"
              class="reg-row-sub">
              <span class="reg-row-sub-text registry-desc">{{ r.description }}</span>
            </span>
          </div>
          <!-- the generic row side: numbers, bars, badges - things the
               plain name/desc stack doesn't express; seated on line 1
               between the name and the right control -->
          <slot
            name="side"
            :row="r" />
          <!-- the toggle is the shared TogglePill (card t_d93864e7): the
               MCP tab's pill look, now here too - same 26px pill, same
               dot, same armed aria. A button whose armed state comes from
               the reactive model carries no :checked prop for Vue's prop
               diff to skip (t_87303e8f), so the checkbox revision-remount
               machinery is gone with it. -->
          <TogglePill
            v-if="toggleable"
            class="registry-toggle reg-row-ctrl"
            :state="!r.disabled"
            :disabled="busy !== null"
            :armed="armed === r.name"
            :label="`${r.disabled ? 'enable' : 'disable'} ${r.name}`"
            :confirm-label="`confirm apply to ${r.name}?`"
            @click="onToggle(r)" />
          <span
            v-else-if="showState"
            class="registry-state reg-row-ctrl"
            :aria-label="`${r.disabled ? 'enable' : 'disable'} ${r.name}`">{{ r.disabled ? "disabled" : "on" }}</span>
        </div>
        <span
          v-if="armed === r.name"
          class="registry-confirm reg-row-note">{{ noteFor(r) }}</span>
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
  /* card t_561dc4e0: the row geometry (paddings, separators, the
     name/subtitle typography, the right-control seat) lives in the
     shared assets/css/registry-row.css - ONE definition, five tabs.
     The section's old 12px horizontal inset dies with it: the row law
     is the Cron reference's 12px pane inset, which the consumer body
     (.skills-body / .tools-body) already carries - this block keeps
     only the vertical seat. */
  .registry-list {
    padding: 8px 0 14px;
  }
  .registry-rows {
    list-style: none;
    margin: 0;
    padding: 0;
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
  .registry-off .reg-row-name {
    opacity: 0.55;
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
    padding-top: 0;
  }
  /* the desc: the row law's own voice rides .reg-row-sub-text; only
     the flex seat (it is the subtitle's one and only text child) is
     local - the dim is the .reg-row-sub anatomy's, nothing more. */
  .registry-desc {
    min-width: 0;
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