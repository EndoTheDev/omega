// omega webui: unit tests for the observability tab (card t_e703180e - the
// face half of the observability arc). Two halves, the house convention:
//
//   1. the PURE half (harness/observability/ui/rows.ts): the wire guards,
//      the errors-only filter, the newest-first render, the session query,
//      the two formatters and the 404 sentences. The fixture below is a
//      byte-faithful slice of GET /observability/events (the capsule's
//      Event shape, harness/observability/ring.go).
//   2. the SFC wiring (harness/observability/ui/ObservabilityTab.vue) as
//      structural pins - the node suite has no DOM, and the browser half
//      is a live check on the lane, not a journey.
//
// Run: node --test test/observability.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  describeReadError,
  errorCount,
  EVENTS_LIMIT,
  EVENTS_PATH,
  eventsQuery,
  formatDuration,
  formatEventTime,
  LOGS_PATH,
  logsQuery,
  normalizeEvent,
  parseEventsPayload,
  parseLogsPayload,
  REFRESH_MS,
  RENDER_CAP,
  shortSession,
  TAIL_LINES,
  visibleEvents,
  type ObservatoryEvent,
} from "../../harness/observability/ui/rows.ts";
import {
  isNarrowWidth as obsIsNarrowWidth,
  NARROW_MAX_PX,
} from "../../harness/observability/ui/narrow.ts";
import { isNarrowWidth as shellIsNarrowWidth } from "../app/composables/narrow.ts";
import { DOCKABLES, TAB_MODULES, defaultHostTabs } from "../app/composables/dockables.ts";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
const sfc = readFileSync(
  join(root, "harness", "observability", "ui", "ObservabilityTab.vue"),
  "utf8",
);
const app = readFileSync(join(root, "webui", "app", "app.vue"), "utf8");

// --- the fixture: the capsule's events body, verbatim shape -------------
// One turn: start, a tool call with its derived duration, the tool result
// (an error - IsError on the wire), a stream error, a turn end.
const FIXTURE = {
  events: [
    {
      seq: 11,
      time: "2026-11-11T01:20:00.100000000Z",
      session: "sess-aaaaaaaa-1111",
      type: "agent_start",
      level: "info",
      summary: "run started",
    },
    {
      seq: 12,
      time: "2026-11-11T01:20:00.400000000Z",
      session: "sess-aaaaaaaa-1111",
      type: "turn_start",
      level: "info",
      turn: 1,
      summary: "turn 1",
    },
    {
      seq: 13,
      time: "2026-11-11T01:20:01.000000000Z",
      session: "sess-aaaaaaaa-1111",
      type: "tool_call",
      level: "info",
      tool: "shell.run",
      turn: 1,
      duration_ms: 812,
      summary: "shell.run",
    },
    {
      seq: 14,
      time: "2026-11-11T01:20:01.812000000Z",
      session: "sess-aaaaaaaa-1111",
      type: "tool_result",
      level: "error",
      tool: "shell.run",
      duration_ms: 812,
      summary: "shell.run failed",
      error: "exit status 2",
    },
    {
      seq: 15,
      time: "2026-11-11T01:20:02.000000000Z",
      session: "sess-bbbbbbbb-2222",
      type: "stream_end",
      level: "error",
      summary: "provider stream ended with an error",
      error: "upstream 502",
    },
    {
      seq: 16,
      time: "2026-11-11T01:20:02.500000000Z",
      session: "sess-bbbbbbbb-2222",
      type: "turn_end",
      level: "warn",
      turn: 1,
      summary: "turn ended without a clean stop",
    },
  ],
  returned: 6,
  buffered: 6,
  capacity: 1024,
};

// --- the wire guards ---------------------------------------------------

