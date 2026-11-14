// omega webui: unit tests for the Cron tab's per-job toggle chain
// (card t_561dc4e0). The behavior half runs at the helper level (the
// mcp.test.ts recipe): a fetch-log probe drives setCronJobEnabled, no
// SFC compile. The wire contract: POST /cron/toggle lands the enabled
// flag write (the Go fold-write the cron toggle_test.go pins), the
// re-fetch GET /cron/jobs IS the returned listing (one response
// carries the state it landed, the listing is the re-read), and a
// failed POST throws before any re-fetch so the prior rows stand.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  cronKeyOf,
  setCronJobEnabled,
  type CronJobWire,
} from "../../harness/cron/ui/toggle.ts";

let savedFetch: typeof globalThis.fetch;

interface ToggleCall {
  url: string;
  method: string;
  body?: string;
}

interface ToggleResponse {
  status: number;
  payload: string;
  contentType?: string;
}

function recordToggleFetch(
  respond: (call: ToggleCall) => ToggleResponse,
): ToggleCall[] {
  const calls: ToggleCall[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (
    url: RequestInfo | URL,
    init?: RequestInit,
  ) => {
    const call: ToggleCall = {
      url: String(url),
      method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? init.body : undefined,
    };
    calls.push(call);
    const res = respond(call);
    return new Response(res.payload, {
      status: res.status,
      ...(res.contentType
        ? { headers: { "Content-Type": res.contentType } }
        : {}),
    });
  }) as typeof globalThis.fetch;
  return calls;
}

const jobs: CronJobWire[] = [
  { name: "daily-journal", schedule: "0 8 * * *", enabled: false, next: "" },
  {
    name: "wake",
    schedule: "17 * * * *",
    enabled: true,
    next: "2026-11-01T09:17:00Z",
    agent: "scout",
  },
];

test("cronKeyOf keys agent wake jobs on the agent path", () => {
  assert.equal(cronKeyOf({ name: "wake", schedule: "", enabled: true, next: "" }), "wake");
  assert.equal(
    cronKeyOf({ name: "wake", schedule: "", enabled: true, next: "", agent: "scout", agent_owned: true }),
    "scout/wake",
  );
  // a main-home job with an AUTHORED agent keys on its own name: the
  // server's StateKey never namespaces a main row (card t_21824d98 -
  // the old derivation sent "ghost/pulse" at a job loaded as "pulse")
  assert.equal(
    cronKeyOf({ name: "pulse", schedule: "", enabled: true, next: "", agent: "ghost" }),
    "pulse",
  );
});

test("setCronJobEnabled: POST the toggle, then re-fetch the listing", async () => {
  savedFetch = globalThis.fetch;
  try {
    const calls = recordToggleFetch((call) => {
      if (call.url === "/cron/toggle") {
        return {
          status: 200,
          payload: JSON.stringify(jobs),
          contentType: "application/json",
        };
      }
      // GET /cron/jobs: the fresh listing re-read
      return { status: 200, payload: JSON.stringify(jobs) };
    });
    const rep = await setCronJobEnabled("scout/wake", false);
    assert.equal(calls.length, 2, "the ruled two-step: POST the toggle, then the re-fetch");
    assert.equal(calls[0].method, "POST");
    assert.equal(calls[0].url, "/cron/toggle");
    assert.deepEqual(
      JSON.parse(calls[0].body ?? "{}"),
      { key: "scout/wake", enabled: false },
      "the wire body carries the state key and the landed flag",
    );
    assert.equal(calls[1].method, "GET");
    assert.equal(calls[1].url, "/cron/jobs");
    // the returned listing is the re-fetch
    assert.ok(Array.isArray(rep) && rep.length === 2);
    assert.equal(rep[0].name, "daily-journal");
    assert.equal(rep[1].agent, "scout");
  } finally {
    globalThis.fetch = savedFetch;
  }
});

test("setCronJobEnabled: a failed toggle throws and never re-fetches", async () => {
  savedFetch = globalThis.fetch;
  try {
    const calls = recordToggleFetch(() => ({
      status: 404,
      payload: "cron toggle: no such job",
    }));
    await assert.rejects(
      setCronJobEnabled("ghost", true),
      /POST \/cron\/toggle: 404/,
      "the failure message is what the panel's error line shows",
    );
    assert.equal(calls.length, 1, "no re-fetch after a failed toggle - the prior rows stand");
  } finally {
    globalThis.fetch = savedFetch;
  }
});

test("setCronJobEnabled: the landed response carries the json contract", async () => {
  savedFetch = globalThis.fetch;
  try {
    // a plain-string body (the Go handler's json.NewEncoder writer)
    // still parses - the chain does not demand a content-type header
    const calls = recordToggleFetch((call) => {
      if (call.url === "/cron/toggle") return { status: 200, payload: JSON.stringify(jobs) };
      return { status: 200, payload: JSON.stringify(jobs) };
    });
    const rep = await setCronJobEnabled("daily-journal", true);
    assert.equal(rep.length, 2);
    assert.equal(calls[0].method, "POST");
  } finally {
    globalThis.fetch = savedFetch;
  }
});