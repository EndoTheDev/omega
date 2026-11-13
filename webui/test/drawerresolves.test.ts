// omega webui: the mobile DRAWER resolution law (card t_07ad4fda).
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
// The bug is a WIDTH breakpoint, not a device: a desktop browser resized
// under 768px reproduces it. The node suite has no DOM, so the law is
// pinned as source assertions on app.vue plus the registry derivation
// that decides which ids a drawer can actually show.
//
// Run: node --test "test/*.test.ts"
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  INSPECTOR_TAB_COMPONENTS,
  TAB_MODULES,
  defaultHostTabs,
} from "../app/composables/dockables.ts";
import { scanModuleUIs } from "../app/composables/compose.ts";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const app = readFileSync(join(root, "webui", "app", "app.vue"), "utf8");

/** the mobile drawers, keyed by their side: the active-id ref the
    branch chain tests against, plus the ids the drawer can show. */
const DRAWERS = {
  left: {
    activeRef: "sidebarDrawerActiveId",
    ids: ["sidebar"] as readonly string[],
  },
  right: {
    activeRef: "inspectorDrawerActiveId",
    ids: INSPECTOR_TAB_COMPONENTS as readonly string[],
  },
  bottom: {
    activeRef: "bottomDrawerActiveId",
    ids: defaultHostTabs("bottom") as readonly string[],
  },
} as const;

/** the app.vue slice for one MobileDrawer: from its opening tag to its
    closing tag. The drawers are siblings, so the first `</MobileDrawer>`
    after the opening tag ends the block. */
function drawerBlock(side: keyof typeof DRAWERS): string {
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

test("every drawer tab that is not a module panel resolves by its own branch (card t_07ad4fda)", () => {
  for (const [side, drawer] of Object.entries(DRAWERS)) {
    const block = drawerBlock(side as keyof typeof DRAWERS);
    for (const id of drawer.ids) {
      if (PANEL_MODULES.has(id)) continue; // the InspectorPanel catch-all is the honest resolution
      const branch = new RegExp(
        `${drawer.activeRef}\\s*===\\s*'${id}'`,
      );
      assert.ok(
        branch.test(block),
        `the ${side} drawer carries no branch for '${id}' - it falls through to ` +
          `InspectorPanel(module="${id}") and renders "module not loaded"`,
      );
    }
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
  for (const side of ["left", "right", "bottom"] as const) {
    const block = drawerBlock(side);
    assert.equal(
      block.split("<InspectorPanel").length - 1,
      1,
      `the ${side} drawer keeps ONE module catch-all (it is the panel resolution, not a fallback for tabs)`,
    );
  }
});
