// omega webui: layout persistence v:3 (v0.14.4 milestone 3, ruling 6).
// parseAnyLayout is the TOTAL deserializer: ANY JSON shape parses to a
// valid tree, no throw path, with a repair receipt for everything it had
// to ease. The malformed-case table runs RED-first: every case here was
// either a crash, a silent default-tree fallback, or a rejected record
// under the v:2 strict reader. serialize writes integer px split sizes;
// fractions derive at load. The backup-law and round-trip gate tests pin
// the writer contract the store persists through.

import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  parseAnyLayout,
  serialize,
  deserializeLayout,
  MIN_PANE_PX,
  DOCK_LAYOUT_KEY,
  DOCK_LAYOUT_BAK_KEY,
  createDock,
  type DockNode,
  type DockSplit,
  type DockPane,
} from "../app/composables/dock.ts";

const asSplit = (n: DockNode) => n as DockSplit;
const asPane = (n: DockNode) => n as DockPane;

// tiny Storage stand-in (the loader tests' shared shape)
class MemoryStorage {
  private m = new Map<string, string>();
  getItem(k: string): string | null {
    return this.m.get(k) ?? null;
  }
  setItem(k: string, v: string): void {
    this.m.set(k, String(v));
  }
  removeItem(k: string): void {
    this.m.delete(k);
  }
  key(i: number): string | null {
    return [...this.m.keys()][i] ?? null;
  }
  get length(): number {
    return this.m.size;
  }
}
const panes = (t: DockNode): DockPane[] => {
  const out: DockPane[] = [];
  const walk = (n: DockNode) => {
    if ("children" in n) (n as DockSplit).children.forEach(walk);
    else out.push(n as DockPane);
  };
  walk(t);
  return out;
};

// the load fallback the store hands in (migrateLegacy there, a fixed row
// here): parseAnyLayout must call it EXACTLY on the garbage class
const fallback = (): DockNode => {
  const a: DockPane = { kind: "pane", id: "f1", tabs: [{ component: "chat" }], active: 0, fixed: null };
  const b: DockPane = { kind: "pane", id: "f2", tabs: [{ component: "files" }], active: 0, fixed: null };
  return { kind: "split", id: "f0", direction: "row", children: [a, b], sizes: [0.5, 0.5] };
};

