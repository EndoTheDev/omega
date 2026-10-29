// omega webui: the morphing composer button + the retry action (t_357cbe9c
// + chat-last 2a; retry face retired t_95d2bc34). ONE button: busy -> stop,
// idle -> send (dimmed at rest, armed by a draft or attachment). The RETRY
// affordance rendered in the message action row now - the newest assistant
// response's chip dispatches the same action pipe (no /chat flag, no text
// resubmit). Run: node --test engine/webui/test/ (node >= 23 runs TS natively)
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
const chatMessage = readFileSync(
  join(here, "../app/components/chat/ChatMessage.vue"),
  "utf8",
);
const chatItem = readFileSync(
  join(here, "../app/components/ChatItem.vue"),
  "utf8",
);
const chatTab = readFileSync(
  join(here, "../app/components/ChatTab.vue"),
  "utf8",
);
const modelSelect = readFileSync(
  join(here, "../app/components/chat/ModelSelect.vue"),
  "utf8",
);

describe("composerFace: the morphing button's two faces", () => {
  it("busy is stop - always", () => {
    assert.equal(composerFace(true), "stop");
  });
  it("idle is send - with or without an exchange in the transcript", () => {
    assert.equal(composerFace(false), "send");
  });
  it("the composer no longer renders a retry face", () => {
    // the retry affordance moved to the action row (t_95d2bc34): the
    // composer's face vocabulary carries no retry branch, the button
    // never wears the retry class, and no canRetry prop reaches it
    assert.doesNotMatch(composer, /face === ['"]retry/);
    assert.doesNotMatch(composer, /canRetry/);
    assert.doesNotMatch(composer, /class="\{ cancel: face === 'stop', retry/);
    assert.doesNotMatch(css, /\.send\.retry/);
  });
  it("idle + empty composer is the DIMMED send (disabled, not morphing)", () => {
    // the disabled binding disables ONLY on the send face - which at
    // idle + no draft (no attachments) is exactly where it lands
    assert.equal(composerFace(false), "send");
  });
});

describe("retry rides the action pipe", () => {
  // chat.ts's retry() POSTs /ui/actions {module:"http_channel",
  // action:"retry", args:{session}} - the server replays the persisted
  // last user turn as a sibling variant. A fetch stub proves the wire
  // shape without a canvas store: the module qualification is the
  // chat panel's contract (card t_ec45b670). The TRIGGER is the newest
  // assistant response's action-row chip now (card t_95d2bc34).
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

describe("the retry affordance lives in the message action row", () => {
  // the composer dropped the face; the chip took it (t_95d2bc34 ruling
  // 1): only the newest assistant response carries it, gated by the
  // transcript OWNER (ChatTab) - ChatMessage never knows the transcript
  it("the msg-retry chip renders beside fork/copy, inside the action row", () => {
    assert.match(chatMessage, /class="msg-retry"/);
    const actionsAt = chatMessage.indexOf('class="msg-actions"');
    const copyAt = chatMessage.indexOf('class="msg-copy"', actionsAt);
    const retryAt = chatMessage.indexOf('class="msg-retry"', actionsAt);
    assert.ok(retryAt > actionsAt, "the chip lives inside the action row");
    assert.ok(retryAt > copyAt, "the chip sits beside fork/copy");
    // the row is assistant-only and hidden while streaming - the chip
    // rides the row's own v-if, it renders nowhere else
    const otherAt = chatMessage.indexOf('class="msg-retry"', retryAt + 1);
    assert.equal(otherAt, -1, "one chip site, inside the row");
  });

  it("ChatMessage receives the newest+idle signal from the transcript owner", () => {
    // ChatMessage does not know the transcript: ChatItem carries the
    // retryable prop through, ChatTab derives it (idle + newest response)
    assert.match(chatItem, /:retryable="retryable && item\.role === 'assistant'"/);
    assert.match(chatTab, /retryableOfItem\(item\)/);
    // the signal: canRetry (idle gate) AND the item IS the newest
    // assistant response
    assert.match(chatTab, /item\.id === lastAssistantId\.value/);
    // the chip's click carries the SAME host.retry the composer called
    // - no new endpoint, no new plumbing
    assert.match(chatTab, /@retry="onRetry"/);
    assert.match(chatTab, /void host\.retry\(stateKey\)/);
  });
});

describe("composer structure: one button, + in the input row", () => {
  it("the toolbar carries exactly ONE button - the morphing slot", () => {
    // the toolbar section lives between '<div class="toolbar">' and the
    // template's closing tag; count its <button elements (1 morphing
    // one). The toolbar is the template's last element, and since
    // t_641c2870 it nests a .model-group div - slicing to the first
    // '</div>' would stop at the group's closer, BEFORE the send
    // button, so slice to the template end instead.
    const start = composer.indexOf('<div class="toolbar">');
    assert.ok(start > -1, "composer has a toolbar");
    const toolbar = composer.slice(start, composer.indexOf("</template>", start));
    const buttons = toolbar.match(/<button/g) ?? [];
    assert.equal(buttons.length, 1, "toolbar has exactly one button");
    assert.match(toolbar, /:class="\{ cancel: face === 'stop'/, "the stop face rides the same element");
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
  it("the + input row never contributes to the toolbar wrap", () => {
    assert.match(css, /\.composer-row\s*\{[^}]*display: flex/);
    assert.match(css, /\.plus-btn\s*\{[^}]*flex: none/);
  });
});