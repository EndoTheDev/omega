// omega webui: unit tests for the logs tab's severity filter (card
// t_221dc498). The filter is pure: classifyLogLine/filterLogLines in
// api/http_channel/ui/severity.ts classify the raw tail lines on the
// in-band marker convention (harness/logging Errorf -> "ERROR: ",
// Warnf -> "WARN: " after the log.LstdFlags timestamp - the Go-side
// pin is TestFileLoggerWarnf). The node suite pins the pure half and
// the SFC wiring (the composition-test convention); the live chip
// behavior is the journey recipe's witness (103-logs-severity-filter).

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  classifyLogLine,
  filterLogLines,
  LOG_SEVERITIES,
  type LogSeverity,
} from "../../api/http_channel/ui/severity.ts";

const sfc = readFileSync(
  join(dirname(fileURLToPath(import.meta.url)), "..", "..", "api", "http_channel", "ui", "LogsList.vue"),
  "utf8",
);

// --- the pure classifier ----------------------------------------------

test("classifyLogLine reads the marker after the timestamp", () => {
  assert.equal(classifyLogLine("2026/11/08 02:42:00 agent loop starting, model=m"), "info");
  assert.equal(classifyLogLine("2026/11/08 02:42:00 ERROR: stream error: boom"), "error");
  assert.equal(classifyLogLine("2026/11/08 02:42:00 WARN: vision_model set but no sink"), "warning");
  assert.equal(classifyLogLine("2026/11/08 02:42:00 WARNING: foreign writer"), "warning");
});

test("classifyLogLine classifies a marker with no timestamp, never mid-line", () => {
  assert.equal(classifyLogLine("ERROR: bare line"), "error");
  assert.equal(classifyLogLine("WARN: bare line"), "warning");
  // a message QUOTING a marker stays info - only the prefix classifies
  assert.equal(classifyLogLine("2026/11/08 02:42:00 agent said: ERROR: not a marker"), "info");
  assert.equal(classifyLogLine("plain text, no logger shape"), "info");
});

test("LOG_SEVERITIES carries the card's three levels in chip order", () => {
  assert.deepEqual(LOG_SEVERITIES, ["info", "warning", "error"]);
});

test("filterLogLines keeps the enabled severities, order preserved", () => {
  const lines = [
    "2026/11/08 02:42:00 agent loop starting",
    "2026/11/08 02:42:01 ERROR: stream error",
    "2026/11/08 02:42:02 WARN: no caption sink",
    "2026/11/08 02:42:03 WARN: second warning",
  ];
  const all: Record<LogSeverity, boolean> = { info: true, warning: true, error: true };
  assert.equal(filterLogLines(lines, all).length, 4);

  const noInfo: Record<LogSeverity, boolean> = { info: false, warning: true, error: true };
  const kept = filterLogLines(lines, noInfo);
  assert.deepEqual(
    kept.map((e) => e.severity),
    ["error", "warning", "warning"],
  );
  assert.equal(kept[0]!.text, lines[1]);

  const onlyErrors: Record<LogSeverity, boolean> = { info: false, warning: false, error: true };
  assert.deepEqual(
    filterLogLines(lines, onlyErrors).map((e) => e.text),
    [lines[1]],
  );

  // all-off is legal: the tab shows the empty-filter state, not a lie
  const none: Record<LogSeverity, boolean> = { info: false, warning: false, error: false };
  assert.equal(filterLogLines(lines, none).length, 0);
});

// --- the SFC wiring (shape pins - the browser half is the journey) ----

test("LogsList renders one toggle chip per severity in the band", () => {
  assert.ok(sfc.includes("v-for=\"sev in LOG_SEVERITIES\""), "chips must iterate LOG_SEVERITIES");
  assert.ok(sfc.includes(":aria-pressed=\"sevEnabled[sev]\""), "chip state rides aria-pressed");
  assert.ok(sfc.includes("class=\"band-tool logs-sev-chip\""), "chips use the band-tool look");
  // the aria-label is a template literal over the chip loop - one
  // source string covers every severity
  assert.ok(sfc.includes("toggle ${sev} lines"), "chip aria-label must name the toggled severity");
});

test("LogsList renders the filtered stream, not the raw lines", () => {
  assert.ok(sfc.includes("filterLogLines"), "the filter helper must be wired in");
  assert.ok(sfc.includes("v-for=\"(entry, i) in shown\""), "the body iterates the filtered entries");
  assert.ok(sfc.includes("{{ shown.length }} of {{ lines.length }} lines"), "the meta line carries filtered/total");
  assert.ok(sfc.includes("no lines match the severity filter"), "the all-off state needs its own copy");
});

test("LogsList keeps the follow law on the filtered view", () => {
  assert.ok(
    sfc.includes("watch(shown, follow, { flush: \"post\" })"),
    "follow must watch the filtered view, so a toggle re-runs the stick-to-bottom law",
  );
});
