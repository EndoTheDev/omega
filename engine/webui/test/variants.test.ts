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
  variantAnchors,
  variantLabel,
  variantOf,
  SessionBusyError,
  type StreamHandlers,
  type TranscriptDetail,
  type TranscriptItem,
  type VariantBlock,
} from "../app/composables/omega.ts";

import {
  createChatHost,
  createCanvasTranscript,
  findMessageItem,
  type ChatHostDeps,
} from "../app/composables/chat.ts";

const here = dirname(fileURLToPath(import.meta.url));
const chatMessage = readFileSync(
  join(here, "../app/components/chat/ChatMessage.vue"),
  "utf8",
);
const variantSwitcher = readFileSync(
  join(here, "../app/components/chat/VariantSwitcher.vue"),
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

// --- variantAnchors (t_0df4efd8): WHERE the switcher renders. A
// multi-row replay's branch siblings are run HEAD row ids, and the
// active head can be a THINKING row or a TOOL CALL row - a plain
// message-only lookup renders nothing.

// the live-probed shape (t_078a2c35 verify, run-25Oct191736): the
// retry replayed thinking + tool_call + tool_result + text - the
// heads (46, 47) are both THINKING rows, the fresh answer text (49)
// is not a sibling
const MULTIBRANCH: VariantBlock = {
  message_id: "45",
  siblings: ["46", "47"],
  count: 2,
  active: 1,
};
const multiRunItems: TranscriptItem[] = [
  { kind: "message", role: "user", content: "q", id: "45" },
  { kind: "thinking", content: "hm", id: "47" } as TranscriptItem,
  // the tool_result row (48) merged onto the call item - the id stays
  // the CALL row's
  { kind: "tool", name: "grep", status: "done", result: "x", id: "33" } as TranscriptItem,
  { kind: "message", role: "assistant", content: "answer", id: "49" },
];

describe("variantAnchors: the switcher survives multi-row replays", () => {
  it("a thinking/tool head's run anchors its last stored text bubble", () => {
    const anchors = variantAnchors(multiRunItems, [MULTIBRANCH]);
    assert.equal(anchors.size, 1);
    assert.equal(anchors.get("49"), MULTIBRANCH);
    // the head rows themselves do NOT render chips
    assert.equal(anchors.get("47"), undefined);
    assert.equal(anchors.get("33"), undefined);
  });

  it("a text-less run anchors the head block itself", () => {
    const items: TranscriptItem[] = [
      { kind: "message", role: "user", content: "q", id: "45" },
      { kind: "thinking", content: "hm", id: "47" } as TranscriptItem,
    ];
    const anchors = variantAnchors(items, [MULTIBRANCH]);
    assert.equal(anchors.get("47"), MULTIBRANCH);
    // and the head as a tool line (no thinking, no text)
    const toolOnly: TranscriptItem[] = [
      { kind: "message", role: "user", content: "q", id: "45" },
      { kind: "tool", name: "grep", status: "done", result: "x", id: "47" } as TranscriptItem,
    ];
    assert.equal(variantAnchors(toolOnly, [MULTIBRANCH]).get("47"), MULTIBRANCH);
  });

  it("a plain one-row reply anchors the head bubble (the classic case)", () => {
    const items: TranscriptItem[] = [
      { kind: "message", role: "user", content: "q", id: "45" },
      { kind: "message", role: "assistant", content: "v2", id: "47" },
    ];
    const anchors = variantAnchors(items, [MULTIBRANCH]);
    assert.equal(anchors.get("47"), MULTIBRANCH);
    assert.equal(anchors.size, 1);
  });

  it("one anchor per branch: the tool-call head does not double-anchor", () => {
    // head = the tool call row itself, run carries a text row: the
    // text bubble wins, the tool line stays chipless
    const items: TranscriptItem[] = [
      { kind: "message", role: "user", content: "q", id: "45" },
      { kind: "tool", name: "grep", status: "done", result: "x", id: "47" } as TranscriptItem,
      { kind: "message", role: "assistant", content: "answer", id: "49" },
    ];
    const anchors = variantAnchors(items, [MULTIBRANCH]);
    assert.deepEqual(
      [...anchors.keys()].sort(),
      ["49"],
      JSON.stringify([...anchors.entries()]),
    );
  });

  it("live items (no id) never anchor; orphan results carry no id", () => {
    const items: TranscriptItem[] = [
      { kind: "message", role: "user", content: "q" }, // live user row
      { kind: "thinking", content: "hm" } as TranscriptItem, // live thinking
      { kind: "tool", name: "tool", status: "done", result: "orphan", id: "48" } as TranscriptItem,
    ];
    assert.equal(variantAnchors(items, [MULTIBRANCH]).size, 0);
  });

  it("single-run branch points and absent variants anchor nothing", () => {
    const one: VariantBlock = { message_id: "45", siblings: ["47"], count: 1, active: 0 };
    assert.equal(variantAnchors(multiRunItems, [one]).size, 0);
    assert.equal(variantAnchors(multiRunItems, undefined).size, 0);
    assert.equal(variantAnchors(multiRunItems, []).size, 0);
  });
});

describe("canvas fold: stored ids ride the thinking/tool rows", () => {
  it("a multi-row replay's rows carry their ids (head anchorable)", () => {
    const t = createCanvasTranscript();
    const rows = [
      { id: "45", role: "user", status: "complete", blocks: [{ type: "text", content: "q" }] },
      { id: "47", role: "assistant", status: "complete", blocks: [{ type: "thinking", content: "hm" }] },
      {
        id: "33",
        role: "assistant",
        status: "complete",
        blocks: [{ type: "tool_call", tool_call: { id: "c1", name: "grep", arguments: {} } }],
      },
      {
        id: "48",
        role: "tool",
        status: "complete",
        blocks: [{ type: "tool_result", content: "x", tool_call_id: "c1" }],
      },
      { id: "49", role: "assistant", status: "complete", blocks: [{ type: "text", content: "answer" }] },
    ] as never;
    const items = t.apply({ session: "s1", rows });
    const ids = items.map((it) => (it as { id?: string }).id);
    assert.deepEqual(ids, ["45", "47", "33", "49"], JSON.stringify(ids));
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
    // t_0df4efd8: the chip markup is ONE component (VariantSwitcher) -
    // the bubble's action row mounts it and forwards the sibling index
    assert.match(chatMessage, /<VariantSwitcher/);
    assert.match(chatMessage, /v-if="variant"/);
    assert.match(chatMessage, /@select="\$emit\('select', \$event\)"/);
    assert.match(variantSwitcher, /class="variant-switcher"/);
    assert.match(variantSwitcher, /\{\{ vLabel\.index \+ 1 \}\}\/\{\{ vLabel\.count \}\}/);
    assert.match(variantSwitcher, /emit\('select', vLabel\.index - 1\)/);
    assert.match(variantSwitcher, /emit\('select', vLabel\.index \+ 1\)/);
  });
  it("arrows disable at the range edges", () => {
    assert.match(variantSwitcher, /:disabled="vLabel\.index <= 0"/);
    assert.match(variantSwitcher, /:disabled="vLabel\.index >= vLabel\.count - 1"/);
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
  it("head-row anchors ride ThinkingBlock and ToolCallLine too (t_0df4efd8)", () => {
    assert.match(chatItem, /:variant="variant"\n    @select="\(i\) => variant && \$emit\('select', variant\.message_id, i\)"/);
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
      st.restored = renderTranscriptItems([
        { kind: "user", id: "1", content: "q" },
        { kind: "message", id: "5", content: "selected answer" },
      ]);
      const ok = await host.selectVariant("c1", "1", 0);
      assert.equal(ok, true);
      assert.deepEqual(posts, [{ message_id: "1", select: 0 }]);
      assert.equal(transcriptCalls, 1, "the select re-reads the branch truth");
      // the fallback render was REPLACED by the refetch's render (a
      // canvas-covered tab repaints through the snapshot refetch)
      assert.equal(st.restored[1]?.kind, "message");
      assert.equal((st.restored[1] as { content: string }).content, "selected answer");
      assert.deepEqual(st.variants, [branch]);
    } finally {
      globalThis.fetch = saved;
    }
  });

  it("a select repaints a canvas-covered tab through the snapshot refetch", async () => {
    let refreshes = 0;
    const branch: VariantBlock = { message_id: "1", siblings: ["2", "5"], count: 2, active: 1 };
    const saved = globalThis.fetch;
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      const u = String(url);
      if (u.endsWith("/variants"))
        return { ok: true, json: async () => ({ variants: [branch] }) } as unknown as Response;
      if (u.endsWith("/transcript"))
        return jsonDetail({
          session: {} as TranscriptDetail["session"],
          running: false,
          variants: [branch],
          items: [{ kind: "user", id: "1", content: "q" }],
        });
      return CLEAN_RUN;
    }) as typeof fetch;
    try {
      const host = createChatHost(
        deps({ canvasRefresh: () => { refreshes++; } }),
      );
      host.ensureState("c1", "s1");
      await host.selectVariant("c1", "1", 0);
      assert.equal(refreshes, 1, "the canvas snapshot refetch repainted the panel");
    } finally {
      globalThis.fetch = saved;
    }
  });
});

