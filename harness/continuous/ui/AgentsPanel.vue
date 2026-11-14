<template>
  <!-- card t_7e2b7002 + t_372b9e9a: the Agents panel on the shared
       RegistryList (one browser pattern across the registry tabs).
       0.17.3 make-over (card t_df015e66): ONE create door - the single
       "+" opens one form whose agent-kind choice decides the wire
       (scaffold / continuous / clone). The "@" duplicate button and its
       near-identical form died with ruling 1. -->
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
    <!-- card t_308e6d9b: the UNIVERSAL band - the create "+" rides the
         band; the band renders before the .agents-body scrollport:
         scrolling the list never moves it. -->
    <TabBand title="Agents">
      <div class="agents-actions">
        <button
          class="agents-new band-tool"
          title="create an agent"
          aria-label="create an agent"
          @click="toggleCreate">+</button>
      </div>
    </TabBand>
    <div class="agents-body">
      <!-- THE ONE CREATE DOOR (ruling 1): one form, the kind inside. -->
      <form
        v-if="showCreate"
        class="agents-create"
        @submit.prevent="submitCreate">
        <input
          v-model="create.name"
          class="agents-create-name"
          placeholder="agent name (lowercase, hyphens ok)"
          aria-label="agent name" />
        <select
          v-model="create.kind"
          class="agents-create-name"
          aria-label="agent kind">
          <option
            v-for="k in AGENT_KINDS"
            :key="k"
            :value="k"
            :disabled="kindNeedsContinuous(k) && !continuousAvailable">{{ agentKindLabel(k) }}</option>
        </select>
        <input
          v-model="create.description"
          class="agents-create-desc"
          placeholder="one-line description (optional)"
          aria-label="agent description" />
        <template v-if="create.kind === 'continuous'">
          <input
            v-model="create.directive"
            class="agents-create-desc"
            placeholder="directive (standing intent)"
            aria-label="directive" />
          <input
            v-model="create.schedule"
            class="agents-create-desc"
            placeholder="wake schedule (cron, e.g. 17 * * * *)"
            aria-label="wake schedule" />
        </template>
        <template v-else-if="create.kind === 'clone'">
          <select
            v-model="create.inherit_from"
            class="agents-create-name"
            aria-label="inherit from agent">
            <option value="">choose the agent to copy</option>
            <option
              v-for="p in cloneCandidates"
              :key="p.name"
              :value="p.name">{{ p.name }}</option>
          </select>
          <p class="agents-create-note">
            a full materialized copy at birth - definition, skills, toggles,
            persona files and the session store. The copy diverges freely and
            the source may be deleted afterwards: clone + delete IS the rename
            path.
          </p>
        </template>
        <p
          v-else
          class="agents-create-note">
          an empty home: nothing enabled, no skills - an agent never inherits
          main's feature set.
        </p>
        <button
          class="agents-create-go"
          :disabled="!create.name.trim() || creating"
          type="submit">{{ creating ? "creating…" : "create" }}</button>
      </form>
      <p
        v-if="createError"
        class="settings-error agents-error">{{ createError }}</p>
      <p
        v-if="notice"
        class="agents-status">{{ notice }}</p>

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
        :openable="rowHasChat"
        empty-text="no agents registered"
        @toggle="onToggle"
        @open="openAgentChat"
        @expand="onExpand">
        <!-- card t_81109e1c (ruling 3): the main agent rides the roster
             like every other home and its row says which one it is - the
             badge sits beside the name, so it survives a host that
             serves no toggle seam (where the locked pill never renders). -->
        <template #side="{ row }">
          <span
            v-if="row.name === DefaultAgentName"
            class="agents-main-badge"
            title="the main agent - the home at the daemon's root">main</span>
        </template>
        <template #detail="{ row }">
          <!-- the expansion: the agent's details + edit (card t_2d8deb8c
               ruling 3). The DEFAULT agent's config IS the home config -
               the edit form is not offered (the backend refuses with the
               same law); the line says why. The per-agent sessions
               endpoints stay in the API; the tab does not render them. -->
          <p
            v-if="row.name === DefaultAgentName"
            class="agents-readonly-note">the default agent's config is the home config - edit it there; it is read-only here</p>
          <template v-else>
            <form
              v-if="editDraft[row.name]"
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

              <!-- ruling 7: the inherited knobs carry their source. The
                   chip is the STATE (the layer file omits the key); the
                   button beside it is the layer's own reset verb. -->
              <div class="agents-edit-field">
                <span class="agents-edit-label">
                  system prompt
                  <span
                    v-if="chipInherited(row.name, 'system_prompt')"
                    class="agents-inherit-chip"
                    :title="chipNote(row.name, 'system_prompt')">{{ chipText(row.name, "system_prompt") }}</span>
                </span>
                <textarea
                  v-model="editDraft[row.name].system_prompt"
                  class="agents-edit-input agents-edit-prompt"
                  rows="3"
                  placeholder="(inherited from main)"
                  aria-label="system prompt" />
                <span
                  v-if="chipInherited(row.name, 'system_prompt')"
                  class="agents-inherit-note">{{ chipNote(row.name, "system_prompt") }}</span>
                <button
                  v-else
                  type="button"
                  class="agents-reset"
                  aria-label="inherit system prompt from main"
                  @click="resetField(row.name, 'system_prompt')">inherit from main</button>
              </div>

              <div class="agents-edit-pair">
                <div class="agents-edit-field">
                  <span class="agents-edit-label">
                    provider instance
                    <span
                      v-if="chipInherited(row.name, 'provider.instance')"
                      class="agents-inherit-chip"
                      :title="chipNote(row.name, 'provider.instance')">{{ chipText(row.name, "provider.instance") }}</span>
                  </span>
                  <!-- item 4: a REFERENCED instance from settings. No host,
                       no api key, no type - an agent file holds no secret
                       (agent/configedit.go law 2). -->
                  <select
                    v-model="editDraft[row.name].provider_instance"
                    class="agents-edit-input"
                    aria-label="provider instance">
                    <option value="default">default - the host's own provider</option>
                    <option
                      v-for="inst in providerInstances"
                      :key="inst"
                      :value="inst">{{ inst }}</option>
                  </select>
                  <span
                    v-if="chipInherited(row.name, 'provider.instance')"
                    class="agents-inherit-note">{{ chipNote(row.name, "provider.instance") }}</span>
                  <button
                    v-else
                    type="button"
                    class="agents-reset"
                    aria-label="inherit provider from main"
                    @click="resetField(row.name, 'provider.instance')">inherit from main</button>
                </div>
                <div class="agents-edit-field">
                  <span class="agents-edit-label">
                    model
                    <span
                      v-if="chipInherited(row.name, 'provider.model_name')"
                      class="agents-inherit-chip"
                      :title="chipNote(row.name, 'provider.model_name')">{{ chipText(row.name, "provider.model_name") }}</span>
                  </span>
                  <input
                    v-model="editDraft[row.name].model_name"
                    class="agents-edit-input"
                    placeholder="(inherited from main)"
                    aria-label="model name" />
                  <span
                    v-if="chipInherited(row.name, 'provider.model_name')"
                    class="agents-inherit-note">{{ chipNote(row.name, "provider.model_name") }}</span>
                  <button
                    v-else
                    type="button"
                    class="agents-reset"
                    aria-label="inherit model from main"
                    @click="resetField(row.name, 'provider.model_name')">inherit from main</button>
                </div>
              </div>

              <div class="agents-edit-field">
                <span class="agents-edit-label">
                  max turns
                  <span
                    v-if="chipInherited(row.name, 'max_turns')"
                    class="agents-inherit-chip"
                    :title="chipNote(row.name, 'max_turns')">{{ chipText(row.name, "max_turns") }}</span>
                </span>
                <input
                  v-model="editDraft[row.name].max_turns"
                  class="agents-edit-input"
                  type="number"
                  min="0"
                  placeholder="(inherited from main)"
                  aria-label="max turns" />
                <span
                  v-if="chipInherited(row.name, 'max_turns')"
                  class="agents-inherit-note">{{ chipNote(row.name, "max_turns") }}</span>
                <button
                  v-else
                  type="button"
                  class="agents-reset"
                  aria-label="inherit max turns from main"
                  @click="resetField(row.name, 'max_turns')">inherit from main</button>
              </div>

              <!-- ruling 4: ALWAYS OFF. The list IS the enabled set. -->
              <div class="agents-edit-field">
                <span class="agents-edit-label">toggles</span>
                <div
                  class="agents-check-list"
                  role="group"
                  aria-label="toggles">
                  <label
                    v-for="m in toggleChoicesFor(row.name)"
                    :key="m"
                    :class="['agents-check', { 'agents-check-off': !toggleNames.includes(m) }]"
                    :title="toggleNames.includes(m) ? '' : `${m} is off on the host - uncheck it here, or re-enable the capsule in settings`">
                    <input
                      v-model="editDraft[row.name].toggles"
                      type="checkbox"
                      :value="m" />{{ m }}
                  </label>
                </div>
                <span class="agents-inherit-note">{{ agentTogglesNote(editDraft[row.name].toggles) }}</span>
              </div>

              <!-- ruling 5: the explicit allowlist over the agent's
                   VISIBLE registry. Empty = no skills, never main's. -->
              <div class="agents-edit-field">
                <span class="agents-edit-label">skills</span>
                <div
                  v-if="skillOptionsFor(row.name).length"
                  class="agents-check-list"
                  role="group"
                  aria-label="skills">
                  <label
                    v-for="s in skillOptionsFor(row.name)"
                    :key="s"
                    class="agents-check">
                    <input
                      v-model="editDraft[row.name].skills_allow"
                      type="checkbox"
                      :value="s" />{{ s }}
                  </label>
                </div>
                <span class="agents-inherit-note">{{ agentSkillsNote(editDraft[row.name].skills_allow) }}</span>
              </div>

              <!-- inherit-as-disabled: main's artifacts are visible and
                   inert until the kind is switched on; enabling copies
                   the artifact into this home (never a live link). -->
              <div class="agents-edit-field">
                <span class="agents-edit-label">inherit from main</span>
                <div
                  class="agents-check-list"
                  role="group"
                  aria-label="inherit from main">
                  <label
                    v-for="kind in AGENT_INHERIT_KINDS"
                    :key="kind"
                    class="agents-check">
                    <input
                      v-model="editDraft[row.name].inherit[kind]"
                      type="checkbox" />{{ kind }}
                  </label>
                </div>
                <span class="agents-inherit-note">on = materialize a copy into this home at the next save</span>
              </div>

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

            <!-- ruling 2: delete = delete the HOME, sessions included.
                 The confirm names the count before the trigger fires. -->
            <div class="agents-danger">
              <button
                v-if="deleting !== row.name"
                type="button"
                class="agents-delete"
                :aria-label="agentDeleteLabel(row.name)"
                @click="deleting = row.name">{{ agentDeleteLabel(row.name) }}</button>
              <template v-else>
                <p class="agents-delete-confirm">{{ agentDeleteConfirm(row.name, sessionCountOf(row.name)) }}</p>
                <div class="agents-delete-go">
                  <button
                    type="button"
                    class="agents-delete-go-btn"
                    :disabled="removing === row.name"
                    @click="deleteHome(row.name)">delete the home</button>
                  <button
                    type="button"
                    class="agents-cancel"
                    @click="deleting = null">cancel</button>
                </div>
              </template>
              <p
                v-if="deleteError[row.name]"
                class="settings-error agents-error">{{ deleteError[row.name] }}</p>
            </div>
          </template>

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
  AGENT_INHERIT_KINDS,
  AGENT_KINDS,
  agentDeleteConfirm,
  agentDeleteLabel,
  agentKindLabel,
  agentRosterSubtitle,
  agentSkillsNote,
  agentStatusFor,
  agentTogglesNote,
  chipOf,
  cloneSources,
  continuousIndex,
  createRequest,
  DefaultAgentName,
  editRequest,
  emptyCreateDraft,
  fieldChips,
  kindNeedsContinuous,
  mergedRowDetail,
  skillOptions,
  toggleChoices,
  toggleOptions,
} from "./agentsRows.ts";
import type { AgentEditDraft, AgentInheritKind, OmegaAgent, SettingsScope } from "./agentsRows.ts";
import {
  evidenceSummary,
  rowStatus,
  type OmegaChatMessage,
  type OmegaContinuousRow,
  type OmegaEvidenceRow,
} from "./continuousRows.ts";
import type { RegistryRow } from "~/composables/omega";

