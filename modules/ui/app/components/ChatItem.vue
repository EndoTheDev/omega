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
    :item-id="item.id"
    :variant="item.role === 'assistant' ? variant : null"
    :final-ms="item.finalMs"
    :streaming="item.streaming"
    @select="(i) => item.id && variant && $emit('select', variant.message_id, i)"
    @fork="item.id && $emit('fork', item.id)" />
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
    :interrupted="item.interrupted"
    :final-ms="item.finalMs"
    :output="item.output"
    :result="item.result"
    :variant="variant"
    @select="(i) => variant && $emit('select', variant.message_id, i)" />
  <VerdictCard
    v-else-if="item.kind === 'verdict'"
    :item="item"
    :session-id="sessionId" />
</template>

<script setup lang="ts">
  import type { TranscriptItem, VariantBlock } from "../composables/omega";

  defineProps<{
    item: TranscriptItem;
    showThinking: boolean;
    // this item's branch block (assistant messages with >=2 sibling
    // runs) - resolved by ChatTab through variantOf, null elsewhere
    variant?: VariantBlock | null;
    // the chat tab's session id - verdict cards POST answers against it
    sessionId?: string;
  }>();
  const emit = defineEmits<{
    (e: "select", messageId: string, index: number): void;
    (e: "fork", messageId: string): void;
  }>();
</script>
