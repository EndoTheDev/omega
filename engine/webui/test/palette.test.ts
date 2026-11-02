// omega webui: the / command palette and @ mention picker (t_07aed3f9).
// The TUI is the spec: "/" at input start lists the commands (shell +
// module registry + skills), "@" lists workspace files plus the
// session:/skill: injections, a selected command acts LOCALLY without
// an agent round-trip, Esc dismisses. Expansion itself is SERVER-SIDE
// (core.ExpandAtMentions on /chat) - these tests own the picker's pure
// token logic and the production-boundary wiring assertions (test law:
// the signal shape production sends).
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  WEBUI_SHELL_COMMANDS,
  SHELL_COMMAND_NAMES,
  paletteCommands,
  filterCommands,
  mentionQueryAt,
  mentionPrefixOf,
  mentionInnerQuery,
  replaceMentionToken,
  clampIndex,
} from "../app/composables/commands.ts";
// card t_6215474b: the shell's TopLevel command palette (Ctrl+K) - the
// registry build + filter are pure, the overlay's grammar is pinned by
// file assertions (node cannot import .vue)
import {
  buildPaletteCommands,
  filterPaletteCommands,
  type PaletteCommandDeps,
} from "../app/composables/commandRegistry.ts";

const here = dirname(fileURLToPath(import.meta.url));
const composer = readFileSync(
  join(here, "../app/components/chat/AppComposer.vue"),
  "utf8",
);
const chatTab = readFileSync(join(here, "../app/components/ChatTab.vue"), "utf8");
const host = readFileSync(join(here, "../app/composables/chat.ts"), "utf8");
const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");

describe("command palette assembly", () => {
  it("shells minus /exit, the full TUI law", () => {
    const names = WEBUI_SHELL_COMMANDS.map((c) => c.name);
    for (const cmd of ["/copy", "/modules", "/extensions", "/panels", "/theme", "/variant", "/fork", "/retry", "/help"]) {
      assert.ok(names.includes(cmd), `missing the TUI shell command ${cmd}`);
    }
    assert.ok(!names.includes("/exit"), "the webui has nothing to quit - /exit is out");
    assert.ok(names.includes("/new"), "the webui equivalent of the TUI /new rides the palette");
    assert.ok(names.includes("/sessions"));
  });

  it("SHELL_COMMAND_NAMES is exactly the shell set - the routing set the host dispatches on", () => {
    assert.equal(SHELL_COMMAND_NAMES.size, WEBUI_SHELL_COMMANDS.length);
    for (const c of WEBUI_SHELL_COMMANDS) assert.ok(SHELL_COMMAND_NAMES.has(c.name));
  });

  it("assembles shell -> extension -> skill, skills carry the slash", () => {
    const items = paletteCommands(
      [{ name: "/modcmd", description: "a module command" }],
      [{ name: "greet-writer", description: "writes greetings" }],
    );
    assert.equal(items[0]!.kind, "shell");
    const ext = items.find((i) => i.name === "/modcmd");
    assert.ok(ext, "module commands ride the palette");
    assert.equal(ext!.kind, "extension");
    const skill = items.find((i) => i.name === "/greet-writer");
    assert.ok(skill, "skills list as /<skill-name>");
    assert.equal(skill!.kind, "skill");
    assert.ok(skill!.description.includes("writes"));
  });

  it("filters case-insensitively over name AND description", () => {
    const items: ReturnType<typeof paletteCommands> = [
      { name: "/theme", description: "apply a theme", kind: "shell" },
      { name: "/copy", description: "copy to clipboard", kind: "shell" },
    ];
    assert.deepEqual(filterCommands("/TH", items).map((i) => i.name), ["/theme"]);
    assert.deepEqual(filterCommands("clip", items).map((i) => i.name), ["/copy"]);
    assert.equal(filterCommands("zzz", items).length, 0);
    assert.equal(filterCommands("   ", items).length, 2);
  });
});