describe("parseAnyLayout: the repair table (ruling 6)", () => {
  it("garbage / non-object / JSON-fail boots the default tree WITH a receipt", () => {
    for (const raw of ["{broken", "", "42", "[1,2]", "null", '"tree"']) {
      const p = parseAnyLayout(raw, fallback);
      assert.ok(p.tree, `${raw} still yields a tree`);
      assert.deepEqual(p.tree.children ? p.tree.children.length : 0, 2, `${raw} uses the fallback row`);
      assert.ok(
        p.repairs.length > 0 && p.repairs.some((r) => /default/i.test(r)),
        `${raw}: the repair receipt names the default boot`,
      );
    }
    assert.deepEqual(parseAnyLayout(null, fallback).repairs, [], "no record: not garbage, no repair");
  });

  it("unknown version still loads (total deserializer) with a receipt", () => {
    const raw = JSON.stringify({
      v: 99,
      tree: { kind: "split", id: "n1", direction: "row", sizes: [0.5, 0.5], children: [
        { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: null },
        { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: 240 },
      ] },
    });
    const p = parseAnyLayout(raw, fallback);
    assert.ok(p.tree);
    assert.equal(panes(p.tree).length, 2);
    assert.ok(p.repairs.some((r) => /version/i.test(r)), "the receipt names the version");
  });

  it("unknown kind -> the node is DROPPED, the split hoists its survivor", () => {
    const raw = JSON.stringify({
      v: 3,
      tree: { kind: "split", id: "n1", direction: "row", px: [500, 500], children: [
        { kind: "blob", id: "n2" },
        { kind: "pane", id: "n3", tabs: [{ component: "chat" }], active: 0, fixed: 240 },
      ] },
    });
    const p = parseAnyLayout(raw, fallback);
    assert.ok(p.tree);
    assert.equal(panes(p.tree).length, 1, "the unknown node is gone");
    assert.ok(p.repairs.some((r) => /kind|dropped|hoist/i.test(r)));
  });

  it("a split with < 2 surviving children hoists the survivor (no empty splits)", () => {
    const raw = JSON.stringify({
      v: 2,
      tree: { kind: "split", id: "n1", direction: "row", sizes: [0.5, 0.5], children: [
        { kind: "split", id: "n2", direction: "column", sizes: [1], children: [
          { kind: "pane", id: "n3", tabs: [{ component: "chat" }], active: 0, fixed: null },
        ] },
        { kind: "blob", id: "n4" },
      ] },
    });
    const p = parseAnyLayout(raw, fallback);
    assert.ok(p.tree);
    assert.equal(panes(p.tree).length, 1, "the lone pane survives alone");
    // the root IS the hoisted pane (no wrapping split remains at the top)
    assert.equal((p.tree as DockPane).kind, "pane");
  });

  it("bad / colliding ids are re-minted (unique, well-formed)", () => {
    const raw = JSON.stringify({
      v: 2,
      tree: { kind: "split", id: "", direction: "row", sizes: [0.5, 0.5], children: [
        { kind: "pane", id: "nX", tabs: [{ component: "chat" }], active: 0, fixed: null },
        { kind: "pane", id: "nX", tabs: [{ component: "files" }], active: 0, fixed: null },
      ] },
    });
    const p = parseAnyLayout(raw, fallback);
    const ids = panes(p.tree).map((p2) => p2.id);
    assert.notEqual(ids[0], ids[1], "the collision was re-minted apart");
    assert.ok(p.repairs.some((r) => /id/i.test(r)), "the receipt names the id repair");
  });

  it("non-finite or negative fixed px -> null (flexible), with a receipt", () => {
    const raw = JSON.stringify({
      v: 2,
      tree: { kind: "split", id: "n1", direction: "row", sizes: [0.5, 0.5], children: [
        { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: NaN },
        { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: -80 },
      ] },
    });
    const p = parseAnyLayout(raw, fallback);
    for (const pane of panes(p.tree)) assert.equal(pane.fixed, null);
    assert.ok(p.repairs.some((r) => /fixed|width|height/i.test(r)));
  });

  it("sub-floor px clamps to 128 (fixed px AND split px children)", () => {
    const raw = JSON.stringify({
      v: 2,
      tree: { kind: "split", id: "n1", direction: "row", sizes: [0.05, 0.95], children: [
        { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: 50 },
        { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: null },
      ] },
    });
    const p = parseAnyLayout(raw, fallback);
    assert.equal(panes(p.tree)[0]!.fixed, MIN_PANE_PX);
    assert.ok(p.repairs.some((r) => /floor/i.test(r)));
    // split px children too (a v:3 record): px [10, 490] -> [128, 372]
    const raw3 = JSON.stringify({
      v: 3,
      tree: { kind: "split", id: "n1", direction: "row", px: [10, 490], children: [
        { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: null },
        { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: null },
      ] },
    });
    const p3 = parseAnyLayout(raw3, fallback);
    assert.deepEqual(
      asSplit(p3.tree).sizes.map((s) => Math.round(s * 1000)),
      [207, 793],
      "the clamped px shares derive the fractions",
    );
  });

  it("active out of range clamps; 1e-17 fractions do not zero a flexible pane", () => {
    const raw = JSON.stringify({
      v: 2,
      tree: { kind: "split", id: "n1", direction: "row", sizes: [1e-17, 0.9999999999999999], children: [
        { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: null },
        { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 9, fixed: null },
      ] },
    });
    const p = parseAnyLayout(raw, fallback);
    const [a, b] = panes(p.tree);
    assert.equal(b!.active, 0, "active clamps into range");
    const sizes = asSplit(p.tree).sizes;
    assert.ok(sizes[0]! > 0, `the 1e-17 shares survives as ${sizes[0]} (px floor, not 0)`);
    assert.ok(p.repairs.length > 0);
  });

  it("depth > 32 collapses the deepest nodes (no runaway recursion)", () => {
    let node: Record<string, unknown> = { kind: "pane", id: "nDeep", tabs: [], active: 0, fixed: null };
    for (let d = 0; d < 40; d++) {
      node = { kind: "split", id: "nD" + d, direction: d % 2 ? "row" : "column", sizes: [0.5, 0.5], children: [node, { kind: "pane", id: "nP" + d, tabs: [{ component: "files" }], active: 0, fixed: null }] };
    }
    const p = parseAnyLayout(JSON.stringify({ v: 2, tree: node }), fallback);
    assert.ok(p.tree);
    assert.ok(p.repairs.some((r) => /depth/i.test(r)), "the receipt names the collapse");
    panes(p.tree); // the walked tree terminates
  });

  it("tabs are cleaned per-tab (unknown/dropped tabs leave an empty pane, not a crash)", () => {
    const raw = JSON.stringify({
      v: 2,
      tree: { kind: "split", id: "n1", direction: "row", sizes: [0.5, 0.5], children: [
        { kind: "pane", id: "n2", tabs: [{ component: "nope" }, { component: "chat" }], active: 1, fixed: null },
        { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: null },
      ] },
    });
    const p = parseAnyLayout(raw, fallback);
    const a = panes(p.tree)[0]!;
    assert.deepEqual(a.tabs.map((t) => t.component), ["chat"], "the unknown tab is dropped, the chat survives");
  });
});

