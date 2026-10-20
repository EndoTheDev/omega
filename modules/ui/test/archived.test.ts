// omega webui: unit tests for the archived-session group (card
// t_e527b3e8): archived sessions form the LAST group in the session
// list - bucket placement stays pure in composables/omega.ts, the
// footer toggle dies. Run: node --test "test/*.test.ts" (node >= 23
// runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import {
  bucketSessions,
  SIDEBAR_BUCKETS,
} from "../app/composables/omega.ts";
import type { Session } from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string): string => readFileSync(`${root}${p}`, "utf8");

function sess(overrides: Partial<Session> & { id: string }): Session {
  return {
    label: overrides.id,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const NOW = new Date("2026-10-20T14:00:00");
const TODAY = "2026-10-20";

describe("bucketSessions: the archived group", () => {
  it("always lands last, after the newest time bucket", () => {
    const b = bucketSessions(
      [
        // archived but the FRESHEST timestamp in the list - age never
        // lifts it out of the bottom group
        sess({ id: "arch", archived: true, updated_at: `${TODAY}T13:00:00` }),
        sess({ id: "today", updated_at: `${TODAY}T09:00:00` }),
        sess({ id: "old", updated_at: "2026-10-01T10:00:00" }),
      ],
      NOW,
    );
    assert.deepEqual(b.archived.map((s) => s.id), ["arch"]);
    assert.deepEqual(b.today.map((s) => s.id), ["today"]);
    assert.deepEqual(b.earlier.map((s) => s.id), ["old"]);
  });

  it("archived beats pinned - no row lives in two groups", () => {
    const b = bucketSessions(
      [sess({ id: "both", archived: true, pinned: true })],
      NOW,
    );
    assert.deepEqual(b.archived.map((s) => s.id), ["both"]);
    assert.deepEqual(b.pinned, []);
  });

  it("live rows never leak into the archived group", () => {
    const b = bucketSessions(
      [
        sess({ id: "live", updated_at: `${TODAY}T09:00:00` }),
        sess({ id: "flagless" }),
      ],
      NOW,
    );
    assert.deepEqual(b.archived, []);
  });

  it("unarchiving drops the row back into its day bucket instantly", () => {
    const boxed = [sess({ id: "boxed", archived: true })];
    assert.equal(bucketSessions(boxed, NOW).archived.length, 1);
    // the optimistic patch flips the flag on the row the list holds
    const unarchived = boxed.map((s) => ({ ...s, archived: false }));
    const b = bucketSessions(unarchived, NOW);
    assert.deepEqual(b.archived, []);
    assert.deepEqual(b.earlier.map((s) => s.id), ["boxed"]);
  });

  it("the archived group sorts newest-first within itself", () => {
    const b = bucketSessions(
      [
        sess({ id: "a1", archived: true, updated_at: "2026-10-14T10:00:00" }),
        sess({ id: "a2", archived: true, updated_at: "2026-10-18T10:00:00" }),
      ],
      NOW,
    );
    assert.deepEqual(b.archived.map((s) => s.id), ["a2", "a1"]);
  });

  it("an empty archived group is empty - and the label allowlist ends with it", () => {
    assert.deepEqual(bucketSessions([], NOW).archived, []);
    assert.deepEqual([...SIDEBAR_BUCKETS], [
      "pinned",
      "today",
      "yesterday",
      "earlier",
      "archived",
    ]);
  });
});

// --- wiring: the footer toggle died with the group ---
describe("archived group wiring (sources)", () => {
  it("the sidebar holds no archived footer toggle and no showArchived prop", () => {
    const sidebar = src("app/components/sessions/AppSidebar.vue");
    assert.ok(!sidebar.includes("sessions-foot"), "sessions-foot removed");
    assert.ok(!sidebar.includes("archived-toggle"), "archived-toggle removed");
    assert.ok(!sidebar.includes("showArchived"), "showArchived prop removed");
    assert.ok(
      !sidebar.includes("toggle-archived"),
      "toggle-archived emit removed",
    );
  });

  it("the archived section renders last, dimmed via bucket-archived", () => {
    const sidebar = src("app/components/sessions/AppSidebar.vue");
    assert.match(
      sidebar,
      /\{ label: "archived", rows: b\.archived, archived: true \}/,
    );
    assert.match(sidebar, /bucket-archived/);
    const css = src("app/assets/css/main.css");
    assert.match(css, /\.session-list li\.bucket-archived button\.bucket-label/);
  });

  it("app.vue fetches both archive sides in one list", () => {
    const app = src("app/app.vue");
    assert.ok(!app.includes("showArchived"), "the filter ref is gone");
    assert.ok(!app.includes("toggle-archived"));
    assert.match(app, /await fetchSessions\(\)/);
  });

  it("fetch + search ride archived=include from the shared composables", () => {
    const omega = src("app/composables/omega.ts");
    assert.match(
      omega,
      /"\/sessions\?archived=include"/,
    );
    assert.match(
      omega,
      /&archived=include/,
    );
    assert.ok(!omega.includes("filterArchived"), "the filter helper is gone");
  });
});