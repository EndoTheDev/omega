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
  DEFAULT_SOCKET_TOPICS,
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
  assert.deepEqual(JSON.parse(sock.sent[0]!), { type: "subscribe", topics: [...DEFAULT_SOCKET_TOPICS] });
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

test("the ladder runs even when the dead socket never reports close", async () => {
  // The liveness deadline cuts a half-open wire with socket.close(), but the
  // browser's close EVENT can be deferred for the whole outage or never
  // arrive: measured in the browser under `ab set offline on` (both
  // agent-browser 0.26.0 and 0.42.0), close() was called at 15-16s of silence
  // and the close event stayed absent for the entire dark window - it only
  // landed once the network returned. A client that schedules its reconnect
  // from that event sits dark with NO timer armed: no reconnect, no onopen,
  // no catch-up refetch, the pane stays stale and journey 111 reds. Recovery
  // belongs to the ladder the moment the deadline fires (card t_71f28526).
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
  await sleep(400); // past 3x the budget: the cut has fired, no close event came
  assert.equal(sock.closed, true, "the quiet-wire check closed the dead socket");
  assert.equal(
    closes.length,
    1,
    "the cut itself handed recovery to the ladder (no close event was ever delivered)",
  );
  assert.equal(client.status.value, "reconnecting");
  await sleep(1000); // attempt 0 = 500ms, with room for a loaded box
  assert.equal(
    FakeSocket.made.length,
    2,
    "a fresh connection replaces the quiet one without waiting for a close event",
  );
  // the browser's own close event, arriving late, is not news: it must not
  // null the live socket or step the ladder a second time
  sock.drop();
  await sleep(300);
  assert.equal(FakeSocket.made.length, 2, "a stale close never spawns a second ladder step");
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

test("the quiet-wire deadline runs from the last frame, not from the last check", async () => {
  // The budget is a DEADLINE measured from the last frame, so a check that
  // finds the wire alive must re-arm for the REMAINDER. Re-arming a fresh
  // full budget lets a dead wire sit undetected for up to 2x the budget: at
  // the shipped 15s that is a 30s discovery window, longer than the outage
  // a reconnect catch-up is given - an 18s cut never closed the socket, the
  // half-open connection revived with the network, and the catch-up refetch
  // that brings the missed rows back never ran (card t_71f28526).
  FakeSocket.made = [];
  const BUDGET = 500;
  const client = createSocketClient(
    {},
    {
      factory: (u) => new FakeSocket(u) as unknown as WebSocket,
      url: "ws://lane/ws",
      rand: () => 0.5,
      livenessMs: BUDGET,
    },
  );
  client.connect();
  const sock = FakeSocket.made[0]!;
  sock.open();
  await sleep(30); // one frame early in the first budget window, then silence
  sock.push({ topic: "health", data: { status: "ok" } });
  const quietFrom = Date.now();
  // 1.4x the budget: a remainder re-arm cuts at ~1x, a full-budget re-arm
  // needs ~2x
  while (!sock.closed && Date.now() - quietFrom < BUDGET * 1.4) await sleep(10);
  const lateness = Date.now() - quietFrom;
  assert.equal(
    sock.closed,
    true,
    `the cut landed ${lateness}ms after the last frame (budget ${BUDGET}ms) - the deadline is the budget, not a fresh one`,
  );
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

// --- the logs topic's gate (card t_0e7bc0bb) ------------------------------

test("logs is the sixth topic and is NOT in the default subscription", () => {
  assert.deepEqual([...SOCKET_TOPICS], ["chat", "chips", "canvas", "runs", "health", "logs"]);
  assert.ok(!(DEFAULT_SOCKET_TOPICS as readonly string[]).includes("logs"));
});

test("the shell adds logs on the pane's request and drops it again", () => {
  FakeSocket.made = [];
  const client = createSocketClient(
    {},
    { factory: (u) => new FakeSocket(u) as unknown as WebSocket, url: "ws://lane/ws", rand: () => 0.5 },
  );
  client.connect();
  const sock = FakeSocket.made[0]!;
  sock.open();
  const withLogs = [...DEFAULT_SOCKET_TOPICS, "logs"] as const;
  client.subscribe(withLogs);
  assert.deepEqual(client.topics(), [...withLogs]);
  assert.deepEqual(JSON.parse(sock.sent[1]!), { type: "subscribe", topics: [...withLogs] });
  // the reconnect re-sends the CURRENT set - the gate survives a drop
  sock.drop();
  return sleep(600).then(() => {
    FakeSocket.made[1]!.open();
    assert.deepEqual(JSON.parse(FakeSocket.made[1]!.sent[0]!), {
      type: "subscribe",
      topics: [...withLogs],
    });
    // the last logs pane unmounts
    client.subscribe(DEFAULT_SOCKET_TOPICS);
    assert.ok(!client.topics().includes("logs"));
    client.close();
  });
});

test("a logs frame hands the pane only the appended lines, verbatim", () => {
  const got: string[][] = [];
  const handlers: SocketHandlers = { logs: (lines) => got.push(lines) };
  dispatchFrame(
    { topic: "logs", data: { lines: ["2026/11/11 10:00:00 one", "2026/11/11 10:00:01 two"] } },
    handlers,
  );
  assert.deepEqual(got, [["2026/11/11 10:00:00 one", "2026/11/11 10:00:01 two"]]);
  // malformed payloads are silent no-ops, never a crash
  dispatchFrame({ topic: "logs", data: { lines: "nope" } }, handlers);
  dispatchFrame({ topic: "logs", data: { lines: [1, "kept", null] } }, handlers);
  dispatchFrame({ topic: "logs", data: { lines: [] } }, handlers);
  dispatchFrame({ topic: "logs" }, handlers);
  assert.deepEqual(got.at(-1), ["kept"]);
  assert.equal(got.length, 2);
});
