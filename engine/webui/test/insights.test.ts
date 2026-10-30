// omega webui: unit tests for the insights surface (card t_e1e61680).
// Two layers, matching the house patterns:
// - omega.ts fetchInsights: real fetch interception (URL shape + the
//   honest days semantics: null = bare /insights, 0 = all time);
// - InsightsTab.vue: the payload contract the renderer relies on and
//   the honest-empty rule (an empty window prints text, not charts).
// Run: node --test engine/webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import { fetchInsights } from "../app/composables/omega.ts";
import { INSIGHTS_DEFAULT_DAYS } from "../app/components/insights/defaultWindow.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "..", "..");

function read(...parts: string[]): string {
  return readFileSync(join(modules, ...parts), "utf8");
}

// --- fetchInsights: URL contract ---

test("fetchInsights with no arg requests the bare endpoint (server default window)", async () => {
  const seen: { url: string; init: RequestInit | undefined }[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL, init?: RequestInit) => {
    seen.push({ url: String(url), init });
    return Promise.resolve(new Response(
      JSON.stringify({
        period: "Last 30 days", period_start: "2026-09-22", period_end: "2026-10-22",
        days: 30, sessions: 2, messages: 5, user_messages: 3, tool_calls: 3,
        total_tokens: 18, avg_session_msgs: 2.5,
        tools: [{ name: "shell.run", count: 2 }],
        daily: [{ day: "Mon", count: 0 }], // short shape: the rest zeroed client-side is the server's job
        notable_msgs: { value: 4, detail: "first session" },
        notable_tokens: { value: 0, detail: "" },
        notable_tools: { value: 0, detail: "" },
      }),
      { status: 200, headers: { "content-type": "application/json" } },
    ));
  }) as typeof globalThis.fetch;
  try {
    const p = await fetchInsights();
    assert.equal(seen.length, 1);
    assert.equal(seen[0].url, "/insights");
    assert.equal(p.days, 30);
    assert.ok(seen[0].init === undefined || seen[0].init.method === undefined,
      "GET, not POST");
    assert.equal(p.tools[0].name, "shell.run");
    assert.equal(p.notable_msgs.value, 4);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("fetchInsights carries days: 0 = all time, N = explicit window", async () => {
  const urls: string[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL) => {
    urls.push(String(url));
    return Promise.resolve(new Response("{}", { status: 200, headers: { "content-type": "application/json" } }));
  }) as typeof globalThis.fetch;
  try {
    await fetchInsights(0);
    await fetchInsights(7);
    assert.deepEqual(urls, ["/insights?days=0", "/insights?days=7"]);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("fetchInsights throws the server's message, not a bare status", async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = (() => Promise.resolve(new Response(
    JSON.stringify({ error: "session store not configured" }),
    { status: 501, headers: { "content-type": "application/json" } },
  ))) as typeof globalThis.fetch;
  try {
    await assert.rejects(fetchInsights(), /session store not configured/);
  } finally {
    globalThis.fetch = realFetch;
  }
});

// --- InsightsTab.vue: the render contract ---

test("the view renders from the payload - every block the TUI prints has a home", () => {
  const vue = read("webui", "app", "components", "insights", "InsightsTab.vue");
  // one source: the omega fetch helper, no inline fetch
  assert.match(vue, /fetchInsights/, "the view fetches through omega.ts");
  assert.ok(!/fetch\(\s*["'`\/]/.test(vue), "no private fetch: the endpoint URL lives in omega.ts");
  // the TUI report's blocks, in the view
  assert.match(vue, /sessions/, "overview: sessions");
  assert.match(vue, /tool_calls/, "overview: tool calls");
  assert.match(vue, /total_tokens/, "overview: tokens");
  assert.match(vue, /avg_session_msgs/, "overview: average");
  assert.match(vue, /dailyRows/, "activity by day renders from the daily computed");
  assert.match(vue, /data\.tools/, "top tools renders from tools");
  assert.match(vue, /notable_msgs/, "notable sessions render");
  assert.match(vue, /notable_tokens/, "notable tokens render");
  // honest bars: CSS widths from counts, no chart dependency
  assert.match(vue, /:style="\{ width: barPct/, "day bars are CSS widths");
  assert.ok(!/^\s*import .*(?:chart|d3|echarts)/im.test(vue) && !vue.includes("<canvas"),
    "no chart dependency: CSS bars only");
});

test("an empty window is honest: text, zeroed payload, no fake rows", () => {
  const vue = read("webui", "app", "components", "insights", "InsightsTab.vue");
  // the sessions === 0 branch prints text (mirrors the TUI's
  // "[no sessions in the last N days]"), all-time gets its own voice
  assert.match(vue, /data\.sessions === 0/, "the empty branch exists");
  assert.match(vue, /no sessions in the last/, "windowed empty text");
  assert.match(vue, /no sessions yet/, "all-time empty text");
  // notables with value 0 never render a row (TUI parity: they are absent)
  assert.match(vue, /\.filter\(\(n\) => n\.row\.value > 0\)/, "zero notables are omitted");
});

test("the tab's initial window is 7 days, not the server default 30 (Endo 2026-10-29)", async () => {
  // the exported default constant is the tab's initial `days` value
  assert.equal(INSIGHTS_DEFAULT_DAYS, 7);
  // the initial ref is wired to the constant (behavior via the import,
  // not a literal in the SFC)
  const vue = read("webui", "app", "components", "insights", "InsightsTab.vue");
  assert.match(vue, /ref<number \| null>\(INSIGHTS_DEFAULT_DAYS\)/);
  // the bare-endpoint fold survives: load() still maps null -> no param
  assert.match(vue, /days\.value === null \? null : days\.value/);
});

test("the window selector rides the themed listbox and mirrors the TUI arg semantics", () => {
  const vue = read("webui", "app", "components", "insights", "InsightsTab.vue");
  assert.match(vue, /<BandSelect/, "BandSelect, not a native select (card t_acad02ec)");
  assert.match(vue, /value: 0, label: "all time"/, "0 = all time option");
  assert.match(vue, /value: 30, label: "30 days"/, "30 days option in the selector");
  assert.match(vue, /watch\(days, \(\) => load\(\)\)/, "a window change refetches");
});

test("the view's home is the secondary sidebar (card t_d7fd3ad3)", () => {
  const dockables = read("webui", "app", "composables", "dockables.ts");
  assert.match(dockables, /id: "insights"/, "registry entry exists");
  // the home ruling recorded where it lives: right shell, unclosable,
  // capital-I label - the default-visible inspector tab
  assert.match(
    dockables,
    /id: "insights",\n    title: "Insights",/,
    "the tab label reads Insights (capital I)",
  );
  assert.match(
    dockables,
    /\n    area: "right",\n    closable: false,\n  \},\n\];/s,
    "insights is the last registry entry, right-shell, unclosable",
  );
  assert.match(dockables, /card t_d7fd3ad3/, "the ruling is recorded at the entry");
  // the app outlet exists (the tab renders wherever it lives)
  const app = read("webui", "app", "app.vue");
  assert.match(app, /#pane-insights/, "the outlet template exists");
  assert.match(app, /<InsightsTab/, "the view mounts in the outlet");
  // the mobile drawer seats the shell-owned tab before the module
  // catch-all (InspectorPanel(module="insights") would render an empty
  // body - extensions' t_14f73742 ruling, insights joins it)
  assert.match(
    app,
    /inspectorDrawerActiveId === 'insights'/,
    "the mobile drawer has its insights branch",
  );
});