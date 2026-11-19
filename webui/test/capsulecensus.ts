// omega webui: the capsule-UI import census (pure scanner, no parser dependency).
//
// A capsule UI lives outside webui/ (harness/<name>/ui), so the webui dead-export pass
// cannot see it as a consumer. This scanner reads the capsule UIs' own source to answer
// the two questions that gap leaves open: which shell modules do they import, and is
// every imported name still exported where they import it from.

import { existsSync, statSync } from "node:fs";
import path from "node:path";

// The ONE alias the shell's own sources use (nuxt.config's alias table).
export const ALIASES: Readonly<Record<string, string>> = { "~": "app" };

export interface ScannedImport {
  /** the importing file, absolute */
  file: string;
  /** the raw specifier */
  specifier: string;
  /** the module it resolves to, absolute; null for packages and unresolvable paths */
  resolved: string | null;
  /** imported binding names as the TARGET exports them; "*" = namespace import */
  names: string[];
}

const IMPORT_RE = /(?:^|[\r\n])[ \t]*import\s+(type\s+)?([^;'"]*?)\s*from\s*["']([^"']+)["']/g;
const SIDE_EFFECT_RE = /(?:^|[\r\n])[ \t]*import\s*["']([^"']+)["']/g;
const REEXPORT_RE =
  /(?:^|[\r\n])[ \t]*export\s+(type\s+)?(\{[\s\S]*?\}|\*)\s*from\s*["']([^"']+)["']/g;

/** the exported name a `{ a, b as c, type d }` entry publishes */
function bindingName(raw: string): string | null {
  const text = raw.replace(/^\s*type\s+/, "").trim();
  if (!text) return null;
  const match = text.match(/^(?:[A-Za-z_$][\w$]*\s+as\s+)?([A-Za-z_$][\w$]*)$/);
  return match ? match[1]! : null;
}

/** the target-side names an import clause binds */
export function clauseNames(clause: string): string[] {
  const names: string[] = [];
  const braces = clause.match(/\{([\s\S]*)\}/);
  if (braces) {
    for (const raw of braces[1]!.split(",")) {
      const name = bindingName(raw);
      if (name) names.push(name);
    }
  }
  if (/\*\s+as\s+/.test(clause)) {
    names.push("*");
    return names;
  }
  // a bare identifier outside the braces is a default import
  const head = clause.split("{")[0]!.replace(/\*/g, "").trim();
  for (const part of head.split(",")) {
    if (part.trim()) names.push("default");
  }
  return names;
}

export function resolveSpecifier(
  specifier: string,
  fromFile: string,
  appDir: string,
): string | null {
  let base: string;
  if (specifier.startsWith(".")) {
    base = path.resolve(path.dirname(fromFile), specifier);
  } else {
    const slash = specifier.indexOf("/");
    const alias = ALIASES[slash < 0 ? specifier : specifier.slice(0, slash)];
    if (!alias) return null;
    const rest = slash < 0 ? "" : specifier.slice(slash + 1);
    // aliases are webui-relative ("app"), so they resolve against appDir's parent
    base = path.join(path.dirname(appDir), alias, rest);
  }
  for (const candidate of [base, `${base}.ts`, `${base}.vue`, path.join(base, "index.ts")]) {
    if (existsSync(candidate) && statSync(candidate).isFile()) return candidate;
  }
  return null;
}

/** every import (and re-export) a source file declares, resolved where it can be */
export function scanImports(source: string, file: string, appDir: string): ScannedImport[] {
  const out: ScannedImport[] = [];
  for (const match of source.matchAll(IMPORT_RE)) {
    out.push({
      file,
      specifier: match[3]!,
      resolved: resolveSpecifier(match[3]!, file, appDir),
      names: clauseNames(match[2]!),
    });
  }
  for (const match of source.matchAll(SIDE_EFFECT_RE)) {
    out.push({
      file,
      specifier: match[1]!,
      resolved: resolveSpecifier(match[1]!, file, appDir),
      names: [],
    });
  }
  for (const match of source.matchAll(REEXPORT_RE)) {
    out.push({
      file,
      specifier: match[3]!,
      resolved: resolveSpecifier(match[3]!, file, appDir),
      names: match[2]!.startsWith("*") ? ["*"] : clauseNames(match[2]!),
    });
  }
  return out;
}

const DECL_RE =
  /^[ \t]*export\s+(?:default\s+)?(?:declare\s+)?(?:async\s+)?(?:const|let|var|function\s*\*?|class|interface|type|enum)\s+([A-Za-z_$][\w$]*)/gm;
const EXPORT_LIST_RE = /^[ \t]*export\s+(?:type\s+)?\{([\s\S]*?)\}/gm;
const DEFAULT_RE = /^[ \t]*export\s+default\b/m;
const STAR_RE = /^[ \t]*export\s+\*\s*(?:as\s+[\w$]+\s*)?from/m;

/** the names `source` exports; a star re-export makes the set open-ended */
export function exportedNames(source: string, file: string): { names: Set<string>; star: boolean } {
  // a .vue SFC publishes its component as the default export and nothing else
  if (file.endsWith(".vue")) return { names: new Set(["default"]), star: false };
  const names = new Set<string>();
  for (const match of source.matchAll(DECL_RE)) names.add(match[1]!);
  for (const match of source.matchAll(EXPORT_LIST_RE)) {
    for (const raw of match[1]!.split(",")) {
      const name = bindingName(raw);
      if (name) names.add(name);
    }
  }
  if (DEFAULT_RE.test(source)) names.add("default");
  return { names, star: STAR_RE.test(source) };
}