describe("host fork anchoring: setFork + the validated send", () => {
  it("an assistant target anchors the composer; the next send chains onto it", async () => {
    const posts: Record<string, unknown>[] = [];
    const saved = globalThis.fetch;
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      const u = String(url);
      if (u.endsWith("/chat")) {
        posts.push(JSON.parse(String(init?.body)));
        return CLEAN_RUN;
      }
      return CLEAN_RUN;
    }) as typeof fetch;
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s1");
      const st = host.states.get("c1")!;
      // the VIEW resolved the anchor against the rendered transcript
      host.setFork("c1", "2");
      assert.equal(st.forkFrom, "2", "the anchor is armed");
      const ok = await host.submit("c1", "continue here", undefined, undefined, st.forkFrom ?? undefined);
      assert.equal(ok, true);
      const body = posts[0]!;
      assert.equal(body.fork, "2", "the send chains onto the anchor");
      assert.equal((body.messages as unknown[]).length, 1, "the typed message rides");
      assert.equal(st.forkFrom, null, "the anchor consumed");
    } finally {
      globalThis.fetch = saved;
    }
  });

  it("clearFork drops the anchor without sending", () => {
    const host = createChatHost(deps());
    host.ensureState("c1", "s1");
    host.setFork("c1", "2");
    host.setFork("c1", null);
    assert.equal(host.states.get("c1")!.forkFrom, null);
  });
});

