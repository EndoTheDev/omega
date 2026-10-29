// omega webui: the registry-browser SKILLS tab toggle behavior (v0.14.1,
// card t_372b9e9a). Pure-surface test like moduletoggle.test.ts: the
// helpers bind at the wire (GET /skills + GET/PUT /disabled), the
// source greps pin the component wiring (the composition-test
// convention - node --test has no DOM).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  normalizeDisabledPayload,
  withDisabledFlags,
  registryNote,
  filterRegistryRows,
  type RegistryRow,
} from "../app/composables/omega.ts";
import {
  refreshSkills,
  type SkillInfo,
} from "../../../modules/skills/ui/refresh.ts";

// module UI source lives at the repo root (engine/webui/test is three
// levels under it)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");
const read = (...p: string[]) => readFileSync(join(root, "engine", "webui", ...p), "utf8");

// --- normalizeDisabledPayload: a junk fold never kills the tab ---

test("disabled fold normalizer: lists pass through, junk collapses to []", () => {
  assert.deepEqual(
    normalizeDisabledPayload({ skills: ["a", "b"], tools: [], agents: ["main"] }),
    { skills: ["a", "b"], tools: [], agents: ["main"] },
  );
  // nulls and non-strings never ride the list
  assert.deepEqual(
    normalizeDisabledPayload({ skills: [null, 5, "a"] }),
    { skills: ["a"], tools: [], agents: [] },
  );
  // a junk payload is all-[]: the tab renders an empty fold, not a crash
  for (const junk of [null, 42, "x", [], { skills: "nope" }, { skills: [5] }]) {
    assert.deepEqual(normalizeDisabledPayload(junk), { skills: [], tools: [], agents: [] });
  }
});

// --- withDisabledFlags: the fold merges by name, fold-only names are ignored ---

test("skill rows merge the fold: disabled states land, inputs untouched", () => {
  const rows = withDisabledFlags(
    [
      { name: "skills", description: "read a skill" },
      { name: "dj-music", description: "manage the library" },
    ],
    ["dj-music"],
  );
  assert.equal(rows[0].disabled, false);
  assert.equal(rows[1].disabled, true);
  // the fold entry no row names is ignored (the server refuses
  // unknown-name disables, so this is dead fold state, not a phantom row)
  assert.equal(withDisabledFlags(rows, ["dj-music", "ghost"])[0].disabled, false);
});

// --- registryNote: the skills consequence names the prompt build ---

test("arming a skill disable names the prompt-build consequence; enable is silent", () => {
  assert.equal(registryNote("skills", "dj-music", false), "disable dj-music? excluded from the session prompt build");
  assert.equal(registryNote("skills", "dj-music", true), "");
});

// --- the tab renders the shared pattern and writes the shared wire ---

test("SkillsList consumes the shared component and the fold wire", () => {
  // module UI source lives at the repo root, engine/webui/test is three
  // levels under it
  const src = readFileSync(join(root, "modules", "skills", "ui", "SkillsList.vue"), "utf8");
  assert.ok(src.includes("<RegistryList"), "the shared component, not a bespoke list");
  assert.ok(src.includes("kind=\"skills\""), "the skills namespace rides to the note law");
  assert.ok(src.includes('from "./refresh"'), "the fetch chain rides the refresh helper (card t_ec57d5b8)");
  const refreshTs = readFileSync(join(root, "modules", "skills", "ui", "refresh.ts"), "utf8");
  assert.ok(refreshTs.includes('fetch("/skills")') && refreshTs.includes('fetch("/disabled")'), "self-contained inventory + fold fetch");
  assert.ok(!src.includes('fetch("/skills"'), "the inventory GET lives in the helper, not the SFC");
  assert.ok(src.includes("/disabled/skills"), "the toggle PUTs the skills namespace");
  assert.ok(src.includes("new Set(fold.value)"), "flags merge by name");
  assert.ok(!src.includes("composables/omega"), "no shell client dependency - module UI carries its own wiring");
  assert.ok(
    refreshTs.includes("status === 501") && refreshTs.includes("update.toggleable = false"),
    "a missing seam renders read-only (the miss rules live in the helper)",
  );
});

// t_504a78ca regression: RegistryList gates with `v-if="rows.length"`.
// rows as a plain function makes .length the ARITY (0) - data loads,
// the tab renders empty. The rows binding must hand RegistryList an
// array (computed, the AgentsPanel shape), not a function.
test("SkillsList builds rows as a computed (arity trap: a plain function rows() renders empty)", () => {
  const src = readFileSync(join(root, "modules", "skills", "ui", "SkillsList.vue"), "utf8");
  assert.ok(src.includes("const rows = computed("), "rows computed, so :rows unwraps to the array");
  assert.ok(!src.includes("function rows()"), "no plain-function row builder under :rows");
  assert.ok(src.includes(':rows="rows"'), "the shared component gets the rows binding");
});

// --- filterRegistryRows: the shared narrowing contract ---

test("the row filter narrows by name or description", () => {
  const rows: RegistryRow[] = [
    { name: "skills", description: "read a skill", disabled: false },
    { name: "dj-music", description: "Library management", disabled: true },
  ];
  assert.deepEqual(
    filterRegistryRows(rows, "dj").map((r) => r.name),
    ["dj-music"],
  );
  assert.deepEqual(
    filterRegistryRows(rows, "LIBRARY").map((r) => r.name),
    ["dj-music"],
  );
  assert.equal(filterRegistryRows(rows, "  ").length, 2);
});