test("parseEventsPayload reads the events body and keeps the ring size", () => {
  const body = parseEventsPayload(FIXTURE);
  assert.equal(body.events.length, 6);
  assert.equal(body.buffered, 6);
  assert.equal(body.capacity, 1024, "the ring's capacity rides the answer");
  // every record survives with its derived level and the optional fields
  assert.deepEqual(
    body.events.map((e) => [e.seq, e.type, e.level]),
    [
      [11, "agent_start", "info"],
      [12, "turn_start", "info"],
      [13, "tool_call", "info"],
      [14, "tool_result", "error"],
      [15, "stream_end", "error"],
      [16, "turn_end", "warn"],
    ],
  );
  assert.equal(body.events[3]!.tool, "shell.run");
  assert.equal(body.events[3]!.duration_ms, 812);
  assert.equal(body.events[3]!.error, "exit status 2");
  // a record with no session (a one-shot run) simply has none
  const bare = parseEventsPayload({ events: [{ type: "agent_end", level: "info", summary: "done" }] });
  assert.equal(bare.events[0]!.session, undefined);
});

test("parseEventsPayload never throws on a malformed body", () => {
  for (const bad of [null, undefined, 3, "nope", [], {}, { events: "nope" }]) {
    const body = parseEventsPayload(bad);
    assert.deepEqual(body.events, [], `body ${JSON.stringify(bad)} yields no rows`);
    assert.equal(typeof body.buffered, "number");
    assert.equal(typeof body.capacity, "number");
  }
  // a record without a type has no row to show; the rest of the page lives
  const mixed = parseEventsPayload({
    events: [{ seq: 1 }, null, "x", { type: "turn_end", summary: "ok" }],
  });
  assert.deepEqual(mixed.events.map((e) => e.type), ["turn_end"]);
  assert.equal(mixed.events[0]!.level, "info", "a missing level reads info, never blank");
});

test("normalizeEvent drops non-objects and keeps the optional fields only when set", () => {
  assert.equal(normalizeEvent(null), null);
  assert.equal(normalizeEvent([{ type: "x" }]), null);
  const e = normalizeEvent({
    seq: 2,
    time: "t",
    type: "tool_call",
    level: "warn",
    tool: "",
    turn: 0,
    duration_ms: 0,
    summary: "s",
    error: "",
  })!;
  assert.equal(e.tool, undefined, "an empty tool is not a tool");
  assert.equal(e.error, undefined, "an empty error is not an error");
  assert.equal(e.turn, 0, "a zero turn is still the loop's turn");
  assert.equal(e.duration_ms, 0, "a zero duration is the pair's own answer");
});

test("parseLogsPayload keeps the tail verbatim", () => {
  const body = parseLogsPayload({ path: "/home/x/.omega/omega.log", lines: ["a\rb", "c"] });
  assert.equal(body.path, "/home/x/.omega/omega.log");
  assert.deepEqual(body.lines, ["a\rb", "c"], "tail lines are the process's own bytes");
  assert.deepEqual(parseLogsPayload(null), { path: "", lines: [] });
  assert.deepEqual(parseLogsPayload({ lines: [1, "ok"] }).lines, ["ok"]);
});

// --- the reads' queries ------------------------------------------------

test("eventsQuery scopes the session only when one is in scope", () => {
  assert.equal(eventsQuery(""), `limit=${EVENTS_LIMIT}`);
  assert.equal(eventsQuery("sess-1"), `session=sess-1&limit=${EVENTS_LIMIT}`);
  // the ring's page ceiling IS the limit: one page is the whole history
  assert.equal(EVENTS_LIMIT, 1000);
  assert.ok(EVENTS_LIMIT <= 1024, "the ring cap bounds the page");
});

test("logsQuery asks for the route's default tail", () => {
  assert.equal(logsQuery(), `lines=${TAIL_LINES}`);
  assert.equal(TAIL_LINES, 200, "the capsule's tail default");
});

test("the read paths are the capsule's two routes", () => {
  assert.equal(EVENTS_PATH, "/observability/events");
  assert.equal(LOGS_PATH, "/observability/logs");
});

// --- the errors-only filter + the newest-first render ------------------

test("visibleEvents paints newest first (the wire is oldest first)", () => {
  const events = parseEventsPayload(FIXTURE).events;
  assert.deepEqual(
    visibleEvents(events, false).map((e) => e.seq),
    [16, 15, 14, 13, 12, 11],
  );
});

