// omega webui: back/forward navigation history (card t_6215474b). The
// VS Code read: every tab activation and every pane focus records the
// (paneId, tabId) pair; ArrowLeft walks BACK through the visited tabs,
// ArrowRight re-advances. NOT scroll positions, NOT content state - the
// entry is the tab reference only, and back/forward ACTIVATES it the
// same way a click would (one path for buttons and keys).
//
// PURE model below (plain arrays + index, node-tested), reactive shell
// on top (the cluster's dimmed states read canBack/canForward). The
// activator decides whether an entry still resolves: a pane or tab that
// closed meanwhile is dead - back/forward walks past it, and an entry
// that never activates dimms nothing.

import { computed, ref } from "vue";
import { findPane, type DockNode, type DockTab } from "./dock.ts";

/** one history entry: the (paneId, tabId) pair - the RULING's shape
    (2026-11-02): tab activation + pane focus, nothing else */
export interface NavEntry {
  paneId: string;
  /** the tab's within-pane identity (navTabId) - key for chat, the
      viewer ref for viewer tabs, the component id for singletons */
  tabId: string;
}

/** stack cap (ponytail): avoids unbounded growth on a click-heavy
    session; raise if ever needed */
export const NAV_HISTORY_CAP = 100;

/** a tab's within-pane identity: chat mints a stable `key` at creation,
    viewer tabs identify by their payload, every singleton is unique per
    pane by component id. Plain data over indices - indices move on
    close/reorder, identities survive both. */
export function navTabId(tab: DockTab): string {
  if (tab.key) return tab.key;
  if (tab.viewer) return `viewer:${tab.viewer.kind}:${tab.viewer.name}`;
  return tab.component;
}

export function navEntriesEqual(a: NavEntry, b: NavEntry): boolean {
  return a.paneId === b.paneId && a.tabId === b.tabId;
}

/** the push model (pure): dedupe the CURRENT entry (a re-click on an
    already-active tab pushes nothing), then splice in place - the
    forward branch discards (browser semantics), the cap drops the
    OLDEST. Returns the next { entries, index }. */
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

/** back = previous entry. null at the stack bottom. */
export function navBack(entries: readonly NavEntry[], index: number): number | null {
  return index > 0 ? index - 1 : null;
}

/** forward = re-advance. null at the stack top. */
export function navForward(entries: readonly NavEntry[], index: number): number | null {
  return index < entries.length - 1 && index >= 0 ? index + 1 : null;
}

/** where does an entry live NOW: the tab's index in its pane, or null
    when the pane or the tab died since the entry was recorded. */
export function locateEntry(tree: DockNode, e: NavEntry): number | null {
  const pane = findPane(tree, e.paneId);
  if (!pane) return null;
  const i = pane.tabs.findIndex((t) => navTabId(t) === e.tabId);
  return i >= 0 ? i : null;
}

/** the reactive shell the app binds. `activate` performs the focus
    (the raw dock focus - the ONE navigation path for buttons, keys
    and the palette); it returns false when the entry no longer
    resolves, and back/forward keeps walking past dead entries. */
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
      // dead entry: cut it out of the middle - the current entry slides
      // into its slot (index rides it) and the walk continues
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
      // dead entry: cut it out of the middle, keep the walk going
      entries.value = entries.value.filter((x, i) => i !== target);
    }
  }

  return { entries, index, canBack, canForward, push, back, forward };
}