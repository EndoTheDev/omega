// omega webui: unit tests for the answerable gate question (card
// t_63204dfd). Two surfaces over the SAME verdict endpoint:
// - the chat card (GateCard.vue at the escalated tool call row):
//   approve / deny / follow-up prompt;
// - the notification card (NotificationsPanel.vue): approve / deny
//   only, body click still jumps to the session.
// Layers, matching the house patterns:
// - the pure helpers (omega.ts): approveDenyOptions, gateAnswerBody,
//   isSettledRace, toolCallCommand, bindGateQuestions;
// - the fetch wrapper: postVerdict carries the message field;
// - source pins: the components' wiring (components are not importable
//   in node).
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  approveDenyOptions,
  bindGateQuestions,
  gateAnswerBody,
  isSettledRace,
  postVerdict,
  toolCallCommand,
  type PendingVerdictCard,
  type ToolItem,
  type TranscriptItem,
  type VerdictItem,
} from "../app/composables/omega.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, ".."); // webui/ (webui/test -> webui -> webui)

function read(...parts: string[]): string {
  return readFileSync(join(modules, ...parts), "utf8");
}

// gateCardRow is the wire shape the /verdicts listing serves for a gate
// escalation: id + session + the approve/deny pair.
function gateCardRow(id = "c1", sessionId = "s1", title = "rm -rf build"): PendingVerdictCard {
  return {
    id,
    session_id: sessionId,
    title,
    options: [
      { label: "approve", description: "run the command" },
      { label: "deny", description: "refuse; the agent is told the command did not run" },
    ],
    allow_text: true,
    expires_at: 1893456000000,
  };
}

// runningShell is a rendered running shell.run call row (the freeze).
function runningShell(id: string, command: string): ToolItem {
  return {
    kind: "tool",
    name: "shell.run",
    status: "running",
    output: JSON.stringify({ command }),
  };
}

// --- the pair contract ---

test("approveDenyOptions finds the approve/deny pair, case-insensitive, 1-based", () => {
  const ix = approveDenyOptions(gateCardRow("c1", "s1").options);
  assert.deepEqual(ix, { approve: 1, deny: 2 });
  const reordered = approveDenyOptions([{ label: "Deny" }, { label: "Approve" }]);
  assert.deepEqual(reordered, { approve: 2, deny: 1 }, "the labels pair positions, not slots");
});

test("approveDenyOptions: null when the card is not a gate question", () => {
  // a verdict tool's custom question has neither label (or only one)
  assert.equal(approveDenyOptions([{ label: "Ship" }, { label: "Hold" }]), null);
  assert.equal(approveDenyOptions([{ label: "approve" }, { label: "Stop" }]), null);
  assert.equal(approveDenyOptions(undefined), null);
  assert.equal(approveDenyOptions([]), null);
});

// --- the answer bodies ---

test("gateAnswerBody: approve runs the command, deny skips it", () => {
  const card = gateCardRow();
  assert.deepEqual(gateAnswerBody(card, "approve"), { card_id: "c1", option_index: 1 });
  assert.deepEqual(gateAnswerBody(card, "deny"), { card_id: "c1", option_index: 2 });
});

test("gateAnswerBody: followup denies AND rides the typed guidance as message", () => {
  const card = gateCardRow("c9");
  assert.deepEqual(gateAnswerBody(card, "followup", "copy the file instead"), {
    card_id: "c9",
    option_index: 2,
    message: "copy the file instead",
  });
});

test("gateAnswerBody: an empty / blank followup input falls back to the plain deny", () => {
  const card = gateCardRow();
  assert.deepEqual(gateAnswerBody(card, "followup", ""), { card_id: "c1", option_index: 2 });
  assert.deepEqual(gateAnswerBody(card, "followup", "   \t "), { card_id: "c1", option_index: 2 });
  // the guidance is trimmed, never raw-padded
  const body = gateAnswerBody(card, "followup", "  use cp instead ");
  assert.equal((body as { message?: string }).message, "use cp instead");
});

test("gateAnswerBody: the pair contract reads the card's own options", () => {
  const card = gateCardRow();
  card.options = [{ label: "Approve" }, { label: "Deny" }];
  assert.deepEqual(gateAnswerBody(card, "approve"), { card_id: "c1", option_index: 1 });
});