test("the errors-only chip keeps the error rows and nothing else", () => {
  const events = parseEventsPayload(FIXTURE).events;
  const errors = visibleEvents(events, true);
  assert.deepEqual(errors.map((e) => e.seq), [15, 14], "newest error first");
  assert.ok(errors.every((e) => e.level === "error"));
  assert.equal(errorCount(events), 2, "the meta line's error count");
  // a warn is NOT an error: the chip is errors-only, not errors+warnings
  assert.ok(!errors.some((e) => e.type === "turn_end"));
});

test("visibleEvents bounds the DOM at the render cap, newest kept", () => {
  const many: ObservatoryEvent[] = Array.from({ length: RENDER_CAP + 50 }, (_, i) => ({
    seq: i + 1,
    time: "2026-11-11T00:00:00Z",
    type: "tool_result",
    level: "info",
    summary: `row ${i + 1}`,
  }));
  const shown = visibleEvents(many, false);
  assert.equal(shown.length, RENDER_CAP, "the cap holds");
  assert.equal(shown[0]!.seq, RENDER_CAP + 50, "the newest row leads");
  assert.equal(shown[RENDER_CAP - 1]!.seq, 51, "the oldest 50 fell off the DOM");
});

// --- session scope -----------------------------------------------------

test("shortSession attributes a row in the all-sessions scope", () => {
  const events = parseEventsPayload(FIXTURE).events;
  assert.equal(shortSession(events[0]!.session), "sess-aaa");
  assert.equal(shortSession(undefined), "-", "a sessionless run says so");
  assert.equal(shortSession("tny"), "tny");
});

// --- the formatters ----------------------------------------------------

test("formatEventTime renders the row's clock reading", () => {
  const at = new Date(2026, 10, 11, 1, 20, 3, 0); // local 01:20:03
  assert.equal(formatEventTime(at.toISOString()), "01:20:03");
  assert.equal(formatEventTime(""), "-");
  assert.equal(formatEventTime("not-a-time"), "not-a-time", "the server's bytes, not a blank cell");
});

test("formatDuration reads the derived pair duration", () => {
  assert.equal(formatDuration(0), "0ms");
  assert.equal(formatDuration(7), "7ms");
  assert.equal(formatDuration(812), "812ms");
  assert.equal(formatDuration(1000), "1.0s");
  assert.equal(formatDuration(1430), "1.4s");
  assert.equal(formatDuration(undefined), "-", "an unpaired call carries no duration");
  assert.equal(formatDuration(-1), "-");
});

// --- the honest 404s ---------------------------------------------------

test("describeReadError turns the capsule's 404s into states, not failures", () => {
  assert.match(describeReadError(EVENTS_PATH, 404), /module .* not loaded/);
  assert.match(describeReadError(LOGS_PATH, 404), /no log file/);
  assert.equal(describeReadError(EVENTS_PATH, 500), "GET /observability/events: 500");
});

// --- the registry / manifests ------------------------------------------

test("the tab is declared in TAB_MODULES, with its own registry entry", () => {
  assert.ok(
    (TAB_MODULES as readonly string[]).includes("observability"),
    "the shell declares the composition (dockables.ts TAB_MODULES)",
  );
  const entry = DOCKABLES.find((d) => d.id === "observability");
  assert.ok(entry, "the registry carries the tab");
  // card t_01c53e78 (Endo ruling 2026-11-13): the surviving logs pane is
  // named "Logs" - the raw-tail http_channel pane that carried that name
  // retired with its component
  assert.equal(entry!.title, "Logs");
  assert.equal(entry!.area, "bottom", "the logs home - a wide, short stream reads best");
  assert.equal(entry!.closable, false, "the pinned-bottom shape");
  assert.equal(entry!.policy, "singleton");
  assert.equal(entry!.icon, "activity", "its own glyph, not the logs scroll");
  // the fresh bottom strip seats the terminal and the Logs tab; the
  // retired raw-tail pane has no seat (nor a registry entry to restore)
  assert.deepEqual(defaultHostTabs("bottom"), ["terminal", "observability"]);
  assert.ok(
    !DOCKABLES.some((d) => (d.id as string) === "http_channel"),
    "the retired raw-tail pane left the registry (deletability)",
  );
});

