// omega webui: the day stamp renders as the day divider, never as bubble text.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const read = (rel: string): string => readFileSync(join(here, rel), "utf8");

const chatMessage = read("../app/components/chat/ChatMessage.vue");
const chatTab = read("../app/components/ChatTab.vue");
const css = read("../app/assets/css/main.css");

import { stripDayStamp, userDayStamp } from "../app/composables/datestamp.ts";
import { splitRefSegments } from "../app/composables/pills.ts";

describe("the stamp's display mirror (wire form: agent/datestamp.go)", () => {
  it("a stamped head yields its ISO day", () => {
    assert.equal(userDayStamp("[2026-11-28] hello"), "2026-11-28");
    assert.equal(userDayStamp("[2026-11-28]hello"), "2026-11-28", "no separator space is allowed");
    assert.equal(userDayStamp("[2026-11-28]"), "2026-11-28", "the bare head still parses");
    assert.equal(userDayStamp("[2024-02-29] leap day"), "2024-02-29");
  });

  it("a head the calendar does not have is not a stamp", () => {
    assert.equal(userDayStamp("[2026-02-30] impossible"), null);
    assert.equal(userDayStamp("[2026-13-01] impossible"), null);
    assert.equal(userDayStamp("[2026-00-10] impossible"), null);
    assert.equal(userDayStamp("[2023-02-29] not a leap year"), null);
    assert.equal(userDayStamp("[2026-1-2] unpadded"), null);
  });

  it("a bracket that is not at the head is prose", () => {
    assert.equal(userDayStamp("see [2026-11-28] here"), null);
    assert.equal(userDayStamp("[2026-11-28 missing the closer"), null);
    assert.equal(userDayStamp("[note] [2026-11-28] two heads"), null);
    assert.equal(userDayStamp(""), null);
  });

  it("the bubble text is the content after the head, minus one separator space", () => {
    assert.equal(stripDayStamp("[2026-11-28] hello"), "hello");
    assert.equal(stripDayStamp("[2026-11-28]hello"), "hello");
    assert.equal(stripDayStamp("[2026-11-28] /greet hi"), "/greet hi");
    assert.equal(stripDayStamp("[2026-11-28]  two spaces"), " two spaces", "exactly one separator goes");
    assert.equal(stripDayStamp("[2026-11-28]"), "");
    assert.equal(stripDayStamp("[2026-11-28] @docs/readme.md"), "@docs/readme.md");
  });

  it("an unstamped content comes back byte-identical", () => {
    for (const text of ["", "hello", "[2026-02-30] x", "[note] hi", "  padded  ", "/greet hi"]) {
      assert.equal(stripDayStamp(text), text);
    }
  });

  it("stripping before the scan restores the leading-skill pill", () => {
    const segs = splitRefSegments(stripDayStamp("[2026-11-28] /greet hi"));
    assert.equal(segs[0]!.token?.kind, "skill");
    assert.equal(segs[0]!.token?.name, "greet");
    assert.equal(segs[0]!.token?.start, 0, "the skill token sits at text start");
    assert.deepEqual(
      segs.map((s) => (s.token ? "PILL:" + s.token.token : s.text)),
      ["PILL:/greet", " hi"],
    );
    // the stamp itself is what broke the anchor
    assert.deepEqual(splitRefSegments("[2026-11-28] /greet hi"), [
      { text: "[2026-11-28] /greet hi" },
    ]);
  });
});

describe("the wiring (ChatMessage.vue + ChatTab.vue + main.css)", () => {
  it("the bubble scans and copies the stripped text, never the raw content", () => {
    assert.ok(
      chatMessage.includes("stripDayStamp(props.content)"),
      "ChatMessage derives the user text through the mirror",
    );
    assert.ok(
      chatMessage.includes("splitRefSegments(userText.value)"),
      "the parts scan rides the stripped text",
    );
    assert.ok(
      !chatMessage.includes("splitRefSegments(props.content)"),
      "the stamp must never reach the parts scan",
    );
    assert.ok(
      chatMessage.includes("copyText(userText.value)"),
      "the clipboard rides the stripped text",
    );
    assert.ok(
      !chatMessage.includes("copyText(props.content)"),
      "the stamp must never reach the clipboard",
    );
  });

  it("the assistant markdown path still reads the raw content", () => {
    assert.match(chatMessage, /renderMarkdown\(content\)/);
    assert.match(chatMessage, /\[props\.content, props\.streaming\]/);
  });

  it("both transcript branches render the divider above the stamped row", () => {
    assert.equal(
      (chatTab.match(/class="day-divider"/g) ?? []).length,
      2,
      "one divider in stacked, one in flat",
    );
    assert.ok(chatTab.includes("userDayStamp"), "the divider reads the stamp mirror");
    assert.match(
      chatTab,
      /entry\.kind !== "message" \|\| entry\.role !== "user"/,
      "a tool run and an assistant row never open a day",
    );
    assert.ok(chatTab.includes("blockDay(block)"), "stacked mode anchors on the block's head");
  });

  it("the divider rule is token-only (chatcolumn.test.ts rule() convention)", () => {
    const escaped = "\\.day-divider";
    const m = css.match(new RegExp(`^${escaped}\\s*\\{([^}]*)\\}`, "m"));
    assert.ok(m, ".day-divider is missing from main.css");
    const body = m![1];
    assert.match(body, /display:\s*flex/);
    assert.match(body, /font-family:\s*var\(--mono\)/);
    assert.match(body, /font-size:\s*12px/);
    assert.match(body, /color:\s*var\(--text-dim\)/);
    assert.ok(!/#[0-9a-fA-F]{3,8}\b/.test(body), "tokens only, no raw hex");
    assert.match(
      css,
      /\.day-divider::before,\s*\n\.day-divider::after\s*\{[^}]*border-top:\s*1px solid var\(--stroke\)/,
      "both hairlines ride the stroke token",
    );
  });
});
