// omega webui: unit tests for the EXTENSIONS inspector tab's pure row
// model (card t_14f73742 - the webui parity of the TUI /extensions).
// Run: node --test "test/*.test.ts" (node >= 23 runs TS natively).
// The endpoint's Go side is covered server-side; this pins the shell
// mapping: payload order preserved, has-ui derived from the
// composition manifest (module panel list), honest empty state.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  extensionsRows,
  extensionsSummary,
} from "../app/components/shell/extensionsRows.ts";
import type { OmegaExtension } from "../app/composables/omega.ts";

function ext(name: string, over: Partial<OmegaExtension> = {}): OmegaExtension {
  return { name, tools: 2, commands: 1, seams: ["tools"], ...over };
}

describe("extensionsRows", () => {
  it("preserves payload order (mount order) and maps fields", () => {
    const payload = [ext("logging"), ext("agent-loop"), ext("cron")];
    const rows = extensionsRows(payload, ["cron"]);
    assert.deepEqual(
      rows.map((r) => r.name),
      ["logging", "agent-loop", "cron"],
    );
    assert.equal(rows[0].tools, 2);
    assert.equal(rows[0].commands, 1);
    assert.deepEqual(rows[0].seams, ["tools"]);
  });

  it("derives has-ui from the module-panel list", () => {
    const payload = [ext("cron"), ext("provider"), ext("kanban")];
    const rows = extensionsRows(payload, ["cron", "http_channel", "kanban", "skills", "tools"]);
    assert.equal(rows[0].hasUi, true); // cron ships ui/dockable.json
    assert.equal(rows[1].hasUi, false); // provider has no panel
    assert.equal(rows[2].hasUi, true);
  });

  it("empty description carries an empty string (omitted on the wire)", () => {
    const rows = extensionsRows([ext("mystery", { description: undefined })], []);
    assert.equal(rows[0].description, "");
    assert.equal(rows[0].hasUi, false);
  });

  it("null seams on the wire become an empty array", () => {
    const rows = extensionsRows(
      [ext("weird", { seams: null as unknown as string[] })],
      [],
    );
    assert.deepEqual(rows[0].seams, []);
  });

  it("empty payload renders zero rows - the honest empty state", () => {
    assert.equal(extensionsRows([], ["cron", "kanban"]).length, 0);
  });
});

describe("extensionsSummary", () => {
  it("counts modules, tools, and commands across the load", () => {
    const rows = extensionsRows(
      [ext("a", { tools: 3, commands: 2 }), ext("b", { tools: 1, commands: 0 })],
      [],
    );
    assert.equal(extensionsSummary(rows), "2 modules - 4 tools, 2 commands");
  });

  it("singularizes one module", () => {
    assert.equal(extensionsSummary(extensionsRows([ext("a")], [])), "1 module - 2 tools, 1 command");
  });
});