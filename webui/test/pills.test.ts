// omega webui: reference pills. The /skill invocations
// and @file attachments of the composer turn into inline chips that
// REPLACE their token spans (card t_e67c372b); the sent message keeps
// the pill shape. The token model is
// pills.ts (pure, imported here directly); the components' wiring is
// pinned at the source level (the served shape), as palette.test.ts
// does. Run: node --test webui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel: string): string => readFileSync(join(here, rel), "utf8");

const composer = read("../app/components/chat/AppComposer.vue");
const chatMessage = read("../app/components/chat/ChatMessage.vue");
const pill = read("../app/components/chat/ReferencePill.vue");
const chatTab = read("../app/components/ChatTab.vue");
const host = read("../app/composables/chat.ts");
const commands = read("../app/composables/commands.ts");
const icons = read("../app/composables/icons.ts");
const css = read("../app/assets/css/main.css");
const coreAtMention = read("../../core/atmention.go");
const serverAtMention = read("../../agent/api/server_atmention.go");

import {
  chipFor,
  deleteTokenAt,
  editorNodes,
  filterMentionRows,
  mentionDirOf,
  mentionLeafQuery,
  mentionRows,
  scanRefTokens,
  splitRefSegments,
  tokenBeforeCaret,
} from "../app/composables/pills.ts";

// --- the token scan ---------------------------------------------------

describe("reference token scan (t_7f09bd0c)", () => {
  it("a leading /word is the skill invocation - the composer accepts it at draft start", () => {
    const tokens = scanRefTokens("/greet-writer write a note");
    assert.equal(tokens.length, 1);
    assert.deepEqual(tokens[0], {
      kind: "skill",
      token: "/greet-writer",
      label: "greet-writer",
      name: "greet-writer",
      start: 0,
    });
  });

  it("a mid-text /word is prose - no pill source", () => {
    assert.deepEqual(scanRefTokens("the flag -/not-a-pill stays"), []);
  });

  // card t_d4443ab5: the composer's chip editor reads skill tokens the way
  // it reads @file tokens - anywhere a word starts - because its slash
  // palette is token-based (t_d1a2951d accepts a completed /word mid-draft).
  // The transcript keeps the leading-invocation law: one scan, one switch.
  it("the composer scan pills an inline /skill; the transcript keeps it prose", () => {
    const text = "say hi then /greet-writer write it";
    const inline = scanRefTokens(text, { inlineSkills: true });
    assert.equal(inline.length, 1);
    assert.deepEqual(inline[0], {
      kind: "skill",
      token: "/greet-writer",
      label: "greet-writer",
      name: "greet-writer",
      start: text.indexOf("/greet"),
    });
    assert.deepEqual(scanRefTokens(text), [], "the transcript surface: leading only");
    // the leading form reads IDENTICALLY on both surfaces
    assert.deepEqual(
      scanRefTokens("/greet-writer x", { inlineSkills: true }),
      scanRefTokens("/greet-writer x"),
    );
    // the boundary shape is the @ token's: a glued "/" stays prose
    assert.deepEqual(scanRefTokens("flag -/not-a-pill", { inlineSkills: true }), []);
    // and the chip plan follows the same switch
    const chips = editorNodes(
      text,
      { caret: text.length, paletteOpen: false, forced: new Set(), dissolved: new Set() },
      () => true,
      { inlineSkills: true },
    );
    assert.deepEqual(
      chips.map((p) => (p.type === "chip" ? "CHIP:" + p.token.token : p.text)),
      ["say hi then ", "CHIP:/greet-writer", " write it"],
    );
  });

  it("a standalone @path is a file token, mid-word @ is an email", () => {
    const tokens = scanRefTokens("read @docs/readme.md then user@host.io");
    assert.equal(tokens.length, 1);
    const tok = tokens[0]!;
    assert.equal(tok.kind, "file");
    assert.equal(tok.token, "@docs/readme.md");
    assert.equal(tok.label, "readme.md");
    assert.equal(tok.name, "docs/readme.md");
    assert.equal(tok.start, 5);
  });

  it("a directory token (trailing slash) has no basename - no pill", () => {
    // the drill form never renders as a pill; it is a list state, not a pill
    assert.deepEqual(
      scanRefTokens("look at @src/ for it").filter((t) => t.token.includes("/")),
      [],
    );
  });

  it("an @ token after an open paren still counts; the file pill rides any position", () => {
    const tokens = scanRefTokens("(@api.ts) here");
    assert.equal(tokens.length, 1);
    assert.equal(tokens[0]!.name, "api.ts");
  });

  it("trailing punctuation belongs to the sentence, not the token", () => {
    const tokens = scanRefTokens("see @docs/x.md, please");
    assert.equal(tokens[0]!.token, "@docs/x.md");
    const segs = splitRefSegments("see @docs/x.md, please");
    assert.deepEqual(
      segs.map((s) => (s.token ? "PILL:" + s.token.token : s.text)),
      ["see ", "PILL:@docs/x.md", ", please"],
    );
  });

  it("multiple tokens sort by position", () => {
    const tokens = scanRefTokens("/greet hi @a.md @b.ts");
    assert.deepEqual(
      tokens.map((t) => t.name),
      ["greet", "a.md", "b.ts"],
    );
  });
});

