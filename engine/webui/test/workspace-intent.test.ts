// omega webui: unit tests for the draft workspace intent (card
// t_ea7e6a58). The wire half: streamChat carries {workspace} only on
// the bare create. The host half: the unbound state's workspaceIntent
// rides the FIRST send (consumed there), never an ephemeral one. The
// @ picker half: fetchMentionFiles roots at the workspace when one is
// set, keeps the process-CWD read when not.
// Run: node --test test/workspace-intent.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";

import {
  streamChat,
  fetchMentionFiles,
  type StreamHandlers,
} from "../app/composables/omega.ts";

import {
  createChatHost,
  type ChatHostDeps,
} from "../app/composables/chat.ts";

const silent: StreamHandlers = {
  onUsage: () => {},
};

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

let savedFetch: typeof globalThis.fetch;
afterEach(() => {
  globalThis.fetch = savedFetch;
});

type Call = [string, string, unknown?];
function recordFetch(respond: (url: string) => Response) {
  const calls: Call[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
    const u = String(url);
    calls.push([
      init?.method ?? "GET",
      u,
      init?.body ? JSON.parse(String(init.body)) : undefined,
    ]);
    return respond(u);
  }) as typeof globalThis.fetch;
  return calls;
}

const run: () => Response = () =>
  sseResponse([
    'event: session\ndata: {"id":"sess-w1"}\n\n',
    'event: agent_end\ndata: {"type":"agent_end"}\n\n',
  ]);

describe("streamChat: the workspace wire half", () => {
  it("the intent rides the bare create only", async () => {
    const calls = recordFetch(run);
    await streamChat([{ role: "user", content: "q" }], null, silent, undefined, undefined, undefined, undefined, "/tmp/proj");
    const body = calls[0]?.[2] as Record<string, unknown>;
    assert.equal(body.workspace, "/tmp/proj", "the folder rides the create");
    assert.equal("session_id" in body, false, "a bare create carries no session id");

    await streamChat([{ role: "user", content: "q" }], "sess-9", silent, undefined, undefined, undefined, undefined, "/tmp/proj");
    const body2 = calls[1]?.[2] as Record<string, unknown>;
    assert.equal(body2.session_id, "sess-9");
    assert.equal("workspace" in body2, false, "a bound send never carries the folder");

    await streamChat([{ role: "user", content: "q" }], null, silent);
    const body3 = calls[2]?.[2] as Record<string, unknown>;
    assert.equal("workspace" in body3, false, "no intent, no field");
  });
});

describe("fetchMentionFiles: the @ listing root", () => {
  it("a workspace lists /workspaces/files for THAT folder", async () => {
    const calls = recordFetch((u) =>
      ({ ok: true, json: async () => ({ path: "/tmp/proj", entries: [{ name: "note.txt", is_dir: false }] }) }) as unknown as Response);
    const payload = await fetchMentionFiles("/tmp/proj");
    assert.match(calls[0]![1], /^\/workspaces\/files\?path=/, "the workspace endpoint serves the listing");
    assert.ok(calls[0]![1].includes(encodeURIComponent("/tmp/proj")));
    assert.equal(payload.entries[0]?.name, "note.txt");
  });

  it("rootless keeps the /files/browse read", async () => {
    const calls = recordFetch((u) =>
      ({ ok: true, json: async () => ({ entries: [{ name: "cwd.txt", is_dir: false }] }) }) as unknown as Response);
    const payload = await fetchMentionFiles("");
    assert.equal(calls[0]![1], "/files/browse");
    assert.equal(payload.entries[0]?.name, "cwd.txt");
  });
});

describe("the chat host: draft intent state", () => {
  it("a fresh state carries no intent", () => {
    const host = createChatHost(deps());
    host.ensureState("w1");
    assert.equal(host.states.get("w1")!.workspaceIntent, "");
  });

  it("the first send consumes the intent, the row is born pinned", async () => {
    const calls = recordFetch(run);
    let rebound: string | null = null;
    const host = createChatHost(deps({ setTabSessionRef: (_k, id) => (rebound = id) }));
    const st = host.ensureState("w1");
    st.workspaceIntent = "/tmp/proj";
    await host.submit("w1", "hello");
    assert.equal(st.sessionId, "sess-w1", "the session frame binds");
    assert.equal(rebound, "sess-w1");
    const body = calls[0]?.[2] as Record<string, unknown>;
    assert.equal(body.workspace, "/tmp/proj", "the intent rode the create");
    assert.equal(st.workspaceIntent, "", "the send consumed the intent");

    // a follow-up send is bound today: the row is what the chip reads,
    // no workspace field anywhere on the wire
    calls.length = 0;
    await host.submit("w1", "again");
    const body2 = calls[0]?.[2] as Record<string, unknown>;
    assert.equal(body2.session_id, "sess-w1");
    assert.equal("workspace" in body2, false);
  });

  it("an ephemeral draft never applies the intent", async () => {
    const calls = recordFetch(run);
    const host = createChatHost(deps());
    const st = host.ensureState("w1", null, true);
    st.workspaceIntent = "/tmp/proj";
    await host.submit("w1", "hello");
    const body = calls[0]?.[2] as Record<string, unknown>;
    assert.equal(body.ephemeral, true);
    assert.equal("workspace" in body, false, "no row to pin - no field on the wire");
  });
});

