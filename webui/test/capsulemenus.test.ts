// omega webui: the secondary-capsule row menus - skills, tools, MCP, the file tree and the
// notifications feed.
//
// RULING: EXISTING VERBS ONLY. Every row is a route the capsule already owned or a pure
// client intent, and a verb that cannot act on its row is ABSENT (never a dead row): no
// toggle without the disabled seam, no Open/Copy/chat lane on a directory, no chat lane
// without a bound chat tab, no Clear all on an empty feed. The browser half is recipe 120.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { skillMenuItems } from "../../harness/skills/ui/skillMenu.ts";
import { toolMenuItems } from "../../harness/tools/ui/toolMenu.ts";
import { mcpMenuItems } from "../../harness/mcp/ui/mcpMenu.ts";
import { fileMenuItems } from "../../harness/files/ui/fileMenu.ts";
import { notificationMenuItems } from "../../harness/notifications/ui/notificationMenu.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(root, ...p), "utf8");

function ids(entries: readonly unknown[]): string[] {
  return entries.map((e) => (e as { id?: string }).id ?? "separator");
}

/** every deps slot logs its own name, so a builder that wires the wrong verb shows up. */
function calls(...names: string[]) {
  const log: string[] = [];
  const deps: Record<string, () => void> = {};
  for (const name of names) deps[name] = () => log.push(name);
  return { log, deps };
}

function fire(entries: readonly unknown[]): void {
  for (const entry of entries) (entry as { run?: () => void }).run?.();
}

describe("skills rows: Open, Enable or Disable, Delete", () => {
  it("offers the row's own three verbs, Delete wearing the danger color", () => {
    const c = calls("open", "toggle", "remove");
    const items = skillMenuItems({ disabled: false, toggleable: true }, c.deps);
    assert.deepEqual(ids(items), ["skill:open", "skill:toggle", "separator", "skill:delete"]);
    assert.equal((items[1] as { label: string }).label, "Disable");
    assert.equal((items[3] as { danger?: boolean }).danger, true);
    fire(items);
    assert.deepEqual(c.log, ["open", "toggle", "remove"]);
  });

  it("reads Enable on a disabled row - the verb follows the state", () => {
    const c = calls("open", "toggle", "remove");
    const items = skillMenuItems({ disabled: true, toggleable: true }, c.deps);
    assert.equal((items[1] as { label: string }).label, "Enable");
  });

  it("drops the toggle row on a lane with no disabled seam", () => {
    const c = calls("open", "toggle", "remove");
    const items = skillMenuItems({ disabled: false, toggleable: false }, c.deps);
    assert.deepEqual(ids(items), ["skill:open", "separator", "skill:delete"]);
  });
});

describe("tools rows: Enable or Disable", () => {
  it("offers the one verb the tab owns, never a separator", () => {
    const c = calls("toggle");
    const items = toolMenuItems({ disabled: false, toggleable: true }, c.deps);
    assert.deepEqual(ids(items), ["tool:toggle"]);
    assert.equal((items[0] as { label: string }).label, "Disable");
    fire(items);
    assert.deepEqual(c.log, ["toggle"]);
  });

  it("reads Enable on a disabled tool", () => {
    const c = calls("toggle");
    const items = toolMenuItems({ disabled: true, toggleable: true }, c.deps);
    assert.equal((items[0] as { label: string }).label, "Enable");
  });

  it("registers nothing without the seam - the browser's own menu stays", () => {
    const c = calls("toggle");
    assert.deepEqual(toolMenuItems({ disabled: false, toggleable: false }, c.deps), []);
  });
});

describe("MCP rows: Open, Edit, Enable or Disable, Delete", () => {
  it("offers Open, the entry Edit, the toggle and a danger Delete", () => {
    const c = calls("open", "edit", "toggle", "remove");
    const items = mcpMenuItems({ enabled: true }, c.deps);
    assert.deepEqual(ids(items), [
      "mcp:open",
      "mcp:edit",
      "mcp:toggle",
      "separator",
      "mcp:delete",
    ]);
    assert.equal((items[2] as { label: string }).label, "Disable");
    assert.equal((items[4] as { danger?: boolean }).danger, true);
    fire(items);
    assert.deepEqual(c.log, ["open", "edit", "toggle", "remove"]);
  });

  it("reads Enable on a disabled server", () => {
    const c = calls("open", "edit", "toggle", "remove");
    const items = mcpMenuItems({ enabled: false }, c.deps);
    assert.equal((items[2] as { label: string }).label, "Enable");
  });

  it("Edit rides the lossless save: the wire carries the whole entry", () => {
    const edit = read("harness", "mcp", "ui", "edit.ts");
    assert.match(edit, /"\/mcp\/save"/, "the form POSTs the existing upsert route");
    assert.match(edit, /env/, "the draft carries env");
    assert.match(edit, /headers/, "the draft carries headers");
    assert.match(edit, /args/, "the draft carries args");
  });
});

