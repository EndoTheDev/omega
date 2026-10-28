<template>
  <!-- card t_7e2b7002: the Agents panel, same family as the inspector
       views (cron, skills, tools). Agent rows (name, description,
       status-by-counts); click = detail (config summary, skills, last
       activity); primary action = "new session as this agent";
       creation v1 = scaffold + config.yaml edit in the file tree, no
       config form. Fetches its own payloads (omega.ts is owned by
       card t_14f73742 while it runs - the wiring pass folds these
       calls into the composable later). -->
  <section class="pane agents">
    <header class="agents-head">
      <h2 class="agents-title">agents</h2>
      <button
        class="agents-new"
        title="scaffold a new agent directory"
        @click="showCreate = !showCreate">@</button>
    </header>

    <form
      v-if="showCreate"
      class="agents-create"
      @submit.prevent="createAgent">
      <input
        v-model="createName"
        class="agents-create-name"
        placeholder="agent name (lowercase, hyphens ok)"
        aria-label="agent name" />
      <input
        v-model="createDescription"
        class="agents-create-desc"
        placeholder="one-line description (optional)"
        aria-label="agent description" />
      <button
        class="agents-create-go"
        :disabled="!createName || creating"
        type="submit">scaffold</button>
    </form>
    <p
      v-if="createError"
      class="settings-error agents-error">{{ createError }}</p>

    <p
      v-if="loadError"
      class="settings-error agents-error">{{ loadError }}</p>
    <p
      v-else-if="loading"
      class="agents-status">loading…</p>
    <ol
      v-else
      class="agents-list"
      role="list">
      <li
        v-for="a in profiles"
        :key="a.name"
        :class="['agents-row', { active: a.name === selected }]"
        @click="select(a)">
        <span class="agents-row-name">{{ a.name }}</span>
        <span class="agents-row-desc">{{ agentDescriptionText(a) }}</span>
        <span class="agents-row-meta">{{ agentModelText(a) }}</span>
        <span class="agents-row-meta">{{ a.skill_count }} skills · {{ agentSessionText(a) }}</span>
      </li>
    </ol>

    <div
      v-if="detail"
      class="agents-detail">
      <h3 class="agents-detail-title">{{ detail.name }}</h3>
      <dl class="agents-config">
        <dt>model</dt><dd>{{ agentModelText(detail) || "-" }}</dd>
        <dt>provider</dt><dd>{{ detail.provider_type || "-" }}</dd>
        <dt>dir</dt><dd>{{ detail.dir }}</dd>
        <dt>skills</dt><dd>{{ agentSkillsText(detail) || "none" }}</dd>
        <dt>memory</dt><dd>{{ agentMemoryText(detail) }}</dd>
        <dt>last activity</dt><dd>{{ detail.last_activity || "-" }}</dd>
      </dl>
      <button
        class="agents-session-new"
        title="create a session bound to this agent"
        :disabled="startingSession"
        @click="newSessionAs(detail.name)">new session as this agent</button>
      <p
        v-if="sessionStartMsg"
        class="agents-status">{{ sessionStartMsg }}</p>
      <ol
        v-if="detailSessions.length"
        class="agents-sessions"
        role="list">
        <li
          v-for="s in detailSessions"
          :key="s.id"
          class="agents-session-row"
          :title="`open ${agentSessionTitle(s)} in the main pane`"
          @click="openAgentSession(s.id)">
          <span class="agents-session-title">{{ agentSessionTitle(s) }}</span>
          <span class="agents-session-meta">{{ s.id }}</span>
        </li>
      </ol>
      <p
        v-else
        class="agents-status">no sessions as this agent</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  agentDescriptionText,
  agentMemoryText,
  agentModelText,
  agentSessionTitle,
  agentSkillsText,
} from "./agentsRows.ts";
import type { OmegaAgent, OmegaAgentSession } from "./agentsRows.ts";

