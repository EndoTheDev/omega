// omega webui: the Agents tab AS A CHAT DOOR
// by ).
//
// The tab is a roster of agent homes; a row click opens a FULL chat tab
// bound to that agent's CANONICAL chat - one stable conversation, minted
// on the spot when the home holds none - focuses an already-open tab
// instead of minting a duplicate, and the main agent rides the roster
// like every other home.
//
// Two layers, the house pattern:
// agentsRows.ts (PURE): the roster subtitle's facts (model, session
//   count, last activity), the preview line, the main-agent marker;
// source pins: the module->shell seam (AgentsPanel POSTs the canonical
//   open, dispatches the copied event literal, app.vue listens and runs
//   THE chat opener, whose focus-before-mint order already lives in
//   openChatTab).
// The payload half (GET /agents carries canonical_session_id +
// last_report, main included) is pinned on the Go side:
// harness/continuous/agentsrest_test.go TestAgentsRosterCanonicalChat.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  agentActivityText,
  agentIsMain,
  agentPreview,
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
const rowsSrc = readFileSync(
  join(root, "harness", "continuous", "ui", "agentsRows.ts"),
  "utf8",
);
const listSrc = readFileSync(
  join(root, "webui", "app", "components", "RegistryList.vue"),
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
  canonical_session_id: "scout-canonical",
  last_report: "patrol complete, 0 anomalies",
  last_report_at: "2026-11-10T11:00:00Z",
};

// the roster's facts --------------------------------------------------

test("the roster subtitle carries description, model, session count and last activity", () => {
  const now = new Date("2026-11-10T12:00:00Z");
  assert.equal(
    agentRosterSubtitle(scout, now),
    "probe duty · qwen3 · 3 sessions · last 2h ago",
  );
  // the count is always a fact; a model-less agent never renders a phantom model.
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
  assert.equal(agentActivityText("2026-09-01T12:00:00Z", now), "2026-09-01");
});

// the main agent is a row like any other -------------

