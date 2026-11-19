// omega webui: the tab's VIEW record - the values a
// dock-tab switch destroys. DockPane renders ONE outlet for the ACTIVE tab
// and there is no KeepAlive in the tree, so every switch unmounts ChatTab
// and took the reader's scroll place, the follow latch, the tool-row fold
// pins and the activity line's clock with it (the audit measured scrollTop
// 91 -> 1362 and an open tool row closing on a switch away and back).
//
// The settled design is the STORE RECORD keyed by the tab (ChatState.view),
// never <KeepAlive> on the outlet. Two layers are pinned here:
//   - pure/behavioural: the fold ADDRESS ladder (durable only - no slot) and
//     load()'s reuse guard (a remount refetches nothing while the state
//     already holds the session and no run moved the row stamp);
//   - source: the pane's three wiring points (scroll record/restore, the
//     injected fold store, the activity anchor's run+phase guard).
// Run: node --test test/
import { afterEach, describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  createChatHost,
  emptyChatViewState,
  type ChatHostDeps,
} from "../app/composables/chat.ts";
import { foldAddress, toolChildKey } from "../app/composables/toolGroup.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

function deps(over: Partial<ChatHostDeps> = {}): ChatHostDeps {
  return {
    setTabSessionRef: () => {},
    refreshSessions: () => {},
    sessionRow: () => undefined,
    markRead: () => {},
    isFocused: () => false,
    ...over,
  };
}

// stubTranscript answers GET /sessions/<id>/transcript and counts the fires -
// the refetch witness the card's fifth acceptance item is about.
function stubTranscript(stamp: () => string): { calls: string[]; restore: () => void } {
  const saved = globalThis.fetch;
  const calls: string[] = [];
  globalThis.fetch = (async (input: unknown) => {
    calls.push(String(input));
    return {
      ok: true,
      json: async () => ({
        session: { id: "s1", created_at: "2026-01-01T00:00:00Z", updated_at: stamp() },
        running: false,
        items: [],
      }),
    } as unknown as Response;
  }) as typeof fetch;
  return { calls, restore: () => (globalThis.fetch = saved) };
}

const realFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = realFetch;
});

// --- the record itself ------------------------------------------------------

describe("ChatViewState: per-tab, never shared", () => {
  it("mints a fresh record per call - the fold map and the anchor are not aliased", () => {
    const a = emptyChatViewState();
    const b = emptyChatViewState();
    a.folds.set("call:t1", true);
    assert.equal(b.folds.get("call:t1"), undefined, "one tab's pins never leak into another");
    assert.notEqual(a.folds, b.folds);
    // the defaults: a fresh tab follows the tail and has loaded nothing
    assert.equal(a.followPinned, true);
    assert.equal(a.scrollTop, 0);
    assert.equal(a.activity, null);
    assert.equal(a.stamp, "");
  });

  it("each chat state carries its own record + run counter", () => {
    const host = createChatHost(deps());
    const one = host.ensureState("c1", "s1");
    const two = host.ensureState("c2", "s2");
    assert.notEqual(one.view, two.view);
    one.view.folds.set("group:call:t1", false);
    assert.equal(two.view.folds.has("group:call:t1"), false);
    one.view.scrollTop = 420;
    assert.equal(two.view.scrollTop, 0);
    assert.equal(one.runSeq, 0, "a state starts before any run");
  });
});

// --- the fold address: durable only ----------------------------------------

describe("foldAddress: the durable ladder, never a slot", () => {
  it("call id first, then the store row id", () => {
    assert.equal(foldAddress({ callId: "t1", id: "r1" }), "call:t1");
    assert.equal(foldAddress({ id: "r1" }), "row:r1");
  });

  it("an id-less row has NO fold address - a slot pin would land on another call", () => {
    assert.equal(foldAddress({}), undefined);
    // the child KEY keeps the slot floor (an element still needs a key);
    // the fold PIN does not - that asymmetry is the card's point
    assert.equal(toolChildKey({ kind: "tool", name: "x", status: "done" }, 4), "slot:4");
  });

  it("the two spellings agree on the durable half (one ladder)", () => {
    const row = { kind: "tool" as const, name: "x", status: "done", callId: "c9", id: "r9" };
    assert.equal(toolChildKey(row, 0), foldAddress(row));
  });
});

// --- load(): the loaded view is reused until the row says otherwise --------

