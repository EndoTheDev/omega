// omega webui: the pane area's right-click menu (the right-click arc's long tail).
//
// The row list is a pure builder (composables/panes.ts) so the law is testable: the
// standard close first, the pane's own split verbs below a rule, and an action the pane
// cannot take ABSENT. The browser half (menu opens, a row acts, Escape closes) is recipe
// 118; this file pins the rows and the registration in DockPane.vue.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { paneMenuItems } from "../app/composables/panes.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id?: string }).id ?? "separator");
}

function deps() {
  const calls: string[] = [];
  return {
    calls,
    closePane: () => calls.push("close"),
    splitRight: () => calls.push("right"),
    splitDown: () => calls.push("down"),
  };
}

describe("paneMenuItems: close pane, then the pane's own split verbs", () => {
  it("puts the standard Close pane first, then a rule, then the split verbs", () => {
    const d = deps();
    const items = paneMenuItems(d);
    assert.deepEqual(ids(items), ["pane:close", "separator", "pane:split-right", "pane:split-down"]);
    assert.deepEqual(
      items.map((i) => (i as { label?: string }).label ?? "|"),
      ["Close pane", "|", "Split right", "Split down"],
    );
    const close = items[0] as { danger?: boolean; run: () => void };
    assert.equal(close.danger, true, "closing a pane takes its tabs with it");
    close.run();
    (items[2] as { run: () => void }).run();
    (items[3] as { run: () => void }).run();
    assert.deepEqual(d.calls, ["close", "right", "down"]);
  });

  it("drops the rule when the standard row is absent (the last main pane, nothing to close)", () => {
    const items = paneMenuItems({ ...deps(), closePane: null });
    assert.deepEqual(ids(items), ["pane:split-right", "pane:split-down"]);
  });

  it("offers close alone when the pane cannot split (a zone at its floor)", () => {
    const items = paneMenuItems({ ...deps(), splitRight: null, splitDown: null });
    assert.deepEqual(ids(items), ["pane:close"]);
  });

  it("registers nothing on a pane with no verb at all - the right-click does nothing", () => {
    assert.deepEqual(paneMenuItems({ closePane: null, splitRight: null, splitDown: null }), []);
  });
});

describe("the wiring: the pane area registers into the shared store", () => {
  const pane = read("webui", "app", "components", "DockPane.vue");

  it("the pane root's right-click runs the pane handler", () => {
    assert.match(pane, /@contextmenu="onPaneContextMenu"/);
    assert.match(pane, /function onPaneContextMenu\(e: MouseEvent\)/);
  });

  it("the handler builds the rows from the dock's own refusals and opens the shared menu", () => {
    assert.match(
      pane,
      /closePane: api\.canRemovePane\(props\.node\.id\) \? \(\) => api\.remove\(props\.node\.id\) : null/,
      "the close row exists exactly where the pane's own remove affordance does",
    );
    assert.match(
      pane,
      /openContextMenu\(e\.clientX, e\.clientY, rows\)\) e\.preventDefault\(\)/,
      "the native menu is suppressed only when a menu actually opens",
    );
  });

  it("content keeps the browser's own menu, and a pinned pane offers nothing", () => {
    assert.match(pane, /if \(pinned\.value\) return;/, "a sidebar or the bottom shell has no pane verbs");
    assert.match(
      pane,
      /closest\?\.\("\.dock-content, \.dock-tab, \.dock-menu"\)/,
      "chat text, previews and tab chips never reach the pane handler",
    );
  });
});
