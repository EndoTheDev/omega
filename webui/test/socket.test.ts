// omega webui: unit tests for the push channel client (card t_896cf5a8).
//
// The pure halves are tested directly (envelope guard, backoff ladder,
// topic dispatch, chip frame guard); the transport is driven against a
// fake WebSocket so the reconnect ladder and the subscribe frame are
// real behaviour, not source-string hopes. The last block pins the
// deletion receipt: nothing in the shell polls beside the socket.
// Run: node --test webui/test/ (node >= 23)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  SOCKET_BACKOFF_BASE_MS,
  SOCKET_BACKOFF_MAX_MS,
  SOCKET_TOPICS,
  backoffDelay,
  createSocketClient,
  dispatchFrame,
  normalizeChipFrame,
  parseEnvelope,
  socketURL,
  type SocketEnvelope,
  type SocketHandlers,
} from "../app/composables/socket.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "app");
const src = (p: string) => readFileSync(join(root, p), "utf8");

// --- parseEnvelope: the envelope guard ------------------------------------

test("a pushed frame parses: topic, session, event, seq, data", () => {
  const env = parseEnvelope(
    JSON.stringify({ topic: "chat", session: "s1", event: "response_chunk", seq: 42, data: { type: "response_chunk" } }),
  );
  assert.ok(env);
  assert.equal(env.topic, "chat");
  assert.equal(env.session, "s1");
  assert.equal(env.event, "response_chunk");
  assert.equal(env.seq, 42);
  assert.deepEqual(env.data, { type: "response_chunk" });
});

test("a malformed or typeless frame degrades to null - the loop never throws", () => {
  assert.equal(parseEnvelope("not json"), null);
  assert.equal(parseEnvelope(""), null);
  assert.equal(parseEnvelope("[]"), null);
  assert.equal(parseEnvelope(JSON.stringify({ session: "s1" })), null);
  assert.equal(parseEnvelope(null), null);
  assert.equal(parseEnvelope(42), null);
});

test("an already-parsed object envelope passes through (fake sockets hand objects)", () => {
  const env = parseEnvelope({ topic: "health", data: { status: "ok" } });
  assert.equal(env?.topic, "health");
});

// --- dispatchFrame: the typed topic dispatch ------------------------------

test("the five topics land on their typed handlers", () => {
  const seen: string[] = [];
  const handlers: SocketHandlers = {
    health: (h) => seen.push(`health:${h.status}`),
    runs: (s, a) => seen.push(`runs:${s}:${a}`),
    chips: (s, f) => seen.push(`chips:${s}:${f.call_id}`),
    canvas: (e, _p) => seen.push(`canvas:${e}`),
    chat: (s, e) => seen.push(`chat:${s}:${e}`),
  };
  dispatchFrame({ topic: "health", data: { status: "ok" } }, handlers);
  dispatchFrame({ topic: "runs", session: "s1", data: { active: true } }, handlers);
  dispatchFrame({ topic: "chips", session: "s1", data: { call_id: "c1", text: "x" } }, handlers);
  dispatchFrame({ topic: "canvas", data: { type: "view_model_delta", panel: { id: "chat" } } }, handlers);
  dispatchFrame({ topic: "chat", session: "s1", event: "tool_call", data: { type: "tool_call" } }, handlers);
  assert.deepEqual(seen, [
    "health:ok",
    "runs:s1:true",
    "chips:s1:c1",
    "canvas:view_model_delta",
    "chat:s1:tool_call",
  ]);
});

test("a canvas frame without an event name falls back to the payload's type", () => {
  let event = "";
  dispatchFrame(
    { topic: "canvas", data: { type: "view_model_delta", panel: {} } },
    { canvas: (e) => (event = e) },
  );
  assert.equal(event, "view_model_delta");
});

test("an unknown topic is ignored - the set is closed server-side", () => {
  let touched = 0;
  dispatchFrame(
    { topic: "board", data: {} },
    { health: () => (touched += 1), canvas: () => (touched += 1), chat: () => (touched += 1) },
  );
  assert.equal(touched, 0);
});

