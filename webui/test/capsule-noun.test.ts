// omega webui: the CAPSULE NOUN LAW (card t_617c2ad0, the webui side of
// the seam constitution; the Go precedent is agent/seams_test.go's noun
// scans + scripts/nm-purity.sh). The tree must tell the truth: a
// capsule's UI lives in harness/<name>/ui/ (or api/<name>/ui/ for the
// serving shell) and is discovered by the build's directory scan - the
// shell's components dir carries NO file or directory NAMED for a
// capsule. Deleting the capsule is then mechanical: the codegen drops
// its manifest entry, the build prunes the component, the tree shows
// the removal.
//
// This pins the PATH law: walk webui/app/components/ and forbid every
// capsule noun in any path segment. Content references are shell
// vocabulary, not ownership - composables/dockables.ts names the
// capsule ids (the shell's own composition law), notificationFeed.ts /
// NotificationToasts.vue are the any-emitter toast machinery Endo
// ruled shell-owned (card t_617c2ad0 body), and shell files legitimately
// import the row model of the continuous capsule (AgentsPanel). What
// ownership is about here is the FILE HOME, and the file home is the
// path.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");

// the capsule nouns (the seam constitution's service names + the tab
// capsules). A path under webui/app/components/ carrying any of these
// (case-insensitive substring) is capsule UI squatting in the shell.
const CAPSULE_NOUNS = [
  "browser",
  "terminal",
  "notifications",
  "continuous",
  "kanban",
  "cron",
  "skills",
  "mcp",
  "tools",
  "memory",
  "trust",
  "verdicts",
];

// walk components/ bottom-up, POSIX-shaped relative paths
function walkComponents(dir: string, rel: string): string[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return [];
  }
  const paths: string[] = [];
  for (const e of entries.sort()) {
    if (e.startsWith(".")) continue; // tooling junk (.nuxt)
    const full = join(dir, e);
    const relp = rel === "" ? e : `${rel}/${e}`;
    if (e === "node_modules") continue; // symlinked by checkouts
    if (statSync(full).isDirectory()) {
      paths.push(...walkComponents(full, relp));
    } else {
      paths.push(relp);
    }
  }
  return paths;
}

describe("capsule noun law (webui)", () => {
  it("webui/app/components/ carries no capsule-named path", () => {
    const paths = walkComponents(join(root, "app", "components"), "");
    assert.ok(paths.length > 0, "the components tree walked empty - the walk is broken");
    const offenders: string[] = [];
    for (const p of paths) {
      const lowered = p.toLowerCase();
      for (const noun of CAPSULE_NOUNS) {
        if (lowered.includes(noun)) {
          offenders.push(`${p} carries the "${noun}" noun`);
        }
      }
    }
    assert.deepEqual(
      offenders,
      [],
      "capsule UI squats in the shell's components dir (card t_617c2ad0): a capsule's views live in harness/<name>/ui/",
    );
  });
});

// --- the two manifests (the codegen's panel/tab partition) ---------------

// module name extraction: the generated list's `{ module: "x" }` entries
function manifestModules(rel: string): string[] {
  const text = src(rel);
  return [...text.matchAll(/module: "([^"]+)"/g)].map((m) => m[1]);
}

describe("module-panels / module-tabs partition", () => {
  it("a capsule appears in exactly one manifest", () => {
    const panels = manifestModules("app/composables/module-panels.generated.ts");
    const tabs = manifestModules("app/composables/module-tabs.generated.ts");
    const seen = new Set<string>();
    for (const m of [...panels, ...tabs]) {
      assert.ok(!seen.has(m), `capsule "${m}" is listed twice across the manifests`);
      seen.add(m);
    }
  });

  it("module-tabs carries only the shell's tab capsules, one view each", () => {
    // TAB_MODULES is the shell's pinned-root composition (dockables.ts)
    const m = src("app/composables/dockables.ts");
    const tabDecl = m.match(/export const TAB_MODULES = \[([^\]]*)\]/);
    const tabDeclBody = tabDecl?.[1] ?? "";
    assert.ok(tabDeclBody.length > 0, "dockables.ts must declare TAB_MODULES");
    const tabModules = [...tabDeclBody.matchAll(/"([a-z_0-9]+)"/g)].map((x) => x[1]);
    assert.ok(tabModules.length > 0, "TAB_MODULES parsed empty");
    const tabs = manifestModules("app/composables/module-tabs.generated.ts");
    for (const t of tabs) {
      assert.ok(
        tabModules.includes(t),
        `"${t}" rides module-tabs but is not in TAB_MODULES - the shell never declared its composition`,
      );
    }
    assert.equal(
      new Set(tabs).size,
      tabs.length,
      "a tab capsule is listed twice in module-tabs",
    );
  });

  it("every tab capsule has a dockable registry entry", () => {
    const registryText = src("app/composables/dockables.ts");
    const app = src("app/app.vue");
    for (const t of ["terminal", "browser", "notifications"]) {
      const ids = [...registryText.matchAll(/^\s+"([a-z_0-9]+)",$/gm)].map((x) => x[1]);
      assert.ok(ids.includes(t), `dockables registry lost the "${t}" tab id`);
      assert.ok(
        app.includes(`t.module === "${t}"`),
        `app.vue stopped binding the "${t}" tab from the manifest - the hardcoded import is law-violating residue`,
      );
    }
  });

  it("app.vue hard-imports no capsule tab view", () => {
    const app = src("app/app.vue");
    for (const dead of [
      'from "./components/browser/',
      'from "./components/terminal/',
      'from "./components/notifications/',
    ]) {
      assert.ok(!app.includes(dead), `app.vue hard-imports capsule UI again: ${dead}`);
    }
  });
});