describe("sent-message pill render (t_7f09bd0c)", () => {
  it("the text splits into pill tokens and verbatim segments", () => {
    const segs = splitRefSegments("/greet see @docs/x.md, please");
    assert.deepEqual(
      segs.map((s) => (s.token ? "PILL:" + s.token.token : s.text)),
      ["PILL:/greet", " see ", "PILL:@docs/x.md", ", please"],
    );
  });

  it("a pill-free text stays one verbatim segment", () => {
    const segs = splitRefSegments("no tokens here");
    assert.equal(segs.length, 1);
    assert.equal(segs[0]!.text, "no tokens here");
  });
});

// --- the chip law (card t_e67c372b: chip replaces text) ---------------

describe("chipFor - the chip rule per token", () => {
  const tok = scanRefTokens("see @docs/readme.md now")[0]!;
  const base = { paletteOpen: false, forced: new Set<string>(), dissolved: new Set<string>() };

  it("a parked caret (outside the span) stands the chip", () => {
    assert.equal(chipFor(tok, { ...base, caret: 20 }), true);
    assert.equal(chipFor(tok, { ...base, caret: 4 }), true, "before the span");
  });

  it("the caret strictly inside the span: the token is being edited - raw", () => {
    assert.equal(chipFor(tok, { ...base, caret: tok.start + 1 }), false);
    assert.equal(chipFor(tok, { ...base, caret: tok.start + tok.token.length - 1 }), false);
  });

  it("the caret at the span end with the palette open: the query is running - raw", () => {
    assert.equal(
      chipFor(tok, { ...base, caret: tok.start + tok.token.length, paletteOpen: true }),
      false,
    );
  });

  it("the caret at the span end, palette closed: the chip forms (Esc finalizes)", () => {
    assert.equal(chipFor(tok, { ...base, caret: tok.start + tok.token.length }), true);
  });

  it("forced chips regardless of the caret - the accept path's fresh swallow", () => {
    assert.equal(chipFor(tok, { ...base, caret: tok.start, forced: new Set([tok.token]) }), true);
  });

  it("dissolved: raw while the caret works the span (end included), re-formed once it leaves", () => {
    const dissolved = new Set([tok.token]);
    const end = tok.start + tok.token.length;
    assert.equal(chipFor(tok, { ...base, caret: end, dissolved }), false, "right at the reveal");
    assert.equal(chipFor(tok, { ...base, caret: tok.start + 3, dissolved }), false, "editing inside");
    assert.equal(chipFor(tok, { ...base, caret: 30, dissolved }), true, "caret moved away");
    assert.equal(chipFor(tok, { ...base, caret: 0, dissolved }), true, "caret before the span");
  });

  it("the rule is per-token: a dissolved key leaves its identical twin alone", () => {
    const text = "@a.md middle @a.md";
    const [first, second] = scanRefTokens(text);
    const end2 = second!.start + second!.token.length;
    assert.equal(
      chipFor(second!, { ...base, caret: end2, dissolved: new Set([second!.token]) }),
      false,
      "the dissolved one stays raw",
    );
    assert.equal(
      chipFor(first!, { ...base, caret: end2, dissolved: new Set([second!.token]) }),
      true,
      "the twin keeps its chip",
    );
  });
});

