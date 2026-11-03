// omega webui: the Files tab boot race (card t_3e12c367). Two failure
// paths, one defect class: (1) the mount fetch goes out while focus is
// still unresolved and the focus watch's re-root refetch goes out
// milliseconds later - the STALE response can resolve after the fresh
// one's and clobber the header + tree (bare tree, cwd fallback, until a
// remount); (2) the boot focus carried the FIRST chat tab in tree order
// - a sessionless draft on the shipped default pane - so the
// focused-session seam never landed a session at all and the re-root
// watch never fired. Pins: the latest-wins round guard on loadRoot, the
// boot focus carrying the ACTIVE chat tab, and the no-session
// cwd-fallback law intact. Render-free; node's built-in test runner.
// Run: node --test test/filesboot.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import { latestWins } from "../app/composables/latestfetch.ts";
import { activeChatTab, makePane, makeSplit, openTab, focusTab } from "../app/composables/dock.ts";
import { filesHeaderVisible } from "../app/composables/omega.ts";
import type { DockNode, DockTab } from "../app/composables/dock.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

describe("the boot race's latest-wins guard (composables/latestfetch.ts)", () => {
  it("the newest request owns the write: a newer begin revokes older rounds", () => {
    const rounds = latestWins();
    const stale = rounds.begin();
    const fresh = rounds.begin();
    // the boot fetch (stale) and the re-root refetch (fresh) both fly;
    // whichever resolves last must not clobber the newest REQUEST
    assert.equal(stale(), false, "a superseded round is never admitted");
    assert.equal(fresh(), true, "the latest round is admitted");
  });

  it("every newer begin revokes ALL older rounds, regardless of resolve order", () => {
    const rounds = latestWins();
    const a = rounds.begin();
    const b = rounds.begin();
    const c = rounds.begin();
    assert.equal(a(), false);
    assert.equal(b(), false);
    assert.equal(c(), true);
  });

  it("a sole round admits (the no-race boot path keeps its settle flags)", () => {
    const rounds = latestWins();
    const only = rounds.begin();
    assert.equal(only(), true);
    assert.equal(only(), true, "admission is stable until a newer begin");
  });
});

describe("boot focus carries the ACTIVE chat tab (dock.activeChatTab)", () => {
  const sessionTab = (id: string, key: string): DockTab => ({
    component: "chat",
    sessionRef: id,
    key,
  });
  const withSessionTab = (active: number): { tree: DockNode; paneId: string } => {
    // the shipped default pane: a sessionless draft at index 0, the
    // user's session bound at index 1 (the endo repro shape)
    let tree: DockNode = makePane("chat");
    const paneId = (tree as { id: string }).id;
    tree = openTab(tree, paneId, sessionTab("sess-1", "k1"));
    tree = focusTab(tree, paneId, active);
    return { tree, paneId };
  };

  it("the session tab active beside a draft: focus follows what is SHOWN", () => {
    const { tree, paneId } = withSessionTab(1);
    const loc = activeChatTab(tree);
    assert.ok(loc, "an active chat tab is found");
    assert.equal(loc!.paneId, paneId);
    assert.equal(loc!.index, 1, "the ACTIVE tab, not the first in tree order");
    assert.equal(loc!.tab.sessionRef, "sess-1");
  });

  it("the draft active (session tab backgrounded): the draft carries - no session focus", () => {
    const { tree } = withSessionTab(0);
    const loc = activeChatTab(tree);
    assert.ok(loc);
    assert.equal(loc!.index, 0);
    assert.equal(loc!.tab.sessionRef, undefined);
  });

  it("no chat tab anywhere: null (board-only layout keeps no session focus)", () => {
    assert.equal(activeChatTab(makePane("board")), null);
  });

  it("depth-first across panes: the pane's own active tab wins", () => {
    const boardPane = makePane("board");
    const chatPane = makePane("chat");
    const tree = makeSplit("row", [boardPane, openTab(chatPane, chatPane.id, sessionTab("sess-2", "k2"))]);
    const loc = activeChatTab(tree);
    assert.ok(loc);
    assert.equal(loc!.paneId, chatPane.id);
    assert.equal(loc!.tab.sessionRef, "sess-2");
  });
});

describe("the fix wires itself into the boot paths (structural pins)", () => {
  const tab = read("app/components/files/FilesTab.vue");
  const app = read("app/app.vue");
  const dock = read("app/composables/dock.ts");

  it("loadRoot's writes are gated by the latest-wins guard", () => {
    assert.match(tab, /const rootRounds = latestWins\(\)/);
    assert.match(tab, /const admitted = rootRounds\.begin\(\)/);
    assert.match(tab, /if \(!admitted\(\)\) return; \/\/ a newer root request owns the state/);
    assert.match(tab, /if \(admitted\(\)\) loadingRoot\.value = false/);
    // the carry is unchanged: every browse still asks with the session id
    assert.match(tab, /fetchFilesBrowse\(\s*"",\s*sessionId\.value/);
  });

  it("every boot branch carries the ACTIVE chat tab, not the first", () => {
    // the URL-carried fallback branch, the unknown-id branch, the home
    // branch, and refocusFrom's keep-valid-focus fallback: 4 sites, one law
    const carries = app.match(/dock\.activeChatTab\(\) \?\? dock\.firstChatTab\(\)/g) ?? [];
    assert.ok(carries.length >= 4, `expected >=4 active-first carry sites, got ${carries.length}`);
  });

  it("dock exports activeChatTab and the DockApi exposes it", () => {
    assert.match(dock, /export function activeChatTab\(/);
    assert.match(dock, /activeChatTab: \(\) => ChatTabLocation \| null/);
    assert.match(dock, /activeChatTab: \(\) => activeChatTab\(tree\.value\)/);
  });

  it("the no-session cwd-fallback law stays INTACT (fresh boot, no sessions)", () => {
    // a boot with no focused session keeps the deliberate bare tree:
    // the fallback render is only wrong when a focused session EXISTS
    assert.equal(filesHeaderVisible("cwd"), false);
    assert.equal(filesHeaderVisible(undefined), false);
    assert.equal(filesHeaderVisible("session"), true);
  });
});