describe("serialize v:3: integer px on disk (ruling 6)", () => {
  it("split nodes carry px, not fraction sizes", () => {
    const a = { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: null } as unknown as DockPane;
    const b = { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: 240 } as unknown as DockPane;
    const root = { kind: "split", id: "n1", direction: "row", children: [a, b], sizes: [1 / 3, 2 / 3] } as unknown as DockSplit;
    const rec = JSON.parse(serialize(root, [])) as { v: number; tree: DockSplit };
    assert.equal(rec.v, 3);
    assert.equal(rec.tree.sizes, undefined, "fraction sizes are gone from disk");
    // chat flexes over the 760px remainder (1/3 vs files' fixed 240);
    // files keeps its fixed px whole
    assert.deepEqual(rec.tree.px, [760, 240]);
    // px -> fractions derive back; chat's flexible share stays 100% of the
    // remainder (the only flexible child), files keeps its fixed 240
    const back = parseAnyLayout(JSON.stringify(rec), fallback);
    assert.deepEqual(
      asSplit(back.tree).sizes.map((s) => Math.round(s * 100) / 100),
      [0.76, 0.24],
    );
  });

  it("v:2 records migrate: fraction * nominal-container -> px at load, re-persist writes v:3", () => {
    const raw = JSON.stringify({
      v: 2,
      tree: { kind: "split", id: "n1", direction: "row", sizes: [0.5, 0.5], children: [
        { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: null },
        { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: null },
      ] },
      closedEdges: [],
    });
    const p = parseAnyLayout(raw, fallback);
    assert.deepEqual(asSplit(p.tree).sizes, [0.5, 0.5], "the migration preserves ratios");
    const rec = JSON.parse(serialize(p.tree, [])) as { v: number };
    assert.equal(rec.v, 3, "the next persist is v:3");
    // the strict reader accepts the v:3 shape too (deserialize() keeps working)
    assert.ok(deserializeLayout(serialize(p.tree, [])), "the strict reader reads v:3");
  });
});

describe("the round-trip gate (ruling 6: one gated writer)", () => {
  it("a fresh default boot persists nothing and does not write a backup", () => {
    const store = new MemoryStorage();
    createDock(store as unknown as Storage);
    assert.equal(store.getItem(DOCK_LAYOUT_BAK_KEY), null, "no repair, no backup");
    assert.equal(store.getItem(DOCK_LAYOUT_KEY), null, "a fresh boot writes on first mutation, not before");
  });

  it("a REPAIRED layout backs up the raw record BEFORE the first persist", () => {
    const store = new MemoryStorage();
    store.setItem(DOCK_LAYOUT_KEY, "{broken");
    createDock(store as unknown as Storage);
    assert.equal(store.getItem(DOCK_LAYOUT_BAK_KEY), "{broken", "the raw record is copied");
    assert.ok(JSON.parse(store.getItem(DOCK_LAYOUT_KEY)!).v === 3, "the layout re-persisted (the receipt path)");
  });

  it("a healthy layout never lands a bak file", () => {
    const store = new MemoryStorage();
    const healthy = JSON.stringify({
      v: 3,
      tree: { kind: "split", id: "n1", direction: "row", px: [500, 500], children: [
        { kind: "pane", id: "n2", tabs: [{ component: "chat" }], active: 0, fixed: null },
        { kind: "pane", id: "n3", tabs: [{ component: "files" }], active: 0, fixed: null },
      ] },
      closedEdges: [],
    });
    store.setItem(DOCK_LAYOUT_KEY, healthy);
    createDock(store as unknown as Storage);
    assert.equal(store.getItem(DOCK_LAYOUT_BAK_KEY), null);
    assert.ok(store.getItem(DOCK_LAYOUT_KEY)!.includes('"px"'), "the v:3 record re-persisted");
  });
});