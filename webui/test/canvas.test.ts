// omega webui: unit tests for the canvas surface (arc 3c, card
// t_516d7494). Three layers, matching the house patterns:
// - composables/omega.ts canvas wrappers: GET /ui/canvas + POST
//   /ui/actions URL + body contract under fetch interception;
// - the canvas store: snapshot-render + delta-update + the action
//   pipe's optimistic pending / error states, driven through the same
//   production paths (no ref unwrapping - the test law);
// - handlePayload's view_model_delta dispatch: the exact wire frame
//   the arc 3b server serialization sends;
// - source pins: CanvasPanel.vue (the generic renderer contract), the
//   app.vue outlet + stream wiring, and the dockables registry entry
//   (layout only - panel presence must never hardcode here).
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  fetchCanvas,
  postCanvasAction,
  normalizeCanvasPanel,
  handlePayload,
  type CanvasPanel,
} from "../app/composables/omega.ts";
import {
  createCanvasStore,
  formatCanvasPayload,
  toastRowOf,
} from "../app/composables/canvas.ts";
import {
  DOCK_COMPONENTS,
  DOCKABLES,
  COMPONENT_TITLES,
  MENU_DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
} from "../app/composables/dockables.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "..", "..", "..");

function read(...parts: string[]): string {
  return readFileSync(join(modules, ...parts), "utf8");
}

const WIRE_PANEL = {
  id: "kanban.board",
  title: "Board",
  kind: "kanban-board",
  payload: { cards: 3 },
  actions: [{ id: "refresh", label: "Refresh", module: "kanban" }],
};

// json(): the Response shape production sends (Content-Type json).
function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

// --- wire wrappers: URL + body contract ---