const profiles = ref<OmegaAgent[]>([]);
const fold = ref<string[]>([]);
// card t_c7222095: the continuous roster, joined into the agent rows
// by name. A failed/absent roster is the honest degradation - the rows
// keep their config facts, the status word and the continuous kind
// vanish.
const roster = ref<OmegaContinuousRow[]>([]);
const continuousAvailable = ref(false);
// loadError covers the LIST fetch; toggleError covers a refused toggle
const loading = ref(false);
const loadError = ref("");
// notice is the panel's own receipt line (a delete's outcome, "deleted
// scout's home - 3 sessions went with it") - never an error lane.
const notice = ref("");
// card t_87303e8f: `loaded` gates the empty-text beat in RegistryList.
const loaded = ref(false);
const toggleError = ref<string | null>(null);
const busy = ref<string | null>(null);

// --- the ONE create door (ruling 1 + 6) -------------------------------
const showCreate = ref(false);
const creating = ref(false);
const create = reactive(emptyCreateDraft());
const createError = ref("");

function toggleCreate(): void {
  showCreate.value = !showCreate.value;
  if (showCreate.value) createError.value = "";
}

/** cloneCandidates is every OTHER agent home (main included) - the
    create form's own name never offers itself as a copy source. */
const cloneCandidates = computed<OmegaAgent[]>(() =>
  cloneSources(profiles.value, create.name),
);

