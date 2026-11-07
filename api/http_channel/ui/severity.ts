// omega webui: severity classification for the logs tail (card
// t_221dc498). The omega log is a flat text file whose severity rides
// IN-BAND as a marker right after the log.LstdFlags timestamp -
// harness/logging's Errorf writes "ERROR: ", Warnf "WARN: " (the
// convention's Go-side pin: TestFileLoggerWarnf). This file is the
// UI-side half of that contract: pure, import-safe for the node suite
// (the narrow.ts recipe), no DOM code.
//
// Anything unmarked is info: the logger's Printf, the banner, panics
// and any foreign line that lands in the file. A marker found mid-line
// (a message QUOTING "ERROR: ...") never classifies - only the prefix
// right after the timestamp (or line start) does.

/** the three severities the logs tab filters on */
export type LogSeverity = "info" | "warning" | "error";

/** every severity, in chip order (info first - the stream's bulk) */
export const LOG_SEVERITIES: readonly LogSeverity[] = [
  "info",
  "warning",
  "error",
] as const;

/** log.LstdFlags writes "2006/01/02 15:04:05 " before every entry */
const TIMESTAMP = /^\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2} /;

/**
 * classifyLogLine reads one raw log line's severity. The timestamp is
 * optional: a line shaped like a marker with no timestamp (a foreign
 * writer) still classifies; everything else is info.
 */
export function classifyLogLine(line: string): LogSeverity {
  const body = TIMESTAMP.test(line) ? line.replace(TIMESTAMP, "") : line;
  if (body.startsWith("ERROR:")) return "error";
  if (body.startsWith("WARN:") || body.startsWith("WARNING:")) return "warning";
  return "info";
}

/**
 * filterLogLines pairs every line with its severity and keeps the
 * enabled ones - the tab's whole filter, pure so the node suite can
 * pin it end to end.
 */
export function filterLogLines(
  lines: readonly string[],
  enabled: Record<LogSeverity, boolean>,
): Array<{ text: string; severity: LogSeverity }> {
  return lines
    .map((text) => ({ text, severity: classifyLogLine(text) }))
    .filter((entry) => enabled[entry.severity]);
}
