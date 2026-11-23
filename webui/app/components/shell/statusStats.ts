// omega webui: the statusbar's STAT SET - the one catalog the bar renders and its
// right-click menu toggles. Visibility is a persisted layout fact (dock.ts's record), so
// the bar and its menu can never disagree about what is on it.

import type { ContextMenuEntry } from "../../composables/contextMenu.ts";

export interface StatusStat {
  /** stable id; the persisted hidden set holds these */
  id: string;
  /** the menu row's name - the segment's own word, never the rendered figure */
  label: string;
}

export const STATUS_STATS: readonly StatusStat[] = [
  { id: "pending", label: "Pending calls" },
  { id: "tokens", label: "Tokens" },
  { id: "cache", label: "Prompt cache" },
  { id: "context", label: "Context window" },
  { id: "throughput", label: "Throughput" },
  { id: "speed", label: "Generation speed" },
  { id: "messages", label: "Message count" },
  { id: "version", label: "Build version" },
];

/** a stat shows unless the persisted hidden set names it */
export function statVisible(hidden: readonly string[], id: string): boolean {
  return !hidden.includes(id);
}

/** the persisted reader: junk entries drop, never throw; unknown ids are kept (a later
    build may add one back) and simply match no stat in this build */
export function parseHiddenStats(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  const out: string[] = [];
  for (const entry of raw) {
    if (typeof entry === "string" && entry !== "" && !out.includes(entry)) out.push(entry);
  }
  return out;
}

/**
 * THE STAT MENU: every stat with its check state, so the menu is the bar's own inventory.
 * `checked` IS visibility (a check means the segment is on the bar); a click flips it.
 *
 * Reset is the last row and the one write that clears the WHOLE hidden set - it un-toggles
 * every hidden id through the same toggle writer (unknown ids included: the set is the
 * layout record's, not this build's catalog), so every stat shows again.
 */
export function statMenuItems(
  hidden: readonly string[],
  toggle: (id: string, hidden: boolean) => void,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = STATUS_STATS.map((stat) => {
    const visible = statVisible(hidden, stat.id);
    return {
      id: "stat:" + stat.id,
      label: stat.label,
      checked: visible,
      run: () => toggle(stat.id, visible),
    };
  });
  items.push({ kind: "separator" });
  items.push({
    id: "stat:reset",
    label: "Reset statusbar",
    // nothing hidden = nothing to reset: the row stays in the inventory, dimmed
    disabled: hidden.length === 0,
    run: () => {
      for (const id of hidden) toggle(id, false);
    },
  });
  return items;
}
