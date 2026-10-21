// omega webui: variant navigation (t_a62eeb43). The switcher is driven
// entirely by the server's variants block: variantOf/variantLabel do
// the index/total math, renderTranscriptItems carries the store ids,
// the switcher markup is conditional on the resolved block, the fork
// chip is historical-only, and the host POSTs the select endpoint then
// re-fetches (the active path IS the re-render). Run: node --test test/
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  renderTranscriptItems,
  streamChat,
  variantLabel,
  variantOf,
  type StreamHandlers,
  type TranscriptDetail,
  type VariantBlock,
} from "../app/composables/omega.ts";

import {
  createChatHost,
  findMessageItem,
  type ChatHostDeps,
} from "../app/composables/chat.ts";

const here = dirname(fileURLToPath(import.meta.url));
const chatMessage = readFileSync(
  join(here, "../app/components/chat/ChatMessage.vue"),
  "utf8",
);
const chatItem = readFileSync(
  join(here, "../app/components/ChatItem.vue"),
  "utf8",
);
const composer = readFileSync(
  join(here, "../app/components/chat/AppComposer.vue"),
  "utf8",
);
const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");

const silent: StreamHandlers = {
  onThinking: () => {},
  onResponse: () => {},
  onToolCall: () => {},
  onToolResult: () => {},
  onStatus: () => {},
  onUsage: () => {},
};

// three siblings of one branch point (U1 re-run twice): the fresh retry
// is active with no selector state - server default
const BRANCH: VariantBlock = {
  message_id: "3",
  siblings: ["4", "7", "9"],
  count: 3,
  active: 2,
};

describe("variantOf: the switcher renders only when siblings exist", () => {
  it("an assistant bubble listed as a sibling resolves its branch block", () => {
    const msg = { kind: "message", role: "assistant", content: "v3", id: "9" };
    assert.deepEqual(variantOf(msg, [BRANCH]), BRANCH);
  });
  it("a single-run branch point (count < 2) never shows a switcher", () => {
    const one: VariantBlock = { message_id: "3", siblings: ["4"], count: 1, active: 0 };
    const msg = { kind: "message", role: "assistant", content: "v1", id: "4" };
    assert.equal(variantOf(msg, [one]), null);
  });
  it("user bubbles and live-built bubbles (no id) resolve to null", () => {
    const user = { kind: "message", role: "user", content: "q", id: "5" };
    const live = { kind: "message", role: "assistant", content: "partial", streaming: true };
    assert.equal(variantOf(user, [BRANCH]), null);
    assert.equal(variantOf(live, [BRANCH]), null);
  });
  it("a session without variants (absent block / empty list) shows nothing", () => {
    const msg = { kind: "message", role: "assistant", content: "a", id: "4" };
    assert.equal(variantOf(msg, undefined), null);
    assert.equal(variantOf(msg, []), null);
  });
});

describe("variantLabel: the « 2/3 » index/total math", () => {
  it("the rendered bubble IS the active sibling - one-indexed active", () => {
    assert.deepEqual(variantLabel(BRANCH), { index: 2, count: 3 });
    assert.deepEqual(variantLabel(BRANCH, "9"), { index: 2, count: 3 });
  });
  it("this bubble's own id wins over the block's active index", () => {
    assert.deepEqual(variantLabel(BRANCH, "4"), { index: 0, count: 3 });
    assert.deepEqual(variantLabel(BRANCH, "7"), { index: 1, count: 3 });
  });
  it("an out-of-range active or stale id clamps into range", () => {
    assert.deepEqual(variantLabel({ ...BRANCH, active: 5 }), { index: 2, count: 3 });
    // a stale id not in siblings falls back to the clamped active
    assert.deepEqual(variantLabel(BRANCH, "999"), { index: 2, count: 3 });
  });
  it("degenerate count stays renderable", () => {
    assert.deepEqual(variantLabel({ message_id: "3", count: 0, active: 0 }), {
      index: 0,
      count: 1,
    });
  });
});

