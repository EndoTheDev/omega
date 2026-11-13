// omega webui: the mobile DRAWER resolution law (card t_07ad4fda, widened by
// card t_572015db).
//
// Below 768px the three edge shells collapse into overlay drawers, and
// each drawer renders its active tab through a v-if/v-else-if chain that
// ends in a generic `<InspectorPanel :module="activeId">` catch-all. That
// catch-all resolves the PANEL manifest (module-panels.generated.ts) -
// fine for a module that ships an inspector panel body, a trap for every
// tab whose view lives elsewhere:
//
//   - a TAB capsule (TAB_MODULES: terminal / browser / notifications /
//     observability) rides module-tabs.generated.ts, not the panel list;
//   - a SHELL-owned view (files, settings, extensions, insights, agents)
//     is imported by app.vue and needs its own branch.
//
// A tab with no branch renders "module not loaded" for a tab that IS
// loaded (card t_e703180e fixed the Logs tab this way; card t_07ad4fda
// found the same shape on `terminal` - the bottom drawer's first default
// tab - and on `agents` in the right drawer).
//
// t_572015db settled the cross-host half. A tab is FREE CONTENT in the
// dock: any unlocked tab can be dragged into any pane, so a drawer's tab
// list (which reads the LIVE edge pane) can hold a tab no branch of that
// drawer covers - agents dragged into the bottom pane, terminal into the
// right one, any shell-owned view dragged anywhere. The app clamps instead
// of branching every permutation (chat and the viewer render from the tab
// payload a drawer chain does not carry): DRAWER_RENDERABLE is the id set
// each drawer actually resolves, the derivation keeps only those ids, and
// every edge-home tab stays reachable through its OWN drawer's floor.
//
// The bug is a WIDTH breakpoint, not a device: a desktop browser resized
// under 768px reproduces it. The node suite has no DOM, so the law is
// pinned as source assertions on app.vue plus the registry derivation that
// decides which ids a drawer can show, seat, and reach.
//
// Run: node --test "test/*.test.ts"
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  DRAWER_RENDERABLE,
  DRAWER_UNBRANCHED,
  drawerTabs,
} from "../app/composables/mobileDrawer.ts";
import {
  COMPONENT_TITLES,
  DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  TAB_MODULES,
  defaultHostTabs,
  hostsOf,
  type DockComponentName,
} from "../app/composables/dockables.ts";
import { scanModuleUIs } from "../app/composables/compose.ts";
import type { DockPane } from "../app/composables/dock.ts";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const app = readFileSync(join(root, "webui", "app", "app.vue"), "utf8");

/** the mobile drawers, keyed by their side: the shell whose edge pane it
    reads, the active-id ref the branch chain tests against, and the registry
    floor app.vue feeds the derivation (a source pin - the floor is what keeps
    an edge-home tab reachable when its live pane instance is elsewhere). */
const DRAWERS = {
  left: {
    shell: "sidebar",
    activeRef: "sidebarDrawerActiveId",
    floorArg: `drawerTabsFor("sidebar", ["sidebar"])`,
    floor: ["sidebar"] as readonly DockComponentName[],
  },
  right: {
    shell: "inspector",
    activeRef: "inspectorDrawerActiveId",
    floorArg: `drawerTabsFor("inspector", INSPECTOR_TAB_COMPONENTS)`,
    floor: INSPECTOR_TAB_COMPONENTS,
  },
  bottom: {
    shell: "bottom",
    activeRef: "bottomDrawerActiveId",
    floorArg: `drawerTabsFor("bottom", defaultHostTabs("bottom"))`,
    floor: defaultHostTabs("bottom"),
  },
} as const;

type DrawerSide = keyof typeof DRAWERS;
const SIDES = Object.keys(DRAWERS) as DrawerSide[];

/** the dock host name a drawer side stands for */
const HOST_OF_SIDE: Record<DrawerSide, string> = {
  left: "left",
  right: "right",
  bottom: "bottom",
};

