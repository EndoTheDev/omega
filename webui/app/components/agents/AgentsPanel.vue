<template>
  <!-- card t_7e2b7002 + t_372b9e9a: the Agents panel on the shared
       RegistryList (one browser pattern across the registry tabs).
       card t_c7222095: the Continuous dockable folded in here; the
       "+" in the header is the continuous create flow, "@" the config
       scaffold. A host serving no continuous module keeps the tab
       honest: no badge, no "+". -->
  <!-- card t_2d8deb8c (Endo's three rulings): 1) the DEFAULT agent
       CANNOT be disabled - its row carries the locked pill (permanently
       on, no arm step) and the backend refuses a curl; 2) the rows
       ride the cron row law - two-line anatomy, the right-anchored
       pill, the live dot + status word on the subtitle line; 3) the
       expansion shows the agent's DETAILS + EDIT, and session rows are
       gone from the tab (the per-agent sessions endpoints stay in the
       API - the tab simply does not render them). Waking is NOT
       the tab's job (ruling 3 of 2026-11-01): the strip here keeps
       only what the continuous merge already owned; no wake control
       was added to non-continuous rows. The list is FLAT (Endo: "flat
       for now. no grouping."). -->
  <section class="pane agents">
    <!-- card t_308e6d9b: the UNIVERSAL band - the scaffold "@"
         control rides the band; the band renders before the
         .agents-body scrollport: scrolling the list never moves it. -->
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
      <template #detail="{ row }">
        <!-- the expansion: the agent's details + edit (card t_2d8deb8c
             ruling 3). The DEFAULT agent's config IS the home config -
             the edit form is not offered (the backend refuses with the
             same law); the line says why. The per-agent sessions
             endpoints stay in the API; the tab does not render them. -->
        <p
          v-if="row.name === DefaultAgentName"
          class="agents-readonly-note">the default agent's config is the home config - edit it there; it is read-only here</p>
        <form
          v-else-if="editDraft[row.name]"
          class="agents-edit"
          @submit.prevent="saveAgentEdit(row.name)">
          <label class="agents-edit-field">
            <span class="agents-edit-label">description</span>
            <input
              v-model="editDraft[row.name].description"
              class="agents-edit-input"
              placeholder="one-line description"
              aria-label="agent description" />
          </label>
          <div class="agents-edit-pair">
            <label class="agents-edit-field">
              <span class="agents-edit-label">provider type</span>
              <select
                v-model="editDraft[row.name].provider_type"
                class="agents-edit-input"
                aria-label="provider type">
                <option value="">(home config)</option>
                <option value="ollama">ollama</option>
                <option value="openai">openai</option>
                <option value="anthropic">anthropic</option>
              </select>
            </label>
            <label class="agents-edit-field">
              <span class="agents-edit-label">model</span>
              <input
                v-model="editDraft[row.name].model_name"
                class="agents-edit-input"
                placeholder="(home config)"
                aria-label="model name" />
            </label>
          </div>
          <label class="agents-edit-field">
            <span class="agents-edit-label">host</span>
            <input
              v-model="editDraft[row.name].host"
              class="agents-edit-input"
              placeholder="(home config)"
              aria-label="provider host" />
          </label>
          <label class="agents-edit-field">
            <span class="agents-edit-label">api key</span>
            <input
              type="password"
              v-model="editDraft[row.name].api_key"
              class="agents-edit-input"
              placeholder="(leave empty to keep)"
              aria-label="api key" />
          </label>
          <label class="agents-edit-field">
            <span class="agents-edit-label">system prompt</span>
            <textarea
              v-model="editDraft[row.name].system_prompt"
              class="agents-edit-input agents-edit-prompt"
              rows="3"
              placeholder="(home config)"
              aria-label="system prompt" />
          </label>
          <div class="agents-edit-go">
            <button
              class="agents-edit-save"
              type="submit"
              :disabled="saving === row.name">save</button>
            <span
              v-if="editMsg[row.name]"
              class="agents-edit-msg">{{ editMsg[row.name] }}</span>
          </div>
        </form>

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
  agentStatusFor,
  continuousIndex,
  DefaultAgentName,
  mergedRowDetail,
} from "./agentsRows.ts";
import type { OmegaAgent } from "./agentsRows.ts";
import {
  evidenceSummary,
  rowStatus,
  type OmegaChatMessage,
  type OmegaContinuousRow,
  type OmegaEvidenceRow,
} from "../../../../harness/continuous/ui/continuousRows.ts";
import type { RegistryRow } from "~/composables/omega";