/** submitCreate routes the ONE form by its KIND (the pure createRequest):
    scaffold -> POST /agents, continuous -> POST /continuous, clone ->
    POST /agents {inherit_from} (the landed CLONE law - a materialized
    copy at birth, never a live link). */
async function submitCreate(): Promise<void> {
  const req = createRequest(create);
  if (!req) {
    createError.value =
      create.kind === "clone"
        ? "choose the agent to copy - a clone needs its source"
        : "an agent needs a name";
    return;
  }
  creating.value = true;
  createError.value = "";
  try {
    const res = await fetch(req.path, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(req.body),
    });
    if (!res.ok) throw new Error(await res.text());
    Object.assign(create, emptyCreateDraft());
    showCreate.value = false;
    await load(true);
  } catch (e) {
    createError.value = e instanceof Error ? e.message : String(e);
  } finally {
    creating.value = false;
  }
}

// the wake strip state, per expanded agent (records - reactive maps
// mirror the create-flow pattern, no per-row component)
const waking = ref<string | null>(null);
const wakeInput = reactive<Record<string, string>>({});
const wakeMsg = reactive<Record<string, string>>({});
const chatByAgent = reactive<Record<string, OmegaChatMessage[]>>({});
const evidenceByAgent = reactive<Record<string, OmegaEvidenceRow[]>>({});

