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
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  WEBUI_SHELL_COMMANDS,
  SHELL_COMMAND_NAMES,
  paletteCommands,
  filterCommands,
  mentionQueryAt,
  mentionPrefixOf,
  replaceMentionToken,
  slashTokenAt,
  replaceSlashToken,
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

// --- t_d1a2951d: the slash token is caret-based, mid-sentence included ---

describe("slash token parsing (t_d1a2951d)", () => {
  it("activates on a /token at the caret, anywhere in the draft", () => {
    assert.deepEqual(slashTokenAt("use /sk to fix", 7), { active: true, token: "/sk" });
    assert.deepEqual(slashTokenAt("/th", 3), { active: true, token: "/th" }, "leading stays");
    assert.deepEqual(slashTokenAt("/", 1), { active: true, token: "/" });
    // a paren is a word boundary like the file-token law
    assert.deepEqual(slashTokenAt("(try /th", 8), { active: true, token: "/th" });
    // the caret may sit before the token's tail: the full word extends it
    assert.deepEqual(slashTokenAt("/theme dark", 3), { active: true, token: "/th" });
  });

  it("stands down past the token - args and plain sentence tails", () => {
    assert.equal(slashTokenAt("use /sk to fix", 8).active, false, "past the space");
    assert.equal(slashTokenAt("hello world", 11).active, false);
    // a/b is one word that does not START with "/" - no palette
    assert.equal(slashTokenAt("check a/b", 9).active, false);
    // URLs: the slash never opens the palette mid-URL word
    assert.equal(slashTokenAt("see https://x", 13).active, false);
  });

  it("replaceSlashToken replaces only the whole word under the caret", () => {
    // mid-sentence: the sentence around the token stays
    const mid = replaceSlashToken("use /sk to fix", 7, "/sk", "/skill");
    assert.equal(mid.draft, "use /skill to fix");
    assert.equal(mid.caret, 10);
    // leading: the token is the first word, args ride along
    const lead = replaceSlashToken("/th dark", 3, "/th", "/theme");
    assert.equal(lead.draft, "/theme dark");
    assert.equal(lead.caret, 6);
    // a caret outside any /word is a no-op
    const miss = replaceSlashToken("use /sk to fix", 14, "/sk", "/skill");
    assert.equal(miss.draft, "use /sk to fix");
    // a drifted query (stale token) is a no-op
    const stale = replaceSlashToken("use /sk to fix", 7, "/zz", "/skill");
    assert.equal(stale.draft, "use /sk to fix");
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
    assert.ok(composer.includes(":aria-expanded"), "the editor exposes open state");
    assert.ok(composer.includes("aria-activedescendant"), "the highlight is announced");
    assert.ok(composer.includes(`e.key === "ArrowDown"`), "arrow keys move the selection");
    assert.ok(composer.includes(`e.key === "Escape"`), "Esc dismisses the palette");
    assert.ok(composer.includes(`e.key === "Tab"`), "Tab completes without submitting");
    assert.ok(composer.includes("@pointerdown.prevent"), "clicking a row never steals focus");
  });

  it("the composer gates the slash palette on the caret's token, not draft start", () => {
    assert.ok(composer.includes("slashTokenAt"), "the token rule drives the gates");
    assert.equal(
      composer.includes('startsWith("/")'),
      false,
      "the draft-starts-with gate is retired (t_d1a2951d)",
    );
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

  // t_d1fe01da: the dispatch wire carries the invoking tab's session, so
  // a session-scoped command (/export, /label, /branch) acts on THAT tab
  // instead of demanding the id in its args - and a refusal arrives with
  // the server's own words, never a client-side "no handler".
  it("the dispatch binds the invoking tab's session and surfaces the server's own message", () => {
    const wire = readFileSync(join(here, "../app/composables/commands.ts"), "utf8");
    assert.match(wire, /sessionId = "",/, "dispatchCommand takes the invoking session");
    assert.ok(
      wire.includes("if (sessionId) body.session_id = sessionId"),
      "the wire field is session_id, and a tab with no session yet sends none (args keep their legacy shape)",
    );
    assert.ok(
      host.includes('dispatchCommand(name, args, st.sessionId || "")'),
      "the host hands the dispatching tab's own session to the wire",
    );
    assert.ok(
      wire.includes("throw new Error(detail || `${r.status}`)"),
      "a refused dispatch surfaces the server's message - the client never invents a miss",
    );
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
    assert.ok(filterPaletteCommands("logs", cmds).some((c) => c.id === "open:observability"));
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

  // Endo rider 2026-11-12: the palette had Escape but no outside-click
  // close. The overlay carries no scrim by design, so the outside click is
  // a document listener bounded by the root ref - and it must be pointerdown
  // so the opening click cannot immediately re-close the palette.
  it("an outside click closes the overlay (no scrim, document listener)", () => {
    assert.ok(overlay.includes('ref="rootEl"'), "the surface is ref-bound");
    assert.ok(
      overlay.includes('document.addEventListener("pointerdown", onDocPointer)'),
      "outside clicks are caught on the document",
    );
    assert.ok(
      overlay.includes('document.removeEventListener("pointerdown", onDocPointer)'),
      "the listener dies with the component",
    );
    assert.ok(
      overlay.includes("rootEl.value?.contains(target as Node)"),
      "only clicks OUTSIDE the surface close it",
    );
    assert.ok(overlay.includes('emit("close")'), "the close rides the existing @close seam");
    // Escape must still be the same path (no key handler rewrite)
    assert.ok(/e\.key === "Escape"[\s\S]{0,80}emit\("close"\)/.test(overlay));
    // item navigation is untouched: the row click stays a run(), not a close
    assert.ok(/@click="run\(c\)"/.test(overlay), "row clicks still run the command");
  });

  // the shell's trigger is the one outside surface the overlay must not
  // close on pointerdown: its own click toggles, so closing first would
  // flicker closed-then-reopened. One toggle serves both doors.
  it("the nav-cluster trigger toggles instead of flickering", () => {
    const cluster = readFileSync(
      join(here, "../app/components/nav/NavCluster.vue"),
      "utf8",
    );
    assert.ok(cluster.includes("data-palette-trigger"), "the trigger is marked");
    assert.ok(
      overlay.includes('target.closest("[data-palette-trigger]")'),
      "the outside handler skips the trigger",
    );
    assert.ok(app.includes('@open-palette="togglePalette"'), "the trigger rides the toggle");
    assert.ok(
      /function togglePalette\(\)[\s\S]{0,200}paletteOpen\.value = !paletteOpen\.value/.test(app),
      "one toggle closes an open palette",
    );
    assert.ok(!app.includes("function openPalette("), "the one-way opener is gone");
  });

  it("the overlay CSS reads the tokens, and hangs from the top bar (card t_d4443ab5)", () => {
    assert.ok(css.includes(".command-palette {"), "the overlay has real CSS");
    const block = css.slice(css.indexOf(".command-palette {"), css.indexOf(".command-palette-input"));
    assert.ok(block.includes("position: fixed") && block.includes("top:"), "top-center fixed");
    // VS Code quick-open geometry: the panel's top edge IS the top bar's
    // lower edge (one --menubar-h for both), so it overlays the panes and
    // never pushes them
    assert.ok(
      block.includes("top: var(--menubar-h)"),
      "the panel hangs from the menubar's own height",
    );
    assert.ok(
      /--menubar-h: 35px/.test(css),
      "the menubar height is the one anchor number",
    );
    assert.ok(
      /\.menubar\s*\{[^}]*height: var\(--menubar-h\)/.test(css),
      "the bar itself reads the same number",
    );
    assert.ok(block.includes("width: 420px"), "the overlay matches the trigger's band");
    assert.ok(css.includes("background: var(--z2)"), "tokens carry it");
  });

  // card t_d83b54b8: ONE geometry, two doors. The trigger click and Ctrl+K
  // ride ONE toggle into ONE overlay, so ONE rule anchors the panel - at
  // the top, under the bar. The way this breaks on one path only is a
  // SECOND anchor rule (a media query, a late override) re-seating the
  // panel mid-screen, which the block-contents test above cannot see:
  // it reads the first block and stops. Pin the count across every
  // stylesheet instead.
  it("one anchor rule for the panel - a second one could re-seat it (card t_d83b54b8)", () => {
    const cssDir = join(here, "../app/assets/css");
    const sheets = readdirSync(cssDir).filter((f: string) => f.endsWith(".css"));
    const anchor = /\.command-palette\s*\{/g;
    const homes = sheets.filter(
      (f: string) => (readFileSync(join(cssDir, f), "utf8").match(anchor) ?? []).length > 0,
    );
    assert.deepEqual(homes, ["main.css"], "the panel's geometry lives in main.css alone");
    const total = homes.reduce(
      (n: number, f: string) => n + (readFileSync(join(cssDir, f), "utf8").match(anchor) ?? []).length,
      0,
    );
    assert.equal(total, 1, "exactly one .command-palette anchor rule, never a second");
    // and that one rule is the fixed, top-hanging, centred band
    const one = css.slice(css.indexOf(".command-palette {"));
    const block = one.slice(0, one.indexOf("}"));
    assert.match(block, /position:\s*fixed/, "the one rule is the fixed overlay");
    assert.match(block, /top:\s*var\(--menubar-h\)/, "anchored at the bar's lower edge");
    assert.match(block, /left:\s*50%/, "centred on the bar");
    assert.match(block, /transform:\s*translateX\(-50%\)/, "centred on the bar");
  });
});

// --- card t_cc6d0920: the TAKEOVER -------------------------------------
// HIS 2026-11-16 reconfirmation: the palette was never meant to hang its
// own input box above the bar - the top bar's time field IS the input, in
// place. The field element is TELEPORTED into the cluster's seat (one
// element, one focus path - never a second copy), the panel below is the
// results dropdown alone (the merged t_d83b54b8 geometry, untouched), and
// the bar's other chrome (arrows + the Ctrl+K toggle) hides by
// VISIBILITY so the seat cannot slide while the field is up.
describe("the palette takeover (t_cc6d0920)", () => {
  const overlay = readFileSync(
    join(here, "../app/components/nav/CommandPalette.vue"),
    "utf8",
  );
  const cluster = readFileSync(
    join(here, "../app/components/nav/NavCluster.vue"),
    "utf8",
  );
  const menubar = readFileSync(
    join(here, "../app/components/shell/MenuBar.vue"),
    "utf8",
  );
  const app = readFileSync(join(here, "../app/app.vue"), "utf8");
  const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");

  it("the panel TELEPORTS its field into the bar's seat - one element, never a copy", () => {
    assert.equal(
      (overlay.match(/<input/g) ?? []).length,
      1,
      "exactly one input: the takeover moves the field, it never clones it",
    );
    assert.match(overlay, /<Teleport[\s\S]{0,120}to="#nav-palette-slot"/);
    assert.match(
      overlay,
      /:disabled="!inPlace"/,
      "below the drawer breakpoint there is no seat - the field falls back into the panel",
    );
    assert.match(
      overlay,
      /inPlace \? 'nav-palette-input is-live' : 'command-palette-input'/,
      "one field, two seats: the bar's chip or the panel's own row",
    );
    // the field is not a descendant of the panel root, so the outside-click
    // close must exempt it by ref
    assert.ok(
      overlay.includes("inputEl.value?.contains(target)"),
      "a pointerdown in the teleported field never closes the palette",
    );
    assert.match(overlay, /inPlace: boolean/, "the seat bit is a declared prop");
    // a live breakpoint cross re-mounts the field in the other seat (the
    // Teleport flips `disabled`) - the caret rides along, or the field on
    // screen would swallow typing
    assert.match(
      overlay,
      /watch\(\s*\(\) => props\.inPlace,[\s\S]{0,160}inputEl\.value\?\.focus\(\)/,
      "the field keeps its caret when the seat moves",
    );
  });

  it("the cluster IS the seat: the clock button stands down, the slot takes its box", () => {
    assert.match(cluster, /id="nav-palette-slot"/, "the teleport target exists");
    assert.match(cluster, /class="nav-palette-slot"/);
    assert.match(
      cluster,
      /v-if="!paletteOpen"[\s\S]{0,200}class="nav-palette-input"/,
      "the clock yields to the field - they are never up together",
    );
    assert.match(
      cluster,
      /:class="\{ 'palette-open': paletteOpen \}"/,
      "the cluster knows the palette is up",
    );
    assert.match(cluster, /paletteOpen: boolean/, "and declares the bit");
    // display: contents is what makes the seat transparent to the flex row:
    // the teleported field is a flex item exactly as the button was
    assert.match(css, /\.nav-palette-slot\s*\{[^}]*display:\s*contents/);
    // and the field carries the affordance's own seat geometry
    assert.match(css, /\.nav-palette-input\.is-live\s*\{/);
    assert.match(
      css,
      /\.nav-palette-input\.is-live\s*\{[^}]*cursor:\s*text/,
      "the seat is a text field while it holds one",
    );
  });

  it("the bar's other chrome HIDES without moving the seat", () => {
    const hide = css.slice(css.indexOf(".nav-cluster.palette-open .nav-btn"));
    const block = hide.slice(hide.indexOf("{"), hide.indexOf("}"));
    assert.match(block, /visibility:\s*hidden/, "hidden, not removed");
    assert.match(block, /pointer-events:\s*none/, "a hidden control never takes a click");
    assert.doesNotMatch(
      block,
      /display:\s*none/,
      "display:none would drop the arrows + toggle from the row and slide the seat sideways",
    );
  });

  it("app.vue feeds the ONE open bit to both halves", () => {
    assert.match(app, /:palette-open="paletteOpen"/, "the cluster reads the one state");
    assert.match(app, /:in-place="!isMobile"/, "below 768px the seat does not exist");
    assert.match(menubar, /:palette-open="paletteOpen"/, "MenuBar passes it down");
    assert.match(menubar, /paletteOpen: boolean/, "MenuBar declares it");
    // the field is a REAL input now, so a click in it keeps the caret: the
    // shell's pane-focus click pass must not claim the gesture and hand the
    // caret to the composer (live: the first click after boot lost it)
    assert.ok(
      app.includes('.closest(".command-palette, #nav-palette-slot")'),
      "a click in the palette's own surfaces is not a pane gesture",
    );
    // the clock keeps ticking while it is hidden (the timer is the
    // component's, not the button's) - Esc brings back a current time
    assert.match(
      cluster,
      /onMounted\(armClock\)/,
      "the clock timer outlives the button's v-if",
    );
  });

  it("the results panel keeps the merged anchor geometry (t_d83b54b8)", () => {
    const one = css.slice(css.indexOf(".command-palette {"));
    const block = one.slice(0, one.indexOf("}"));
    assert.match(block, /top:\s*var\(--menubar-h\)/, "the list drops from the bar's edge");
    assert.match(block, /width:\s*420px/, "the tuned band");
    assert.match(block, /z-index:\s*200/, "over the panes, above every chrome");
  });
});