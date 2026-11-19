// omega webui: the / command palette and @ mention picker. Slash commands run LOCALLY
// and never hit the agent; the palette lists the webui's shell commands, the extension
// command registry (ctx.Commands via GET /commands) and skills as /<skill-name>.
// @-mention expansion itself is SERVER-SIDE on the /chat submit path
// (core.ExpandAtMentions) - the picker here only helps TYPE the tokens; it never reads
// or expands files.

// palette model

export interface PaletteItem {
  name: string;
  description: string;
  // shell = webui-native local action; extension = a module's registered command
  // (dispatched server-side, never the agent); skill = content injection on the next
  // send; file/session = @-mention inserts; dir is the @ path list's drill row -
  // picking it never completes a token, it opens the folder's own list
  kind: "shell" | "extension" | "skill" | "file" | "dir" | "session";
}

// The one set the ChatTab + host use to route a "/" draft.

// The webui's shell set: /exit is deliberately absent (nothing to quit),.
export const WEBUI_SHELL_COMMANDS: PaletteItem[] = [
  { name: "/copy", description: "copy the last response to the clipboard", kind: "shell" },
  { name: "/modules", description: "open settings > modules (inventory + toggles + expansion)", kind: "shell" },
  { name: "/extensions", description: "open settings > modules (alias of /modules)", kind: "shell" },
  { name: "/fork", description: "aim the next send to continue from the newest response", kind: "shell" },
  //
  // command palette (the shell's searchable command list) and answers
  // with one line. The description points there, so the reference and
  // the row never disagree.
  { name: "/help", description: "open the command palette - every command, searchable", kind: "shell" },
  { name: "/new", description: "start a new chat tab", kind: "shell" },
  { name: "/panels", description: "open the canvas panels tab", kind: "shell" },
  { name: "/retry", description: "re-run the last user turn as a new response variant", kind: "shell" },
  { name: "/sessions", description: "open the sessions sidebar", kind: "shell" },
  { name: "/theme", description: "apply a theme: dark, light, auto, or a user theme name", kind: "shell" },
  { name: "/variant", description: "show the response variants on the active path", kind: "shell" },
];

// paletteCommands assembles the slash list.
export function paletteCommands(
  extensions: { name: string; description?: string }[],
  skills: { name: string; description: string }[],
): PaletteItem[] {
  return [
    ...WEBUI_SHELL_COMMANDS,
    ...extensions.map((e) => ({
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

// filterCommands: a case-insensitive substring over the name AND the description.
export function filterCommands(query: string, items: PaletteItem[]): PaletteItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items.filter(
    (i) => i.name.toLowerCase().includes(q) || i.description.toLowerCase().includes(q),
  );
}

// the @ mention token

// MentionQuery at a caret: active when a token like "@.".
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

// MentionPrefix classifies a mention token.
export type MentionPrefix = "" | "session:" | "skill:";

export function mentionPrefixOf(token: string): MentionPrefix {
  const inner = token.slice(1).toLowerCase();
  if (inner.startsWith("session:")) return "session:";
  if (inner.startsWith("skill:")) return "skill:";
  return "";
}

// pickList classifies the mention dropdown rows for a token.
export function pickList(token: string): "files" | "sessions" | "skills" {
  return mentionPrefixOf(token).slice(0, -1) as "files" | "sessions" | "skills";
}

// replaceMentionToken writes a completed mention back into the draft at the caret.
// Returns the new draft; the caller moves the caret to start + insertion.length.
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

// the / slash token

// SlashQuery at a caret: active when the caret sits inside a token that starts with "/"
// and runs back to a word boundary (whitespace, an open paren, or the text start) - the
// draft's leading word is just the boundary-free case, so a leading slash behaves
// exactly.
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

// replaceSlashToken writes a completed command back into the draft at the caret.
// Returns the new draft; the caller moves the caret to start + insertion.length.
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

// clampIndex is the only arithmetic the palette keyboard nav does.
export function clampIndex(active: number, delta: number, len: number): number {
  if (len <= 0) return -1;
  if (active < 0) return delta < 0 ? len - 1 : 0;
  return (active + delta + len) % len;
}

// the wire (production shapes; fetch happens at the components)

export interface PaletteExtensionCommand {
  name: string;
  description?: string;
  args?: string[];
}

// CommandDispatchWire is POST /commands/dispatch's answer.
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

// dispatchCommand POSTs one extension command (or /<skill-name>).
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