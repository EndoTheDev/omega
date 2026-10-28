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
    for (const cmd of ["/copy", "/extensions", "/panels", "/theme", "/variant", "/fork", "/retry", "/help"]) {
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

  it("slash commands never reach the submit path - ChatTab intercepts before the send", () => {
    const sendIdx = chatTab.indexOf("function onSend");
    const interceptIdx = chatTab.indexOf('text.startsWith("/")');
    assert.ok(interceptIdx > sendIdx, "the intercept lives in onSend");
    assert.ok(chatTab.includes("host.command(stateKey, text)"), "the host routes the command");
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