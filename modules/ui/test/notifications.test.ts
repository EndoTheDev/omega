// omega webui: unit tests for the notifications surface (card
// t_5d9032d3). Three layers, matching the house patterns:
// - composables/notifications.ts fetch wrappers: URL + body contract
//   under fetch interception;
// - the pure helpers: age rendering (honest "" on garbage), the live
//   upsert (dedupe + cap), and the toast lifetime asymmetry;
// - NotificationCenter.vue source pins: the mount + the route wiring
//   the dock shell depends on (source checks, the panel source is not
//   importable in node).
// Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  fetchNotifications,
  markNotificationRead,
  markAllNotificationsRead,
  notificationAge,
  upsertRecord,
  toastLifetime,
  type OmegaNotification,
} from "../app/composables/notifications.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "..", "..", "..");

function read(...parts: string[]): string {
  return readFileSync(join(modules, ...parts), "utf8");
}

function rec(over: Partial<OmegaNotification> = {}): OmegaNotification {
  return {
    id: 1,
    kind: "task",
    source_id: "t_abc123",
    title: "task t_abc123 done",
    verdict: "ok",
    created_at: "2026-10-22T04:00:00Z",
    read: false,
    ...over,
  };
}

// --- fetch wrappers: URL + body contract ---

test("fetchNotifications requests the bare endpoint, no arg", async () => {
  const seen: string[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL) => {
    seen.push(String(url));
    return Promise.resolve(new Response(
      JSON.stringify({ notifications: [], unread: 0 }),
      { status: 200, headers: { "content-type": "application/json" } },
    ));
  }) as typeof globalThis.fetch;
  try {
    const page = await fetchNotifications(false);
    assert.equal(seen.length, 1);
    assert.equal(seen[0], "/notifications");
    assert.deepEqual(page.notifications, []);
    assert.equal(page.unread, 0);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("fetchNotifications unreadOnly adds exactly the unread=1 filter", async () => {
  const seen: string[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL) => {
    seen.push(String(url));
    return Promise.resolve(new Response(
      JSON.stringify({ notifications: [{ id: 3, kind: "cron", source_id: "nightly", title: "cron nightly finished", verdict: "failed", created_at: "2026-10-22T04:00:00Z", read: false }] }),
      { status: 200, headers: { "content-type": "application/json" } },
    ));
  }) as typeof globalThis.fetch;
  try {
    const page = await fetchNotifications(true);
    assert.equal(seen[0], "/notifications?unread=1");
    assert.equal(page.notifications[0].verdict, "failed");
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("markNotificationRead POSTs {id} to /notifications/read", async () => {
  const seen: { url: string; method: string; body: string }[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL, init?: RequestInit) => {
    seen.push({ url: String(url), method: init?.method ?? "", body: String(init?.body) });
    return Promise.resolve(new Response(JSON.stringify({ ok: true }), { status: 200 }));
  }) as typeof globalThis.fetch;
  try {
    await markNotificationRead(7);
    assert.equal(seen[0].url, "/notifications/read");
    assert.equal(seen[0].method, "POST");
    assert.deepEqual(JSON.parse(seen[0].body), { id: 7 });
    await markAllNotificationsRead();
    assert.deepEqual(JSON.parse(seen[1].body), { all: true });
  } finally {
    globalThis.fetch = realFetch;
  }
});

// --- notificationAge ---

test("notificationAge renders seconds, minutes, hours, days", () => {
  const now = Date.parse("2026-10-22T12:00:00Z");
  assert.equal(notificationAge("2026-10-22T11:59:50Z", now), "10s");
  assert.equal(notificationAge("2026-10-22T11:30:00Z", now), "30m");
  assert.equal(notificationAge("2026-10-22T09:00:00Z", now), "3h");
  assert.equal(notificationAge("2026-10-18T12:00:00Z", now), "4d");
});

test("notificationAge is honest on garbage: empty string, no lie", () => {
  assert.equal(notificationAge(""), "");
  assert.equal(notificationAge("not-a-timestamp"), "");
  // a future timestamp (clock skew) clamps to 0s, never negative
  assert.equal(notificationAge("2026-10-22T12:00:01Z", Date.parse("2026-10-22T12:00:00Z")), "0s");
});

// --- upsertRecord: the live frame fold ---

test("upsertRecord prepends a new record and caps the list", () => {
  const a = rec({ id: 1 });
  const b = rec({ id: 2 });
  const capped = upsertRecord([a], b, 2);
  assert.deepEqual(capped.map((x) => x.id), [2, 1]);
  const third = rec({ id: 3 });
  assert.deepEqual(upsertRecord(capped, third, 2).map((x) => x.id), [3, 2]);
});

test("upsertRecord dedupes a reconnect replay instead of stacking", () => {
  const a = rec({ id: 1 });
  const again = rec({ id: 1, read: true });
  const out = upsertRecord([a], again, 50);
  assert.equal(out.length, 1);
  assert.equal(out[0].read, true);
});

// --- toastLifetime: the failed-stays-longer asymmetry ---

test("failed verdicts hold the toast longer than ok ones", () => {
  assert.equal(toastLifetime("ok"), 6000);
  assert.equal(toastLifetime("failed"), 12000);
  assert.ok(toastLifetime("failed") > toastLifetime("ok"));
});

// --- component source pins (the shell wiring) ---

test("MenuBar hosts the NotificationCenter and re-emits its open intent", () => {
  const menubar = read("modules", "ui", "app", "components", "shell", "MenuBar.vue");
  assert.ok(menubar.includes("<NotificationCenter"), "bell joins the menubar");
  assert.ok(
    menubar.includes('@open-notification="openNotification"') === false,
    "the re-emit is MenuBar's own job, app.vue binds the handler",
  );
  assert.ok(
    /<NotificationCenter\s+@open="\$emit\('open-notification', \$event\)"\s*\/>/.test(menubar),
    "open intents re-emit verbatim",
  );
});

test("app.vue routes every notified kind (task -> board, cron -> tab, chat -> session)", () => {
  const app = read("modules", "ui", "app", "app.vue");
  assert.ok(app.includes("@open-notification="), "MenuBar binding exists");
  assert.ok(app.includes('case "task"'), "task routes");
  assert.ok(app.includes('case "cron"'), "cron routes");
  assert.ok(app.includes('case "chat"'), "chat routes");
  assert.ok(app.includes('"/board"'), "the task route lands on the board");
});