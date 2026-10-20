<template>
  <!-- fragment (no wrapper element): renders EXACTLY the DOM the flat
       transcript v-for rendered inline, so the classic chronolog stays
       pixel-unchanged - the container layouts own the wrappers, this
       component owns the per-item markup for both appearances -->
  <ChatMessage
    v-if="item.kind === 'message'"
    :role="item.role"
    :content="item.content"
    :images="item.images"
    :time="item.time"
    :final-ms="item.finalMs"
    :streaming="item.streaming" />
  <ThinkingBlock
    v-else-if="item.kind === 'thinking' && showThinking"
    :content="item.content"
    :final-ms="item.finalMs"
    :streaming="item.streaming" />
  <ToolCallLine
    v-else-if="item.kind === 'tool'"
    :name="item.name"
    :status="item.status"
    :interrupted="item.interrupted"
    :final-ms="item.finalMs"
    :output="item.output"
    :result="item.result" />
  <div
    v-else-if="item.kind === 'interrupted'"
    class="interrupted-line"
    data-lane-proof="A">
    <span>{{ item.label ?? "stream interrupted" }}</span>
    <button
      class="resume"
      @click="emit('retry')">
      retry
    </button>
  </div>
</template>

<script setup lang="ts">
  import type { TranscriptItem } from "../composables/chat";

  defineProps<{
    item: TranscriptItem;
    showThinking: boolean;
  }>();
  const emit = defineEmits<{ (e: "retry"): void }>();
</script>