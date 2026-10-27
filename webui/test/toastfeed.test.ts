import { test } from "node:test";
import assert from "node:assert/strict";

// Arc 4b (t_9339ad05): the webui renders toasts from the canvas. The
// dock's toast decoder and the feed's dedupe are the moving parts;
// these tests import the production shapes.

import { onToastPanel } from "../app/composables/canvas.ts";
import { toastLifetime, upsertRecord, type OmegaNotification } from "../app/composables/notifications.ts";
import { createCanvasStore } from "../app/composables/canvas.ts";

// decodeToast mirrors NotificationCenter.vue's decoder (script-setup
// code can't be imported from node --test; the shape law is pinned
// here and the component consumes the same fields).
function decodeToast(payload: unknown): OmegaNotification | null {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.id !== "number") return null;
  const kind = p.kind;
  if (kind !== "task" && kind !== "cron" && kind !== "chat") return null;
  return {
    id: p.id,
    kind,
    source_id: typeof p.source_id === "string" ? p.source_id : "",
    title: typeof p.title === "string" ? p.title : "",
    verdict: p.verdict === "failed" ? "failed" : "ok",
    created_at: typeof p.created_at === "string" ? p.created_at : "",
    read: p.read === true,
  };
}

test("toast feed: deltas and snapshots toast once, replays drop", () => {
  const store = createCanvasStore();
  const frames: unknown[] = [];
  const off = onToastPanel((p) => frames.push(p));
  const row = { id: 7, kind: "task", source_id: "t1", title: "task t1 done", verdict: "ok", created_at: "2026-10-24", read: false };

  // a deltas arrives on a run stream: the toast fires
  store.applyDelta({ id: "toast", title: "Toast", kind: "toast", payload: row, actions: [] });
  assert.equal(frames.length, 1);

  // the same state refetched on a snapshot (reconnect replay): silent
  store.applySnapshot([{ id: "toast", title: "Toast", kind: "toast", payload: row, actions: [] }]);
  assert.equal(frames.length, 1);

  // a new emit (different record): the toast fires again
  store.applyDelta({ id: "toast", title: "Toast", kind: "toast", payload: { ...row, id: 8, verdict: "failed" }, actions: [] });
  assert.equal(frames.length, 2);

  // an empty toast panel (nothing emitted): no toast
  store.applyDelta({ id: "toast", title: "Toast", kind: "toast", actions: [] });
  assert.equal(frames.length, 2);

  // list panels never toast
  store.applyDelta({ id: "notifications", title: "Notifications", kind: "list", payload: { unread: 1, rows: [row] },
    actions: [{ id: "mark_all", label: "mark all read", module: "notifications" }] });
  assert.equal(frames.length, 2);

  off();
});

test("toast feed: subscription closes", () => {
  const store = createCanvasStore();
  let count = 0;
  const off = onToastPanel(() => { count += 1; });
  off();
  store.applyDelta({ id: "toast", title: "Toast", kind: "toast", payload: { id: 1, kind: "chat" }, actions: [] });
  assert.equal(count, 0);
});

test("toast decoder: the dock reads its own record shape", () => {
  assert.equal(decodeToast({ id: 3, kind: "cron", title: "cron nightly finished", verdict: "failed" })?.verdict, "failed");
  assert.equal(decodeToast({ id: 3, kind: "task", read: true })?.read, true);
  assert.equal(decodeToast({ kind: "task" }), null); // no id: not a toast
  assert.equal(decodeToast({ id: 3, kind: "bogus" }), null); // unknown kind
  assert.equal(decodeToast(null), null);
  assert.equal(decodeToast("task t1 done"), null);
});

test("toast lifetime keeps the failed asymmetry", () => {
  assert.equal(toastLifetime("failed"), 12000);
  assert.equal(toastLifetime("ok"), 6000);
});

test("upsert (the dock's live list fold) dedupes and caps", () => {
  const n = (id: number): OmegaNotification => ({ id, kind: "task", source_id: "", title: "", verdict: "ok", created_at: "", read: false });
  let list: OmegaNotification[] = upsertRecord([], n(1));
  list = upsertRecord(list, n(2));
  // a new id prepends; a known id replaces in place
  assert.deepEqual(list.map((x) => x.id), [2, 1]);
  list = upsertRecord(list, { ...n(1), read: true });
  assert.deepEqual(list.map((x) => x.id), [2, 1]);
  assert.equal(list[1]!.read, true);
  assert.equal(list.length, 2);
});