// --- the definition layer's edit form (0.17.3 field set) --------------
// description, system prompt, provider INSTANCE, model, max turns,
// toggles, skills allowlist, the inherit flags. host / api_key are NOT
// here: the panel never carries a secret (law 2).
const editDraft = reactive<Record<string, AgentEditDraft>>({});
const editMsg = reactive<Record<string, string>>({});
const saving = ref<string | null>(null);

// --- the inheritance read (ruling 7) + the host's definitions ---------
// GET /settings answers the scopes (the SAME payload the settings pane
// renders; its `overridden` flag is the authored-vs-inherited state) and
// the named provider instances the provider picker references. GET
// /modules answers the capsule names a toggle can turn on.
const scopes = ref<SettingsScope[]>([]);
const providerInstances = ref<string[]>([]);
const modules = ref<Array<{ name: string; state?: string; toggleable?: boolean }>>([]);

const toggleNames = computed<string[]>(() => toggleOptions(modules.value));

/** toggleChoicesFor is the union of the offered capsules and the ones
    this agent's file already enables (a host-side disable after the fact
    must stay visible, dimmed, or the next toggle save is a 400 the
    operator cannot explain). */
function toggleChoicesFor(name: string): string[] {
  return toggleChoices(toggleNames.value, editDraft[name]?.toggles ?? []);
}

