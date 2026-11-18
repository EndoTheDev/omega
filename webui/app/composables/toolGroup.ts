// omega webui: tool-call grouping (card t_e4280de7). Consecutive tool
// call rows (a call + its result merges to one row; several calls in a
// row with no prose between) collapse into ONE collapsible group block
// - the run's vertical noise reduction. The group rides the SAME
// display mode the rows do (omega.tool-display: auto | expanded |
// collapsed, composables/toolDisplay.ts): auto keeps the group open
// while the run is live or any child row is running, collapsed shuts
// it, expanded forces it open. A manual summary click pins the group's
// state for the session, exactly like a tool row's pin (card
// t_868633b3). Pure logic here; the render side is ToolGroup.vue.

import type { TranscriptItem, ToolItem } from "./omega";
import type { ToolDisplay } from "./toolDisplay";

export interface ToolRunGroup {
  toolgroup: true;
  items: ToolItem[];
}

// ChatEntry: one transcript render unit - a single item in flow, or a
// collapsible group swallowing a consecutive tool run.
export type ChatEntry = TranscriptItem | ToolRunGroup;

// isToolRun: the template-side narrowing (an `in` check is the honest
// union discriminator Vue's compiler narrows too).
export function isToolRun(e: ChatEntry): e is ToolRunGroup {
  return (e as ToolRunGroup).toolgroup === true;
}

// groupToolRuns folds consecutive kind:"tool" items into groups.
// - a run of ONE row stays in flow (nothing to collapse);
// - any other item kind (message, thinking, verdict) breaks the run:
//   only strictly consecutive tool renders group;
// - a tool row that anchors a variant (t_0df4efd8 multi-row replay
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
    // hasVariant may flip between renders (the branch list loads after
    // the transcript) - recompute means a group can split apart; the
    // entry keys ride the ITEM identities, so components keep their
    // state through the split.
    if (item.kind === "tool" && !hasVariant(item)) {
      run.push(item);
      continue;
    }
    flush();
    // the non-tool item (or the variant-anchored row) renders in flow
    out.push(item);
  }
  flush();
  return out;
}

// foldAddress is a tool row's DURABLE address for its manual fold pin
// (card t_15572849): the same identity ladder the child key rides, with
// NO slot floor. The fold pin is the READER's state and it now lives on
// the tab's view record so a dock-tab switch (which unmounts the whole
// pane) cannot drop it - which means the address must survive a remount
// too. A slot is stable only within ONE payload/instance: after a
// remount the same slot renders a different row, so a slot pin would
// close/open a call the reader never touched (the F1 mis-assignment, one
// level up). undefined = the row is not pinnable; it keeps an
// instance-local pin, exactly like before. Pure.
export function foldAddress(item: { callId?: string; id?: string }): string | undefined {
  if (item.callId) return `call:${item.callId}`;
  if (item.id) return `row:${item.id}`;
  return undefined;
}

// toolChildKey: the v-for key for a group's CHILD rows (card
// t_64212867). The container is keyed by its head item, so a container
// can survive while its membership shifts - a variant anchor split, or
// a paint that re-serves an older head object. An index key then hands
// one call's ToolCallLine instance (its fold pin, its running meter) to
// a DIFFERENT call: the F1 mis-assignment one level down. Key on the
// call's own identity instead:
// - the tool call id (the chips join key and the canvas cache key - a
//   row that survives a paint keeps this one);
// - else the store row id (a live canvas row always carries one,
//   t_0df4efd8; the REST restore path carries call ids only);
// - else the slot. An item with NO identity has nothing stable to key
//   on, so the slot is the honest floor - and the prefixes keep that
//   floor from ever colliding with a real call id. The ladder IS
//   foldAddress (one spelling of the durable half); the slot is only
//   the key's floor, never a fold pin's. Pure.
export function toolChildKey(item: ToolItem, index: number): string {
  return foldAddress(item) ?? `slot:${index}`;
}

// toolGroupWantOpen: the pure mode+state -> open mapping for a group
// the user has NOT pinned. Auto keeps the group open while the run is
// live (calls still streaming in - closing per call would yoyo) or any
// child row is live (composables/toolStatus.ts: running, or pending its
// out-of-band result - async agent loop phase 3, the chip stays visible
// until its wake fills it); a settled group collapses. Expanded/collapsed
// force one state, like toolRowWantOpen. Pure.
export function toolGroupWantOpen(
  anyLive: boolean,
  live: boolean,
  mode: ToolDisplay,
): boolean {
  if (mode === "expanded") return true;
  if (mode === "collapsed") return false;
  return anyLive || live;
}
