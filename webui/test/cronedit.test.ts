// omega webui: unit tests for the Cron tab's JOB EDITOR chain (card
// t_ecec864e). The behavior half runs at the helper level (the
// crontoggle.test.ts recipe): a fetch-log probe drives saveCronJob and
// deleteCronJob, no SFC compile. The wire contract: POST /cron/save
// carries the WHOLE job definition and the response is re-fetched into
// the fresh listing (one response carries the state it landed); a
// failed POST throws before any re-fetch so the prior rows stand; the
// client validation mirrors the Go schema law EXACTLY - the editor
// never ships a job the loader would reject.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  DELIVER_TARGETS,
  cadenceOf,
  deleteCronJob,
  draftOf,
  editableOf,
  emptyDraft,
  jobMeta,
  lastMeta,
  parseSkills,
  saveCronJob,
  statusStamp,
  validateDraft,
  type JobDraft,
} from "../../harness/cron/ui/edit.ts";

let savedFetch: typeof globalThis.fetch;

interface SaveCall {
  url: string;
  method: string;
  body?: string;
}

interface FetchResponse {
  status: number;
  payload: string;
  contentType?: string;
}

function recordFetch(
  respond: (call: SaveCall) => FetchResponse,
): SaveCall[] {
  const calls: SaveCall[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (
    url: RequestInfo | URL,
    init?: RequestInit,
  ) => {
    const call: SaveCall = {
      url: String(url),
      method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? init.body : undefined,
    };
    calls.push(call);
    const r = respond(call);
    return new Response(r.payload, {
      status: r.status,
      headers: r.contentType === undefined ? {} : { "Content-Type": r.contentType },
    });
  }) as unknown as typeof fetch;
  return calls;
}

function restoreFetch() {
  globalThis.fetch = savedFetch;
}

const LISTING = JSON.stringify([
  { name: "one", kind: "cron", schedule: "0 8 * * *", deliver: "new-session", enabled: true, next: "" },
]);

function draft(over: Partial<JobDraft> = {}): JobDraft {
  return { ...emptyDraft(), name: "probe", schedule: "0 8 * * *", prompt: "p", ...over };
}

// --- the wire contract ---

test("saveCronJob POSTs the whole job and answers the fresh listing", async () => {
  const calls = recordFetch((call) => {
    if (call.url === "/cron/save") {
      return { status: 200, payload: JSON.stringify([{ name: "probe", enabled: true, next: "" }]), contentType: "application/json" };
    }
    if (call.url === "/cron/jobs") return { status: 200, payload: LISTING, contentType: "application/json" };
    return { status: 404, payload: "wrong lane" };
  });
  try {
    const d = draft({ minutes: "", schedule: "*/5 * * * *" });
    const jobs = await saveCronJob(d);
    assert.equal(jobs.length, 1);
    const save = calls.find((c) => c.url === "/cron/save");
    assert.ok(save, "the save POST happened");
    assert.equal(save.method, "POST");
    const body = JSON.parse(save.body!) as Record<string, unknown>;
    // the FULL definition rides the wire - the server round-trips it
    // through the loader's own ParseConfig before a byte lands
    assert.equal(body.name, "probe");
    assert.equal(body.kind, "cron");
    assert.equal(body.schedule, "*/5 * * * *");
    assert.equal(body.prompt, "p");
    assert.equal(body.deliver, "new-session");
    assert.equal(body.enabled, true);
    // the two-call chain: save -> jobs re-fetch (the toggle contract)
    assert.deepEqual(calls.map((c) => c.url), ["/cron/save", "/cron/jobs"]);
    // minutes and schedule are kind-exclusive on the wire
    const d2 = draft({ kind: "interval", minutes: "7", schedule: "0 8 * * *" });
    await saveCronJob(d2);
    const body2 = JSON.parse(calls.filter((c) => c.url === "/cron/save")[1]!.body!) as Record<string, unknown>;
    assert.equal(body2.minutes, 7);
    assert.equal(body2.schedule, "");
  } finally {
    restoreFetch();
  }
});

test("saveCronJob throws with the server detail and never re-fetches on failure", async () => {
  const calls = recordFetch(() => ({ status: 400, payload: "cron save: interval jobs need minutes >= 1" }));
  try {
    await assert.rejects(
      () => saveCronJob(draft({ kind: "interval", minutes: "0" })),
      /POST \/cron\/save: 400 - cron save: interval jobs need minutes >= 1/,
    );
    assert.equal(calls.length, 1, "a failed save must not re-fetch the listing");
  } finally {
    restoreFetch();
  }
});

