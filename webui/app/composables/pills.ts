// omega webui: reference pill tokens (card t_7f09bd0c). The composer's
// /skill invocations and @file attachments are TOKENS in the draft text -
// the composer's pill strip (AppComposer) and the sent-message pills
// (ChatMessage) derive from the same scan, so the composer state and the
// transcript agree on what a pill is. No transcript mining, no prose
// heuristics: a pill exists only on a token the composer produced - a
// leading /<skill-name> or a standalone @<path>, the exact shapes the two
// autocompletes insert. Pure functions only: node-tested at this module,
// the components are source-pinned in test/pills.test.ts.

import { filterCommands } from "./commands.ts";
import type { PaletteItem } from "./commands.ts";

// --- the token model ---

export interface RefToken {
  // skill = an injected-content invocation ("/name"); file = a path
  // reference ("@path") the model reads with its own tools
  kind: "skill" | "file";
  // the raw token text ("/greet", "@docs/readme.md")
  token: string;
  // what the pill shows: the skill name, the file's basename
  label: string;
  // how the pill is addressed: the skill name (the SKILL.md folder), the
  // file's relative path (the viewer ref + hover title)
  name: string;
  // the token's start index in the scanned text
  start: number;
}

export interface RefSegment {
  text?: string;
  token?: RefToken;
}

const SKILL_TOKEN = /^\/[A-Za-z0-9_-]+/;
const FILE_TOKEN = /(^|[\s(])@([^\s)]+)/g;

const isBasename = (name: string): boolean => {
  const base = name.split("/").pop() ?? "";
  return base !== "" && base !== "." && base !== "..";
};

// scanRefTokens parses a text into its pill tokens:
//
//   skill  - "/word" at the TEXT START only. The composer inserts the
//            token from the slash palette at draft start; shell and
//            extension verbs never reach a transcript bubble (the send
//            routes them), so a leading slash IS a skill invocation.
//   file   - a standalone "@path" (text start, or after whitespace or an
//            open paren). A mid-word @ - user@host - is an email, not a
//            file reference (the server's AtFilePattern law). A token
//            with no basename ("@dir/") drills, never renders a pill.
export function scanRefTokens(text: string): RefToken[] {
  const out: RefToken[] = [];
  const skill = text.match(SKILL_TOKEN);
  if (skill) {
    out.push({
      kind: "skill",
      token: skill[0],
      label: skill[0].slice(1),
      name: skill[0].slice(1),
      start: 0,
    });
  }
  FILE_TOKEN.lastIndex = 0;
  for (const m of text.matchAll(FILE_TOKEN)) {
    const start = m.index + m[1].length;
    // trailing punctuation belongs to the sentence, not the reference -
    // the pill's path is the clean token ("@docs/x.md," opens x.md)
    const token = ("@" + m[2]).replace(/[.,;:!?]+$/, "");
    const name = token.slice(1);
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

// splitRefSegments turns a text into rendered pieces: the pill tokens as
// tokens, everything between as text. The consumer renders segments in
// order - the user bubble keeps its literal text with pills inline.
export function splitRefSegments(text: string): RefSegment[] {
  const tokens = scanRefTokens(text);
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

// tokenBeforeCaret finds the pill a Backspace kills whole (the "empty
// pill deletes" law): the caret sits immediately behind a token's end.
export function tokenBeforeCaret(text: string, caret: number): RefToken | null {
  for (const t of scanRefTokens(text)) {
    if (t.start + t.token.length === caret) return t;
  }
  return null;
}

// deleteTokenAt removes the token span and parks the caret where it was.
export function deleteTokenAt(text: string, t: RefToken): { draft: string; caret: number } {
  return {
    draft: text.slice(0, t.start) + text.slice(t.start + t.token.length),
    caret: t.start,
  };
}

// --- the @ autocomplete's path law ---

// mentionDirOf extracts the token's directory part: "@docs/re" drills
// into "docs/", a bare "@" browses the root. Prefix tokens never reach
// here (the composer branches on session:/skill: first).
export function mentionDirOf(token: string): string {
  const inner = token.slice(1);
  const slash = inner.lastIndexOf("/");
  return slash === -1 ? "" : inner.slice(0, slash + 1);
}

// mentionLeafQuery is what filters the drilled rows: the composer typed
// "@docs/read" - the rows are docs/'s children, so the filter is "read",
// not "docs/read".
export function mentionLeafQuery(token: string): string {
  const inner = token.slice(1);
  const slash = inner.lastIndexOf("/");
  return slash === -1 ? inner : inner.slice(slash + 1);
}

// mentionRows orders a directory listing for the @ list: directories
// first, each alphabetical (the card's law). Dir rows drill (kind "dir"),
// file rows complete to a pill token.
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
      description: "folder - drill in",
      kind: "dir" as const,
    })),
    ...files.map((f) => ({
      name: f.name,
      description: "reference this file",
      kind: "file" as const,
    })),
  ];
}

// filterMentionRows filters a drilled list by the typed leaf (see
// mentionLeafQuery); an empty leaf returns the full list in order.
export function filterMentionRows(token: string, rows: PaletteItem[]): PaletteItem[] {
  const leaf = mentionLeafQuery(token);
  if (!leaf) return rows;
  return filterCommands(leaf, rows);
}