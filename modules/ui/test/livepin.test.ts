// omega webui: unit tests for the stacked live-run layout split (card
// t_c40164cf) - the pinned question header and the scrolling agent body
// both derive from the same transcript via liveRunSlice. Pure
// functions only: the branch rendering itself is presentation over
// these shapes. Run: node --test modules/ui/test/ (node >= 23 runs TS)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  liveRunSlice,
  splitBlocks,
  isAtBottom,
  pillVisible,
  scrollFlushTick,
} from "../app/composables/omega.ts";

import type { TranscriptItem } from "../app/composables/omega.ts";

// same builders the live stream and the restore path produce
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

describe("liveRunSlice", () => {
  it("older history + new turn: pin = the last user message, body = its run", () => {
    const items = [
      user("first question"),
      assistant("first answer"),
      user("fix the build"),
      thinking("reading main.go"),
      tool("go_build"),
      assistant("fixed"),
    ];
    const { user: pin, body } = liveRunSlice(items);
    assert.equal(pin, items[2]);
    assert.deepEqual(body, items.slice(3));
  });

  it("just sent, nothing streaming yet: pin set, body empty", () => {
    const items = [user("older"), assistant("old"), user("new question")];
    const { user: pin, body } = liveRunSlice(items);
    assert.equal(pin, items[2]);
    assert.deepEqual(body, []);
  });

  it("mid-stream: thinking/tool/message items all land in the body", () => {
    const items = [user("q"), thinking("hm"), tool("files_read"), assistant("partial")];
    const { user: pin, body } = liveRunSlice(items);
    assert.equal(pin, items[0]);
    assert.deepEqual(body, items.slice(1));
  });

  it("consecutive user messages: the LAST one pins, earlier ones ride in history", () => {
    const items = [user("a"), assistant("x"), user("b"), user("c")];
    const { user: pin, body } = liveRunSlice(items);
    assert.equal(pin, items[3]);
    assert.deepEqual(body, []);
  });

  it("no user message in the transcript -> no pin, no branch (settled render)", () => {
    assert.deepEqual(liveRunSlice([]), { user: null, body: [] });
    assert.deepEqual(liveRunSlice([assistant("reply")]), {
      user: null,
      body: [],
    });
  });

  it("thinking/tool items BEFORE the first user message never become the pin", () => {
    const items = [assistant("error bubble"), user("retry"), thinking("ok")];
    const { user: pin, body } = liveRunSlice(items);
    assert.equal(pin, items[1]);
    assert.deepEqual(body, items.slice(2));
  });

  it("agrees with splitBlocks: the live slice is the transcript's last block", () => {
    const items = [
      user("one"),
      assistant("first answer"),
      user("two"),
      thinking("hm"),
      assistant("second answer"),
    ];
    const { user: pin, body } = liveRunSlice(items);
    const lastBlock = splitBlocks(items).at(-1)!;
    assert.equal(lastBlock[0], pin);
    assert.deepEqual(lastBlock.slice(1), body);
  });
});

describe("live body tail-follow (long body)", () => {
  // the .live-body is its own scroll container with the same follow
  // math as .messages - a long run exceeds the viewport and the pill
  // / near-bottom gate decides following
  it("a run longer than the pane: pinned false, pill shows", () => {
    const scrollTop = 0;
    const clientHeight = 400;
    const scrollHeight = 4000;
    assert.equal(isAtBottom(scrollTop, clientHeight, scrollHeight), false);
    assert.equal(pillVisible(scrollTop, clientHeight, scrollHeight), true);
  });

  it("following the tail: pinned true, pill hidden", () => {
    const clientHeight = 400;
    const scrollHeight = 4000;
    const scrollTop = scrollHeight - clientHeight;
    assert.equal(isAtBottom(scrollTop, clientHeight, scrollHeight), true);
    assert.equal(pillVisible(scrollTop, clientHeight, scrollHeight), false);
  });

  it("a body that does not fill the pane counts as at bottom (nothing to follow)", () => {
    assert.equal(isAtBottom(0, 400, 300), true);
  });
});

describe("scrollFlushTick (hidden-document tail-follow, card t_467a0386)", () => {
  // the observed repro shape: a run streamed with the document hidden
  // (user alt-tabbed away) never followed the tail - rAF callbacks are
  // frozen for a hidden document's whole lifetime. The tick planner
  // picks the flush that works in each state.
  it("a visible document keeps the rAF flush", () => {
    assert.equal(scrollFlushTick(false), "raf");
  });

  it("a hidden document flushes on a timer instead (rAF is frozen there)", () => {
    assert.equal(scrollFlushTick(true), "timer");
  });
});