test("fetchCanvas GETs /ui/canvas and normalizes the descriptor array", async () => {
  const seen: string[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL) => {
    seen.push(String(url));
    return Promise.resolve(json([WIRE_PANEL, { junk: true }, null, {
      id: "ghost.panel",
      title: "",
      kind: "",
      payload: undefined,
      actions: [{ id: "ping", module: "ghost" }, { nope: 1 }],
    }]));
  }) as typeof fetch;
  try {
    const panels = await fetchCanvas();
    assert.deepEqual(seen, ["/ui/canvas"]);
    assert.equal(panels.length, 2);
    assert.deepEqual(panels[0], {
      id: "kanban.board",
      title: "Board",
      kind: "kanban-board",
      payload: { cards: 3 },
      actions: [{ id: "refresh", label: "Refresh", module: "kanban" }],
    });
    // defaults: title falls back to the id, kind to the plain render,
    // label-less actions carry their id, junk action rows drop
    assert.deepEqual(panels[1], {
      id: "ghost.panel",
      title: "ghost.panel",
      kind: "default",
      payload: null,
      actions: [{ id: "ping", label: "ping", module: "ghost" }],
    });
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("fetchCanvas: nothing mounted is a state, not an error; non-arrays normalize empty", async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = (async () => json([])) as typeof fetch;
  try {
    assert.deepEqual(await fetchCanvas(), []);
  } finally {
    globalThis.fetch = realFetch;
  }
  globalThis.fetch = (async () => json(null)) as typeof fetch;
  try {
    assert.deepEqual(await fetchCanvas(), []);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("postCanvasAction POSTs the module-qualified call; 4xx/5xx throw the server's verbatim reason", async () => {
  const seen: { url: string; method: string; body: string }[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = (async (url: string | URL, init?: RequestInit) => {
    seen.push({
      url: String(url),
      method: init?.method ?? "",
      body: String(init?.body),
    });
    return json({ result: "ok" });
  }) as typeof fetch;
  try {
    const result = await postCanvasAction("kanban", "refresh");
    assert.deepEqual(seen, [
      {
        url: "/ui/actions",
        method: "POST",
        body: JSON.stringify({ module: "kanban", action: "refresh" }),
      },
    ]);
    assert.equal(result, "ok");
  } finally {
    globalThis.fetch = realFetch;
  }
  // route failure: the sentinel text is the error message (the panel's
  // error state renders it verbatim)
  globalThis.fetch = (async () =>
    new Response('canvas: unknown module: "ghost"', { status: 404 })) as typeof fetch;
  try {
    await assert.rejects(postCanvasAction("ghost", "x"), /unknown module/);
  } finally {
    globalThis.fetch = realFetch;
  }
  // the handler's own error: a 500, same verbatim rule
  globalThis.fetch = (async () =>
    new Response("probe: exploded", { status: 500 })) as typeof fetch;
  try {
    await assert.rejects(postCanvasAction("kanban", "refresh"), /exploded/);
  } finally {
    globalThis.fetch = realFetch;
  }
});

// --- the canvas store: snapshot-render + delta-update + action states ---

test("applySnapshot replaces the list, order preserved, junk dropped", () => {
  const store = createCanvasStore();
  store.applySnapshot([WIRE_PANEL, null, { id: "x.y", title: "Y", kind: "default", payload: null, actions: [] }]);
  assert.deepEqual(store.panels.map((p) => p.id), ["kanban.board", "x.y"]);
  // a later refresh replaces wholesale
  store.applySnapshot([{ id: "z.z", title: "Z", kind: "default", payload: null, actions: [] }]);
  assert.deepEqual(store.panels.map((p) => p.id), ["z.z"]);
  // non-array body: the previous view survives (malformed never blanks)
  store.applySnapshot("junk");
  assert.deepEqual(store.panels.map((p) => p.id), ["z.z"]);
});

test("applyDelta updates the matching panel in place, appends unknown ids", () => {
  const store = createCanvasStore();
  store.applySnapshot([WIRE_PANEL, { id: "x.y", title: "Y", kind: "default", payload: null, actions: [] }]);
  // the arc 3b SSE frame body: {type, panel} - only the panel matters here
  store.applyDelta({
    id: "x.y",
    title: "Y renamed",
    kind: "default",
    payload: { cards: 7 },
    actions: [{ id: "ping", module: "x" }],
  });
  assert.deepEqual(
    store.panels.map((p) => p.id),
    ["kanban.board", "x.y"],
    "delta replaces in place, never reorders",
  );
  assert.equal(store.panels[1]!.title, "Y renamed");
  assert.deepEqual(store.panels[1]!.payload, { cards: 7 });
  store.applyDelta(WIRE_PANEL);
  // replace-in-place never reorders
  assert.deepEqual(store.panels.map((p) => p.id), ["kanban.board", "x.y"]);
  assert.deepEqual(store.panels[0]!.payload, { cards: 3 });
  // malformed descriptor: skipped, the snapshot is the catch-up
  store.applyDelta({ nope: true });
  store.applyDelta(null);
  assert.equal(store.panels.length, 2);
});

test("invoke: optimistic pending observed synchronously, error state on 4xx/5xx, idle again on success", async () => {
  const store = createCanvasStore();
  store.applySnapshot([WIRE_PANEL]);
  const panel = store.panels[0]!;
  const action = panel.actions[0]!;
  let release: ((r: Response) => void) | null = null;
  const realFetch = globalThis.fetch;
  globalThis.fetch = (async () =>
    await new Promise<Response>((resolve) => {
      release = resolve;
    })) as typeof fetch;
  try {
    const call = store.invoke(panel, action);
    // the optimistic beat: pending is visible BEFORE the response lands
    assert.equal(store.actionState(panel.id, action.id).status, "pending");
    // success resets cleanly (the panel updates via deltas, not the response)
    release!(json({ result: "done" }));
    await call;
    // reactive proxy: compare fields (an own 'error: undefined' key is
    // deep-equal noise against a literal)
    const done = store.actionState(panel.id, action.id);
    assert.equal(done.status, "idle");
    assert.equal(done.error, undefined);
  } finally {
    globalThis.fetch = realFetch;
  }
  // route failure: the sentinel text is the error message (the panel's
  // error state renders it verbatim), then a later success clears it
  globalThis.fetch = (async () =>
    new Response('canvas: unknown action: "refresh" on "kanban"', { status: 404 })) as typeof fetch;
  await store.invoke(panel, action);
  const st = store.actionState(panel.id, action.id);
  assert.equal(st.status, "error");
  assert.match(st.error ?? "", /unknown action/);
  assert.ok(st.status === "error");
  // a later success clears the error state, not just pending
  globalThis.fetch = (async () => json({ result: 1 })) as typeof fetch;
  await store.invoke(panel, action);
  const cleared = store.actionState(panel.id, action.id);
  assert.equal(cleared.status, "idle");
  assert.equal(cleared.error, undefined);
  globalThis.fetch = realFetch;
});

test("refresh keeps the previous view on a failed snapshot fetch", async () => {
  const store = createCanvasStore();
  store.applySnapshot([WIRE_PANEL]);
  const realFetch = globalThis.fetch;
  globalThis.fetch = (async () => new Response("boom", { status: 500 })) as typeof fetch;
  try {
    await store.refresh();
    assert.deepEqual(store.panels.map((p) => p.id), ["kanban.board"]);
  } finally {
    globalThis.fetch = realFetch;
  }
});

// --- handlePayload: the delta frame dispatches like every event ---

function deltaFrame(panel: unknown): Record<string, unknown> {
  return { type: "view_model_delta", panel };
}

test("view_model_delta reaches onViewModelDelta, normalized", () => {
  const seen: CanvasPanel[] = [];
  handlePayload(
    deltaFrame(WIRE_PANEL),
    { onViewModelDelta: (p) => seen.push(p) } as unknown as Parameters<typeof handlePayload>[1],
    {},
  );
  assert.deepEqual(seen, [
    {
      id: "kanban.board",
      title: "Board",
      kind: "kanban-board",
      payload: { cards: 3 },
      actions: [{ id: "refresh", label: "Refresh", module: "kanban" }],
    },
  ]);
  // malformed descriptors are skipped, never crash the stream
  const len = seen.length;
  handlePayload(
    deltaFrame({ junk: true }),
    { onViewModelDelta: (p) => seen.push(p) } as unknown as Parameters<typeof handlePayload>[1],
    {},
  );
  handlePayload(
    deltaFrame(null),
    { onViewModelDelta: (p) => seen.push(p) } as unknown as Parameters<typeof handlePayload>[1],
    {},
  );
  assert.equal(seen.length, len);
});

test("absent onViewModelDelta: the frame is a no-op (no canvas, snapshot is the catch-up)", () => {
  handlePayload(
    deltaFrame(WIRE_PANEL),
    {} as unknown as Parameters<typeof handlePayload>[1],
    {},
  );
});

// --- the v1 plain render ---

test("formatCanvasPayload: pretty JSON, null payload marker", () => {
  assert.equal(formatCanvasPayload(null), "(no payload)");
  assert.equal(formatCanvasPayload({ a: 1 }), JSON.stringify({ a: 1 }, null, 2));
});

// --- the toast panel body (card t_aca53fee) ---------------------------------

const TOAST_RECORD = {
  id: 42,
  kind: "task",
  source_id: "abc",
  title: "Build finished",
  verdict: "ok",
  created_at: "2026-10-24T03:00:00Z",
  read: false,
};

test("toastRowOf: the toast panel renders its record row read-only, not the JSON dump", () => {
  // the arc 4b payload IS one record row: pass it through the row pipeline
  assert.deepEqual(toastRowOf("toast", TOAST_RECORD), TOAST_RECORD);
  assert.deepEqual(toastRowOf("toast", { id: 7, kind: "cron" }), { id: 7, kind: "cron" });
  // every other kind keeps the v1 plain render
  assert.equal(toastRowOf("list", TOAST_RECORD), null, "only kind toast");
  assert.equal(toastRowOf("default", TOAST_RECORD), null);
  // a toast with no record (the empty seeded panel) renders nothing row-shaped
  assert.equal(toastRowOf("toast", null), null);
  assert.equal(toastRowOf("toast", undefined), null);
  // junk payloads fall back to the plain render, never crash
  assert.equal(toastRowOf("toast", "junk"), null);
  assert.equal(toastRowOf("toast", 7), null);
  assert.equal(toastRowOf("toast", [TOAST_RECORD]), null, "not a rows array");
  assert.equal(toastRowOf("toast", { unrelated: true }), null, "nothing row-shaped");
});

test("normalizeCanvasPanel guards the descriptor", () => {
  assert.equal(normalizeCanvasPanel(null), null);
  assert.equal(normalizeCanvasPanel("junk"), null);
  assert.equal(normalizeCanvasPanel({ title: "no id" }), null);
  const p = normalizeCanvasPanel(WIRE_PANEL);
  assert.ok(p);
  assert.equal(p!.id, "kanban.board");
});

// --- source pins: the production boundary the node runner cannot import ---

test("CanvasPanel.vue: the generic renderer contract (title + payload view + action buttons)", () => {
  const src = read("modules", "ui", "app", "components", "canvas", "CanvasPanel.vue");
  // the descriptor render: title, the kind chip, the JSON payload view
  assert.match(src, /panel\.title/, "renders the descriptor's title");
  assert.match(src, /panel\.kind/, "renders the kind chip");
  assert.match(src, /canvas-payload/, "renders the payload view region");
  assert.match(src, /formatCanvasPayload/, "payload view is the shared plain formatter");
  // the action buttons: pending disables the button, error renders
  assert.match(src, /panel\.actions\b/, "renders the descriptor's actions");
  assert.match(src, /disabled/, "pending state disables the button");
  assert.match(src, /pending/, "pending state renders");
  assert.match(src, /canvas-error/, "error state renders");
  // kind handling: v1 was the plain render only (Q2b); card t_aca53fee
  // settled the toast kind - the record row renders read-only through
  // the row pipeline, NEVER the raw JSON dump, and there is still no
  // speculative per-kind template switching (the gate is script-side)
  assert.ok(!/v-if="panel\.kind/.test(src), "no speculative kind branches");
  assert.match(src, /toastRowOf\(/, "the toast panel routes through the row pipeline");
  // the row text width-truncates at the pane width (kind/key fields on
  // one line) - the same contract the list panel's rows render under
  const css = readFileSync(join(modules, "modules", "ui", "app", "assets", "css", "main.css"), "utf8");
  assert.match(
    css,
    /\.canvas-row-text \{[\s\S]*?text-overflow: ellipsis/,
    "canvas row lines truncate at the pane width",
  );
  // the click emits to the owner: the component holds no store
  assert.match(src, /emit\('invoke'/s, "action clicks emit to the owner");
  assert.ok(!/fetch\(/.test(src), "the renderer never fetches");
});

test("app.vue: the canvas outlet, the boot snapshot, and the delta wiring", () => {
  const app = read("modules", "ui", "app", "app.vue");
  assert.match(app, /#pane-canvas/, "the canvas dock outlet exists");
  assert.match(app, /canvas\.refresh\(\)/, "the snapshot loads at boot (Q3b)");
  assert.match(app, /canvasDelta:\s*\(panel\)/, "the host forwards run-stream deltas");
  assert.match(app, /canvas\.applyDelta\(panel\)/, "deltas merge into the store");
  assert.match(app, /canvas\.invoke/, "action clicks route through the store's invoke");
  // Q5a law: the shell has no hardcoded panel id, the store is the
  // presence list
  assert.ok(!/canvas\.panels\.some|panelsById/.test(app), "no hardcoded panel presence check");
});

test("chat.ts: canvas deltas forward from every stream handler set", () => {
  const chat = read("modules", "ui", "app", "composables", "chat.ts");
  assert.match(
    chat,
    /onViewModelDelta[^\n]*deps\.canvasDelta\?\.\(panel\)/,
    "the transport handlers forward the delta to the app's canvas",
  );
  // chat-last 2a: the chat panel is also the chat pane's render source -
  // the view reads it through the host accessor, the meter repaints
  assert.match(chat, /chatPanel\(\): CanvasPanel \| null/, "the host exposes the panel descriptor");
  assert.match(chat, /normalizeChatPayload\(panel\.payload\)/, "the payload guards at the paint site");
});

test("dockables: canvas is a layout-only registry entry (Q5a law)", () => {
  const src = read("modules", "ui", "app", "composables", "dockables.ts");
  const entry = DOCKABLES.find((d) => d.id === "canvas");
  assert.ok((DOCK_COMPONENTS as readonly string[]).includes("canvas"), "the outlet id exists");
  assert.ok(entry, "the registry entry exists");
  assert.equal(entry!.area, "right");
  assert.equal(entry!.policy, "singleton");
  assert.equal(entry!.closable, false);
  assert.ok(entry!.icon.paths.length > 0, "an icon is declared");
  assert.equal(COMPONENT_TITLES.canvas, "Canvas");
  assert.ok(MENU_DOCKABLES.includes("canvas"), "the '+' menu can refill the canvas tab");
  assert.ok(
    (INSPECTOR_TAB_COMPONENTS as readonly string[]).includes("canvas"),
    "the canvas loads with the inspector strip",
  );
  // no panel id may enter the registry: the component list is shell
  // domains only (the canvas contract's panels live in the snapshot)
  assert.ok(
    !DOCK_COMPONENTS.some((c) => c.includes(".")),
    "no module-qualified panel id in the registry",
  );
});