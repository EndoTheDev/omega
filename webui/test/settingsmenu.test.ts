// omega webui: the settings tab's right-click menu (the right-click arc's long tail).
//
// One standard row - Close - and it is offered only where the tab can close, so the menu
// never shows a verb the tab's own X would refuse. The row list is a pure builder
// (components/settings/settingsMenu.ts); the browser half is recipe 118.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { settingsMenuItems } from "../app/components/settings/settingsMenu.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

describe("settingsMenuItems: the standard Close row", () => {
  it("offers Close, and Close closes the tab it was opened on", () => {
    const calls: string[] = [];
    const items = settingsMenuItems(true, () => calls.push("close"));
    assert.deepEqual(
      items.map((i) => (i as { id?: string; label?: string }).id + "/" + (i as { label?: string }).label),
      ["settings:close/Close"],
    );
    (items[0] as { run: () => void }).run();
    assert.deepEqual(calls, ["close"]);
  });

  it("offers nothing on a tab that cannot close (the locked last tab)", () => {
    assert.deepEqual(settingsMenuItems(false, () => {}), []);
  });
});

describe("the wiring: the settings pane registers into the shared store", () => {
  const tab = read("webui", "app", "components", "settings", "SettingsTab.vue");
  const app = read("webui", "app", "app.vue");

  it("the pane root's right-click runs the settings handler", () => {
    assert.match(tab, /@contextmenu="onSettingsContextMenu"/);
    assert.match(tab, /function onSettingsContextMenu\(e: MouseEvent\)/);
    assert.match(tab, /settingsMenuItems\(props\.canClose === true, \(\) => emit\("close"\)\)/);
  });

  it("an editable field keeps the browser's menu - paste lives there", () => {
    assert.match(
      tab,
      /closest\?\.\("input, textarea, select, \[contenteditable\], dialog"\)/,
      "inputs, textareas and the config editor dialog never lose their native menu",
    );
  });

  it("app.vue hands the dock's own refusal down and closes through closeTab", () => {
    assert.match(app, /:can-close="settingsClosable\(pane\)"/);
    assert.match(app, /@close="closeSettingsTab\(pane\)"/);
    assert.match(app, /function settingsClosable\(paneId: string\): boolean \{[\s\S]*?lastTabLocked\(pane\)/);
    assert.match(app, /function closeSettingsTab\(paneId: string\) \{[\s\S]*?dock\.closeTab\(pane\.id, index\)/);
  });
});
