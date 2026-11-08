<template>
  <!-- card t_e4280de7: consecutive tool call rows collapse into ONE
       group block. The details element stays uncontrolled (no :open
       binding) like the row/block folds: the browser owns the toggle,
       this side applies the mode on mount and on state/mode change
       only. Inside, every child row renders EXACTLY what ChatItem's
       tool branch renders (ToolCallLine + its gate card) - a group is a
       container, never a re-render of the rows. -->
  <details
    ref="el"
    class="toolgroup">
    <summary @click="onSummaryClick">
      <!-- the group's wrench carries the run's aggregate status:
           running = accent (the default), all settled = ok, any child
           errored = err - one glance says what the run did -->
      <Icon
        name="tools"
        class="tool-ic"
        :class="state" />
      <span class="toolgroup-label">{{ items.length }} tool calls</span>
      <span
        v-if="live"
        class="toolgroup-live"
        >…</span
      >
    </summary>
    <div class="toolgroup-body">
      <template
        v-for="(item, i) in items"
        :key="i">
        <ToolCallLine
          :name="item.name"
          :status="item.status"
          :final-ms="item.finalMs"
          :answer-ms="item.answerMs"
          :output="item.output"
          :result="item.result"
          :gate="item.gate" />
        <!-- card t_63204dfd: a pending gate question bound to THIS
             running tool row - while the run awaits the answer the
             group is open (auto), so the card stays reachable -->
        <GateCard
          v-if="gateCards[i]"
          :card="gateCards[i]!"
          :session-id="sessionId" />
      </template>
    </div>
  </details>
</template>

<script setup lang="ts">
  import { computed, nextTick, onMounted, ref, watch } from "vue";

  import GateCard from "./GateCard.vue";
  import Icon from "../ui/Icon.vue";
  import ToolCallLine from "./ToolCallLine.vue";
  import type { PendingVerdictCard, ToolItem } from "../../composables/omega";
  import { toolGroupWantOpen } from "../../composables/toolGroup";
  import { useToolDisplay } from "../../composables/toolDisplay";

  const props = defineProps<{
    // the grouped consecutive tool rows (groupToolRuns) - the result
    // merges onto the call item, so one row covers one call+result pair
    items: ToolItem[];
    // per-child gate bindings, index-aligned with items (ChatTab
    // resolves them the same way it feeds ChatItem) - null = none
    gateCards: (PendingVerdictCard | null)[];
    // the chat tab's session id - gate cards POST answers against it
    sessionId?: string;
    // the session's run is live: auto keeps the group open across the
    // whole run (a per-call close/open yoyo as calls stream in is
    // noise, not feedback) - the busy gate rides the transcript
    live?: boolean;
  }>();

  // --- aggregate state: one wrench color for the whole run ---
  const anyError = computed(() => props.items.some((it) => it.status === "error"));
  const anyRunning = computed(() => props.items.some((it) => it.status === "running"));
  const state = computed(() =>
    anyError.value ? "error" : anyRunning.value ? "" : "done",
  );

  // --- auto open/close (the row/block state machine, card t_868633b3
  //     shape): the shared mode decides what the group does on its own;
  //     a manual summary click PINS the group for the session and mode
  //     flips + status transitions recompute around it, never over it.
  const el = ref<HTMLDetailsElement | null>(null);
  const display = useToolDisplay();
  const pinned = ref<boolean | null>(null);

  function applyAuto() {
    if (pinned.value !== null || !el.value) return;
    el.value.open = toolGroupWantOpen(anyRunning.value, props.live ?? false, display.value);
  }

  // nextTick: child statuses, the live gate and the mode flip land in
  // render passes - apply after the DOM settles.
  watch(
    [anyRunning, () => props.live, display],
    () => void nextTick(applyAuto),
  );
  onMounted(applyAuto);

  // Click fires BEFORE the browser's toggle default action, so !open is
  // the state the click is about to produce - pin that, and auto never
  // touches this group again this session.
  function onSummaryClick() {
    if (!el.value) return;
    pinned.value = !el.value.open;
  }
</script>
