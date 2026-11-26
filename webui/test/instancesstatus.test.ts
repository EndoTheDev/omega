// omega webui: the connection list's state dots.
// The law these pin, one block each:
//   1. the STATE TABLE - the facts decide green/orange, an absent fact is unknown (never
//      degraded), and the wire's own answer class is honoured;
//   2. PRECEDENCE - a not-omega answer is red whatever the hatch flag says;
//   3. the flag SOFTENS only "nothing answered", and only on the row that carries it;
//   4. the wire read - one batch request, no-route detection on an old build's SPA shell;
//   5. the POLL LIFECYCLE - open reads now, tick reads again, close stops the timer, a
//      save reads once more, and a close during a read paints nothing.
// Run: node --test test/instancesstatus.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  STATUS_NO_ROUTE_NOTE,
  classifyInstance,
  fetchInstanceStatus,
  instanceReasons,
  instanceStates,
  startInstanceStatusPoll,
  stateFor,
  stateView,
  type InstanceState,
  type InstanceStatusReply,
  type InstanceStatusRow,
} from "../app/composables/instancesStatus.ts";
import type { SavedInstance } from "../app/composables/instances.ts";

const row = (patch: Partial<InstanceStatusRow>): InstanceStatusRow => ({
  url: "http://box:9000",
  state: "healthy",
  ...patch,
});

const facts = (provider: boolean | undefined, database: boolean | undefined): Record<string, unknown> => {
  const body: Record<string, unknown> = {};
  if (provider !== undefined) body.provider = { reachable: provider };
  if (database !== undefined) body.database = { healthy: database };
  return body;
};

describe("classifyInstance: the state table", () => {
  const cases: Array<[string, InstanceStatusRow, boolean, InstanceState]> = [
    ["both facts healthy", row({ facts: facts(true, true) }), false, "healthy"],
    ["provider not reachable", row({ facts: facts(false, true) }), false, "warning"],
    ["database unhealthy", row({ facts: facts(true, false) }), false, "warning"],
    ["both degraded", row({ facts: facts(false, false) }), false, "warning"],
    ["no facts carried at all (unsourced is unknown, not broken)", row({ facts: undefined }), false, "healthy"],
    ["provider absent, store healthy", row({ facts: facts(undefined, true) }), false, "healthy"],
    ["provider healthy, store absent", row({ facts: facts(true, undefined) }), false, "healthy"],
    ["provider degraded, store absent", row({ facts: facts(false, undefined) }), false, "warning"],
    ["an empty provider object is unsourced", row({ facts: { provider: {} } }), false, "healthy"],
    ["the wire said warning (its own read of the same body)", row({ state: "warning" }), false, "warning"],
    ["the wire said unverified", row({ state: "unverified" }), false, "unverified"],
    ["nothing answered, plain row", row({ state: "error", failure: "unreachable" }), false, "error"],
    ["nothing answered, hatched row", row({ state: "error", failure: "unreachable" }), true, "unverified"],
    ["the host guard refused, hatched row", row({ state: "error", failure: "guard" }), true, "unverified"],
    ["a syntax refusal, hatched row", row({ state: "error", failure: "syntax" }), true, "unverified"],
    ["not omega, plain row", row({ state: "error", failure: "not-omega" }), false, "error"],
    ["not omega, hatched row: the answer wins over the flag", row({ state: "error", failure: "not-omega" }), true, "error"],
    ["not omega over a healthy body still loses to its own failure", row({ state: "warning", failure: "not-omega" }), false, "error"],
  ];
  for (const [name, status, flagged, want] of cases) {
    it(name, () => {
      assert.equal(classifyInstance(status, flagged), want);
    });
  }
});

describe("stateView: one word + one class per state", () => {
  it("maps the four states", () => {
    assert.deepEqual(stateView("healthy"), { word: "healthy", cls: "ok" });
    assert.deepEqual(stateView("warning"), { word: "warning", cls: "warn" });
    assert.deepEqual(stateView("error"), { word: "error", cls: "down" });
    assert.deepEqual(stateView("unverified"), { word: "unverified", cls: "unverified" });
  });

  it("an unknown state reads as error, never as a guess", () => {
    assert.equal(stateView("mystery" as InstanceState).cls, "down");
  });
});

describe("instanceStates: the list's flags fold into the map", () => {
  const list: SavedInstance[] = [
    { name: "here", url: "http://here:9000" },
    { name: "hatch", url: "http://dead:9001", unverified: true },
    { name: "dead", url: "http://dead:9002" },
  ];
  const reply: InstanceStatusReply = {
    checked: true,
    note: "",
    rows: [
      row({ url: "http://here:9000", state: "healthy", facts: facts(true, true) }),
      row({ url: "http://dead:9001", state: "error", failure: "unreachable", reason: "unreachable at that address" }),
      row({ url: "http://dead:9002", state: "error", failure: "unreachable", reason: "unreachable at that address" }),
    ],
  };

  it("softens the hatched row only", () => {
    const states = instanceStates(list, reply);
    assert.equal(states.get("http://here:9000"), "healthy");
    assert.equal(states.get("http://dead:9001"), "unverified");
    assert.equal(states.get("http://dead:9002"), "error");
  });

  it("a trailing-slash spelling finds the same row", () => {
    const states = instanceStates(list, reply);
    assert.equal(stateFor(states, "http://here:9000/"), "healthy");
    assert.equal(stateFor(states, "http://elsewhere:1"), "");
  });

  it("an unchecked reply carries no states at all", () => {
    const states = instanceStates(list, { rows: [], checked: false, note: STATUS_NO_ROUTE_NOTE });
    assert.equal(states.size, 0);
  });

  it("the reasons ride the same address key", () => {
    const reasons = instanceReasons(reply);
    assert.match(reasons.get("http://dead:9002") ?? "", /unreachable/);
  });
});

