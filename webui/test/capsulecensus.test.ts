// omega webui: the capsule-UI census.
//
// Capsule UIs live outside webui/ (harness/<name>/ui), so the webui dead-export pass
// reads their shell imports as orphans. The census closes the gap from the consumer
// side: it derives the module set from the capsule sources themselves, so a new capsule
// import enters the census with no edit here.
//
// The bridge table below is the other half. knip only analyzes files inside its
// workspace (webui/), so a shell export whose consumers are all capsule UIs is reported
// as dead; a named import in this file makes the test itself the in-workspace consumer.
// The table must equal what the capsule UIs import and webui does not, so a new capsule
// import of the feed store reds here until the bridge names it.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  clearAll,
  clearOne,
  markAllRead,
  markRecordRead,
  notificationsList,
  notificationsUnread,
} from "../app/components/shell/notificationFeed.ts";

import {
  clauseNames,
  exportedNames,
  resolveSpecifier,
  scanImports,
  type ScannedImport,
} from "./capsulecensus.ts";

const webuiRoot = fileURLToPath(new URL("..", import.meta.url));
const appDir = path.join(webuiRoot, "app");
const repoRoot = path.resolve(webuiRoot, "..");
const harnessRoot = path.join(repoRoot, "harness");

// the in-workspace consumer knip cannot reach: module -> its imported names
const BRIDGE: Readonly<Record<string, Readonly<Record<string, unknown>>>> = {
  "components/shell/notificationFeed.ts": {
    clearAll,
    clearOne,
    markAllRead,
    markRecordRead,
    notificationsList,
    notificationsUnread,
  },
};

function walk(dir: string, accept: (file: string) => boolean, out: string[] = []): string[] {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, accept, out);
    else if (accept(full)) out.push(full);
  }
  return out;
}

/** module -> the names importing files bind from it */
function importedSurface(files: readonly string[]): Map<string, Set<string>> {
  const surface = new Map<string, Set<string>>();
  for (const file of files) {
    let imports: ScannedImport[];
    try {
      imports = scanImports(readFileSync(file, "utf8"), file, appDir);
    } catch {
      continue; // an unreadable file cannot prove or break the census
    }
    for (const entry of imports) {
      if (!entry.resolved) continue;
      const names = surface.get(entry.resolved) ?? new Set<string>();
      for (const name of entry.names) names.add(name);
      surface.set(entry.resolved, names);
    }
  }
  return surface;
}

const capsuleFiles = readdirSync(harnessRoot, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .flatMap((entry) => {
    const uiDir = path.join(harnessRoot, entry.name, "ui");
    try {
      return walk(uiDir, (file) => /\.(ts|vue)$/.test(file));
    } catch {
      return []; // a capsule with no UI dir
    }
  });
const capsuleSurface = importedSurface(capsuleFiles);
const inWorkspaceSurface = importedSurface(
  [...walk(appDir, (file) => /\.(ts|vue)$/.test(file)),
    ...walk(path.join(webuiRoot, "test"), (file) => /\.ts$/.test(file))].filter(
    (file) => !/capsulecensus/.test(path.basename(file)),
  ),
);

test("every name a capsule UI imports is exported by the shell module it names", () => {
  assert.ok(capsuleSurface.size > 0, "the scan found no capsule imports - the census is blind");
  for (const [module, names] of capsuleSurface) {
    if (!module.startsWith(appDir)) continue; // a capsule-local import stays outside the shell
    const exports = exportedNames(readFileSync(module, "utf8"), module);
    const missing = [...names].filter(
      (name) => name !== "*" && !exports.star && !exports.names.has(name),
    );
    assert.deepEqual(
      missing,
      [],
      `${path.relative(appDir, module)} no longer exports ${missing.join(", ")} - a capsule UI imports it`,
    );
  }
});

test("the bridge table is exactly the capsule-only surface of each bridged module", () => {
  for (const [relative, bridge] of Object.entries(BRIDGE)) {
    const module = path.join(appDir, relative);
    const imported = capsuleSurface.get(module) ?? new Set<string>();
    const local = inWorkspaceSurface.get(module) ?? new Set<string>();
    const capsuleOnly = [...imported].filter((name) => !local.has(name)).sort();
    assert.deepEqual(
      Object.keys(bridge).sort(),
      capsuleOnly,
      `${relative}: the bridge must name what capsule UIs import and webui does not`,
    );
    for (const [name, value] of Object.entries(bridge)) {
      assert.notEqual(value, undefined, `${relative}: ${name} is not a live binding`);
    }
  }
});

// the scanner's own pins - the census is only as honest as these readings

test("clauseNames reads named, aliased, type and default bindings", () => {
  assert.deepEqual(clauseNames("{ a, b as c, type d }"), ["a", "c", "d"]);
  assert.deepEqual(clauseNames("* as ns"), ["*"]);
  assert.deepEqual(clauseNames("Widget, { a }"), ["a", "default"]);
});

test("resolveSpecifier reads the one alias and relative paths, and drops packages", () => {
  const file = path.join(appDir, "components", "shell", "notificationFeed.ts");
  assert.equal(
    resolveSpecifier("~/components/shell/notificationFeed", file, appDir),
    path.join(appDir, "components", "shell", "notificationFeed.ts"),
  );
  assert.equal(
    resolveSpecifier("../../composables/narrow", file, appDir),
    path.join(appDir, "composables", "narrow.ts"),
  );
  assert.equal(resolveSpecifier("vue", file, appDir), null);
});

test("scanImports reads an SFC's multi-line imports and a re-export", () => {
  const file = path.join(repoRoot, "harness", "probe", "ui", "Probe.vue");
  const source = [
    "<script setup lang=\"ts\">",
    "  import {",
    "    notificationsList,",
    "    type OmegaNotification,",
    "  } from \"~/components/shell/notificationFeed\";",
    "  import { ref } from \"vue\";",
    "  export { markAllRead } from \"~/components/shell/notificationFeed\";",
    "</script>",
  ].join("\n");
  const imports = scanImports(source, file, appDir);
  const target = path.join(appDir, "components", "shell", "notificationFeed.ts");
  assert.deepEqual(
    imports.filter((entry) => entry.resolved === target).map((entry) => entry.names.sort()),
    [["OmegaNotification", "notificationsList"], ["markAllRead"]],
  );
  assert.equal(imports.find((entry) => entry.specifier === "vue")?.resolved, null);
});

test("exportedNames reads declarations, lists, defaults and star re-exports", () => {
  const file = path.join(appDir, "composables", "probe.ts");
  const source = [
    "export const a = 1;",
    "export async function b() {}",
    "export function* c() {}",
    "export interface D {}",
    "export type E = string;",
    "const f = 1;",
    "export { f as g };",
    "export default a;",
  ].join("\n");
  assert.deepEqual([...exportedNames(source, file).names].sort(), [
    "D",
    "E",
    "a",
    "b",
    "c",
    "default",
    "g",
  ]);
  assert.equal(exportedNames(`${source}\nexport * from "./x.ts";`, file).star, true);
  assert.deepEqual(
    [...exportedNames("<template></template>", path.join(appDir, "probe.vue")).names],
    ["default"],
  );
});
