// omega webui: unit tests for card t_4f0e88f2 (chat tabs + URL
// semantics): the dock engine's chat-tab key ops (setTabSessionRef /
// closeSessionTabs / lookups) and the chat host's per-tab stream model.
// Run: node --test test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { withScratchSingleton } from "./scratchDockable.ts";

import {
  openTab,
  assignChatKeys,
  closeSessionTabs,
  findChatTab,
  firstChatTab,
  firstDraftChatTab,
  makePane,
  makeSplit,
  nextTabKey,
  serialize,
  deserialize,
  setTabSessionRef,
  type DockNode,
  type DockPane as DockPaneNode,
} from "../app/composables/dock.ts";
import {
  createChatHost,
  type ChatHostDeps,
} from "../app/composables/chat.ts";
import { SessionBusyError } from "../app/composables/omega.ts";

// --- helpers ---------------------------------------------------------------

// default-shaped tree: sidebar(240) | chat
function split(children: DockNode[]): DockNode {
  return makeSplit("row", children);
}

function seededTree(chatSession?: string): DockNode {
  const keyed = assignChatKeys(split([makePane("sidebar", 240), makePane("chat")]));
  return chatSession !== undefined ?
    setTabSessionRef(keyed, firstChatTab(keyed)!.tab.key!, chatSession)
    : keyed;
}

function paneAt(tree: DockNode, i: number): DockPaneNode {
  // tests only build split-root trees
  return (tree as { children: DockNode[] }).children[i] as DockPaneNode;
}

// --- dock ops: chat tab keys ------------------------------------------------

describe("chat tab instance keys", () => {
  it("assignChatKeys keys every chat tab; sidebar/inspector stay unkeyed", () => {
    const keyed = assignChatKeys(
      split([makePane("sidebar", 240), makePane("chat"), makePane("inspector", 240)]),
    );
    for (const t of paneAt(keyed, 0).tabs)
      assert.equal(t.key, undefined, "non-chat unkeyed");
    for (const t of paneAt(keyed, 1).tabs)
      assert.match(t.key ?? "", /^c[0-9a-z]+$/, "chat keyed");
    for (const t of paneAt(keyed, 2).tabs)
      assert.equal(t.key, undefined, "non-chat unkeyed");
  });

  it("keys survive a serialize/deserialize round trip", () => {
    const keyed = assignChatKeys(split([makePane("sidebar", 240), makePane("chat")]));
    const tab = firstChatTab(keyed)!.tab;
    assert.match(tab.key ?? "", /^c[0-9a-z]+$/);
    const loaded = deserialize(serialize(keyed))!;
    const again = firstChatTab(loaded)!.tab;
    assert.equal(again.key, tab.key);
  });

  it("nextTabKey mints a fresh namespace, no collision with node ids", () => {
    const a = nextTabKey();
    const b = nextTabKey();
    assert.notEqual(a, b);
    assert.match(a, /^c/);
    assert.equal(/^n/.test(a), false);
  });
});

describe("setTabSessionRef", () => {
  it("binds the tab by key, leaving sibling tabs alone", () => {
    const tree = assignChatKeys(split([makePane("chat"), makePane("chat")]));
    const key2 = paneAt(tree, 1).tabs[0]!.key!;
    const next = setTabSessionRef(tree, key2, "s2");
    assert.equal(paneAt(next, 0).tabs[0]!.sessionRef, undefined);
    assert.equal(paneAt(next, 1).tabs[0]!.sessionRef, "s2");
    // pure: the source tree is untouched
    assert.equal(paneAt(tree, 1).tabs[0]!.sessionRef, undefined);
  });

  it("clears a binding with null (persisted shape drops it)", () => {
    const bound = seededTree("s1");
    const key = firstChatTab(bound)!.tab.key!;
    const tree = setTabSessionRef(bound, key, null);
    const round = JSON.parse(serialize(tree));
    const tab = round.tree.children[1]!.tabs[0]! as { key: string; sessionRef?: string };
    assert.equal(tab.sessionRef, undefined);
    assert.match(tab.key, /^c/);
  });

  it("no-op when the key is gone", () => {
    const tree = seededTree("s1");
    assert.equal(setTabSessionRef(tree, "c999", "s9"), tree);
  });
});

