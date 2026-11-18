// omega webui: unit tests for verdict cards v1.1 (cards,
// ). Three layers, matching the house patterns:
// the pure card helpers: args parse, result parse, wire restore
//
// the fetch wrappers: fetchVerdicts / postVerdict URL + body contract
//   under fetch interception, incl. the 409 race reason surfacing;
// source pins: ChatItem routes the verdict item to VerdictCard and the
//   bell consumes fetchVerdicts (source checks, components are not
//   importable in node).
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  verdictCardFromToolArgs,
  verdictFromResultText,
  renderTranscriptItems,
  fetchVerdicts,
  postVerdict,
  isVerdictTool,
  verdictIssuedAt,
  awaitsWake,
  transcriptHasPending,
  transcriptPendingCount,
  type VerdictItem,
} from "../app/composables/omega.ts";
import { createCanvasTranscript } from "../app/composables/chat.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, ".."); // webui/ (webui/test -> webui -> webui)

function read(...parts: string[]): string {
  return readFileSync(join(modules, ...parts), "utf8");
}

// -- pure helpers --

test("verdictCardFromToolArgs builds the card from the tool args", () => {
  const card = verdictCardFromToolArgs({
    title: "Approve the apply?",
    body: "this writes the patch",
    options: [
      { label: "yes, apply", description: "merge the worktree now" },
      { label: "no" },
    ],
    multi: true,
    allow_text: true,
    timeout_s: 300,
  });
  assert.equal(card.kind, "verdict");
  assert.equal(card.title, "Approve the apply?");
  assert.equal(card.body, "this writes the patch");
  assert.deepEqual(card.options, [
    { label: "yes, apply", description: "merge the worktree now" },
    { label: "no" },
  ]);
  assert.equal(card.multi, true);
  assert.equal(card.allowText, true);
  assert.equal(card.timeoutS, 300);
  assert.equal(card.status, "pending");
});

test("verdictCardFromToolArgs keeps bare-string options as label-only rows", () => {
  const card = verdictCardFromToolArgs({ title: "q", options: ["yes", "no"] });
  assert.deepEqual(card.options.map((o) => o.label), ["yes", "no"]);
  assert.deepEqual(card.options.map((o) => o.description), [undefined, undefined]);
  assert.equal(card.multi, false);
});

test("verdictCardFromToolArgs degrades malformed args, never crashes", () => {
  for (const blob of [null, undefined, {}, { options: "not-an-array" }, { title: 42, options: [42, "ok"] }]) {
    const card = verdictCardFromToolArgs(blob);
    assert.equal(card.kind, "verdict");
    assert.equal(card.status, "pending");
    assert.ok(Array.isArray(card.options));
  }
  const withJunk = verdictCardFromToolArgs({ title: 5, options: [{ label: "" }, {}, "ok"], allow_text: 1 as unknown as boolean });
  assert.equal(withJunk.title, "question");
  assert.deepEqual(withJunk.options, [{ label: "ok" }]); // junk rows drop, not fatal
  assert.equal(withJunk.allowText, false); // non-true allow_text is false, not 1
});

test("verdictFromResultText parses the tool-result JSON shapes", () => {
  assert.deepEqual(
    verdictFromResultText(`{"card_id":"c1","verdict":"answer","option_index":2,"option":"no"}`),
    { verdictKind: "answer", picked: "no", answerText: undefined },
  );
  assert.deepEqual(
    verdictFromResultText(`{"card_id":"c2","verdict":"answer","text":"omega"}`),
    { verdictKind: "answer", picked: undefined, answerText: "omega" },
  );
  assert.deepEqual(
    verdictFromResultText(`{"card_id":"c3","verdict":"answer","option_indexes":[1,3],"options":["a","c"]}`),
    { verdictKind: "answer", picked: "a, c", answerText: undefined },
  );
  assert.deepEqual(
    verdictFromResultText(`{"card_id":"c1","verdict":"skip","skipped":true}`),
    { verdictKind: "skip", picked: undefined, answerText: undefined },
  );
  assert.deepEqual(
    verdictFromResultText(`{"card_id":"c1","verdict":"expired","expired":true}`),
    { verdictKind: "expired", picked: undefined, answerText: undefined },
  );
});