// --- the race ---

test("isSettledRace: the settled vocabulary collapses, other failures do not", () => {
  for (const why of [
    "verdict card c1 already answered",
    "verdict card c1 already skipped",
    "verdict card c1 already expired",
  ]) {
    assert.equal(isSettledRace(why), true, why);
  }
  for (const why of ["verdict: 404", "network failure", "no pending verdict card c1"]) {
    assert.equal(isSettledRace(why), false, why);
  }
});

test("postVerdict carries the follow-up message to the same endpoint", async () => {
  const seen: { url: string; body: string }[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL, init: { method?: string; body?: string }) => {
    seen.push({ url: String(url), body: String(init?.body ?? "") });
    return Promise.resolve(new Response(JSON.stringify({ card_id: "c1", verdict: "answer", option: "deny" }), {
      status: 200, headers: { "content-type": "application/json" },
    }));
  }) as typeof globalThis.fetch;
  try {
    await postVerdict("s1", { card_id: "c1", option_index: 2, message: "use cp instead" });
    assert.equal(seen[0].url, "/sessions/s1/verdicts");
    assert.deepEqual(JSON.parse(seen[0].body), { card_id: "c1", option_index: 2, message: "use cp instead" });
  } finally {
    globalThis.fetch = realFetch;
  }
});

// --- the chat binding ---

test("bindGateQuestions: no session or no cards binds nothing", () => {
  const items: TranscriptItem[] = [runningShell("tc1", "rm -rf build")];
  assert.deepEqual(bindGateQuestions(items, [gateCardRow()], null), { assigned: [], standalone: [] });
  assert.deepEqual(bindGateQuestions(items, [], "s1"), { assigned: [], standalone: [] });
  // a card for another session belongs to that session's surface
  assert.deepEqual(bindGateQuestions(items, [gateCardRow("c1", "s2")], "s1"), {
    assigned: [],
    standalone: [],
  });
});

test("bindGateQuestions: the gate card rides its running shell.run row by command match", () => {
  const shell = runningShell("tc1", "rm -rf build");
  const items: TranscriptItem[] = [shell];
  const bound = bindGateQuestions(items, [gateCardRow()], "s1");
  assert.equal(bound.assigned.length, 1);
  assert.equal(bound.assigned[0].item, shell);
  assert.equal(bound.assigned[0].card.id, "c1");
  assert.deepEqual(bound.standalone, []);
});

test("bindGateQuestions: a settled row is no ride, the card renders standalone", () => {
  const shell = runningShell("tc1", "rm -rf build");
  shell.status = "error";
  shell.result = "approval denied";
  const bound = bindGateQuestions([shell], [gateCardRow()], "s1");
  assert.deepEqual(bound.assigned, []);
  assert.deepEqual(bound.standalone.map((c) => c.id), ["c1"]);
});

test("bindGateQuestions: a pending verdict-tool card owns the pause, nothing binds over it", () => {
  const shell = runningShell("tc1", "rm -rf build");
  const verdictPending: VerdictItem = {
    kind: "verdict",
    title: "Ship?",
    options: [],
    multi: false,
    allowText: false,
    status: "pending",
  };
  assert.deepEqual(bindGateQuestions([shell, verdictPending], [gateCardRow()], "s1"), {
    assigned: [],
    standalone: [],
  });
  // the verdict card settled: the gate question binds again
  verdictPending.status = "settled";
  const after = bindGateQuestions([shell, verdictPending], [gateCardRow()], "s1");
  assert.equal(after.assigned.length, 1);
});

test("bindGateQuestions: the byte-truncated title still matches its command", () => {
  // the backend truncates the card title to 120 bytes with a ... tail
  const command = "docker run --rm -v /data:/data alpine:latest sh -c 'apk add --no-cache rsync && rsync -a /data/ /backup/'";
  const shell = runningShell("tc1", command);
  const bound = bindGateQuestions([shell], [gateCardRow("c1", "s1", command.slice(0, 117) + "\u2026")], "s1");
  assert.equal(bound.assigned.length, 1);
});

test("bindGateQuestions: two escalations bind two rows, command match wins first", () => {
  const a = runningShell("tc1", "rm a.txt");
  const b = runningShell("tc2", "rm b.txt");
  const bound = bindGateQuestions([a, b], [gateCardRow("c1", "s1", "rm b.txt")], "s1");
  assert.equal(bound.assigned.length, 1);
  assert.equal(bound.assigned[0].item, b, "the command match beats row order");
  assert.equal(bound.assigned[0].card.id, "c1");
});

