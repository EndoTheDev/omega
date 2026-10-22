<template>
  <div
    class="verdict-card"
    :class="{ settled: item.status === 'settled', stale: item.stale }">
    <div class="verdict-head">
      <span class="verdict-mark">?</span>
      <span class="verdict-title">{{ item.title }}</span>
      <span
        v-if="item.status === 'pending' && !item.stale && countdown"
        class="verdict-countdown">{{ countdown }}</span>
    </div>
    <p
      v-if="item.body"
      class="verdict-body">{{ item.body }}</p>

    <template v-if="item.status === 'pending' && !item.stale">
      <!-- v1.1 list layout: one row per option, label over its
           optional description. Single cards answer on click; multi
           cards toggle rows and confirm. -->
      <div
        v-if="item.options.length"
        class="verdict-options">
        <button
          v-for="(opt, i) in item.options"
          :key="opt.label"
          class="verdict-option-row"
          :class="{ selected: item.multi && selected.has(i + 1) }"
          :disabled="answering"
          :aria-pressed="item.multi ? selected.has(i + 1) : undefined"
          @click="item.multi ? toggle(i + 1) : pick(i + 1)">
          <span
            v-if="item.multi"
            class="verdict-check">{{ selected.has(i + 1) ? "[x]" : "[ ]" }}</span>
          <span class="verdict-row-text">
            <span class="verdict-label">{{ opt.label }}</span>
            <span
              v-if="opt.description"
              class="verdict-desc">{{ opt.description }}</span>
          </span>
        </button>
      </div>
      <button
        v-if="item.multi && item.options.length"
        class="verdict-confirm"
        :disabled="answering || !selected.size"
        @click="confirmMulti">confirm</button>
      <div
        v-if="item.allowText"
        class="verdict-free">
        <span class="verdict-free-label">type your own answer</span>
        <input
          v-model="draft"
          class="verdict-input"
          :disabled="answering"
          placeholder="your answer"
          @keyup.enter="sendFree" />
        <button
          class="verdict-free-send"
          :disabled="answering || !draft.trim()"
          @click="sendFree">answer</button>
      </div>
      <button
        class="verdict-skip"
        :disabled="answering"
        @click="skip">skip</button>
      <p
        v-if="error"
        class="verdict-error">{{ error }}</p>
    </template>

    <template v-else>
      <p class="verdict-outcome">{{ outcomeText }}</p>
    </template>
  </div>
</template>

<script setup lang="ts">
  // Verdict card (card t_2ecf9c51, v1.1 t_42d4dc09): the interactive
  // render of a verdict tool call. The card lives in the transcript
  // stream between the preamble bubble and the run's continuation;
  // answering posts to /sessions/{id}/verdicts (no card_id - this
  // session's oldest pending card, one run per session), the run's
  // tool_result frame then settles the card from the server's side.
  // The countdown is a CLIENT approximation (frame arrival +
  // timeout_s): the server owns the deadline.
  import { computed, onBeforeUnmount, onMounted, ref } from "vue";

  import type { VerdictItem } from "../../composables/omega";
  import { postVerdict } from "../../composables/omega";

  const props = defineProps<{
    item: VerdictItem;
    // the answer endpoint's session boundary - the chat tab's session
    sessionId?: string;
  }>();

  const draft = ref("");
  const answering = ref(false);
  const error = ref("");
  // multi-select picks (option numbers, 1-based)
  const selected = ref(new Set<number>());
  const now = ref(Date.now());
  let ticker: ReturnType<typeof setInterval> | null = null;

  onMounted(() => {
    ticker = setInterval(() => {
      now.value = Date.now();
    }, 1000);
  });
  onBeforeUnmount(() => {
    if (ticker) clearInterval(ticker);
  });

  // countdown: mm:ss until the client's approximation of the deadline.
  // Past zero renders "expiring" - the server may have fired the safe
  // answer already; the incoming tool_result renders the truth.
  const countdown = computed(() => {
    if (!props.item.issuedAt) return "";
    const timeoutS = props.item.timeoutS && props.item.timeoutS > 0 ? props.item.timeoutS : 1200;
    const left = props.item.issuedAt + timeoutS * 1000 - now.value;
    if (left <= 0) return "expiring…";
    const m = Math.floor(left / 60000);
    const s = Math.floor((left % 60000) / 1000);
    return `${m}:${String(s).padStart(2, "0")}`;
  });

  const outcomeText = computed(() => {
    const i = props.item;
    if (i.stale && i.status === "pending") return "no answer recorded (the run ended first)";
    switch (i.verdictKind) {
      case "answer":
        return i.picked
          ? `answered: ${i.picked}`
          : i.answerText
            ? `answered: ${i.answerText}`
            : "answered";
      case "skip":
        return "skipped";
      case "expired":
        return "no answer (card expired)";
      default:
        return "no answer recorded";
    }
  });

  async function send(body: Parameters<typeof postVerdict>[1]) {
    if (!props.sessionId) {
      error.value = "no session yet - send a message first";
      return;
    }
    answering.value = true;
    error.value = "";
    try {
      await postVerdict(props.sessionId, body);
      // the run's tool_result frame settles the card from the server;
      // no local flip (the frame is the truth, the race is server-side)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      answering.value = false;
    }
  }

  function pick(index: number) {
    void send({ option_index: index });
  }

  function toggle(index: number) {
    const next = new Set(selected.value);
    if (!next.delete(index)) next.add(index);
    selected.value = next;
  }

  function confirmMulti() {
    if (!selected.value.size) return;
    void send({ option_indexes: [...selected.value].sort((a, b) => a - b) });
  }

  function sendFree() {
    const text = draft.value.trim();
    if (!text) return;
    void send({ text });
  }

  function skip() {
    void send({ skip: true });
  }
