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
  invoke(panel: CanvasPanel, action: CanvasPanelAction): Promise<void>;
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
      if (p) next.push(p);
    }
    panels.splice(0, panels.length, ...next);
  }

  function applyDelta(raw: unknown) {
    const p = normalizeCanvasPanel(raw);
    if (!p) return;
    const i = panels.findIndex((x) => x.id === p.id);
    if (i >= 0) panels[i] = p;
    else panels.push(p);
  }

  async function invoke(panel: CanvasPanel, action: CanvasPanelAction): Promise<void> {
    const st = actionState(panel.id, action.id);
    st.status = "pending";
    st.error = undefined;
    try {
      await postCanvasAction(action.module, action.id);
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