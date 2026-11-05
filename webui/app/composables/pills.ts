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

// tokenBeforeCaret finds the pill behind the caret - the Backspace
// dissolve's target (the "pill dissolves to its text" law): the caret
// sits immediately behind a token's end.
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

// --- the chip law (card t_e67c372b): a pill REPLACES the text it represents ---

// The composer no longer renders pills on a strip above the textarea with
// the raw token still sitting in the text. The editor renders chip nodes
// inline where the token spans were; the draft string keeps the raw
// tokens throughout (the server-side /chat at-mention expansion is the
// consumer of that exact text), so a chip and its span are one and the
// same data - the chip is a VIEW of the span, never a duplicate.

// ChipOpts carries the editor's edit state into the chip rule:
// - caret: the caret's index in draft (token) space
// - paletteOpen: a query surface (/ or @) is active at the caret
// - forced: chip keys the accept path just inserted (fresh swallow)
// - dissolved: chip keys a Backspace revealed raw; they stay raw as long
//   as the caret works their span (inside or at its end)
export interface ChipOpts {
  caret: number;
  paletteOpen: boolean;
  forced: ReadonlySet<string>;
  dissolved: ReadonlySet<string>;
}

// A chip is keyed by its raw token text: two identical tokens behave
// identically, and the rule is per-token - the dissolved one whose caret
// still works it stays raw while its twin keeps its chip.
export function chipKeyOf(t: RefToken): string {
  return t.token;
}

// chipFor decides whether ONE pill-able token renders as an inline chip.
//   forced  - the accept path's fresh swallow: the caret parks right at
//             the insertion end, the chip still forms.
//   inside  - the caret is strictly within the span: the token is being
//             typed or edited, it renders raw.
//   dissolved - same window including the span end (the caret parked
//             exactly where the dissolve left it), it stays raw until the
//             caret leaves; away from the span it re-forms.
//   palette at the span end - the running query is still being accepted
//             or edited at this token, it renders raw.
export function chipFor(t: RefToken, o: ChipOpts): boolean {
  const end = t.start + t.token.length;
  if (o.forced.has(t.token)) return true;
  const inside = o.caret > t.start && o.caret < end;
  if (inside) return false;
  if (o.dissolved.has(t.token)) return !(o.caret > t.start && o.caret <= end);
  if (o.paletteOpen && o.caret === end) return false;
  return true;
}

// EditorNode is one piece of the editor's DOM plan: verbatim text or a
// chip that replaces its token span.
export interface EditorNode {
  type: "text";
  text: string;
}
export interface EditorChipNode {
  type: "chip";
  token: RefToken;
}
export type EditorPiece = EditorNode | EditorChipNode;

// editorNodes turns a draft into the editor's DOM plan: text nodes
// verbatim, chip nodes for the tokens the chip rule says must replace
// their spans. A token that must NOT chip (a skill name that is still an
// unknown slash word, a token under the caret) folds back into the text
// around it, so the plain reading of the plan equals the draft exactly.
// The isPill callback is the chip-eligibility knowledge the composer
// holds (a shell verb like "/theme" is never a chip - the skill snapshot
// load decides); a token failing it renders raw even before the rule.
export function editorNodes(
  text: string,
  o: ChipOpts,
  isPill?: (t: RefToken) => boolean,
): EditorPiece[] {
  const out: EditorPiece[] = [];
  let carry = "";
  const flush = () => {
    if (carry !== "") {
      out.push({ type: "text", text: carry });
      carry = "";
    }
  };
  for (const seg of splitRefSegments(text)) {
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