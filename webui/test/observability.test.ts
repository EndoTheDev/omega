// omega webui: unit tests for the observability tab
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
  CRON_LABEL_PREFIX,
  describeReadError,
  errorCount,
  EVENTS_LIMIT,
  EVENTS_PATH,
  eventsQuery,
  eventSource,
  formatDuration,
  formatEventTime,
  GATEWAY_ORIGIN,
  LOGS_PATH,
  logsQuery,
  normalizeEvent,
  parseEventsPayload,
  parseLogsPayload,
  parseSessionLabels,
  passesFilter,
  REFRESH_MS,
  RENDER_CAP,
  SESSIONS_PATH,
  shortSession,
  SOURCES,
  sourceCounts,
  TAIL_LINES,
  visibleEvents,
  type ObservatoryEvent,
  type Source,
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

// the fixture: the capsule's events body, verbatim shape ------------- One turn.
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

// the wire guards ------------------------------------------------

test("parseEventsPayload reads the events body and keeps the ring size", () => {
  const body = parseEventsPayload(FIXTURE);
  assert.equal(body.events.length, 6);
  assert.equal(body.buffered, 6);
  assert.equal(body.capacity, 1024, "the ring's capacity rides the answer");
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
    origin: "",
  })!;
  assert.equal(e.tool, undefined, "an empty tool is not a tool");
  assert.equal(e.error, undefined, "an empty error is not an error");
  assert.equal(e.origin, undefined, "an empty origin is no origin");
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

// the reads' queries ---------------------------------------------

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

// the errors-only filter + the newest-first render ---------------

test("visibleEvents paints newest first (the wire is oldest first)", () => {
  const events = parseEventsPayload(FIXTURE).events;
  assert.deepEqual(
    visibleEvents(events, {}).map((e) => e.seq),
    [16, 15, 14, 13, 12, 11],
  );
});

test("the errors-only chip keeps the error rows and nothing else", () => {
  const events = parseEventsPayload(FIXTURE).events;
  const errors = visibleEvents(events, { errorsOnly: true });
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
  const shown = visibleEvents(many, {});
  assert.equal(shown.length, RENDER_CAP, "the cap holds");
  assert.equal(shown[0]!.seq, RENDER_CAP + 50, "the newest row leads");
  assert.equal(shown[RENDER_CAP - 1]!.seq, 51, "the oldest 50 fell off the DOM");
});

// the source family -------------------------------------------------

test("the source vocabulary is the five the ring carries", () => {
  assert.deepEqual([...SOURCES], ["llm", "agent", "cron", "kanban", "gateway"]);
});

test("eventSource reads the record's own provenance", () => {
  const rec = (over: Partial<ObservatoryEvent>): ObservatoryEvent => ({
    seq: 1,
    time: "2026-11-11T00:00:00Z",
    type: "turn_start",
    level: "info",
    summary: "s",
    ...over,
  });
  assert.equal(eventSource(rec({ type: "stream_end" })), "llm", "the provider stream");
  assert.equal(eventSource(rec({ type: "tool_call", tool: "kanban.list" })), "kanban");
  assert.equal(eventSource(rec({ type: "tool_result", tool: "kanban.complete" })), "kanban");
  assert.equal(eventSource(rec({ type: "tool_call", tool: "shell.run" })), "agent");
  assert.equal(eventSource(rec({ type: "agent_end" })), "agent");
  assert.equal(eventSource(rec({ type: "agent_start", session: undefined })), "agent");
});

test("eventSource gives the serving layer its own source off the run origin", () => {
  const rec = (over: Partial<ObservatoryEvent>): ObservatoryEvent => ({
    seq: 1,
    time: "2026-11-11T00:00:00Z",
    type: "turn_start",
    level: "info",
    summary: "s",
    ...over,
  });
  assert.equal(eventSource(rec({ type: "agent_start", origin: GATEWAY_ORIGIN })), "gateway");
  assert.equal(
    eventSource(rec({ type: "stream_end", origin: GATEWAY_ORIGIN })),
    "gateway",
    "the RUN outranks the stream's own record - a gateway run's whole trace is the serving layer's",
  );
  assert.equal(
    eventSource(rec({ type: "tool_call", tool: "kanban.list", origin: GATEWAY_ORIGIN })),
    "gateway",
    "and the tool's capsule too",
  );
  assert.equal(
    eventSource(rec({ type: "agent_start", origin: "interactive" })),
    "agent",
    "a terminal run is not the serving layer - only the gateway origin names it",
  );
  assert.equal(eventSource(rec({ type: "agent_start", origin: "" })), "agent", "an empty origin is no origin");
});