const profiles = ref<OmegaAgent[]>([]);
const fold = ref<string[]>([]);
// card t_c7222095: the continuous roster, joined into the agent rows
// by name. A failed/absent roster is the honest degradation - the
// rows keep their config facts, the status word and "+" vanish.
const roster = ref<OmegaContinuousRow[]>([]);
const continuousAvailable = ref(false);
// loadError covers the LIST fetch; toggleError covers a refused toggle
const loading = ref(false);
const loadError = ref("");
// card t_87303e8f: `loaded` gates the empty-text beat in RegistryList.
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
// mirror the create-flow pattern, no per-row component)
const waking = ref<string | null>(null);
const wakeInput = reactive<Record<string, string>>({});
const wakeMsg = reactive<Record<string, string>>({});
const chatByAgent = reactive<Record<string, OmegaChatMessage[]>>({});
const evidenceByAgent = reactive<Record<string, OmegaEvidenceRow[]>>({});
// the edit affordance (card t_2d8deb8c ruling 3): one draft per
// expanded agent, seeded from the profile facts on expand. host /
// api_key / system_prompt are NOT in the profile payload - they ride
// empty and only write when the operator fills them (absent facts can
// never be blanked by a round-trip that did not receive them).
interface EditDraft {
  description: string;
  provider_type: string;
  model_name: string;
  host: string;
  api_key: string;
  system_prompt: string;
}
const editDraft = reactive<Record<string, EditDraft>>({});
const editMsg = reactive<Record<string, string>>({});
const saving = ref<string | null>(null);
// false when the host serves no toggle seam (501 on /disabled): the
// list stays, read-only.
const toggleable = ref(false);

// the row model: name + description + config detail for the row, the
// fold state merged by name, the continuous wake state folded in by
// name (the status dot+word rides the row law's subtitle line).
// card t_2d8deb8c: the DEFAULT agent is LOCKED - `disabled` never
// renders true for it (a stale persisted entry a pre-lock build left
// in the fold does not render a disabled row) and the pill is
// non-interactive. The under-row preview line DIED with ruling 3 -
// the gray message-preview line Endo circled.
const rows = computed<RegistryRow[]>(() => {
  const set = new Set(fold.value);
  const idx = continuousIndex(roster.value);
  return profiles.value.map((a) => {
    const c = idx[a.name] ?? null;
    return {
      name: a.name,
      description: a.description || "",
      detail: mergedRowDetail(a, c),
      disabled: a.name === DefaultAgentName ? false : set.has(a.name),
      locked: a.name === DefaultAgentName,
      ...(agentStatusFor(c) ? { status: agentStatusFor(c) } : {}),
    };
  });
});

function rosterOf(name: string): OmegaContinuousRow | null {
  const idx = continuousIndex(roster.value);
  return idx[name] ?? null;
}

