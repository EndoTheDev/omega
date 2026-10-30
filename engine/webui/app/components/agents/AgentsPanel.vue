<template>
  <!-- card t_7e2b7002 + t_372b9e9a: the Agents panel reworked onto the
       shared RegistryList (one browser pattern across skills, tools,
       agents - same look, same two-step toggle, same expand).
       The toggle is the SOFT one: a disabled agent stays registered
       (folder presence is registration, the home is never touched) and
       only loses its roster row + wake triggers - config.yaml
       disabled.agents. Click = expand (config summary, lazily loaded
       session list, "new session as this agent"). Creation v1 =
       scaffold + config.yaml edit in the file tree, no config form.
       The composables/omega.ts disabled helpers drive the wire.

       card t_c7222095 - THE MERGE: the Continuous dockable folded in
       here (deletability law: one tab for one concept). Agent row =
       config + inline wake-state badge + last-report preview; the
       expanded detail carries the wake/sleep strip, the canonical
       chat and the evidence ledger (the ContinuousPanel pattern, all
       its fetches preserved). The "+" in the header is the
       continuous create flow (name, directive, schedule); "@" stays
       the config scaffold. A host serving no continuous module keeps
       the tab honest: no badge, no preview, no "+". -->
  <section class="pane agents">
    <!-- card t_308e6d9b: the UNIVERSAL band (the shared TabBand) - the
         scaffold "+" / "@" controls ride the band now (the agent
         head/styles below own nothing else). The band renders before
         the .agents-body scrollport: scrolling the list never moves
         it. -->
    <TabBand title="Agents">
      <div class="agents-actions">
        <button
          v-if="continuousAvailable"
          class="agents-new band-tool"
          title="create a continuous agent (home + canonical chat + schedule)"
          @click="showContCreate = !showContCreate">+</button>
        <button
          class="agents-new band-tool"
          title="scaffold a new agent directory"
          @click="showCreate = !showCreate">@</button>
      </div>
    </TabBand>
    <div class="agents-body">
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

    <form
      v-if="showContCreate"
      class="agents-create"
      @submit.prevent="createContinuous">
      <input
        v-model="contName"
        class="agents-create-name"
        placeholder="agent name (lowercase, hyphens ok)"
        aria-label="continuous agent name" />
      <input
        v-model="contDirective"
        class="agents-create-desc"
        placeholder="directive (standing intent)"
        aria-label="directive" />
      <input
        v-model="contSchedule"
        class="agents-create-desc"
        placeholder="wake schedule (cron, e.g. 17 * * * *)"
        aria-label="wake schedule" />
      <button
        class="agents-create-go"
        :disabled="!contName || creatingCont"
        type="submit">create</button>
    </form>
    <p
      v-if="contCreateError"
      class="settings-error agents-error">{{ contCreateError }}</p>

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
      @expand="onExpand">
      <template #side="{ row }">
        <span
          v-if="rowStatusOf(row.name)"
          class="agents-badge"
          :class="{ unread: isUnread(row.name) }">{{ rowStatusOf(row.name) }}</span>
      </template>
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

        <!-- the continuous strip (card t_c7222095): only for an agent
             with a wake home - the wake buttons, wake message, the
             canonical chat and the evidence ledger, all the
             ContinuousPanel fetches preserved. -->
        <template v-if="rosterOf(row.name)">
          <h4 class="agents-cont-sub">wake controls</h4>
          <div class="agents-cont-actions">
            <button
              class="agents-cont-wake"
              title="wake this agent now (one budgeted run)"
              :disabled="waking === row.name"
              @click="wake(row.name, wakeInput[row.name] || '')">wake</button>
            <button
              class="agents-cont-sleep"
              title="sleep this agent (ends a running wake)"
              @click="sleep(row.name)">sleep</button>
          </div>
          <input
            v-model="wakeInput[row.name]"
            class="agents-cont-message"
            placeholder="optional wake message (goes into the canonical chat)"
            aria-label="wake message" />
          <p
            v-if="wakeMsg[row.name]"
            class="agents-status">{{ wakeMsg[row.name] }}</p>

          <h4 class="agents-cont-sub">canonical chat</h4>
          <ol
            v-if="chatByAgent[row.name]?.length"
            class="agents-cont-chat"
            role="list">
            <li
              v-for="(m, i) in chatByAgent[row.name]"
              :key="i"
              class="agents-cont-chat-row">{{ m.content }}</li>
          </ol>
          <p
            v-else
            class="agents-status">no messages yet</p>

          <h4 class="agents-cont-sub">evidence</h4>
          <p class="agents-cont-evidence-sum">{{ evidenceSummaryText(row.name) }}</p>
          <ol
            v-if="evidenceByAgent[row.name]?.length"
            class="agents-cont-evidence"
            role="list">
            <li
              v-for="e in evidenceByAgent[row.name]"
              :key="e.id"
              class="agents-cont-evidence-row">
              <span
                :class="['agents-cont-evidence-ok', e.ok ? 'ok' : 'failed']">{{ e.ok ? "ok" : "failed" }}</span>
              <span class="agents-cont-evidence-tool">{{ e.tool }}</span>
              <span class="agents-cont-evidence-verifier">{{ e.verifier || "-" }}</span>
              <span class="agents-cont-evidence-hash">{{ e.args_hash.slice(0, 8) }}</span>
            </li>
          </ol>
          <p
            v-else
            class="agents-status">no tool calls recorded</p>
        </template>
      </template>
    </RegistryList>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import {
  agentSessionTitle,
  continuousIndex,
  mergedRowDetail,
} from "./agentsRows.ts";
import type { OmegaAgent, OmegaAgentSession } from "./agentsRows.ts";
import {
  evidenceSummary,
  previewLine,
  rowStatus,
  type OmegaChatMessage,
  type OmegaContinuousRow,
  type OmegaEvidenceRow,
} from "./continuousRows.ts";
import type { RegistryRow } from "~/composables/omega";

