// omega webui: the retry UX rulings (t_19c100dd, Endo 2026-10-25).
// Ruling 1 - retry renders in place: a retry's payload REPLACES the
// response slot (the server cuts the old sibling's rows, card
// t_1c9793a8's payload lane), and the canvas transcript applies each
// payload as a full replacement - the old row is gone from the render
// the moment the retry's first delta lands, never stacked.
// Ruling 2 - the action row sits at the BOTTOM of the message:
// switcher + fork + copy in one row, hidden while streaming.
// Run: node --test test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { createCanvasTranscript } from "../app/composables/chat.ts";
import type { ChatPayloadWire } from "../app/composables/omega.ts";

const here = dirname(fileURLToPath(import.meta.url));
const chatMessage = readFileSync(
  join(here, "../app/components/chat/ChatMessage.vue"),
  "utf8",
);
const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");

function userRow(id: string, content: string): ChatPayloadWire["rows"] {
  return [{ id, role: "user", status: "complete", blocks: [{ type: "text", content }] }];
}
function assistantRow(
  id: string | undefined,
  content: string,
  streaming = false,
): ChatPayloadWire["rows"] {
  return [{
    ...(id ? { id } : {}),
    role: "assistant",
    status: streaming ? "streaming" : "complete",
    blocks: [{ type: "text", content }],
  }];
}

describe("ruling 1: a retry delta replaces the response slot in place", () => {
  it("the old row is gone from the render once the retry streams", () => {
    const t = createCanvasTranscript();
    // the settled exchange: user + the old answer
    const before = t.apply({
      session: "s1",
      rows: [...userRow("1", "say hi"), ...assistantRow("2", "first answer")],
    });
    assert.equal(before.length, 2);
    assert.equal((before[1] as { content: string }).content, "first answer");

    // the retry's mid-run payload: the server cut the old sibling's
    // rows - the fresh answer streams into the SAME slot
    const mid = t.apply({
      session: "s1",
      running: true,
      rows: [...userRow("1", "say hi"), ...assistantRow(undefined, "fres", true)],
    });
    // never stacked: ONE response slot, the fresh content in it
    assert.equal(mid.length, 2, `retry stacked rows: ${JSON.stringify(mid)}`);
    const fresh = mid[1] as { content: string; streaming?: boolean; role: string };
    assert.equal(fresh.role, "assistant");
    assert.equal(fresh.content, "fres");
    assert.equal(fresh.streaming, true, "the fresh row streams");
    // the old content is nowhere in the render
    for (const it of mid) {
      assert.notEqual((it as { content: string }).content, "first answer");
    }
  });

  it("a variant flip renders the sibling in the same slot", () => {
    const t = createCanvasTranscript();
    t.apply({
      session: "s1",
      rows: [...userRow("1", "say hi"), ...assistantRow("3", "second answer")],
    });
    // the switcher flips to variant 1: the snapshot refetch serves the
    // selected sibling alone - one slot, N variants
    const flipped = t.apply({
      session: "s1",
      rows: [...userRow("1", "say hi"), ...assistantRow("2", "first answer")],
    });
    assert.equal(flipped.length, 2);
    assert.equal((flipped[1] as { content: string }).content, "first answer");
  });

  it("the stream end settles the fresh row in the same slot", () => {
    const t = createCanvasTranscript();
    t.apply({
      session: "s1",
      rows: [...userRow("1", "say hi"), ...assistantRow("2", "first answer")],
    });
    t.apply({
      session: "s1",
      running: true,
      rows: [...userRow("1", "say hi"), ...assistantRow(undefined, "fres", true)],
    });
    const done = t.apply({
      session: "s1",
      rows: [
        ...userRow("1", "say hi"),
        ...assistantRow("5", "fresh answer"),
      ],
    });
    assert.equal(done.length, 2);
    const settled = done[1] as { content: string; streaming?: boolean; id?: string };
    assert.equal(settled.content, "fresh answer");
    assert.equal(settled.streaming, undefined);
    assert.equal(settled.id, "5");
  });
});

describe("ruling 2: the action row sits at the bottom of the message", () => {
  it("switcher, fork and copy render in ONE row, inside the bubble", () => {
    // the row exists and guards the three affordances in order
    assert.match(chatMessage, /class="msg-actions"/);
    const actionsAt = chatMessage.indexOf('class="msg-actions"');
    const switcherAt = chatMessage.indexOf('class="variant-switcher"', actionsAt);
    const forkAt = chatMessage.indexOf('class="msg-fork"', actionsAt);
    const copyAt = chatMessage.indexOf('class="msg-copy"', actionsAt);
    assert.ok(switcherAt > actionsAt, "switcher in the row");
    assert.ok(forkAt > switcherAt, "fork after the switcher, one row");
    assert.ok(copyAt > forkAt, "copy after the fork, one row");
    // the row closes before the bubble does: all three live inside it
    const bubbleClose = chatMessage.lastIndexOf("</template>");
    const rowClose = chatMessage.lastIndexOf("</div>", bubbleClose);
    assert.ok(copyAt < rowClose, "the affordances sit inside the action row");
    // exactly one row container - never scattered chips
    assert.equal((chatMessage.match(/class="msg-actions"/g) ?? []).length, 1);
  });

  it("the row hides while the message streams", () => {
    assert.match(chatMessage, /<div\s+v-if="!streaming"\s+class="msg-actions">/);
  });

  it("the row is the bubble's last child - under the content, not a top edge", () => {
    // source order: the row follows the streaming cursor span and
    // closes the bubble; nothing sits between the row and the bubble's
    // closing tag
    const bubble = chatMessage.slice(
      chatMessage.indexOf('class="bubble"'),
      chatMessage.indexOf("</div>", chatMessage.indexOf('class="msg-actions"')),
    );
    const cursorAt = bubble.indexOf('class="cursor"');
    const rowAt = bubble.indexOf('class="msg-actions"');
    assert.ok(cursorAt > -1 && rowAt > cursorAt, "the row renders under the content");
    // the retired top-edge placements are gone from the stylesheet
    assert.doesNotMatch(css, /\.variant-switcher\s*\{[^}]*position:\s*absolute/, "no top-straddle switcher");
    assert.doesNotMatch(css, /\.msg-fork\s*\{[^}]*position:\s*absolute/, "no top-edge fork chip");
    assert.doesNotMatch(css, /\.msg-copy\s*\{[^}]*position:\s*absolute/, "no top-edge copy chip");
    // the row itself is an aligned flex row at the bottom
    assert.match(css, /\.msg-actions\s*\{[^}]*display:\s*flex/);
    assert.match(css, /\.msg-actions\s*\{[^}]*margin-top:/, "the row sits below the content");
  });
});