// the status voice: the roster row's word (awake / "next wake HH:MM" /
// "sleeping"), undefined for a config-only agent - the default agent
// has no roster, so its row carries no invented state.
function rowStatusVoice(name: string): { on: boolean; label: string } | undefined {
  return agentStatusFor(rosterOf(name));
}
// rowStatus word, raw (the wake strip's state references keep using
// the vocabulary; the subtitle renders it decorated).
function rowStatusOf(name: string): string {
  const c = rosterOf(name);
  return c ? rowStatus(c) : "";
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
    // 404/501 is a host without the continuous module - no status, no
    // "+"; any other failure is the honest empty roster.
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

// expand: the edit draft seeds from the profile facts (the list
// payload already carries them); a rostered agent also refreshes its
// canonical chat + evidence - the chat fetch marks the canonical
// session read server-side, the same clear the ContinuousPanel detail
// owned, so the unread badge honest-drops. Session loads are GONE
// (card t_2d8deb8c: the tab does not render sessions).
function onExpand(row: RegistryRow): void {
  const p = profiles.value.find((a) => a.name === row.name);
  if (p && row.name !== DefaultAgentName) {
    editDraft[row.name] = {
      description: p.description ?? "",
      provider_type: p.provider_type ?? "",
      model_name: p.model_name ?? "",
      host: "",
      api_key: "",
      system_prompt: "",
    };
    editMsg[row.name] = "";
  }
  const name = row.name;
  if (!rosterOf(name)) return;
  void loadContinuousDetail(name);
}

// the canonical chat + evidence refresh, split out of onExpand (a
// failed read keeps the detail; the strip is a read).
async function loadContinuousDetail(name: string): Promise<void> {
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

// the soft toggle: PUT /disabled/agents never touches agent home data;
// the response carries the fresh fold when it landed cleanly, a failure
// falls back to the GET so the rows re-render from persisted truth.
// The default agent never reaches here: its pill is locked
// (RegistryList skips the emit for a locked row) and the handler
// refuses a curl - the UI lock is a mirror of the backend law.
async function onToggle(name: string, enabled: boolean): Promise<void> {
  if (busy.value !== null) return;
  if (name === DefaultAgentName && !enabled) return; // never
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

// saveAgentEdit PUTs the DIFF the draft carries through
// PUT /agents/{name}: a changed description/provider/model and any
// filled host/api_key/system_prompt. Nothing typed = nothing sent
// (the honest "no changes"). Unknown fields never blank: nil is
// untouched on the wire.
async function saveAgentEdit(name: string): Promise<void> {
  const d = editDraft[name];
  if (!d) return;
  const p = profiles.value.find((a) => a.name === name);
  if (!p) return;
  const body: Record<string, string> = {};
  if (d.description !== (p.description ?? "")) body.description = d.description;
  if (d.provider_type !== (p.provider_type ?? "")) body.provider_type = d.provider_type;
  if (d.model_name !== (p.model_name ?? "")) body.model_name = d.model_name;
  if (d.host.trim()) body.host = d.host.trim();
  if (d.api_key) body.api_key = d.api_key;
  if (d.system_prompt.trim()) body.system_prompt = d.system_prompt;
  if (!Object.keys(body).length) {
    editMsg[name] = "no changes";
    return;
  }
  saving.value = name;
  editMsg[name] = "";
  try {
    const res = await fetch(`/agents/${encodeURIComponent(name)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (!res.ok) throw new Error(await res.text());
    await load();
    // re-seed the draft from the saved truth (host/api_key stay
    // as-typed-hidden; they are write-only inputs here)
    const fresh = profiles.value.find((a) => a.name === name);
    if (fresh) {
      editDraft[name].description = fresh.description ?? "";
      editDraft[name].provider_type = fresh.provider_type ?? "";
      editDraft[name].model_name = fresh.model_name ?? "";
    }
    editMsg[name] = "saved";
  } catch (e) {
    editMsg[name] = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = null;
  }
}

onMounted(load);

defineExpose({ load, profiles, fold, rows, roster, rowStatusOf, rowStatusVoice });
</script>

<style scoped>
/* card t_308e6d9b: the root is a band + scroll body; card t_2d8deb8c:
   the body's inset/padding rides the CRON reference (4px 12px 14px)
   - padding/margin/gap parity with the cron tab. */
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
  padding: 4px 12px 14px;
}
/* card t_c7222095: the merged header actions ("+" continuous, "@" scaffold). */
.agents-actions {
  display: flex;
  gap: 4px;
}
/* card t_2d8deb8c: the edit affordance - the scaffold field set,
   label/input pairs, the same token-voiced inputs the create flow
   uses. The default agent renders the read-only note instead. */
.agents-readonly-note {
  color: var(--text-dim, #8a8a92);
  font-size: 11px;
  margin: 4px 0;
}
.agents-edit {
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin: 4px 0 6px 10px;
  max-width: 420px;
}
.agents-edit-field {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.agents-edit-pair {
  display: flex;
  gap: 8px;
}
.agents-edit-pair .agents-edit-field {
  flex: 1;
}
.agents-edit-label {
  color: var(--text-dim, #8a8a92);
  font-size: 10px;
  text-transform: lowercase;
}
.agents-edit-input {
  background: var(--z2, rgba(128, 128, 128, 0.12));
  border: 1px solid var(--z3, rgba(128, 128, 128, 0.3));
  border-radius: 6px;
  color: var(--text);
  font: inherit;
  padding: 4px 8px;
  min-width: 0;
}
.agents-edit-prompt {
  resize: vertical;
}
.agents-edit-go {
  display: flex;
  align-items: center;
  gap: 8px;
}
.agents-edit-save {
  background: var(--z2, rgba(128, 128, 128, 0.12));
  border: 1px solid var(--z3, rgba(128, 128, 128, 0.3));
  border-radius: 6px;
  color: var(--text);
  cursor: pointer;
  font: inherit;
  padding: 4px 10px;
}
.agents-edit-msg {
  color: var(--text-dim, #8a8a92);
  font-size: 10px;
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