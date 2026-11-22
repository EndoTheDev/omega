<template>
  <!--
    the nav cluster - back / forward / command palette.
    VS Code states: the ends DIM via the disabled attribute - a real disabled button,
    never a click-swallowing lookalike.
  -->
  <div
    class="nav-cluster"
    :class="{ 'palette-open': paletteOpen }"
    role="group"
    aria-label="navigation">
    <button
      class="icon-btn nav-btn"
      aria-label="Back"
      title="Back (Alt+Left)"
      :disabled="!canBack"
      @click="$emit('back')">
      <Icon
        name="nav-back"
        :size="16" />
    </button>
    <button
      class="icon-btn nav-btn"
      aria-label="Forward"
      title="Forward (Alt+Right)"
      :disabled="!canForward"
      @click="$emit('forward')">
      <Icon
        name="nav-forward"
        :size="16" />
    </button>
    <!--
      the affordance's placeholder is a LIVE clock (re-rendered on every minute
      boundary, never a frozen label).
      data-palette-trigger: CommandPalette's outside-click handler exempts it, so
      opening the palette with this click cannot close it again on the way out.
      The clock keeps ticking while hidden: the timer belongs to this component, not to
      the button's visibility, so Esc brings back a current time, never a stale one.
    -->
    <button
      v-if="!paletteOpen"
      class="nav-palette-input"
      type="button"
      aria-label="Command palette (Ctrl+K)"
      title="Command palette (Ctrl+K)"
      data-palette-trigger
      @click="$emit('open-palette')">
      <span
        class="nav-palette-clock"
        aria-hidden="true">{{ clock }}</span>
    </button>
    <!-- the palette's seat: CommandPalette teleports its input here. -->
    <span
      id="nav-palette-slot"
      class="nav-palette-slot" />
    <button
      class="icon-btn nav-btn"
      aria-label="Command palette"
      title="Command palette (Ctrl+K)"
      data-palette-trigger
      @click="$emit('open-palette')">
      <Icon
        name="palette"
        :size="16" />
    </button>
  </div>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from "vue";
  import Icon from "../ui/Icon.vue";

  // the dims ride the nav history's canBack/canForward (app.vue passes the computed's
  // values down - cluster stays a dumb four-control strip). `paletteOpen` is the
  // takeover's one bit: the cluster hides its chrome (the arrows + the Ctrl+K toggle)
  // so the seat never moves, and the affordance stands down for the teleported field.
  defineProps<{
    canBack: boolean;
    canForward: boolean;
    paletteOpen: boolean;
  }>();

  defineEmits<{
    (e: "back"): void;
    (e: "forward"): void;
    (e: "open-palette"): void;
  }>();

  // the fake input's placeholder. The timer re-arms on the NEXT minute boundary, so the
  // label is never more than a minute stale and the bar re-renders once a minute - not
  // once a second, and never at all once the component is gone.
  function formatClock(d: Date): string {
    const pad = (n: number) => String(n).padStart(2, "0");
    return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }

  const clock = ref(formatClock(new Date()));
  let clockTimer: ReturnType<typeof setTimeout> | null = null;

  function armClock(): void {
    const rest = 60_000 - (Date.now() % 60_000);
    clockTimer = setTimeout(() => {
      clock.value = formatClock(new Date());
      armClock();
    }, rest);
  }

  onMounted(armClock);
  onBeforeUnmount(() => {
    if (clockTimer !== null) clearTimeout(clockTimer);
    clockTimer = null;
  });
</script>
