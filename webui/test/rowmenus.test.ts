// omega webui: the row right-click menus - sessions, agents, crons.
//
// RULING: EXISTING VERBS ONLY. Each builder offers exactly the verbs the tree already
// carries, and a verb that cannot act on its row is ABSENT (never a dead row): Stop needs a
// live run, Sleep needs an awake agent, Run now/Stop run need the live-fire mark, Delete
// needs a home the delete route owns, and the locked default agent carries no delete at
// all. The browser half is recipe 117.
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
    rename: () => log.push("rename"),
    branch: () => log.push("branch"),
    exportSession: () => log.push("export"),
    copyId: () => log.push("copy"),
    archive: () => log.push("archive"),
    remove: () => log.push("delete"),
    stop: () => log.push("stop"),
  };
}

describe("session rows: New session, Rename, Branch, Export, Copy id, Archive, Delete, Stop", () => {
  it("offers the seven stable verbs on an idle row, and no Stop", () => {
    const d = calls();
    const items = sessionMenuItems({ id: "s1", running: false, archived: false }, d);
    assert.deepEqual(ids(items), [
      "session:new",
      "session:rename",
      "separator",
      "session:branch",
      "session:export",
      "session:copy-id",
      "separator",
      "session:archive",
      "session:delete",
    ]);
    for (const item of items) if ((item as { id?: string }).id) (item as { run: () => void }).run();
    assert.deepEqual(d.log, ["new", "rename", "branch", "export", "copy", "archive", "delete"]);
  });

  it("reads Unarchive on an archived row - the verb follows the state", () => {
    const items = sessionMenuItems({ id: "s1", running: false, archived: true }, calls());
    const row = items.find((e) => (e as { id?: string }).id === "session:archive") as {
      label: string;
    };
    assert.equal(row.label, "Unarchive");
  });

  it("Copy id is client-side and not destructive", () => {
    const log: string[] = [];
    const items = sessionMenuItems(
      { id: "s_42", running: false },
      {
        newSession: () => {},
        rename: () => {},
        branch: () => {},
        exportSession: () => {},
        copyId: () => log.push("copy s_42"),
        archive: () => {},
        remove: () => {},
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

  it("Delete is destructive; Stop rides the live row only", () => {
    const idle = sessionMenuItems({ id: "s1", running: false }, calls());
    const del = idle.find((e) => (e as { id?: string }).id === "session:delete") as {
      danger?: boolean;
    };
    assert.equal(del.danger, true, "a delete wears the danger color");

    const d = calls();
    const items = sessionMenuItems({ id: "s1", running: true }, d);
    assert.deepEqual(ids(items).slice(-2), ["separator", "session:stop"]);
    const stop = items[items.length - 1] as { danger?: boolean; run: () => void };
    assert.equal(stop.danger, true, "a stop wears the danger color");
    stop.run();
    assert.deepEqual(d.log, ["stop"]);
  });
});

describe("agent rows: New agent, Sleep, Delete", () => {
  it("offers New agent, Sleep while awake, and Delete for a real agent", () => {
    const log: string[] = [];
    const items = agentMenuItems(
      { name: "scout", deletable: true, awake: true },
      {
        newAgent: () => log.push("new"),
        stop: () => log.push("sleep"),
        remove: () => log.push("delete"),
      },
    );
    assert.deepEqual(ids(items), [
      "agent:new",
      "separator",
      "agent:stop",
      "separator",
      "agent:delete",
    ]);
    for (const item of items) (item as { run?: () => void }).run?.();
    assert.deepEqual(log, ["new", "sleep", "delete"]);
  });

  it("drops Sleep on a sleeping agent - the verb cannot act", () => {
    const items = agentMenuItems(
      { name: "scout", deletable: true, awake: false },
      { newAgent: () => {}, stop: () => {}, remove: () => {} },
    );
    assert.deepEqual(ids(items), ["agent:new", "separator", "agent:delete"]);
  });

  it("never offers Delete on the locked default agent", () => {
    const items = agentMenuItems(
      { name: "main", deletable: false },
      { newAgent: () => {}, stop: () => {}, remove: () => {} },
    );
    assert.deepEqual(ids(items), ["agent:new"]);
  });
});

describe("cron rows: Run now or Stop run, Pause or Resume, Delete", () => {
  const deps = (log?: string[]) => ({
    runNow: () => log?.push("run"),
    stopRun: () => log?.push("stop"),
    toggle: () => log?.push("toggle"),
    remove: () => log?.push("delete"),
  });

  it("offers Run now on an idle job and Stop run on a live one", () => {
    const idle = cronMenuItems({ name: "digest", enabled: true }, deps());
    assert.equal((idle[0] as { id?: string }).id, "cron:run-now");
    const live = cronMenuItems({ name: "digest", enabled: true, running: true }, deps());
    assert.equal((live[0] as { id?: string }).id, "cron:stop-run");
    assert.equal((live[0] as { danger?: boolean }).danger, true, "a stop wears the danger color");
  });

  it("reads Pause on an enabled job and Resume on a paused one", () => {
    const enabled = cronMenuItems({ name: "digest", enabled: true }, deps());
    assert.equal((enabled[1] as { label: string }).label, "Pause");
    const paused = cronMenuItems({ name: "digest", enabled: false }, deps());
    assert.equal((paused[1] as { label: string }).label, "Resume");
  });

  it("wires the row's own run, toggle and delete writers", () => {
    const log: string[] = [];
    const items = cronMenuItems({ name: "digest", enabled: true }, deps(log));
    assert.deepEqual(ids(items), ["cron:run-now", "cron:toggle", "separator", "cron:delete"]);
    for (const item of items) (item as { run?: () => void }).run?.();
    assert.deepEqual(log, ["run", "toggle", "delete"]);
  });

  it("hides Delete on an agent-owned job - that file is not main's to delete", () => {
    const items = cronMenuItems(
      { name: "wake", enabled: true, agent_owned: true },
      deps(),
    );
    assert.deepEqual(ids(items), ["cron:run-now", "cron:toggle"]);
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
    assert.match(sidebar, /rename: \(\) => startRename\(s\.id\)/, "Rename is the row's inline editor");
    assert.match(sidebar, /archive: \(\) => emit\("archive", s\.id, !s\.archived\)/, "Archive is the row's own PATCH");
    assert.match(sidebar, /remove: \(\) => emit\("remove", s\.id\)/, "Delete is the shell's confirm");
    assert.match(sidebar, /copyId: \(\) => void copySessionId\(s\.id\)/, "Copy id rides the row's one copy path");
    assert.match(sidebar, /stopSessionRun\(/);
  });

  it("the agents list hands its builder to the shared RegistryList", () => {
    assert.match(agents, /:row-menu="rowMenu"/);
    assert.match(agents, /agentMenuItems\(/);
    assert.match(agents, /stop: \(\) => void sleep\(row\.name\)/, "Sleep is the wake control");
    assert.match(list, /@contextmenu="onRowMenu\(\$event, r\)"/);
    assert.match(list, /props\.rowMenu\?\.\(r\)/);
  });

  it("the cron row right-clicks into the cron builder and shows the status dot", () => {
    assert.match(cron, /@contextmenu="onRowMenu\(\$event, j\)"/);
    assert.match(cron, /cronMenuItems\(/);
    assert.match(cron, /runNow: \(\) => void applyRun\(j, runCronJobNow\)/);
    assert.match(cron, /stopRun: \(\) => void applyRun\(j, stopCronRun\)/);
    assert.match(cron, /cron-dot-\$\{cronDotOf\(j\)\}/, "the row wears its last fire's dot");
  });

  it("a surface with no rows registered leaves the browser's menu alone", () => {
    assert.match(
      list,
      /const items = props\.rowMenu\?\.\(r\) \?\? \[\];[\s\S]*?if \(items\.length === 0\) return;/,
      "no rows = the native menu stays",
    );
  });
});
