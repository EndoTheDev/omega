// Module-UI composition scan.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import type { DockableSpec } from "./dockables";
// a declared icon is a NAME in the webui iconset - membership is checked at build time
// so a typo never reaches render
import { ICON_NAMES } from "./icons.ts";

// One discovered module UI directory.
export interface ModuleUI {
  module: string;
  dir: string;
  // component file names shipped there, sorted
  components: string[];
  dockable: DockableSpec | null;
}

// scanModuleUIs returns every candidate dir under ANY of the given parent roots that
// carries a <name>/ui/ directory with at least one.vue file, sorted by module name. A
// nonexistent root yields nothing (the tree never sees it; the clean-clone build is
// what runs the scan for real). A module name found in an earlier root wins - harness/
// is passed first, so a shell dir can never shadow an opinionated module's panel.
// Sorted twice - directory entries AND results - so the generated panel list is stable
// regardless of readdir order.
export function scanModuleUIs(...roots: string[]): ModuleUI[] {
  const byName = new Map<string, ModuleUI>();
  for (const root of roots) {
    let names: string[];
    try {
      names = readdirSync(root);
    } catch {
      continue;
    }
    for (const name of names.sort()) {
      // dot/underscore dirs are tooling junk (.nuxt,.worktrees); node_modules never.
      if (name === "webui" || name === "node_modules" || name === "modules" ||
          name.startsWith(".") || name.startsWith("_")) {
        continue;
      }
      if (byName.has(name)) continue;
      const dir = join(root, name, "ui");
      if (!existsSync(dir) || !statSync(dir).isDirectory()) continue;
      const components = readdirSync(dir)
        .filter((f) => f.endsWith(".vue"))
        .sort();
      if (components.length === 0) continue;
      byName.set(name, { module: name, dir, components, dockable: readDockableSpec(dir) });
    }
  }
  return [...byName.values()].sort((a, b) => a.module.localeCompare(b.module));
}

// readDockableSpec parses + validates ui/dockable.json - the dockable contract.
// Strictly typed and total: anything unreadable, malformed, or of the wrong shape is
// null (the scan must never throw on module content it does not own).
const DOCKABLE_POLICIES = new Set(["singleton", "multi"]);

function validIcon(v: unknown): boolean {
  return typeof v === "string" &&
    v !== "" &&
    (ICON_NAMES as readonly string[]).includes(v);
}

function readDockableSpec(dir: string): DockableSpec | null {
  const path = join(dir, "dockable.json");
  if (!existsSync(path)) return null;
  try {
    const raw: unknown = JSON.parse(readFileSync(path, "utf8"));
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
    const d = raw as { title?: unknown; icon?: unknown; policy?: unknown };
    if (d.title !== undefined && (typeof d.title !== "string" || d.title === "")) return null;
    if (d.policy !== undefined && !DOCKABLE_POLICIES.has(d.policy as string)) return null;
    if (d.icon !== undefined && !validIcon(d.icon)) return null;
    return raw as DockableSpec;
  } catch {
    return null;
  }
}
