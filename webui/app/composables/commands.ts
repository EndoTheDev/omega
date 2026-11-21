// the / command palette and @ mention picker. Slash commands run LOCALLY and never hit the
// agent; the palette lists the webui's shell commands, the extension registry (GET
// /commands) and skills as /<name>. @-mention expansion is SERVER-SIDE on the /chat submit
// path (core.ExpandAtMentions) - this picker only helps TYPE the tokens.

export interface PaletteItem {
  name: string;
  description: string;
  // shell = local action; extension = a module's registered command (dispatched
  // server-side); skill = content injection on the next send; dir is the @ drill row,
  // which opens the folder's list instead of completing a token
  kind: "shell" | "extension" | "skill" | "file" | "dir" | "session";
}

// the webui's shell set: /exit is deliberately absent (nothing to quit)
export const WEBUI_SHELL_COMMANDS: PaletteItem[] = [
  { name: "/branch", description: "branch a new chat tab from this session (or a given id)", kind: "shell" },
  { name: "/copy", description: "copy the last response to the clipboard", kind: "shell" },
  { name: "/modules", description: "open settings > modules (inventory + toggles + expansion)", kind: "shell" },
  { name: "/extensions", description: "open settings > modules (alias of /modules)", kind: "shell" },
  { name: "/fork", description: "aim the next send to continue from the newest response", kind: "shell" },
  // /help opens the command palette and answers with one line; the description points
  // there, so the reference and the row never disagree.
  { name: "/help", description: "open the command palette - every command, searchable", kind: "shell" },
  { name: "/label", description: "set this session's label (no text clears it)", kind: "shell" },
  { name: "/new", description: "start a new chat tab", kind: "shell" },
  { name: "/panels", description: "open the canvas panels tab", kind: "shell" },
  { name: "/resume", description: "open a session here: /resume <#|id|label>", kind: "shell" },
  { name: "/retry", description: "re-run the last user turn as a new response variant", kind: "shell" },
  { name: "/sessions", description: "open the sessions sidebar", kind: "shell" },
  { name: "/theme", description: "apply a theme: dark, light, auto, or a user theme name", kind: "shell" },
  { name: "/variant", description: "show the response variants on the active path", kind: "shell" },
];

export function paletteCommands(
  extensions: { name: string; description?: string }[],
  skills: { name: string; description: string }[],
): PaletteItem[] {
  return [
    ...WEBUI_SHELL_COMMANDS,
    // ONE row per name: the host routes a shell name to the app's own handler BEFORE any
    // dispatch (chat.ts), so a module command of the same name is a second row over a
    // door that can never open - the store capsule's /new, /resume, /branch and /label
    // (and /sessions) read as dead duplicates precisely because of this.
    ...extensions
      .filter((e) => !SHELL_COMMAND_NAMES.has(e.name))
      .map((e) => ({
        name: e.name,
        description: e.description || "module command",
        kind: "extension" as const,
      })),
    ...skills.map((s) => ({
      name: "/" + s.name,
      description: s.description || "inject this skill's content into the next send",
      kind: "skill" as const,
    })),
  ];
}

// The one set the ChatTab + host use to route a "/" draft.
export const SHELL_COMMAND_NAMES = new Set(WEBUI_SHELL_COMMANDS.map((c) => c.name));

// case-insensitive substring over the name AND the description
export function filterCommands(query: string, items: PaletteItem[]): PaletteItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items.filter(
    (i) => i.name.toLowerCase().includes(q) || i.description.toLowerCase().includes(q),
  );
}

// MentionQuery at a caret: active when a token like "@." runs to the caret
export interface MentionQuery {
  active: boolean;
  token: string;
}

export function mentionQueryAt(draft: string, caret: number): MentionQuery {
  const upto = draft.slice(0, Math.max(0, Math.min(caret, draft.length)));
  const start = upto.search(/@\S*$/);
  if (start === -1) return { active: false, token: "" };
  return { active: true, token: upto.slice(start) };
}

// MentionPrefix classifies a mention token
export type MentionPrefix = "" | "session:" | "skill:";

export function mentionPrefixOf(token: string): MentionPrefix {
  const inner = token.slice(1).toLowerCase();
  if (inner.startsWith("session:")) return "session:";
  if (inner.startsWith("skill:")) return "skill:";
  return "";
}

export function pickList(token: string): "files" | "sessions" | "skills" {
  return mentionPrefixOf(token).slice(0, -1) as "files" | "sessions" | "skills";
}

