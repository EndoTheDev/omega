// omega webui: the model-switch dialog's rules. The trigger arithmetic and the three
// outcomes are pinned here as pure functions (no DOM); the SFC pins then prove the wiring
// - the probe runs before any switch, the dialog is a real alertdialog with exactly three
// outcomes, and no "don't ask again" escape hatch exists.
// Run: node --test test/modelswitch.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";

import {
  planModelSwitch,
  runModelSwitchChoice,
  type ModelSwitchIO,
} from "../app/composables/modelSwitch.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (...parts: string[]) => readFileSync(join(root, ...parts), "utf8");

const chatTab = read("app", "components", "ChatTab.vue");
const dialog = read("app", "components", "chat", "ModelSwitchDialog.vue");
const app = read("app", "app.vue");

describe("the trigger: only a smaller window prompts", () => {
  it("a target that fits applies with no dialog", () => {
    assert.deepEqual(planModelSwitch({ tokens: 8000, context_window: 128000 }), {
      kind: "apply",
    });
    assert.deepEqual(planModelSwitch({ tokens: 8000, context_window: 8000 }), {
      kind: "apply",
    });
  });

  it("a smaller window prompts and carries the numbers", () => {
    assert.deepEqual(planModelSwitch({ tokens: 12000, context_window: 8192 }), {
      kind: "prompt",
      tokens: 12000,
      window: 8192,
      loss: 3808,
    });
  });

  it("no probe (no session, failed call) applies silently", () => {
    assert.deepEqual(planModelSwitch(null), { kind: "apply" });
  });

  it("an unreported window never prompts on its own", () => {
    assert.deepEqual(planModelSwitch({ tokens: 50000, context_window: 0 }), {
      kind: "apply",
    });
  });
});

describe("the three outcomes", () => {
  function io(log: string[], opts?: { failCompact?: boolean }): ModelSwitchIO {
    return {
      compact: async () => {
        log.push("compact");
        if (opts?.failCompact) throw new Error("compactor offline");
        return "compacted this conversation - 12 rows folded";
      },
      receipt: (line) => log.push(`receipt:${line}`),
      apply: () => log.push("apply"),
    };
  }

  it("compact and switch: the receipt lands BEFORE the switch", async () => {
    const log: string[] = [];
    const out = await runModelSwitchChoice("compact", io(log));
    assert.deepEqual(log, [
      "compact",
      "receipt:compacted this conversation - 12 rows folded",
      "apply",
    ]);
    assert.equal(out.applied, true);
    assert.match(out.receipt ?? "", /compacted/);
  });

  it("switch anyway: applies, never compacts", async () => {
    const log: string[] = [];
    const out = await runModelSwitchChoice("anyway", io(log));
    assert.deepEqual(log, ["apply"]);
    assert.equal(out.applied, true);
  });

  it("cancel: touches nothing", async () => {
    const log: string[] = [];
    const out = await runModelSwitchChoice("cancel", io(log));
    assert.deepEqual(log, []);
    assert.equal(out.applied, false);
  });

  it("a failed compact NEVER applies the switch (no silent truncation)", async () => {
    const log: string[] = [];
    const out = await runModelSwitchChoice(
      "compact",
      io(log, { failCompact: true }),
    );
    assert.deepEqual(log, ["compact"]);
    assert.equal(out.applied, false);
    assert.match(out.error ?? "", /compactor offline/);
  });
});

describe("the wiring (SFC source pins)", () => {
  it("the pick is probed, and only a fitting plan applies", () => {
    assert.match(chatTab, /fetchSessionContext\(id, v\)/, "the probe carries the target slot");
    assert.match(chatTab, /planModelSwitch\(await fetchSessionContext/, "the plan reads the probe");
    assert.match(
      chatTab,
      /if \(plan\.kind === "apply"\) \{\s*\n\s*applyModel\(v\);/,
      "the silent path applies through the plan, never around it",
    );
    const emits = chatTab.match(/emit\("update:model", v\)/g) ?? [];
    assert.equal(emits.length, 1, "one apply door (applyModel) in the whole tab");
  });

  it("the compact rides the existing /compact path", () => {
    assert.match(chatTab, /dispatchCommand\("\/compact", "", id\)/, "one compact door");
    assert.match(chatTab, /receipt: \(line\)/, "the receipt is landed, not dropped");
  });

  it("the dialog is a modal alertdialog with exactly three outcomes", () => {
    assert.match(dialog, /role="alertdialog"/);
    assert.match(dialog, /aria-modal="true"/);
    assert.match(dialog, /compact and switch/);
    assert.match(dialog, /switch anyway/);
    assert.match(dialog, /\n\s+cancel\n/, "the third outcome is a Cancel button");
    assert.match(dialog, /Teleport to="body"/, "the modal paints above the dock chrome");
  });

  it("the dialog shows current, window, and the loss", () => {
    assert.match(dialog, /\{\{ short\(tokens\) \}\}/);
    assert.match(dialog, /\{\{ short\(window\) \}\}/);
    assert.match(dialog, /\{\{ short\(loss\) \}\}/);
  });

  it("no 'don't ask again' preference exists anywhere in the path", () => {
    for (const src of [dialog, chatTab]) {
      assert.doesNotMatch(src, /don'?t ask/i);
      assert.doesNotMatch(src, /localStorage/, "the condition decides, never a stored choice");
    }
  });

  it("type-to-capture yields to the open model dialog", () => {
    assert.match(app, /modalOpen: !!deleteId\.value \|\| modelSwitchDialogOpen\.value/);
  });
});