/** the app.vue slice for one MobileDrawer: from its opening tag to its
    closing tag. The drawers are siblings, so the first `</MobileDrawer>`
    after the opening tag ends the block. */
function drawerBlock(side: DrawerSide): string {
  const chunks = app.split("<MobileDrawer").slice(1);
  const block = chunks
    .map((c) => c.slice(0, c.indexOf("</MobileDrawer>")))
    .find((c) => c.includes(`side="${side}"`));
  assert.ok(block, `app.vue mounts the ${side} drawer`);
  return block;
}

/** the ids a module PANEL manifest can resolve - a module with a ui/
    directory whose .vue files are panel bodies. TAB capsules ship a .vue
    too but are not panels (their dockable is one tab view). */
const PANEL_MODULES = new Set(
  scanModuleUIs(join(root, "harness"))
    .map((m) => m.module)
    .filter((m) => !(TAB_MODULES as readonly string[]).includes(m)),
);

/** the drawer's clamp, exactly as app.vue composes it: a declared branch id,
    or a module panel the generic catch-all honestly resolves. */
function canRender(side: DrawerSide, id: DockComponentName): boolean {
  return (
    (DRAWER_RENDERABLE[DRAWERS[side].shell] as readonly string[]).includes(id) ||
    PANEL_MODULES.has(id)
  );
}

/** every host that may keep an instance of `id` - the two registry fields the
    dock machinery reads (hostsOf's `homes`, and the fresh-seat `defaultHome`) */
function homesOf(id: DockComponentName): string[] {
  const entry = DOCKABLES.find((d) => d.id === id);
  if (!entry) throw new Error(`${id} is a registry entry`);
  return [...new Set<string>([...hostsOf(id), entry.defaultHome ?? entry.area])];
}

/** every id the drawer block branches on (`<ref> === '<id>'`) */
function branchIds(side: DrawerSide): string[] {
  const block = drawerBlock(side);
  const re = new RegExp(`${DRAWERS[side].activeRef}\\s*===\\s*'([a-z-]+)'`, "g");
  return [...block.matchAll(re)].map((m) => m[1]!);
}

function paneOf(tabs: DockComponentName[], edge: "left" | "right" | "bottom"): DockPane {
  return {
    kind: "pane",
    id: "n1",
    tabs: tabs.map((component) => ({ component })),
    active: 0,
    fixed: 240,
    edge,
  };
}

const title = (id: DockComponentName) => COMPONENT_TITLES[id] ?? id;

/** the drawer's clamp as a predicate, mirroring app.vue's drawerTabsFor */
function clamped(side: DrawerSide) {
  return (id: DockComponentName) => canRender(side, id);
}

test("every id a drawer can render carries its own branch, ahead of the catch-all", () => {
  for (const side of SIDES) {
    const block = drawerBlock(side);
    const catchAll = block.lastIndexOf("<InspectorPanel");
    assert.ok(catchAll > 0, `the ${side} drawer ends in a module catch-all`);
    for (const id of DRAWER_RENDERABLE[DRAWERS[side].shell]) {
      // the renderable map is a CLAIM, so it is asserted unconditionally:
      // declaring an id here and leaning on the catch-all would re-create
      // the lie this card removed ("files" is both a dock id and a panel
      // module name, and it must still carry its own FilesTab branch)
      const branch = new RegExp(`${DRAWERS[side].activeRef}\\s*===\\s*'${id}'`);
      assert.ok(
        branch.test(block),
        `the ${side} drawer carries no branch for '${id}' - it falls through to ` +
          `InspectorPanel(module="${id}") and renders "module not loaded"`,
      );
      assert.ok(
        block.search(branch) < catchAll,
        `the ${side} drawer's '${id}' branch must precede the InspectorPanel catch-all`,
      );
    }
  }
});

