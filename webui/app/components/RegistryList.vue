<template>
  <!-- v0.14.1: the registry-browser list. ONE component, three
       consumers (skills, tools, agents) - rows with a name, one
       toggle each, and an expandable detail section. The parent owns
       the data (fetches + fold state through composables/omega.ts);
       this renders the rows and drives the toggle - ONE click, ONE
       write (card t_5051d523: the arm/confirm two-step is dead; it
       made every row need a double click), rendered with the shared
       TogglePill (card t_d93864e7). A host without the toggle seam
       (501) renders the list read-only: no pills, the state text only.
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
  <section
    class="registry-list"
    :class="{ 'feel-sessions': feel === 'sessions' }">
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
        :class="{
          'registry-off': r.disabled,
          'registry-row-open': openableRow(r),
          'reg-row-active': activeName === r.name,
        }">
        <div
          class="reg-row-head"
          @click="onHeadClick($event, r)">
          <button
            v-if="hasDetail(r)"
            class="registry-expander reg-row-lead"
            type="button"
            :aria-expanded="isExpanded(r.name)"
            :aria-label="isExpanded(r.name) ? `collapse ${r.name}` : `expand ${r.name}`"
            @click.stop="flipExpand(r)">
            <!-- card t_db378a0d: the expander glyph is the CONSUMER's - the
                 registry family keeps the text chevron it has always
                 rendered, and a tab adopting the sessions vocabulary asks
                 for `expander="chevron"` (the sessions sidebar's lucide
                 chevron-right, 12px, stroke, currentColor) which rotates
                 90deg while open. -->
            <Icon
              v-if="expander === 'chevron'"
              class="registry-expander-icon"
              :class="{ open: isExpanded(r.name) }"
              name="chevron-right"
              :size="12" />
            <template v-else>{{ isExpanded(r.name) ? "▾" : "▸" }}</template>
          </button>
          <div class="reg-row-main">
            <button
              v-if="openableRow(r)"
              type="button"
              class="reg-row-name registry-open"
              :title="`open ${r.name}`"
              @click.stop="emit('open', r)">{{ r.name }}</button>
            <span
              v-else
              class="reg-row-name">{{ r.name }}</span>
            <!-- the row law's subtitle: a flex line so bars/badges can
                 ride it; the dim 11px rides the .reg-row-sub anatomy
                 class (the consumer keeps no row typography). Card
                 t_2d8deb8c: the status vocabulary rides this line too -
                 a colored dot + the short state word (green = active,
                 dim = sleeping), cron's "next HH:MM" placement. -->
            <span
              v-if="r.description || r.status"
              class="reg-row-sub">
              <span
                v-if="r.description"
                class="reg-row-sub-text registry-desc">{{ r.description }}</span>
              <span
                v-if="r.status"
                class="reg-row-status"
                :class="{ 'status-on': r.status.on }">
                <i
                  class="reg-row-dot"
                  aria-hidden="true"></i>{{ r.status.label }}
              </span>
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
               dot. ONE activation applies the write (card t_5051d523):
               the arm step that ate the first click is gone, and the
               shared pill carries no armed state to resurrect it. A
               button whose state comes from the reactive model carries
               no :checked prop for Vue's prop diff to skip (t_87303e8f),
               so the checkbox revision-remount machinery is gone with
               it. A NO-TOGGLE row (card t_db378a0d, the default agent)
               renders NO control at all, and the consumer's own badge
               says why - a switch that can never legitimately move is
               noise, not a safeguard. -->
          <TogglePill
            v-if="toggleable && !r.noToggle"
            class="registry-toggle reg-row-ctrl"
            :state="!r.disabled"
            :disabled="busy !== null"
            :label="toggleLabel(r)"
            :hint="toggleHint(r)"
            @click="onToggle(r)" />
          <span
            v-else-if="showState && !r.noToggle"
            class="registry-state reg-row-ctrl"
            :aria-label="`${r.disabled ? 'enable' : 'disable'} ${r.name}`">{{ r.disabled ? "disabled" : "on" }}</span>
        </div>
        <!-- card t_2d8deb8c: the under-row preview line DIED with it
             (ruling 3) - the two-line anatomy is the law, the status
             word rides the subtitle. Card t_5051d523: the armed
             confirm note died with the arm step - the same consequence
             text rides the pill's title (toggleHint). -->
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
  import Icon from "./ui/Icon.vue";
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
          keeps the plain name span and byte-identical renders).
          card t_81109e1c: a PREDICATE form - the agents tab opens a
          chat only for a row whose agent has a session to bind, so the
          rows without one stay plain spans instead of dead buttons. */
      openable?: boolean | ((row: RegistryRow) => boolean);
      /** card t_db378a0d: the row's INTERACTION vocabulary. "quiet" (the
          default) is the ROW LAW's plain list - hierarchy by typography,
          one hairline, no boxes - and keeps every registry tab that has
          not taken its theming round byte-identical. "sessions" adopts
          the sessions sidebar's row feel for THIS consumer: chip radius,
          the z1-raised hover, the name brightening to the full text
          colour, the hairline fading out from under the raised chip, the
          whole HEAD line as the open surface, and an active row (see
          `activeName`). Round 1 = the agents tab only; cron / tools /
          skills / mcp / modules follow in their own rounds. */
      feel?: "quiet" | "sessions";
      /** the row that carries the sessions ACTIVE treatment - the one the
          operator has OPEN (the agents tab passes the agent whose session
          is the focused chat). "" = no row is active. Only meaningful
          under feel="sessions"; the quiet list ignores it. */
      activeName?: string;
      /** the expander glyph: "text" renders the registry's own ▸ / ▾,
          "chevron" the sessions sidebar's lucide chevron-right (12px,
          stroke, currentColor) rotating 90deg while open. Per consumer,
          because the registry family's five tabs take the vocabulary one
          round at a time. */
      expander?: "text" | "chevron";
    }>(),
    {
      busy: null,
      error: null,
      loaded: false,
      toggleable: false,
      showState: true,
      emptyText: "nothing registered",
      openable: false,
      feel: "quiet",
      activeName: "",
      expander: "text",
    },
  );

  const openableRow = (r: RegistryRow): boolean =>
    typeof props.openable === "function" ? props.openable(r) : props.openable;

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

  // --- one-click toggle (card t_5051d523) ---
  // Card t_87303e8f's checkbox trap needed the per-row remount hack
  // when the toggle was an input: a :checked prop Vue could skip. The
  // shared TogglePill (card t_d93864e7) is a button whose state comes
  // from the reactive model - no DOM prop to miss - so the remount
  // machinery is gone with the checkbox. The arm step is gone too: the
  // first (and only) click commits. The pill applies the intent and the
  // fold write re-renders the rows from the response.
  function onToggle(r: RegistryRow) {
    if (props.busy !== null) return;
    // card t_db378a0d: a no-toggle row never renders a pill; the guard
    // stays so a consumer cannot reach the write path by another door.
    if (r.noToggle) return;
    // the target direction IS the row's disabled flag: a disabled row
    // emits enabled=true (the fold write re-renders from the response)
    emit("toggle", r.name, r.disabled);
  }

  // toggleLabel is the pill's accessible name: the move it offers.
  const toggleLabel = (r: RegistryRow) =>
    `${r.disabled ? "enable" : "disable"} ${r.name}`;

  // toggleHint (card t_5051d523): the consequence text ("excluded from
  // the session prompt build") used to gate the second click. It now
  // rides the pill's title - the click never waits on it. The helper
  // speaks the consequence only; the title already carries the request
  // ("disable x") through the label.
  const toggleHint = (r: RegistryRow) =>
    r.noToggle ? "" : registryNote(props.kind, r.disabled);

  // --- the sessions feel's open surface (card t_db378a0d) ---
  // Under feel="sessions" the whole HEAD line is the open surface (the
  // sessions row's read), not just the name button. A control inside the
  // head already owns its own action - the name button and the expander
  // stop propagation themselves, and the pill's click is skipped here so
  // a toggle never opens a chat. The DETAIL below the head is never a
  // click surface: it carries the agent's edit form.
  function onHeadClick(e: MouseEvent, r: RegistryRow) {
    if (props.feel !== "sessions") return;
    if (!openableRow(r)) return;
    const el = e.target as Element | null;
    if (el && el.closest("button, a, input, select, textarea, label")) return;
    emit("open", r);
  }

  // --- expandable detail: per-row, click-driven, one set per mount ---
  const expanded = ref<Record<string, boolean>>({});

  const isExpanded = (name: string) => !!expanded.value[name];
  function flipExpand(r: RegistryRow) {
    expanded.value = { ...expanded.value, [r.name]: !expanded.value[r.name] };
    if (isExpanded(r.name)) emit("expand", r);
  }

  defineExpose({ flipExpand, isExpanded });
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
  /* card t_db378a0d: the sessions expander glyph - the sidebar's lucide
     chevron, closed at rest and rotated a quarter turn while the row is
     open (main.css .bucket-chevron's transition verbatim). Only a
     consumer that asked for `expander="chevron"` renders it. */
  .registry-expander-icon {
    display: block;
    transition: transform 0.12s ease;
  }
  .registry-expander-icon.open {
    transform: rotate(90deg);
  }
  /* --- card t_db378a0d: THE SESSIONS FEEL -------------------------------
     The agents tab's opt-in interaction vocabulary, lifted from the
     sessions sidebar's row law (main.css: .session-list li / li:hover /
     li.active / li.active .session-title) so the two lists read as one
     language. Every rule is gated on .feel-sessions: the quiet list -
     cron, tools, skills, mcp, modules - keeps the ROW LAW's plain render
     until its own round, byte for byte. The anatomy itself does not move:
     two lines, one right control, semibold name over a dim subtitle, one
     hairline at rest. */
  .registry-list.feel-sessions .registry-row {
    border-radius: var(--r-chip);
    /* the raised chip reaches past the text column so the row reads as
       ONE surface; the negative margin keeps the name on the list's own
       left edge, so nothing shifts when the hover lands */
    margin: 0 -8px;
    padding: 5px 8px;
    transition:
      background-color 0.12s ease,
      border-color 0.12s ease;
  }
  .registry-list.feel-sessions .registry-row-open {
    cursor: pointer;
  }
  .registry-list.feel-sessions .registry-row:hover,
  .registry-list.feel-sessions .registry-row.reg-row-active {
    background: var(--z1-raised);
    /* his fork ruling ("try first"): the hairline under a raised chip
       fades, so the chip never crosses a seam. One-line revert if it
       reads wrong: drop this border-color declaration, nothing else. */
    border-bottom-color: transparent;
  }
  .registry-list.feel-sessions .registry-row:hover:not(.registry-off) .reg-row-name {
    /* the rest opacity is 0.9; hover takes the name to the full text */
    opacity: 1;
  }
  /* the active row is the one the operator has OPEN - the sessions
     sidebar's li.active, name and all (the accent at the row law's
     resting 0.9 would read as a dimmed accent, so the open row carries
     the name at full strength) */
  .registry-list.feel-sessions .registry-row.reg-row-active .reg-row-name {
    color: var(--accent);
    opacity: 1;
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
  /* card t_2d8deb8c: the under-row preview DIED (ruling 3) - the
     under-row lines are the note line above only. */
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