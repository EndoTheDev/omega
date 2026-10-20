<template>
  <details
    ref="el"
    class="toolcall">
    <summary @click="onSummaryClick">
      <span
        class="tool-dot"
        :class="status" />
      <span class="tool-name">{{ name }}</span>
      <span
        v-if="elapsedLabel"
        class="tool-elapsed">{{ elapsedLabel }}</span
      >
    </summary>
    <div
      v-if="output"
      class="tool-section">
      <span class="tool-section-label">input</span>
      <pre class="tool-output">{{ output }}</pre>
    </div>
    <div
      v-if="result"
      class="tool-section">
      <span class="tool-section-label">output</span>
      <pre
        class="tool-output tool-result"
        :class="{ 'is-error': status === 'error' }"
        >{{ result }}</pre
      >
    </div>
  </details>
</template>

<script setup lang="ts">
  import { nextTick, onMounted, ref, watch } from "vue";

  import { toolRowWantOpen, useToolDisplay } from "../../composables/toolDisplay";
  import { useElapsed } from "../../composables/useElapsed";

  const props = defineProps<{
    name: string;
    status: string;
    interrupted?: boolean;
    output?: string;
    result?: string;
    // server-measured duration from restored history - wins over the
    // live meter (restored items mount final and never start one)
    finalMs?: number;
  }>();

  // Elapsed counter (tool_call event -> tool_result arrival): ticks while
  // the call is running, freezes at the final value once the result
  // lands - the finished line keeps showing how long the call took.
  // Restored history arrives already-final and renders nothing.
  // live = no result yet AND the stream survived it: a dead stream never
  // delivers the tool result, so the interrupted flag stops the meter -
  // without it the counter would tick forever over a call that cannot
  // finish. It stays honest: dot and status keep saying running.
  const elapsedLabel = useElapsed(
    () => props.finalMs,
    () => props.status === "running" && !props.interrupted,
    // a restored STILL-RUNNING call counts on from the server's
    // elapsed-so-far (live-run merge) - not from the mount moment
    () => props.elapsedMs,
  );

  // --- auto open/close (/tools auto parity, card t_868633b3) ---
  // The settings knob (auto | expanded | collapsed) decides what a row
  // does on its own: auto expands a running call and collapses it once
  // it settles. A manual summary click PINS the row - its open state is
  // the user's for the session, and mode flips and status transitions
  // recompute around it, never over it. The details element stays
  // uncontrolled (no :open binding): the browser owns the toggle, this
  // side applies the mode on mount and on status/mode change only.
  const el = ref<HTMLDetailsElement | null>(null);
  const display = useToolDisplay();
  const pinned = ref<boolean | null>(null);

  function applyAuto() {
    if (pinned.value !== null || !el.value) return;
    el.value.open = toolRowWantOpen(props.status, display.value, props.interrupted);
  }

  // nextTick: the status flip and the result's v-if land in the same
  // render pass - apply after the DOM settles.
  watch(
    [() => props.status, () => props.interrupted, display],
    () => void nextTick(applyAuto),
  );
  onMounted(applyAuto);

  // Click fires BEFORE the browser's toggle default action, so !open is
  // the state the click is about to produce - pin that, and auto mode
  // never touches this row again this session.
  function onSummaryClick() {
    if (!el.value) return;
    pinned.value = !el.value.open;
  }
</script>