describe("load() does not refetch a state already loaded (unless a run went live)", () => {
  it("a remount's second load() fires no fetch while the row stamp holds", async () => {
    const rows = { updated_at: "stamp-1" };
    const net = stubTranscript(() => rows.updated_at);
    const host = createChatHost(
      deps({
        sessionRow: () => ({
          id: "s1",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: rows.updated_at,
        }),
      }),
    );
    try {
      await host.load("c1", "s1");
      assert.equal(net.calls.length, 1, "the first load restores");
      assert.equal(host.states.get("c1")!.loaded, true);
      // the dock-tab switch: the pane unmounts and mounts again -> load()
      await host.load("c1", "s1");
      assert.equal(net.calls.length, 1, "already loaded + row unchanged + idle = no refetch");
    } finally {
      net.restore();
    }
  });

  it("a run that moved the row stamp makes the same load() fetch", async () => {
    const rows = { updated_at: "stamp-1" };
    const net = stubTranscript(() => rows.updated_at);
    const host = createChatHost(
      deps({
        sessionRow: () => ({
          id: "s1",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: rows.updated_at,
        }),
      }),
    );
    try {
      await host.load("c1", "s1");
      rows.updated_at = "stamp-2"; // a run (any tab, any window) persisted
      await host.load("c1", "s1");
      assert.equal(net.calls.length, 2, "the local view is stale - refetch");
    } finally {
      net.restore();
    }
  });

  it("a live run on the session is stale even before the row stamp moves", async () => {
    const net = stubTranscript(() => "stamp-1");
    const host = createChatHost(
      deps({
        sessionRow: () => ({
          id: "s1",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "stamp-1",
        }),
      }),
    );
    try {
      await host.load("c1", "s1");
      // the server-reported active set (a detached run another window began)
      host.noteServerActive(["s1"]);
      await host.load("c1", "s1");
      assert.equal(net.calls.length, 2, "the runHub truth outranks the row stamp");
    } finally {
      net.restore();
    }
  });

  it("force = the callers that KNOW the truth moved (socket catch-up, variant select)", async () => {
    const net = stubTranscript(() => "stamp-1");
    const host = createChatHost(
      deps({
        sessionRow: () => ({
          id: "s1",
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "stamp-1",
        }),
      }),
    );
    try {
      await host.load("c1", "s1");
      await host.load("c1", "s1", true);
      assert.equal(net.calls.length, 2, "a forced load always reads");
      await host.reconnectCatchUp();
      assert.equal(net.calls.length, 3, "the socket catch-up is forced - a gap is exactly when the stamp lies");
    } finally {
      net.restore();
    }
  });

  it("an unknown session row refetches rather than guess", async () => {
    const net = stubTranscript(() => "stamp-1");
    const host = createChatHost(deps({ sessionRow: () => undefined }));
    try {
      await host.load("c1", "s1");
      await host.load("c1", "s1");
      assert.equal(net.calls.length, 2, "no row = no 'unchanged' claim");
    } finally {
      net.restore();
    }
  });
});

// --- the pane's wiring (the node suite has no DOM: source pins) ------------

describe("ChatTab's view record wiring (card t_15572849)", () => {
  const tab = () => src("components/ChatTab.vue");

  it("provides the fold store and addresses every render site", () => {
    const t = tab();
    assert.match(t, /provide\(CHAT_FOLD_PINS_KEY, foldPins\)/);
    assert.match(t, /get: \(key\) => viewRecord\(\)\?\.folds\.get\(key\)/);
    assert.match(t, /view\.folds\.set\(key, open\)/);
    // three ChatItem sites (pinned slot, stacked flow, chronolog) and two
    // group sites carry the address -> a manual pin reaches the store
    assert.equal(
      (t.match(/:fold-key="rowFoldKey\(entry\)"/g) ?? []).length,
      3,
      "every ChatItem site forwards the row's fold address",
    );
    assert.equal(
      (t.match(/:fold-key="groupFoldKey\(entry\.items\)"/g) ?? []).length,
      2,
      "both group sites forward the head row's address",
    );
  });

  it("records the reader's place on every scroll and on unmount, restores on mount", () => {
    const t = tab();
    assert.match(t, /view\.scrollTop = el\.scrollTop/);
    assert.match(t, /view\.followPinned = pinned\.value/);
    // onScroll writes it in BOTH branches (the follow write's own event is
    // still the reader's place)
    assert.equal((t.match(/recordScroll\(\);/g) ?? []).length, 3, "both scroll branches + the unmount");
    assert.match(t, /onMounted\(async \(\) => \{[\s\S]*restoreScroll\(\)/);
    // the restore is OUR write, not a reader decision: it primes the guard's
    // fuel and registers the write (the followWrite law)
    const restore = t.slice(t.indexOf("function restoreScroll"), t.indexOf("function jumpToLive"));
    assert.match(restore, /lastScrollHeight = el\.scrollHeight/);
    assert.match(restore, /el\.scrollTop = target/);
    assert.match(restore, /followWrite = el\.scrollTop/);
    assert.match(restore, /pinned\.value = view\.followPinned/);
  });

  it("resumes the activity clock only for the same run's same phase", () => {
    const t = tab();
    const meter = t.slice(t.indexOf("function startActivityMeter"), t.indexOf("onBeforeUnmount(() => tick?.stop())"));
    assert.match(
      meter,
      /stored\.run === st\.runSeq && stored\.key === a\.key/,
      "a resumed anchor must belong to THIS run and THIS phase",
    );
    assert.match(meter, /consumed = Math\.max\(0, Date\.now\(\) - stored\.startedAt\)/);
    assert.match(meter, /view\.activity = \{ run: st\.runSeq, key: a\.key, startedAt: Date\.now\(\) - consumed \}/);
    assert.match(meter, /if \(view\) view\.activity = null/, "an idle phase retires the anchor");
    // the run counter's writer: every transport start bumps it (one place)
    assert.match(src("composables/chat.ts"), /st\.runSeq \+= 1/);
  });

  it("the tool rows read and write the store pin, and apply it on mount", () => {
    for (const file of ["components/chat/ToolCallLine.vue", "components/chat/ToolGroup.vue"]) {
      const c = src(file);
      assert.match(c, /inject\(CHAT_FOLD_PINS_KEY, null\)/, `${file} reads the pane's fold store`);
      assert.match(c, /folds\.set\(key, open\)/, `${file} writes the reader's pin to the store`);
      assert.match(c, /pin !== null/, `${file} applies a restored pin instead of skipping it`);
    }
    // the row address reaches the tool line through ChatItem (a fragment
    // that owns the per-item markup) and the group's children directly
    assert.match(src("components/ChatItem.vue"), /:fold-key="foldKey"/);
    assert.match(src("components/chat/ToolGroup.vue"), /:fold-key="foldAddress\(item\)"/);
  });
});