describe("mention token parsing", () => {
  it("activates on a running @token at the caret only", () => {
    assert.deepEqual(mentionQueryAt("hello @notes.t", 14), { active: true, token: "@notes.t" });
    assert.equal(mentionQueryAt("hello @notes.txt and", 22).active, false);
    assert.deepEqual(mentionQueryAt("@", 1), { active: true, token: "@" });
    // the caret may sit before the token's tail
    assert.equal(mentionQueryAt("email me at user@site.com", 14).active, false);
  });

  it("classifies the two injected-context prefixes", () => {
    assert.equal(mentionPrefixOf("@session:abc"), "session:");
    assert.equal(mentionPrefixOf("@skill:foo"), "skill:");
    assert.equal(mentionPrefixOf("@notes.txt"), "");
    assert.equal(mentionInnerQuery("@session:sess-1"), "sess-1");
    assert.equal(mentionInnerQuery("@skill:"), "");
    assert.equal(mentionInnerQuery("@notes"), "notes");
  });

  it("replaces the token span at the caret, preserving the rest", () => {
    const out = replaceMentionToken("see @notes.t please", 12, "@notes.t", "@notes.txt");
    assert.equal(out.draft, "see @notes.txt please");
    assert.equal(out.caret, 14);
    // a stale/mismatched token is a no-op
    const same = replaceMentionToken("see @notes.t please", 11, "@other", "@x");
    assert.equal(same.draft, "see @notes.t please");
  });
});

describe("palette keyboard nav", () => {
  it("wraps at both ends and starts the selection at a highlight edge", () => {
    assert.equal(clampIndex(0, 1, 3), 1);
    assert.equal(clampIndex(2, 1, 3), 0);
    assert.equal(clampIndex(0, -1, 3), 2);
    assert.equal(clampIndex(-1, 1, 3), 0, "up-arrow from nothing starts at the first");
    assert.equal(clampIndex(-1, -1, 3), 2);
    assert.equal(clampIndex(0, 1, 0), -1);
    assert.equal(clampIndex(0, 1, 1), 0);
  });
});

describe("production-boundary wiring (test law: the served shape)", () => {
  it("the composer renders a keyboard-navigable listbox with aria", () => {
    assert.ok(composer.includes('role="listbox"'), "the palette is an ARIA listbox");
    assert.ok(composer.includes('role="option"'));
    assert.ok(composer.includes(":aria-expanded"), "the textarea exposes open state");
    assert.ok(composer.includes("aria-activedescendant"), "the highlight is announced");
    assert.ok(composer.includes(`e.key === "ArrowDown"`), "arrow keys move the selection");
    assert.ok(composer.includes(`e.key === "Escape"`), "Esc dismisses the palette");
    assert.ok(composer.includes(`e.key === "Tab"`), "Tab completes without submitting");
    assert.ok(composer.includes("@pointerdown.prevent"), "clicking a row never steals focus");
  });

  it("/skill tokens reach the message path - command drafts route through the host's send law", () => {
    const sendIdx = chatTab.indexOf("function onSend");
    const interceptIdx = chatTab.indexOf('text.startsWith("/")');
    assert.ok(interceptIdx > sendIdx, "the intercept lives in onSend");
    assert.ok(
      chatTab.includes("host.command(stateKey, text,"),
      "the host routes the command - with the t_7f09bd0c send ctx, a skill dispatch arms AND posts",
    );
    // and the intercept really returns - the plain send path stays after it
    const after = chatTab.slice(interceptIdx, interceptIdx + 400);
    assert.ok(after.includes("return;"), "a slash draft is consumed");
  });

  it("the host runs shell verbs locally and module/skill commands via dispatch", () => {
    assert.ok(host.includes("SHELL_COMMAND_NAMES.has(name)"), "shell names route to the app handler");
    assert.ok(host.includes("dispatchCommand"), "everything else POSTs /commands/dispatch");
    assert.ok(host.includes("pendingSkill"), "skills arm as a system preamble");
    assert.ok(host.includes('{ role: "system", content: st.pendingSkill.content }'), "the next send carries the armed skill");
  });

  it("the palette is a themed dropdown on the tokens, above the draft", () => {
    assert.ok(css.includes(".composer-palette"), "the palette has real CSS");
    assert.ok(css.includes("var(--z2)") || css.includes("background: var(--z2)"), "it reads the design tokens");
  });
});

// --- card t_6215474b: the shell command palette (Ctrl+K) ---------------

function fakeDeps(rec: string[]): PaletteCommandDeps {
  const tag = (s: string) => () => rec.push(s);
  return {
    openDockable: (id) => rec.push("open:" + id),
    openChat: tag("open:chat"),
    closeCurrentTab: tag("tab:close"),
    toggleBottomPanel: tag("panel:toggle-bottom"),
    toggleSidebar: (side) => rec.push("sidebar:toggle-" + side),
    openSessionsSidebar: tag("sessions:pane"),
    openRecentSession: tag("sessions:recent"),
    reloadLayout: tag("layout:reload"),
    clearSavedLayout: tag("layout:clear"),
  };
}

