// tool-call grouping: a manual summary click pins the group's state for the session.

import type { TranscriptItem, ToolItem } from "./omega";
import type { ToolDisplay } from "./toolDisplay";

export interface ToolRunGroup {
  toolgroup: true;
  items: ToolItem[];
}

export type ChatEntry = TranscriptItem | ToolRunGroup;

export function isToolRun(e: ChatEntry): e is ToolRunGroup {
  return (e as ToolRunGroup).toolgroup === true;
}

// groupToolRuns folds CONSECUTIVE tool rows into groups. A run of ONE stays in flow, any
// other item kind breaks the run, and a row that anchors a variant is NEVER swallowed (the
// switcher must stay visible), so it splits its run into short, flat ones. Pure.
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

// foldAddress is a tool row's DURABLE address for its manual fold pin: the identity ladder
// the child key rides, with NO slot floor. The pin must survive a remount, and a slot is
// stable only within ONE payload/instance, so a slot pin could close or open a call the
// reader never touched. undefined = not pinnable (an instance-local pin). Pure.
export function foldAddress(item: { callId?: string; id?: string }): string | undefined {
  if (item.callId) return `call:${item.callId}`;
  if (item.id) return `row:${item.id}`;
  return undefined;
}

// toolChildKey is the v-for key for a group's CHILD rows. The container is keyed by its head
// item, so membership can shift under it (a variant split, a paint re-serving an older head)
// and an index key would hand one call's ToolCallLine instance - its fold pin, its running
// meter - to a DIFFERENT call. Key on the row's own identity: the tool call id, else the
// store row id, else the slot (the honest floor, prefixed so it cannot collide with a call
// id). The ladder IS foldAddress; the slot is the key's floor, never a fold pin's. Pure.
export function toolChildKey(item: ToolItem, index: number): string {
  return foldAddress(item) ?? `slot:${index}`;
}

// toolGroupWantOpen: the pure mode+state -> open mapping for a group the user has NOT pinned.
export function toolGroupWantOpen(
  anyLive: boolean,
  live: boolean,
  mode: ToolDisplay,
): boolean {
  if (mode === "expanded") return true;
  if (mode === "collapsed") return false;
  return anyLive || live;
}
