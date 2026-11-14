// omega webui: the approval TOAST (card t_a2ed8412).
//
// Endo's live report: an approval only surfaced when he switched to the
// notifications tab. The mechanism: a pending verdict reaches this store
// through the shell's ordinary event hints (a chat frame's call/turn
// edge - the chat topic is NOT pane-scoped, the hub fans every run to
// every page), so the BADGE always counted a background approval. What
// it never had was a voice: the badge rides a dock chip that is not on
// screen when the notifications tab is elsewhere. The arrival now
// toasts.
//
// Layers, the house way:
// - the pure arrival diff (newlyPendingVerdicts): seeding, one toast per
//   card, re-issue after the card leaves the list, malformed rows;
// - the toast dwell constant;
// - source pins: the union shape, the store wiring the arrivals into the
//   toast stack, the toast component's verdict branch, and the click
//   route into the notifications dockable (components are not
//   importable in node).
// Run: node --test "test/*.test.ts" (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  newlyPendingVerdicts,
  toastLifetime,
  VERDICT_TOAST_MS,
} from "../app/composables/notifications.ts";
import type { PendingVerdictCard } from "../app/composables/omega.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "..");

function read(...parts: string[]): string {
  return readFileSync(join(modules, ...parts), "utf8");
}

function card(id: string, session = "sess-bg"): PendingVerdictCard {
  return {
    id,
    session_id: session,
    title: `approval ${id}`,
    expires_at: 1794659999000,
  };
}

// --- the pure arrival diff ----------------------------------------------

test("newlyPendingVerdicts seeds on the first read - a reload never replays", () => {
  const seen = new Set<string>();
  // the boot read: two cards already pending when the page loaded
  const boot = newlyPendingVerdicts(seen, [card("a"), card("b")], { seed: true });
  assert.deepEqual(boot, [], "a page that just loaded toasts nothing");
  assert.deepEqual([...seen].sort(), ["a", "b"], "the seed remembers every live card");

  // the next read: no arrivals, the set is unchanged
  assert.deepEqual(newlyPendingVerdicts(seen, [card("a"), card("b")]), []);
});

test("newlyPendingVerdicts returns the arrivals - one toast per new card", () => {
  const seen = new Set<string>();
  newlyPendingVerdicts(seen, [card("a")], { seed: true });

  // a background session issues a card: the hint's REST read sees it
  const arrivals = newlyPendingVerdicts(seen, [card("a"), card("bg-1")]);
  assert.deepEqual(arrivals.map((c) => c.id), ["bg-1"], "only the new card toasts");

  // the same read again: the arrival is remembered, no second toast
  assert.deepEqual(newlyPendingVerdicts(seen, [card("a"), card("bg-1")]), []);

  // a burst of two: both toast, in the server's order
  const burst = newlyPendingVerdicts(seen, [card("a"), card("bg-1"), card("bg-2"), card("bg-3")]);
  assert.deepEqual(burst.map((c) => c.id), ["bg-2", "bg-3"]);
});

test("newlyPendingVerdicts forgets a card that left the list - a re-issue toasts", () => {
  const seen = new Set<string>();
  newlyPendingVerdicts(seen, [], { seed: true });
  newlyPendingVerdicts(seen, [card("x")]);
  assert.equal(seen.has("x"), true);

  // answered / expired: the card left the pending list
  newlyPendingVerdicts(seen, []);
  assert.equal(seen.has("x"), false, "an absent card leaves the announced set");

  // the same id comes back (a retry, a re-ask): it toasts again
  assert.deepEqual(newlyPendingVerdicts(seen, [card("x")]).map((c) => c.id), ["x"]);
});

test("newlyPendingVerdicts never announces a malformed row", () => {
  const seen = new Set<string>();
  newlyPendingVerdicts(seen, [], { seed: true });
  const junk = [
    { title: "no id" },
    { id: "", title: "empty id" },
    { id: 7, title: "numeric id" },
    null,
  ] as unknown as PendingVerdictCard[];
  assert.deepEqual(newlyPendingVerdicts(seen, junk), [], "a shape the server never sends toasts nothing");
  assert.equal(seen.size, 0, "and it is never remembered");
});