test("verdictFromResultText: non-JSON and unknown kinds are a dead card, not a crash", () => {
  for (const raw of ["", "plain refusal text", `{"verdict":"weird"}`, `null`]) {
    const got = verdictFromResultText(raw);
    assert.equal(got.verdictKind, "expired", `raw: ${raw}`);
    assert.equal(got.picked, undefined);
  }
});

// -- wire restore --

test("renderTranscriptItems: a verdict call renders the card, not a tool line", () => {
  const items = renderTranscriptItems([
    { kind: "message", content: "go?", role: "user", time: "t0" },
    { kind: "tool_call", tool_call: { id: "tc1", name: "verdict.request", arguments: { title: "Approve?", options: ["yes", "no"] } } },
    { kind: "tool_result", tool_call_id: "tc1", content: `{"card_id":"c1","verdict":"answer","option_index":1,"option":"yes"}`, elapsed_ms: 4200 },
  ]);
  assert.deepEqual(items.map((i) => (i.kind === "verdict" ? "verdict" : i.kind)), ["message", "verdict"]);
  const card = items[1];
  assert.equal(card.kind, "verdict");
  if (card.kind !== "verdict") return;
  assert.equal(card.title, "Approve?");
  assert.deepEqual(card.options, [{ label: "yes" }, { label: "no" }]);
  // the matching tool_result settled the card with the recorded verdict
  assert.equal(card.status, "settled");
  assert.equal(card.verdictKind, "answer");
  assert.equal(card.picked, "yes");
});

test("renderTranscriptItems: described options survive the restore (list layout data)", () => {
  const items = renderTranscriptItems([
    { kind: "tool_call", tool_call: { id: "tc1", name: "verdict.request", arguments: {
      title: "Pick lanes",
      options: [{ label: "Ship", description: "merge now" }, { label: "Hold" }],
      multi: true,
      allow_text: true,
    } } },
  ]);
  const card = items[0];
  if (card.kind !== "verdict") throw new Error("expected a verdict item");
  assert.deepEqual(card.options, [
    { label: "Ship", description: "merge now" },
    { label: "Hold" },
  ]);
  assert.equal(card.multi, true);
  assert.equal(card.allowText, true);
});

test("renderTranscriptItems: a pending card with no result row is ACTIONABLE, anchored on the wire's stamp", () => {
  const started = "2026-11-18T05:00:00.000Z";
  const items = renderTranscriptItems([
    { kind: "tool_call", started_at: started, tool_call: { id: "tc1", name: "verdict.request", arguments: { title: "Old question", timeout_s: 180 } } },
  ]);
  const card = items[0];
  if (card.kind !== "verdict") throw new Error("expected a verdict item");
  assert.equal(card.status, "pending");
  // verdict.request is yield-class. "No live run" therefore says nothing about the
  // card, and the retired `stale` bit (which read exactly that) painted every real card
  // dead.
  assert.equal("stale" in card, false, "there is no run-derived liveness bit on a card");
  assert.equal(card.issuedAt, Date.parse(started), "the wire timestamp is the countdown anchor");
  assert.equal(card.timeoutS, 180);
});

test("verdictIssuedAt: no clock, no anchor (never a fabricated one)", () => {
  assert.equal(verdictIssuedAt(undefined), undefined);
  assert.equal(verdictIssuedAt(""), undefined);
  assert.equal(verdictIssuedAt("not-a-date"), undefined);
  assert.equal(verdictIssuedAt("2026-11-18T05:00:00Z"), Date.parse("2026-11-18T05:00:00Z"));
});

