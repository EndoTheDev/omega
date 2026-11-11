// omega webui: the Agents tab AS A CHAT DOOR (card t_81109e1c).
//
// The tab is a roster of agent homes; a row click opens a FULL chat tab
// bound to that agent's MOST RECENT session, focuses an already-open tab
// instead of minting a duplicate, and the main agent rides the roster
// like every other home.
//
// Two layers, the house pattern:
// - agentsRows.ts (PURE): the roster subtitle's facts (model, session
//   count, last activity), the main-agent marker;
// - source pins: the module->shell seam (AgentsPanel dispatches the
//   copied event literal, app.vue listens and runs THE chat opener,
//   whose focus-before-mint order already lives in openChatTab).
// The payload half (GET /agents carries last_session_id, main included)
// is pinned on the Go side: harness/continuous/agentsrest_test.go
// TestAgentsRosterLastSessionID.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  agentActivityText,
  agentIsMain,
  agentRosterSubtitle,
  agentSessionCountText,
  DefaultAgentName,
  type OmegaAgent,
} from "../../harness/continuous/ui/agentsRows.ts";
import { CHAT_SESSION_OPEN_EVENT } from "../app/composables/chat.ts";
import { firstChatTab, makePane, makeSplit } from "../app/composables/dock.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const panelSrc = readFileSync(
  join(root, "harness", "continuous", "ui", "AgentsPanel.vue"),
  "utf8",
);
const appSrc = readFileSync(join(root, "webui", "app", "app.vue"), "utf8");
const chatSrc = readFileSync(
  join(root, "webui", "app", "composables", "chat.ts"),
  "utf8",
);

const scout: OmegaAgent = {
  name: "scout",
  dir: "/home/x/.omega/agents/scout",
  description: "probe duty",
  provider_type: "ollama",
  model_name: "qwen3",
  skill_count: 1,
  skills: ["probe"],
  memory_size: 0,
  session_count: 3,
  last_activity: "2026-11-10T10:00:00Z",
  last_session_id: "sess-new",
};

// --- the roster's facts (ruling 2's "name, model, session count, last
// activity" row) -----------------------------------------------------

test("the roster subtitle carries description, model, session count and last activity", () => {
  const now = new Date("2026-11-10T12:00:00Z");
  assert.equal(
    agentRosterSubtitle(scout, now),
    "probe duty · qwen3 · 3 sessions · last 2h ago",
  );
  // the count is always a fact; a model-less agent never renders a
  // phantom model token (the agentModelText law already owns that voice)
  const newborn: OmegaAgent = {
    name: "newborn",
    dir: "/home/x/.omega/agents/newborn",
    skill_count: 0,
    memory_size: 0,
    session_count: 0,
  };
  assert.equal(agentRosterSubtitle(newborn, now), "no sessions");
});

test("session count voice: zero is honest, never 0 sessions", () => {
  assert.equal(agentSessionCountText(0), "no sessions");
  assert.equal(agentSessionCountText(-1), "no sessions");
  assert.equal(agentSessionCountText(Number.NaN), "no sessions");
  assert.equal(agentSessionCountText(1), "1 session");
  assert.equal(agentSessionCountText(7), "7 sessions");
});

test("last activity voice: relative age, honest absence, no negative ages", () => {
  const now = new Date("2026-11-10T12:00:00Z");
  assert.equal(agentActivityText(undefined, now), "");
  assert.equal(agentActivityText("not-a-time", now), "");
  assert.equal(agentActivityText("2026-11-10T11:59:30Z", now), "just now");
  // clock skew (a future stamp) reads "just now", never -3m ago
  assert.equal(agentActivityText("2026-11-10T12:03:00Z", now), "just now");
  assert.equal(agentActivityText("2026-11-10T11:35:00Z", now), "25m ago");
  assert.equal(agentActivityText("2026-11-10T09:00:00Z", now), "3h ago");
  assert.equal(agentActivityText("2026-11-07T12:00:00Z", now), "3d ago");
  // past the month mark the raw date is more useful than "47d ago"
  assert.equal(agentActivityText("2026-09-01T12:00:00Z", now), "2026-09-01");
});

// --- ruling 3: the main agent is a row like any other ----------------