describe("renderTranscriptItems: store ids ride the restore", () => {
  it("user and assistant items carry their wire id; the live tail does not", () => {
    const items = renderTranscriptItems([
      { kind: "user", id: "3", content: "q" },
      { kind: "message", id: "4", content: "a", response_ms: 1200 },
      { kind: "message", content: "streaming tail", running: true },
    ]);
    assert.deepEqual(
      items.map((i) => ("id" in i ? i.id : undefined)),
      ["3", "4", undefined],
    );
    // roles and durations are untouched by the id pass-through
    assert.equal(items[0]?.kind, "message");
    const a1 = items[0] as { role: string };
    const a2 = items[1] as { role: string; finalMs?: number };
    assert.equal(a1.role, "user");
    assert.equal(a2.role, "assistant");
    assert.equal(a2.finalMs, 1200);
  });
});

describe("findMessageItem: the variants surface's address", () => {
  it("finds historical messages by id, newest first", () => {
    const found = findMessageItem(
      renderTranscriptItems([
        { kind: "user", id: "3", content: "q" },
        { kind: "message", id: "4", content: "a" },
      ]),
      "3",
    );
    assert.equal(found?.role, "user");
  });
  it("live-built items (no id) are never found - historical only", () => {
    assert.equal(
      findMessageItem(
        [{ kind: "message", role: "assistant", content: "live", streaming: true }],
        "4",
      ),
      null,
    );
  });
});

describe("ChatMessage markup: switcher + fork affordance", () => {
  it("the switcher mounts ONLY on a variant block, one-indexed, arrows emit", () => {
    assert.match(chatMessage, /v-if="variant"/);
    assert.match(chatMessage, /variant-switcher/);
    assert.match(chatMessage, /\{\{ vLabel\.index \+ 1 \}\}\/\{\{ vLabel\.count \}\}/);
    assert.match(chatMessage, /emit\('select', vLabel\.index - 1\)/);
    assert.match(chatMessage, /emit\('select', vLabel\.index \+ 1\)/);
  });
  it("arrows disable at the range edges", () => {
    assert.match(chatMessage, /:disabled="vLabel\.index <= 0"/);
    assert.match(chatMessage, /:disabled="vLabel\.index >= vLabel\.count - 1"/);
  });
  it("the fork button is historical-only (an id, not streaming)", () => {
    assert.match(chatMessage, /v-if="itemId && !streaming"/);
    assert.match(chatMessage, /class="msg-fork"/);
    assert.match(chatMessage, /emit\('fork'\)/);
  });
  it("the styles exist: hover-revealed fork chip, always-on switcher", () => {
    assert.match(css, /\.variant-switcher \{/);
    assert.match(css, /\.msg-fork \{/);
    assert.match(css, /\.bubble:hover \.msg-fork/);
    // the switcher is NOT hover-gated: only the fork chip fades
    assert.doesNotMatch(css, /\.bubble:hover \.variant-switcher/);
  });
});

describe("ChatItem + composer wiring", () => {
  it("ChatItem feeds the branch block to assistant items and re-emits", () => {
    assert.match(chatItem, /:variant="item\.role === 'assistant' \? variant : null"/);
    assert.match(chatItem, /emit\('select', variant\.message_id, i\)/);
    assert.match(chatItem, /emit\('fork', item\.id\)/);
  });
  it("the composer shows the fork chip with a drop control", () => {
    assert.match(composer, /class="fork-chip"/);
    assert.match(composer, /\$emit\('clear-fork'\)/);
    assert.match(css, /\.fork-chip \{/);
  });
});

// --- the host half: stubbed fetch routing by URL - /chat streams, the
// transcript GET serves a detail, the variants POST captures its body ---

let savedFetch: typeof globalThis.fetch;
afterEach(() => {
  globalThis.fetch = savedFetch;
});

function sseResponse(frames: string[]): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const f of frames) controller.enqueue(enc.encode(f));
      controller.close();
    },
  });
  return { ok: true, body } as unknown as Response;
}