test("the main agent is included and marked, not filtered out", () => {
  assert.equal(DefaultAgentName, "default");
  assert.equal(agentIsMain({ name: "default" }), true);
  assert.equal(agentIsMain({ name: "scout" }), false);
  // the panel renders every profile it gets, main first in the payload.
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

// + 4: the click opens THE canonical chat ---------------

test("a roster row click opens the agent's canonical chat, minting when the home holds none", () => {
  assert.equal(CHAT_SESSION_OPEN_EVENT, "omega:open-chat-session");
  // the copied literal (module UI imports no shell code): the panel's
  // spelling must match the shell's constant exactly
  assert.match(
    panelSrc,
    /CHAT_SESSION_OPEN_EVENT = "omega:open-chat-session"/,
    "the panel copies the literal",
  );
  assert.match(chatSrc, /CHAT_SESSION_OPEN_EVENT = "omega:open-chat-session"/);
  assert.match(panelSrc, /@open="openAgentChat"/, "the row open is wired");
  // EVERY row is clickable: a session-less agent's click mints its canonical, so no.
  assert.match(
    panelSrc,
    /:openable="true"/,
    "every row is openable - the mint covers the session-less agent",
  );
  assert.ok(
    !/rowHasChat/.test(panelSrc),
    "the old session-existence predicate is gone - it made fresh agents dead spans",
  );
  // the mint is the write: POST /agents/{name}/canonical
  assert.match(
    panelSrc,
    /fetch\(`\/agents\/\$\{encodeURIComponent\(name\)\}\/canonical`, \{\s*method: "POST",/,
    "the click asks the capsule for the canonical chat",
  );
  // .and the dispatch carries the id the answer named, never a guess
  assert.match(
    panelSrc,
    /const id = sess\.id \?\? "";/,
    "the id comes from the mint/open answer",
  );
  assert.match(
    panelSrc,
    /new CustomEvent\(CHAT_SESSION_OPEN_EVENT, \{ detail: id \}\)/,
    "the dispatch carries the session id",
  );
  // the most-recent binding is dead on BOTH halves of the wire
  assert.ok(
    !/last_session_id/.test(panelSrc) && !/last_session_id/.test(rowsSrc),
    "the most-recent session id is gone from the panel and its row type",
  );
  assert.match(
    rowsSrc,
    /canonical_session_id\?: string;/,
    "the row type carries the canonical id",
  );
  assert.match(
    panelSrc,
    /profiles\.value\.find\(\(a\) => canonicalOf\(a\.name\) === id\)/,
    "the active row matches the canonical chat the shell is showing",
  );
});

test("the preview line: one collapsed line of the agent's newest reply, with its age", () => {
  const now = new Date("2026-11-10T12:00:00Z");
  assert.deepEqual(agentPreview(scout, now), {
    text: "patrol complete, 0 anomalies",
    at: "1h ago",
  });
  // whitespace collapses: the row is ONE line, never a multiline report
  const noisy: OmegaAgent = {
    ...scout,
    last_report: "line one\n\nline   two\t end",
  };
  assert.equal(agentPreview(noisy, now).text, "line one line two end");
  // no reply yet = no preview line (never a fabricated placeholder)
  const silent: OmegaAgent = { ...scout, last_report: "" };
  assert.deepEqual(agentPreview(silent, now), { text: "", at: "" });
  const noReport: OmegaAgent = { ...scout, last_report: undefined };
  assert.deepEqual(agentPreview(noReport, now), { text: "", at: "" });
  assert.match(panelSrc, /const pv = agentPreview\(a\);/, "the row builds the preview");
  assert.match(
    panelSrc,
    /\.\.\.\(pv\.text \? \{ preview: pv\.text, previewAt: pv\.at \} : \{\}\)/,
    "an empty preview omits the line entirely",
  );
  assert.match(
    listSrc,
    /v-if="r\.preview"[\s\S]{0,200}?class="registry-preview"/,
    "the preview text renders",
  );
  assert.match(listSrc, /class="registry-preview-at"/, "the age renders beside it");
  assert.match(listSrc, /\.registry-preview \{[\s\S]{0,160}?text-overflow: ellipsis;/, "one line, ellipsized");
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
  // the dock's lookup is what makes true for a session opened by ANY path.
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

// full separation -------------------------------------

test("an agent's work sessions are inspected from its expanded row, not the sessions sidebar", () => {
  // the panel loads the agent's OWN session rows when the row opens
  assert.match(
    panelSrc,
    /async function loadAgentSessions\(name: string\): Promise<void> \{/,
    "the expansion loads the agent's sessions",
  );
  assert.match(
    panelSrc,
    /fetch\(`\/agents\/\$\{encodeURIComponent\(name\)\}\/sessions`\)/,
    "the per-agent sessions route is the read",
  );
  assert.match(panelSrc, /void loadAgentSessions\(name\);/, "the expand fires the read");
  assert.match(panelSrc, /class="agents-session-list"/, "the list renders");
  assert.match(panelSrc, /@click="openSession\(s\.id\)"/, "a session row opens the chat");
  assert.match(
    panelSrc,
    /new CustomEvent\(CHAT_SESSION_OPEN_EVENT, \{ detail: id \}\)/,
    "opening a work session rides the same seam",
  );
  // main's row offers none: its sessions ARE the human's own list
  assert.match(
    panelSrc,
    /if \(name === DefaultAgentName\) \{\s*sessionsByAgent\[name\] = \[\];\s*return;/,
    "the main row lists no work sessions",
  );
  assert.match(
    panelSrc,
    /FULL SEPARATION[\s\S]{0,200}?not in the sessions sidebar/,
    "the separation is stated where the section lives",
  );
});

// /new folds an agent chat, never forks it -------------

test("/new in a machinery-owned chat folds it; the human's own chat still forks", () => {
  const at = appSrc.indexOf('case "/new": {');
  assert.ok(at > -1, "the /new branch exists");
  const body = appSrc.slice(at, appSrc.indexOf('case "/sessions": {', at));
  // the fold is a POST on the focused session
  assert.match(
    body,
    /fetch\(`\/sessions\/\$\{encodeURIComponent\(id\)\}\/new`, \{\s*method: "POST",/,
    "the branch asks the server to fold the focused conversation",
  );
  assert.match(body, /body\.kind === "compacted"/, "a fold keeps the tab where it is");
  assert.match(body, /return true;/, "a fold ends the command - no new tab");
  const foldEnd = body.indexOf("openChatTab(null)");
  assert.ok(foldEnd > -1, "the fork fallback survives");
  assert.ok(
    body.indexOf('body.kind === "compacted"') < foldEnd,
    "the fold is decided BEFORE the fork - a compacted chat never opens a draft",
  );
});
