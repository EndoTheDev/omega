// omega webui: the statusbar's right-click stat menu and its persisted hidden set.
//
// The catalog + menu builder are pure (components/shell/statusStats.ts), so the law is
// testable: every stat is one row, the row carries the state it flips, and the hidden set
// is a layout fact - it survives a reload through dock.ts's record.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  STATUS_STATS,
  parseHiddenStats,
  statMenuItems,
  statVisible,
} from "../app/components/shell/statusStats.ts";
import { createDock, deserializeLayout, serialize } from "../app/composables/dock.ts";

// tiny Storage stand-in (the layout tests' shared shape)
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
  raw(k: string): string | null {
    return this.m.get(k) ?? null;
  }
}

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id?: string }).id ?? "separator");
}

describe("the stat catalog", () => {
  it("names each segment once, ids unique", () => {
    const seen = new Set<string>();
    for (const stat of STATUS_STATS) {
      assert.ok(stat.id.length > 0);
      assert.ok(stat.label.length > 0);
      assert.ok(!seen.has(stat.id), `duplicate stat id ${stat.id}`);
      seen.add(stat.id);
    }
    assert.ok(seen.size >= 5, "the bar's segments are all in the catalog");
  });

  it("every catalogued stat is one checked row on the bar's menu", () => {
    const items = statMenuItems([], () => {});
    assert.deepEqual(
      ids(items),
      [...STATUS_STATS.map((s) => "stat:" + s.id), "separator", "stat:reset"],
      "the catalog first, then the rule, then the one reset row",
    );
    assert.deepEqual(
      items.slice(0, STATUS_STATS.length).map((i) => (i as { checked: boolean }).checked),
      STATUS_STATS.map(() => true),
      "nothing hidden = every row checked",
    );
    for (const item of items.slice(0, STATUS_STATS.length)) {
      assert.equal(typeof (item as { run: unknown }).run, "function");
    }
  });

  it("Reset statusbar is the LAST row and clears the whole hidden set", () => {
    const calls: [string, boolean][] = [];
    const items = statMenuItems(["tokens", "context", "gone-in-a-later-build"], (id, hidden) =>
      calls.push([id, hidden]),
    );
    const last = items[items.length - 1] as {
      id: string;
      label: string;
      checked?: boolean;
      disabled?: boolean;
      run: () => void;
    };
    assert.equal(last.id, "stat:reset");
    assert.equal(last.label, "Reset statusbar");
    assert.equal(last.checked, undefined, "Reset is a verb, not a switch");
    assert.equal(last.disabled, false, "a hidden stat makes it fireable");
    last.run();
    assert.deepEqual(
      calls,
      [
        ["tokens", false],
        ["context", false],
        ["gone-in-a-later-build", false],
      ],
      "every hidden id rides the same toggle write back to visible",
    );
  });

  it("Reset with nothing hidden is still listed, and dimmed", () => {
    const items = statMenuItems([], () => {
      throw new Error("a disabled reset must not write");
    });
    const last = items[items.length - 1] as { disabled?: boolean; run: () => void };
    assert.equal(last.disabled, true);
    last.run();
  });

  it("a click flips the ONE stat it names, and only that stat", () => {
    const calls: [string, boolean][] = [];
    const items = statMenuItems(["context"], (id, hidden) => calls.push([id, hidden]));
    const target = items.find((i) => (i as { id: string }).id === "stat:context") as {
      checked: boolean;
      run: () => void;
    };
    assert.equal(target.checked, false, "a hidden stat reads unchecked");
    target.run();
    assert.deepEqual(calls, [["context", false]], "clicking a hidden stat shows it");
    const tokens = items.find((i) => (i as { id: string }).id === "stat:tokens") as {
      run: () => void;
    };
    tokens.run();
    assert.deepEqual(calls[1], ["tokens", true], "clicking a shown stat hides it");
  });

  it("hidden means the segment is gone, and unknown ids never match a stat", () => {
    assert.equal(statVisible(["tokens"], "tokens"), false);
    assert.equal(statVisible(["tokens"], "context"), true);
    assert.deepEqual(parseHiddenStats(["tokens", 7, "", "tokens", null]), ["tokens"]);
    assert.deepEqual(parseHiddenStats("nope"), []);
  });
});

describe("the hidden set persists in the layout record", () => {
  it("survives a reload through the SAME store the panes persist to", () => {
    const store = new MemoryStorage();
    const first = createDock(store);
    first.setStatHidden("tokens", true);
    first.setStatHidden("context", true);
    const raw = store.getItem("omega.dock-layout");
    assert.ok(raw, "the stat toggle wrote the layout record");
    assert.deepEqual(JSON.parse(raw!).hiddenStats, ["tokens", "context"]);

    const second = createDock(store);
    assert.deepEqual([...second.hiddenStats.value], ["tokens", "context"]);
    second.setStatHidden("tokens", false);
    assert.deepEqual([...second.hiddenStats.value], ["context"]);
    assert.deepEqual(JSON.parse(store.getItem("omega.dock-layout")!).hiddenStats, ["context"]);
  });

  it("the record round-trips the sets, and an empty set writes no field", () => {
    const dock = createDock(new MemoryStorage());
    const raw = serialize(dock.tree.value, [], 1200, undefined, ["files"], ["tokens"]);
    const back = deserializeLayout(raw);
    assert.deepEqual(back?.hiddenTabs, ["files"]);
    assert.deepEqual(back?.hiddenStats, ["tokens"]);
    const bare = JSON.parse(serialize(dock.tree.value));
    assert.equal("hiddenStats" in bare, false, "a stow-free record keeps its old shape");
    assert.equal("hiddenTabs" in bare, false);
  });

  it("a junk entry drops without rejecting the record", () => {
    const record = JSON.stringify({
      v: 4,
      tree: { kind: "pane", id: "n1", tabs: [{ component: "chat" }], active: 0, fixed: null },
      hiddenStats: ["tokens", 5, ""],
      hiddenTabs: "nope",
    });
    const back = deserializeLayout(record);
    assert.ok(back, "one bad visibility entry never kills the layout");
    assert.deepEqual(back?.hiddenStats, ["tokens"]);
    assert.equal(back?.hiddenTabs, undefined);
  });
});