test("deleteCronJob POSTs the name and answers the fresh listing; unknown is loud", async () => {
  let respond: (call: SaveCall) => FetchResponse = () =>
    ({ status: 200, payload: JSON.stringify([]), contentType: "application/json" });
  const calls = recordFetch((call) => respond(call));
  try {
    const jobs = await deleteCronJob("probe");
    assert.deepEqual(jobs, []);
    assert.equal(calls[0].url, "/cron/delete");
    assert.equal(JSON.parse(calls[0].body!).name, "probe");
    assert.equal(calls[1].url, "/cron/jobs");
    // the 404 lane: a no-op delete is visible, never silent
    respond = () => ({ status: 404, payload: "cron delete: no job \"ghost\" in the loaded cron set" });
    await assert.rejects(() => deleteCronJob("ghost"), /POST \/cron\/delete: 404/);
  } finally {
    restoreFetch();
  }
});

// --- the client-side schema law (mirrors harness/cron validateJob) ---

test("validateDraft mirrors the Go schema law exactly", () => {
  assert.equal(validateDraft(draft()), "");
  assert.equal(validateDraft(draft({ kind: "interval", minutes: "5", schedule: "" })), "");
  const cases: Array<[JobDraft, string]> = [
    [draft({ name: "" }), "name is required"],
    [draft({ name: "Bad_Name" }), "name must match [a-z0-9-]"],
    [draft({ kind: "interval", minutes: "0", schedule: "" }), "interval jobs need minutes >= 1"],
    [draft({ kind: "interval", minutes: "5", schedule: "0 8 * * *" }), "interval jobs take minutes, not schedule"],
    [draft({ kind: "cron", minutes: "5" }), "cron jobs take schedule, not minutes"],
    [draft({ schedule: "" }), "schedule is required"],
    [draft({ prompt: " " }), "prompt is required"],
    [draft({ deliver: "smoke-signal" }), "deliver must be"],
    [draft({ file: "notes/x.md" }), "file only applies to deliver append-to-file"],
    [draft({ kind: "hourly" }), "kind must be cron or interval"],
  ];
  for (const [d, want] of cases) {
    assert.ok(validateDraft(d).includes(want), `validateDraft(${JSON.stringify(d)}) = ${validateDraft(d)}, want it to name ${want}`);
  }
});

// --- the scope law + round-trips ---

test("editableOf keeps agent-home wake rows read-only", () => {
  assert.equal(editableOf({ name: "x", schedule: "", enabled: true, next: "" }), true);
  assert.equal(editableOf({ name: "x", schedule: "", enabled: true, next: "", agent: "scout" }), false);
});

test("draftOf round-trips the wire row into the form shape", () => {
  const d = draftOf({
    name: "pulse",
    kind: "interval",
    schedule: "",
    minutes: 15,
    deliver: "continuous-agent",
    model: "glm-5.3",
    file: "",
    enabled: false,
    next: "",
  } as never);
  assert.equal(d.kind, "interval");
  assert.equal(d.minutes, "15");
  assert.equal(d.deliver, "continuous-agent");
  assert.equal(d.model, "glm-5.3");
  assert.equal(d.enabled, false);
  // a legacy row (no kind on the wire) drafts as cron
  const legacy = draftOf({ name: "old", schedule: "0 8 * * *", enabled: true, next: "" } as never);
  assert.equal(legacy.kind, "cron");
});

test("emptyDraft seeds the schema's own defaults", () => {
  const d = emptyDraft();
  assert.equal(d.kind, "cron");
  assert.equal(d.deliver, "new-session");
  assert.equal(d.enabled, true);
  assert.deepEqual(DELIVER_TARGETS, ["new-session", "continuous-agent", "append-to-file"]);
});

// --- the row voice (pure helpers the SFC renders) ---

test("cadenceOf speaks kind-aware cadence", () => {
  assert.equal(cadenceOf({ name: "x", kind: "cron", schedule: "0 8 * * *", enabled: true, next: "" } as never), "0 8 * * *");
  assert.equal(cadenceOf({ name: "x", kind: "interval", minutes: 5, enabled: true, next: "" } as never), "5m interval");
  // legacy rows without kind default like the schema does
  assert.equal(cadenceOf({ name: "x", schedule: "0 6 * * *", enabled: true, next: "" } as never), "0 6 * * *");
});

test("jobMeta renders only the non-default fields", () => {
  const plain = { name: "x", schedule: "", enabled: true, next: "" };
  assert.equal(jobMeta(plain as never), "", "a fully-default job carries no meta noise");
  assert.equal(
    jobMeta({ ...plain, deliver: "append-to-file", file: "notes/r.md", model: "m" } as never),
    " - append-to-file, model m, file notes/r.md",
  );
  assert.equal(
    jobMeta({ ...plain, agent: "scout" } as never),
    " - agent scout",
  );
  // t_3885c975: pinned skills speak in the meta line (fragment only;
  // a plain job stays quiet)
  assert.equal(jobMeta({ ...plain, skills: ["a", "b"] } as never), " - skills a, b");
});

