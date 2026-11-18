<template>
  <details
    ref="el"
    class="toolcall">
    <summary @click="onSummaryClick">
      <Icon
        name="tools"
        class="tool-ic"
        :class="status" />
      <span class="tool-name">{{ name }}</span>
      <span
        v-if="gate"
        class="tool-gate">{{ gate }}</span
      >
      <span
        v-if="elapsedLabel || answerLabel"
        class="tool-elapsed"
        :title="answerLabel ? 'call duration + response return' : undefined"
        >{{ elapsedLabel }}<template v-if="answerLabel"> + {{ answerLabel }}</template></span
      >
      <span
        v-if="status === 'pending'"
        class="tool-elapsed">pending</span
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
    <div
      v-else-if="liveText"
      class="tool-section">
      <span class="tool-section-label">{{ liveSettled ? "output" : "live output" }}</span>
      <pre class="tool-output tool-live">{{ liveText }}</pre>
    </div>
    <!--
      variant anchor: a tool CALL row can be a branch's active head (a text-less.
    -->
    <VariantSwitcher
      v-if="variant"
      class="tool-variant"
      :variant="variant"
      @select="emit('select', $event)" />
  </details>
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, ref, watch } from "vue";

  import Icon from "../ui/Icon.vue";
  import { toolRowWantOpen, useToolDisplay } from "../../composables/toolDisplay";
  import { formatDurationMs } from "../../composables/omega";
  import { isLive } from "../../composables/toolStatus";
  import { useElapsed } from "../../composables/useElapsed";
  import VariantSwitcher from "./VariantSwitcher.vue";
  import type { VariantBlock } from "../../composables/omega";

  const props = defineProps<{
    name: string;
    status: string;
    output?: string;
    result?: string;
    // server-measured duration from restored history - wins over the
    // live meter (restored items mount final and never start one)
    finalMs?: number;
    elapsedMs?: number;
    answerMs?: number;
    gate?: string;
    variant?: VariantBlock | null;
    liveText?: string;
    liveSettled?: boolean;
  }>();

  const emit = defineEmits<{
    (e: "select", index: number): void;
  }>();

  // Elapsed counter (tool_call event -> tool_result arrival). The gate is the SHARED
  // live-set predicate (composables/toolStatus.ts), never a local spelling: the row's
  // own word and its meter must key on the same set.
  const elapsedLabel = useElapsed(
    () => props.finalMs,
    () => isLive(props.status),
    // a restored STILL-RUNNING call counts on from the server's elapsed-so-far.
    () => props.elapsedMs ?? 0,
  );

  // the response-return half: server-measured, never metered.
  const answerLabel = computed(() =>
    props.answerMs != null ? formatDurationMs(props.answerMs) : "",
  );

  // auto open/close --- The settings knob (auto | expanded | collapsed) decides. A
  // manual summary click PINS the row - its open state is the user's for the session,
  // and mode flips and status transitions recompute around it, never over it.
  const el = ref<HTMLDetailsElement | null>(null);
  const display = useToolDisplay();
  const pinned = ref<boolean | null>(null);

  function applyAuto() {
    if (pinned.value !== null || !el.value) return;
    el.value.open = toolRowWantOpen(props.status, display.value);
  }

  // nextTick: the status flip and the result's v-if land in the same render pass.
  watch(
    [() => props.status, display],
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