test("eventSource lets the CRON run outrank the record's own producer", () => {
  const labels = new Map([["sess-cron-1", `${CRON_LABEL_PREFIX}nightly`]]);
  const streamEnd: ObservatoryEvent = {
    seq: 2,
    time: "2026-11-11T00:00:00Z",
    session: "sess-cron-1",
    type: "stream_end",
    level: "info",
    summary: "s",
  };
  assert.equal(eventSource(streamEnd, labels), "cron", "a cron-fired run's whole trace is the scheduler's");
  assert.equal(eventSource(streamEnd), "llm", "without the label the record reads by its own producer");
  const kanbanCall: ObservatoryEvent = { ...streamEnd, type: "tool_call", tool: "kanban.list" };
  assert.equal(eventSource(kanbanCall, labels), "cron", "the run outranks the tool too");
  const otherLabel = new Map([["sess-cron-1", "chat about cron jobs"]]);
  assert.equal(eventSource(kanbanCall, otherLabel), "kanban", "only the scheduler's own prefix counts");
  assert.equal(
    eventSource({ ...streamEnd, origin: GATEWAY_ORIGIN }, labels),
    "cron",
    "the cron label still leads: a scheduled run fired through any surface is the scheduler's",
  );
});

test("parseSessionLabels reads the store list down to id -> label", () => {
  const map = parseSessionLabels([
    { id: "a", label: "cron:nightly", message_count: 3 },
    { id: "b" },
    { id: "", label: "cron:ghost" },
    { label: "cron:no-id" },
    null,
    "nope",
  ]);
  assert.deepEqual([...map.entries()], [["a", "cron:nightly"]]);
  assert.deepEqual([...parseSessionLabels(null).entries()], [], "a malformed body degrades to no labels");
  assert.equal(SESSIONS_PATH, "/sessions", "the labels ride the store's own list");
});

test("sourceCounts counts every record once, filters aside", () => {
  const events = parseEventsPayload(FIXTURE).events;
  const counts = sourceCounts(events);
  assert.equal(counts.llm, 1, "the one stream_end");
  assert.equal(counts.agent, 5, "the loop's own rows");
  assert.equal(counts.cron, 0);
  assert.equal(counts.kanban, 0);
  assert.equal(counts.gateway, 0, "no origin in the fixture: a CLI-shaped run reads by its own producer");
  const total = SOURCES.reduce((n, s) => n + counts[s], 0);
  assert.equal(total, events.length, "every record lands in exactly one source");
});

test("the source chips AND-compose with the severity chip", () => {
  const events = parseEventsPayload(FIXTURE).events;
  const only = (...sources: Source[]) => visibleEvents(events, { sources });
  assert.deepEqual(only("llm").map((e) => e.seq), [15], "the provider stream alone");
  assert.deepEqual(only("agent").map((e) => e.seq), [16, 14, 13, 12, 11], "the loop alone");
  assert.deepEqual(only("llm", "agent").map((e) => e.seq), [16, 15, 14, 13, 12, 11]);
  assert.deepEqual(only().map((e) => e.seq), [], "an empty set is every source OFF, not every source on");
  // AND, not OR: the severity chip still has to agree
  assert.deepEqual(
    visibleEvents(events, { errorsOnly: true, sources: ["agent"] }).map((e) => e.seq),
    [14],
    "errors-only AND agent leaves the failed tool result and nothing else",
  );
  assert.deepEqual(
    visibleEvents(events, { errorsOnly: true, sources: ["llm"] }).map((e) => e.seq),
    [15],
  );
  assert.deepEqual(
    visibleEvents(events, { errorsOnly: true, sources: ["kanban"] }).map((e) => e.seq),
    [],
    "a source with no rows on the page paints nothing",
  );
});

