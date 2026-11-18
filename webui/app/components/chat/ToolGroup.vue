<template>
  <!--
    consecutive tool call rows collapse into ONE group block.
    Inside, every child row renders EXACTLY what ChatItem's tool branch renders - a
    group is a container, never a re-render of the rows.
  -->
  <details
    ref="el"
    class="toolgroup">
    <summary @click="onSummaryClick">
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
      <!-- children key on the CALL's identity (toolChildKey), never the slot. -->
      <template
        v-for="(item, i) in items"
        :key="toolChildKey(item, i)">
        <ToolCallLine
          :name="item.name"
          :status="item.status"
          :final-ms="item.finalMs"
          :elapsed-ms="item.elapsedMs"
          :answer-ms="item.answerMs"
          :output="item.output"
          :result="item.result"
          :gate="item.gate" />
        <!-- a pending gate question bound to THIS running tool row. -->
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
  import { toolChildKey, toolGroupWantOpen } from "../../composables/toolGroup";
  import { useToolDisplay } from "../../composables/toolDisplay";
  import { isLive } from "../../composables/toolStatus";

  const props = defineProps<{
    items: ToolItem[];
    gateCards: (PendingVerdictCard | null)[];
    sessionId?: string;
    live?: boolean;
  }>();

  const anyError = computed(() => props.items.some((it) => it.status === "error"));
  // anyActive: any child in the LIVE set, asked through the one shared predicate.
  const anyActive = computed(() => props.items.some((it) => isLive(it.status)));
  const state = computed(() =>
    anyError.value ? "error" : anyActive.value ? "" : "done",
  );

  // auto open/close (the row/block state machine,
  //     shape): the shared mode decides what the group does on its own;
  //     a manual summary click PINS the group for the session and mode
  //     flips + status transitions recompute around it, never over it.
  const el = ref<HTMLDetailsElement | null>(null);
  const display = useToolDisplay();
  const pinned = ref<boolean | null>(null);

  function applyAuto() {
    if (pinned.value !== null || !el.value) return;
    el.value.open = toolGroupWantOpen(anyActive.value, props.live ?? false, display.value);
  }

  // nextTick: child statuses, the live gate and the mode flip land in render passes.
  watch(
    [anyActive, () => props.live, display],
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
