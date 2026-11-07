<template>
  <!-- omega webui: THE TOGGLE PILL (card t_d93864e7) - the family law
       lives in .agents/DESIGN.md (one switch; button, not checkbox).
       The 26px rounded pill with the 11px state DOT inside -
       dot shifts, border goes --ok when on, transparent background - the
       MCP tab's look (t_83f37525) extracted so the registry rows
       (skills/tools/agents) and the MCP rows speak the same toggle. The
       mechanics stay with the host: the arm/confirm two-step, the write
       path, the confirm note under the row. This owns the LOOK and the
       armed aria only. No :checked prop exists for Vue's prop diff
       to skip, so the t_87303e8f stale-arm trap is structurally dead
       for every host that adopts the pill. -->
  <button
    type="button"
    class="toggle-pill"
    :class="{ on: state, armed: armed }"
    :disabled="disabled"
    :aria-label="armed ? confirmLabel : label"
    :title="armed ? confirmLabel : label"
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
      /** the steady aria: "enable x" / "disable x" */
      label: string;
      /** the ARMED aria: the confirm question (armed = click two commits).
          Defaults to the steady label - a host without the two-step
          (none today) reads exactly as before. */
      confirmLabel?: string;
      /** the two-step middle state. Aria only - the visual arm feedback
          is the confirm note under the row, the MCP reference's shape;
          adding an armed border here would diverge from it. */
      armed?: boolean;
      /** the host's busy lock: one write at a time */
      disabled?: boolean;
    }>(),
    { confirmLabel: "", armed: false, disabled: false },
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