// emit("open-session", id) is the Hermes-bots parity handoff (card
// t_1ba868bb): app.vue routes the id through openChatTab, the same
// opener a sidebar session row uses - the main-pane chat surface, no
// degraded mode. Sends on the open session ride /chat; the row's
// agent binding never switches (server-enforced at creation).
const emit = defineEmits<{ (e: "open-session", id: string): void }>();

const profiles = ref<OmegaAgent[]>([]);
const fold = ref<string[]>([]);
// card t_c7222095: the continuous roster, joined into the agent rows
// by name. A failed/absent roster is the honest degradation - the
// rows keep their config facts, the badge and preview vanish.
const roster = ref<OmegaContinuousRow[]>([]);
const continuousAvailable = ref(false);
// loadError covers the LIST fetch; toggleError covers a refused toggle
const loading = ref(false);
const loadError = ref("");
// card t_87303e8f follow-through: `loaded` was referenced (the
// :loaded prop into RegistryList and set in load()'s finally) but never
// declared - every agents-tab load threw a console
// "ReferenceError: loaded is not defined" and the empty-text beat in
// RegistryList never fired ("loaded is not defined" reds journey 92's
// console-error sweep even when every toggle beat is green).
const loaded = ref(false);
const toggleError = ref<string | null>(null);
const busy = ref<string | null>(null);
const showCreate = ref(false);
const creating = ref(false);
const createName = ref("");
const createDescription = ref("");
const createError = ref("");
// the continuous create flow (the ContinuousPanel header form folded in)
const showContCreate = ref(false);
const creatingCont = ref(false);
const contName = ref("");
const contDirective = ref("");
const contSchedule = ref("");
const contCreateError = ref("");
// the wake strip state, per expanded agent (records - reactive maps
// mirror the sessions/sessionMsg pattern, no per-row component)
const waking = ref<string | null>(null);
const wakeInput = reactive<Record<string, string>>({});
const wakeMsg = reactive<Record<string, string>>({});
const chatByAgent = reactive<Record<string, OmegaChatMessage[]>>({});
const evidenceByAgent = reactive<Record<string, OmegaEvidenceRow[]>>({});
const startingSession = ref<string | null>(null);
const sessionMsg = ref<Record<string, string>>({});
const sessions = ref<Record<string, OmegaAgentSession[]>>({});
// false when the host serves no toggle seam (501 on /disabled): the
// list stays, read-only.
const toggleable = ref(false);

