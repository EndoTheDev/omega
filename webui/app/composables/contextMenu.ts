// omega webui: THE context menu - one store plus one floating surface (ContextMenu.vue).
// Every chrome surface registers its item list here; browser-rendered content (chat text,
// file previews, links, images) keeps the browser's own menu.
//
// Flat items with separators only: no submenus, no icons.

import { computed, ref, shallowRef } from "vue";

export interface ContextMenuItem {
  /** stable id ("tab:close") - the list key, never shown */
  id: string;
  label: string;
  /** the chord that would do the same thing; text only, never a binding */
  shortcut?: string;
  /**
   * a row that mirrors a persisted toggle (the statusbar's stat set): it carries the
   * state it flips, so the menu reads as a switch, never as a one-way verb.
   */
  checked?: boolean;
  /** destructive rows wear the error color */
  danger?: boolean;
  disabled?: boolean;
  run: () => void | Promise<void>;
}

export interface ContextMenuSeparator {
  kind: "separator";
}

export type ContextMenuEntry = ContextMenuItem | ContextMenuSeparator;

export function isSeparator(entry: ContextMenuEntry): entry is ContextMenuSeparator {
  return (entry as ContextMenuSeparator).kind === "separator";
}

export interface MenuCursor {
  x: number;
  y: number;
}

export interface MenuSize {
  w: number;
  h: number;
}

export interface MenuViewport {
  w: number;
  h: number;
}

export interface MenuPlacement {
  left: number;
  top: number;
  flippedX: boolean;
  flippedY: boolean;
}

/** the breathing room a menu keeps from every viewport edge */
export const CONTEXT_MENU_MARGIN_PX = 4;

function clampNumber(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

/**
 * Open at the cursor; FLIP when the box would run past the right or bottom edge (the menu grows
 * up/left from the pointer), then CLAMP so a menu larger than the viewport still starts at the
 * margin instead of running off it.
 */
export function placeContextMenu(
  cursor: MenuCursor,
  size: MenuSize,
  viewport: MenuViewport,
  margin = CONTEXT_MENU_MARGIN_PX,
): MenuPlacement {
  const flippedX = cursor.x + size.w > viewport.w - margin;
  const flippedY = cursor.y + size.h > viewport.h - margin;
  return {
    left: clampNumber(
      flippedX ? cursor.x - size.w : cursor.x,
      margin,
      Math.max(margin, viewport.w - size.w - margin),
    ),
    top: clampNumber(
      flippedY ? cursor.y - size.h : cursor.y,
      margin,
      Math.max(margin, viewport.h - size.h - margin),
    ),
    flippedX,
    flippedY,
  };
}

/** the rows a keyboard can land on: actions that can actually fire */
export function actionIndexes(entries: readonly ContextMenuEntry[]): number[] {
  const rows: number[] = [];
  entries.forEach((entry, i) => {
    if (!isSeparator(entry) && !entry.disabled) rows.push(i);
  });
  return rows;
}

/** THE EMPTY LIST LAW: no fireable row means no menu at all. Separators are spacing, not verbs. */
export function menuHasActions(entries: readonly ContextMenuEntry[]): boolean {
  return entries.some((entry) => !isSeparator(entry) && !entry.disabled);
}

/** drop the edges and collapse runs: a menu never opens on a rule */
export function normalizeMenuEntries(
  entries: readonly ContextMenuEntry[],
): ContextMenuEntry[] {
  const out: ContextMenuEntry[] = [];
  for (const entry of entries) {
    if (!isSeparator(entry)) {
      out.push(entry);
      continue;
    }
    if (out.length === 0 || isSeparator(out[out.length - 1]!)) continue;
    out.push(entry);
  }
  while (out.length > 0 && isSeparator(out[out.length - 1]!)) out.pop();
  return out;
}

/** the next fireable row from `active`, wrapping; -1 when the list has none */
export function stepActiveIndex(
  entries: readonly ContextMenuEntry[],
  active: number,
  delta: 1 | -1,
): number {
  const rows = actionIndexes(entries);
  if (rows.length === 0) return -1;
  const at = rows.indexOf(active);
  if (at === -1) return delta > 0 ? rows[0]! : rows[rows.length - 1]!;
  return rows[(at + delta + rows.length) % rows.length]!;
}

// THE SINGLETON: one menu for the whole window. A surface never keeps its own open state.
const open = ref(false);
const entries = shallowRef<readonly ContextMenuEntry[]>([]);
const cursor = ref<MenuCursor>({ x: 0, y: 0 });
const size = ref<MenuSize>({ w: 0, h: 0 });
const measured = ref(false);
const active = ref(-1);

export const contextMenuOpen = open;
export const contextMenuEntries = entries;
export const contextMenuActive = active;
export const contextMenuMeasured = measured;

function viewportOf(): MenuViewport {
  if (typeof window === "undefined") return { w: 0, h: 0 };
  return { w: window.innerWidth, h: window.innerHeight };
}

export const contextMenuPlacement = computed<MenuPlacement | null>(() =>
  open.value ? placeContextMenu(cursor.value, size.value, viewportOf()) : null,
);

/** the measured box; the component calls it once the surface has a layout */
export function setContextMenuSize(w: number, h: number): void {
  size.value = { w, h };
  measured.value = true;
}

export function setContextMenuActive(index: number): void {
  if (!open.value) return;
  const entry = entries.value[index];
  if (!entry || isSeparator(entry) || entry.disabled) return;
  active.value = index;
}

export function closeContextMenu(): void {
  open.value = false;
  entries.value = [];
  cursor.value = { x: 0, y: 0 };
  size.value = { w: 0, h: 0 };
  measured.value = false;
  active.value = -1;
}

/** returns false when the list carries nothing to fire - the right-click did nothing */
export function openContextMenu(
  x: number,
  y: number,
  items: readonly ContextMenuEntry[],
): boolean {
  const clean = normalizeMenuEntries(items);
  if (!menuHasActions(clean)) {
    closeContextMenu();
    return false;
  }
  entries.value = clean;
  cursor.value = { x, y };
  size.value = { w: 0, h: 0 };
  measured.value = false;
  active.value = actionIndexes(clean)[0]!;
  open.value = true;
  return true;
}

/** fire one row; closes FIRST so a verb that opens a dialog never lands under the menu */
export function runContextMenuItem(index: number): boolean {
  if (!open.value) return false;
  const entry = entries.value[index];
  if (!entry || isSeparator(entry) || entry.disabled) return false;
  closeContextMenu();
  void entry.run();
  return true;
}

/** the menu's whole key grammar: escape, arrows, enter. Returns whether it consumed the key. */
export function handleContextMenuKey(key: string): boolean {
  if (!open.value) return false;
  switch (key) {
    case "Escape":
      closeContextMenu();
      return true;
    case "ArrowDown":
      active.value = stepActiveIndex(entries.value, active.value, 1);
      return true;
    case "ArrowUp":
      active.value = stepActiveIndex(entries.value, active.value, -1);
      return true;
    case "Enter":
      return runContextMenuItem(active.value);
    default:
      return false;
  }
}
