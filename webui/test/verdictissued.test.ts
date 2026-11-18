// omega webui: the verdict ISSUANCE hint.
//
// The gap this closes: a smart-gate escalation registers its card AFTER
// the tool_call hint window (the reviewer round trip runs inside the
// gate, before hub.Ask) and the run is frozen from then until the answer
// no tool_result, no turn_end, no agent_end. The debounced pending read
// (1.5s after the call edge) therefore missed the card and nothing hinted
// again, so a FOREGROUND chat showed no approval until the notifications
// tab's activation read pulled the store. The server now pushes a
// verdict_issued chat-topic frame at registration and app.vue's hint set
// names it.
//
// Layers, the house way (app.vue is not node-importable):
// source pin: app.vue's chat handler runs hintNotifications for the
//   issuance name, beside the call/turn edges, and the 1.5s debounce stays;
// cross-language name agreement: the Go side claims the event name in
// agent/api (verdictIssuedEvent) and the webui spells it as a literal
//   the nuxt build does not typecheck Go, so nothing else would catch a
//   rename on one side;
// the frame is ws-topic-only (the SSE /chat event set is untouched) and
//   its payload is minimal - the webui re-reads GET /verdicts on it.
// Run: node --test "test/*.test.ts" (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join, resolve } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const webui = resolve(here, "..");
const repo = resolve(webui, "..");
const read = (...parts: string[]) => readFileSync(join(...parts), "utf8");

const appSrc = () => read(webui, "app", "app.vue");
const socketsSrc = () => read(repo, "agent", "api", "sockets.go");

// goIssuedEvent reads the event name the Go side claims.
function goIssuedEvent(source: string): string {
  const m = source.match(/verdictIssuedEvent\s*=\s*"([a-z_]+)"/);
  assert.ok(m, "agent/api declares the verdict issuance event name");
  return m![1]!;
}

// appHintSet returns the body of app.vue's chat-topic handler, up to the hint call it
// ends with.
function appHintSet(source: string): string {
  const start = source.indexOf("chat: (session, event, payload) =>");
  assert.ok(start >= 0, "app.vue wires the chat topic's handler");
  const hint = source.indexOf("hintNotifications();", start);
  assert.ok(hint > start, "the chat handler reaches hintNotifications");
  return source.slice(start, hint);
}

// -- app.vue: the issuance name rides the hint set -----------------------

test("app.vue's chat hint set names the issuance frame beside the call/turn edges", () => {
  const chat = appHintSet(appSrc());
  for (const edge of ["tool_call", "tool_result", "turn_end", "agent_end"]) {
    assert.ok(chat.includes(`event === "${edge}"`), `the hint set keeps ${edge}`);
  }
  assert.ok(
    chat.includes(`event === "verdict_issued"`),
    "the issuance frame is in the hint set - a gate card registers after the call-edge window and nothing else would pull it",
  );
  for (const noisy of ["response_chunk", "thinking_chunk", "stream"]) {
    const re = new RegExp(`event === "${noisy}"`);
    assert.ok(!re.test(chat), `${noisy} must not hint the pending read`);
  }
});

test("the 1.5s hint debounce stays - issuance is a hint, not a bypass", () => {
  const app = appSrc();
  const at = app.indexOf("function hintNotifications()");
  assert.ok(at >= 0, "hintNotifications exists");
  const body = app.slice(at, app.indexOf("\n  }", at));
  assert.ok(body.includes("1500"), "the one-pull-per-1.5s window is unchanged");
  assert.ok(body.includes("refreshNotifications"), "the hint still pulls the REST truth");
});

// -- the cross-language name agreement -----------------------------------

test("the Go event name and the webui literal agree", () => {
  const go = goIssuedEvent(socketsSrc());
  assert.equal(go, "verdict_issued", "the Go constant is the claimed name");
  assert.ok(
    appHintSet(appSrc()).includes(`event === "${go}"`),
    `app.vue must spell ${go} exactly - the nuxt build cannot see the Go constant`,
  );
});

// -- the frame's shape: ws-topic-only, minimal payload -------------------

test("the issuance frame rides the chat topic only - the SSE path is untouched", () => {
  const go = socketsSrc();
  const push = go.slice(go.indexOf("func (s *Server) publishVerdictIssued"));
  assert.ok(push.length > 0, "the publish hook exists");
  const body = push.slice(0, push.indexOf("\n}"));
  assert.ok(
    body.includes("PublishEvent(TopicChat, card.SessionID, verdictIssuedEvent"),
    "one ws chat-topic frame, scoped to the card's own session",
  );
  assert.ok(!body.includes("emitRunFrame"), "issuance never enters the run/SSE broadcast funnel");
  assert.ok(!body.includes("Publish(Topic"), "no second topic carries the issuance");

  const frame = go.slice(go.indexOf("type verdictIssuedFrame struct"));
  const fields = frame.slice(0, frame.indexOf("\n}"));
  assert.ok(fields.includes('json:"card_id"'), "the hint names the card");
  assert.ok(fields.includes('json:"session_id"'), "the hint names the session");
  assert.ok(fields.includes('json:"title"'), "the hint carries a label for the toast");
  assert.ok(fields.includes("CardID") && fields.includes("SessionID") && fields.includes("Title"));
  // MINIMAL: the card's answerable shape is the REST read's job
  for (const rest of ["Options", "ExpiresAt", "Body", "AllowText"]) {
    assert.ok(!fields.includes(rest), `the payload must not duplicate the REST card shape (${rest})`);
  }
});

// -- the server side of the seam ----------------------------------------

test("the hub notifies at issuance and the server subscribes at boot", () => {
  const hub = read(repo, "harness", "verdict", "hub.go");
  assert.ok(hub.includes("func (h *Hub) OnCardRegistered(fn func(core.QuestionPrompt))"));
  const ask = hub.slice(hub.indexOf("func (h *Hub) Ask("));
  const body = ask.slice(0, ask.indexOf("timer := time.AfterFunc"));
  assert.ok(body.includes("slices.Clone(h.listeners)"), "listeners are snapshotted under the lock");
  assert.ok(
    body.includes("for _, fn := range listeners") && body.includes("fn(card)"),
    "Ask fires the listeners after registration",
  );
  // the push must not be registered inside the hub's lock
  const fireAt = body.indexOf("for _, fn := range listeners");
  const unlockAt = body.indexOf("h.mu.Unlock()");
  assert.ok(unlockAt >= 0 && unlockAt < fireAt, "listeners fire OUTSIDE the hub lock");

  const sockets = socketsSrc();
  assert.ok(
    sockets.includes("type verdictCardSource interface") && sockets.includes("OnCardRegistered(fn func(core.QuestionPrompt))"),
    "agent/api declares its own consumer face - no harness import, no core seam type",
  );
  const server = read(repo, "agent", "api", "server.go");
  const rewire = server.slice(server.indexOf("func (s *Server) RewireSeams("));
  const rewireBody = rewire.slice(0, rewire.indexOf("\n}"));
  assert.ok(rewireBody.includes("s.subscribeVerdictCards(ctx)"), "boot and every live rewire wire the push");
  assert.ok(
    rewireBody.indexOf("s.seams.mu.Unlock()") < rewireBody.indexOf("s.subscribeVerdictCards(ctx)"),
    "the subscription happens after the seams lock is released",
  );
});