test("passesFilter is the one gate the render walks", () => {
  const events = parseEventsPayload(FIXTURE).events;
  assert.equal(passesFilter(events[0]!, {}), true, "no filter, no drop");
  assert.equal(passesFilter(events[3]!, { errorsOnly: true }), true);
  assert.equal(passesFilter(events[0]!, { errorsOnly: true }), false);
  assert.equal(passesFilter(events[3]!, { sources: ["agent"] }), true);
  assert.equal(passesFilter(events[3]!, { sources: ["llm"] }), false);
});

test("the clear button's watermark hides what was read, and only that", () => {
  const events = parseEventsPayload(FIXTURE).events;
  const cleared = visibleEvents(events, { clearedThrough: 14 });
  assert.deepEqual(cleared.map((e) => e.seq), [16, 15], "rows at or below the watermark are out of view");
  assert.deepEqual(
    visibleEvents(events, { clearedThrough: 16 }).map((e) => e.seq),
    [],
    "clearing the whole page empties the view",
  );
  assert.deepEqual(
    visibleEvents(events, { clearedThrough: 0 }).map((e) => e.seq),
    [16, 15, 14, 13, 12, 11],
    "the watermark defaults to nothing cleared",
  );
  // the ring is untouched: the same page still holds every record
  assert.equal(errorCount(events), 2);
});

// session scope --------------------------------------------------

test("shortSession attributes a row in the all-sessions scope", () => {
  const events = parseEventsPayload(FIXTURE).events;
  assert.equal(shortSession(events[0]!.session), "sess-aaa");
  assert.equal(shortSession(undefined), "-", "a sessionless run says so");
  assert.equal(shortSession("tny"), "tny");
});

// the formatters -------------------------------------------------

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

// the honest 404s ------------------------------------------------

test("describeReadError turns the capsule's 404s into states, not failures", () => {
  assert.match(describeReadError(EVENTS_PATH, 404), /module .* not loaded/);
  assert.match(describeReadError(LOGS_PATH, 404), /no log file/);
  assert.equal(describeReadError(EVENTS_PATH, 500), "GET /observability/events: 500");
});

// the registry / manifests ---------------------------------------

