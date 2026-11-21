// omega webui: the dock tab's right-click menu - the shared context menu's FIRST surface.
//
// The verb list is a pure builder (dockables.ts) so the law is testable; the browser half
// (menu opens, items act, Escape closes) is recipe 116. The rest of this file pins the
// wiring: one surface instance for the whole window, and the right-click selecting the row
// it menus.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { dockTabMenuItems } from "../app/composables/dockables.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id: string }).id);
}

function deps() {
  const calls: string[] = [];
  return {
    calls,
    close: () => calls.push("close"),
    closeOthers: () => calls.push("close-others"),
  };
}

describe("dockTabMenuItems: close, close others", () => {
  it("offers Close and Close others on a closable tab among others", () => {
    const d = deps();
    const items = dockTabMenuItems(
      { tabs: [{ component: "chat" }, { component: "viewer" }] },
      0,
      d,
    );
    assert.deepEqual(ids(items), ["tab:close", "tab:close-others"]);
    assert.deepEqual(
      items.map((i) => (i as { label: string }).label),
      ["Close", "Close others"],
    );
    for (const i of items) (i as { run: () => void }).run();
    assert.deepEqual(d.calls, ["close", "close-others"]);
  });

  it("offers no Close on the locked last tab - and then no menu at all", () => {
    const items = dockTabMenuItems({ tabs: [{ component: "chat" }] }, 0, deps());
    assert.deepEqual(items, [], "the last tab never closes, so nothing is fireable");
  });

  it("offers no Close on an uncloseable component, but still closes its neighbours", () => {
    const items = dockTabMenuItems(
      { tabs: [{ component: "sidebar" }, { component: "chat" }] },
      0,
      deps(),
    );
    assert.deepEqual(ids(items), ["tab:close-others"]);
  });

  it("offers nothing on a lone uncloseable tab", () => {
    assert.deepEqual(
      dockTabMenuItems({ tabs: [{ component: "files" }] }, 0, deps()),
      [],
    );
  });

  it("offers no Close others when no other tab can close", () => {
    const items = dockTabMenuItems(
      { tabs: [{ component: "chat" }, { component: "sidebar" }] },
      0,
      deps(),
    );
    assert.deepEqual(ids(items), ["tab:close"]);
  });

  it("an out-of-range index registers nothing", () => {
    assert.deepEqual(
      dockTabMenuItems({ tabs: [{ component: "chat" }] }, 3, deps()),
      [],
    );
  });
});

describe("the wiring: one surface, selected row, no local menu", () => {
  const pane = read("webui", "app", "components", "DockPane.vue");
  const app = read("webui", "app", "app.vue");
  const menu = read("webui", "app", "components", "ui", "ContextMenu.vue");

  it("the tab's right-click prevents the native menu and opens the shared one", () => {
    assert.match(pane, /@contextmenu\.prevent="onTabContextMenu\(\$event, i\)"/);
    assert.match(pane, /function onTabContextMenu\(e: MouseEvent, index: number\)/);
    assert.match(
      pane,
      /onTabContextMenu[\s\S]*?openContextMenu\(\s*e\.clientX,\s*e\.clientY,\s*dockTabMenuItems\(props\.node, index,/,
      "the dock tab registers its rows into the shared store",
    );
  });

  it("the right-click selects the tab it menus", () => {
    assert.match(
      pane,
      /function onTabContextMenu\(e: MouseEvent, index: number\) \{\s*api\.focus\(props\.node\.id, index\);/,
    );
  });

  it("Close others closes descending, so every index visited is still the live one", () => {
    assert.match(pane, /for \(let i = count - 1; i >= 0; i -= 1\) \{/);
    assert.match(pane, /if \(i === index\) continue;/);
  });

  it("DockPane builds no menu of its own for tabs", () => {
    assert.doesNotMatch(
      pane,
      /@contextmenu(?![.\w-]*=")[^>]*>/,
      "a right-click without the shared opener would keep the native menu",
    );
  });

  it("app.vue mounts exactly one surface", () => {
    const mounts = app.match(/<ContextMenu\b/g) ?? [];
    assert.equal(mounts.length, 1);
    assert.match(app, /import ContextMenu from "\.\/components\/ui\/ContextMenu\.vue";/);
  });

  it("the surface is store-driven: no surface passes it rows", () => {
    assert.match(menu, /v-if="contextMenuOpen"/);
    assert.match(menu, /v-for="\(entry, i\) in contextMenuEntries"/);
    assert.doesNotMatch(menu, /defineProps/, "the store is the only input");
    assert.match(menu, /Teleport to="body"/, "the menu floats over every pane, never inside one");
  });
});