test("isVerdictTool: the dotted wire verb, and nothing else", () => {
  assert.equal(isVerdictTool("verdict.request"), true);
  // the pre-rename flat name is what the renderer kept matching AFTER the wire moved.
  assert.equal(isVerdictTool("verdict"), false, "the flat pre-rename name matches nothing on the wire");
  assert.equal(isVerdictTool("verdict.request.extra"), false);
  assert.equal(isVerdictTool("shell.run"), false);
  assert.equal(isVerdictTool(undefined), false);
});

test("a canvas payload carrying verdict.request paints a card - actionable, anchored, run ENDED", () => {
  // The canvas path IS the chat pane's render source, and the wire name is the whole
  // regression.
  const started = new Date(Date.now() - 30_000).toISOString();
  const t = createCanvasTranscript();
  const items = t.apply({
    session: "s-live",
    running: false,
    rows: [
      {
        id: "r1",
        role: "assistant",
        status: "complete",
        blocks: [
          {
            type: "tool_call",
            tool_call: {
              id: "tc1",
              name: "verdict.request",
              arguments: {
                title: "J-F8 countdown probe",
                options: [{ label: "Ship", description: "merge the branch now" }, { label: "Hold" }],
                timeout_s: 180,
              },
            },
            started_at: started,
          },
        ],
      },
    ],
  } as never);
  assert.equal(items.length, 1);
  const card = items[0];
  assert.equal(card.kind, "verdict", "the wire name maps to a verdict item, never a tool line");
  if (card.kind !== "verdict") return;
  assert.equal(card.status, "pending");
  assert.equal(card.title, "J-F8 countdown probe");
  assert.deepEqual(card.options, [{ label: "Ship", description: "merge the branch now" }, { label: "Hold" }]);
  assert.equal(card.timeoutS, 180);
  assert.equal(card.issuedAt, Date.parse(started), "the countdown anchors on the server's stamp");
  assert.equal("stale" in card, false, "a yielded run's card is not dead");
});

test("renderTranscriptItems: tool lines are untouched for every other tool", () => {
  const items = renderTranscriptItems([
    { kind: "tool_call", tool_call: { id: "tc1", name: "shell.run", arguments: { cmd: "ls" } } },
    { kind: "tool_result", tool_call_id: "tc1", content: "file.txt" },
  ]);
  assert.deepEqual(items.map((i) => i.kind), ["tool"]);
});

test("renderTranscriptItems: a verdict tool_result without its call renders nothing", () => {
  const items = renderTranscriptItems([
    { kind: "tool_result", tool_call_id: "tc9", content: `{"verdict":"answer","option":"yes"}` },
  ]);
  assert.deepEqual(items, []);
});

// -- fetch wrappers --