describe("fetchInstanceStatus: one batch question", () => {
  it("asks urls= once per address, encoded", async () => {
    const seen: string[] = [];
    const reply = await fetchInstanceStatus(["http://a:1", "http://b:2/path"], async (target) => {
      seen.push(target);
      return new Response(JSON.stringify({ rows: [row({ url: "http://a:1" })] }), { status: 200 });
    });
    assert.deepEqual(seen, ["/instances/status?urls=http%3A%2F%2Fa%3A1&urls=http%3A%2F%2Fb%3A2%2Fpath"]);
    assert.equal(reply.checked, true);
    assert.equal(reply.rows.length, 1);
  });

  it("an old build's SPA shell is not an answer: no route, no dots", async () => {
    const reply = await fetchInstanceStatus(["http://a:1"], async () =>
      new Response("<!doctype html><html></html>", { status: 200 }),
    );
    assert.equal(reply.checked, false);
    assert.equal(reply.note, STATUS_NO_ROUTE_NOTE);
    assert.equal(reply.rows.length, 0);
  });

  it("a refusal is the same note: never a guessed colour", async () => {
    const reply = await fetchInstanceStatus(["http://a:1"], async () => new Response("too many", { status: 400 }));
    assert.equal(reply.checked, false);
    assert.match(reply.note, /400/);
  });

  it("a row with no address is dropped", async () => {
    const reply = await fetchInstanceStatus(["http://a:1"], async () =>
      new Response(JSON.stringify({ rows: [{ state: "healthy" }, { url: "http://a:1", state: "healthy" }] }), {
        status: 200,
      }),
    );
    assert.equal(reply.rows.length, 1);
  });
});

describe("startInstanceStatusPoll: open, tick, close, save", () => {
  const list: SavedInstance[] = [{ name: "a", url: "http://a:1" }];

  function harness(loadImpl?: (rows: readonly SavedInstance[]) => Promise<InstanceStatusReply>) {
    const loads: number[] = [];
    const replies: InstanceStatusReply[] = [];
    const timer: { fn: (() => void) | null } = { fn: null };
    const cleared: unknown[] = [];
    const poll = startInstanceStatusPoll({
      intervalMs: 30_000,
      load:
        loadImpl ??
        (async () => {
          loads.push(Date.now());
          return { rows: [row({ url: "http://a:1" })], checked: true, note: "" };
        }),
      onReply: (reply) => replies.push(reply),
      setInterval: (fn) => {
        timer.fn = fn;
        return 7;
      },
      clearInterval: (handle) => {
        cleared.push(handle);
        timer.fn = null;
      },
    });
    return { poll, loads, replies, cleared, fireTick: () => timer.fn?.() };
  }

  const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

  it("opening reads immediately, a tick reads again, closing stops the timer", async () => {
    const { poll, loads, replies, cleared, fireTick } = harness();
    assert.equal(poll.running, false);
    poll.start(list);
    assert.equal(poll.running, true);
    await flush();
    assert.equal(loads.length, 1, "open is one read now");
    assert.equal(replies.length, 1);

    fireTick();
    await flush();
    assert.equal(loads.length, 2, "one tick is one read");

    poll.stop();
    assert.equal(poll.running, false);
    assert.deepEqual(cleared, [7], "close clears the timer it armed");
    assert.equal(fireTick(), undefined, "no tick survives the close");
    await flush();
    assert.equal(loads.length, 2, "a closed poll reads nothing more");
  });

  it("a save reads once while the poll runs", async () => {
    const { poll, loads } = harness();
    poll.start(list);
    await flush();
    await poll.refresh(list);
    assert.equal(loads.length, 2, "the save's read is exactly one more");
    poll.stop();
  });

  it("a close during a read paints nothing", async () => {
    let release: (() => void) | null = null;
    const { poll, replies } = harness(
      () =>
        new Promise<InstanceStatusReply>((resolve) => {
          release = () => resolve({ rows: [], checked: true, note: "" });
        }),
    );
    poll.start(list);
    poll.stop();
    release?.();
    await flush();
    assert.equal(replies.length, 0, "a shut panel never repaints");
  });

  it("a second start does not arm a second timer", async () => {
    const { poll, loads, cleared } = harness();
    poll.start(list);
    poll.start(list);
    await flush();
    assert.equal(loads.length, 1);
    poll.stop();
    assert.equal(cleared.length, 1);
  });
});
