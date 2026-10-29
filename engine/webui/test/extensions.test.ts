// omega webui: unit tests for the EXTENSIONS inspector tab's pure row
// model (card t_14f73742 - the webui parity of the TUI /extensions;
// card t_210a0d19 - the tab becomes the module lifecycle cockpit).
// Run: node --test "test/*.test.ts" (node >= 23 runs TS natively).
// The endpoint's Go side is covered server-side; this pins the shell
// merge: payload order preserved, lifecycle fused from three sources
// (GET /extensions inventory, GET /modules introspection, the
// composition manifest), the expansion detail (tools, commands,
// panels), honest empty states.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  extensionsRows,
  extensionsSummary,
  type PanelRef,
} from "../app/components/shell/extensionsRows.ts";
import type { OmegaExtension, ModuleRow } from "../app/composables/omega.ts";

function ext(name: string, over: Partial<OmegaExtension> = {}): OmegaExtension {
  return {
    name,
    tools: 2,
    commands: 1,
    seams: ["tools"],
    tool_names: ["tool_a", "tool_b"],
    command_names: ["/cmd"],
    ...over,
  };
}

function mod(name: string, over: Partial<ModuleRow> = {}): ModuleRow {
  return { name, state: "mounted", toggleable: true, ...over };
}

const PANELS: PanelRef[] = [
  { module: "cron", title: "Cron" },
  { module: "kanban", title: "Kanban" },
];

describe("extensionsRows", () => {
  it("preserves payload order (mount order) and maps fields", () => {
    const payload = [ext("logging"), ext("agent-loop"), ext("cron")];
    const rows = extensionsRows(payload, PANELS, []);
    assert.deepEqual(
      rows.map((r) => r.name),
      ["logging", "agent-loop", "cron"],
    );
    assert.equal(rows[0].tools, 2);
    assert.equal(rows[0].commands, 1);
    assert.deepEqual(rows[0].seams, ["tools"]);
  });

  it("carries the expansion detail verbatim (tools, commands)", () => {
    // THE component contract (card t_210a0d19 acceptance): a seeded
    // module's expanded row lists exactly these names - the template
    // renders row.toolNames / row.commandNames joined, nothing else.
    const rows = extensionsRows(
      [ext("skills", {
        tools: 1,
        commands: 1,
        tool_names: ["skills"],
        command_names: ["/skills"],
      })],
      [],
      [],
    );
    assert.deepEqual(rows[0].toolNames, ["skills"]);
    assert.deepEqual(rows[0].commandNames, ["/skills"]);
  });

  it("folds missing name arrays to empty (pre-t_210a0d19 host)", () => {
    const rows = extensionsRows(
      [ext("old", { tool_names: undefined, command_names: undefined })],
      [],
      [],
    );
    assert.deepEqual(rows[0].toolNames, []);
    assert.deepEqual(rows[0].commandNames, []);
  });

  it("empty description carries an empty string (omitted on the wire)", () => {
    const rows = extensionsRows(
      [ext("mystery", { description: undefined })],
      [],
      [],
    );
    assert.equal(rows[0].description, "");
    assert.equal(rows[0].hasUi, false);
  });

  it("null seams on the wire become an empty array", () => {
    const rows = extensionsRows(
      [ext("weird", { seams: null as unknown as string[] })],
      [],
      [],
    );
    assert.deepEqual(rows[0].seams, []);
  });

  it("derives has-ui and the panel list from the composition manifest", () => {
    const payload = [ext("cron"), ext("provider"), ext("kanban")];
    const rows = extensionsRows(payload, PANELS, []);
    assert.equal(rows[0].hasUi, true); // cron ships ui/dockable.json
    assert.deepEqual(rows[0].panels, ["Cron"]);
    assert.equal(rows[1].hasUi, false); // provider has no panel
    assert.deepEqual(rows[1].panels, []);
    assert.equal(rows[2].hasUi, true);
    assert.deepEqual(rows[2].panels, ["Kanban"]);
  });

  it("merges the registry introspection: live state + toggle + dependents", () => {
    const rows = extensionsRows(
      [ext("kanban"), ext("store"), ext("http_channel")],
      [],
      [
        mod("kanban", { dependents: ["cron"] }),
        mod("store", { toggleable: false }),
      ],
    );
    // a module both endpoints know: ONE row, the registry's lifecycle
    assert.equal(rows.filter((r) => r.name === "kanban").length, 1);
    assert.equal(rows[0].state, "mounted");
    assert.equal(rows[0].toggleable, true);
    assert.deepEqual(rows[0].dependents, ["cron"]);
    // machine-critical: same, with the always-on law
    assert.equal(rows[1].state, "mounted");
    assert.equal(rows[1].toggleable, false);
    // a module the introspection never lists = engine ring (tier law)
    assert.equal(rows[2].state, "engine");
    assert.equal(rows[2].toggleable, false);
  });

  it("appends disabled modules the inventory never listed - one row, zero detail", () => {
    const rows = extensionsRows(
      [ext("kanban")],
      [],
      [mod("kanban"), mod("cron", { state: "disabled" })],
    );
    assert.equal(rows.length, 2);
    // mounted first (payload order), disabled after (registry order)
    assert.equal(rows[0].name, "kanban");
    assert.equal(rows[0].state, "mounted");
    assert.equal(rows[1].name, "cron");
    assert.equal(rows[1].state, "disabled");
    assert.equal(rows[1].tools, 0);
    assert.deepEqual(rows[1].toolNames, []); // nothing mounted to expand
    assert.deepEqual(rows[1].commandNames, []);
    // the disabled row still toggles (that is how it comes back)
    assert.equal(rows[1].toggleable, true);
  });

  it("empty payloads render zero rows - the honest empty state", () => {
    assert.equal(extensionsRows([], [], []).length, 0);
  });

  it("a disabled-only load still rows (the cockpit must show the off switch)", () => {
    const rows = extensionsRows([], [], [mod("cron", { state: "disabled" })]);
    assert.equal(rows.length, 1);
    assert.equal(rows[0].state, "disabled");
  });
});

describe("extensionsSummary", () => {
  it("counts modules, tools, and commands across the load", () => {
    const rows = extensionsRows(
      [ext("a", { tools: 3, commands: 2 }), ext("b", { tools: 1, commands: 0 })],
      [],
      [],
    );
    assert.equal(extensionsSummary(rows), "2 modules - 4 tools, 2 commands");
  });

  it("singularizes one module", () => {
    assert.equal(extensionsSummary(extensionsRows([ext("a")], [], [])), "1 module - 2 tools, 1 command");
  });

  it("disabled rows contribute zero tools (nothing is mounted)", () => {
    const rows = extensionsRows(
      [ext("a")],
      [],
      [mod("a"), mod("off", { state: "disabled" })],
    );
    assert.equal(extensionsSummary(rows), "2 modules - 2 tools, 1 command");
  });
});