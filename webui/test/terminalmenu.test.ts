// omega webui: the terminal's right-click menu (the right-click arc's long tail).
//
// The terminal is chrome by the design line, so its right-click is the terminal's own
// menu: Copy / Paste / Clear, the platform terminal's three. Copy is disabled on an empty
// selection (a row that copies nothing reads as a broken copy) and the menu always has a
// fireable row. The browser half (a real right-button press, the rows acting) is recipe
// 118; this file pins the rows and the registration in the terminal capsule's tab.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { terminalMenuItems } from "../../harness/terminal/ui/terminalMenu.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id?: string }).id ?? "separator");
}

function deps(hasSelection: boolean) {
  const calls: string[] = [];
  return {
    calls,
    hasSelection,
    copy: () => {
      calls.push("copy");
    },
    paste: () => {
      calls.push("paste");
    },
    clear: () => {
      calls.push("clear");
    },
  };
}

describe("terminalMenuItems: Copy, Paste, Clear", () => {
  it("lists the three rows, Copy first and Clear under a rule", () => {
    const d = deps(true);
    const items = terminalMenuItems(d);
    assert.deepEqual(ids(items), ["terminal:copy", "terminal:paste", "separator", "terminal:clear"]);
    assert.deepEqual(
      items.map((i) => (i as { label?: string }).label ?? "|"),
      ["Copy", "Paste", "|", "Clear"],
    );
  });

  it("runs each row's own verb", () => {
    const d = deps(true);
    const items = terminalMenuItems(d);
    for (const item of items) (item as { run?: () => void }).run?.();
    assert.deepEqual(d.calls, ["copy", "paste", "clear"]);
  });

  it("disables Copy when there is no selection, and still opens (Paste and Clear can act)", () => {
    const items = terminalMenuItems(deps(false));
    assert.equal((items[0] as { disabled?: boolean }).disabled, true);
    assert.equal((items[1] as { disabled?: boolean }).disabled, undefined);
    assert.equal((items[3] as { disabled?: boolean }).disabled, undefined);
  });
});

describe("the wiring: the terminal tab registers into the shared store", () => {
  const tab = read("harness", "terminal", "ui", "TerminalTab.vue");

  it("the terminal surface's right-click is captured before the browser's menu", () => {
    assert.match(tab, /@contextmenu\.capture="onContextMenu"/);
    assert.match(tab, /function onContextMenu\(e: MouseEvent\)/);
    assert.match(tab, /terminalMenuItems\(\{[\s\S]*?hasSelection: t\.hasSelection\(\)/);
  });

  it("Copy reads xterm's selection through the one copy path", () => {
    assert.match(tab, /const text = t\.getSelection\(\)/);
    assert.match(tab, /copyText\(text\)/);
  });

  it("Paste hands the clipboard text to xterm's own paste, Clear drops the scrollback", () => {
    assert.match(tab, /await readClipboardText\(\)/);
    assert.match(tab, /t\.paste\(text\)/);
    assert.match(tab, /term\.value\?\.clear\(\)/);
  });

  it("the menu's key grammar is CAPTURE-phase, so a focused xterm cannot swallow Escape", () => {
    const surface = read("webui", "app", "components", "ui", "ContextMenu.vue");
    assert.match(surface, /window\.addEventListener\("keydown", onKey, true\)/);
    assert.match(surface, /handleContextMenuKey\(e\.key\)\) return;\s*\n\s*e\.preventDefault\(\);\s*\n\s*e\.stopPropagation\(\);/);
  });
});
