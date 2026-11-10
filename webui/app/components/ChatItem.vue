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
    :attachments="item.attachments"
    :time="item.time"
    :item-id="item.id"
    :variant="item.role === 'assistant' ? variant : null"
    :final-ms="item.finalMs"
    :streaming="item.streaming"
    :retryable="retryable && item.role === 'assistant'"
    @select="(i) => item.id && variant && $emit('select', variant.message_id, i)"
    @fork="item.id && $emit('fork', item.id)"
    @retry="$emit('retry')" />
  <ThinkingBlock
    v-else-if="item.kind === 'thinking' && showThinking"
    :content="item.content"
    :final-ms="item.finalMs"
    :streaming="item.streaming"
    :variant="variant"
    @select="(i) => variant && $emit('select', variant.message_id, i)" />
  <ToolCallLine
    v-else-if="item.kind === 'tool'"
    :name="item.name"
    :status="item.status"
    :final-ms="item.finalMs"
    :answer-ms="item.answerMs"
    :output="item.output"
    :result="item.result"
    :live-text="item.liveText"
    :live-settled="item.liveSettled"
    :gate="item.gate"
    :variant="variant"
    @select="(i) => variant && $emit('select', variant.message_id, i)" />
  <!-- card t_63204dfd: a pending gate question bound to this running
       tool row - the three-option card rides the row it froze -->
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
    // this item's branch block (assistant messages with >=2 sibling
    // runs) - resolved by ChatTab through variantOf, null elsewhere
    variant?: VariantBlock | null;
    // the run is idle AND this item is the transcript's newest assistant
    // response - ChatMessage renders the retry chip inside the action row
    retryable?: boolean;
    // the chat tab's session id - verdict cards POST answers against it
    sessionId?: string;
    // card t_63204dfd: the pending gate question bound to THIS tool
    // row (ChatTab's binding) - the three-option card rides the row
    gateCard?: PendingVerdictCard | null;
  }>();
  const emit = defineEmits<{
    (e: "select", messageId: string, index: number): void;
    (e: "fork", messageId: string): void;
    (e: "retry"): void;
  }>();
</script>