// --- the last-fire status voice (card t_3885c975) ---

// A local RFC3339 stamp the formatter re-renders in the same (host)
// zone: built from local date parts, carried through UTC, so the
// asserted output is one shape on every lane.
function localRfc3339(y: number, mo: number, d: number, h: number, mi: number): string {
  return new Date(y, mo - 1, d, h, mi).toISOString();
}

test("lastMeta speaks the last fire's outcome", () => {
  const quiet = { name: "x", schedule: "", enabled: true, next: "" };
  // the never-fired row stays quiet - "-" means no fire, silence here
  // is the same truth
  assert.equal(lastMeta(quiet as never), "");
  const at = localRfc3339(2026, 11, 7, 2, 14);
  assert.equal(
    lastMeta({ ...quiet, status: "ok", last: at } as never),
    " - last ok 11/07 02:14",
  );
  assert.equal(
    lastMeta({ ...quiet, status: "error", last: at, status_note: "provider exploded" } as never),
    ` - last error 11/07 02:14: provider exploded`,
  );
});

test("lastMeta caps a long error note - the row stays a row", () => {
  const at = localRfc3339(2026, 11, 7, 2, 14);
  const long = "x".repeat(300);
  const out = lastMeta({ name: "x", schedule: "", enabled: true, next: "", status: "error", last: at, status_note: long });
  assert.ok(out.includes("last error 11/07 02:14: "), out);
  assert.ok(out.endsWith("..."), "the note ends with the ellipsis");
  // 120 chars + stamp + voice + ellipsis - the displayed note is the cap
  assert.ok(out.length < 300, `row line length ${out.length} blew past the cap`);
});

test("statusStamp renders local compact stamps, verbatim on junk", () => {
  assert.equal(statusStamp(localRfc3339(2026, 11, 7, 2, 14)), "11/07 02:14");
  assert.equal(statusStamp("not-a-time"), "not-a-time");
});

// --- the fire-pinned skills (card t_3885c975) ---

test("parseSkills mirrors the Go normalizeSkills law", () => {
  assert.deepEqual(parseSkills("  omega-dev , ponytail,,grill-me  "), ["omega-dev", "ponytail", "grill-me"]);
  assert.deepEqual(parseSkills(""), []);
  assert.deepEqual(parseSkills("   "), []);
});

test("validateDraft pins the skills law (names, no duplicates)", () => {
  const cases: Array<[JobDraft, string]> = [
    [draft({ skills: "omega-dev, ponytail" }), ""],
    [draft({ skills: "Bad_Name" }), "skill names must match [a-z0-9-]"],
    [draft({ skills: "twice, twice" }), "duplicate skill twice"],
    [draft({ skills: "  " }), ""], // empty list is the default
  ];
  for (const [d, want] of cases) {
    assert.equal(validateDraft(d), want, `validateDraft(skills=${d.skills})`);
  }
});

test("saveCronJob carries the skills list on the wire", async () => {
  const calls = recordFetch((call) => {
    if (call.url === "/cron/save") return { status: 200, payload: "[]", contentType: "application/json" };
    if (call.url === "/cron/jobs") return { status: 200, payload: LISTING, contentType: "application/json" };
    return { status: 404, payload: "wrong lane" };
  });
  try {
    await saveCronJob(draft({ skills: "omega-dev, ponytail" }));
    const body = JSON.parse(calls.find((c) => c.url === "/cron/save")!.body!) as Record<string, unknown>;
    assert.deepEqual(body.skills, ["omega-dev", "ponytail"]);
  } finally {
    restoreFetch();
  }
});

test("draftOf round-trips the pinned skills into the editor voice", () => {
  const d = draftOf({ name: "x", schedule: "0 8 * * *", enabled: true, next: "", skills: ["a", "b"] } as never);
  assert.equal(d.skills, "a, b");
  // a row with no pins drafts an empty input
  const bare = draftOf({ name: "y", schedule: "0 8 * * *", enabled: true, next: "" } as never);
  assert.equal(bare.skills, "");
});

test("draftOf carries the listing row's prompt - editing a job never loses the message (t_3885c975 review fix)", () => {
  const d = draftOf({ name: "x", schedule: "0 8 * * *", enabled: true, next: "", prompt: "the authored message" } as never);
  assert.equal(d.prompt, "the authored message");
  // a row with no prompt drafts empty (create flow)
  const bare = draftOf({ name: "y", schedule: "0 8 * * *", enabled: true, next: "" } as never);
  assert.equal(bare.prompt, "");
});