function jsonDetail(detail: TranscriptDetail): Response {
  return { ok: true, json: async () => detail } as unknown as Response;
}

const CLEAN_RUN = sseResponse([
  'event: response_chunk\ndata: {"type":"response_chunk","content":"new answer"}\n\n',
  'event: agent_end\ndata: {"type":"agent_end"}\n\n',
]);

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

describe("host.selectVariant: POST then the path re-renders", () => {
  it("POSTs the branch id + sibling index, then refetches the ACTIVE path", async () => {
    const posts: Record<string, unknown>[] = [];
    let transcriptCalls = 0;
    const branch: VariantBlock = { message_id: "1", siblings: ["2", "5"], count: 2, active: 1 };
    // the refetched path renders the NEWLY selected sibling
    const detail: TranscriptDetail = {
      session: {} as TranscriptDetail["session"],
      running: false,
      variants: [branch],
      items: [
        { kind: "user", id: "1", content: "q" },
        { kind: "message", id: "5", content: "selected answer" },
      ],
    };
    const saved = globalThis.fetch;
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      const u = String(url);
      if (u.endsWith("/variants")) {
        posts.push(JSON.parse(String(init?.body)));
        return { ok: true, json: async () => ({ variants: [branch] }) } as unknown as Response;
      }
      if (u.endsWith("/transcript")) {
        transcriptCalls++;
        return jsonDetail(detail);
      }
      return CLEAN_RUN;
    }) as typeof fetch;
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s1");
      // seed the stale path (variant 2 active) before the flip
      const st = host.states.get("c1")!;
      st.transcript = renderTranscriptItems([
        { kind: "user", id: "1", content: "q" },
        { kind: "message", id: "5", content: "selected answer" },
      ]);
      const ok = await host.selectVariant("c1", "1", 0);
      assert.equal(ok, true);
      assert.deepEqual(posts, [{ message_id: "1", select: 0 }]);
      assert.equal(transcriptCalls, 1, "the select re-fetches the active path");
      // the transcript was REPLACED by the refetch's render
      assert.equal(st.transcript[1].kind, "message");
      assert.equal((st.transcript[1] as { content: string }).content, "selected answer");
      assert.deepEqual(st.variants, [branch]);
    } finally {
      globalThis.fetch = saved;
    }
  });
});

describe("host.fork: anchor the next send on an assistant target", () => {
  it("assistant target sets forkFrom; the next send POSTs /chat {fork}; the chip clears", async () => {
    const posts: Record<string, unknown>[] = [];
    const saved = globalThis.fetch;
    // route: transcript GET (serves the seeded ids through refetch),
    // variants POST, /chat streams
    const detail: TranscriptDetail = {
      session: {} as TranscriptDetail["session"],
      running: false,
      items: [
        { kind: "user", id: "1", content: "q" },
        { kind: "message", id: "2", content: "answer" },
      ],
    };
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      const u = String(url);
      if (u.endsWith("/chat")) {
        posts.push(JSON.parse(String(init?.body)));
        return CLEAN_RUN;
      }
      if (u.endsWith("/transcript")) return jsonDetail(detail);
      if (u.endsWith("/variants")) {
        return { ok: true, json: async () => ({ variants: [] }) } as unknown as Response;
      }
      return CLEAN_RUN;
    }) as typeof fetch;
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s1");
      const st = host.states.get("c1")!;
      st.transcript = renderTranscriptItems(detail.items);
      await host.fork("c1", "2");
      assert.equal(st.forkFrom, "2", "the anchor is armed");
      const ok = await host.submit("c1", "continue here");
      assert.equal(ok, true);
      const body = posts[0]!;
      assert.equal(body.fork, "2", "the send chains onto the anchor");
      assert.equal((body.messages as unknown[]).length, 1, "the typed message rides");
      assert.equal(body.retry, undefined);
      assert.equal(st.forkFrom, null, "the anchor cleared on send");
    } finally {
      globalThis.fetch = saved;
    }
  });
});

