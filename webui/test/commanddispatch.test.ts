// omega webui: the palette's dispatch contract.
// Two laws, both behavioural - the host is the real chat host with the
// wire stubbed:
// the invoking tab's session rides the dispatch body, so a
//     session-scoped command (/export, /search) acts on the tab
//     the operator typed in - and the shell verbs (/new /resume
//     /branch /label) never touch the wire at all;
// what the status line shows is the SERVER's answer - a refusal
//     arrives in its own words, and a table answer that opens with a
//     blank line still shows its header line instead of nothing.
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { createChatHost, type ChatHostDeps } from "../app/composables/chat.ts";

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

type Captured = { url: string; body: Record<string, unknown> | null };

function stubFetch(reply: () => Response): { calls: Captured[]; restore: () => void } {
  const saved = globalThis.fetch;
  const calls: Captured[] = [];
  globalThis.fetch = (async (input: unknown, init?: { body?: unknown }) => {
    calls.push({
      url: String(input),
      body: init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : null,
    });
    return reply();
  }) as typeof fetch;
  return { calls, restore: () => (globalThis.fetch = saved) };
}

const json = (body: unknown, status = 200): Response =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("palette dispatch: the tab's session rides the wire", () => {
  it("a bound tab sends its own session with the command", async () => {
    const wire = stubFetch(() =>
      json({ kind: "command", text: "[exported 0 messages to s-1.jsonl]" }),
    );
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s-1");
      await host.command("c1", "/export");
      assert.equal(wire.calls.length, 1, "one dispatch, no agent round trip");
      assert.equal(wire.calls[0]!.url, "/commands/dispatch");
      assert.deepEqual(
        wire.calls[0]!.body,
        { name: "/export", args: "", session_id: "s-1" },
        "the body names the command and the tab's session",
      );
      assert.match(
        host.states.get("c1")!.statusLine,
        /exported 0 messages to s-1\.jsonl/,
        "the answer is the server's own text",
      );
    } finally {
      wire.restore();
    }
  });

  it("an unsaved tab sends no session - the args keep their own shape", async () => {
    const wire = stubFetch(() => json({ kind: "command", text: "\n NAME MSGS\n" }));
    try {
      const host = createChatHost(deps());
      host.ensureState("c1"); // no session bound yet
      await host.command("c1", "/tree");
      assert.deepEqual(
        wire.calls[0]!.body,
        { name: "/tree", args: "" },
        "no session_id key at all when the tab has none",
      );
    } finally {
      wire.restore();
    }
  });

  it("args ride through beside the session", async () => {
    const wire = stubFetch(() => json({ kind: "command", text: "[search: needle]" }));
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s-1");
      await host.command("c1", "/search needle");
      assert.deepEqual(wire.calls[0]!.body, {
        name: "/search",
        args: "needle",
        session_id: "s-1",
      });
    } finally {
      wire.restore();
    }
  });

  // t_146d3133: /new /resume /branch /label are SHELL verbs - the pane is the shell's, so
  // the shell opens, branches and renames. Nothing about them belongs on this wire.
  it("the session verbs never reach the dispatch at all", async () => {
    const wire = stubFetch(() => json({ kind: "command", text: "should never be asked" }));
    const ran: string[] = [];
    try {
      const host = createChatHost(
        deps({
          runCommand: (_key, text) => {
            ran.push(text);
            return true;
          },
        }),
      );
      host.ensureState("c1", "s-1");
      for (const draft of ["/new", "/resume #1", "/branch", "/label mine"]) {
        await host.command("c1", draft);
      }
      assert.equal(wire.calls.length, 0, "no dispatch: the shell handled every one of them");
      assert.deepEqual(ran, ["/new", "/resume #1", "/branch", "/label mine"]);
    } finally {
      wire.restore();
    }
  });

  it("a refusal lands in the owner's words, never as an invented miss", async () => {
    const wire = stubFetch(() => new Response("command failed: no active session to export\n", { status: 500 }));
    try {
      const host = createChatHost(deps());
      host.ensureState("c1");
      await host.command("c1", "/export");
      const line = host.states.get("c1")!.statusLine;
      assert.match(line, /no active session to export/, "the server's refusal is what the operator reads");
      assert.doesNotMatch(line, /no handler/, "the client never manufactures a miss");
    } finally {
      wire.restore();
    }
  });

  it("a table answer opening with a blank line still shows its header", async () => {
    const wire = stubFetch(() =>
      json({ kind: "command", text: "\n  #    NAME      MSGS  SESSION ID\n  1    alpha        0  s-1\n" }),
    );
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s-1");
      await host.command("c1", "/tree");
      const line = host.states.get("c1")!.statusLine;
      assert.ok(line.length > 0, "a blank status line reads as nothing happened");
      assert.match(line, /#\s+NAME/, "the first line that carries text is the header");
    } finally {
      wire.restore();
    }
  });

  it("an answer with no text reads as no answer, never as a fabricated done", async () => {
    // /compact was the specimen: the wire answered no text at all and the status line
    // said "/compact: done", so a command that ran nothing looked like a success.
    const wire = stubFetch(() => json({ kind: "command" }));
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s-1");
      await host.command("c1", "/compact");
      const line = host.states.get("c1")!.statusLine;
      assert.doesNotMatch(line, /done/, "the client never invents a completion");
      assert.match(line, /no answer/, "an empty answer says that it was empty");
    } finally {
      wire.restore();
    }
  });

  it("a compact's own report is what the operator reads", async () => {
    const wire = stubFetch(() =>
      json({
        kind: "command",
        text: "folded 24 messages into a summary - the context now starts at message 137, and the last 10 stay verbatim",
      }),
    );
    try {
      const host = createChatHost(deps());
      host.ensureState("c1", "s-1");
      await host.command("c1", "/compact");
      assert.deepEqual(wire.calls[0]!.body, {
        name: "/compact",
        args: "",
        session_id: "s-1",
      });
      assert.match(host.states.get("c1")!.statusLine, /folded 24 messages .* message 137/);
    } finally {
      wire.restore();
    }
  });
});
