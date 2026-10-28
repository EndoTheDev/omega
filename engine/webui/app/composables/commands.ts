// omega webui: the / command palette and @ mention picker (card
// t_07aed3f9). The TUI is the spec: slash commands run LOCALLY and
// never hit the agent; the palette lists the webui's shell commands,
// the extension command registry (ctx.Commands via GET /commands) and
// skills as /<skill-name>. @-mention expansion itself is SERVER-SIDE
// on the /chat submit path (core.ExpandAtMentions) - the picker here
// only helps TYPE the tokens; it never reads or expands files.
//
// The pure helpers (token parsing, filtering, caret surgery) are
// node-tested at the palette.test.ts boundary; the fetchers are the
// production wire shapes.

// --- palette model ---

export interface PaletteItem {
  // the completed token: "/theme", "/greet-writer", "notes.txt",
  // "session:", "sess-123"
  name: string;
  // one-line what-it-does
  description: string;
  // shell = webui-native local action; extension = a module's
  // registered command (dispatched server-side, never the agent);
  // skill = content injection on the next send; file/session =
  // @-mention inserts
  kind: "shell" | "extension" | "skill" | "file" | "session";
}

// The one set the ChatTab + host use to route a "/" draft: these run
// through the app's own handler (deps.runCommand); everything else is
// a dispatch (module command or skill) to POST /commands/dispatch.
// Declared after the table it mirrors.

// The webui's shell set: the TUI's knownCommands minus /exit (nothing
// to quit) with webui-native equivalents for the rest. The open
// first-pass ruling (card t_07aed3f9): theme, fork, retry, variant,
// help at minimum - the full TUI set maps like this.
export const WEBUI_SHELL_COMMANDS: PaletteItem[] = [
  { name: "/copy", description: "copy the last response to the clipboard", kind: "shell" },
  // card t_210a0d19 rename law: /modules is the new name, /extensions
  // keeps its palette row as the alias - no muscle-memory break.
  { name: "/modules", description: "open the module cockpit (inventory + toggles + expansion)", kind: "shell" },
  { name: "/extensions", description: "open the module cockpit (alias of /modules)", kind: "shell" },
  { name: "/fork", description: "aim the next send to continue from the newest response", kind: "shell" },
  { name: "/help", description: "list the commands", kind: "shell" },
  { name: "/new", description: "start a new chat tab", kind: "shell" },
  { name: "/panels", description: "open the canvas panels tab", kind: "shell" },
  { name: "/retry", description: "re-run the last user turn as a new response variant", kind: "shell" },
  { name: "/sessions", description: "open the sessions sidebar", kind: "shell" },
  { name: "/theme", description: "apply a theme: dark, light, auto, or a user theme name", kind: "shell" },
  { name: "/variant", description: "show the response variants on the active path", kind: "shell" },
];

// paletteCommands assembles the slash list: shell commands, then
// extension commands (GET /commands rows), then skills as /<skill-name>
// - the same assembly order the TUI's buildCommands uses.
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

// The one set the ChatTab + host use to route a "/" draft: these run
// through the app's own handler (deps.runCommand); everything else is
// a dispatch (module command or skill) to POST /commands/dispatch.
export const SHELL_COMMAND_NAMES = new Set(WEBUI_SHELL_COMMANDS.map((c) => c.name));

// filterCommands mirrors the TUI autocomplete: case-insensitive
// substring over the name AND the description (a description match
// finds a command when you remember what it does, not what it is).
export function filterCommands(query: string, items: PaletteItem[]): PaletteItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items.filter(
    (i) => i.name.toLowerCase().includes(q) || i.description.toLowerCase().includes(q),
  );
}

// --- the @ mention token ---

// MentionQuery at a caret: active when a token like "@...": starts
// with @ and runs from the caret back to a word boundary. The raw
// token INCLUDES the @.
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

// MentionPrefix classifies a mention token: "" = bare file search;
// "session:" / "skill:" = the two injected-context lists.
export type MentionPrefix = "" | "session:" | "skill:";

export function mentionPrefixOf(token: string): MentionPrefix {
  const inner = token.slice(1).toLowerCase();
  if (inner.startsWith("session:")) return "session:";
  if (inner.startsWith("skill:")) return "skill:";
  return "";
}

// mentionInnerQuery strips the @ and the prefix: what's left filters
// the list the prefix opened.
export function mentionInnerQuery(token: string): string {
  return token.slice(1).replace(/^session:|^skill:/i, "");
}

// pickList classifies the mention dropdown rows for a token: a bare
// "@" or a non-prefix query lists files PLUS the two prefix rows; a
// prefixed token lists only that prefix's rows.
export function pickList(token: string): "files" | "sessions" | "skills" {
  return mentionPrefixOf(token).slice(0, -1) as "files" | "sessions" | "skills";
}

// replaceMentionToken writes a completed mention back into the draft
// at the caret: the token span (from the query start to the caret)
// becomes the insertion. Returns the new draft; the caller moves the
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

// clampIndex is the only arithmetic the palette keyboard nav does:
// ArrowDown/ArrowUp move active, wrapping at the ends. From no
// selection (-1), ArrowDown highlights the first row, ArrowUp the
// last; an empty list stays unselected.
export function clampIndex(active: number, delta: number, len: number): number {
  if (len <= 0) return -1;
  if (active < 0) return delta < 0 ? len - 1 : 0;
  return (active + delta + len) % len;
}

// --- the wire (production shapes; fetch happens at the components) ---

export interface PaletteExtensionCommand {
  name: string;
  description?: string;
  args?: string[];
}

// CommandDispatchWire is POST /commands/dispatch's answer: kind
// "command" carries the module's result text; kind "skill" carries the
// content the next send arms as a system preamble.
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

export async function dispatchCommand(
  name: string,
  args = "",
): Promise<CommandDispatchWire> {
  const r = await fetch("/commands/dispatch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, args }),
  });
  if (!r.ok) {
    const body = await r.text();
    throw new Error(body || `${r.status}`);
  }
  return r.json();
}