describe("editorNodes - the editor's DOM plan", () => {
  const pillable = (t: { kind: string; name: string }): boolean =>
    t.kind === "file" || t.name === "greet";
  const opts = (caret: number) => ({
    caret,
    paletteOpen: false,
    forced: new Set<string>(),
    dissolved: new Set<string>(),
  });

  it("a pill-able token becomes a chip node that carries exactly its raw token", () => {
    const nodes = editorNodes("/greet hi", opts(10), pillable);
    assert.deepEqual(
      nodes.map((n) =>
        n.type === "chip" ? "CHIP:" + n.token.token + "@" + n.token.start : "TEXT:" + n.text,
      ),
      ["CHIP:/greet@0", "TEXT: hi"],
    );
  });

  it("the draft text is recoverable from the plan: chips carry their spans, text nodes carry the rest", () => {
    const draft = "/greet see @docs/x.md, please";
    const nodes = editorNodes(draft, opts(draft.length), pillable);
    const read = nodes
      .map((n) => (n.type === "chip" ? n.token.token : n.text))
      .join("");
    assert.equal(read, draft);
  });

  it("a non-pillable token (/theme, a shell verb) folds back into the text", () => {
    const nodes = editorNodes("/theme hi", opts(9), pillable);
    assert.deepEqual(
      nodes.map((n) => (n.type === "chip" ? "CHIP" : "TEXT:" + n.text)),
      ["TEXT:/theme hi"],
    );
  });

  it("the running query stays raw (palette open at the span end)", () => {
    const draft = "/greet args";
    const nodes = editorNodes(draft, { ...opts(6), paletteOpen: true }, pillable);
    assert.equal(nodes.length, 1);
    assert.equal(nodes[0]!.type, "text");
  });
});

// --- the backspace law ------------------------------------------------

describe("token delete (backspace on a chip dissolves it to its text)", () => {
  it("tokenBeforeCaret fires only at the token's end", () => {
    assert.equal(tokenBeforeCaret("/greet hi", 6)!.token, "/greet");
    assert.equal(tokenBeforeCaret("/greet hi", 9), null); // after "hi", no token behind
    assert.equal(tokenBeforeCaret("x @a.md", 7)!.token, "@a.md");
    assert.equal(tokenBeforeCaret("x @a.md", 6), null); // one char inside the token
    assert.equal(tokenBeforeCaret("", 0), null);
  });

  it("deleteTokenAt (the chip x's outright remove) cuts the span and parks the caret at the cut", () => {
    const tok = scanRefTokens("/greet rest")[0]!;
    const next = deleteTokenAt("/greet rest", tok);
    assert.equal(next.draft, " rest");
    assert.equal(next.caret, 0);
  });
});

// --- the @ path list (directories first, leaf filtering) --------------

describe("@ autocomplete paths (dirs first, drill-in)", () => {
  it("mentionRows orders directories first, each alphabetical", () => {
    const rows = mentionRows([
      { name: "zeta.ts", is_dir: false },
      { name: "api", is_dir: true },
      { name: "docs", is_dir: true },
      { name: "alpha.ts", is_dir: false },
    ]);
    assert.deepEqual(rows.map((r) => r.name), ["api", "docs", "alpha.ts", "zeta.ts"]);
    assert.equal(rows[0]!.kind, "dir");
    assert.equal(rows[3]!.kind, "file");
  });

  it("the drill reads the token's dir; a bare token browses the root", () => {
    assert.equal(mentionDirOf("@"), "");
    assert.equal(mentionDirOf("@docs/re"), "docs/");
    assert.equal(mentionLeafQuery("@docs/read"), "read");
    assert.equal(mentionLeafQuery("@"), "");
  });

  it("filterMentionRows filters on the leaf under a drill, the whole leaf bare", () => {
    const rows = mentionRows([{ name: "read.md", is_dir: false }, { name: "ref.md", is_dir: false }]);
    assert.equal(filterMentionRows("@docs/re", rows).length, 2); // "re" matches both
    assert.deepEqual(filterMentionRows("@docs/read", rows).map((r) => r.name), ["read.md"]);
    assert.equal(filterMentionRows("@", rows).length, 2);
  });
});

// --- production-boundary wiring (the served shape) --------------------

