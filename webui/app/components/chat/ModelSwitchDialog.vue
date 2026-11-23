<template>
  <Teleport to="body">
    <!--
      THE model-switch decision: blocking on purpose. A banner would let a stray click
      dismiss it and the switch would truncate silently - exactly what the dialog exists
      to prevent. No stored preference can suppress it: the condition decides every time.
    -->
    <div class="confirm-scrim" @click.self="$emit('cancel')">
      <div
        ref="dialogEl"
        class="confirm-dialog model-switch-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="model-switch-title"
        aria-describedby="model-switch-body"
        tabindex="-1"
        @keydown.esc.stop.prevent="$emit('cancel')">
        <p
          id="model-switch-title"
          class="confirm-title">Switch to a smaller context?</p>
        <p
          id="model-switch-body"
          class="confirm-body">
          <span class="ms-model">{{ model }}</span> holds
          <strong :title="`${tokens} tokens`">{{ short(tokens) }}</strong> tokens of
          live context, but its window is
          <strong :title="`${window} tokens`">{{ short(window) }}</strong>. Switching
          without compacting drops
          <strong
            class="ms-loss"
            :title="`${loss} tokens`">~{{ short(loss) }}</strong>
          tokens.
        </p>
        <p
          v-if="error"
          class="ms-error"
          role="alert">{{ error }}</p>
        <div class="confirm-actions">
          <button
            type="button"
            class="new-btn"
            :disabled="busy"
            @click="$emit('cancel')">
            cancel
          </button>
          <button
            type="button"
            class="ms-anyway"
            :disabled="busy"
            @click="$emit('anyway')">
            switch anyway
          </button>
          <button
            type="button"
            class="ms-compact"
            :disabled="busy"
            @click="$emit('compact')">
            {{ busy ? "compacting..." : "compact and switch" }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
  import { onMounted, ref } from "vue";
  import { formatTokens } from "../../composables/omega";

  defineProps<{
    /** the picked slot ("provider:model"), the dialog's subject */
    model: string;
    tokens: number;
    window: number;
    loss: number;
    /** a compact is in flight: every outcome waits for it */
    busy?: boolean;
    error?: string;
  }>();

  defineEmits<{
    (e: "compact"): void;
    (e: "anyway"): void;
    (e: "cancel"): void;
  }>();

  const dialogEl = ref<HTMLElement | null>(null);
  const short = (n: number) => formatTokens(Math.max(0, Math.round(n)));

  // the dialog takes focus on open so Escape and Tab land inside it; the primary action
  // is deliberately NOT pre-focused - Enter never compacts a conversation by accident.
  onMounted(() => dialogEl.value?.focus());
</script>

<style scoped>
  .model-switch-dialog {
    max-width: 420px;
  }
  .ms-model {
    font-family: var(--mono);
    color: var(--text);
  }
  .ms-loss {
    color: var(--err);
  }
  .ms-error {
    margin: 0 0 12px;
    font-size: 12px;
    color: var(--err);
  }
  .ms-anyway,
  .ms-compact {
    border-radius: var(--r-chip);
    padding: 5px 12px;
    font-size: 12px;
    cursor: pointer;
  }
  .ms-anyway {
    background: transparent;
    border: 1px solid var(--stroke);
    color: var(--text-dim);
  }
  .ms-anyway:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--accent);
  }
  .ms-compact {
    background: var(--accent);
    border: none;
    color: #181818;
    font-weight: 700;
  }
  .ms-compact:hover:not(:disabled) {
    filter: brightness(1.1);
  }
  .ms-anyway:disabled,
  .ms-compact:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
