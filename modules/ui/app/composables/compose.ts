// Module-UI composition scan. THE WHOLE MECHANISM: a module ships UI by
// putting Vue single-file components in modules/<name>/ui/. No manifest,
// no runtime registry, no plugin protocol - a directory is the
// registration, and only the top-level .vue files of that directory are
// panels. The build reads this at nuxt-config-eval time; the shell has
// one generated panel list and one mount point (see nuxt.config.ts, the
// generated module-panels.generated.ts, and the inspector's mount).
// Pure on purpose: the unit suite (test/composition.test.ts) exercises
// this against scratch dirs with no Nuxt involved.

import { existsSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// One discovered module UI directory.
export interface ModuleUI {
  // module directory name; also the auto-import namespace prefix
  module: string;
  // absolute path of the module's ui/ directory
  dir: string;
  // component file names shipped there, sorted
  components: string[];
}

// scanModuleUIs returns every dir under modulesRoot that carries a
// modules/<name>/ui/ directory with at least one .vue file, sorted by
// module name. A nonexistent modulesRoot yields [] (the dev tree never
// sees it; the clean-clone build is what runs the scan for real).
// Sorted twice - directory entries AND results - so the generated panel
// list is stable regardless of readdir order.
export function scanModuleUIs(modulesRoot: string): ModuleUI[] {
  let names: string[];
  try {
    names = readdirSync(modulesRoot);
  } catch {
    return [];
  }
  const found: ModuleUI[] = [];
  for (const name of names.sort()) {
    // dot/underscore dirs are tooling junk (.nuxt); node_modules never
    // ships omega UI; "ui" is the shell module itself - registering the
    // shell inside its own shell would be a loop, the shell ships its
    // UI in place
    if (name === "ui" || name === "node_modules" ||
        name.startsWith(".") || name.startsWith("_")) {
      continue;
    }
    const dir = join(modulesRoot, name, "ui");
    if (!existsSync(dir) || !statSync(dir).isDirectory()) continue;
    const components = readdirSync(dir)
      .filter((f) => f.endsWith(".vue"))
      .sort();
    if (components.length === 0) continue;
    found.push({ module: name, dir, components });
  }
  return found;
}