test("no drawer branch resolves an id the drawer has not declared renderable", () => {
  for (const side of SIDES) {
    assert.deepEqual(
      branchIds(side).sort(),
      [...DRAWER_RENDERABLE[DRAWERS[side].shell]].sort(),
      `the ${side} drawer's branches and DRAWER_RENDERABLE.${DRAWERS[side].shell} must ` +
        `agree - a branch outside the map is dead (the clamp never emits that id), a ` +
        `map entry without a branch is the "module not loaded" bug`,
    );
  }
});

test("every edge-home dockable is renderable in its home drawer", () => {
  for (const side of SIDES) {
    const host = HOST_OF_SIDE[side];
    for (const d of DOCKABLES) {
      if (!homesOf(d.id).includes(host)) continue;
      assert.ok(
        canRender(side, d.id),
        `'${d.id}' may live in the ${host} shell but the ${side} drawer cannot ` +
          `resolve it - add it to DRAWER_RENDERABLE.${DRAWERS[side].shell} with a ` +
          `branch, or let a module panel resolve it`,
      );
    }
  }
});

test("every edge-home dockable is reachable from its home drawer's floor", () => {
  // the drawer unions the live pane with the floor; a home tab absent from
  // the floor is stranded on mobile once the live pane holds something else
  for (const side of SIDES) {
    const host = HOST_OF_SIDE[side];
    for (const d of DOCKABLES) {
      if (!homesOf(d.id).includes(host)) continue;
      assert.ok(
        (DRAWERS[side].floor as readonly string[]).includes(d.id),
        `the ${side} drawer's floor omits '${d.id}', which may live in the ${host} ` +
          `shell - a drifted pane would strand it on mobile`,
      );
    }
  }
});

test("every openable dockable is triaged: drawer-rendered, panel-resolved, or declared unbranched", () => {
  const rendered = new Set(SIDES.flatMap((s) => [...DRAWER_RENDERABLE[DRAWERS[s].shell]]));
  for (const d of DOCKABLES) {
    const triaged =
      rendered.has(d.id) || PANEL_MODULES.has(d.id) || DRAWER_UNBRANCHED.includes(d.id);
    assert.ok(
      triaged,
      `'${d.id}' is in no drawer (DRAWER_RENDERABLE), no panel manifest and not in ` +
        `DRAWER_UNBRANCHED - a new dockable has to pick how the narrow viewport ` +
        `surfaces it`,
    );
  }
  // and the exemption list stays honest: an id declared unbranched must not
  // also be claimed renderable (or panel-resolved) somewhere
  for (const id of DRAWER_UNBRANCHED) {
    assert.ok(!rendered.has(id), `'${id}' is declared unbranched AND renderable`);
    assert.ok(!PANEL_MODULES.has(id), `'${id}' is declared unbranched AND panel-resolved`);
  }
});

test("each drawer derives its tabs through the clamp, never the raw edge read", () => {
  for (const side of SIDES) {
    assert.ok(
      app.includes(DRAWERS[side].floorArg),
      `the ${side} drawer must derive through drawerTabsFor(<shell>, <floor>) so the ` +
        `clamp and the floor union apply`,
    );
  }
  assert.ok(
    !app.includes("drawerTabsForEdge("),
    "app.vue must not read an edge pane raw: every drawer routes through drawerTabsFor " +
      "(the clamp is what keeps a drifted tab out of the active-id chain)",
  );
  assert.ok(app.includes("DRAWER_RENDERABLE"), "the shell reads the clamp map");
});