function scopeOf(name: string): SettingsScope | undefined {
  return scopes.value.find((s) => s.name === name);
}

/** chipFor resolves one knob's authored-vs-inherited state for a row. */
function chipFor(name: string, key: string) {
  return chipOf(fieldChips(scopeOf(name)), key);
}

/** chipInherited / chipText / chipNote are the template's three reads of
    that state (a template call per fact keeps the markup free of optional
    chaining that a chip's own guard already answered). */
function chipInherited(name: string, key: string): boolean {
  return chipFor(name, key)?.inherited === true;
}

function chipText(name: string, key: string): string {
  return chipFor(name, key)?.chip ?? "";
}

function chipNote(name: string, key: string): string {
  return chipFor(name, key)?.note ?? "";
}

function skillOptionsFor(name: string): string[] {
  const p = profiles.value.find((a) => a.name === name);
  return p ? skillOptions(p) : [];
}

function sessionCountOf(name: string): number {
  const p = profiles.value.find((a) => a.name === name);
  return p?.session_count ?? 0;
}

// --- the delete lane (ruling 2) ---------------------------------------
const deleting = ref<string | null>(null);
const removing = ref<string | null>(null);
const deleteError = reactive<Record<string, string>>({});

/** agentSessionReceipt voices the delete's own count, the number the
    confirm dialog promised. */
function agentSessionReceipt(n?: number): string {
  if (!Number.isFinite(n) || (n ?? 0) <= 0) return "no sessions were stored";
  return n === 1 ? "1 session went with it" : `${n} sessions went with it`;
}

/** deleteHome DELETEs the whole home - definition, persona files, the
    agent's own skills and its session store. The confirm above named the
    session count; the receipt answers the same number. */
async function deleteHome(name: string): Promise<void> {
  removing.value = name;
  deleteError[name] = "";
  try {
    const res = await fetch(`/agents/${encodeURIComponent(name)}`, {
      method: "DELETE",
    });
    if (!res.ok) throw new Error(await res.text());
    const receipt = (await res.json()) as { sessions?: number };
    deleting.value = null;
    delete editDraft[name];
    delete editMsg[name];
    await load(true);
    notice.value = `deleted ${name}'s home - ${agentSessionReceipt(receipt.sessions)}`;
  } catch (e) {
    deleteError[name] = e instanceof Error ? e.message : String(e);
  } finally {
    removing.value = null;
  }
}

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
      // card t_81109e1c: the subtitle carries the roster's facts -
      // description, model, session count, last activity.
      description: agentRosterSubtitle(a),
      detail: mergedRowDetail(a, c),
      disabled: a.name === DefaultAgentName ? false : set.has(a.name),
      locked: a.name === DefaultAgentName,
      ...(agentStatusFor(c) ? { status: agentStatusFor(c) } : {}),
    };
  });
});