describe("closeSessionTabs", () => {
  it("closes every tab bound to a session across panes; others stay", async () => {
    // two panes, two chat tabs: pane1 keeps an extra unbound tab
    await withScratchSingleton(async (scratch) => {
      const base = assignChatKeys(split([makePane("chat"), makePane("chat")]));
      const k1 = paneAt(base, 0).tabs[0]!.key!;
      const k2 = paneAt(base, 1).tabs[0]!.key!;
      let tree = setTabSessionRef(base, k1, "gone");
      tree = setTabSessionRef(tree, k2, "kept");
      tree = openTab(tree, paneAt(tree, 0).id, { component: scratch });
      // pane1: [gone, scratch] -> [scratch]; pane2 keeps "kept"
      const next = closeSessionTabs(tree, "gone");
      const p1 = paneAt(next, 0);
      const p2 = paneAt(next, 1);
      assert.deepEqual(p1.tabs.map((t) => t.component), [scratch]);
      assert.equal(p1.tabs.length, 1);
      assert.equal(p2.tabs[0]!.sessionRef, "kept");
    });
  });

  it("closing the last tab empties the pane (it survives showing the menu)", () => {
    const bound = seededTree("gone");
    const key = firstChatTab(bound)!.tab.key!;
    const tree = setTabSessionRef(bound, key, "gone");
    assert.equal(paneAt(tree, 1).tabs.length, 1);
    const next = closeSessionTabs(tree, "gone");
    const chatPane = paneAt(next, 1);
    assert.equal(chatPane.tabs.length, 0, "pane empty, not removed");
  });

  it("unrelated session ids change nothing", () => {
    const tree = seededTree("s1");
    assert.equal(closeSessionTabs(tree, "other"), tree);
  });
});

describe("chat tab lookups", () => {
  it("findChatTab locates by key across panes and reports position", () => {
    const tree = seededTree();
    const key = firstChatTab(tree)!.tab.key!;
    const loc = findChatTab(tree, key)!;
    assert.equal(loc.paneId, paneAt(tree, 1).id);
    assert.equal(loc.index, 0);
    assert.equal(firstChatTab(tree, "missing"), null);
    assert.equal(findChatTab(tree, "c_nope"), null);
  });
});

// card t_311838e4: the '+' new flow reuses an open DRAFT chat tab
// (no session binding = the untitled new-chat state) instead of
// minting a duplicate.
describe("firstDraftChatTab", () => {
  it("finds the unbound chat tab; a bound one is not draft", () => {
    const draft = seededTree(); // no binding
    assert.equal(paneAt(draft, 1).tabs[0]!.sessionRef, undefined);
    const loc = firstDraftChatTab(draft);
    assert.equal(loc?.paneId, paneAt(draft, 1).id);
    assert.equal(loc?.index, 0);
    // the first send bound a session: no draft remains
    const bound = seededTree("s1");
    assert.equal(firstDraftChatTab(bound), null);
  });

  it("picks the draft across panes, depth-first, past bound siblings", () => {
    const base = assignChatKeys(split([makePane("chat"), makePane("chat")]));
    let tree = setTabSessionRef(base, paneAt(base, 0).tabs[0]!.key!, "s1");
    tree = openTab(tree, paneAt(tree, 0).id, { component: "chat", key: nextTabKey() });
    const loc = firstDraftChatTab(tree)!;
    assert.equal(loc.paneId, paneAt(tree, 0).id);
    assert.equal(loc.index, 1, "the second tab of pane 0 is the draft");
    assert.equal(loc.tab.sessionRef, undefined);
  });
});

// --- the chat host ----------------------------------------------------------

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

function setBusy(host: ReturnType<typeof createChatHost>, keys: string[]): void {
  (host.busyKeys as unknown as { value: Set<string> }).value = new Set(keys);
}

