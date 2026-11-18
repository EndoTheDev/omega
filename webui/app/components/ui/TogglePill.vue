<template>
  <!--
    omega webui: THE TOGGLE PILL.
    The pill carries NO armed state and NO confirm label any more, so that shape cannot
    come back through the shared component: one activation emits exactly one intent and
    the host applies it.
    The consequence text that used to gate the second click now rides the pill's title
    (`hint`) - readable on hover, never a gate - and it is folded into the accessible
    name: a screen reader never sees a title tooltip, so `:aria-label="label"` alone
    left the consequence (module dependents, registry exclusions) unheard.
    No:checked prop exists for Vue's prop diff to skip, so the stale-arm trap stays
    structurally dead.
  -->
  <button
    type="button"
    class="toggle-pill"
    :class="{ on: state }"
    :disabled="disabled"
    :aria-label="accessibleName"
    :title="accessibleName"
    @click="emit('click')">
    <span class="toggle-pill-thumb"></span>
  </button>
</template>

<script setup lang="ts">
  import { computed } from "vue";

  // state (not modelValue: a switch, not a select.
  const props = withDefaults(
    defineProps<{
      state: boolean;
      label: string;
      hint?: string;
      disabled?: boolean;
    }>(),
    { hint: "", disabled: false },
  );

  // accessibleName: the next move plus its consequence, composed ONCE and bound to both
  // attributes so the tooltip and the accessible name can never drift.
  const accessibleName = computed(() =>
    props.hint ? `${props.label} - ${props.hint}` : props.label,
  );

  const emit = defineEmits<{
    (e: "click"): void;
  }>();
</script>

<style scoped>
  /* the pill: the MCP tab's toggle, byte-for-byte. */
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