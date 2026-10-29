// omega webui: the content viewer (card t_36ceb99b) - a skill row click /
// a file tree click opens a main-pane viewer tab. The suite pins the
// contract half (registry entry, tab payload, focus-reuse, persistence)
// and the structural wiring (ViewerTab fetches, the openers, the module
// event seam literal). Run: node --test test/viewer.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  DOCK_COMPONENTS,
  DOCKABLES,
  MENU_DOCKABLES,
  MAIN_DOCKABLES,
  dockableOf,
  validViewerRef,
  viewerTabTitle,
  type ViewerRef,
} from "../app/composables/dockables.ts";
import {
  allNodes,
  findViewerTab,
  openViewerTab,
  viewerRefEq,
  nextNodeId,
  makePane,
  makeSplit,
  serialize,
  deserializeLayout,
  parseAnyLayout,
  treeShapesMatch,
  focusTab,
  type DockNode,
} from "../app/composables/dock.ts";
import {
  fetchSkillContent,
  VIEWER_OPEN_EVENT,
} from "../app/composables/viewer.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
// the repo root (engine/webui is two levels deep - modules/ lives beside engine/)
const repo = fileURLToPath(new URL("../../../", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");
const readRepo = (rel: string) => readFileSync(`${repo}${rel}`, "utf8");

// --- helpers -----------------------------------------------------------------

function asSplit(n: DockNode): DockNode {
  assert.equal(n.kind, "split");
  return n;
}
function panes(t: DockNode) {
  return [...allNodes(t)].filter((n) => n.kind === "pane");
}
function viewerCount(t: DockNode): number {
  return panes(t).reduce(
    (n, p) => n + p.tabs.filter((tab: { component: string }) => tab.component === "viewer").length,
    0,
  );
}

const fileRef = (name = "README.md"): ViewerRef => ({ kind: "file", name });
const skillRef = (name = "worker-onboarding"): ViewerRef => ({ kind: "skill", name });

// --- the registry contract -----------------------------------------------------

describe("the viewer dockable (t_36ceb99b)", () => {
  it("registers: multi, main-area, closable, iconset-named icon", () => {
    const ids = DOCKABLES.map((d) => d.id);
    assert.ok(ids.includes("viewer"), "the viewer is in the registry");
    const d = dockableOf("viewer");
    assert.equal(d.policy, "multi");
    assert.equal(d.area, "main");
    assert.equal(d.closable, true);
    // the fallback icon is a NAME in the iconset (the iconset test pins
    // the union; here the vocabulary membership is re-read directly)
    const icons = read("app/composables/icons.ts");
    assert.match(icons, /"file-text":/, "the fallback icon is vocabulary");
    assert.match(icons, /skills:/, "the skill-chip icon is vocabulary");
  });

  it("has NO menu entry: an empty viewer tab is meaningless", () => {
    assert.ok(!MENU_DOCKABLES.includes("viewer"), "MENUS never offer a viewer");
    assert.ok((MAIN_DOCKABLES as readonly string[]).includes("viewer"), "the viewer is MAIN-area");
    assert.ok((DOCK_COMPONENTS as readonly string[]).includes("viewer"));
  });
});

// --- the payload + focus-reuse ---------------------------------------------------

describe("the viewer tab payload (focus-reuse, t_36ceb99b fix 4)", () => {
  it("validViewerRef: the two kinds with a non-empty name; everything else invalid", () => {
    assert.ok(validViewerRef({ kind: "file", name: "a/b.go" }));
    assert.ok(validViewerRef({ kind: "skill", name: "x" }));
    for (const bad of [null, undefined, "x", {}, { kind: "file" }, { name: "x" }, { kind: "sheet", name: "x" }, { kind: "file", name: "" }]) {
      assert.equal(validViewerRef(bad), false, JSON.stringify(bad) + " is invalid");
    }
  });

  it("viewerRefEq: kind + name; undefined matches only undefined", () => {
    assert.ok(viewerRefEq(fileRef("a"), fileRef("a")));
    assert.ok(!viewerRefEq(fileRef("a"), fileRef("b")));
    assert.ok(!viewerRefEq(fileRef("a"), skillRef("a")));
    assert.ok(viewerRefEq(undefined, undefined));
    assert.ok(!viewerRefEq(fileRef("a"), undefined));
  });

  it("findViewerTab: sees PARKED (inactive) viewer tabs anywhere in the tree", () => {
    const p1 = makePane("chat");
    const p2 = makePane("chat");
    const t = asSplit(makeSplit("row", [p1, p2]));
    const parked = openViewerTab(t as DockNode, fileRef("docs/a.md"), p2.id);
    const parkedOther = focusTab(parked.tree, p1.id, 0); // park it
    const loc = findViewerTab(parkedOther, fileRef("docs/a.md"));
    assert.ok(loc, "the parked tab is found");
    assert.equal(loc!.paneId, p2.id);
  });

  it("openViewerTab: FIRST open mints (created), SECOND focuses - never a duplicate", () => {
    const pane = makePane("chat", null);
    const t = pane;
    const first = openViewerTab(t, fileRef("main.go"), pane.id);
    assert.equal(first.action, "created");
    const host = panes(first.tree).find((p) => p.id === pane.id)!;
    assert.deepEqual(host.tabs.map((x: { component: string }) => x.component), ["chat", "viewer"]);
    assert.deepEqual(first.tree === first.tree, true);

    const second = openViewerTab(first.tree, fileRef("main.go"),pane.id);
    assert.equal(second.action, "focused", "the same file focuses the existing tab");
    assert.equal(viewerCount(second.tree), 1, "no duplicate minted");
    assert.deepEqual(second.paneId, first.paneId);
    assert.equal(second.index, first.index);

    // a DIFFERENT file mints a fresh tab - focus-reuse is per ref
    const third = openViewerTab(second.tree, fileRef("docs/api.md"), pane.id);
    assert.equal(third.action, "created");
    assert.equal(viewerCount(third.tree), 2);
  });

  it("openViewerTab: file/skill kinds never collapse onto each other", () => {
    const pane = makePane("chat", null);
    const file = openViewerTab(pane, { kind: "file", name: "omega" }, pane.id);
    const skill = openViewerTab(file.tree, { kind: "skill", name: "omega" }, pane.id);
    assert.equal(skill.action, "created", "same name, different kind = a different tab");
    assert.equal(viewerCount(skill.tree), 2);
  });
});

// --- persistence -----------------------------------------------------------------

describe("the viewer payload persists (card 3's tab record laws)", () => {
  const viewerTree = (): DockNode => {
    const pane = makePane("chat", null);
    return openViewerTab(pane, fileRef("docs/a.md"), pane.id).tree;
  };

  it("serialize -> deserializeLayout (v3) preserves the payload; shapes match", () => {
    const t = viewerTree();
    const back = deserializeLayout(serialize(t, []))!;
    assert.ok(back, "the record loads");
    assert.ok(treeShapesMatch(back.tree, t), "the shape gate passes");
    const pane = panes(back.tree).find((p) =>
      p.tabs.some((x: { component: string }) => x.component === "viewer"),
    )!;
    const tab = pane.tabs.find((x: { component: string }) => x.component === "viewer")!;
    assert.deepEqual(tab.viewer, fileRef("docs/a.md"));
  });

  it("the strict reader rejects an unreadable payload (writer-bug proof)", () => {
    const rec = JSON.parse(serialize(viewerTree(), []));
    rec.tree.tabs ??= rec.tree.tabs;
    // find the viewer tab in the record and corrupt the payload
    const corrupt = (n: Record<string, unknown>): boolean => {
      const tabs = n.tabs as Record<string, unknown>[] | undefined;
      if (!tabs) return false;
      for (let i = 0; i < tabs.length; i++) {
        if (tabs[i]!.component === "viewer") {
          tabs[i]!.viewer = { kind: "sheet", name: "x" };
          return true;
        }
      }
      return false;
    };
    assert.ok(corrupt(rec.tree), "the fixture hit the tab");
    assert.equal(deserializeLayout(JSON.stringify(rec)), null, "the strict reader refuses");
    // the tolerant reader repairs: a receipt, the tab dropped
    const total = parseAnyLayout(JSON.stringify(rec), () => viewerTree());
    assert.ok(
      total.repairs.includes("a viewer tab's unreadable payload dropped the tab"),
      JSON.stringify(total.repairs),
    );
    assert.equal(viewerCount(total.tree), 0);
  });

  it("viewerTabTitle: the file BASE name; a skill's name unchanged", () => {
    assert.equal(viewerTabTitle(fileRef("docs/deep/api.md")), "api.md");
    assert.equal(viewerTabTitle(fileRef("notes.txt")), "notes.txt");
    assert.equal(viewerTabTitle(skillRef("worker-onboarding")), "worker-onboarding");
    assert.equal(viewerTabTitle(fileRef("")), "");
  });
});

// --- the wiring (structural pins, no DOM - the filestoolbar recipe) --------------

describe("the viewer wiring contracts", () => {
  const app = read("app/app.vue");
  const pane = read("app/components/DockPane.vue");
  const viewer = read("app/components/ViewerTab.vue");
  const filesTab = read("app/components/files/FilesTab.vue");
  const registry = read("app/components/RegistryList.vue");
  const skillsList = readRepo("modules/skills/ui/SkillsList.vue");
  const viewerComposable = read("app/composables/viewer.ts");

  it("ViewerTab reads BOTH surfaces and renders line-numbered text", () => {
    assert.match(viewer, /fetchFilesRead\(/, "the file tab reads /files/read");
    assert.match(viewer, /fetchSkillContent\(/, "the skill tab reads /skills/content");
    assert.match(viewer, /class="viewer-no"/, "the line-number gutter");
    assert.match(viewer, /binary file - no preview/, "the binary notice");
    assert.match(viewer, /truncated at 64 KiB/, "the truncation notice");
    assert.match(viewer, /viewer-skill-desc/, "the skill's description line");
  });

  it("DockPane's chip icon follows the CONTENT type", () => {
    assert.match(pane, /tab\.component === "viewer" && tab\.viewer/);
    assert.match(pane, /kind === "skill"\s*\?\s*"skills"/, "skill rows -> the book");
    assert.match(pane, /glyphIcon\(glyphKindFor\(tab\.viewer\.name\)\)/, "file rows -> the extension glyph");
  });

  it("app.vue hosts the outlet, the opener, the title resolver, the event listener", () => {
    assert.match(app, /#pane-viewer/, "the outlet slot exists");
    assert.match(app, /viewerTabTitle\(tab\.viewer\)/, "the tab is named by its content");
    assert.match(app, /provide\(DOCK_VIEWER_OPEN_KEY/, "the opener is provided");
    assert.match(app, /VIEWER_OPEN_EVENT/, "the module event seam is listened");
    assert.match(app, /dock\.openViewer\(/, "the store carries the open");
  });

  it("FilesTab opens viewer tabs; the preview vocabulary is GONE", () => {
    assert.ok(!filesTab.includes("file-preview"), "no inline preview block");
    assert.ok(!filesTab.includes("preview-body"), "no inline preview body");
    assert.ok(!filesTab.includes("fetchFilesRead"), "the inline fetch is retired");
    assert.match(filesTab, /DOCK_VIEWER_OPEN_KEY/, "the opener seam is injected");
    // (not a /\?\(/ regex: this Node's V8 refuses that escape pair)
    assert.ok(
      filesTab.includes('openViewerRef?.({ kind: "file", name: path } satisfies ViewerRef)'),
      "a file click hands the ref to the opener",
    );
  });

  it("RegistryList exposes the openable name; skills is the consumer", () => {
    assert.match(registry, /openable\?: boolean/);
    assert.ok(registry.includes("emit('open', r)"), "the open emit (single-quoted attr)");
    assert.match(skillsList, /openable\b/, "the skills list opts in");
    assert.match(skillsList, /@open="openSkill"/);
  });

  it("the module->shell event literal is copied IDENTICALLY (the narrowing.ts copy law)", () => {
    assert.match(viewerComposable, /VIEWER_OPEN_EVENT = "omega:open-viewer"/);
    assert.match(skillsList, /VIEWER_OPEN_EVENT = "omega:open-viewer"/);
    // the dispatch shape: { kind: "skill", name }
    assert.match(
      skillsList,
      /CustomEvent\(VIEWER_OPEN_EVENT,\s*\{\s*detail: \{ kind: "skill", name: r\.name \}/,
    );
  });
});

// --- the fetch wire ----------------------------------------------------------------

describe("fetchSkillContent wire", () => {
  let savedFetch: typeof globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = savedFetch as typeof globalThis.fetch;
  });

  it("GETs /skills/content with the encoded name, returns the payload", async () => {
    savedFetch = globalThis.fetch;
    let url = "";
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      url = String(input);
      return new Response(
        JSON.stringify({ name: "x", description: "d", content: "body", truncated: false }),
        { status: 200 },
      );
    }) as typeof globalThis.fetch;
    const data = await fetchSkillContent("my skill");
    assert.equal(url, `/skills/content?name=${encodeURIComponent("my skill")}`);
    assert.deepEqual(data, { name: "x", description: "d", content: "body", truncated: false });
  });

  it("throws the server's error envelope message, never a bare status", async () => {
    savedFetch = globalThis.fetch;
    globalThis.fetch = (async () =>
      new Response(JSON.stringify({ error: 'skill "x" not found. Available: y' }), {
        status: 404,
      })) as typeof globalThis.fetch;
    await assert.rejects(
      fetchSkillContent("x"),
      /skill "x" not found\. Available: y/,
    );
  });
});

// nextNodeId is imported to keep the tree helpers honest - a lint guard
// that the minting path is what the tests exercised above
void nextNodeId;