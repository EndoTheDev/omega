<template>
  <!--
    v0.14.1: the registry-browser list.
    v0.14.3: the insights tab joins as the read-only stat consumer - `showState=false`
    drops the right state text (insight rows have no fold), and the generic #side slot
    seats a row's right cluster (the CSS width bars the plain renderer can't express).:
    the rows ride THE ROW LAW - the shared assets/css/registry-row.css anatomy (name /
    subtitle stack, one right control, hairline separators).
  -->
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
            <!-- the expander glyph is the CONSUMER's. -->
            <Icon
              v-if="expander === 'chevron'"
              class="registry-expander-icon"
              :class="{ open: isExpanded(r.name) }"
              name="chevron-right"
              :size="12" />
            <template v-else>{{ isExpanded(r.name) ? "▾" : "▸" }}</template>
          </button>
          <div class="reg-row-main">
            <!--
              the NAME LINE: the row law says a name's companion badge rides BESIDE.
            -->
            <div class="reg-row-name-line">
              <button
                v-if="openableRow(r)"
                type="button"
                class="reg-row-name registry-open"
                :title="`open ${r.name}`"
                @click.stop="emit('open', r)">{{ r.name }}</button>
              <span
                v-else
                class="reg-row-name">{{ r.name }}</span>
              <slot
                name="name-side"
                :row="r" />
            </div>
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
            <!--
              THE PREVIEW LINE - one line of what the subject last said, with its age.
              Its own line under the subtitle, so the facts line keeps its exact shape;
              a row without a preview renders byte-identically to before.
            -->
            <span
              v-if="r.preview"
              class="reg-row-sub reg-row-preview">
              <span class="registry-preview">{{ r.preview }}</span>
              <span
                v-if="r.previewAt"
                class="registry-preview-at">{{ r.previewAt }}</span>
            </span>
          </div>
          <slot
            name="side"
            :row="r" />
          <!--
            the toggle is the shared TogglePill.
            A NO-TOGGLE row renders NO control at all, and the consumer's own badge
            says why - a switch that can never legitimately move is noise, not a
            safeguard.
          -->
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
        <!--
          the under-row note line is gone.
          brought back exactly ONE under-row line (the preview above) for the consumer
          that has something to say.: the armed confirm note died with the arm step -
          the same consequence text rides the pill's title (toggleHint).
        -->
        <template v-if="isExpanded(r.name)">
          <ul
            v-if="hasDetail(r)"
            class="registry-detail">
            <li
              v-for="line in r.detail"
              :key="line"
              class="registry-detail-line"><span>{{ line }}</span></li>
          </ul>
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
      /**
       * the name click opens the row's content in a main-pane viewer tab (the skills
       * consumer only; everything else keeps the plain name span and byte-identical
       * renders).: a PREDICATE form - the agents tab opens a chat only for a row whose
       * agent has a session to bind, so the rows without one stay plain spans instead
       * of dead buttons.
       */
      openable?: boolean | ((row: RegistryRow) => boolean);
      /**
       * the row's INTERACTION vocabulary.
       * "quiet" (the default) is the ROW LAW's plain list - hierarchy by typography,
       * one hairline, no boxes - and keeps every registry tab that has not taken its
       * theming round byte-identical.
       */
      feel?: "quiet" | "sessions";
      activeName?: string;
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

  // toggle carries the CONFIRMED direction.
  const emit = defineEmits<{
    (e: "toggle", name: string, enabled: boolean): void;
    (e: "expand", row: RegistryRow): void;
    (e: "open", row: RegistryRow): void;
  }>();

  const hasDetail = (r: RegistryRow) => !!(r.detail && r.detail.length);

  const rowKey = (r: RegistryRow) => r.name;

  // one-click toggle --- checkbox trap needed the per-row remount hack when.
  function onToggle(r: RegistryRow) {
    if (props.busy !== null) return;
    // a no-toggle row never renders a pill; the guard stays so a consumer cannot.
    if (r.noToggle) return;
    // the target direction IS the row's disabled flag.
    emit("toggle", r.name, r.disabled);
  }

  // toggleLabel is the pill's accessible name: the move it offers.
  const toggleLabel = (r: RegistryRow) =>
    `${r.disabled ? "enable" : "disable"} ${r.name}`;

  // toggleHint: the consequence text ("excluded from the session prompt build") used.
  // It now rides the pill's title - the click never waits on it.
  const toggleHint = (r: RegistryRow) =>
    r.noToggle ? "" : registryNote(props.kind, r.disabled);

  // the sessions feel's open surface --- Under feel="sessions" the whole HEAD line. A
  // control inside the head already owns its own action - the name button and the
  // expander stop propagation themselves, and the pill's click is skipped here so a
  // toggle never opens a chat. The DETAIL below the head is never a click surface: it
  // carries the agent's edit form.
  function onHeadClick(e: MouseEvent, r: RegistryRow) {
    if (props.feel !== "sessions") return;
    if (!openableRow(r)) return;
    const el = e.target as Element | null;
    if (el && el.closest("button, a, input, select, textarea, label")) return;
    emit("open", r);
  }

  const expanded = ref<Record<string, boolean>>({});

  const isExpanded = (name: string) => !!expanded.value[name];
  function flipExpand(r: RegistryRow) {
    expanded.value = { ...expanded.value, [r.name]: !expanded.value[r.name] };
    if (isExpanded(r.name)) emit("expand", r);
  }

  defineExpose({ flipExpand, isExpanded });