describe("command registry build (t_6215474b)", () => {
  it("derives an open command for every dockable but sidebar and viewer", () => {
    const rec: string[] = [];
    const cmds = buildPaletteCommands(fakeDeps(rec));
    const opens = cmds.filter((c) => c.id.startsWith("open:")).map((c) => c.id.slice(5));
    assert.ok(opens.includes("browser"), "Browser is reachable - THE user pain case");
    assert.ok(opens.includes("terminal"));
    assert.ok(opens.includes("settings"), "the + menu's exclusion never applied to the palette");
    assert.ok(!opens.includes("sidebar"), "the locked tab is the toggle's business");
    assert.ok(!opens.includes("viewer"), "an empty viewer tab is meaningless");
    // chat is multi: "open chat" is a fresh tab, labeled honestly
    const chat = cmds.find((c) => c.id === "open:chat")!;
    assert.ok(chat, "every dockable is reachable - chat included");
    assert.equal(chat.title, "New chat");
    assert.ok(cmds.some((c) => c.title === "Open Browser"));
  });

  it("ships the shell-action set with real deps wired", () => {
    const rec: string[] = [];
    const cmds = buildPaletteCommands(fakeDeps(rec));
    const run = (id?: string) => cmds.find((c) => c.id === id)!.run();
    run("tab:close");
    run("panel:toggle-bottom");
    run("sidebar:toggle-left");
    run("sidebar:toggle-right");
    run("sessions:recent");
    run("layout:reload");
    run("layout:clear");
    assert.deepEqual(rec, [
      "tab:close",
      "panel:toggle-bottom",
      "sidebar:toggle-left",
      "sidebar:toggle-right",
      "sessions:recent",
      "layout:reload",
      "layout:clear",
    ]);
  });

  it("no command fakes a keybinding hint (the keybindings card owns them)", () => {
    const cmds = buildPaletteCommands(fakeDeps([]));
    assert.ok(cmds.every((c) => !c.hint));
  });
});

describe("command registry filter (substring, the settled lazy default)", () => {
  const cmds = buildPaletteCommands(fakeDeps([]));

  it("matches case-insensitively over title, id and keywords", () => {
    assert.ok(filterPaletteCommands("brow", cmds).some((c) => c.title === "Open Browser"));
    assert.ok(filterPaletteCommands("BROW", cmds).some((c) => c.title === "Open Browser"));
    // keywords rescue odd voice titles
    assert.ok(filterPaletteCommands("logs", cmds).some((c) => c.id === "open:http_channel"));
    // the id is fuel too
    assert.ok(filterPaletteCommands("tab:close", cmds).some((c) => c.id === "tab:close"));
  });

  it("an empty/whitespace query returns the full list in order", () => {
    assert.deepEqual(filterPaletteCommands("", cmds), [...cmds]);
    assert.equal(filterPaletteCommands("   ", cmds).length, cmds.length);
    assert.equal(filterPaletteCommands("zzz-none", cmds).length, 0);
  });
});

describe("command palette overlay + wiring (t_6215474b)", () => {
  const overlay = readFileSync(
    join(here, "../app/components/nav/CommandPalette.vue"),
    "utf8",
  );
  const app = readFileSync(join(here, "../app/app.vue"), "utf8");
  const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");

  it("the overlay: input autofocus, listbox rows, the keyboard grammar", () => {
    assert.ok(overlay.includes('role="dialog"'), "it is a dialog overlay");
    assert.ok(overlay.includes('role="listbox"') && overlay.includes('role="option"'));
    assert.ok(overlay.includes("inputEl.value?.focus()"), "input autofocus on open");
    for (const k of ["ArrowDown", "ArrowUp", "Enter", "Escape"]) {
      assert.ok(overlay.includes(`"${k}"`), `${k} drives the overlay`);
    }
    assert.ok(overlay.includes("clampIndex"), "one arithmetic with the composer palette");
    assert.ok(overlay.includes("scrollIntoView"), "the selection stays in the visible window");
    assert.ok(overlay.includes("no matching command"), "an empty query state renders");
  });

  it("app.vue mounts the overlay and feeds the built registry", () => {
    assert.ok(app.includes("<CommandPalette"), "the overlay is mounted");
    assert.ok(app.includes(':open="paletteOpen"'));
    assert.ok(app.includes('@close="paletteOpen = false"'));
    assert.ok(app.includes("buildPaletteCommands"), "the registry builds from app deps");
  });

  it("the overlay CSS reads the tokens, top-center above the menubar", () => {
    assert.ok(css.includes(".command-palette {"), "the overlay has real CSS");
    const block = css.slice(css.indexOf(".command-palette {"), css.indexOf(".command-palette-input"));
    assert.ok(block.includes("position: fixed") && block.includes("top:"), "top-center fixed");
    assert.ok(css.includes("background: var(--z2)"), "tokens carry it");
  });
});