describe("file tree rows: New, Open, Copy, Copy path, Add file to chat", () => {
  it("offers every file verb when a chat tab is bound", () => {
    const c = calls("create", "open", "copy", "copyPath", "addToChat");
    const items = fileMenuItems({ isDir: false, chatBound: true }, c.deps);
    assert.deepEqual(ids(items), [
      "file:new",
      "file:open",
      "separator",
      "file:copy",
      "file:copy-path",
      "separator",
      "file:chat",
    ]);
    fire(items);
    assert.deepEqual(c.log, ["create", "open", "copy", "copyPath", "addToChat"]);
  });

  it("drops the chat lane with no bound chat tab", () => {
    const c = calls("create", "open", "copy", "copyPath", "addToChat");
    const items = fileMenuItems({ isDir: false, chatBound: false }, c.deps);
    assert.deepEqual(ids(items), [
      "file:new",
      "file:open",
      "separator",
      "file:copy",
      "file:copy-path",
    ]);
  });

  it("keeps a directory row to New and Copy path - Open/Copy/chat cannot act", () => {
    const c = calls("create", "open", "copy", "copyPath", "addToChat");
    const items = fileMenuItems({ isDir: true, chatBound: true }, c.deps);
    assert.deepEqual(ids(items), ["file:new", "separator", "file:copy-path"]);
  });
});

describe("notification rows: Clear all, and only that", () => {
  it("offers the feed's one verb on a populated feed", () => {
    const c = calls("clearAll");
    const items = notificationMenuItems({ count: 3, clearing: false }, c.deps);
    assert.deepEqual(ids(items), ["notif:clear-all"]);
    fire(items);
    assert.deepEqual(c.log, ["clearAll"]);
  });

  it("registers nothing on an empty feed or mid-clear", () => {
    const c = calls("clearAll");
    assert.deepEqual(notificationMenuItems({ count: 0, clearing: false }, c.deps), []);
    assert.deepEqual(notificationMenuItems({ count: 3, clearing: true }, c.deps), []);
  });
});

describe("the registration: each capsule hands its builder to the shell", () => {
  it("the skills list passes its builder as RegistryList's row-menu", () => {
    const skills = read("harness", "skills", "ui", "SkillsList.vue");
    assert.match(skills, /:row-menu="rowMenu"/);
    assert.match(skills, /skillMenuItems\(/);
    assert.match(skills, /startEdit\(row\.name, true\)/);
  });

  it("the tools list passes its builder as RegistryList's row-menu", () => {
    const tools = read("harness", "tools", "ui", "ToolsList.vue");
    assert.match(tools, /:row-menu="rowMenu"/);
    assert.match(tools, /toolMenuItems\(/);
  });

  it("the MCP rows right-click into their own builder and carry a detail", () => {
    const mcp = read("harness", "mcp", "ui", "McpServers.vue");
    const removeTs = read("harness", "mcp", "ui", "remove.ts");
    assert.match(mcp, /@contextmenu="onRowMenu\(\$event, s\)"/);
    assert.match(mcp, /mcpMenuItems\(/);
    assert.match(mcp, /mcp-confirm/, "Delete arms the detail's one confirm");
    assert.match(mcp, /deleteMcpServer\(/, "the delete rides its own helper");
    assert.match(removeTs, /"\/mcp\/delete"/);
  });

  it("the file tree rows right-click into their builder and the host owns New", () => {
    const tree = read("harness", "files", "ui", "FileTree.vue");
    const tab = read("harness", "files", "ui", "FilesTab.vue");
    assert.match(tree, /@contextmenu="onRowMenu\(\$event, entry\)"/);
    assert.match(tree, /fileMenuItems\(/);
    assert.match(tree, /@create="\(n\) => emit\('create', n\)"/, "a nested branch re-emits");
    assert.match(tab, /@create="onCreateNode"/);
    assert.match(tab, /@notice="\(m\) => \(toolError = m\)"/);
  });

  it("the notification records right-click into the feed's one verb", () => {
    const notif = read("harness", "notifications", "ui", "NotificationsPanel.vue");
    assert.match(notif, /@contextmenu="onRowMenu\(\$event\)"/);
    assert.match(notif, /notificationMenuItems\(/);
  });
});
