// omega webui: unit tests for the 60vh max-height sweep - thinking blocks
// and tool RESULT blocks get the exact codeblock treatment (60vh cap +
// internal scroll + collapse affordance), tool-call command INPUTS keep
// their short one-liner voice, and the final assistant answer bubble is
// never capped. The caps live in main.css and the class split in
// ToolCallLine.vue, so the tests parse both and pin the rules that make
// the sweep true. Run: node --test webui/test/ (node >= 23)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");
const toolcall = readFileSync(
  join(here, "../app/components/chat/ToolCallLine.vue"),
  "utf8",
);

// Bulk rule extractor: every `selector { body }` block of the sheet as
// { selectors: string[], body: string }. Depth-aware, so @media/@keyframes
// wrappers and their inner blocks are paired correctly: at-rule wrappers
// and their bodies never leak into later selector text. Inner keyframe
// frames (to/from/%) show up as their own junk rules - harmless.
const RULES: { selectors: string[]; body: string }[] = [];
{
  let selStart = 0;
  const open: { sel: string; start: number }[] = [];
  for (let i = 0; i < css.length; i++) {
    if (css.startsWith("/*", i)) {
      const end = css.indexOf("*/", i);
      if (end !== -1) {
        i = end + 1;
        continue;
      }
    }
    const ch = css[i];
    if (ch === "{") {
      const sel = css.slice(selStart, i).replace(/\/\*[\s\S]*?\*\//g, " ")
        .trim();
      open.push({ sel, start: i + 1 });
      selStart = i + 1;
    } else if (ch === "}") {
      const block = open.pop();
      if (block && !block.sel.startsWith("@")) {
        RULES.push({
          selectors: block.sel.split(",").map((s: string) => s.trim())
            .filter(Boolean),
          body: css.slice(block.start, i),
        });
      }
      selStart = i + 1;
    }
  }
}

function decl(body: string, prop: string): string | null {
  const m = new RegExp(`(?:^|;)\\s*${prop}\\s*:\\s*([^;]+)`, "m").exec(body);
  return m ? m[1].trim() : null;
}

// Rules whose selector EXACTLY includes `sel` as one selector in the
// group (a selector like `.msg-elapsed` never matches `.msg`).
function rulesFor(sel: string) {
  return RULES.filter((r) => r.selectors.includes(sel));
}

describe("thinking block cap", () => {
  it(".thinking-body gets the codeblock treatment: 60vh + internal scroll", () => {
    const rs = rulesFor(".thinking-body");
    assert.ok(rs.length === 1, "exactly one .thinking-body rule");
    assert.equal(decl(rs[0].body, "max-height"), "60vh");
    assert.equal(decl(rs[0].body, "overflow-y"), "auto");
  });
});

describe("tool result cap", () => {
  it(".tool-output stays at the short 240px command cap", () => {
    const rs = rulesFor(".tool-output");
    assert.ok(rs.length >= 1, ".tool-output rule exists");
    for (const r of rs) {
      assert.equal(decl(r.body, "max-height"), "240px", ".tool-output keeps 240px");
    }
  });

  it(".tool-result overrides to the 60vh codeblock cap", () => {
    const rs = rulesFor(".tool-output.tool-result");
    assert.ok(rs.length === 1, "exactly one .tool-output.tool-result rule");
    assert.equal(decl(rs[0].body, "max-height"), "60vh");
  });

  it("result pre carries tool-result, command input does not", () => {
    const input = /class="tool-output"[ >]/.exec(toolcall);
    assert.ok(input, "command input keeps plain .tool-output");
    const result = /class="tool-output tool-result"/.exec(toolcall);
    assert.ok(result, "result pre carries the .tool-result class");
  });
});

describe("answer never capped", () => {
  // The elements the answer flows through. Fenced codeblocks inside .md
  // cap themselves by design (60vh + internal scroll) - they are not the
  // pane. Everything else in the chain must grow freely.
  const ANSWER_CHAIN = [".messages", ".msg", ".bubble", ".bubble .md"];

  it("no rule for the answer's chain declares a max-height", () => {
    for (const sel of ANSWER_CHAIN) {
      const rs = rulesFor(sel);
      for (const r of rs) {
        assert.equal(
          decl(r.body, "max-height"),
          null,
          `${sel} must grow freely (max-height found)`,
        );
        assert.equal(
          decl(r.body, "overflow"),
          null,
          `${sel} must grow freely (overflow found)`,
        );
      }
    }
  });

  it("every chain element actually has a rule (guards against renames)", () => {
    for (const sel of ANSWER_CHAIN) {
      assert.ok(rulesFor(sel).length >= 1, `${sel} rule exists`);
    }
  });
});

describe("codeblock reference pattern intact", () => {
  it("fenced code keeps its 60vh cap + internal scroll", () => {
    const rs = rulesFor(".bubble .md .codeblock pre");
    assert.ok(rs.length === 1, ".codeblock pre rule exists");
    assert.equal(decl(rs[0].body, "max-height"), "60vh");
    assert.equal(decl(rs[0].body, "overflow"), "auto");
  });
});