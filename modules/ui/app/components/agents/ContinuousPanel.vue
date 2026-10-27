<template>
  <!-- card t_72ecca67 (ruling 18): the Continuous roster - agents with
       last-report preview, next wake, unread state; click = detail
       strip (canonical chat tail + evidence rows + wake/sleep); the
       header form is the create flow: name, directive, schedule ->
       POST /continuous (the home folder + the canonical chat). Same
       inspector family as AgentsPanel; fetches its own payloads. -->
  <section class="pane continuous">
    <header class="continuous-head">
      <h2 class="continuous-title">continuous</h2>
      <button
        class="continuous-new"
        title="create a continuous agent (home + canonical chat + schedule)"
        @click="showCreate = !showCreate">+</button>
    </header>

    <form
      v-if="showCreate"
      class="continuous-create"
      @submit.prevent="createAgent">
      <input
        v-model="createName"
        class="continuous-create-name"
        placeholder="agent name (lowercase, hyphens ok)"
        aria-label="agent name" />
      <input
        v-model="createDirective"
        class="continuous-create-directive"
        placeholder="directive (standing intent)"
        aria-label="directive" />
      <input
        v-model="createSchedule"
        class="continuous-create-schedule"
        placeholder="wake schedule (cron, e.g. 17 * * * *)"
        aria-label="wake schedule" />
      <button
        class="continuous-create-go"
        :disabled="!createName || creating"
        type="submit">create</button>
    </form>
    <p
      v-if="createError"
      class="settings-error continuous-error">{{ createError }}</p>

    <p
      v-if="loadError"
      class="settings-error continuous-error">{{ loadError }}</p>
    <p
      v-else-if="loading"
      class="continuous-status">loading…</p>
    <ol
      v-else
      class="continuous-list"
      role="list">
      <li
        v-for="a in roster"
        :key="a.name"
        :class="['continuous-row', { active: a.name === selected, unread: a.unread }]"
        @click="select(a)">
        <span class="continuous-row-name">{{ a.name }}</span>
        <span class="continuous-row-status">{{ rowStatus(a) }}</span>
        <span class="continuous-row-preview">{{ previewLine(a) }}</span>
      </li>
      <li
        v-if="roster.length === 0"
        class="continuous-status">no continuous agents yet - use +</li>
    </ol>

    <div
      v-if="detail"
      class="continuous-detail">
      <h3 class="continuous-detail-title">{{ detail.name }}</h3>
      <p class="continuous-detail-meta">{{ detail.description || "-" }}</p>
      <div class="continuous-actions">
        <button
          class="continuous-wake"
          title="wake this agent now (one budgeted run)"
          :disabled="waking"
          @click="wake(detail.name, wakeMessage)">wake</button>
        <button
          class="continuous-sleep"
          title="sleep this agent (ends a running wake)"
          @click="sleep(detail.name)">sleep</button>
      </div>
      <input
        v-model="wakeMessage"
        class="continuous-message"
        placeholder="optional wake message (goes into the canonical chat)"
        aria-label="wake message" />
      <p
        v-if="wakeMsg"
        class="continuous-status">{{ wakeMsg }}</p>

      <h4 class="continuous-sub">canonical chat</h4>
      <ol
        v-if="chat.length"
        class="continuous-chat"
        role="list">
        <li
          v-for="(m, i) in chat"
          :key="i"
          class="continuous-chat-row">{{ m.content }}</li>
      </ol>
      <p
        v-else
        class="continuous-status">no messages yet</p>

      <h4 class="continuous-sub">evidence</h4>
      <p class="continuous-evidence-sum">{{ evidenceSummary(evidence) }}</p>
      <ol
        v-if="evidence.length"
        class="continuous-evidence"
        role="list">
        <li
          v-for="e in evidence"
          :key="e.id"
          class="continuous-evidence-row">
          <span
            :class="['continuous-evidence-ok', e.ok ? 'ok' : 'failed']">{{ e.ok ? "ok" : "failed" }}</span>
          <span class="continuous-evidence-tool">{{ e.tool }}</span>
          <span class="continuous-evidence-verifier">{{ verifierVoice(e) }}</span>
          <span class="continuous-evidence-hash">{{ e.args_hash.slice(0, 8) }}</span>
        </li>
      </ol>
      <p
        v-else
        class="continuous-status">no tool calls recorded</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  evidenceSummary,
  previewLine,
  rowStatus,
  verifierVoice,
  type OmegaChatMessage,
  type OmegaContinuousRow,
  type OmegaEvidenceRow,
} from "./continuousRows.ts";

const roster = ref<OmegaContinuousRow[]>([]);
const detail = ref<OmegaContinuousRow | null>(null);
const selected = ref("");
const chat = ref<OmegaChatMessage[]>([]);
const evidence = ref<OmegaEvidenceRow[]>([]);
const loading = ref(false);
const loadError = ref("");
const showCreate = ref(false);
const creating = ref(false);
const createName = ref("");
const createDirective = ref("");
const createSchedule = ref("");
const createError = ref("");
const waking = ref(false);
const wakeMessage = ref("");
const wakeMsg = ref("");