// the row model: name + description + config detail for the row, the
// fold state merged by name (withDisabledFlags's law, shell side uses
// the shared omega.ts helper), the continuous wake state folded in by
// name (badge in the #side slot, preview under the row - card
// t_c7222095).
const rows = computed<RegistryRow[]>(() => {
  const set = new Set(fold.value);
  const idx = continuousIndex(roster.value);
  return profiles.value.map((a) => {
    const c = idx[a.name] ?? null;
    return {
      name: a.name,
      description: a.description || "",
      detail: mergedRowDetail(a, c),
      disabled: set.has(a.name),
      ...(c ? { preview: previewLine(c) } : {}),
    };
  });
});

function rosterOf(name: string): OmegaContinuousRow | null {
  const idx = continuousIndex(roster.value);
  return idx[name] ?? null;
}

// the badge voice: the roster row's status ("sleeping" / "next wake
// HH:MM" / "unread report"), empty for a config-only agent.
function rowStatusOf(name: string): string {
  const c = rosterOf(name);
  return c ? rowStatus(c) : "";
}

function isUnread(name: string): boolean {
  return rosterOf(name)?.unread ?? false;
}

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
    // the continuous roster rides the same load (card t_c7222095):
    // 404/501 is a host without the continuous module - no badge, no
    // preview, no "+"; any other failure is the honest empty roster.
    await loadRoster();
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
    loaded.value = true;
  }
}

async function loadRoster(): Promise<void> {
  try {
    const res = await fetch("/continuous/roster");
    if (!res.ok) {
      continuousAvailable.value = false;
      roster.value = [];
      return;
    }
    roster.value = ((await res.json()) ?? []) as OmegaContinuousRow[];
    continuousAvailable.value = true;
  } catch {
    continuousAvailable.value = false;
    roster.value = [];
  }
}

// expand: the session list loads lazily on every expand (a session
// added elsewhere shows up the next time the row opens); a rostered
// agent also refreshes its canonical chat + evidence - the chat fetch
// marks the canonical session read server-side, the same clear the
// ContinuousPanel detail owned, so the unread badge honest-drops.
async function onExpand(row: RegistryRow): Promise<void> {
  void loadSessions(row);
  const name = row.name;
  if (!rosterOf(name)) return;
  const enc = encodeURIComponent(name);
  try {
    const res = await fetch(`/continuous/${enc}/chat`);
    if (res.ok) {
      chatByAgent[name] =
        ((await res.json()) as { messages?: OmegaChatMessage[] })?.messages ?? [];
    }
  } catch {
    chatByAgent[name] = []; // the detail stays; the chat is a read
  }
  try {
    const res = await fetch(`/continuous/${enc}/evidence`);
    if (res.ok) {
      evidenceByAgent[name] = ((await res.json()) as OmegaEvidenceRow[]) ?? [];
    }
  } catch {
    evidenceByAgent[name] = [];
  }
}

function evidenceSummaryText(name: string): string {
  return evidenceSummary(evidenceByAgent[name] ?? []);
}

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

