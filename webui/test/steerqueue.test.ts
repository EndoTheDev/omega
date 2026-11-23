// omega webui: the steer queue's client law - the composer hint, the visible rows, and the
// 202 send path (a live turn takes the send instead of a second run).
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { dropQueuedRow, mergeQueuedRows, sendHint } from "../app/composables/chat.ts";
import { handlePayload, streamChat, type QueuedRow } from "../app/composables/omega.ts";

const here = dirname(fileURLToPath(import.meta.url));
const chatTab = readFileSync(join(here, "../app/components/ChatTab.vue"), "utf8");
const chatTs = readFileSync(join(here, "../app/composables/chat.ts"), "utf8");

const row = (id: string, content = id): QueuedRow => ({ id, content, at: "" });

describe("the composer's send hint (rule 5)", () => {
  it("names what a send will do, one line per state", () => {
    assert.equal(sendHint({ busy: true, sleeping: false, hasDraft: true }), "send steers the running turn");
    assert.equal(sendHint({ busy: false, sleeping: true, hasDraft: false }), "send wakes the agent now");
    assert.equal(sendHint({ busy: false, sleeping: false, hasDraft: true }), "send starts a turn");
  });

  it("stays clean when there is nothing to say", () => {
    assert.equal(sendHint({ busy: false, sleeping: false, hasDraft: false }), "");
  });

  it("a run outranks a pending call: steering is what a busy send does", () => {
    assert.equal(sendHint({ busy: true, sleeping: true, hasDraft: true }), "send steers the running turn");
  });
});

describe("the visible queue rows (rule 4)", () => {
  it("appends in order and never doubles an id", () => {
    const first = mergeQueuedRows([], [row("q1", "one"), row("q2", "two")]);
    assert.deepEqual(first.map((r) => r.content), ["one", "two"]);
    const again = mergeQueuedRows(first, [row("q2", "two"), row("q3", "three")]);
    assert.deepEqual(again.map((r) => r.content), ["one", "two", "three"]);
  });

  it("drops one row by id, and leaves an unknown id alone", () => {
    const rows = [row("q1"), row("q2")];
    assert.deepEqual(dropQueuedRow(rows, "q1").map((r) => r.id), ["q2"]);
    assert.equal(dropQueuedRow(rows, "nope"), rows);
    assert.equal(dropQueuedRow(rows, ""), rows);
  });
});

describe("delivery retires the row (rules 4 + 8)", () => {
  it("handlePayload reports a delivered queue row by id", () => {
    const seen: string[] = [];
    handlePayload(
      { type: "steer_delivered", id: "q7", message: { role: "user", content: "hi" } },
      { onSteerDelivered: (id) => seen.push(id) },
      {},
    );
    assert.deepEqual(seen, ["q7"]);
  });

  it("a steer_delivered frame carries no transcript decoration", () => {
    // the transcript row is the persisted user message; the frame only names the queue row
    let rowId = "";
    let painted = 0;
    handlePayload(
      { type: "steer_delivered", id: "q9", message: { role: "user", content: "hi" } },
      {
        onSteerDelivered: (id) => (rowId = id),
        onViewModelDelta: () => painted++,
      },
      {},
    );
    assert.equal(rowId, "q9");
    assert.equal(painted, 0, "the frame is a queue signal, not a transcript block");
  });
});

describe("a 202 send parks the row (rules 1-2, client side)", () => {
  it("streamChat answers onQueued instead of streaming", async () => {
    const realFetch = globalThis.fetch;
    globalThis.fetch = (async () => ({
      status: 202,
      ok: true,
      json: async () => ({ queued: [row("q1", "steer me")] }),
    })) as unknown as typeof fetch;
    try {
      const queued: QueuedRow[] = [];
      let streamed = 0;
      await streamChat(
        [{ role: "user", content: "steer me" }],
        "s1",
        {
          onQueued: (rows) => queued.push(...rows),
          onViewModelDelta: () => streamed++,
        },
        undefined,
        undefined,
        undefined,
        false,
        undefined,
      );
      assert.deepEqual(queued.map((r) => r.content), ["steer me"]);
      assert.equal(streamed, 0, "a queued send starts no run, so no delta arrives");
    } finally {
      globalThis.fetch = realFetch;
    }
  });
});

describe("source pins (the surfaces the node harness cannot mount)", () => {
  it("ChatTab renders the queue and the hint above the composer", () => {
    assert.match(chatTab, /class="steer-queue"/);
    assert.match(chatTab, /v-for="row in queued"/);
    assert.match(chatTab, /class="steer-del"/);
    assert.match(chatTab, /@click="onQueueDelete\(row\.id\)"/);
    assert.match(chatTab, /class="send-hint"/);
    // the queue block sits BEFORE the composer in the template (its last slot in the pane)
    assert.ok(chatTab.indexOf('class="steer-queue"') < chatTab.indexOf("<AppComposer"));
  });

  it("a busy session's send goes to the queue, never back into the composer", () => {
    assert.match(chatTs, /if \(st\.sessionId && busyAll\.value\.has\(st\.sessionId\)\) \{/);
    assert.match(chatTs, /sendSteer\(messages, st\.sessionId, model, thinking\)/);
    assert.match(chatTs, /st\.queued = mergeQueuedRows\(st\.queued, rows\)/);
  });

  it("a delivered steer is dropped from the queue, not decorated in the transcript", () => {
    assert.match(chatTs, /onSteerDelivered: \(id\) => \{\s*st\.queued = dropQueuedRow\(st\.queued, id\);/);
  });
});