describe("composer chip law wiring (source-pinned, card t_e67c372b)", () => {
  it("the composer's editor is a contenteditable div with the placeholder", () => {
    assert.ok(/ref="ta"\s*\n\s*class="composer-input"/.test(composer), "the editor div is the ta handle");
    assert.ok(composer.includes('contenteditable="true"'), "the editor is editable");
    assert.ok(composer.includes("data-placeholder"), "the placeholder rides the data attr");
    assert.ok(composer.includes('role="textbox"'), "the editor is announced");
  });

  it("the strip is gone - chips render inline in the flow", () => {
    assert.doesNotMatch(composer, /ref-strip/);
    assert.doesNotMatch(composer, /ref-pill-wrap/);
    assert.ok(composer.includes('wrap.className = "ref-chip"'), "the non-editable chip wrapper");
    assert.ok(
      composer.includes('wrap.contentEditable = "false"'),
      "the chip is non-editable",
    );
    assert.ok(
      composer.includes("wrap.dataset.refchip"),
      "the reader's marker: the chip is a draft span, not text",
    );
    assert.ok(
      composer.includes("wrap.dataset.token = t.token"),
      "chip data-token carries the raw span out",
    );
  });

  it("ONE renderer, two surfaces - the chip mounts the same pill component", () => {
    assert.ok(composer.includes('from "./ReferencePill.vue"'));
    assert.ok(chatMessage.includes('from "./ReferencePill.vue"'));
    assert.ok(composer.includes("h(ReferencePill"), "the chip mounts the renderer");
  });

  it("the chip keeps the open law + the outright-remove affordance", () => {
    assert.ok(composer.includes("openViewerRef?.({ kind: t.kind, name: t.name })"));
    assert.ok(composer.includes("onClick: () => removeToken(t)"), "the x removes the token");
    assert.ok(chatMessage.includes("DOCK_VIEWER_OPEN_KEY"), "the message pill rides the same inject");
  });

  it("backspace behind a chip dissolves - the reveal, not the delete", () => {
    assert.ok(composer.includes("function dissolveBehindCaret"), "the dissolve law");
    assert.ok(
      composer.includes('e.key === "Backspace" && !paletteOpen.value && dissolveBehindCaret()'),
      "palette-open backspace edits the query char by char, dissolved spans delete a char",
    );
    assert.ok(
      composer.includes("tokenBeforeCaret(props.draft, caret, CHIP_SCAN)"),
      "the dissolve target is the token behind the caret (the editor's chip scan)",
    );
    assert.ok(
      composer.includes("dissolvedChip.value = next") &&
        composer.includes('next.add(tok.token)'),
      "the reveal is suppressed while the caret works the span",
    );
  });

  it("every chip call site scans with the composer's inline-skill switch", () => {
    assert.ok(
      composer.includes("const CHIP_SCAN = { inlineSkills: true } as const;"),
      "one surface switch, named",
    );
    assert.ok(
      composer.includes("scanRefTokens(props.draft, CHIP_SCAN)"),
      "the mount probe and the sync's live-key sweep",
    );
    assert.ok(
      /editorNodes\([\s\S]{0,400}?isPill,\s*CHIP_SCAN,/.test(composer),
      "the plan builds on the same scan",
    );
  });

  it("the accept path fresh-swallows: the completed token chips immediately", () => {
    assert.ok(
      composer.includes('forced.add("/" + item.name)'),
      "the skill accept keys the chip by its RAW token (chipKeyOf), not the bare name",
    );
    assert.ok(composer.includes("forced.add(insertion)"), "the mention pick");
    assert.ok(composer.includes("const forced = new Set(forcedChip.value);"),
      "sets are replaced whole (the plan's computed re-runs)");
  });

  it("the editor sync rebuilds only on disagreement; typing never churns the caret", () => {
    assert.ok(composer.includes("function syncEditor"), "the sync");
    assert.ok(composer.includes("domText === props.draft"), "text parity guards the rebuild");
    assert.ok(composer.includes("watch(editorPlan"), "the plan drives the chip layer");
    assert.ok(composer.includes("placeCaretAt"), "the caret is restored at the same draft index");
  });

  it("the sent/transcript pill law is unchanged", () => {
    assert.ok(chatMessage.includes("<ReferencePill"), "the bubble mounts the pill renderer");
    assert.ok(chatMessage.includes('props.role === "user"'), "the scan is user-bubble only");
    assert.ok(chatMessage.includes("v-html=\"rendered\""), "the assistant render stays");
    assert.ok(css.includes(".bubble .ref-pill { vertical-align: middle; }"));
  });

  it("the canvas click law passes chip clicks through", () => {
    assert.ok(composer.includes(".ref-chip, .model-select"), "the chip is a control seat");
  });

  it("the sent draft carries the raw tokens to /chat (the expansion law intact)", () => {
    assert.ok(coreAtMention.includes("ReferenceOnly bool"), "the core env carries the flag");
    assert.ok(
      coreAtMention.includes("if env.ReferenceOnly {\n\t\treturn token, nil, atLiteral, nil\n\t}"),
      "file tokens ride through as references",
    );
    assert.ok(serverAtMention.includes("ReferenceOnly: true"), "the webui path sets it");
    const skillBranch = coreAtMention.indexOf('strings.HasPrefix(path, "skill:")');
    const refOnly = coreAtMention.indexOf("if env.ReferenceOnly {");
    assert.ok(refOnly > skillBranch, "the file branches turn literal AFTER the injections");
  });

  it("the token grammar the server keeps literal equals the chips' grammar", () => {
    assert.ok(coreAtMention.includes("card t_7f09bd0c"));
  });
});

describe("the send + attach semantics (t_7f09bd0c)", () => {
  it("a send ctx exists and ChatTab passes model/thinking/fork into the command", () => {
    assert.ok(host.includes("export interface CommandSend"), "the host declares the send ctx");
    assert.ok(chatTab.includes("model: modelValue.value"), "the send carries the model");
    assert.ok(chatTab.includes("thinking: thinkingLevelWire(props.thinking)"));
    assert.ok(chatTab.includes("fork: fork ?? undefined"), "the fork anchor rides");
  });
});

describe("styles (the chip family law)", () => {
  it("the chip + pill read the design tokens; no color literals", () => {
    for (const sel of [".ref-chip", ".ref-pill", ".ref-pill.skill", ".ref-remove"]) {
      assert.ok(css.includes(sel), "missing selector: " + sel);
    }
    assert.ok(css.includes("border: 1px solid var(--stroke)"), "the pill rides var(--stroke)");
    assert.ok(!/\.ref-pill\b[^}]*#[0-9a-f]/i.test(css), "not a raw hex outside var(--)");
  });

  it("the sent bubble keeps the pill inline with the text flow", () => {
    assert.ok(css.includes(".bubble .ref-pill { vertical-align: middle; }"));
  });

  it("the editor reads pre-wrap so text nodes carry their newlines; the placeholder is the :empty ::before", () => {
    const input = css.slice(css.indexOf(".composer .composer-input {"));
    assert.ok(input.includes("white-space: pre-wrap"), "text nodes carry newlines");
    assert.ok(input.includes("line-height: 21px"), "the deterministic row math holds");
    assert.ok(css.includes(".composer .composer-input:empty::before"), "the placeholder rule");
    assert.ok(css.includes('content: attr(data-placeholder)'), "the placeholder reads the data attr");
    assert.ok(css.includes("cursor: text"), "the editor is a text surface");
  });
});

describe("pill component + iconset (source-pinned)", () => {
  it("the pill renders iconset icons - zap for skills, the document glyph for files", () => {
    assert.ok(pill.includes("'zap'"), "skills carry the zap");
    assert.ok(pill.includes("'file-generic'"), "files carry the document glyph");
    assert.ok(icons.includes("Zap,"), "the zap rides the lucide import map");
    assert.ok(icons.includes("zap: Zap"), "one entry, one name - the iconset law");
  });

  it("pill data flows into the renderer unchanged; skills read /name, files the basename", () => {
    assert.ok(pill.includes('"/" + props.pill.name'));
    assert.ok(pill.includes("props.pill.label"));
    assert.ok(pill.includes('(e: "open")'), "the pill offers its open event");
  });
});

describe("shared list-keyboard grammar (no duplication)", () => {
  it("clampIndex is the one nav arithmetic - declared in commands, used by every list", () => {
    assert.ok(commands.includes("export function clampIndex"));
    // palette.test.ts pins the command palette + composer's use; the
    // pills law: the composer's list nav still rides the same helper
    assert.ok(composer.includes("clampIndex"), "the composer list nav rides clampIndex");
  });
});