// replaceMentionToken writes a completed mention back at the caret; the caller moves the
// caret to start + insertion.length.
export function replaceMentionToken(
  draft: string,
  caret: number,
  queryToken: string,
  insertion: string,
): { draft: string; caret: number } {
  const upto = draft.slice(0, Math.max(0, Math.min(caret, draft.length)));
  const start = upto.search(/@\S*$/);
  if (start === -1 || upto.slice(start) !== queryToken) {
    return { draft, caret };
  }
  return {
    draft: draft.slice(0, start) + insertion + draft.slice(caret),
    caret: start + insertion.length,
  };
}

// SlashQuery at a caret: active inside a token that starts with "/" and runs back to a word
// boundary (whitespace, an open paren, or the text start).
export interface SlashQuery {
  active: boolean;
  token: string;
}

export function slashTokenAt(draft: string, caret: number): SlashQuery {
  const upto = draft.slice(0, Math.max(0, Math.min(caret, draft.length)));
  const m = upto.match(/(^|[\s(])(\/\S*)$/);
  if (!m) return { active: false, token: "" };
  return { active: true, token: m[2] ?? "" };
}

// replaceSlashToken writes a completed command back at the caret; the caller moves the
// caret to start + insertion.length.
export function replaceSlashToken(
  draft: string,
  caret: number,
  queryToken: string,
  insertion: string,
): { draft: string; caret: number } {
  const at = Math.max(0, Math.min(caret, draft.length));
  const w = /(^|[\s(])(\/\S*)/g;
  let m: RegExpExecArray | null;
  while ((m = w.exec(draft)) !== null) {
    const start = m.index + m[1]!.length;
    const token = m[2]!;
    const end = start + token.length;
    if (at < start || at > end) continue;
    if (!token.startsWith(queryToken)) return { draft, caret };
    return {
      draft: draft.slice(0, start) + insertion + draft.slice(end),
      caret: start + insertion.length,
    };
  }
  return { draft, caret };
}

// the only arithmetic the palette keyboard nav does
export function clampIndex(active: number, delta: number, len: number): number {
  if (len <= 0) return -1;
  if (active < 0) return delta < 0 ? len - 1 : 0;
  return (active + delta + len) % len;
}

export interface PaletteExtensionCommand {
  name: string;
  description?: string;
  args?: string[];
}

// SessionRefRow is the slice of a sessions row /resume resolves against - the list the
// sessions pane already holds, so the answer names a row the human can see.
export interface SessionRefRow {
  id: string;
  label?: string;
}

// resolveSessionRef answers what `/resume <arg>` names: #N is 1-based in the ROWS PASSED IN
// (the caller hands the order its own surface renders), then an exact id, then a
// case-insensitive label PREFIX. The id/label fall-through is the store's
// HandleResumeCommand rule, so a reference resolves to the same session on every surface.
// Pure: the caller owns the list and the open.
export function resolveSessionRef(rows: SessionRefRow[], arg: string): SessionRefRow | null {
  const want = arg.trim();
  if (!want) return null;
  const digits = want.startsWith("#") ? want.slice(1) : want;
  if (/^\d+$/.test(digits)) {
    const n = Number(digits);
    if (n >= 1 && n <= rows.length) return rows[n - 1] ?? null;
  }
  const exact = rows.find((r) => r.id === want);
  if (exact) return exact;
  const lower = want.toLowerCase();
  return (
    rows.find((r) => (r.label ?? "") !== "" && (r.label ?? "").toLowerCase().startsWith(lower)) ??
    null
  );
}

// POST /commands/dispatch's answer
export interface CommandDispatchWire {
  kind: "command" | "skill";
  text?: string;
  name?: string;
  content?: string;
}

export async function fetchPaletteCommands(): Promise<PaletteExtensionCommand[]> {
  const r = await fetch("/commands");
  if (!r.ok) return [];
  return r.json();
}

export async function fetchPaletteSkills(): Promise<{ name: string; description: string }[]> {
  const r = await fetch("/skills");
  if (!r.ok) return [];
  return r.json();
}

// POSTs one extension command (or /<skill-name>) and returns its dispatch answer
export async function dispatchCommand(
  name: string,
  args = "",
  sessionId = "",
): Promise<CommandDispatchWire> {
  const body: { name: string; args: string; session_id?: string } = { name, args };
  if (sessionId) body.session_id = sessionId;
  const r = await fetch("/commands/dispatch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!r.ok) {
    const detail = await r.text();
    throw new Error(detail || `${r.status}`);
  }
  return r.json();
}