</script>

<style scoped>
  .verdict-card {
    margin: 8px 0;
    padding: 10px 12px;
    background: var(--z1-raised, var(--z1, transparent));
    border: 1px solid var(--stroke, #444);
    border-radius: 8px;
    border-left: 3px solid var(--accent, #e8833a);
  }
  .verdict-card.settled,
  .verdict-card.stale {
    border-left-color: var(--stroke, #444);
    opacity: 0.85;
  }
  .verdict-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .verdict-mark {
    color: var(--accent, #e8833a);
    font-weight: 700;
  }
  .verdict-title {
    font-weight: 600;
  }
  .verdict-countdown {
    margin-left: auto;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
    font-size: 0.85em;
  }
  .verdict-body {
    margin: 6px 0 4px;
    color: var(--text-dim);
    white-space: pre-wrap;
  }
  /* v1.1 list layout: the rows stack top to bottom, one per row - no
     wrapping pill buttons. Each row carries its label and, when the
     option has one, its description underneath. */
  .verdict-options {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin: 8px 0 2px;
  }
  .verdict-option-row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    padding: 5px 10px;
    border: 1px solid var(--stroke, #444);
    border-radius: 6px;
    background: var(--z0);
    color: var(--text);
    cursor: pointer;
    text-align: left;
    font: inherit;
  }
  .verdict-option-row:hover:not(:disabled) {
    border-color: var(--accent, #e8833a);
  }
  .verdict-option-row:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .verdict-option-row.selected {
    border-color: var(--accent, #e8833a);
    background: var(--accent-dim, var(--z0));
  }
  .verdict-check {
    color: var(--accent, #e8833a);
    font-variant-numeric: tabular-nums;
  }
  .verdict-row-text {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .verdict-label {
    white-space: pre-wrap;
  }
  .verdict-desc {
    color: var(--text-dim);
    font-size: 0.85em;
    white-space: pre-wrap;
  }
  .verdict-confirm {
    margin-top: 4px;
    align-self: flex-start;
    padding: 4px 12px;
    border: 1px solid var(--accent, #e8833a);
    border-radius: 6px;
    background: var(--z0);
    color: var(--text);
    cursor: pointer;
    font: inherit;
  }
  .verdict-confirm:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .verdict-free {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
  }
  .verdict-free-label {
    color: var(--text-dim);
    font-size: 0.85em;
    white-space: nowrap;
  }
  .verdict-input {
    flex: 1;
    padding: 4px 8px;
    border: 1px solid var(--stroke, #444);
    border-radius: 6px;
    background: var(--z0);
    color: var(--text);
  }
  .verdict-free-send {
    padding: 4px 12px;
    border: 1px solid var(--stroke, #444);
    border-radius: 6px;
    background: var(--z0);
    color: var(--text);
    cursor: pointer;
    font: inherit;
  }
  .verdict-free-send:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .verdict-skip {
    margin-top: 6px;
    background: none;
    border: none;
    padding: 0;
    color: var(--text-dim);
    text-decoration: underline;
    cursor: pointer;
    font-size: 0.85em;
  }
  .verdict-error {
    margin: 6px 0 0;
    color: var(--error, #e5534b);
    font-size: 0.85em;
  }
  .verdict-outcome {
    margin: 6px 0 0;
    color: var(--text-dim);
    font-style: italic;
  }
</style>