</script>

<style scoped>
  .registry-list {
    padding: 8px 0 14px;
  }
  .registry-rows {
    list-style: none;
    margin: 0;
    padding: 0;
  }
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
  .registry-expander-icon {
    display: block;
    transition: transform 0.12s ease;
  }
  .registry-expander-icon.open {
    transform: rotate(90deg);
  }
  /*
   * THE SESSIONS FEEL ------------------------------- The agents tab's opt-in.
   * Every rule is gated on.feel-sessions: the quiet list - cron, tools, skills, mcp,
   * modules - keeps the ROW LAW's plain render until its own round, byte for byte.
   * Round 1 wore the negative margin at rest too, which widened the quiet list's
   * separator ~16px - "exactly as today" is literal (review round 1, nit 3).
   */
  .registry-list.feel-sessions .registry-row {
    transition:
      background-color 0.12s ease,
      border-color 0.12s ease,
      border-radius 0.12s ease,
      margin 0.12s ease,
      padding 0.12s ease;
  }
  .registry-list.feel-sessions .registry-row-open {
    cursor: pointer;
  }
  .registry-list.feel-sessions .registry-row:hover,
  .registry-list.feel-sessions .registry-row.reg-row-active {
    background: var(--z1-raised);
    border-radius: var(--r-chip);
    /*
     * the raised chip reaches past the text column so the row reads as ONE surface; the
     * negative margin and the matching padding cancel each other, so the name never
     * shifts - only the chip's edges grow / /
     */
    margin: 0 -8px;
    padding: 5px 8px;
    /*
     * his fork rule ("try first"): the hairline under a raised chip fades, so the
     * chip never crosses a seam.
     */
    border-bottom-color: transparent;
  }
  .registry-list.feel-sessions .registry-row:has(+ .registry-row:hover),
  .registry-list.feel-sessions .registry-row:has(+ .registry-row.reg-row-active) {
    border-bottom-color: transparent;
  }
  .registry-list.feel-sessions .registry-row:hover:not(.registry-off) .reg-row-name {
    opacity: 1;
  }
  /* the active row is the one the operator has OPEN. */
  .registry-list.feel-sessions .registry-row.reg-row-active .reg-row-name {
    color: var(--accent);
    opacity: 1;
  }
  .registry-state {
    font-size: 10px;
    opacity: 0.5;
    padding-top: 0;
  }
  /* the desc: the row law's own voice rides.reg-row-sub-text. */
  .registry-desc {
    min-width: 0;
  }
  /*
   * the preview line.
   * One line, ellipsized - a long report never grows the row - with its age pinned
   * right.
   */
  .reg-row-preview {
    margin-top: 1px;
  }
  .registry-preview {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .registry-preview-at {
    flex: none;
    opacity: 0.7;
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