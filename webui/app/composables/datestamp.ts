// omega webui: the day stamp's display mirror.
//
// The stamp rides the user message's CONTENT on the wire (agent/datestamp.go): the date
// is append-only history, never a system-prompt value, so a midnight roll does not cost
// every session its warm prefix cache. The bubble therefore strips it and the transcript
// draws it as the day divider - the wire keeps the bytes either way.

const STAMP_DATE_LEN = 10;
// the shortest stamped head: "[YYYY-MM-DD]"
const STAMP_PREFIX_LEN = STAMP_DATE_LEN + 2;
const ISO_DAY = /^\d{4}-\d{2}-\d{2}$/;

// realDay rejects a layout-shaped day the calendar does not have (2026-02-30, month 13).
function realDay(day: string): boolean {
  if (!ISO_DAY.test(day)) return false;
  const year = Number(day.slice(0, 4));
  const month = Number(day.slice(5, 7));
  const date = Number(day.slice(8, 10));
  if (month < 1 || month > 12 || date < 1) return false;
  const leap = (year % 4 === 0 && year % 100 !== 0) || year % 400 === 0;
  const last = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1]!;
  return date <= last;
}

// userDayStamp returns the ISO day a user message carries at its head, or null. The head
// must be "[" + a real date + "]"; everything after the "]" is free. Mirrors the wire
// reader (agent/datestamp.go: userDateStamp).
export function userDayStamp(content: string): string | null {
  if (content.length < STAMP_PREFIX_LEN || content[0] !== "[") return null;
  const day = content.slice(1, 1 + STAMP_DATE_LEN);
  if (!realDay(day)) return null;
  if (content[1 + STAMP_DATE_LEN] !== "]") return null;
  return day;
}

// stripDayStamp returns the text the bubble shows: the content after the stamp's "]",
// minus one separator space (stampMessage writes exactly one). An unstamped content
// comes back byte-identical.
export function stripDayStamp(content: string): string {
  if (userDayStamp(content) === null) return content;
  const rest = content.slice(STAMP_PREFIX_LEN);
  return rest.startsWith(" ") ? rest.slice(1) : rest;
}