test("the main agent is included and marked, not filtered out", () => {
  assert.equal(DefaultAgentName, "default");
  assert.equal(agentIsMain({ name: "default" }), true);
  assert.equal(agentIsMain({ name: "scout" }), false);
  // the panel renders every profile it gets, main first in the payload
  // (LoadProfiles starts with the home root), and marks that row
  assert.match(panelSrc, /profiles\.value\.map\(/, "no filter drops the main row");
  assert.ok(
    !/profiles\.value\.filter\(/.test(panelSrc),
    "the roster is the whole profile list - no row is filtered out",
  );
  assert.match(
    panelSrc,
    /v-if="row\.name === DefaultAgentName"[\s\S]{0,160}?class="agents-main-badge"/,
    "the main row carries the badge",
  );
  assert.match(panelSrc, /\.agents-main-badge \{/, "the badge has a rule");
});

// --- ruling 1 + 4: the click opens THE chat on the latest session ----

test("a roster row click dispatches the session-open event with the agent's latest session", () => {
  assert.equal(CHAT_SESSION_OPEN_EVENT, "omega:open-chat-session");
  // the copied literal (module UI imports no shell code): the panel's
  // spelling must match the shell's constant exactly
  assert.match(
    panelSrc,
    /CHAT_SESSION_OPEN_EVENT = "omega:open-chat-session"/,
    "the panel copies the literal",
  );
  assert.match(chatSrc, /CHAT_SESSION_OPEN_EVENT = "omega:open-chat-session"/);
  // the row name is the opener, and the detail IS the session id
  assert.match(panelSrc, /@open="openAgentChat"/, "the row open is wired");
  assert.match(
    panelSrc,
    /new CustomEvent\(CHAT_SESSION_OPEN_EVENT, \{ detail: id \}\)/,
    "the dispatch carries the session id",
  );
  assert.match(
    panelSrc,
    /const id = sessionOf\(row\.name\);/,
    "the id is resolved from the row's own agent",
  );
  // only a row with a session to bind is clickable - no dead button
  assert.match(panelSrc, /:openable="rowHasChat"/);
  assert.match(
    panelSrc,
    /function rowHasChat\(row: RegistryRow\): boolean \{\s*return sessionOf\(row\.name\) !== "";/,
  );
});

test("app.vue listens on the seam and runs the ordinary chat opener", () => {
  assert.match(
    appSrc,
    /window\.addEventListener\(CHAT_SESSION_OPEN_EVENT, onChatSessionOpenEvent\)/,
  );
  assert.match(
    appSrc,
    /if \(typeof id === "string" && id !== ""\) openChatTab\(id\);/,
    "a non-empty id goes straight to openChatTab",
  );
  // the removal is hygiene for hot-reloading harnesses
  assert.match(
    appSrc,
    /window\.removeEventListener\(CHAT_SESSION_OPEN_EVENT, onChatSessionOpenEvent\)/,
  );
});

test("focus-not-duplicate: openChatTab checks for a bound tab BEFORE it mints", () => {
  const fn = appSrc.slice(appSrc.indexOf("function openChatTab("));
  const body = fn.slice(0, fn.indexOf("\n  }\n"));
  const check = body.indexOf("dock.firstChatTab(sessionId)");
  const focus = body.indexOf("dock.focus(found.paneId, found.index)");
  const mint = body.indexOf("dock.openTab(pane,");
  assert.ok(check > -1 && focus > -1 && mint > -1, "the opener's three beats exist");
  assert.ok(check < mint, "the bound-tab check precedes the mint");
  assert.ok(focus < mint, "the focus happens before the mint");
});

test("a chat tab bound to the session is found wherever it lives (one tab per session)", () => {
  // the dock's lookup is what makes ruling 4 true for a session opened
  // by ANY path (sidebar, notification, agents row): the second open of
  // the same session id resolves to the SAME pane/index.
  const left = makePane("chat", null, "left");
  left.tabs = [{ component: "chat", sessionRef: "sess-new", key: "k1" }];
  const tree = makeSplit("row", [left, makePane("agents")]);
  const found = firstChatTab(tree, "sess-new");
  assert.ok(found, "the bound tab was found in its non-main pane");
  assert.equal(found?.paneId, left.id);
  assert.equal(found?.tab.key, "k1");
  assert.equal(firstChatTab(tree, "sess-other"), null, "an unknown session mints");
});

test("an agent-bound tab is named by its session, never a generic chat chip", () => {
  // the sidebar list carries DEFAULT-agent sessions only, so the title
  // resolver finds nothing for a roster-opened agent session - the id
  // prefix names the tab (two agent chats must not look alike)
  assert.match(
    appSrc,
    /return s\?\.label \|\| `\$\{tab\.sessionRef\.slice\(0, 8\)\}\.\.\.`;/,
  );
  assert.ok(
    !/return s\?\.label \|\| "chat";/.test(appSrc),
    "the generic fallback is gone",
  );
});
