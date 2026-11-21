// omega webui: the row right-click menus - sessions, agents, crons.
//
// RULING: EXISTING VERBS ONLY. Each builder offers exactly the verbs the tree already
// carries, and a verb that cannot act on its row is ABSENT (never a dead row): Stop needs a
// live run, Delete needs a home the delete route owns, and the locked default agent carries
// no delete at all. The browser half is recipe 117.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { sessionMenuItems } from "../../harness/store/ui/sessionMenu.ts";
import { agentMenuItems } from "../../harness/continuous/ui/agentMenu.ts";
import { cronMenuItems } from "../../harness/cron/ui/cronMenu.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id?: string }).id ?? "separator");
}

function calls() {
  const log: string[] = [];
  return {
    log,
    newSession: () => log.push("new"),
    branch: () => log.push("branch"),
    exportSession: () => log.push("export"),
    copyId: () => log.push("copy"),
    stop: () => log.push("stop"),
  };
}

describe("session rows: New session, Branch, Export, Copy id, Stop", () => {
  it("offers the four stable verbs on an idle row, and no Stop", () => {
    const d = calls();
    const items = sessionMenuItems({ id: "s1", running: false }, d);
    assert.deepEqual(ids(items), [
      "session:new",
      "session:branch",
      "session:export",
      "session:copy-id",
    ]);
    for (const item of items) (item as { run: () => void }).run();
    assert.deepEqual(d.log, ["new", "branch", "export", "copy"]);
  });

  it("Copy id is client-side and not destructive", () => {
    const log: string[] = [];
    const items = sessionMenuItems(
      { id: "s_42", running: false },
      {
        newSession: () => {},
        branch: () => {},
        exportSession: () => {},
        copyId: () => log.push("copy s_42"),
        stop: () => {},
      },
    );
    const row = items.find((e) => (e as { id?: string }).id === "session:copy-id") as {
      label: string;
      run: () => void;
      danger?: boolean;
    };
    assert.equal(row.label, "Copy id");
    assert.equal(row.danger, undefined, "a copy is not destructive");
    row.run();
    assert.deepEqual(log, ["copy s_42"], "the row is the row's own copy path");
  });

  it("adds Stop on a running row - the route 404s on an idle one, so idle offers none", () => {
    const d = calls();
    const items = sessionMenuItems({ id: "s1", running: true }, d);
    assert.deepEqual(ids(items), [
      "session:new",
      "session:branch",
      "session:export",
      "session:copy-id",
      "separator",
      "session:stop",
    ]);
    const stop = items[items.length - 1] as { danger?: boolean; run: () => void };
    assert.equal(stop.danger, true, "a stop wears the danger color");
    stop.run();
    assert.deepEqual(d.log, ["stop"]);
  });
});

describe("agent rows: Delete, New agent", () => {
  it("offers New agent and Delete for a real agent", () => {
    const log: string[] = [];
    const items = agentMenuItems(
      { name: "scout", deletable: true },
      { newAgent: () => log.push("new"), remove: () => log.push("delete") },
    );
    assert.deepEqual(ids(items), ["agent:new", "separator", "agent:delete"]);
    for (const item of items) (item as { run?: () => void }).run?.();
    assert.deepEqual(log, ["new", "delete"]);
  });

  it("never offers Delete on the locked default agent", () => {
    const items = agentMenuItems(
      { name: "main", deletable: false },
      { newAgent: () => {}, remove: () => {} },
    );
    assert.deepEqual(ids(items), ["agent:new"]);
  });
});

describe("cron rows: Pause or Resume, Delete", () => {
  it("reads Pause on an enabled job and Resume on a paused one", () => {
    const enabled = cronMenuItems(
      { name: "digest", enabled: true },
      { toggle: () => {}, remove: () => {} },
    );
    assert.equal((enabled[0] as { label: string }).label, "Pause");
    const paused = cronMenuItems(
      { name: "digest", enabled: false },
      { toggle: () => {}, remove: () => {} },
    );
    assert.equal((paused[0] as { label: string }).label, "Resume");
  });

  it("wires the row's own toggle and delete writers", () => {
    const log: string[] = [];
    const items = cronMenuItems(
      { name: "digest", enabled: true },
      { toggle: () => log.push("toggle"), remove: () => log.push("delete") },
    );
    assert.deepEqual(ids(items), ["cron:toggle", "separator", "cron:delete"]);
    for (const item of items) (item as { run?: () => void }).run?.();
    assert.deepEqual(log, ["toggle", "delete"]);
  });

  it("hides Delete on an agent-owned job - that file is not main's to delete", () => {
    const items = cronMenuItems(
      { name: "wake", enabled: true, agent_owned: true },
      { toggle: () => {}, remove: () => {} },
    );
    assert.deepEqual(ids(items), ["cron:toggle"]);
  });
});

describe("the wiring: each row surface registers into the ONE shared menu", () => {
  const sidebar = read("harness", "store", "ui", "AppSidebar.vue");
  const agents = read("harness", "continuous", "ui", "AgentsPanel.vue");
  const cron = read("harness", "cron", "ui", "CronJobs.vue");
  const list = read("webui", "app", "components", "RegistryList.vue");

  it("the sessions row right-clicks into the session builder", () => {
    assert.match(sidebar, /@contextmenu="onRowMenu\(\$event, s\)"/);
    assert.match(sidebar, /sessionMenuItems\(/);
    assert.match(sidebar, /copyId: \(\) => void copySessionId\(s\.id\)/, "Copy id rides the row's one copy path");
    assert.match(sidebar, /stopSessionRun\(/);
  });

  it("the agents list hands its builder to the shared RegistryList", () => {
    assert.match(agents, /:row-menu="rowMenu"/);
    assert.match(agents, /agentMenuItems\(/);
    assert.match(list, /@contextmenu="onRowMenu\(\$event, r\)"/);
    assert.match(list, /props\.rowMenu\?\.\(r\)/);
  });

  it("the cron row right-clicks into the cron builder", () => {
    assert.match(cron, /@contextmenu="onRowMenu\(\$event, j\)"/);
    assert.match(cron, /cronMenuItems\(/);
  });

  it("a surface with no rows registered leaves the browser's menu alone", () => {
    assert.match(
      list,
      /const items = props\.rowMenu\?\.\(r\) \?\? \[\];[\s\S]*?if \(items\.length === 0\) return;/,
      "no rows = the native menu stays",
    );
  });
});