test("newlyPendingVerdicts keeps a valid row inside a malformed batch", () => {
  const seen = new Set<string>();
  newlyPendingVerdicts(seen, [], { seed: true });
  const mixed = [null, card("ok-1")] as unknown as PendingVerdictCard[];
  assert.deepEqual(newlyPendingVerdicts(seen, mixed).map((c) => c.id), ["ok-1"]);
});

// --- the dwell ----------------------------------------------------------

test("an approval toast outlives a finished thing's", () => {
  assert.equal(VERDICT_TOAST_MS, 15000);
  assert.ok(
    VERDICT_TOAST_MS > toastLifetime("failed"),
    "the approval toast lives longer than the longest record toast: a question waits on a click",
  );
});

// --- source pins: the wiring -------------------------------------------

test("the store toasts ARRIVALS from the hint path, never from the panel", () => {
  const store = read("app/components/shell/notificationFeed.ts");
  assert.match(store, /export type NotificationToast\s*=/, "the stack carries a typed union");
  assert.match(store, /kind: "record"; record: OmegaNotification/, "a finished thing keeps its shape");
  assert.match(store, /kind: "verdict"; card: PendingVerdictCard/, "an approval rides its own branch");
  assert.match(
    store,
    /for \(const card of arrivals\) pushVerdictToast\(card\)/,
    "the refresh path announces the diff (the boot read, an event hint, a reconnect)",
  );
  assert.match(store, /newlyPendingVerdicts,/, "the store consumes the pure diff");
  // the diff is computed inside refresh(), so EVERY pull shares it - the
  // panel's activation read included, and a card pending there is old
  // news (the seed) rather than a fresh toast
  const refresh = store.slice(store.indexOf("async function refresh()"), store.indexOf("function pushVerdictToast"));
  assert.match(refresh, /verdictsSeeded/, "the first read seeds the announced set");
  assert.match(refresh, /pendingVerdicts\.value = vpage\.cards/, "the pending list still lands");
});

test("the two REST reads are independent - a broken feed never blanks the questions", () => {
  const store = read("app/components/shell/notificationFeed.ts");
  const refresh = store.slice(store.indexOf("async function refresh()"), store.indexOf("export function newlyPendingVerdicts"));
  assert.match(refresh, /fetchNotifications\(false\)\.catch\(\(\) => null\)/, "the feed read degrades alone");
  assert.match(refresh, /fetchVerdicts\(""\)\.catch\(\(\) => null\)/, "the verdict read degrades alone");
  assert.match(refresh, /if \(vpage\)/, "the pending list updates on its own answer");
});

test("the toast component renders the verdict branch and routes its click", () => {
  const vue = read("app/components/shell/NotificationToasts.vue");
  assert.match(vue, /t\.kind === 'record' && t\.record\.verdict === 'failed'/, "the failed rail reads the record branch only");
  assert.match(vue, /approval: t\.kind === 'verdict'/, "an approval wears its own rail");
  assert.match(vue, /t\.kind === "verdict" \? t\.card\.title : t\.record\.title/, "the title comes from the union");
  assert.match(vue, /"approval requested"/, "a question reports no verdict - it asks");
  assert.match(vue, /if \(t\.kind === "verdict"\) \{[\s\S]*?emit\("open", openToastVerdict/, "the click emits the open intent");
  assert.match(vue, /border-left-color: var\(--accent\)/, "accent, never the error red");
});

test("the approval toast keeps the menubar's single forward", () => {
  const bar = read("app/components/shell/MenuBar.vue");
  // one open intent, one forward: the toast's payload discriminates, so
  // the menubar needs no second event (and app.vue stays the only router)
  assert.match(bar, /@open="\$emit\('open-notification', \$event\)"/, "the stack forwards its open intent");
  assert.ok(!bar.includes("open-approval"), "no second event was added to the menubar");
  const toasts = read("app/components/shell/NotificationToasts.vue");
  assert.match(
    toasts,
    /emit\("open", openToastVerdict\(t\)\)/,
    "the approval click rides the same open intent",
  );
  const app = read("app/app.vue");
  const router = app.slice(app.indexOf("function openNotification("), app.indexOf("function openNotification(") + 900);
  assert.match(router, /typeof card\.session_id === "string"/, "the payload is discriminated as a pending card");
  assert.match(router, /paletteOpenDockable\("notifications"\)/, "its answering surface is the notifications tab");
  assert.match(app, /^\s+PendingVerdictCard,/m, "the card type is imported, not conjured");
});
