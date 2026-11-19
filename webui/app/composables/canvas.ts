// omega webui: the canvas store (arc 3c) - the webui's one view-model state. PANEL
// PRESENCE LIVES HERE, never in a hardcoded list (settled Q5a). The canvas CONTRACT
// (snapshot 200, delta frames, the action pipe with optimistic pending/error, v1
// descriptor schema) is the tabbed renderer AND the transcript's shared source
// (chat-last 2a) and the toast funnel's feed (arc 4b) - v0.14.3 folded the standalone
// host tab, and moved the module surface into settings; the contract itself stays.

import { reactive } from "vue";
import {
  fetchCanvas,
  normalizeCanvasPanel,
  postCanvasAction,
  type CanvasPanel,
  type CanvasPanelAction,
} from "./omega.ts";

// action states live per panel+action.
export interface CanvasActionState {
  status: "idle" | "pending" | "error";
  error?: string;
}

export interface CanvasStore {
  panels: CanvasPanel[];
  applySnapshot(list: unknown): void;
  applyDelta(raw: unknown): void;
  actionState(panelId: string, actionId: string): CanvasActionState;
  invoke(panel: CanvasPanel, action: CanvasPanelAction, args?: unknown): Promise<void>;
  refresh(): Promise<void>;
}

// formatCanvasPayload and toastRowOf left with the canvas host tab.

// createCanvasStore holds the app's canvas.
export function createCanvasStore(): CanvasStore {
  const panels = reactive<CanvasPanel[]>([]);
  // the action states live in a reactive Map so a pending/error flip
  // re-renders exactly the invoking button's label + the error line
  const states = reactive(new Map<string, CanvasActionState>());
  const key = (panelId: string, actionId: string) => `${panelId}\u0000${actionId}`;

  function actionState(panelId: string, actionId: string): CanvasActionState {
    const k = key(panelId, actionId);
    let st = states.get(k);
    if (!st) {
      st = { status: "idle" };
      states.set(k, st);
    }
    return st;
  }

  function applySnapshot(list: unknown) {
    // non-array body = malformed: the previous view survives (the same transient rule
    // as refresh's failed fetch - a bad snapshot never blanks the panels)
    if (!Array.isArray(list)) return;
    const next: CanvasPanel[] = [];
    for (const item of list) {
      const p = normalizeCanvasPanel(item);
      if (p) {
        next.push(p);
        feedToastPanel(p);
      }
    }
    panels.splice(0, panels.length, ...next);
  }

  function applyDelta(raw: unknown) {
    const p = normalizeCanvasPanel(raw);
    if (!p) return;
    const i = panels.findIndex((x) => x.id === p.id);
    if (i >= 0) panels[i] = p;
    else panels.push(p);
    feedToastPanel(p);
    announceDelta(p);
  }

  async function invoke(panel: CanvasPanel, action: CanvasPanelAction, args?: unknown): Promise<void> {
    const st = actionState(panel.id, action.id);
    st.status = "pending";
    st.error = undefined;
    try {
      await postCanvasAction(action.module, action.id, args);
      st.status = "idle";
    } catch (e) {
      st.status = "error";
      st.error = e instanceof Error ? e.message : String(e);
    }
  }

  async function refresh(): Promise<void> {
    try {
      applySnapshot(await fetchCanvas());
    } catch {
    }
  }

  return {
    panels,
    applySnapshot,
    applyDelta,
    actionState,
    invoke,
    refresh,
  };
}

// the module-UI delta seam ----------------------

// A module view that renders beyond a panel's view-model (the kanban board. Module UI
// imports no shell code, so the event literal is duplicated on both sides ON PURPOSE -
// the reverse direction is `omega-select-board` (composables/notifications.ts ->
// harness/kanban/ui/Board.vue), the same discipline.
export const CANVAS_DELTA_EVENT = "omega-canvas-delta";

function announceDelta(p: CanvasPanel): void {
  if (typeof window === "undefined") return; // node tests: no page to tell
  window.dispatchEvent(new CustomEvent(CANVAS_DELTA_EVENT, { detail: p }));
}

// CANVAS_RESET_ID is the reset detail's id: the same event, one more emitter. Boot
// never fires it: a cold page load mounts its views, which fetch themselves - a boot
// pulse would only double every read. Consumers match the id (the literal is duplicated
// on their side, same discipline as CANVAS_DELTA_EVENT); `detail.panel` is absent
// because a reset is about the page, not one panel.
export const CANVAS_RESET_ID = "reconnect";

export function announceCanvasReset(): void {
  if (typeof window === "undefined") return; // node tests: no page to tell
  window.dispatchEvent(
    new CustomEvent(CANVAS_DELTA_EVENT, { detail: { id: CANVAS_RESET_ID } }),
  );
}

// the toast feed ------------------------------

// Panels of kind "toast" are canvas events, not persistent surface.
// Handlers receive the payload verbatim; decoding is the consumers' domain (the dock
// knows the record shape, the canvas contract does not).
type ToastPanelHandler = (payload: unknown) => void;

const toastHandlers = new Set<ToastPanelHandler>();
let lastToastFingerprint = "";

// fingerprintOf is the dedupe key: the payload's stable serialization.
// An unserializable payload degrades to the empty string and still
// toasts once per distinct string (a toast that never fires is worse).
function fingerprintOf(payload: unknown): string {
  try {
    return JSON.stringify(payload) ?? "";
  } catch {
    return "";
  }
}

// feedToastPanel is the internal funnel.
function feedToastPanel(p: CanvasPanel): void {
  if (p.kind !== "toast" || p.payload === null || p.payload === undefined) return;
  const fp = fingerprintOf(p.payload);
  if (fp === lastToastFingerprint) return;
  lastToastFingerprint = fp;
  for (const fn of toastHandlers) fn(p.payload);
}

// onToastPanel subscribes one toast consumer (the dock's toast stack).
export function onToastPanel(fn: ToastPanelHandler): () => void {
  toastHandlers.add(fn);
  return () => {
    toastHandlers.delete(fn);
  };
}

// emitLocalToast publishes a CLIENT-SIDE toast through the same funnel the canvas.
export function emitLocalToast(payload: unknown): void {
  if (payload === null || payload === undefined) return;
  const fp = fingerprintOf(payload);
  if (fp === lastToastFingerprint) return;
  lastToastFingerprint = fp;
  for (const fn of toastHandlers) fn(payload);
}