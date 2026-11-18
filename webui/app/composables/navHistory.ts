// omega webui: back/forward navigation history. The activator decides whether an entry
// still resolves: a pane or tab that closed meanwhile is dead - back/forward walks past
// it, and an entry that never activates dimms nothing.

import { computed, ref } from "vue";
import { findPane, type DockNode, type DockTab } from "./dock.ts";

export interface NavEntry {
  paneId: string;
  tabId: string;
}

/** stack cap (ponytail): avoids unbounded growth on a click-heavy session. */
export const NAV_HISTORY_CAP = 100;

/** a tab's within-pane identity. */
export function navTabId(tab: DockTab): string {
  if (tab.key) return tab.key;
  if (tab.viewer) return `viewer:${tab.viewer.kind}:${tab.viewer.name}`;
  return tab.component;
}

export function navEntriesEqual(a: NavEntry, b: NavEntry): boolean {
  return a.paneId === b.paneId && a.tabId === b.tabId;
}

/** the push model (pure): dedupe the CURRENT entry (a re-click on an already-active. */
export function navPush(
  entries: readonly NavEntry[],
  index: number,
  e: NavEntry,
  cap = NAV_HISTORY_CAP,
): { entries: NavEntry[]; index: number } {
  const cur = entries[index];
  if (cur && navEntriesEqual(cur, e)) return { entries: [...entries], index };
  const kept = entries.slice(0, index + 1);
  kept.push(e);
  if (kept.length > cap) kept.splice(0, kept.length - cap);
  return { entries: kept, index: kept.length - 1 };
}

/** back = previous entry. */
export function navBack(entries: readonly NavEntry[], index: number): number | null {
  return index > 0 ? index - 1 : null;
}

/** forward = re-advance. */
export function navForward(entries: readonly NavEntry[], index: number): number | null {
  return index < entries.length - 1 && index >= 0 ? index + 1 : null;
}

/** where does an entry live NOW. */
export function locateEntry(tree: DockNode, e: NavEntry): number | null {
  const pane = findPane(tree, e.paneId);
  if (!pane) return null;
  const i = pane.tabs.findIndex((t) => navTabId(t) === e.tabId);
  return i >= 0 ? i : null;
}

/** the reactive shell the app binds. */
export function createNavHistory(activate: (e: NavEntry) => boolean) {
  const entries = ref<NavEntry[]>([]);
  const index = ref(-1);
  const canBack = computed(() => index.value > 0);
  const canForward = computed(
    () => index.value >= 0 && index.value < entries.value.length - 1,
  );

  function push(e: NavEntry) {
    const next = navPush(entries.value, index.value, e);
    entries.value = next.entries;
    index.value = next.index;
  }

  function back() {
    while (index.value > 0) {
      const target = index.value - 1;
      const e = entries.value[target];
      if (activate(e)) {
        index.value = target;
        break;
      }
      entries.value = entries.value.filter((x, i) => i !== target);
      index.value -= 1;
    }
  }

  function forward() {
    while (canForward.value) {
      const target = index.value + 1;
      const e = entries.value[target];
      if (activate(e)) {
        index.value = target;
        break;
      }
      entries.value = entries.value.filter((x, i) => i !== target);
    }
  }

  return { entries, index, canBack, canForward, push, back, forward };
}