// --- card t_81109e1c: the row click opens the agent's chat ------------
// Ruling 1: a click opens a FULL interactive chat on the agent's
// session (the ordinary ChatTab, a different session id). Ruling 2: it
// lands on the agent's MOST RECENT session - the id the roster payload
// carries (last_session_id). Ruling 4 is the shell's: app.vue's
// openChatTab focuses a tab already bound to that session instead of
// minting a second one, so this panel never needs its own dedupe.
//
// Module UI imports no shell code (the Board precedent), so the open
// rides a window CustomEvent. THE LITERAL IS COPIED from
// composables/chat.ts (CHAT_SESSION_OPEN_EVENT) - the agents chat test
// pins both spellings identical.
const CHAT_SESSION_OPEN_EVENT = "omega:open-chat-session";

function sessionOf(name: string): string {
  const p = profiles.value.find((a) => a.name === name);
  return p?.last_session_id ?? "";
}

/** the roster's openable predicate: only a row with a session to bind
    is clickable - an agent with none keeps the plain name span rather
    than a button that would do nothing. */
function rowHasChat(row: RegistryRow): boolean {
  return sessionOf(row.name) !== "";
}

function openAgentChat(row: RegistryRow): void {
  const id = sessionOf(row.name);
  if (!id) return;
  window.dispatchEvent(new CustomEvent(CHAT_SESSION_OPEN_EVENT, { detail: id }));
}

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

/** loadSettings reads the two host definitions the edit form references:
    the agent scopes (the authored-vs-inherited state) and the named
    provider instances (the picker's options). A host that answers
    neither leaves both empty - the form then shows every knob as
    inherited, which is the layer's own floor. */
async function loadSettings(): Promise<void> {
  try {
    const res = await fetch("/settings");
    if (!res.ok) return;
    const body = (await res.json()) as {
      groups?: Array<{ key?: string; knobs?: Array<{ key?: string; value?: unknown }> }>;
      scopes?: SettingsScope[];
    };
    scopes.value = body.scopes ?? [];
    const providerGroup = (body.groups ?? []).find((g) => g.key === "provider");
    const instancesKnob = (providerGroup?.knobs ?? []).find(
      (k) => k.key === "provider.instances",
    );
    const rows = Array.isArray(instancesKnob?.value) ? instancesKnob.value : [];
    providerInstances.value = rows
      .map((r) => (r && typeof r === "object" ? (r as { name?: unknown }).name : null))
      .filter((n): n is string => typeof n === "string" && n !== "");
  } catch {
    /* a settings read that never landed leaves the inherited floor */
  }
}

/** loadModules reads the capsule names a toggle may turn on (the mounted,
    toggleable ones - the subset law starts at the picker). */
async function loadModules(): Promise<void> {
  try {
    const res = await fetch("/modules");
    if (!res.ok) {
      modules.value = [];
      return;
    }
    const body = (await res.json()) as {
      modules?: Array<{ name?: string; state?: string; toggleable?: boolean }>;
    };
    modules.value = (body.modules ?? []).filter(
      (m): m is { name: string; state?: string; toggleable?: boolean } =>
        typeof m.name === "string",
    );
  } catch {
    modules.value = [];
  }
}

/** load refetches the roster's four reads. `quiet` reloads WITHOUT the
    loading beat: a save, a reset or a delete must not swap the list out
    for a "loading…" line, because the swap unmounts RegistryList and the
    operator's expansion (with the form they were just using) collapses
    under them. */
