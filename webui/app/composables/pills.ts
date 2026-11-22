// omega webui: reference pill tokens.

import { filterCommands } from "./commands.ts";
import type { PaletteItem } from "./commands.ts";

export interface RefToken {
  // skill = an injected-content invocation ("/name"); file = a path reference ("@path")
  // read with the model's tools; dir = a FOLDER reference - the trailing slash is the
  // discriminator
  kind: "skill" | "file" | "dir";
  token: string;
  label: string;
  name: string;
  start: number;
}

export interface RefSegment {
  text?: string;
  token?: RefToken;
}

const SKILL_LEADING = /^\/([A-Za-z0-9_-]+)/;
// the composer's chip editor accepts a skill at ANY word boundary (its palette is
// token-based, so a completed /word chips wherever the palette accepted it).
const SKILL_ANYWHERE = /(^|[\s(])\/([A-Za-z0-9_-]+)/g;
const FILE_TOKEN = /(^|[\s(])@([^\s)]+)/g;
// the @ palette's two PREFIX rows are injections, never paths.
const SKILL_PREFIX = /^skill:(.+)$/;
const SESSION_PREFIX = /^session:/;

const isBasename = (name: string): boolean => {
  const base = name.split("/").pop() ?? "";
  return base !== "" && base !== "." && base !== "..";
};

// ScanOpts: ONE token model, two surfaces differing on one point - inlineSkills (the
// composer's chip editor, where a completed /word chips wherever its token-based palette
// accepted it) vs absent (the transcript, where a LEADING /word IS the invocation).
export interface ScanOpts {
  inlineSkills?: boolean;
}

// scanRefTokens is the one token model:
// skill - "/word" at TEXT START, or at any word boundary when opts.inlineSkills. Also
//         "@skill:<name>": the @ palette's prefix row is the other door to the same
//         server-side injection, so it wears the skill pill while the raw token (the
//         draft, i.e. the expansion's exact source) is kept.
// file  - a standalone "@path" (text start, or after whitespace or an open paren); a
//         mid-word @ is an email (the server's AtFilePattern law).
// dir   - a standalone "@path/": the trailing slash names a folder, so the pill shows its
//         basename and the raw token keeps the slash the model follows itself.
// none  - "@session:<id>" stays literal text: an opaque id has no reference shape.
export function scanRefTokens(text: string, opts: ScanOpts = {}): RefToken[] {
  const out: RefToken[] = [];
  if (opts.inlineSkills) {
    SKILL_ANYWHERE.lastIndex = 0;
    for (const m of text.matchAll(SKILL_ANYWHERE)) {
      const start = m.index + m[1]!.length;
      const name = m[2]!;
      out.push({
        kind: "skill",
        token: "/" + name,
        label: name,
        name,
        start,
      });
    }
  } else {
    const skill = text.match(SKILL_LEADING);
    if (skill) {
      const name = skill[1]!;
      out.push({
        kind: "skill",
        token: skill[0],
        label: name,
        name,
        start: 0,
      });
    }
  }
  FILE_TOKEN.lastIndex = 0;
  for (const m of text.matchAll(FILE_TOKEN)) {
    const start = m.index + m[1].length;
    const token = ("@" + m[2]).replace(/[.,;:!?]+$/, "");
    const name = token.slice(1);
    // prefix rows are injections, so classify them BEFORE the path branches
    const skillInjection = name.match(SKILL_PREFIX);
    if (skillInjection) {
      const skillName = skillInjection[1]!;
      out.push({
        kind: "skill",
        token,
        label: skillName,
        name: skillName,
        start,
      });
      continue;
    }
    if (name.endsWith("/")) {
      const path = name.replace(/\/+$/, "");
      if (!isBasename(path)) continue; // "@" or "@//" - not a reference
      out.push({
        kind: "dir",
        token,
        label: path.split("/").pop() ?? path,
        name: path,
        start,
      });
      continue;
    }
    if (SESSION_PREFIX.test(name)) continue;
    if (!isBasename(name)) continue;
    out.push({
      kind: "file",
      token,
      label: name.split("/").pop() ?? name,
      name,
      start,
    });
  }
  return out.sort((a, b) => a.start - b.start);
}

export function splitRefSegments(text: string, opts: ScanOpts = {}): RefSegment[] {
  const tokens = scanRefTokens(text, opts);
  const out: RefSegment[] = [];
  let at = 0;
  for (const t of tokens) {
    if (t.start > at) out.push({ text: text.slice(at, t.start) });
    out.push({ token: t });
    at = t.start + t.token.length;
  }
  if (at < text.length) out.push({ text: text.slice(at) });
  return out;
}

// tokenBeforeCaret finds the pill that ends exactly at the caret.
export function tokenBeforeCaret(
  text: string,
  caret: number,
  opts: ScanOpts = {},
): RefToken | null {
  for (const t of scanRefTokens(text, opts)) {
    if (t.start + t.token.length === caret) return t;
  }
  return null;
}

export function deleteTokenAt(text: string, t: RefToken): { draft: string; caret: number } {
  return {
    draft: text.slice(0, t.start) + text.slice(t.start + t.token.length),
    caret: t.start,
  };
}

// the chip law: a chip is a VIEW of the token's span, never a duplicate. The draft keeps
// the raw tokens (the server-side /chat at-mention expansion consumes that text).

// ChipOpts carries the editor's edit state into the chip rule: `forced` keys the accept
// path just inserted, `dissolved` keys a Backspace revealed raw.
export interface ChipOpts {
  caret: number;
  paletteOpen: boolean;
  forced: ReadonlySet<string>;
  dissolved: ReadonlySet<string>;
}

export function chipKeyOf(t: RefToken): string {
  return t.token;
}

// chipFor decides whether ONE pill-able token renders as an inline chip: raw while the
// caret is inside the span or accepting a palette query at its end, or after a dissolve
// until the caret leaves the span; otherwise it chips.
export function chipFor(t: RefToken, o: ChipOpts): boolean {
  const end = t.start + t.token.length;
  if (o.forced.has(t.token)) return true;
  const inside = o.caret > t.start && o.caret < end;
  if (inside) return false;
  if (o.dissolved.has(t.token)) return !(o.caret > t.start && o.caret <= end);
  if (o.paletteOpen && o.caret === end) return false;
  return true;
}

export interface EditorNode {
  type: "text";
  text: string;
}
export interface EditorChipNode {
  type: "chip";
  token: RefToken;
}
export type EditorPiece = EditorNode | EditorChipNode;

// editorNodes turns a draft into the editor's DOM plan: text verbatim, chip nodes for the
// tokens the rule says replace their spans; a non-chipping token folds back into the
// surrounding text, so the plan reads back as the draft exactly.
export function editorNodes(
  text: string,
  o: ChipOpts,
  isPill?: (t: RefToken) => boolean,
  opts: ScanOpts = {},
): EditorPiece[] {
  const out: EditorPiece[] = [];
  let carry = "";
  const flush = () => {
    if (carry !== "") {
      out.push({ type: "text", text: carry });
      carry = "";
    }
  };
  for (const seg of splitRefSegments(text, opts)) {
    if (seg.text) {
      carry += seg.text;
      continue;
    }
    const t = seg.token!;
    if (!isPill?.(t) || !chipFor(t, o)) {
      carry += t.token;
      continue;
    }
    flush();
    out.push({ type: "chip", token: t });
  }
  flush();
  return out;
}

// mentionDirOf extracts the token's directory part; prefix tokens never reach here.
export function mentionDirOf(token: string): string {
  const inner = token.slice(1);
  const slash = inner.lastIndexOf("/");
  return slash === -1 ? "" : inner.slice(0, slash + 1);
}

export function mentionLeafQuery(token: string): string {
  const inner = token.slice(1);
  const slash = inner.lastIndexOf("/");
  return slash === -1 ? inner : inner.slice(slash + 1);
}

// mentionRows orders a directory listing: folders first, then files, each group by name.
export function mentionRows(
  entries: { name: string; is_dir: boolean }[],
): PaletteItem[] {
  const byName = (a: { name: string }, b: { name: string }): number =>
    a.name.localeCompare(b.name);
  const dirs = entries.filter((e) => e.is_dir).sort(byName);
  const files = entries.filter((e) => !e.is_dir).sort(byName);
  return [
    ...dirs.map((d) => ({
      name: d.name,
      description: "reference this folder",
      kind: "dir" as const,
    })),
    ...files.map((f) => ({
      name: f.name,
      description: "reference this file",
      kind: "file" as const,
    })),
  ];
}

export function filterMentionRows(token: string, rows: PaletteItem[]): PaletteItem[] {
  const leaf = mentionLeafQuery(token);
  if (!leaf) return rows;
  return filterCommands(leaf, rows);
}