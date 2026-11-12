<template>
  <!-- omega webui: THE TOGGLE PILL (card t_d93864e7) - the family law
       lives in .agents/DESIGN.md (one switch; button, not checkbox).
       The 26px rounded pill with the 11px state DOT inside -
       dot shifts, border goes --ok when on, transparent background - the
       MCP tab's look (t_83f37525) extracted so the registry rows
       (skills/tools/agents) and the MCP rows speak the same toggle. The
       MECHANICS stay with the host: the write path, the busy lock, the
       consequence hint. This owns the LOOK only.

       CARD t_5051d523: ONE CLICK = ONE SWITCH. The two-step arm/confirm
       (click one armed the row, click two committed) is DEAD - it made
       every tab's enable/disable need a double click. The pill carries
       NO armed state and NO confirm label any more, so that shape
       cannot come back through the shared component: one activation
       emits exactly one intent and the host applies it. The consequence
       text that used to gate the second click now rides the pill's
       title (`hint`) - readable on hover, never a gate. A native
       <button> keeps keyboard parity: Enter and Space fire the same
       single click, so there is no keydown path to double-fire. No
       :checked prop exists for Vue's prop diff to skip, so the
       t_87303e8f stale-arm trap stays structurally dead. -->
  <button
    type="button"
    class="toggle-pill"
    :class="{ on: state }"
    :disabled="disabled"
    :aria-label="label"
    :title="hint ? `${label} - ${hint}` : label"
    @click="emit('click')">
    <span class="toggle-pill-thumb"></span>
  </button>
</template>

<script setup lang="ts">
  // state (not modelValue: a switch, not a select - the click emits the
  // INTENT and the host owns the truth, re-rendering the row from the
  // server's answer; the pill carries no persistent state of its own).
  withDefaults(
    defineProps<{
      /** the row's current state, rendered as the dot + border truth */
      state: boolean;
      /** the accessible name: "enable x" / "disable x" (the next move) */
      label: string;
      /** the consequence hint (what an exclusion means, a module's
          dependents). Tooltip only - card t_5051d523 killed the arm
          step that used to render this text as a gate. */
      hint?: string;
      /** the host's busy lock: one write at a time */
      disabled?: boolean;
    }>(),
    { hint: "", disabled: false },
  );

  const emit = defineEmits<{
    (e: "click"): void;
  }>();
</script>

<style scoped>
  /* the pill: the MCP tab's toggle (t_83f37525), byte-for-byte - this IS
     the design reference every host converges on. Tokens only. */
  .toggle-pill {
    width: 26px;
    height: 15px;
    padding: 0;
    border: 1px solid color-mix(in srgb, currentcolor 30%, transparent);
    border-radius: 8px;
    background: transparent;
    position: relative;
    cursor: pointer;
    flex-shrink: 0;
  }
  .toggle-pill:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .toggle-pill-thumb {
    position: absolute;
    top: 1px;
    left: 1px;
    width: 11px;
    height: 11px;
    border-radius: 50%;
    background: var(--text-dim);
  }
  .toggle-pill.on {
    border-color: var(--ok);
  }
  .toggle-pill.on .toggle-pill-thumb {
    left: 12px;
    background: var(--ok);
  }
</style>