// the continuous create flow (ruling 18's, verbatim semantics): name +
// directive + schedule -> the home folder + canonical chat. A conflict
// errors loud; the roster reload picks the new row up.
async function createContinuous(): Promise<void> {
  creatingCont.value = true;
  contCreateError.value = "";
  try {
    const res = await fetch("/continuous", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        name: contName.value.trim(),
        directive: contDirective.value,
        schedule: contSchedule.value,
      }),
    });
    if (!res.ok) throw new Error(await res.text());
    contName.value = "";
    contDirective.value = "";
    contSchedule.value = "";
    showContCreate.value = false;
    await load();
  } catch (e) {
    contCreateError.value = e instanceof Error ? e.message : String(e);
  } finally {
    creatingCont.value = false;
  }
}

// wake is ruling 8's human trigger: the typed message (or the standing
// directive) wakes the agent; the report lands in the canonical chat.
async function wake(name: string, message: string): Promise<void> {
  waking.value = name;
  wakeMsg[name] = "";
  try {
    const res = await fetch(`/continuous/${encodeURIComponent(name)}/chat`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ message }),
    });
    if (!res.ok) throw new Error(await res.text());
    wakeMsg[name] = "wake sent - the report lands in the canonical chat";
    wakeInput[name] = "";
    await loadRoster();
  } catch (e) {
    wakeMsg[name] = e instanceof Error ? e.message : String(e);
  } finally {
    waking.value = null;
  }
}

// sleep ends a running wake (ruling 7: asleep is the default state).
async function sleep(name: string): Promise<void> {
  try {
    await fetch(`/continuous/${encodeURIComponent(name)}/sleep`, { method: "POST" });
  } catch {
    /* the roster refresh is the truth */
  }
  wakeMsg[name] = "sleep sent";
  await loadRoster();
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

defineExpose({ load, profiles, fold, rows, roster, rowStatusOf });
</script>

<style scoped>
/* card t_308e6d9b: the root stops being a flat content blob - the
   TabBand stays fixed, the body owns the scroll (structurally, never
   sticky). The audit's calibration finding (the Agents tab was the
   barest tab: no layout at all) starts to converge on the Files
   column shape here. */
.pane.agents {
  flex: 1;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  padding: 0 0 6px;
}
.agents-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 4px 12px 8px;
}
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
/* card t_c7222095: the merged header actions ("+" continuous, "@"
   scaffold) and the inline wake badge. */
.agents-actions {
  display: flex;
  gap: 4px;
}
.agents-badge {
  color: var(--text-dim);
  font-size: 10px;
  white-space: nowrap;
}
.agents-badge.unread {
  color: var(--text);
  font-weight: 600;
}
/* the continuous strip in the expanded detail - the ContinuousPanel
   pattern's styles, renamed agents-cont-* to live under this scope. */
.agents-cont-sub {
  color: var(--text-dim);
  font-size: 11px;
  margin: 6px 0 0;
  text-transform: lowercase;
}
.agents-cont-actions {
  display: flex;
  gap: 6px;
  margin-top: 4px;
}
.agents-cont-actions button {
  background: var(--z2);
  border: 1px solid var(--z3);
  border-radius: 6px;
  color: var(--text);
  cursor: pointer;
  font: inherit;
  padding: 4px 10px;
}
.agents-cont-message {
  background: var(--z2);
  border: 1px solid var(--z3);
  border-radius: 6px;
  color: var(--text);
  font: inherit;
  padding: 6px 8px;
}
.agents-cont-chat,
.agents-cont-evidence {
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 3px;
  margin: 0;
  max-height: 220px;
  overflow-y: auto;
  padding: 0;
}
.agents-cont-chat-row {
  border-bottom: 1px solid var(--z2);
  font-size: 11px;
  padding: 3px 0;
  white-space: pre-wrap;
}
.agents-cont-evidence-row {
  display: flex;
  gap: 6px;
  font-size: 11px;
}
.agents-cont-evidence-ok.ok {
  color: var(--ok, #3f9);
}
.agents-cont-evidence-ok.failed {
  color: var(--err, #f66);
}
.agents-cont-evidence-hash {
  color: var(--text-dim);
  font-family: monospace;
}
.agents-cont-evidence-sum {
  color: var(--text-dim);
  font-size: 11px;
  margin: 0;
}
</style>