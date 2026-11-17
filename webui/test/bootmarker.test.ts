// omega webui: the boot-settled marker (card t_a071ee45).
//
// The shell publishes "my boot reads are done" as
// `document.documentElement.dataset.omegaBooted = "1"`, and the journey
// recipe scripts/journeys/recipes/111-socket-push.journey.sh waits for it
// before installing its fetch recorder. Three nightlies red'd that recipe
// because the boundary was networkidle instead: networkidle returns
// during hydration, the boot reads are issued AFTER it, and the recorder
// then graded a legitimate boot fetch as a poller (43807490891,
// 43845134289, 43908179838).
//
// The whole value is the ISSUED vs RESOLVED distinction, so that is what
// this file pins (the SFC and the store are not importable in node --test;
// source pins are the house form here):
// - the store holds its boot refresh as a promise the shell can await,
//   instead of firing it into the void;
// - app.vue collects the boot group and sets the marker inside the
//   Promise.allSettled continuation, after every read landed;
// - the group still carries each boot read (a read dropped from it would
//   move the boundary back to "issued" for that route).
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

test("the feed store holds its boot refresh as a promise, not a fire-and-forget", () => {
  const store = read("app/components/shell/notificationFeed.ts");
  assert.match(
    store,
    /bootRead = refresh\(\);/,
    "the /notifications + /verdicts boot pair is HELD: the marker awaits its RESOLVE",
  );
  assert.doesNotMatch(
    store,
    /void refresh\(\);/,
    "a voided boot read is the shape the three nightly reds came from",
  );
  assert.match(
    store,
    /export function notificationFeedSettled\(\): Promise<void> \{\s*return bootRead \?\? Promise\.resolve\(\);/,
    "the shell asks the store for the settled promise (resolved when the feed never started)",
  );
});

test("stopNotificationFeed drops the boot read with the rest of the page state", () => {
  const store = read("app/components/shell/notificationFeed.ts");
  const stop = store.slice(store.indexOf("export function stopNotificationFeed"));
  const body = stop.slice(0, stop.indexOf("}\n"));
  assert.match(
    body,
    /bootRead = null;/,
    "a store restarted by HMR/tests must not hand a stale promise to the next page's marker",
  );
});

test("app.vue sets the marker inside the allSettled continuation, never beside it", () => {
  const app = read("app/app.vue");
  assert.equal(
    app.split("dataset.omegaBooted").length - 1,
    1,
    "exactly one writer of the marker",
  );
  assert.match(
    app,
    /Promise\.allSettled\(bootReads\)\.then\(\(\) => \{[^}]*document\.documentElement\.dataset\.omegaBooted = "1";/,
    "set ONLY after every boot read RESOLVES (/models can land last on a stalled provider)",
  );
});

test("the boot group carries every read the poller window grades", () => {
  const app = read("app/app.vue");
  const start = app.indexOf("const bootReads: Promise<unknown>[] = [");
  assert.ok(start > 0, "the group exists");
  const group = app.slice(start, app.indexOf("];", start));
  for (const member of [
    "notificationFeedSettled()", // /notifications + /verdicts
    "refreshModels()", // /models
    "fetchSettingKnob(DEFAULT_WORKSPACE_KNOB)", // /settings?key= (card t_0a9edc72: the one knob, not the registry)
    "refreshSessions()", // /sessions
    "canvas.refresh()", // /ui/canvas
  ]) {
    assert.ok(
      group.includes(member),
      `${member} rides the boot group - dropped, its read lands after the marker`,
    );
  }
});
