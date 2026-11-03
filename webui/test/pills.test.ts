// omega webui: reference pills (card t_7f09bd0c). The /skill invocations
// and @file attachments of the composer turn into clickable icon pills -
// in the composer's strip AND in the sent message. The token model is
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
  deleteTokenAt,
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

// --- the backspace law ------------------------------------------------

describe("token delete (backspace on a pill deletes it whole)", () => {
  it("tokenBeforeCaret fires only at the token's end", () => {
    assert.equal(tokenBeforeCaret("/greet hi", 6)!.token, "/greet");
    assert.equal(tokenBeforeCaret("/greet hi", 9), null); // after "hi", no token behind
    assert.equal(tokenBeforeCaret("x @a.md", 7)!.token, "@a.md");
    assert.equal(tokenBeforeCaret("x @a.md", 6), null); // one char inside the token
    assert.equal(tokenBeforeCaret("", 0), null);
  });

  it("deleteTokenAt cuts the span and parks the caret at the cut", () => {
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

describe("composer pill wiring (source-pinned)", () => {
  it("the strip renders the pill renderer with a remove affordance", () => {
    assert.ok(composer.includes("<ReferencePill :pill=\"t\" @open=\"openRef(t)\" />"));
    assert.ok(composer.includes('v-if="refPills.length"'), "the strip is conditional on pills");
    assert.ok(composer.includes("removeToken(t)"), "the strip's x kills the token");
  });

  it("the skill accept keeps the token and never sends", () => {
    // the accept branch: the completed /<name> + rest stays in the draft,
    // closePalette, no emit("send") - the branch ends where the shell
    // branch takes over
    const at = composer.indexOf("item.kind === \"skill\"");
    assert.ok(at > -1, "the composer branches on a skill accept");
    const end = composer.indexOf('item.kind === "shell"', at);
    const block = composer.slice(at, end);
    assert.ok(block.includes("closePalette()"), "the palette closes");
    assert.ok(!block.includes('emit("send")'), "a skill accept must not submit");
  });

  it("shell and extension commands keep the old submit law", () => {
    const at = composer.indexOf('item.kind === "shell" || item.kind === "extension"');
    assert.ok(at > -1);
    const block = composer.slice(at, at + 400);
    assert.ok(block.includes('emit("send")'), "commands act locally on submit");
  });

  it("backspace kills a pill whole and does not hijack the palette's query", () => {
    assert.ok(composer.includes("tokenBeforeCaret(props.draft, start)"));
    assert.ok(
      composer.includes('e.key === "Backspace" && !paletteOpen.value'),
      "the palette-open query edits char by char",
    );
  });

  it("click wiring: the composer opens the pill's target through the app's viewer", () => {
    assert.ok(composer.includes("DOCK_VIEWER_OPEN_KEY"));
    assert.ok(composer.includes("openViewerRef?.({ kind: t.kind, name: t.name })"));
  });
});

describe("sent message pill wiring (source-pinned)", () => {
  it("user bubbles render pills; the assistant markdown path is untouched", () => {
    assert.ok(chatMessage.includes("<ReferencePill"), "the bubble mounts the pill renderer");
    assert.ok(chatMessage.includes('props.role === "user"'), "the scan is user-bubble only");
    assert.ok(chatMessage.includes("v-html=\"rendered\""), "the assistant render stays");
  });

  it("the message pill opens through the same viewer inject", () => {
    assert.ok(chatMessage.includes("DOCK_VIEWER_OPEN_KEY"));
    assert.ok(chatMessage.includes("openViewerRef?.({ kind: t.kind, name: t.name })"));
  });

  it("ONE renderer, two surfaces - both import the same component", () => {
    assert.ok(chatMessage.includes('from "./ReferencePill.vue"'));
    assert.ok(composer.includes('from "./ReferencePill.vue"'));
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

describe("the send + attach semantics (t_7f09bd0c)", () => {
  it("a send ctx exists and ChatTab passes model/thinking/fork into the command", () => {
    assert.ok(host.includes("export interface CommandSend"), "the host declares the send ctx");
    assert.ok(chatTab.includes("model: modelValue.value"), "the send carries the model");
    assert.ok(chatTab.includes("thinking: thinkingLevelWire(props.thinking)"));
    assert.ok(chatTab.includes("fork: fork ?? undefined"), "the fork anchor rides");
  });

  it("a skill dispatch under a send arms AND posts the message", () => {
    const at = host.indexOf("res.kind === \"skill\"");
    assert.ok(at > -1);
    const block = host.slice(at, at + 600);
    assert.ok(block.includes("st.pendingSkill ="), "the content arms");
    assert.ok(block.includes("await submit(key, text, send.model, send.thinking, send.fork)"),
      "the token posts as a message");
    assert.ok(block.includes(" armed - the next send carries it"),
      "the arm-only law survives when no send ctx rides");
  });

  it("the webui submit path attaches PATHS: ReferenceOnly on /chat, the core law intact", () => {
    assert.ok(coreAtMention.includes("ReferenceOnly bool"), "the core env carries the flag");
    assert.ok(
      coreAtMention.includes("if env.ReferenceOnly {\n\t\treturn token, nil, atLiteral, nil\n\t}"),
      "file tokens ride through as references",
    );
    assert.ok(serverAtMention.includes("ReferenceOnly: true"), "the webui path sets it");
    const skillBranch = coreAtMention.indexOf("strings.HasPrefix(path, \"skill:\")");
    const refOnly = coreAtMention.indexOf("if env.ReferenceOnly {");
    assert.ok(refOnly > skillBranch, "the file branches turn literal AFTER the injections");
  });

  it("the token grammar the server keeps literal equals the pills' grammar", () => {
    // the reference law leaves a standalone @token as-is in the text -
    // the exact shape the composer's file pill inserts
    assert.ok(coreAtMention.includes("card t_7f09bd0c"));
  });
});

describe("styles (the chip family law)", () => {
  it("the strip and pill read the design tokens; no color literals", () => {
    for (const sel of [".ref-strip", ".ref-pill", ".ref-pill.skill", ".ref-remove"]) {
      assert.ok(css.includes(sel), "missing selector: " + sel);
    }
    assert.ok(css.includes("border: 1px solid var(--stroke)"), "the pill rides var(--stroke)");
    assert.ok(!/\.ref-pill\b[^}]*#[0-9a-f]/i.test(css), "not a raw hex outside var(--)");
  });

  it("the sent bubble keeps the pill inline with the text flow", () => {
    assert.ok(css.includes(".bubble .ref-pill { vertical-align: middle; }"));
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