test("the tab is declared in TAB_MODULES, with its own registry entry", () => {
  assert.ok(
    (TAB_MODULES as readonly string[]).includes("observability"),
    "the shell declares the composition (dockables.ts TAB_MODULES)",
  );
  const entry = DOCKABLES.find((d) => d.id === "observability");
  assert.ok(entry, "the registry carries the tab");
  assert.equal(entry!.title, "Logs");
  assert.equal(entry!.area, "bottom", "the logs home - a wide, short stream reads best");
  assert.equal(entry!.closable, false, "the pinned-bottom shape");
  assert.equal(entry!.policy, "singleton");
  assert.equal(entry!.icon, "activity", "its own glyph, not the logs scroll");
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

// the SFC wiring (shape pins) ------------------------------------

test("the band carries the three toggles plus the clear action, aria-pressed throughout", () => {
  assert.ok(sfc.includes('title="Logs"'), "the universal band");
  assert.ok(sfc.includes('class="band-tool obs-chip obs-scope"'), "the session scope chip");
  assert.ok(sfc.includes('aria-label="toggle errors only"'), "the errors-only chip");
  assert.ok(sfc.includes('class="band-tool obs-chip obs-tail"'), "the tail view chip");
  assert.ok(sfc.includes('class="band-tool obs-chip obs-clear"'), "the clear action");
  assert.ok(!sfc.includes('aria-pressed="clear'), "clear is an action, never a toggle");
  assert.ok(sfc.includes(':disabled="tailOpen || !events.length"'), "nothing to clear in the tail view");
  assert.ok(sfc.includes(':disabled="!session"'), "a shell with no open session cannot scope");
  // three toggle bindings in the band + the strip's ONE binding repeated per source chip
  assert.equal(sfc.split(':aria-pressed=').length - 1, 4, "every toggle says its state; no others do");
});

test("the source strip is one toggle per source, multi-select", () => {
  assert.ok(sfc.includes('class="obs-sources"'), "the source family's own row");
  assert.ok(sfc.includes('role="toolbar"'), "a toolbar, like the band's action seat");
  assert.ok(sfc.includes('v-for="s in SOURCES"'), "the chips come from the closed vocabulary");
  assert.ok(sfc.includes(':aria-pressed="sourceOn(s)"'), "each chip is a toggle");
  assert.ok(sfc.includes(':aria-label="\'toggle \' + s + \' records\'"'), "a name per source");
  assert.ok(sfc.includes("new Set(SOURCES)"), "every source starts ON - the unfiltered stream");
  assert.ok(sfc.includes("v-if=\"!tailOpen\""), "the family belongs to the stream view, not the tail");
  // the strip is styled from the existing ladder: no invented colors
  for (const token of ["--accent", "--ok", "--text-dim", "--text"]) {
    assert.ok(sfc.includes(`--src: var(${token})`), `a source accent rides ${token}`);
  }
  assert.ok(
    sfc.includes(".obs-src.src-gateway") &&
      sfc.includes("--src: color-mix(in srgb, var(--accent) 55%, var(--text-dim))"),
    "the gateway accent is a ladder derivation - the palette has no fifth hue and --unread/--err are semantic",
  );
  assert.ok(!sfc.includes("--src: var(--unread)"), "unread is semantic-only (DESIGN.md)");
  assert.ok(!sfc.includes("--src: var(--err)"), "err is failure-only");
});

test("the stream is the PRIMARY view and the tail the SECONDARY one", () => {
  assert.ok(sfc.includes('v-if="!tailOpen"'), "the stream renders unless the tail chip is on");
  assert.ok(sfc.includes('v-else ref="bodyEl"'), "the tail replaces it in the same pane");
  assert.ok(sfc.includes("v-for=\"e in shown\""), "the body iterates the filtered rows");
  assert.ok(sfc.includes("{{ formatEventTime(e.time) }}"), "the time cell");
  assert.ok(sfc.includes("{{ formatDuration(e.duration_ms) }}"), "the duration cell");
  assert.ok(sfc.includes("{{ e.tool || \"\" }}"), "the tool cell");
  assert.ok(sfc.includes('class="obs-cell obs-errline"'), "the failure text rides the row");
  assert.ok(
    sfc.includes(':class="[e.level, \'src-\' + sourceFor(e), { allscope: allScope }]"'),
    "the row carries its level AND its source class",
  );
  assert.ok(sfc.includes(':data-source="sourceFor(e)"'), "the source is readable off the row");
  assert.ok(sfc.includes(".obs-row.error"), "the error rail is styled");
  assert.ok(sfc.includes('color: var(--err)'), "the error colour is the theme's");
  assert.ok(sfc.includes("{{ emptyLine }}"), "one empty state, reason first");
});

test("the scope flips a SERVER parameter; the two filter families do not", () => {
  assert.ok(sfc.includes("eventsQuery(scopedSession.value)"), "the scope rides the query");
  assert.ok(
    sfc.includes("errorsOnly: errorsOnly.value"),
    "errors-only filters the fetched page in-band (no refetch per toggle)",
  );
  assert.ok(sfc.includes("sources: Array.from(enabled.value)"), "the source set rides the same gate");
  assert.ok(sfc.includes("clearedThrough: clearedThrough.value"), "and so does the clear watermark");
  assert.ok(sfc.includes("labels.value"), "the cron attribution reads the store's labels");
  assert.ok(sfc.includes("parseSessionLabels("), "one tolerant parse for GET /sessions");
  assert.ok(sfc.includes("watch(scopedSession, () => void readStream())"), "a scope flip re-reads");
  assert.ok(sfc.includes("watch(tailOpen, () => void refresh())"), "a view flip re-reads");
});

test("the refresh is bounded by mount, not a permanent poll", () => {
  assert.ok(sfc.includes("setInterval(() => void refresh(), REFRESH_MS)"), "the bounded refresh");
  assert.ok(sfc.includes("clearInterval(timer)"), "and it dies with the component");
  assert.ok(REFRESH_MS >= 2500, "the ring is quieter than a log every request writes to");
});

test("the tab measures its own container for the narrow flip", () => {
  assert.ok(sfc.includes("new ResizeObserver"), "the module-UI narrow recipe (t_a2fdc630)");
  assert.ok(sfc.includes("isNarrowWidth"), "the module's own threshold copy");
  for (const px of [0, 100, 240, 300, 360, 400, 640]) {
    assert.equal(obsIsNarrowWidth(px), shellIsNarrowWidth(px), `agreement at ${px}px`);
  }
  assert.equal(NARROW_MAX_PX, 360);
});
