<template>
  <!-- card t_7e2b7002 + t_372b9e9a, v0.14.1: the Agents panel reworked
       onto the shared RegistryList (one browser pattern across skills,
       tools, agents - same look, same two-step toggle, same expand).
       The toggle is the SOFT one: a disabled agent stays registered
       (folder presence is registration, the home is never touched) and
       only loses its roster row + wake triggers - config.yaml
       disabled.agents. Click = expand (config summary, lazily loaded
       session list, "new session as this agent"). Creation v1 =
       scaffold + config.yaml edit in the file tree, no config form.
       The composables/omega.ts disabled helpers drive the wire. -->
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
    <RegistryList
      v-else
      kind="agents"
      :rows="rows"
      :busy="busy"
      :error="toggleError"
      :loaded="loaded"
      :toggleable="toggleable"
      empty-text="no agents registered"
      @toggle="onToggle"
      @expand="loadSessions">
      <template #detail="{ row }">
        <button
          class="agents-session-new"
          title="create a session bound to this agent"
          :disabled="startingSession === row.name"
          @click="newSessionAs(row.name)">new session as this agent</button>
        <p
          v-if="startingSession === row.name"
          class="agents-status">creating…</p>
        <p
          v-else-if="sessionMsg[row.name]"
          class="agents-status">{{ sessionMsg[row.name] }}</p>
        <ol
          v-if="sessions[row.name]?.length"
          class="agents-sessions"
          role="list">
          <li
            v-for="s in sessions[row.name]"
            :key="s.id"
            class="agents-session-row"
            :title="`open ${agentSessionTitle(s)} in the main pane`"
            @click="openAgentSession(s.id)">
            <span class="agents-session-title">{{ agentSessionTitle(s) }}</span>
            <span class="agents-session-meta">{{ s.id }}</span>
          </li>
        </ol>
        <p
          v-else-if="sessions[row.name]"
          class="agents-status">no sessions as this agent</p>
      </template>
    </RegistryList>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { agentSessionTitle, agentDetailLines } from "./agentsRows.ts";
import type { OmegaAgent, OmegaAgentSession } from "./agentsRows.ts";
import type { RegistryRow } from "~/composables/omega";

// emit("open-session", id) is the Hermes-bots parity handoff (card
// t_1ba868bb): app.vue routes the id through openChatTab, the same
// opener a sidebar session row uses - the main-pane chat surface, no
// degraded mode. Sends on the open session ride /chat; the row's
// agent binding never switches (server-enforced at creation).
const emit = defineEmits<{ (e: "open-session", id: string): void }>();

const profiles = ref<OmegaAgent[]>([]);
const fold = ref<string[]>([]);
// loadError covers the LIST fetch; toggleError covers a refused toggle
const loading = ref(false);
const loadError = ref("");
const toggleError = ref<string | null>(null);
const busy = ref<string | null>(null);
const showCreate = ref(false);
const creating = ref(false);
const createName = ref("");
const createDescription = ref("");
const createError = ref("");
const startingSession = ref<string | null>(null);
const sessionMsg = ref<Record<string, string>>({});
const sessions = ref<Record<string, OmegaAgentSession[]>>({});
// false when the host serves no toggle seam (501 on /disabled): the
// list stays, read-only.
const toggleable = ref(false);

// the row model: name + description for the row, the config summary
// as the expandable detail lines (agentsRows.ts pure helper), the
// fold state merged by name (withDisabledFlags's law, shell side uses
// the shared omega.ts helper).
const rows = computed<RegistryRow[]>(() => {
  const set = new Set(fold.value);
  return profiles.value.map((a) => ({
    name: a.name,
    description: a.description || "",
    detail: agentDetailLines(a),
    disabled: set.has(a.name),
  }));
});

async function fetchDisabledAgents(): Promise<string[] | null> {
  const res = await fetch("/disabled");
  if (res.status === 501) {
    toggleable.value = false; // no seam: read-only
    return null;
  }
  if (!res.ok) return null;
  toggleable.value = true;
  const body = (await res.json())?.agents;
  return Array.isArray(body)
    ? body.filter((n): n is string => typeof n === "string")
    : [];
}

async function load(): Promise<void> {
  loading.value = true;
  loadError.value = "";
  try {
    const res = await fetch("/agents");
    if (!res.ok) throw new Error(`GET /agents: ${res.status}`);
    profiles.value = ((await res.json()) ?? []) as OmegaAgent[];
    const agents = await fetchDisabledAgents();
    if (agents !== null) fold.value = agents;
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
    loaded.value = true;
  }
}

// expand: the session list loads lazily on every expand (a session
// added elsewhere shows up the next time the row opens).
async function loadSessions(row: RegistryRow): Promise<void> {
  try {
    const res = await fetch(`/agents/${encodeURIComponent(row.name)}/sessions`);
    if (res.ok) {
      sessions.value = {
        ...sessions.value,
        [row.name]: ((await res.json()) ?? []) as OmegaAgentSession[],
      };
    }
  } catch {
    sessions.value = { ...sessions.value, [row.name]: [] }; // the detail stays; the session list is a read
  }
}

async function createAgent(): Promise<void> {
  creating.value = true;
  createError.value = "";
  try {
    const res = await fetch("/agents", {
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

// the SOFT toggle: PUT /disabled/agents never touches agent home data;
// the response carries the fresh fold when it landed cleanly, a failure
// falls back to the GET so the rows re-render from persisted truth
// (409: the main home has no roster to leave and no wake to refuse).
async function onToggle(name: string, enabled: boolean): Promise<void> {
  if (busy.value !== null) return;
  busy.value = name;
  toggleError.value = null;
  try {
    const res = await fetch("/disabled/agents", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, enabled }),
    });
    if (!res.ok) throw new Error(await res.text());
    const body = (await res.json())?.agents;
    if (Array.isArray(body)) {
      fold.value = body.filter((n): n is string => typeof n === "string");
    } else {
      await load();
    }
  } catch (e) {
    toggleError.value = e instanceof Error ? e.message : String(e);
    try {
      const agents = await fetchDisabledAgents();
      if (agents !== null) fold.value = agents;
    } catch {
      // the fold stays; the error line stands
    }
  } finally {
    busy.value = null;
  }
}

async function newSessionAs(name: string): Promise<void> {
  startingSession.value = name;
  sessionMsg.value = { ...sessionMsg.value, [name]: "" };
  try {
    const res = await fetch(`/agents/${encodeURIComponent(name)}/sessions`, {
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
    // was invisible). The refresh keeps the fold honest too.
    const created = (await res.json()) as OmegaAgentSession;
    if (created.id) emit("open-session", created.id);
    await loadSessions({ name } as RegistryRow);
  } catch (e) {
    sessionMsg.value = {
      ...sessionMsg.value,
      [name]: e instanceof Error ? e.message : String(e),
    };
  } finally {
    startingSession.value = null;
  }
}

onMounted(load);

defineExpose({ load, profiles, fold, rows });
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
.agents-session-new {
  margin: 4px 0;
}
</style>