test("cross-host drag residual: a tab outside a drawer's branch set never surfaces as a chip", () => {
  // REPRO: agents' home is the right pane; dragged into the bottom pane and
  // the viewport narrowed, the bottom drawer showed an Agents chip whose
  // pane rendered "module not loaded". Same class: terminal into the right
  // pane, any shell view anywhere its branch does not exist.
  const bottom = drawerTabs(
    paneOf(["agents", "files"], "bottom"),
    defaultHostTabs("bottom"),
    title,
    clamped("bottom"),
  );
  assert.deepEqual(
    bottom.map((t) => t.id),
    ["files", "terminal", "observability"],
    "the bottom drawer keeps only what it can render, then its home floor",
  );

  const right = drawerTabs(
    paneOf(["terminal", "agents"], "right"),
    INSPECTOR_TAB_COMPONENTS,
    title,
    clamped("right"),
  );
  assert.ok(
    !right.some((t) => t.id === "terminal"),
    "a terminal dragged into the right pane is not a right-drawer chip",
  );
  assert.ok(
    right.some((t) => t.id === "agents"),
    "the agents view stays reachable from ITS home drawer",
  );

  // reachability is the other half of the law: the tab a drawer clamped is
  // still offered by the drawer of the host that owns it
  const bottomHome = drawerTabs(null, defaultHostTabs("bottom"), title, clamped("bottom"));
  assert.ok(bottomHome.some((t) => t.id === "terminal"), "terminal keeps its home drawer");
  const rightHome = drawerTabs(null, INSPECTOR_TAB_COMPONENTS, title, clamped("right"));
  assert.ok(rightHome.some((t) => t.id === "agents"), "agents keeps its home drawer");
});

test("a pane the drawer cannot render still opens on its home floor (never a blank drawer)", () => {
  const drifted = drawerTabs(
    paneOf(["chat", "viewer"], "bottom"),
    defaultHostTabs("bottom"),
    title,
    clamped("bottom"),
  );
  assert.deepEqual(drifted.map((t) => t.id), ["terminal", "observability"]);

  const empty = drawerTabs(paneOf([], "right"), INSPECTOR_TAB_COMPONENTS, title, clamped("right"));
  assert.ok(empty.length > 0, "an emptied edge pane still opens a usable drawer");
  for (const t of empty) {
    assert.ok(canRender("right", t.id), `${t.id}: every floor chip resolves in the chain`);
  }
});

test("the bottom drawer seats the terminal capsule from the tabs manifest (card t_07ad4fda)", () => {
  const block = drawerBlock("bottom");
  assert.ok(TAB_MODULES.includes("terminal"), "the terminal is a tab capsule");
  assert.ok(
    block.includes(':is="terminalTabView"'),
    "the drawer binds the capsule's view from module-tabs.generated.ts",
  );
  assert.ok(
    block.includes(`bottomDrawerActiveId === 'terminal'`),
    "the branch keys on the drawer's own active id",
  );
  assert.ok(
    block.includes(':focused-session="focusedSessionId"'),
    "the drawer copy keeps the focused-session claim the desktop outlet passes",
  );
  // ...and it seats BEFORE the catch-all, or the catch-all wins the chain
  assert.ok(
    block.indexOf(':is="terminalTabView"') < block.lastIndexOf("<InspectorPanel"),
    "the terminal branch precedes the InspectorPanel catch-all",
  );
});

test("the right drawer seats the agents view by its own branch (card t_07ad4fda)", () => {
  const block = drawerBlock("right");
  // agents is a SHELL-owned view: harness/continuous ships the component,
  // the shell imports it - it appears in no generated manifest
  assert.ok(
    block.includes("<AgentsPanel") &&
      block.includes(`inspectorDrawerActiveId === 'agents'`),
    "the drawer mounts AgentsPanel on its own branch",
  );
  assert.ok(
    block.indexOf("<AgentsPanel") < block.lastIndexOf("<InspectorPanel"),
    "the agents branch precedes the InspectorPanel catch-all",
  );
  assert.ok(
    app.includes('import AgentsPanel from "../../harness/continuous/ui/AgentsPanel.vue"'),
    "the shell owns the import (a capsule UI squatting in the shell would red the path law)",
  );
});

test("the drawers still end in exactly one InspectorPanel catch-all each", () => {
  for (const side of SIDES) {
    const block = drawerBlock(side);
    assert.equal(
      block.split("<InspectorPanel").length - 1,
      1,
      `the ${side} drawer keeps ONE module catch-all (it is the panel resolution, not a fallback for tabs)`,
    );
  }
});