test("app.vue binds the tab from the tabs manifest, never a hardcoded import", () => {
  assert.ok(
    app.includes('t.module === "observability"'),
    "the capsule's view is resolved from module-tabs.generated.ts",
  );
  assert.ok(app.includes("#pane-observability"), "the outlet exists");
  assert.ok(!app.includes('from "./components/observability/'), "no capsule UI squatting in the shell");
  assert.ok(
    app.includes(':session="focusedSessionId"'),
    "the OPEN session is the tab's default scope (ruling 2)",
  );
});

// --- the SFC wiring (shape pins) ---------------------------------------

test("the band carries the three controls, aria-pressed throughout", () => {
  // card t_01c53e78: the band title follows the dock title - one word, Logs
  assert.ok(sfc.includes('title="Logs"'), "the universal band");
  assert.ok(sfc.includes('class="band-tool obs-chip obs-scope"'), "the session scope chip");
  assert.ok(sfc.includes('aria-label="toggle errors only"'), "the errors-only chip");
  assert.ok(sfc.includes('class="band-tool obs-chip obs-tail"'), "the tail view chip");
  // three aria-pressed bindings: scope, errors, tail
  assert.equal(sfc.split(':aria-pressed=').length - 1, 3);
  assert.ok(sfc.includes(':disabled="!session"'), "a shell with no open session cannot scope");
});

test("the stream is the PRIMARY view and the tail the SECONDARY one", () => {
  assert.ok(sfc.includes('v-if="!tailOpen"'), "the stream renders unless the tail chip is on");
  assert.ok(sfc.includes('v-else ref="bodyEl"'), "the tail replaces it in the same pane");
  assert.ok(sfc.includes("v-for=\"e in shown\""), "the body iterates the filtered rows");
  assert.ok(sfc.includes("{{ formatEventTime(e.time) }}"), "the time cell");
  assert.ok(sfc.includes("{{ formatDuration(e.duration_ms) }}"), "the duration cell");
  assert.ok(sfc.includes("{{ e.tool || \"\" }}"), "the tool cell");
  assert.ok(sfc.includes('class="obs-cell obs-errline"'), "the failure text rides the row");
  // errors are visually distinct: the level rides the row class, and the
  // stylesheet paints the rail
  assert.ok(sfc.includes(':class="[e.level, { allscope: allScope }]"'), "level classes on the row");
  assert.ok(sfc.includes(".obs-row.error"), "the error rail is styled");
  assert.ok(sfc.includes('color: var(--err)'), "the error colour is the theme's");
});

test("the scope flips a SERVER parameter; the errors chip does not", () => {
  assert.ok(sfc.includes("eventsQuery(scopedSession.value)"), "the scope rides the query");
  assert.ok(
    sfc.includes("visibleEvents(events.value, errorsOnly.value)"),
    "errors-only filters the fetched page in-band (no refetch per toggle)",
  );
  assert.ok(sfc.includes("watch(scopedSession, () => void readStream())"), "a scope flip re-reads");
  assert.ok(sfc.includes("watch(tailOpen, () => void refresh())"), "a view flip re-reads");
});

test("the refresh is bounded by mount, not a permanent poll", () => {
  assert.ok(sfc.includes("setInterval(() => void refresh(), REFRESH_MS)"), "the bounded refresh");
  assert.ok(sfc.includes("clearInterval(timer)"), "and it dies with the component");
  assert.ok(REFRESH_MS >= 2500, "the ring is quieter than a log every request writes to");
  // the push channel's topic set is closed and carries no observability
  // topic - the comment records WHY there is no hub subscription here
  assert.ok(
    sfc.includes("no observability topic"),
    "the deviation from 'ride the hub' is named where a reader looks",
  );
});

test("the tab measures its own container for the narrow flip", () => {
  assert.ok(sfc.includes("new ResizeObserver"), "the module-UI narrow recipe (t_a2fdc630)");
  assert.ok(sfc.includes("isNarrowWidth"), "the module's own threshold copy");
  for (const px of [0, 100, 240, 300, 360, 400, 640]) {
    assert.equal(obsIsNarrowWidth(px), shellIsNarrowWidth(px), `agreement at ${px}px`);
  }
  assert.equal(NARROW_MAX_PX, 360);
});