test("bindGateQuestions: a non-shell row is never a ride", () => {
  const read: ToolItem = { kind: "tool", name: "read_file", status: "running" };
  const bound = bindGateQuestions([read], [gateCardRow()], "s1");
  assert.deepEqual(bound.assigned, []);
  assert.deepEqual(bound.standalone.map((c) => c.id), ["c1"]);
});

test("toolCallCommand reads the command argument, junk-safe", () => {
  assert.equal(toolCallCommand({ kind: "tool", name: "shell.run", status: "running", output: JSON.stringify({ command: "ls" }) }), "ls");
  assert.equal(toolCallCommand({ kind: "tool", name: "shell.run", status: "running", output: "not json" }), "");
  assert.equal(toolCallCommand({ kind: "tool", name: "shell.run", status: "running" }), "");
});

// --- source pins (components are not importable in node) ---

test("GateCard renders the three options and every resolution path", () => {
  const vue = read("app/components/chat/GateCard.vue");
  assert.match(vue, /"approve"/, "the first button runs the command");
  assert.match(vue, /"deny"/, "the second skips it");
  assert.match(vue, /follow-up prompt/, "the third opens the typed surface");
  assert.match(vue, /mode = 'followup'/, "the follow-up button swaps the rows for the input");
  assert.match(vue, /verdict-input/, "the follow-up surface is the text input row");
  assert.match(vue, /sendFollowup/, "the input sends through the followup body");
  assert.match(vue, /void act\("deny"\)/, "an empty send falls back to the plain deny");
  assert.match(vue, /gateAnswerBody\(/, "the POST body is the shared pure helper");
  assert.match(vue, /isSettledRace\(msg\)/, "a lost race collapses, it does not error");
  assert.match(vue, /postVerdict\(props\.sessionId/, "answers post to the session the chat tab owns");
  assert.match(vue, /verdict-outcome/, "the collapsed card renders the resolved state");
});

test("ChatItem rides the gate card on the tool call row, ChatTab binds it", () => {
  const chatItem = read("app/components/ChatItem.vue");
  assert.match(chatItem, /<GateCard/, "the pending gate question rides the tool call row");
  assert.match(chatItem, /item\.kind === 'tool' && gateCard/, "only a tool row hosts the card");
  const chatTab = read("app/components/ChatTab.vue");
  assert.match(chatTab, /bindGateQuestions/, "ChatTab binds pending cards onto the transcript");
  assert.match(chatTab, /:gate-card="gateCardForItem\(item\)"/, "the binding arrives per rendered row");
  assert.match(chatTab, /sessionGateBinding\.standalone/, "cards with no running row render standalone at the tail");
  assert.match(chatTab, /refreshNotifications/, "the chat surface refreshes the shared pending-card truth");
});

test("the notification surface answers approve/deny and keeps the jump", () => {
  const panel = read("../harness/notifications/ui/NotificationsPanel.vue");
  assert.match(panel, /@click\.stop="answerQuestion\(p, 'approve'\)"/, "approve answers in place, it does not navigate");
  assert.match(panel, /@click\.stop="answerQuestion\(p, 'deny'\)"/, "deny answers in place, it does not navigate");
  assert.match(panel, /approveDenyOptions/, "buttons render only on the pair contract");
  assert.match(panel, /openVerdict\(p\)/, "the body click still jumps to the session");
  assert.match(panel, /isSettledRace/, "a late click on a resolved question collapses, no error");
  assert.match(panel, /notif-verdict resolved/, "the resolved state swaps in");
});

test("the chat gate card is the notification card's sibling, same endpoint same verdict", () => {
  // both surfaces answer the SAME pending question id through the SAME
  // endpoint; the chat card binds to the row, the notification row
  // carries the id in its payload
  const wire = read("../agent/api/server_verdicts.go");
  assert.match(wire, /SessionID\s+string\s+`json:"session_id"`/, "the pending listing payload carries the session id");
  assert.match(wire, /Message\s+string\s+`json:"message,omitempty"`/, "the verdict endpoint accepts the follow-up message");
});