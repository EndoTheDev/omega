<template>
  <!-- card t_63204dfd: the chat surface of a pending gate question. The
       escalated shell.run froze the run; this card rides its tool call
       row with the three options (approve runs it, deny skips it,
       follow-up prompt swaps to a text input whose typed guidance
       resolves the question WITHOUT running the command). The card
       answers the SAME pending card id through the SAME verdict
       endpoint the notification card and the CLI loop use - first
       resolution wins, every other surface collapses. When the
       tool_result lands (the gate's stamp line on the call row), the
       binding that owns this card is gone and it unmounts - the stamp
       is the permanent record. -->
  <div
    class="verdict-card gate-card"
    :class="{ settled: resolved }">
    <div class="verdict-head">
      <span class="verdict-mark">?</span>
      <span class="verdict-title">{{ card.title }}</span>
    </div>
    <p
      v-if="card.body"
      class="verdict-body">{{ card.body }}</p>

    <template v-if="resolved">
      <!-- the resolution (this client or another surface): collapse.
           The permanent stamp arrives on the call's tool_result. -->
      <p class="verdict-outcome">{{ resolvedText }}</p>
    </template>
    <template v-else-if="mode === 'followup'">
      <!-- the follow-up input swaps the three rows: typed text resolves
           the question (deny + guidance); a blank send is the plain
           deny -->
      <div class="verdict-free">
        <input
          v-model="draft"
          class="verdict-input"
          :disabled="answering"
          placeholder="what should happen instead"
          @keyup.enter="sendFollowup" />
        <button
          class="verdict-free-send"
          :disabled="answering"
          @click="sendFollowup">send</button>
      </div>
      <p
        v-if="error"
        class="verdict-error">{{ error }}</p>
    </template>
    <template v-else>
      <div class="verdict-options">
        <button
          class="verdict-option-row"
          :disabled="answering"
          @click="act('approve')">
          <span class="verdict-label">approve</span>
          <span class="verdict-desc">run the command</span>
        </button>
        <button
          class="verdict-option-row"
          :disabled="answering"
          @click="act('deny')">
          <span class="verdict-label">deny</span>
          <span class="verdict-desc">skip the command</span>
        </button>
        <button
          class="verdict-option-row"
          :disabled="answering"
          @click="mode = 'followup'">
          <span class="verdict-label">follow-up prompt</span>
          <span class="verdict-desc">deny, then tell the agent what to do instead</span>
        </button>
      </div>
      <p
        v-if="error"
        class="verdict-error">{{ error }}</p>
    </template>
  </div>
</template>

<script setup lang="ts">
  import { computed, ref } from "vue";

  import type { PendingVerdictCard } from "../../composables/omega";
  import { gateAnswerBody, isSettledRace, postVerdict } from "../../composables/omega";

  const props = defineProps<{
    // the pending card (the /verdicts listing row) - the answer posts
    // by its id, one click among any surfaces
    card: PendingVerdictCard;
    // the answer endpoint's session boundary
    sessionId?: string;
  }>();

  const draft = ref("");
  const answering = ref(false);
  const error = ref("");
  const mode = ref<"choose" | "followup">("choose");
  // the resolution holder: set by this client's own POST, or by a lost
  // race (the 409's settled vocabulary) - both collapse, no error
  const resolved = ref<"" | "approve" | "deny" | "race">("");
  const resolvedText = computed(() => {
    switch (resolved.value) {
      case "approve": return "resolved: approved";
      case "deny": return "resolved: denied";
      default: return "resolved (another surface answered)";
    }
  });

  async function act(kind: "approve" | "deny" | "followup", text = "") {
    if (!props.sessionId) {
      error.value = "no session yet - send a message first";
      return;
    }
    answering.value = true;
    error.value = "";
    try {
      await postVerdict(props.sessionId, gateAnswerBody(props.card, kind, text));
      resolved.value = kind === "approve" ? "approve" : "deny";
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (isSettledRace(msg)) {
        resolved.value = "race"; // first resolution won somewhere else
      } else {
        error.value = msg;
      }
    } finally {
      answering.value = false;
    }
  }

  // the follow-up send: empty input IS the plain deny (ruling 1)
  function sendFollowup() {
    const text = draft.value;
    if (!text.trim()) {
      mode.value = "choose";
      void act("deny");
      return;
    }
    void act("followup", text);
  }
</script>

<style scoped>
  /* the VerdictCard.vue vocabulary, rehomed at the tool call row */
  .gate-card {
    margin: 8px 0;
    padding: 10px 12px;
    background: var(--z1-raised, var(--z1, transparent));
    border: 1px solid var(--stroke, #444);
    border-radius: 8px;
    border-left: 3px solid var(--accent, #e8833a);
  }
  .gate-card.settled {
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
    overflow-wrap: anywhere;
  }
  .verdict-body {
    margin: 6px 0 4px;
    color: var(--text-dim);
    white-space: pre-wrap;
  }
  .verdict-options {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
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
  .verdict-desc {
    color: var(--text-dim);
    font-size: 0.85em;
    white-space: pre-wrap;
  }
  .verdict-free {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 4px;
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