// emit("open-session", id) is the Hermes-bots parity handoff (card
// t_1ba868bb): app.vue routes the id through openChatTab, the same
// opener a sidebar session row uses - the main-pane chat surface, no
// degraded mode. Sends on the open session ride /chat; the row's
// agent binding never switches (server-enforced at creation).
const emit = defineEmits<{ (e: "open-session", id: string): void }>();

/* agentSessionText: the list-row status voice. */
function agentSessionText(a: OmegaAgent): string {
  return `${a.session_count} sessions`;
}

const profiles = ref<OmegaAgent[]>([]);
const detail = ref<OmegaAgent | null>(null);
const detailSessions = ref<OmegaAgentSession[]>([]);
const selected = ref("");
const loading = ref(false);
const loadError = ref("");
const showCreate = ref(false);
const creating = ref(false);
const createName = ref("");
const createDescription = ref("");
const createError = ref("");
const startingSession = ref(false);
const sessionStartMsg = ref("");

async function fetchJSON(url: string, init?: RequestInit): Promise<Response> {
  return fetch(url, init);
}

async function load(): Promise<void> {
  loading.value = true;
  loadError.value = "";
  try {
    const res = await fetchJSON("/agents");
    if (!res.ok) throw new Error(`GET /agents: ${res.status}`);
    profiles.value = ((await res.json()) ?? []) as OmegaAgent[];
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
}

async function select(a: OmegaAgent): Promise<void> {
  selected.value = a.name;
  detail.value = a;
  detailSessions.value = [];
  sessionStartMsg.value = "";
  try {
    const res = await fetchJSON(`/agents/${encodeURIComponent(a.name)}/sessions`);
    if (res.ok) detailSessions.value = ((await res.json()) ?? []) as OmegaAgentSession[];
  } catch {
    detailSessions.value = []; // the detail stays; the session list is a read
  }
}

async function createAgent(): Promise<void> {
  creating.value = true;
  createError.value = "";
  try {
    const res = await fetchJSON("/agents", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name: createName.value.trim(), description: createDescription.value }),
    });
    if (!res.ok) throw new Error(await res.text());
    createName.value = "";
    createDescription.value = "";
    showCreate.value = false;
    await load();
  } catch (e) {
    createError.value = e instanceof Error ? e.message : String(e);
  } finally {
    creating.value = false;
  }
}

// openAgentSession: a session row opens exactly the way a sessions-list
// row does (card t_1ba868bb) - one emit, no panel-local transcript.
function openAgentSession(id: string): void {
  emit("open-session", id);
}

async function newSessionAs(name: string): Promise<void> {
  startingSession.value = true;
  sessionStartMsg.value = "";
  try {
    const res = await fetchJSON(`/agents/${encodeURIComponent(name)}/sessions`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      // the server DECODES a request body here (id is optional) - a body
      // -less POST dies at "invalid request: EOF" (t_1ba868bb: found
      // while wiring the open; the surface was write-only twice over).
      body: JSON.stringify({}),
    });
    if (!res.ok) throw new Error(await res.text());
    // 201 carries the created session: open it IN the main pane
    // immediately (the old flow only refreshed the list - the write
    // was invisible). The refresh keeps the detail list honest.
    const created = (await res.json()) as OmegaAgentSession;
    await select(detail.value as OmegaAgent);
    if (created.id) emit("open-session", created.id);
  } catch (e) {
    sessionStartMsg.value = e instanceof Error ? e.message : String(e);
  } finally {
    startingSession.value = false;
  }
}

onMounted(load);

defineExpose({ load, select, profiles, detail, detailSessions });
</script>

<style scoped>
/* the open affordance (card t_1ba868bb): a session row is a click
   target that rides the main-pane opener. */
.agents-session-row {
  cursor: pointer;
}
.agents-session-row:hover {
  background: var(--z2, rgba(128, 128, 128, 0.12));
}
</style>