// omega webui: the terminal pane's keystroke writer.
//
// The pin is the ordering contract, not the transport: one POST in flight at a time,
// bytes leave in push order, printable runs coalesce under a window, and every control
// sequence flushes at once. The scheduler is injected, so the window is deterministic
// without a fake-timer dependency.
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { createInputWriter, strBytes } from "../../harness/terminal/ui/terminalInputWriter.ts";

interface Call {
  bytes: Uint8Array;
  text: string;
  settle: (value?: unknown) => void;
  fail: (err?: unknown) => void;
}

const textOf = (bytes: Uint8Array) => Array.from(bytes, (b) => String.fromCharCode(b)).join("");

const tick = () => new Promise<void>((r) => setTimeout(r, 0));

function rig(opts: { windowMs?: number; maxBurst?: number } = {}) {
  const calls: Call[] = [];
  const log: string[] = [];
  const timers = new Map<number, () => void>();
  let nextTimer = 1;
  let live = 0;
  let overlap = false;

  const writer = createInputWriter({
    ...opts,
    send: (bytes) => {
      live++;
      if (live > 1) overlap = true;
      const text = textOf(bytes);
      log.push(`start:${text}`);
      let done = false;
      return new Promise<unknown>((resolve, reject) => {
        const finish = (fn: () => void) => {
          if (done) return;
          done = true;
          live--;
          fn();
        };
        calls.push({
          bytes,
          text,
          settle: (value) => {
            log.push(`settle:${text}`);
            finish(() => resolve(value));
          },
          fail: (err) => {
            log.push(`fail:${text}`);
            finish(() => reject(err));
          },
        });
      });
    },
    schedule: (fn) => {
      const id = nextTimer++;
      timers.set(id, fn);
      return id;
    },
    cancel: (handle) => {
      timers.delete(handle as number);
    },
  });

  return {
    writer,
    calls,
    log,
    get overlap() {
      return overlap;
    },
    get timers() {
      return timers.size;
    },
    /** fire every armed window, oldest first */
    window() {
      const due = [...timers.values()];
      timers.clear();
      for (const fn of due) fn();
    },
    async settle(i: number, value?: unknown) {
      calls[i].settle(value);
      await tick();
    },
    async fail(i: number, err?: unknown) {
      calls[i].fail(err);
      await tick();
    },
    texts() {
      return calls.map((c) => c.text);
    },
  };
}

describe("createInputWriter: one POST per burst, ordered", () => {
  it("a printable run inside one window leaves as exactly one send", async () => {
    const r = rig();
    const run = "abcdefghij".repeat(6);
    assert.equal(run.length, 60);
    for (const ch of run) r.writer.push(ch);
    assert.equal(r.calls.length, 0, "nothing leaves before the window fires");
    assert.equal(r.timers, 1, "one window for the whole run");

    r.window();
    await tick();
    assert.equal(r.calls.length, 1, "one POST for sixty keys");
    assert.equal(r.calls[0].text, run, "bytes are the concatenation, in order");
    assert.deepEqual([...r.calls[0].bytes], [...strBytes(run)]);
    assert.equal(r.overlap, false);
  });

  it("a control key flushes at once, after the live send settles, never overlapping", async () => {
    const r = rig();
    r.writer.push("abc");
    r.window();
    await tick();
    assert.deepEqual(r.texts(), ["abc"], "the printable run is in flight");

    r.writer.push("\r");
    await tick();
    assert.equal(r.calls.length, 1, "single flight: Enter waits on the POST, not on a window");

    await r.settle(0);
    assert.deepEqual(r.texts(), ["abc", "\r"], "the control byte starts the moment the flight settles");
    assert.deepEqual(r.log, ["start:abc", "settle:abc", "start:\r"]);

    await r.settle(1);
    r.writer.push("def");
    assert.equal(r.timers, 1, "the following printable run coalesces under its own window");
    r.window();
    await tick();
    assert.deepEqual(r.texts(), ["abc", "\r", "def"]);
    await r.settle(2);

    assert.equal(r.overlap, false, "no two sends ever overlap");
  });

  it("a burst pushed during a live send goes out as one POST on settle", async () => {
    const r = rig();
    r.writer.push("a");
    r.window();
    await tick();
    assert.deepEqual(r.texts(), ["a"]);

    r.writer.push("b");
    r.writer.push("c");
    assert.equal(r.timers, 0, "no window is armed while a drain is live");
    assert.equal(r.calls.length, 1, "and nothing starts a second flight");

    await r.settle(0);
    assert.deepEqual(r.texts(), ["a", "bc"], "one POST, everything in push order");
    await r.settle(1);
    assert.equal(r.overlap, false);
  });

  it("a refused burst is dropped, never replayed, and the next burst still leaves", async () => {
    const r = rig();
    r.writer.push("abc");
    r.window();
    await tick();
    await r.fail(0, new Error("offline"));
    await tick();
    assert.equal(r.calls.length, 1, "a rejection is not retried");

    r.writer.push("def");
    r.window();
    await tick();
    assert.deepEqual(r.texts(), ["abc", "def"], "the writer carries on with later bytes");

    await r.settle(1, { ok: false });
    await tick();
    assert.equal(r.calls.length, 2, "a non-2xx answer is not retried either");

    r.writer.push("ghi");
    r.window();
    await tick();
    assert.deepEqual(r.texts(), ["abc", "def", "ghi"]);
    await r.settle(2);
  });

  it("a burst over the ceiling splits into chunks, in order, one flight each", async () => {
    const r = rig();
    const big = "a".repeat(100 * 1024);
    r.writer.push(big);
    assert.equal(r.timers, 1);
    r.window();
    await tick();

    const sizes: number[] = [];
    for (let i = 0; i < 4; i++) {
      assert.equal(r.calls.length, i + 1, "the next chunk waits for the live one");
      sizes.push(r.calls[i].bytes.length);
      await r.settle(i);
    }
    assert.deepEqual(sizes, [32768, 32768, 32768, 4096]);
    assert.equal(r.calls.map((c) => c.text).join(""), big, "the concatenation is the push, in order");
    assert.equal(r.overlap, false);
    assert.equal(r.calls.length, 4, "nothing further was queued");
  });

  it("reset drops pending bytes without a POST and ends the live drain", async () => {
    const r = rig();
    r.writer.push("abc");
    r.writer.reset();
    assert.equal(r.timers, 0, "the window is cancelled");
    r.window();
    await tick();
    assert.equal(r.calls.length, 0, "nothing left");

    r.writer.push("x");
    r.window();
    await tick();
    r.writer.push("yz"); // queued behind the live send
    r.writer.reset();
    await r.settle(0);
    assert.deepEqual(r.texts(), ["x"], "the dying session's bytes never ride a POST");
  });

  it("ships with a real timer and the 32KiB ceiling", async () => {
    const sent: string[] = [];
    const writer = createInputWriter({
      windowMs: 1,
      send: async (bytes) => {
        sent.push(textOf(bytes));
      },
    });
    const big = "z".repeat(32 * 1024 + 1);
    writer.push(big);
    await new Promise<void>((r) => setTimeout(r, 20));
    assert.equal(sent.length, 2, "one byte past the ceiling is a second POST");
    assert.equal(sent[0].length, 32 * 1024);
    assert.equal(sent[1], "z");
    assert.equal(sent.join(""), big);
  });
});
