// omega webui: the canvas store (arc 3c) - the webui's one view-model
// state. Load = the snapshot (GET /ui/canvas via fetchCanvas); deltas
// after (view_model_delta frames ride the run streams through
// StreamHandlers.onViewModelDelta, forwarded by the chat host). PANEL
// PRESENCE LIVES HERE, never in a hardcoded list (settled Q5a): the
// dock and the dockables registry stay layout concerns - whatever the
// canvas lists is what renders, in the plain v1 render (title + JSON
// payload + actions, settled Q2b; rich kinds arrive with phase 4+
// migrations). Action calls go through the one action pipe
// (postCanvasAction, module-qualified, settled Q4b) with optimistic
// pending state and an error state on 4xx/5xx.
//
// Pure-ish by the house test law: the production shapes this store
// consumes are the wire shapes the server sends (the arc 3b
// serialization), so the tests bind the same paths production runs -
// no ref unwrapping, no mock stores.

import { reactive } from "vue";
import {
  fetchCanvas,
  normalizeCanvasPanel,
  postCanvasAction,
  type CanvasPanel,
  type CanvasPanelAction,
} from "./omega.ts";

// action states live per panel+action: optimistically pending at invoke
// time, error state carries the server's reason on 4xx/5xx, back to
// idle on success. The panel re-renders from the deltas; this state is
// only the button's honesty.
export interface CanvasActionState {
  status: "idle" | "pending" | "error";
  error?: string;
}

export interface CanvasStore {
  /** snapshot order preserved; deltas replace in place */
  panels: CanvasPanel[];
  /** load-time catch-up: replaces the whole list with the snapshot */
  applySnapshot(list: unknown): void;
  /** one delta frame: replace-in-place, unknown ids append */
  applyDelta(raw: unknown): void;
  actionState(panelId: string, actionId: string): CanvasActionState;
  /** the action pipe round-trip: optimistic pending, error state on
      4xx/5xx, idle again on success (the panel updates via deltas) */
  invoke(panel: CanvasPanel, action: CanvasPanelAction, args?: unknown): Promise<void>;
  /** refetch the snapshot (boot + later catch-ups). A failed fetch
      keeps the previous view - the same transient rule as sessions. */
  refresh(): Promise<void>;
}

// formatCanvasPayload is the v1 plain render's payload view:
// pretty-printed JSON, or the no-payload marker for null. Pure.
export function formatCanvasPayload(payload: unknown): string {
  if (payload === null) return "(no payload)";
  try {
    return JSON.stringify(payload, null, 2) ?? "(no payload)";
  } catch {
    return "(unrenderable payload)";
  }
}

// toastRowOf is the toast panel's body (card t_aca53fee): kind "toast"
// panels carry ONE record row as the payload (the arc 4b wire shape the
// notifications capsule sends - id/kind/source_id/title/verdict/
// created_at/read). The renderer must show that record read-only, like
// one list-panel line (kind + title fields, width-truncated) - the raw
// JSON dump is debug output, not a panel body. The toast itself still
// pops transiently through the bell's stack; only the persistent
// canvas-panel view changes. Pure: returns the payload object (rendered
// through the row pipeline) or null when there is nothing row-shaped to
// show (every other kind, a junk payload, an empty seeded toast).
export function toastRowOf(kind: string, payload: unknown): Record<string, unknown> | null {
  if (kind !== "toast" || !payload || typeof payload !== "object" || Array.isArray(payload)) {
    return null;
  }
  const p = payload as Record<string, unknown>;
  if (typeof p.id === "undefined" && typeof p.kind === "undefined" && typeof p.title === "undefined") {
    return null;
  }
  return p;
}

// createCanvasStore holds the app's canvas. One per page - panels are
// global state (settled Q10a: panel state is not per-session).
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
    // non-array body = malformed: the previous view survives (the same
    // transient rule as refresh's failed fetch - a bad snapshot never
    // blanks the panels)
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
      // snapshot fetch failure: keep the previous view (the deltas
      // still apply on top of it when a stream is live)
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

// --- the toast feed (arc 4b, t_9339ad05) ---------------------------------

// Panels of kind "toast" are canvas events, not persistent surface (the
// notifications capsule is the first module to ship one). Both transports
// land here: deltas ride the run streams, the snapshot is the catch-up
// for emits that happened between runs. The fingerprint dedupes - the
// same state seen twice (a reconnect replay, a snapshot refetch of an
// unchanged store) toasts once. Handlers receive the payload verbatim;
// decoding is the consumers' domain (the dock knows the record shape,
// the canvas contract does not).
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

// feedToastPanel is the internal funnel: applyDelta and applySnapshot
// call it for every panel; only kind "toast" payloads pass.
function feedToastPanel(p: CanvasPanel): void {
  if (p.kind !== "toast" || p.payload === null || p.payload === undefined) return;
  const fp = fingerprintOf(p.payload);
  if (fp === lastToastFingerprint) return;
  lastToastFingerprint = fp;
  for (const fn of toastHandlers) fn(p.payload);
}

// onToastPanel subscribes one toast consumer (the dock's toast stack).
// The return releases the subscription - the handler set is a feed,
// not a store; the panels list above stays the one state.
export function onToastPanel(fn: ToastPanelHandler): () => void {
  toastHandlers.add(fn);
  return () => {
    toastHandlers.delete(fn);
  };
}

// emitLocalToast publishes a CLIENT-SIDE toast through the same funnel
// the canvas feed uses: one subscriber set, one fingerprint dedupe (the
// same payload toasts once). Local-only by law (card t_d54c718a): no
// transport, no panel kind - the boot repair receipt is the first
// emitter, nothing listens to a private channel for it.
export function emitLocalToast(payload: unknown): void {
  if (payload === null || payload === undefined) return;
  const fp = fingerprintOf(payload);
  if (fp === lastToastFingerprint) return;
  lastToastFingerprint = fp;
  for (const fn of toastHandlers) fn(payload);
}