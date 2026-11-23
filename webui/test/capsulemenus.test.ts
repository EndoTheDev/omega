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
import { skillMenuItems, type SkillMenuDeps } from "../../harness/skills/ui/skillMenu.ts";
import { toolMenuItems, type ToolMenuDeps } from "../../harness/tools/ui/toolMenu.ts";
import { mcpMenuItems, type McpMenuDeps } from "../../harness/mcp/ui/mcpMenu.ts";
import { fileMenuItems, type FileMenuDeps } from "../../harness/files/ui/fileMenu.ts";
import {
  notificationMenuItems,
  type NotificationMenuDeps,
} from "../../harness/notifications/ui/notificationMenu.ts";

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
  // the intersection covers every builder this file drives; the slots it is handed are
  // named by each call, so a missing one fails at the build, not at the click.
  return {
    log,
    deps: deps as unknown as FileMenuDeps &
      SkillMenuDeps &
      NotificationMenuDeps &
      McpMenuDeps &
      ToolMenuDeps,
  };
}

function fire(entries: readonly unknown[]): void {
  for (const entry of entries) (entry as { run?: () => void }).run?.();
}

describe("skills rows: Open, Export, Enable or Disable, Archive, Delete", () => {
  it("offers the row's own verbs, Delete wearing the danger color", () => {
    const c = calls("open", "exportSkill", "toggle", "archive", "remove");
    const items = skillMenuItems({ disabled: false, toggleable: true }, c.deps);
    assert.deepEqual(ids(items), [
      "skill:open",
      "skill:export",
      "skill:toggle",
      "separator",
      "skill:archive",
      "skill:delete",
    ]);
    assert.equal((items[2] as { label: string }).label, "Disable");
    assert.equal((items[5] as { danger?: boolean }).danger, true);
    fire(items);
    assert.deepEqual(c.log, ["open", "exportSkill", "toggle", "archive", "remove"]);
  });

  it("reads Enable on a disabled row - the verb follows the state", () => {
    const c = calls("open", "exportSkill", "toggle", "archive", "remove");
    const items = skillMenuItems({ disabled: true, toggleable: true }, c.deps);
    assert.equal((items[2] as { label: string }).label, "Enable");
  });

  it("drops the toggle row on a lane with no disabled seam", () => {
    const c = calls("open", "exportSkill", "toggle", "archive", "remove");
    const items = skillMenuItems({ disabled: false, toggleable: false }, c.deps);
    assert.deepEqual(ids(items), [
      "skill:open",
      "skill:export",
      "separator",
      "skill:archive",
      "skill:delete",
    ]);
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

describe("file tree rows: New, Rename, Open, Copy, Copy path, Add file to chat, Delete", () => {
  it("offers every file verb when a chat tab is bound", () => {
    const c = calls("create", "rename", "open", "copy", "copyPath", "addToChat", "remove");
    const items = fileMenuItems({ isDir: false, chatBound: true }, c.deps);
    assert.deepEqual(ids(items), [
      "file:new",
      "file:rename",
      "file:open",
      "separator",
      "file:copy",
      "file:copy-path",
      "separator",
      "file:chat",
      "separator",
      "file:delete",
    ]);
    fire(items);
    assert.deepEqual(c.log, [
      "create",
      "rename",
      "open",
      "copy",
      "copyPath",
      "addToChat",
      "remove",
    ]);
  });

  it("drops the chat lane with no bound chat tab", () => {
    const c = calls("create", "rename", "open", "copy", "copyPath", "addToChat", "remove");
    const items = fileMenuItems({ isDir: false, chatBound: false }, c.deps);
    assert.deepEqual(ids(items), [
      "file:new",
      "file:rename",
      "file:open",
      "separator",
      "file:copy",
      "file:copy-path",
      "separator",
      "file:delete",
    ]);
  });

  it("keeps a directory row to New, Rename, Copy path and Delete", () => {
    const c = calls("create", "rename", "open", "copy", "copyPath", "addToChat", "remove");
    const items = fileMenuItems({ isDir: true, chatBound: true }, c.deps);
    assert.deepEqual(ids(items), [
      "file:new",
      "file:rename",
      "separator",
      "file:copy-path",
      "separator",
      "file:delete",
    ]);
  });

  it("Delete arms on the first pick, the card menu's one shape", () => {
    const c = calls("create", "rename", "open", "copy", "copyPath", "addToChat", "remove");
    const armed = fileMenuItems({ isDir: false, chatBound: false, armedDelete: true }, c.deps);
    const del = armed.find((e) => (e as { id?: string }).id === "file:delete") as {
      label: string;
    };
    assert.equal(del.label, "Confirm delete");
  });
});

describe("notification rows: Clear, then Clear all", () => {
  it("offers the record's own clear above the feed's clear-all", () => {
    const c = calls("clearOne", "clearAll");
    const items = notificationMenuItems({ id: 7 }, { count: 3, clearing: false }, c.deps);
    assert.deepEqual(ids(items), ["notif:clear", "separator", "notif:clear-all"]);
    fire(items);
    assert.deepEqual(c.log, ["clearOne", "clearAll"]);
  });

  it("registers nothing on an empty feed or mid-clear", () => {
    const c = calls("clearOne", "clearAll");
    assert.deepEqual(
      notificationMenuItems({ id: 7 }, { count: 0, clearing: false }, c.deps),
      [],
    );
    assert.deepEqual(
      notificationMenuItems({ id: 7 }, { count: 3, clearing: true }, c.deps),
      [],
    );
  });
});

describe("the registration: each capsule hands its builder to the shell", () => {
  it("the skills list passes its builder as RegistryList's row-menu", () => {
    const skills = read("harness", "skills", "ui", "SkillsList.vue");
    assert.match(skills, /:row-menu="rowMenu"/);
    assert.match(skills, /skillMenuItems\(/);
    assert.match(skills, /exportSkill: \(\) => exportSkill\(row\.name\)/, "Export downloads the folder");
    assert.match(skills, /archive: \(\) => void archiveSkill\(row\.name\)/, "Archive rides POST /skills/archive");
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

  it("the file tree rows right-click into their builder and the host owns New, Rename and Delete", () => {
    const tree = read("harness", "files", "ui", "FileTree.vue");
    const tab = read("harness", "files", "ui", "FilesTab.vue");
    assert.match(tree, /@contextmenu="onRowMenu\(\$event, entry\)"/);
    assert.match(tree, /fileMenuItems\(/);
    assert.match(tree, /rename: \(\) => emit\("rename", path\)/, "Rename hands the host the path");
    assert.match(tree, /remove: \(\) => onDeleteRow\(path\)/, "Delete arms, then emits");
    assert.match(tree, /@create="\(n\) => emit\('create', n\)"/, "a nested branch re-emits");
    assert.match(tree, /@rename="\(p\) => emit\('rename', p\)"/, "a nested branch re-emits the rename");
    assert.match(tree, /@delete="\(p\) => emit\('delete', p\)"/, "a nested branch re-emits the delete");
    assert.match(tab, /@create="onCreateNode"/);
    assert.match(tab, /@rename="onRenameNode"/);
    assert.match(tab, /@delete="onDeleteNode"/);
    assert.match(tab, /postFilesRename\(renameTarget\.value, name, sessionId\.value\)/);
    assert.match(tab, /postFilesDelete\(path, sessionId\.value\)/);
    assert.match(tab, /@notice="\(m\) => \(toolError = m\)"/);
  });

  it("the notification records right-click into the feed's own verbs", () => {
    const notif = read("harness", "notifications", "ui", "NotificationsPanel.vue");
    assert.match(notif, /@contextmenu="onRowMenu\(\$event, n\)"/);
    assert.match(notif, /notificationMenuItems\(/);
    assert.match(notif, /clearOne: \(\) => void clearRecord\(n\.id\)/, "Clear rides the record's DELETE");
    assert.match(notif, /clearAll: \(\) => void clearAllRecords\(\)/);
  });
});
