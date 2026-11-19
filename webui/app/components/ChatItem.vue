<template>
  <!--
    fragment (no wrapper element): renders EXACTLY the DOM the flat transcript v-for.
  -->
  <ChatMessage
    v-if="item.kind === 'message'"
    :role="item.role"
    :content="item.content"
    :images="item.images"
    :attachments="item.attachments"
    :time="item.time"
    :item-id="item.id"
    :variant="item.role === 'assistant' ? variant : null"
    :final-ms="item.finalMs"
    :turn-ms="item.turnMs"
    :elapsed-ms="item.elapsedMs"
    :streaming="item.streaming"
    :retryable="retryable && item.role === 'assistant'"
    @select="(i) => item.id && variant && $emit('select', variant.message_id, i)"
    @fork="item.id && $emit('fork', item.id)"
    @retry="$emit('retry')" />
  <ThinkingBlock
    v-else-if="item.kind === 'thinking' && showThinking"
    :content="item.content"
    :final-ms="item.finalMs"
    :elapsed-ms="item.elapsedMs"
    :streaming="item.streaming"
    :variant="variant"
    @select="(i) => variant && $emit('select', variant.message_id, i)" />
  <ToolCallLine
    v-else-if="item.kind === 'tool'"
    :name="item.name"
    :status="item.status"
    :final-ms="item.finalMs"
    :elapsed-ms="item.elapsedMs"
    :answer-ms="item.answerMs"
    :output="item.output"
    :result="item.result"
    :call-id="item.callId"
    :fold-key="foldKey"
    :gate="item.gate"
    :variant="variant"
    @select="(i) => variant && $emit('select', variant.message_id, i)" />
  <GateCard
    v-if="item.kind === 'tool' && gateCard"
    :card="gateCard"
    :session-id="sessionId" />
  <VerdictCard
    v-else-if="item.kind === 'verdict'"
    :item="item"
    :session-id="sessionId" />
</template>

<script setup lang="ts">
  import type { PendingVerdictCard, TranscriptItem, VariantBlock } from "../composables/omega";
  import GateCard from "./chat/GateCard.vue";

  defineProps<{
    item: TranscriptItem;
    showThinking: boolean;
    variant?: VariantBlock | null;
    retryable?: boolean;
    sessionId?: string;
    gateCard?: PendingVerdictCard | null;
    // card t_15572849: this row's durable fold address (toolGroup.ts
    // foldAddress) - the tool line keys its manual open/close pin by it on
    // the tab's view record, so the pin survives the pane's unmount
    foldKey?: string;
  }>();
  const emit = defineEmits<{
    (e: "select", messageId: string, index: number): void;
    (e: "fork", messageId: string): void;
    (e: "retry"): void;
  }>();
</script>
