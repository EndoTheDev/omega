// omega webui: unit tests for the Skills tab's SKILL EDITOR chain
// (card t_0a108f16, the CRUD capsule round). The behavior half runs at
// the helper level (the cronedit.test.ts recipe): a fetch-log probe
// drives saveSkill / deleteSkill / fetchSkillDraft, no SFC compile.
// The wire contract: POST /skills/save carries the whole draft and the
// response IS the fresh listing (one response carries the state it
// landed - no re-fetch round); a failed POST throws so the prior rows
// stand; the client validation mirrors the Go provider law EXACTLY -
// the editor never ships a skill the loader would refuse.

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  deleteSkill,
  emptyDraft,
  fetchSkillDraft,
  saveSkill,
  skillNameLaw,
  validateDraft,
  type SkillDraft,
} from "../../harness/skills/ui/edit.ts";

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

function recordFetch(respond: (call: SaveCall) => FetchResponse): SaveCall[] {
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
  { name: "probe", description: "probe skill" },
]);

function draft(over: Partial<SkillDraft> = {}): SkillDraft {
  return { ...emptyDraft(), name: "probe", description: "probe skill", content: "# probe\n\nbody\n", ...over };
}

// --- the wire contract ---

test("saveSkill POSTs the draft and the response is the fresh listing", async () => {
  const calls = recordFetch((call) => {
    if (call.url === "/skills/save") {
      return { status: 200, payload: LISTING, contentType: "application/json" };
    }
    return { status: 404, payload: "wrong lane" };
  });
  try {
    const rows = await saveSkill(draft());
    assert.equal(rows.length, 1);
    assert.equal(rows[0].name, "probe");
    const save = calls.find((c) => c.url === "/skills/save");
    assert.ok(save, "the save POST happened");
    assert.equal(save.method, "POST");
    const body = JSON.parse(save.body!) as Record<string, unknown>;
    assert.equal(body.name, "probe");
    assert.equal(body.description, "probe skill");
    assert.equal(body.content, "# probe\n\nbody\n");
    // one call on the wire: the save ANSWER IS the listing (the one
    // response carries the state it landed)
    assert.deepEqual(calls.map((c) => c.url), ["/skills/save"]);
  } finally {
    restoreFetch();
  }
});

test("saveSkill failure throws, no listing rides out of a refusal", async () => {
  const calls = recordFetch(() => ({ status: 400, payload: "save: description is required and must be one line" }));
  try {
    await assert.rejects(() => saveSkill(draft({ description: "" })), /POST \/skills\/save: 400/);
    assert.equal(calls.length, 1);
  } finally {
    restoreFetch();
  }
});

test("deleteSkill POSTs the name and lands the listing", async () => {
  const calls = recordFetch((call) => {
    if (call.url === "/skills/delete") {
      return { status: 200, payload: JSON.stringify([]), contentType: "application/json" };
    }
    return { status: 404, payload: "wrong lane" };
  });
  try {
    const rows = await deleteSkill("probe");
    assert.deepEqual(rows, []);
    const del = calls[0];
    assert.equal(del.method, "POST");
    assert.equal((JSON.parse(del.body!) as Record<string, unknown>).name, "probe");
  } finally {
    restoreFetch();
  }
});

test("deleteSkill unknown name throws with the loud 404", async () => {
  const calls = recordFetch(() => ({ status: 404, payload: "skills delete: skill \"ghost\" not found. Available: probe" }));
  try {
    await assert.rejects(() => deleteSkill("ghost"), /404/);
    assert.equal(calls.length, 1);
  } finally {
    restoreFetch();
  }
});

test("fetchSkillDraft prefills from GET /skills/content", async () => {
  const calls = recordFetch((call) => {
    if (call.url === "/skills/content?name=probe") {
      return {
        status: 200,
        payload: JSON.stringify({ name: "probe", description: "probe skill", content: "# body\n", truncated: false }),
        contentType: "application/json",
      };
    }
    return { status: 404, payload: "no such skill" };
  });
  try {
    const d = await fetchSkillDraft("probe");
    assert.equal(d.name, "probe");
    assert.equal(d.description, "probe skill");
    assert.equal(d.content, "# body\n");
    // the name rides the query, URI-escaped (a traversal name never
    // reaches the file system via the query string shape)
    assert.deepEqual(calls.map((c) => c.url), ["/skills/content?name=probe"]);
  } finally {
    restoreFetch();
  }
});

// --- the mirrored validation law ---

test("validateDraft mirrors the Go provider law", () => {
  assert.equal(validateDraft(draft()), "");
  assert.match(validateDraft(draft({ name: "" })), /name is required/);
  assert.match(validateDraft(draft({ name: "../evil" })), /single folder name/);
  assert.match(validateDraft(draft({ name: ".hidden" })), /dot/);
  assert.match(validateDraft(draft({ name: "a/b" })), /single folder name/);
  assert.match(validateDraft(draft({ description: "" })), /description is required/);
  assert.match(validateDraft(draft({ description: "two\nlines" })), /one line/);
  assert.match(validateDraft(draft({ content: "   " })), /body is required/);
  assert.match(validateDraft(draft({ content: "---\nsteals" })), /---/);
});

test("skillNameLaw names the folder law for the placeholder voice", () => {
  assert.equal(skillNameLaw("ok-name"), "");
  assert.notEqual(skillNameLaw("."), "");
  assert.notEqual(skillNameLaw("a\\b"), "");
  assert.notEqual(skillNameLaw(".x"), "");
});