async function load(): Promise<void> {
  loading.value = true;
  loadError.value = "";
  try {
    const res = await fetch("/continuous/roster");
    if (!res.ok) throw new Error(`GET /continuous/roster: ${res.status}`);
    roster.value = (await res.json()) as OmegaContinuousRow[];
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

// select opens the detail strip: the canonical chat view (ruling 5)
// and the evidence rows (ruling 11). The chat fetch marks the session
// read server-side - the roster's unread flag clears on the reload.
async function select(a: OmegaContinuousRow): Promise<void> {
  selected.value = a.name;
  detail.value = a;
  chat.value = [];
  evidence.value = [];
  wakeMsg.value = "";
  try {
    const res = await fetch(`/continuous/${encodeURIComponent(a.name)}/chat`);
    if (!res.ok) throw new Error(`GET chat: ${res.status}`);
    const view = (await res.json()) as { messages?: OmegaChatMessage[] };
    chat.value = view.messages ?? [];
  } catch {
    chat.value = []; // the detail stays; the chat is a read
  }
  try {
    const res = await fetch(`/continuous/${encodeURIComponent(a.name)}/evidence`);
    if (!res.ok) throw new Error(`GET evidence: ${res.status}`);
    evidence.value = ((await res.json()) as OmegaEvidenceRow[]) ?? [];
  } catch {
    evidence.value = [];
  }
}

// createAgent is ruling 18's create flow: name + directive + schedule
// -> the home folder + canonical chat. A conflict errors loud.
async function createAgent(): Promise<void> {
  creating.value = true;
  createError.value = "";
  try {
    const res = await fetch("/continuous", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        name: createName.value.trim(),
        directive: createDirective.value,
        schedule: createSchedule.value,
      }),
    });
    if (!res.ok) throw new Error(await res.text());
    createName.value = "";
    createDirective.value = "";
    createSchedule.value = "";
    showCreate.value = false;
    await load();
  } catch (e) {
    createError.value = e instanceof Error ? e.message : String(e);
  } finally {
    creating.value = false;
  }
}

// wake is ruling 8's human trigger: the typed message (or the standing
// directive) wakes the agent; the report lands in the canonical chat.
async function wake(name: string, message: string): Promise<void> {
  waking.value = true;
  wakeMsg.value = "";
  try {
    const res = await fetch(`/continuous/${encodeURIComponent(name)}/chat`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ message }),
    });
    if (!res.ok) throw new Error(await res.text());
    wakeMsg.value = "wake sent - the report lands in the canonical chat";
    wakeMessage.value = "";
    await load();
  } catch (e) {
    wakeMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    waking.value = false;
  }
}

// sleep ends a running wake (ruling 7: asleep is the default state).
async function sleep(name: string): Promise<void> {
  try {
    await fetch(`/continuous/${encodeURIComponent(name)}/sleep`, { method: "POST" });
    wakeMsg.value = "sleep sent";
  } catch {
    /* the roster refresh is the truth */
  }
  await load();
}

onMounted(load);

defineExpose({ load, select, roster, detail, chat, evidence });
</script>

<style scoped>
.pane.continuous {
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow-y: auto;
}
.continuous-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.continuous-title {
  font-size: 12px;
  text-transform: lowercase;
  letter-spacing: 0.4px;
  color: var(--text-dim);
  margin: 0;
}
.continuous-new,
.continuous-create-go {
  background: var(--z2);
  border: 1px solid var(--z3);
  border-radius: 6px;
  color: var(--text);
  cursor: pointer;
  font: inherit;
  padding: 4px 10px;
}
.continuous-create {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.continuous-create input {
  background: var(--z2);
  border: 1px solid var(--z3);
  border-radius: 6px;
  color: var(--text);
  font: inherit;
  padding: 6px 8px;
  width: 100%;
}
.continuous-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.continuous-row {
  border: 1px solid var(--z2);
  border-radius: 8px;
  cursor: pointer;
  display: grid;
  gap: 2px;
  grid-template-columns: 1fr auto;
  padding: 6px 8px;
}
.continuous-row:hover {
  background: var(--z2);
}
.continuous-row.active {
  border-color: var(--z3);
}
.continuous-row.unread .continuous-row-status {
  color: var(--text);
  font-weight: 600;
}
.continuous-row-name {
  font-weight: 600;
}
.continuous-row-status {
  color: var(--text-dim);
}
.continuous-row-preview {
  color: var(--text-dim);
  font-size: 11px;
  grid-column: 1 / -1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.continuous-detail {
  border-top: 1px solid var(--z2);
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding-top: 8px;
}
.continuous-detail-title {
  font-size: 13px;
  margin: 0;
}
.continuous-detail-meta {
  color: var(--text-dim);
  font-size: 11px;
  margin: 0;
}
.continuous-actions {
  display: flex;
  gap: 6px;
}
.continuous-actions button {
  background: var(--z2);
  border: 1px solid var(--z3);
  border-radius: 6px;
  color: var(--text);
  cursor: pointer;
  font: inherit;
  padding: 4px 10px;
}
.continuous-message {
  background: var(--z2);
  border: 1px solid var(--z3);
  border-radius: 6px;
  color: var(--text);
  font: inherit;
  padding: 6px 8px;
}
.continuous-sub {
  color: var(--text-dim);
  font-size: 11px;
  margin: 6px 0 0;
  text-transform: lowercase;
}
.continuous-chat,
.continuous-evidence {
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin: 0;
  max-height: 220px;
  overflow-y: auto;
  padding: 0;
}
.continuous-chat-row {
  border-bottom: 1px solid var(--z2);
  font-size: 11px;
  padding: 3px 0;
  white-space: pre-wrap;
}
.continuous-evidence-row {
  display: flex;
  gap: 6px;
  font-size: 11px;
}
.continuous-evidence-ok.ok {
  color: var(--ok, #3f9);
}
.continuous-evidence-ok.failed {
  color: var(--err, #f66);
}
.continuous-evidence-hash {
  color: var(--text-dim);
  font-family: monospace;
}
.continuous-status {
  color: var(--text-dim);
  font-size: 11px;
}
.continuous-evidence-sum {
  color: var(--text-dim);
  font-size: 11px;
  margin: 0;
}
.continuous-error {
  font-size: 11px;
}
</style>