describe("chat host: per-tab states", () => {
  it("two tabs on one session hold SEPARATE restoration state (the canvas paints both)", () => {
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    host.ensureState("c2", "s1");
    const a = host.states.get("c1")!;
    const b = host.states.get("c2")!;
    assert.notEqual(a, b);
    // the fallback transcript is per state: the canvas panel is the
    // shared view, the restore keeps whatever each tab loaded itself
    a.restored.push({ kind: "message", role: "user", content: "tab a only" });
    assert.equal(b.restored.length, 0);
  });

  it("ensureState adopts a session id the create path binds onto its key", () => {
    const host = createChatHost(deps());
    const a = host.ensureState("c1"); // unsaved
    assert.equal(a.sessionId, null);
    const again = host.ensureState("c1", "s9");
    assert.equal(again, a);
    assert.equal(a.sessionId, "s9");
  });

  it("closeState drops an idle state, orphan-marks a live one", () => {
    const host = createChatHost(deps());
    host.ensureState("c1");
    host.closeState("c1");
    assert.ok(!host.states.has("c1"));
    host.ensureState("c2", "s1");
    setBusy(host, ["c2"]);
    host.closeState("c2");
    assert.ok(host.states.has("c2"), "busy state stays as orphan");
    assert.equal(host.states.get("c2")!.orphan, true);
  });

  it("busyAll maps per-state busy onto session ids and unions the server set", () => {
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    host.ensureState("c2"); // unsaved
    setBusy(host, ["c1", "c2"]);
    assert.ok(host.busyAll.value.has("s1"), "saved busy feeds the session map");
    assert.ok(!host.busyAll.value.has("s2"), "unsaved busy never maps to a session");
    host.noteServerActive(["s9"]);
    assert.ok(host.busyAll.value.has("s9"));
    host.noteServerActive([]);
    assert.ok(host.busyAll.value.has("s1"), "local streams survive a refresh");
  });

  it("composerBusy is per SESSION (the runHub run slot), one gate for both tabs", () => {
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    host.ensureState("c2", "s1");
    setBusy(host, ["c1"]);
    assert.equal(host.composerBusy("c1"), true, "the streaming tab is busy");
    assert.equal(host.composerBusy("c2"), true, "the OTHER tab on the live session is busy too");
    assert.equal(host.composerBusy("c_missing"), false);
    setBusy(host, []);
    host.noteServerActive(["s1"]);
    assert.equal(host.composerBusy("c1"), true, "server truth gates too");
    host.noteServerActive([]);
    assert.equal(host.composerBusy("c1"), false);
  });
});

describe("chat host: the 409 and the busy affordance", () => {
  it("submit is refused in-page while the session's slot is taken", async () => {
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    host.ensureState("c2", "s1");
    setBusy(host, ["c2"]); // same session busy from the OTHER tab
    const ok = await host.submit("c1", "hello");
    assert.equal(ok, false);
    assert.equal(host.states.get("c1")!.restored.length, 0, "nothing fetched, nothing painted");
  });

  it("a 409 POST /chat lands as the busy status line, not transcript state", async () => {
    const savedFetch = globalThis.fetch;
    globalThis.fetch = (async () =>
      new Response("session already running", { status: 409 })) as typeof fetch;
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s1");
      await host.submit("c1", "hello", "glm-5.3");
      const st = host.states.get("c1")!;
      // the transcript belongs to the canvas panel - a refused send
      // paints NOTHING; the status line above the composer carries it
      // and the composer's retry face is the affordance once it frees
      assert.equal(st.restored.length, 0);
      assert.match(st.statusLine, /busy/);
      assert.equal(host.composerBusy("c1"), false);
    } finally {
      globalThis.fetch = savedFetch;
    }
  });

  it("SessionBusyError is distinct from a transport interruption", () => {
    assert.equal(new SessionBusyError().name, "SessionBusyError");
    assert.notEqual(new SessionBusyError().name, "StreamInterruptedError");
  });
});