async function load(quiet = false): Promise<void> {
  if (!quiet) loading.value = true;
  loadError.value = "";
  try {
    const res = await fetch("/agents");
    if (!res.ok) throw new Error(`GET /agents: ${res.status}`);
    profiles.value = ((await res.json()) ?? []) as OmegaAgent[];
    const agents = await fetchDisabledAgents();
    if (agents !== null) fold.value = agents;
    // the continuous roster rides the same load (card t_c7222095):
    // 404/501 is a host without the continuous module - no status, no
    // continuous kind; any other failure is the honest empty roster.
    await loadRoster();
    // the edit form's definitions (ruling 7's chips + item 4's picker)
    await loadSettings();
    await loadModules();
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

// expand: the edit draft seeds from the layer's EFFECTIVE values (the
// scope payload) with the profile's own facts as the floor; a rostered
// agent also refreshes its canonical chat + evidence - the chat fetch
// marks the canonical session read server-side, the same clear the
// ContinuousPanel detail owned, so the unread badge honest-drops.
// Session loads are GONE (card t_2d8deb8c: the tab does not render
// sessions).
function onExpand(row: RegistryRow): void {
  if (row.name !== DefaultAgentName) seedEdit(row.name);
  const name = row.name;
  if (!rosterOf(name)) return;
  void loadContinuousDetail(name);
}

/** seedEdit fills one draft from the effective layer: an omitted knob
    shows main's value (and its chip), an authored one shows the agent's
    own. The draft is DIFFED against the same seed on save, so an
    untouched inherited knob is never written down as an override. */
function seedEdit(name: string): void {
  const p = profiles.value.find((a) => a.name === name);
  if (!p) return;
  const chips = fieldChips(scopeOf(name));
  const effective = (key: string): string => chipOf(chips, key)?.value ?? "";
  const inherit = {} as Record<AgentInheritKind, boolean>;
  for (const kind of AGENT_INHERIT_KINDS) {
    inherit[kind] = (p.inherits ?? []).includes(kind);
  }
  editDraft[name] = {
    description: p.description ?? "",
    system_prompt: effective("system_prompt"),
    provider_instance: effective("provider.instance") || "default",
    model_name: effective("provider.model_name"),
    max_turns: effective("max_turns"),
    toggles: [...(p.toggles ?? [])],
    skills_allow: [...(p.skills_allow ?? [])],
    inherit,
  };
  editMsg[name] = "";
  deleteError[name] = "";
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

/** saveAgentEdit PUTs the DIFF the draft carries through
    PUT /agents/{name}: only knobs that CHANGED against their effective
    seed are written, so an untouched inherited knob keeps inheriting
    (the layer law: an absent key inherits, an explicitly empty one does
    not). A max turns box cleared while authored is the layer's reset
    verb, not a zero. */
async function saveAgentEdit(name: string): Promise<void> {
  const d = editDraft[name];
  const p = profiles.value.find((a) => a.name === name);
  if (!d || !p) return;
  const body = editRequest(d, p, fieldChips(scopeOf(name)));
  if (!body) {
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
    await load(true);
    seedEdit(name);
    editMsg[name] = "saved";
  } catch (e) {
    editMsg[name] = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = null;
  }
}

/** resetField is the layer's own reset verb: DELETE the knob so the agent
    falls back to main (writing main's current value instead would leave a
    permanent phantom override under the chip's feet). */
async function resetField(name: string, key: string): Promise<void> {
  editMsg[name] = "";
  try {
    const res = await fetch(`/agents/${encodeURIComponent(name)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ unset: [key] }),
    });
    if (!res.ok) throw new Error(await res.text());
    await load(true);
    seedEdit(name);
    editMsg[name] = `${key} inherits from main again`;
  } catch (e) {
    editMsg[name] = e instanceof Error ? e.message : String(e);
  }
}

onMounted(load);

defineExpose({ load, profiles, fold, rows, roster, rowStatusOf, rowStatusVoice });
</script>

<style scoped>
/* card t_81109e1c: the main-agent row badge - a quiet chip on line 1,
   beside the name (the locked pill marks the same row when the toggle
   seam exists; the badge survives a host that serves none). */
.agents-main-badge {
  align-self: center;
  background: var(--z2, rgba(128, 128, 128, 0.12));
  border-radius: 4px;
  color: var(--text-dim, #8a8a92);
  font-size: 9px;
  letter-spacing: 0.04em;
  padding: 1px 5px;
  text-transform: lowercase;
}
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
/* card t_df015e66: the ONE create door. One "+" in the band, one form -
   the kind select decides which endpoint follows. */
.agents-actions {
  display: flex;
  gap: 4px;
}
.agents-create {
  display: flex;
  flex-direction: column;
  gap: 5px;
  margin: 4px 0 8px;
  max-width: 420px;
}
.agents-create-name,
.agents-create-desc {
  background: var(--z2, rgba(128, 128, 128, 0.12));
  border: 1px solid var(--z3, rgba(128, 128, 128, 0.3));
  border-radius: 6px;
  color: var(--text);
  font: inherit;
  min-width: 0;
  padding: 4px 8px;
}
.agents-create-note {
  color: var(--text-dim, #8a8a92);
  font-size: 10px;
  line-height: 1.4;
  margin: 0;
}
.agents-create-go {
  align-self: flex-start;
  background: var(--z2, rgba(128, 128, 128, 0.12));
  border: 1px solid var(--z3, rgba(128, 128, 128, 0.3));
  border-radius: 6px;
  color: var(--text);
  cursor: pointer;
  font: inherit;
  padding: 4px 10px;
}
/* card t_2d8deb8c: the edit affordance - the layer's field set,
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
  gap: 6px;
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
  align-items: center;
  color: var(--text-dim, #8a8a92);
  display: flex;
  font-size: 10px;
  gap: 6px;
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
/* card t_df015e66 ruling 7: the inherited chip + its one-line source.
   The chip is the state (the layer omits the key); the note names the
   link that supplies the value. */
.agents-inherit-chip {
  background: var(--z2, rgba(128, 128, 128, 0.12));
  border: 1px solid var(--z3, rgba(128, 128, 128, 0.3));
  border-radius: 999px;
  color: var(--text-dim, #8a8a92);
  font-size: 9px;
  letter-spacing: 0.02em;
  padding: 0 6px;
  text-transform: lowercase;
  white-space: nowrap;
}
.agents-inherit-note {
  color: var(--text-dim, #8a8a92);
  font-size: 10px;
  line-height: 1.4;
}
.agents-reset {
  align-self: flex-start;
  background: none;
  border: none;
  color: var(--text-dim, #8a8a92);
  cursor: pointer;
  font: inherit;
  font-size: 10px;
  padding: 0;
  text-decoration: underline dotted;
}
.agents-reset:hover {
  color: var(--text);
}
/* the toggle + skills checkbox sets (the always-off law's surface).
   agents-check-off = a capsule this agent enables that the HOST has
   since switched off: shown dim with a reason, so unchecking it is a
   deliberate act (the subset law would refuse an invisible entry). */
.agents-check-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.agents-check {
  align-items: center;
  display: flex;
  font-size: 11px;
  gap: 6px;
}
.agents-check-off {
  color: var(--text-dim, #8a8a92);
}
/* ruling 2: the delete lane. Destructive voice, two beats: arm, then
   the confirm that names what goes. */
.agents-danger {
  border-top: 1px solid var(--z2, rgba(128, 128, 128, 0.12));
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin: 8px 0 4px 10px;
  max-width: 420px;
  padding-top: 6px;
}
.agents-delete {
  align-self: flex-start;
  background: none;
  border: 1px solid var(--z3, rgba(128, 128, 128, 0.3));
  border-radius: 6px;
  color: var(--err, #f66);
  cursor: pointer;
  font: inherit;
  font-size: 10px;
  padding: 3px 8px;
}
.agents-delete-confirm {
  color: var(--err, #f66);
  font-size: 10px;
  line-height: 1.4;
  margin: 0;
}
.agents-delete-go {
  display: flex;
  gap: 6px;
}
.agents-delete-go-btn {
  background: var(--err, #f66);
  border: 1px solid var(--err, #f66);
  border-radius: 6px;
  color: var(--bg, #111);
  cursor: pointer;
  font: inherit;
  font-size: 10px;
  padding: 3px 10px;
}
.agents-cancel {
  background: none;
  border: 1px solid var(--z3, rgba(128, 128, 128, 0.3));
  border-radius: 6px;
  color: var(--text-dim, #8a8a92);
  cursor: pointer;
  font: inherit;
  font-size: 10px;
  padding: 3px 10px;
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
