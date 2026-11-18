// omega webui: tool-call grouping. A manual summary click pins the group's state for
// the session, exactly like a tool row's pin.

import type { TranscriptItem, ToolItem } from "./omega";
import type { ToolDisplay } from "./toolDisplay";

export interface ToolRunGroup {
  toolgroup: true;
  items: ToolItem[];
}

// ChatEntry: one transcript render unit.
export type ChatEntry = TranscriptItem | ToolRunGroup;

// isToolRun: the template-side narrowing (an `in` check is the honest union.
export function isToolRun(e: ChatEntry): e is ToolRunGroup {
  return (e as ToolRunGroup).toolgroup === true;
}

// groupToolRuns folds consecutive kind:"tool" items into groups.
// a run of ONE row stays in flow (nothing to collapse);
// any other item kind (message, thinking, verdict) breaks the run:
//   only strictly consecutive tool renders group;
// a tool row that anchors a variant ( multi-row replay
//   heads) is NEVER swallowed: the switcher must stay visible in flow,
//   so hasVariant splits its run - rows around it flush as short runs
//   (and short runs stay flat). Pure.
export function groupToolRuns(
  items: readonly TranscriptItem[],
  hasVariant: (item: ToolItem) => boolean = () => false,
): ChatEntry[] {
  const out: ChatEntry[] = [];
  let run: ToolItem[] = [];

  const flush = () => {
    if (run.length >= 2) out.push({ toolgroup: true, items: run });
    else out.push(...run);
    run = [];
  };

  for (const item of items) {
    // hasVariant may flip between renders (the branch list loads after the transcript).
    if (item.kind === "tool" && !hasVariant(item)) {
      run.push(item);
      continue;
    }
    flush();
    out.push(item);
  }
  flush();
  return out;
}

// toolChildKey: the v-for key for a group's CHILD rows
// ). The container is keyed by its head item, so a container
// can survive while its membership shifts - a variant anchor split, or
// a paint that re-serves an older head object. An index key then hands
// one call's ToolCallLine instance (its fold pin, its running meter) to
// a DIFFERENT call: the F1 mis-assignment one level down. Key on the
// call's own identity instead:
// the tool call id (the chips join key and the canvas cache key - a
//   row that survives a paint keeps this one);
// else the store row id (a live canvas row always carries one,
// the REST restore path carries call ids only);
// else the slot. An item with NO identity has nothing stable to key
//   on, so the slot is the honest floor - and the prefixes keep that
//   floor from ever colliding with a real call id. Pure.
export function toolChildKey(item: ToolItem, index: number): string {
  if (item.callId) return `call:${item.callId}`;
  if (item.id) return `row:${item.id}`;
  return `slot:${index}`;
}

// toolGroupWantOpen: the pure mode+state -> open mapping for a group the user has NOT.
export function toolGroupWantOpen(
  anyLive: boolean,
  live: boolean,
  mode: ToolDisplay,
): boolean {
  if (mode === "expanded") return true;
  if (mode === "collapsed") return false;
  return anyLive || live;
}