describe("host.replayFork: a USER target reruns that turn immediately", () => {
  it("POSTs {fork: user id} with NO messages (replay semantics)", async () => {
    const posts: Record<string, unknown>[] = [];
    const saved = globalThis.fetch;
    globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
      const u = String(url);
      if (u.endsWith("/chat")) {
        posts.push(JSON.parse(String(init?.body)));
        return CLEAN_RUN;
      }
      if (u.endsWith("/transcript"))
        return jsonDetail({
          session: {} as TranscriptDetail["session"],
          running: false,
          items: [],
        });
      return CLEAN_RUN;
    }) as typeof fetch;
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s1");
      await host.replayFork("c1", "1");
      assert.equal(posts[0]!.fork, "1");
      assert.deepEqual(posts[0]!.messages, []);
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
      bodies.push(JSON.parse(String(init?.body)) as Record<string, unknown>);
      return new Response("", { status: 409 });
    }) as typeof fetch;
    try {
      await assert.rejects(
        streamChat([{ role: "user", content: "c" }], "s1", silent, undefined, undefined, "7"),
        SessionBusyError,
      );
      assert.equal(bodies[0]!.fork, "7");
      assert.equal(bodies[0]!.session_id, "s1");
    } finally {
      globalThis.fetch = saved;
    }
  });
});

describe("a run end refreshes the variants (switcher appears after retry)", () => {
  it("the finalize re-reads the branch block, the panel paints the rows", async () => {
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
      const ok = await host.submit("c1", "retry me");
      assert.equal(ok, true);
      assert.equal(transcriptCalls >= 1, true, "the run end refetched the branch truth");
      assert.deepEqual(host.states.get("c1")!.variants, [branch], "the branch block arrived");
      // the rendered ids come from the canvas rows now - variantOf
      // resolves them the same way once the panel paints the siblings
      const msg = { kind: "message", role: "assistant", id: "9" } as Parameters<typeof variantOf>[0];
      assert.deepEqual(variantOf(msg, host.states.get("c1")!.variants), branch);
    } finally {
      globalThis.fetch = saved;
    }
  });
});