// --- the refresh button (card t_ec57d5b8) --------------------------------
// The behavior half runs at the helper level (the mcp.test.ts recipe):
// a fetch-log probe drives refreshSkills - the same call chain the
// mount and the refresh click make. No SFC compile (node --test has
// no DOM).

// the fetch-log probe (the mcp.test.ts recipe): a stubbed fetch logs
// every call the helper makes, so the refresh click's chain is probed,
// never grepped.

let savedFetch: typeof globalThis.fetch;

interface ProbeCall {
  url: string;
  method: string;
}

function recordFetch(
  respond: (call: ProbeCall) => Response | Promise<Response>,
): ProbeCall[] {
  const calls: ProbeCall[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (
    url: RequestInfo | URL,
    init?: RequestInit,
  ) => {
    const call: ProbeCall = { url: String(url), method: init?.method ?? "GET" };
    calls.push(call);
    return respond(call);
  }) as typeof globalThis.fetch;
  return calls;
}

test("refreshSkills: the refresh click refetches; a post-mount skill shows and the fold stays", async () => {
  const round1 = {
    skills: [
      { name: "skills", description: "read a skill" },
      { name: "dj-music", description: "manage the library" },
    ] as SkillInfo[],
    fold: ["dj-music"],
  };
  // the refresh runs after a skill dir landed in skills/
  const round2 = {
    skills: [
      ...round1.skills,
      { name: "probe-skill", description: "dropped in post-mount" },
    ] as SkillInfo[],
    fold: ["dj-music"],
  };
  const queue = [
    new Response(JSON.stringify(round1.skills), { status: 200 }),
    new Response(JSON.stringify({ skills: round1.fold }), { status: 200 }),
    new Response(JSON.stringify(round2.skills), { status: 200 }),
    new Response(JSON.stringify({ skills: round2.fold }), { status: 200 }),
  ];
  let served = 0;
  const calls = recordFetch(() => queue[served++]);
  try {
    // the two refreshSkills() calls are the mount and the click - the
    // helper IS the chain, the SFC only wires the click
    const first = await refreshSkills();
    const second = await refreshSkills();
    assert.deepEqual(
      calls.map((c) => c.url),
      ["/skills", "/disabled", "/skills", "/disabled"],
      "each press is the GET /skills + /disabled pair",
    );
    assert.equal(second.skills.length, first.skills.length + 1);
    assert.equal(second.skills.at(-1)?.name, "probe-skill", "a skill added after mount shows on the refresh");
    assert.deepEqual(first.fold, ["dj-music"]);
    assert.deepEqual(second.fold, ["dj-music"], "the enabled/disabled fold stays across the refresh");
    assert.equal(second.toggleable, true);
  } finally {
    globalThis.fetch = savedFetch;
  }
});

// a missed /skills read throws so the panel's existing error line
// shows it and the current rows stand (nothing is applied).
test("refreshSkills: a refused /skills throws for the error line, the list stands", async () => {
  const saved = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL) =>
    new Response("{}", { status: String(url) === "/skills" ? 500 : 200 })) as typeof globalThis.fetch;
  try {
    await assert.rejects(refreshSkills, { message: "GET /skills: 500" });
  } finally {
    globalThis.fetch = saved;
  }
});

// the /disabled miss rules the fold lives by: a 501 (no seam) or an
// unreachable host render read-only and keep the shown fold; a plain
// non-501 refusal keeps both the fold and the toggle flag.
test("refreshSkills: a missed /disabled read never guesses the fold", async () => {
  const cases: Array<{
    name: string;
    status: number | "throw";
    fold: string[] | null;
    toggleable: boolean | null;
  }> = [
    { name: "501 no seam", status: 501, fold: null, toggleable: false },
    { name: "throw", status: "throw", fold: null, toggleable: false },
    { name: "503 refusal", status: 503, fold: null, toggleable: null },
    { name: "ok junk body", status: 200, fold: [], toggleable: true },
  ];
  const saved = globalThis.fetch;
  try {
    for (const c of cases) {
      globalThis.fetch = (async (url: RequestInfo | URL) => {
        const u = String(url);
        if (u === "/skills") return new Response("[]", { status: 200 });
        if (!u.startsWith("/disabled")) throw new Error(`unexpected url ${u}`);
        if (c.status === "throw") throw new TypeError("network gone");
        return new Response("{}", { status: c.status });
      }) as typeof globalThis.fetch;
      const update = await refreshSkills();
      assert.deepEqual(update.fold, c.fold, `${c.name}: the shown fold keeps`);
      assert.equal(update.toggleable, c.toggleable, `${c.name}: the toggle flag`);
    }
  } finally {
    globalThis.fetch = saved;
  }
});

// the render half (no DOM, the source greps pin it): the button rides
// the FilesTab toolbar pattern (card t_d6e94e79's mcp tab copy).
test("the skills tab refresh button renders the FilesTab toolbar pattern", () => {
  const src = readFileSync(
    join(root, "modules", "skills", "ui", "SkillsList.vue"),
    "utf8",
  );
  assert.ok(
    src.includes('role="toolbar"') && src.includes('aria-label="skills actions"'),
    "the single header row is the toolbar",
  );
  assert.ok(src.includes('aria-label="refresh skills"'), "the refresh action's accessible name");
  assert.ok(src.includes('title="refresh"'), "the hover title");
  assert.ok(
    src.includes(':disabled="refreshing"') && src.includes('@click="refresh"'),
    "the click fires the refresh chain",
  );
  assert.ok(src.includes("<Icon") && src.includes('name="refresh"'), "the iconset's refresh glyph (the membership law)");
});