// omega webui: unit tests for the stacked chat appearance - the block
// split (splitBlocks) and the appearance knob parse. The block boundary
// matches the backend's segmented persistence: one user message starts
// a turn. Live-block binding is the host's busy slot (chat.ts), not a
// transcript-derived property, so it has no pure unit shape here. Run:
// node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  splitBlocks,
  parseAppearance,
  APPEARANCE_KEY,
  renderTranscriptItems,
} from "../app/composables/omega.ts";

import type { TranscriptItem } from "../app/composables/omega.ts";
import type { TranscriptWireItem } from "../app/composables/omega.ts";

// item builders keep the tests shape-honest: they are the same items the
// live stream and the restore path produce
const user = (content: string): TranscriptItem => ({
  kind: "message",
  role: "user",
  content,
});
const assistant = (content: string): TranscriptItem => ({
  kind: "message",
  role: "assistant",
  content,
});
const thinking = (content: string): TranscriptItem => ({
  kind: "thinking",
  content,
});
const tool = (name: string): TranscriptItem => ({
  kind: "tool",
  name,
  status: "done",
});
const interrupted = (): TranscriptItem => ({ kind: "interrupted" });

describe("splitBlocks", () => {
  it("one exchange = one block: user message opens, subtree belongs to it", () => {
    const items = [
      user("fix the build"),
      thinking("reading main.go"),
      tool("go_build"),
      assistant("fixed"),
    ];
    const blocks = splitBlocks(items);
    assert.equal(blocks.length, 1);
    assert.deepEqual(blocks[0], items);
  });

  it("cutting at every user message: each gets its own block", () => {
    const items = [
      user("first"),
      assistant("one"),
      user("second"),
      thinking("hm"),
      assistant("two"),
      user("third"),
      assistant("three"),
    ];
    const blocks = splitBlocks(items);
    assert.equal(blocks.length, 3);
    assert.deepEqual(blocks[0], [items[0], items[1]]);
    assert.deepEqual(blocks[1], [items[2], items[3], items[4]]);
    assert.deepEqual(blocks[2], [items[5], items[6]]);
  });

  it("leading non-user items form their own block - nothing dropped", () => {
    // an orphaned error bubble from a failed session create
    const items = [assistant("error: session create failed"), user("retry")];
    const blocks = splitBlocks(items);
    assert.equal(blocks.length, 2);
    assert.deepEqual(blocks[0], [items[0]]);
    assert.deepEqual(blocks[1], [items[1]]);
  });

  it("assistant-only tail after a full exchange stays in that block", () => {
    // live: chunks append to the CURRENT block, never open a new one
    const items = [user("hi"), assistant("hello"), assistant(" more")];
    const blocks = splitBlocks(items);
    assert.equal(blocks.length, 1);
    assert.deepEqual(blocks[0], items);
  });

  it("consecutive user messages each open a block", () => {
    const blocks = splitBlocks([user("a"), user("b"), user("c")]);
    assert.equal(blocks.length, 3);
    assert.deepEqual(blocks.map((b) => b.length), [1, 1, 1]);
  });

  it("interruption marker rides inside the block it interrupted", () => {
    const items = [user("a"), assistant("par"), interrupted()];
    const blocks = splitBlocks(items);
    assert.equal(blocks.length, 1);
    assert.deepEqual(blocks[0], items);
  });

  it("restored history and live converge on the same blocks", () => {
    // the wire payload of a two-exchange session, mapped the same way
    // restore maps it
    const wire: TranscriptWireItem[] = [
      { kind: "user", content: "one" },
      { kind: "message", content: "first answer" },
      { kind: "user", content: "two" },
      { kind: "message", content: "second answer" },
    ];
    const blocks = splitBlocks(renderTranscriptItems(wire));
    assert.equal(blocks.length, 2);
    assert.equal(blocks[1].length, 2);
  });

  it("empty transcript -> empty blocks", () => {
    assert.deepEqual(splitBlocks([]), []);
  });
});

describe("parseAppearance / APPEARANCE_KEY", () => {
  it("accepts the two known appearances", () => {
    assert.equal(parseAppearance("chat"), "chat");
    assert.equal(parseAppearance("stacked"), "stacked");
  });

  it("falls back to chat for null and garbage (older build, tampered)", () => {
    assert.equal(parseAppearance(null), "chat");
    assert.equal(parseAppearance(undefined), "chat");
    assert.equal(parseAppearance(""), "chat");
    assert.equal(parseAppearance("timeline"), "chat");
    assert.equal(parseAppearance(42), "chat");
  });

  it("persists under the omega.appearance storage key", () => {
    assert.equal(APPEARANCE_KEY, "omega.appearance");
  });
});