// the seed half (card t_30137f67): a fresh UNBOUND, non-ephemeral
// state initializes its intent from the loaded server.default_workspace
// (the deps.defaultWorkspace getter fed by the app's /settings read).
// The chip renders the value identically to a user-picked one - the
// intent IS the picked value, no new state anywhere.
describe("the default workspace seed", () => {
  it("a fresh unbound draft is born with the configured default", () => {
    const host = createChatHost(deps({ defaultWorkspace: () => "/home/endo" }));
    const st = host.ensureState("w1");
    assert.equal(st.workspaceIntent, "/home/endo", "the chip carries the default");
  });

  it("the send consumes the seeded intent: the row is born pinned", async () => {
    const calls = recordFetch(run);
    const host = createChatHost(deps({ defaultWorkspace: () => "/home/endo" }));
    const st = host.ensureState("w1");
    await host.submit("w1", "hello");
    const body = calls[0]?.[2] as Record<string, unknown>;
    assert.equal(body.workspace, "/home/endo", "the seeded default rode the create");
    assert.equal(st.workspaceIntent, "", "the send consumed the intent");
  });

  it("clearing the chip pre-send sends unpinned, even with a default", async () => {
    const calls = recordFetch(run);
    const host = createChatHost(deps({ defaultWorkspace: () => "/home/endo" }));
    const st = host.ensureState("w1");
    // the composer's clear affordance: onWorkspaceChange("") on an
    // unbound tab (ChatTab) - the intent is only the current draft value
    st.workspaceIntent = "";
    await host.submit("w1", "hello");
    const body = calls[0]?.[2] as Record<string, unknown>;
    assert.equal("workspace" in body, false, "the deliberate clear beat the default");
  });

  it("a different picked value replaces the seed, and the seed never lands twice", () => {
    const host = createChatHost(deps({ defaultWorkspace: () => "/home/endo" }));
    const st = host.ensureState("w1");
    st.workspaceIntent = "/tmp/proj";
    host.seedDefaultWorkspace("/home/endo");
    assert.equal(st.workspaceIntent, "/tmp/proj", "the pick stands");
  });

  it("bound and ephemeral states never seed", () => {
    const host = createChatHost(deps({ defaultWorkspace: () => "/home/endo" }));
    assert.equal(
      host.ensureState("b1", "sess-9").workspaceIntent,
      "",
      "a bound tab reads the row, not a seed",
    );
    assert.equal(
      host.ensureState("e1", null, true).workspaceIntent,
      "",
      "an ephemeral mint has no row to pin",
    );
  });

  it("no default configured: the draft stays blank", () => {
    const host = createChatHost(deps({ defaultWorkspace: () => "" }));
    assert.equal(host.ensureState("w1").workspaceIntent, "");
  });

  it("the boot backfill seeds the pre-resolve blanks only", () => {
    // no dep at creation: the app's /settings read has not resolved -
    // every fresh state is a blank draft, exactly the pre-card world
    const host = createChatHost(deps());
    const draft = host.ensureState("d1");
    const bound = host.ensureState("b2", "sess-1");
    const eph = host.ensureState("e2", null, true);
    const picked = host.ensureState("p1");
    picked.workspaceIntent = "/tmp/picked";
    host.seedDefaultWorkspace("/home/endo");
    assert.equal(draft.workspaceIntent, "/home/endo", "the blank draft took the seed");
    assert.equal(bound.workspaceIntent, "", "");
    assert.equal(eph.workspaceIntent, "", "");
    assert.equal(picked.workspaceIntent, "/tmp/picked", "a picked value stands");
    // idempotent: a second boot-shaped call cannot reseed a consumed
    // value (a send cleared it) into anything
    draft.workspaceIntent = "";
    host.seedDefaultWorkspace("/home/endo");
    assert.equal(draft.workspaceIntent, "/home/endo", "still seeds blanks");
  });
});

function deps(over: Partial<ChatHostDeps> = {}): ChatHostDeps {
  return {
    setTabSessionRef: () => {},
    refreshSessions: () => {},
    sessionRow: () => undefined,
    markRead: () => {},
    isFocused: () => true,
    ...over,
  };
}