test("fetchVerdicts: empty session lists every pending card via /verdicts", async () => {
  const seen: string[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL) => {
    seen.push(String(url));
    return Promise.resolve(new Response(
      JSON.stringify({ cards: [{ id: "c1", session_id: "s1", title: "Approve?", options: ["yes", "no"], expires_at: 1893456000000 }], count: 1 }),
      { status: 200, headers: { "content-type": "application/json" } },
    ));
  }) as typeof globalThis.fetch;
  try {
    const page = await fetchVerdicts();
    assert.equal(seen.length, 1);
    assert.equal(seen[0], "/verdicts");
    assert.equal(page.count, 1);
    assert.equal(page.cards[0].title, "Approve?");
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("fetchVerdicts: a session filter hits /sessions/{id}/verdicts, escaped", async () => {
  const seen: string[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL) => {
    seen.push(String(url));
    return Promise.resolve(new Response(
      JSON.stringify({ cards: [], count: 0 }),
      { status: 200, headers: { "content-type": "application/json" } },
    ));
  }) as typeof globalThis.fetch;
  try {
    const page = await fetchVerdicts("s/1");
    assert.equal(seen[0], "/sessions/s%2F1/verdicts");
    assert.equal(page.count, 0);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("fetchVerdicts: 501 (seam unmounted) degrades to an empty list", async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = (() =>
    Promise.resolve(new Response("not mounted", { status: 501 }))) as typeof globalThis.fetch;
  try {
    const page = await fetchVerdicts();
    assert.deepEqual(page, { cards: [], count: 0 });
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("postVerdict posts the answer to the session endpoint", async () => {
  const seen: { url: string; method: string; body: string }[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL, init: { method?: string; body?: string }) => {
    seen.push({ url: String(url), method: init?.method ?? "GET", body: String(init?.body ?? "") });
    return Promise.resolve(new Response(JSON.stringify({ card_id: "c1", verdict: "answer", option: "yes", option_index: 1 }), {
      status: 200, headers: { "content-type": "application/json" },
    }));
  }) as typeof globalThis.fetch;
  try {
    await postVerdict("s1", { option_index: 1 });
    assert.equal(seen.length, 1);
    assert.equal(seen[0].url, "/sessions/s1/verdicts");
    assert.equal(seen[0].method, "POST");
    assert.deepEqual(JSON.parse(seen[0].body), { option_index: 1 });
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("postVerdict: a multi confirm posts every selected option index", async () => {
  const seen: { url: string; method: string; body: string }[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL, init: { method?: string; body?: string }) => {
    seen.push({ url: String(url), method: init?.method ?? "GET", body: String(init?.body ?? "") });
    return Promise.resolve(new Response(JSON.stringify({ card_id: "cm", verdict: "answer", option_indexes: [1, 3] }), {
      status: 200, headers: { "content-type": "application/json" },
    }));
  }) as typeof globalThis.fetch;
  try {
    await postVerdict("s1", { option_indexes: [1, 3] });
    assert.deepEqual(JSON.parse(seen[0].body), { option_indexes: [1, 3] });
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("postVerdict: a lost race (409) throws the server's reason, so the loser shows it", async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = (() =>
    Promise.resolve(new Response(
      JSON.stringify({ error: "verdict card c1 already answered" }),
      { status: 409, headers: { "content-type": "application/json" } },
    ))) as typeof globalThis.fetch;
  try {
    await assert.rejects(
      () => postVerdict("s1", { card_id: "c1", option_index: 2 }),
      (e: unknown) => {
        assert.ok(e instanceof Error);
        assert.equal(e.message, "verdict card c1 already answered");
        return true;
      },
    );
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("postVerdict: a plain non-OK status still throws something truthful", async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = (() =>
    Promise.resolve(new Response("ghost", { status: 404 }))) as typeof globalThis.fetch;
  try {
    await assert.rejects(() => postVerdict("s1", { skip: true }), /verdict: 404/);
  } finally {
    globalThis.fetch = realFetch;
  }
});

// -- source pins (components are not importable in node) --

test("ChatItem routes verdict items to the VerdictCard component", () => {
  const chatItem = read("app/components/ChatItem.vue");
  assert.match(chatItem, /<VerdictCard(?:\s|>)/, "a verdict item renders as the interactive card");
  assert.match(chatItem, /item\.kind === 'verdict'/, "the verdict kind is the route in");
});

test("VerdictCard renders the list layout and sends every answer shape", () => {
  const vue = read("app/components/chat/VerdictCard.vue");
  assert.match(vue, /verdict-option-row/, "options render as stacked rows (list layout, not pills)");
  assert.match(vue, /flex-direction: column/, "the rows stack top to bottom, one per row");
  assert.match(vue, /verdict-desc/, "each row shows its option description");
  assert.match(vue, /confirmMulti/, "multi cards confirm, they do not answer on first tap");
  assert.match(vue, /option_indexes: \[\.\.\.selected\.value\]/, "the confirm posts every selected index");
  assert.match(vue, /option_index: index/, "single rows still send a numeric option index");
  assert.match(vue, /type your own answer/, "the typed-answer row sits between options and skip");
  assert.match(vue, /postVerdict\(props\.sessionId/, "answers post to the session the chat tab owns");
  assert.match(vue, /skip: true/, "no silent dismissal: the skip posts as a verdict");
  assert.match(vue, /error\.value = e instanceof Error \? e\.message/, "the race reason renders (no silent retry)");
  // a pending card is actionable and counts down.
  assert.match(vue, /item\.status === 'pending' && countdown/, "a pending card renders its countdown");
  assert.match(vue, /verdict-countdown/, "the counting-down label exists");
  assert.doesNotMatch(vue, /item\.stale/, "no run-derived staleness bit decides the card's fate");
});

test("the notifications badge counts pending verdict cards (card t_180db2cb)", () => {
  const store = read("app/components/shell/notificationFeed.ts");
  assert.match(store, /fetchVerdicts/, "the feed store consumes the pending-cards listing");
  assert.match(store, /unread\.value \+ pendingVerdicts\.value\.length/, "the badge sums finished-things and pending questions");
  const panel = read("../harness/notifications/ui/NotificationsPanel.vue");
  assert.match(panel, /openVerdict/, "a pending card row routes to its chat session");
});

test("both render paths ask ONE wire-name predicate (a rename cannot kill the card again)", () => {
  const chat = read("app/composables/chat.ts");
  const omega = read("app/composables/omega.ts");
  // ONE list.
  assert.match(omega, /export const VERDICT_TOOL_NAMES: readonly string\[\] = \["verdict\.request"\]/, "the dotted wire verb, spelled once");
  assert.match(omega, /export function isVerdictTool/, "one predicate owns the name");
  assert.match(omega, /if \(isVerdictTool\(t\.name\)\)/, "the REST restore asks the predicate");
  assert.match(chat, /if \(isVerdictTool\(block\.tool_call\?\.name\)\)/, "the canvas paint asks the same predicate");
  for (const [file, src] of [["chat.ts", chat], ["omega.ts", omega]] as const) {
    assert.doesNotMatch(
      src,
      /(?:tool_call\?\.name|t\.name)\s*===\s*"verdict/,
      `${file} compares a tool NAME to a literal again - that is how the rename silently killed the card`,
    );
  }
});

test("chat.ts maps the card rows live and settles them from the result row", () => {
  const chat = read("app/composables/chat.ts");
  assert.match(chat, /isVerdictTool\(block\.tool_call\?\.name\)/, "the canvas transcript recognizes the card tool");
  assert.match(chat, /verdictCardFromToolArgs/, "live cards build from the row args");
  assert.match(chat, /verdictFromResultText/, "the tool_result row settles the card");
  // A card's liveness is NOT the panel's streaming bit.
  assert.doesNotMatch(chat, /stale\s*=\s*!panelRunning/, "the panel's running flag never decides a card's fate");
  assert.doesNotMatch(chat, /\.stale\b/, "no run-derived staleness bit survives on the canvas path");
});

test("a pending card is a wake waiter, so a reload arms the watcher (card t_79386e36)", () => {
  const pending = renderTranscriptItems([
    { kind: "tool_call", tool_call: { id: "v1", name: "verdict.request", arguments: { title: "ship?" } } },
  ]);
  const settled = renderTranscriptItems([
    { kind: "tool_call", tool_call: { id: "v1", name: "verdict.request", arguments: { title: "ship?" } } },
    { kind: "tool_result", tool_call_id: "v1", content: `{"verdict":"answer","option":"yes"}` },
  ]);
  // the answer wakes a NEW run on the same session - without a waiter that
  // wake's frames land on nobody and the continuation never paints
  assert.equal(transcriptHasPending(pending), true, "a pending card awaits a wake run");
  assert.equal(transcriptHasPending(settled), false);
  assert.equal(transcriptPendingCount(pending), 1, "the status bar agrees with the canvas count");
  assert.equal(transcriptPendingCount(settled), 0);
  // the CARD shape itself is a waiter: the tool-chip branch cannot answer for it, so.
  const card: VerdictItem = { kind: "verdict", title: "ship?", options: [], multi: false, allowText: false, status: "pending" };
  assert.equal(awaitsWake(card), true, "a pending card is a yield-class waiter");
  assert.equal(awaitsWake({ ...card, status: "settled" }), false);
  assert.equal(awaitsWake(undefined), false);
});