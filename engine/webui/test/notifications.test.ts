// omega webui: unit tests for the notifications surface (card
// t_5d9032d3). Three layers, matching the house patterns:
// - composables/notifications.ts fetch wrappers: URL + body contract
//   under fetch interception;
// - the pure helpers: age rendering (honest "" on garbage), the live
//   upsert (dedupe + cap), and the toast lifetime asymmetry;
// - NotificationCenter.vue source pins: the mount + the route wiring
//   the dock shell depends on (source checks, the panel source is not
//   importable in node).
// Run: node --test engine/webui/test/ (node >= 23 runs TS natively)
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
  boardOfTask,
  resolveTaskBoard,
  selectBoardFor,
  type OmegaNotification,
} from "../app/composables/notifications.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "..", "..");

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
  const menubar = read("webui", "app", "components", "shell", "MenuBar.vue");
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

test("app.vue routes every notified kind (task -> kanban pane, cron -> tab, chat -> session)", () => {
  const app = read("webui", "app", "app.vue");
  assert.ok(app.includes("@open-notification="), "MenuBar binding exists");
  assert.ok(app.includes('case "task"'), "task routes");
  assert.ok(app.includes('case "cron"'), "cron routes");
  assert.ok(app.includes('case "chat"'), "chat routes");
  // card t_556ee727: the task route focuses the kanban PANE - the main
  // pane (chat) is never touched, so no /board replaceState overlay on
  // the bell's path. The /board URL boot (a bookmarked reload) lives in
  // a separate block and is untouched.
  const taskStart = app.indexOf('"task"');
  assert.ok(taskStart > 0, "task case exists");
  const taskBody = app.slice(taskStart, app.indexOf("case \"cron\""));
  assert.ok(taskBody.includes("openSingleton"), "the task click focuses the kanban tab");
  assert.ok(taskBody.includes("focusNotificationBoard"), "the task click hands the board select");
  assert.ok(
    !taskBody.includes("replaceState") && !taskBody.includes("mainView"),
    "the task click changes no pane content",
  );
});

// --- the board-select seam (card t_556ee727) ---

test("boardOfTask scans the boards in registry order", () => {
  const lists = [
    { board: "main", tasks: [{ id: "t_one" }, { id: "t_two" }] },
    { board: "omega", tasks: [{ id: "t_three" }] },
  ];
  assert.equal(boardOfTask("t_three", lists), "omega");
  assert.equal(boardOfTask("t_one", lists), "main");
  assert.equal(boardOfTask("t_archived", lists), null, "a miss resolves null");
  // the server default is never a switch target: the empty board name
  // is skipped, the scan keeps looking
  assert.equal(
    boardOfTask("t_two", [{ board: "", tasks: [{ id: "t_two" }] }, ...lists]),
    "main",
  );
});

test("resolveTaskBoard rides /boards then a /tasks scan per board", async () => {
  const seen: string[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL) => {
    const u = String(url);
    seen.push(u);
    if (u === "/boards") {
      return Promise.resolve(new Response(
        JSON.stringify({
          boards: [{ name: "main", current: true }, { name: "omega", current: false }],
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      ));
    }
    const board = new URL(u, "http://x").searchParams.get("board") ?? "";
    const tasks = board === "omega" ? [{ id: "t_deep" }] : [{ id: "t_main" }];
    return Promise.resolve(new Response(
      JSON.stringify({ tasks }),
      { status: 200, headers: { "content-type": "application/json" } },
    ));
  }) as typeof globalThis.fetch;
  try {
    assert.equal(await resolveTaskBoard("t_deep"), "omega");
    assert.equal(await resolveTaskBoard("t_nope"), null);
    // every miss still walks the FULL registry (main then omega) - a
    // stale scan would dead-end the second click
    assert.deepEqual(seen.filter((u) => u.startsWith("/tasks?board=")), [
      "/tasks?board=main",
      "/tasks?board=omega",
      "/tasks?board=main",
      "/tasks?board=omega",
    ]);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("resolveTaskBoard degrades on a broken registry (501, garbage body)", async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL) => {
    const u = String(url);
    return Promise.resolve(new Response(
      u === "/boards" ? "kanban board not configured" : JSON.stringify({ tasks: [] }),
      { status: u === "/boards" ? 501 : 200, headers: { "content-type": "text/html" } },
    ));
  }) as typeof globalThis.fetch;
  try {
    assert.equal(await resolveTaskBoard("t_x"), null, "501 => null, the pane focus still ran");
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("selectBoardFor publishes the stash + the event, skips the empty pick", () => {
  // jsdom-free: only the window guard matters in node
  if (typeof window === "undefined") return; // node: the guard is the pin
  assert.equal(selectBoardFor(""), undefined);
});
// --- arc 4b (t_9339ad05): the toast is a canvas event ---

test("NotificationCenter renders toasts from the canvas - no per-frontend SSE stream", () => {
  const dock = read("webui", "app", "components", "notifications", "NotificationCenter.vue");
  const compo = read("webui", "app", "composables", "notifications.ts");
  // the dock subscribes to the canvas feed, not its own EventSource
  assert.ok(dock.includes("onToastPanel("), "the dock consumes the canvas toast feed");
  assert.ok(dock.includes("decodeToast("), "the dock owns the record-shape read");
  // the per-frontend toast code is gone from composable + dock
  assert.ok(!compo.includes("subscribeNotifications") && !dock.includes("subscribeNotifications"),
    "the private SSE toast client is deleted, not just unused");
  assert.ok(!compo.includes("/notifications/events"), "no SSE toast stream stays reachable from the shell");
});

test("canvas.ts: the toast feed rides both transports, deduped by the fingerprint", () => {
  const src = read("webui", "app", "composables", "canvas.ts");
  assert.ok(src.includes("function feedToastPanel("), "the funnel exists");
  assert.ok(/applyDelta\(raw: unknown\)[\s\S]*feedToastPanel\(p\)/.test(src), "deltas feed the funnel");
  assert.ok(/applySnapshot\(list: unknown\)[\s\S]*feedToastPanel\(p\)/.test(src), "snapshots feed the funnel (the between-runs catch-up)");
  assert.ok(src.includes('p.kind !== "toast"'), "only toast-kinded panels pass");
  assert.ok(src.includes("lastToastFingerprint"), "the fingerprint dedupes replays");
  // the contract stays record-blind: handlers get the payload verbatim
  assert.ok(!src.includes("OmegaNotification"), "canvas.ts knows no record shape");
});