test("a runs frame without a session or a bad payload is dropped", () => {
  let calls = 0;
  dispatchFrame({ topic: "runs", data: { active: true } }, { runs: () => (calls += 1) });
  dispatchFrame({ topic: "runs", session: "s1", data: "nope" }, { runs: () => (calls += 1) });
  assert.equal(calls, 0);
});

// --- the backoff ladder ---------------------------------------------------

test("backoff doubles from 500ms and caps at 15s", () => {
  const mid = () => 0.5; // rand 0.5 = no jitter
  assert.equal(backoffDelay(0, mid), SOCKET_BACKOFF_BASE_MS);
  assert.equal(backoffDelay(1, mid), 1000);
  assert.equal(backoffDelay(2, mid), 2000);
  assert.equal(backoffDelay(3, mid), 4000);
  assert.equal(backoffDelay(4, mid), 8000);
  assert.equal(backoffDelay(5, mid), SOCKET_BACKOFF_MAX_MS);
  assert.equal(backoffDelay(60, mid), SOCKET_BACKOFF_MAX_MS);
});

test("jitter stays inside +/-20% and never collapses below 50ms", () => {
  assert.equal(backoffDelay(0, () => 0), 400); // 500 - 20%
  assert.equal(backoffDelay(0, () => 1), 600); // 500 + 20%
  assert.ok(backoffDelay(0, () => 0) >= 50);
});

test("a garbage attempt index starts the ladder at the base delay", () => {
  assert.equal(backoffDelay(Number.NaN, () => 0.5), SOCKET_BACKOFF_BASE_MS);
  assert.equal(backoffDelay(-3, () => 0.5), SOCKET_BACKOFF_BASE_MS);
});

// --- the chip frame guard -------------------------------------------------

test("a chip frame without a call id drops - nothing to attach to", () => {
  assert.equal(normalizeChipFrame({ tool: "shell.run", text: "x" }), null);
  assert.equal(normalizeChipFrame({ call_id: "", text: "x" }), null);
  assert.equal(normalizeChipFrame("nope"), null);
});

test("a chip frame keeps its streamed text and its flags", () => {
  const f = normalizeChipFrame({ call_id: "c1", tool: "shell.run", text: "line\n", truncated: true });
  assert.ok(f);
  assert.equal(f.call_id, "c1");
  assert.equal(f.tool, "shell.run");
  assert.equal(f.text, "line\n");
  assert.equal(f.truncated, true);
  assert.equal(f.fill, undefined, "a live frame is not a fill");
});

test("a fill frame carries its content", () => {
  const f = normalizeChipFrame({ call_id: "c1", fill: true, content: "done", elapsed_ms: 1200 });
  assert.equal(f?.fill, true);
  assert.equal(f?.content, "done");
  assert.equal(f?.elapsed_ms, 1200);
});

// --- socketURL ------------------------------------------------------------

test("the endpoint rides the page's own origin with the right scheme", () => {
  assert.equal(socketURL({ protocol: "http:", host: "127.0.0.1:8098" }), "ws://127.0.0.1:8098/ws");
  assert.equal(socketURL({ protocol: "https:", host: "omega.lan" }), "wss://omega.lan/ws");
  assert.equal(socketURL(undefined), "/ws");
});

// --- the client, against a fake WebSocket ---------------------------------