describe("host.fork: a USER target reruns that turn immediately", () => {
  it("POSTs {fork: user id} with NO messages (replay semantics)", async () => {
    const posts: Record<string, unknown>[] = [];
    const saved = globalThis.fetch;
    const detail: TranscriptDetail = {
      session: {} as TranscriptDetail["session"],
      running: false,
      items: [
        { kind: "user", id: "1", content: "q" },
        { kind: "message", id: "2", content: "answer" },
      ],
    };
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      const u = String(url);
      if (u.endsWith("/chat")) {
        posts.push(JSON.parse(String(init?.body)));
        return CLEAN_RUN;
      }
      if (u.endsWith("/transcript")) return jsonDetail(detail);
      return CLEAN_RUN;
    }) as typeof fetch;
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s1");
      const st = host.states.get("c1")!;
      st.transcript = renderTranscriptItems(detail.items);
      await host.fork("c1", "1");
      assert.equal(st.forkFrom, null, "a user target never anchors - it reruns");
      const body = posts[0]!;
      assert.equal(body.fork, "1");
      assert.deepEqual(body.messages, []);
    } finally {
      globalThis.fetch = saved;
    }
  });
});

describe("streamChat: the fork body", () => {
  it("a forked send carries fork + session_id + messages", async () => {
    const saved = globalThis.fetch;
    const bodies: Record<string, unknown>[] = [];
    globalThis.fetch = (async (_url: RequestInfo | URL, init?: RequestInit) => {
      bodies.push(JSON.parse(String(init?.body)));
      return new Response("", { status: 409 });
    }) as typeof fetch;
    try {
      await assert.rejects(
        streamChat([{ role: "user", content: "c" }], "s1", silent, undefined, undefined, false, "7"),
        { name: "SessionBusyError" },
      );
      assert.equal(bodies[0].fork, "7");
      assert.equal(bodies[0].session_id, "s1");
    } finally {
      globalThis.fetch = saved;
    }
  });
});

describe("a run end refreshes the variants (switcher appears after retry)", () => {
  it("the post-run refetch replaces the live view with ids + the branch block", async () => {
    const saved = globalThis.fetch;
    let transcriptCalls = 0;
    const branch: VariantBlock = { message_id: "1", siblings: ["2", "9"], count: 2, active: 1 };
    // the transcript GET serves the post-retry truth (the fresh sibling)
    const detail: TranscriptDetail = {
      session: {} as TranscriptDetail["session"],
      running: false,
      variants: [branch],
      items: [
        { kind: "user", id: "1", content: "q" },
        { kind: "message", id: "2", content: "first" },
        { kind: "message", id: "9", content: "new answer" },
      ],
    };
    globalThis.fetch = (async (url: RequestInfo | URL) => {
      if (String(url).endsWith("/transcript")) {
        transcriptCalls++;
        return jsonDetail(detail);
      }
      return CLEAN_RUN;
    }) as typeof fetch;
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s1");
      const st = host.states.get("c1")! as unknown as {
        transcript: { content: string }[];
        variants: VariantBlock[];
      };
      host.ensureState("c1", "s1");
      const ok = await host.submit("c1", "retry me");
      assert.equal(ok, true);
      assert.equal(transcriptCalls >= 1, true, "the run end refetched");
      assert.deepEqual(st.variants, [branch], "the branch block arrived");
      const ids = st.transcript.map((i) => (i as unknown as { id?: string }).id);
      assert.equal(ids.at(-1), "9", "the fresh sibling is on the path");
      // the switcher data + the rendered bubble agree
      const msg = { kind: "message", role: "assistant", id: "9" } as unknown as Parameters<
        typeof variantOf
      >[0];
      assert.deepEqual(variantOf(msg, st.variants), branch);
    } finally {
      globalThis.fetch = saved;
    }
  });
});