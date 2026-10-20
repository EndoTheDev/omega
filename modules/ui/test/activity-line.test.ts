// omega webui: unit tests for the activity line's phase derivation
// (card t_e9baac4b) - the line above the composer narrates the run
// from the transcript + the busy gate, purely: activityPhase() has no
// state, no clock, no host. The component adds only the ticking meter.
// Run: node --test modules/ui/test/ (node >= 23 runs TS)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { activityPhase } from "../app/composables/chat.ts";
import type { TranscriptItem } from "../app/composables/chat.ts";

// same builders the live stream and the restore path produce
const user = (content: string): TranscriptItem => ({
  kind: "message",
  role: "user",
  content,
});
const assistant = (content: string, streaming?: boolean): TranscriptItem => ({
  kind: "message",
  role: "assistant",
  content,
  ...(streaming ? { streaming: true } : {}),
});
const thinking = (content: string, streaming?: boolean): TranscriptItem => ({
  kind: "thinking",
  content,
  ...(streaming ? { streaming: true } : {}),
});
const tool = (
  name: string,
  status: string,
  extra: Partial<{ interrupted: boolean; elapsedMs: number }> = {},
): TranscriptItem => ({
  kind: "tool",
  name,
  status,
  ...extra,
});

describe("activityPhase (card t_e9baac4b)", () => {
  it("idle: null - the line is hidden entirely", () => {
    const items = [user("hi"), thinking("..."), assistant("hello")];
    assert.equal(activityPhase(items, false), null);
    // even a transcript with leftover running/interrupted items
    const dead = [user("hi"), tool("web.search", "running", { interrupted: true })];
    assert.equal(activityPhase(dead, false), null);
  });

  it("thinking deltas arriving: thinking", () => {
    const items = [user("hi"), thinking("let me look at ", true)];
    assert.deepEqual(activityPhase(items, true), { key: "thinking", label: "thinking" });
  });

  it("tool call running: 'running <name>'", () => {
    const items = [
      user("hi"),
      thinking("checking", true),
      tool("web.search", "running"),
    ];
    assert.deepEqual(activityPhase(items, true), {
      key: "tool:web.search",
      label: "running web.search",
      initialMs: undefined,
    });
  });

  it("newest open tool wins over older resolved calls", () => {
    const items = [
      user("hi"),
      tool("web.search", "done"),
      tool("shell", "done"),
      tool("files.read", "running"),
    ];
    const phase = activityPhase(items, true);
    assert.equal(phase?.key, "tool:files.read");
    assert.equal(phase?.label, "running files.read");
  });

  it("response streaming: responding", () => {
    const items = [
      user("hi"),
      thinking("done deliberating"),
      tool("web.search", "done"),
      assistant("the answer", true),
    ];
    assert.deepEqual(activityPhase(items, true), {
      key: "responding",
      label: "responding",
    });
  });

  it("streaming thinking after a resolved tool outranks nothing stale", () => {
    // post-tool thinking (the model re-deliberates before its next move)
    const items = [user("hi"), tool("web.search", "done"), thinking("so,", true)];
    assert.deepEqual(activityPhase(items, true), {
      key: "thinking",
      label: "thinking",
    });
  });

  it("interrupted tool call is never narrated as running", () => {
    const items = [user("hi"), tool("shell", "running", { interrupted: true })];
    // busy still true in the window before the transport death lands:
    // the phase falls back to the run's wait-on-model state
    assert.deepEqual(activityPhase(items, true), {
      key: "thinking",
      label: "thinking",
    });
  });

  it("interrupted open call does not outshout a streaming response", () => {
    const items = [
      user("hi"),
      tool("shell", "running", { interrupted: true }),
      assistant("partial", true),
    ];
    assert.deepEqual(activityPhase(items, true), {
      key: "responding",
      label: "responding",
    });
  });

  it("busy with nothing in the transcript yet: thinking (the send beat the first delta)", () => {
    assert.deepEqual(activityPhase([], true), { key: "thinking", label: "thinking" });
    assert.deepEqual(activityPhase([user("hi")], true), {
      key: "thinking",
      label: "thinking",
    });
  });

  it("busy between tool result and the model's next move: thinking", () => {
    const items = [user("hi"), tool("web.search", "done")];
    assert.deepEqual(activityPhase(items, true), {
      key: "thinking",
      label: "thinking",
    });
  });

  it("a restored STILL-RUNNING tool counts on from the server's elapsed-so-far", () => {
    const items = [user("hi"), tool("web.search", "running", { elapsedMs: 4200 })];
    assert.deepEqual(activityPhase(items, true), {
      key: "tool:web.search",
      label: "running web.search",
      initialMs: 4200,
    });
    // zero elapsedMs stays undefined - the meter starts its own clock
    const zero = [user("hi"), tool("web.search", "running", { elapsedMs: 0 })];
    assert.deepEqual(activityPhase(zero, true), {
      key: "tool:web.search",
      label: "running web.search",
      initialMs: undefined,
    });
  });

  it("a running tool outranks a simultaneously streaming response (restore merge race)", () => {
    const items = [
      user("hi"),
      assistant("preamble", true),
      tool("files.read", "running"),
    ];
    assert.equal(activityPhase(items, true)?.key, "tool:files.read");
  });

  it("unnamable tool call: narrated as 'running tool'", () => {
    const items = [user("hi"), tool("", "running")];
    assert.equal(activityPhase(items, true)?.label, "running tool");
  });
});