// omega webui: the morphing composer button + the retry action (t_357cbe9c
// + chat-last 2a). ONE button: busy -> stop, empty composer + exchange ->
// retry, else send (draft beats retry). The attach affordance lives in
// the input row's "+" now, and retry dispatches the chat panel's retry
// action through the action pipe (no /chat flag, no text resubmit).
// Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { createChatHost, type ChatHostDeps, composerFace } from "../app/composables/chat.ts";
import { SessionBusyError, streamChat, type StreamHandlers } from "../app/composables/omega.ts";

// a send-only wire probe: no handler needs names
const silent: StreamHandlers = {
  onUsage: () => {},
};

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");
const composer = readFileSync(
  join(here, "../app/components/chat/AppComposer.vue"),
  "utf8",
);
const modelSelect = readFileSync(
  join(here, "../app/components/chat/ModelSelect.vue"),
  "utf8",
);

describe("composerFace: the morphing button's three faces", () => {
  // canSend = draft.trim() || attachments.length>0 - encoded here as the
  // boolean the composer feeds in
  it("busy is stop - always, whatever the composer carries", () => {
    assert.equal(composerFace(true, true, false), "stop");
    assert.equal(composerFace(true, false, true), "stop");
    assert.equal(composerFace(true, false, false), "stop");
  });
  it("idle + typed draft means send even when an exchange exists", () => {
    assert.equal(composerFace(false, true, true), "send");
    assert.equal(composerFace(false, false, true), "send");
  });
  it("idle + empty composer + exchange exists means retry", () => {
    assert.equal(composerFace(false, true, false), "retry");
  });
  it("idle + empty composer + no exchange is a disabled send", () => {
    assert.equal(composerFace(false, false, false), "send");
  });
  it("nothing to send is not disabled in the retry face", () => {
    // the button's disabled binding disables ONLY on the send face
    const face = composerFace(false, true, false);
    assert.notEqual(face, "send");
  });
});

describe("retry rides the action pipe", () => {
  // chat.ts's retry() POSTs /ui/actions {module:"http_channel",
  // action:"retry", args:{session}} - the server replays the persisted
  // last user turn as a sibling variant. A fetch stub proves the wire
  // shape without a canvas store: the module qualification is the
  // chat panel's contract (card t_ec45b670).
  const saved = globalThis.fetch;
  afterEach(() => {
    globalThis.fetch = saved;
  });

  function deps(over: Partial<ChatHostDeps> = {}): ChatHostDeps {
    return {
      setTabSessionRef: () => {},
      refreshSessions: () => {},
      sessionRow: () => undefined,
      markRead: () => {},
      isFocused: () => false,
      ...over,
    };
  }

  it("the pipe retry POST carries module + action + the session arg", async () => {
    const calls: Array<{ url: string; body: Record<string, unknown> }> = [];
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      calls.push({
        url: String(url),
        body: JSON.parse(String(init?.body)) as Record<string, unknown>,
      });
      return { ok: true, json: async () => ({ result: { running: true } }) } as unknown as Response;
    }) as typeof fetch;
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    const ok = await host.retry("c1");
    assert.equal(ok, true);
    assert.equal(calls.length, 1);
    assert.equal(calls[0]!.url, "/ui/actions");
    assert.equal(calls[0]!.body.module, "http_channel");
    assert.equal(calls[0]!.body.action, "retry");
    assert.deepEqual(calls[0]!.body.args, { session: "s1" });
  });

  it("a failed pipe call surfaces in the status line, no transcript surgery", async () => {
    globalThis.fetch = (async () =>
      ({ ok: false, status: 500, text: async () => "session not found" }) as unknown as Response) as typeof fetch;
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    const ok = await host.retry("c1");
    assert.equal(ok, false);
    assert.equal(host.states.get("c1")!.restored.length, 0);
    assert.match(host.states.get("c1")!.statusLine, /retry failed/);
  });

  it("retry is refused client-side for unsaved and ephemeral tabs", async () => {
    globalThis.fetch = (async () => {
      throw new Error("no fetch may fire");
    }) as typeof fetch;
    const host = createChatHost(deps());
    host.ensureState("c1"); // unsaved
    assert.equal(await host.retry("c1"), false);
    host.ensureState("c2", "eph-x", true);
    assert.equal(await host.retry("c2"), false);
  });

  it("a normal send still carries its messages and never the retry flag", async () => {
    const bodies: Record<string, unknown>[] = [];
    globalThis.fetch = (async (_url: string, init?: RequestInit) => {
      bodies.push(JSON.parse(String(init?.body)) as Record<string, unknown>);
      return new Response("", { status: 409 });
    }) as typeof fetch;
    await assert.rejects(
      streamChat([{ role: "user", content: "hi" }], "s1", silent),
      SessionBusyError,
    );
    assert.deepEqual(bodies[0]!.messages, [{ role: "user", content: "hi" }]);
    assert.equal(bodies[0]!.retry, undefined, "the /chat retry flag is dead");
  });
});

describe("composer structure: one button, + in the input row", () => {
  it("the toolbar carries exactly ONE button - the morphing slot", () => {
    // the toolbar section lives between '<div class="toolbar">' and
    // '</div>' following it; count its <button elements (1 morphing one)
    const start = composer.indexOf('<div class="toolbar">');
    assert.ok(start > -1, "composer has a toolbar");
    const toolbar = composer.slice(start, composer.indexOf("</div>", start + 400));
    const buttons = toolbar.match(/<button/g) ?? [];
    assert.equal(buttons.length, 1, "toolbar has exactly one button");
    assert.match(toolbar, /:class="\{ cancel: face === 'stop'/, "the stop face rides the same element");
    assert.match(toolbar, /retry: face === 'retry'/, "the retry face rides the same element");
  });
  it("the paperclip toolbar button is dead", () => {
    assert.doesNotMatch(composer, /pick-btn/);
    assert.doesNotMatch(css, /\.pick-btn/);
  });
  it("the + attach affordance sits IN the input row", () => {
    assert.match(composer, /<div class="composer-row">/);
    assert.match(composer, /class="plus-btn"/);
    // plus and textarea are siblings in the same row
    const row = composer.slice(composer.indexOf('<div class="composer-row">'));
    const rowEnd = row.indexOf("</div>");
    assert.match(row.slice(0, rowEnd), /plus-btn/, "plus first");
    assert.match(row.slice(0, rowEnd), /<textarea/, "textarea in the row");
    assert.match(row.slice(0, rowEnd), /placeholder="message omega"/);
  });
});

describe("the squeeze: 320px floor keeps one button + two chips on a row", () => {
  // structural CSS pins: the narrow block shrinks the model chip with an
  // ellipsis instead of letting the button row wrap chaotically
  it("the narrow media block squeezes the model chip", () => {
    const narrow = css.slice(css.indexOf("@media (max-width: 768px)"));
    assert.match(narrow, /model-chip[^}]*min-width: 0/);
    assert.match(narrow, /chip-name[^}]*text-overflow: ellipsis/);
    assert.match(narrow, /chip-name[^}]*white-space: nowrap/);
  });
  it("the model chip's name sits in an ellipsis-able span", () => {
    assert.match(modelSelect, /class="chip-name"/);
  });
  it("the plus input row never contributes to the toolbar wrap", () => {
    assert.match(css, /\.composer-row\s*\{[^}]*display: flex/);
    assert.match(css, /\.plus-btn\s*\{[^}]*flex: none/);
  });
});