class FakeSocket {
  static made: FakeSocket[] = [];
  static OPEN = 1;
  readyState = 0;
  sent: string[] = [];
  closed = false;
  url: string;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(url: string) {
    this.url = url;
    FakeSocket.made.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.closed = true;
  }
  // test drivers
  open() {
    this.readyState = 1;
    this.onopen?.();
  }
  push(frame: unknown) {
    this.onmessage?.({ data: typeof frame === "string" ? frame : JSON.stringify(frame) });
  }
  drop() {
    this.readyState = 3;
    this.onclose?.();
  }
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

test("connect opens one connection, subscribes to the whole topic set, dispatches frames", async () => {
  FakeSocket.made = [];
  const chats: string[] = [];
  const client = createSocketClient(
    { chat: (s, e) => chats.push(`${s}:${e}`) },
    { factory: (u) => new FakeSocket(u) as unknown as WebSocket, url: "ws://lane/ws", rand: () => 0.5 },
  );
  client.connect();
  assert.equal(client.status.value, "connecting");
  assert.equal(FakeSocket.made.length, 1, "one connection per page load");
  const sock = FakeSocket.made[0]!;
  assert.equal(sock.url, "ws://lane/ws");
  sock.open();
  assert.equal(client.status.value, "open");
  assert.deepEqual(JSON.parse(sock.sent[0]!), { type: "subscribe", topics: [...SOCKET_TOPICS] });
  sock.push({ topic: "chat", session: "s1", event: "tool_call", data: { type: "tool_call" } });
  assert.deepEqual(chats, ["s1:tool_call"]);
  client.close();
  assert.equal(client.status.value, "closed");
});

test("a dropped socket reconnects on the backoff ladder and reports the reconnect", async () => {
  FakeSocket.made = [];
  const opens: boolean[] = [];
  const client = createSocketClient(
    { open: ({ reconnect }) => opens.push(reconnect) },
    { factory: (u) => new FakeSocket(u) as unknown as WebSocket, url: "ws://lane/ws", rand: () => 0.5 },
  );
  client.connect();
  FakeSocket.made[0]!.open();
  FakeSocket.made[0]!.drop();
  assert.equal(client.status.value, "reconnecting");
  await sleep(600); // attempt 0 = 500ms, no jitter
  assert.equal(FakeSocket.made.length, 2, "the ladder reconnects without user action");
  FakeSocket.made[1]!.open();
  assert.deepEqual(opens, [false, true], "the second open is a reconnect (the catch-up trigger)");
  client.close();
});

test("close stops the ladder - no socket is re-created after the page goes away", async () => {
  FakeSocket.made = [];
  const client = createSocketClient(
    {},
    { factory: (u) => new FakeSocket(u) as unknown as WebSocket, url: "ws://lane/ws", rand: () => 0.5 },
  );
  client.connect();
  FakeSocket.made[0]!.open();
  FakeSocket.made[0]!.drop();
  client.close();
  await sleep(700);
  assert.equal(FakeSocket.made.length, 1, "a closed client never reconnects");
  assert.equal(client.status.value, "closed");
});

test("subscribe replaces the set, drops unknown names, and keeps all on an empty list", () => {
  FakeSocket.made = [];
  const client = createSocketClient(
    {},
    { factory: (u) => new FakeSocket(u) as unknown as WebSocket, url: "ws://lane/ws", rand: () => 0.5 },
  );
  client.connect();
  const sock = FakeSocket.made[0]!;
  sock.open();
  client.subscribe(["chat", "chips", "board" as never]);
  assert.deepEqual(client.topics(), ["chat", "chips"]);
  assert.deepEqual(JSON.parse(sock.sent[1]!), { type: "subscribe", topics: ["chat", "chips"] });
  client.subscribe([]);
  assert.deepEqual(client.topics(), ["chat", "chips"], "an empty list keeps the current set");
  client.close();
});

test("an envelope the client cannot parse never breaks later frames", () => {
  FakeSocket.made = [];
  const chats: string[] = [];
  const client = createSocketClient(
    { chat: (s, e) => chats.push(`${s}:${e}`) },
    { factory: (u) => new FakeSocket(u) as unknown as WebSocket, url: "ws://lane/ws", rand: () => 0.5 },
  );
  client.connect();
  const sock = FakeSocket.made[0]!;
  sock.open();
  sock.push("{not json");
  sock.push("[]");
  sock.push({ topic: "chat", session: "s1", event: "agent_end", data: { type: "agent_end" } });
  assert.deepEqual(chats, ["s1:agent_end"]);
  client.close();
});

test("a quiet wire is cut and rebuilt - a half-open socket never fires close", async () => {
  FakeSocket.made = [];
  const closes: number[] = [];
  const client = createSocketClient(
    { close: () => closes.push(Date.now()) },
    {
      factory: (u) => new FakeSocket(u) as unknown as WebSocket,
      url: "ws://lane/ws",
      rand: () => 0.5,
      livenessMs: 120,
    },
  );
  client.connect();
  const sock = FakeSocket.made[0]!;
  sock.open();
  await sleep(350); // no frames for ~3x the budget
  assert.equal(sock.closed, true, "the quiet-wire check closed the dead socket");
  sock.drop(); // the close event the browser fires on a local close()
  assert.equal(closes.length, 1, "the close handler ran - the ladder takes over");
  assert.equal(client.status.value, "reconnecting");
  await sleep(600); // attempt 0 = 500ms
  assert.equal(FakeSocket.made.length, 2, "a fresh connection replaces the quiet one");
  client.close();
});

test("frames keep the wire alive - a chatty socket is never cut", async () => {
  FakeSocket.made = [];
  const closes: number[] = [];
  const client = createSocketClient(
    { close: () => closes.push(Date.now()) },
    {
      factory: (u) => new FakeSocket(u) as unknown as WebSocket,
      url: "ws://lane/ws",
      rand: () => 0.5,
      livenessMs: 150,
    },
  );
  client.connect();
  const sock = FakeSocket.made[0]!;
  sock.open();
  for (let i = 0; i < 6; i++) {
    await sleep(80);
    sock.push({ topic: "health", data: { status: "ok" } });
  }
  assert.equal(closes.length, 0, "the 5s health beat would never trip the check");
  client.close();
});

// --- the receipts: nothing polls beside the socket ------------------------

test("the shell's polling timers are gone - the two survivors never fetch", () => {
  // the five deleted fetchers (health 30s, sessions 2s, canvas 15s,
  // notifications+verdicts 15s, gate cards 5s) must not come back
  for (const file of [
    "app.vue",
    "components/ChatTab.vue",
    "components/shell/notificationFeed.ts",
    "composables/chat.ts",
  ]) {
    const text = src(file);
    assert.ok(!text.includes("HEALTH_POLL_MS"), `${file}: the 30s health poll is gone`);
    assert.ok(!text.includes("RUNNING_POLL_MS"), `${file}: the 2s running poll is gone`);
    assert.ok(!text.includes("CANVAS_POLL_MS"), `${file}: the 15s canvas poll is gone`);
    assert.ok(!text.includes("gatePoll"), `${file}: the 5s gate poll is gone`);
    assert.ok(!text.includes("pollTimer"), `${file}: the 15s feed poll is gone`);
  }
  // exactly two setInterval users survive in the shell, and neither
  // fetches: the tab-title spinner and the verdict countdown
  const app = src("app.vue");
  const verdict = src("components/chat/VerdictCard.vue");
  const hits = [app, verdict].filter((t) => t.includes("setInterval")).length;
  assert.equal(hits, 2, "only the spinner and the countdown own a repeat interval");
  assert.ok(app.includes("TITLE_FRAMES"), "the spinner is the title animation");
  assert.ok(verdict.includes("ticker"), "the countdown is a clock, not a fetch");
});

test("the app wires every topic and catches up on reconnect only", () => {
  const app = src("app.vue");
  for (const topic of ["health:", "runs:", "canvas:", "chips:", "chat:"]) {
    assert.ok(app.includes(topic), `the socket handler set carries ${topic}`);
  }
  assert.ok(app.includes("createSocketClient"), "the client is the app's one connection");
  assert.ok(app.includes("if (!reconnect) return"), "the catch-up is the reconnect path, not the cold boot");
  assert.ok(app.includes("socket.close()"), "the page unmount closes the connection");
  assert.ok(app.includes("ingestChipFrame"), "the chips topic feeds the live tail store");
});
