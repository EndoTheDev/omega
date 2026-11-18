// omega webui: reference pill tokens.

import { filterCommands } from "./commands.ts";
import type { PaletteItem } from "./commands.ts";

// -- the token model --

export interface RefToken {
  // skill = an injected-content invocation ("/name"); file = a path reference ("@path")
  // the model reads with its own tools; dir = a FOLDER reference ("@path/") - the
  // trailing slash is the discriminator, so a folder pins exactly like a file
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
// the composer's chip editor reads a skill token at ANY word boundary - its slash
// palette is token-based, so a completed /word must chip wherever the palette accepted
// it.
const SKILL_ANYWHERE = /(^|[\s(])\/([A-Za-z0-9_-]+)/g;
const FILE_TOKEN = /(^|[\s(])@([^\s)]+)/g;
// the @ palette's two PREFIX rows. They are never paths, so they never read as file
// references.
const SKILL_PREFIX = /^skill:(.+)$/;
const SESSION_PREFIX = /^session:/;

const isBasename = (name: string): boolean => {
  const base = name.split("/").pop() ?? "";
  return base !== "" && base !== "." && base !== "..";
};

// ScanOpts is the surface the scan runs for. ONE token model, two
// surfaces that differ on exactly one point:
//
//   inlineSkills - the composer's chip editor. Its slash palette is
// token-based, so a completed /word chips
//                  wherever the palette accepted it, exactly like an @
//                  file token. The composer additionally gates on the
//                  live skill snapshot (a shell verb never chips).
// absent - the transcript. A leading /word IS the invocation (the
//                  server's grammar), and prose keeps arbitrary "/" prose
//                  (paths, "and / or") pill-free.
export interface ScanOpts {
  inlineSkills?: boolean;
}

// scanRefTokens parses a text into its pill tokens:
//
// skill - "/word": at the TEXT START (every surface), or at any word
//            boundary when opts.inlineSkills (the composer's editor).
//            Shell and extension verbs never reach a transcript bubble
//            (the send routes them), so a leading slash IS an invocation.
// file - a standalone "@path" (text start, or after whitespace or an
//            open paren). A mid-word @ - user@host - is an email, not a
//            file reference (the server's AtFilePattern law). A token
//            with no basename ("@dir/") is a DIR token, never a file:
// the trailing slash names a folder.
// dir - a standalone "@path/" (the same boundaries as a file token).
//            The pill shows the folder's name and addresses its path
// (trailing slash dropped); the raw token keeps the slash
//            the exact-source law: the model reads "@artifacts_mmo/" and
//            follows it with its own tools.
// skill - also "@skill:<name>": the @ palette's own prefix row is the
//            OTHER door to the skill injection the server expands, so it
//            wears the skill pill (zap + "/name", its click opens
//            SKILL.md) instead of a path pill. The raw token never
//            changes - the draft is the server expansion's exact source.
//   (none) - "@session:<id>" is an injection too, but an opaque id has no
//            reference shape and no viewer: it stays literal text, never
//            a chip (a file pill there opened a "not found" tab).
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
    // trailing punctuation belongs to the sentence, not the reference.
    const token = ("@" + m[2]).replace(/[.,;:!?]+$/, "");
    const name = token.slice(1);
    // the prefix rows are INJECTIONS, not paths: classify before the path branches so
    // "@skill:pill-demo" never reads as a file named "skill:pill-demo" (that pill wore
    // a document glyph and its click opened a Files tab answering "not found").
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
    // the trailing slash is the dir discriminator: a folder reference pins like a file.
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

// splitRefSegments turns a text into rendered pieces.
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

// tokenBeforeCaret finds the pill behind the caret.
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

// deleteTokenAt removes the token span and parks the caret where it was.
export function deleteTokenAt(text: string, t: RefToken): { draft: string; caret: number } {
  return {
    draft: text.slice(0, t.start) + text.slice(t.start + t.token.length),
    caret: t.start,
  };
}

// -- the chip law: a pill REPLACES the text it represents --

// The composer no longer renders pills on a strip above the textarea with the raw. The
// editor renders chip nodes inline where the token spans were; the draft string keeps
// the raw tokens throughout (the server-side /chat at-mention expansion is the consumer
// of that exact text), so a chip and its span are one and the same data - the chip is a
// VIEW of the span, never a duplicate.

// ChipOpts carries the editor's edit state into the chip rule:
// caret: the caret's index in draft (token) space
// paletteOpen: a query surface (/ or @) is active at the caret
// forced: chip keys the accept path just inserted (fresh swallow)
// dissolved: chip keys a Backspace revealed raw; they stay raw as long
//   as the caret works their span (inside or at its end)
export interface ChipOpts {
  caret: number;
  paletteOpen: boolean;
  forced: ReadonlySet<string>;
  dissolved: ReadonlySet<string>;
}

// A chip is keyed by its raw token text.
export function chipKeyOf(t: RefToken): string {
  return t.token;
}

// chipFor decides whether ONE pill-able token renders as an inline chip.
// forced - the accept path's fresh swallow: the caret parks right at
//             the insertion end, the chip still forms.
// inside - the caret is strictly within the span: the token is being
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

// EditorNode is one piece of the editor's DOM plan.
export interface EditorNode {
  type: "text";
  text: string;
}
export interface EditorChipNode {
  type: "chip";
  token: RefToken;
}
export type EditorPiece = EditorNode | EditorChipNode;

// editorNodes turns a draft into the editor's DOM plan: text nodes verbatim, chip nodes
// for the tokens the chip rule says must replace their spans. A token that must NOT
// chip (a skill name that is still an unknown slash word, a token under the caret)
// folds back into the text around it, so the plain reading of the plan equals the draft
// exactly. The isPill callback is the chip-eligibility knowledge the composer holds (a
// shell verb like "/theme" is never a chip - the skill snapshot load decides); a token
// failing it renders raw even before the rule.
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

// -- the @ autocomplete's path law --

// mentionDirOf extracts the token's directory part. Prefix tokens never reach here (the
// composer branches on session:/skill: first).
export function mentionDirOf(token: string): string {
  const inner = token.slice(1);
  const slash = inner.lastIndexOf("/");
  return slash === -1 ? "" : inner.slice(0, slash + 1);
}

// mentionLeafQuery is what filters the drilled rows.
export function mentionLeafQuery(token: string): string {
  const inner = token.slice(1);
  const slash = inner.lastIndexOf("/");
  return slash === -1 ? inner : inner.slice(slash + 1);
}

// mentionRows orders a directory listing for the @ list.
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

// filterMentionRows filters a drilled list by the typed leaf (see mentionLeafQuery).
export function filterMentionRows(token: string, rows: PaletteItem[]): PaletteItem[] {
  const leaf = mentionLeafQuery(token);
  if (!leaf) return rows;
  return filterCommands(leaf, rows);
}