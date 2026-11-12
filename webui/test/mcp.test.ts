// omega webui: the MCP tab (card t_c7136c50). Registry membership +
// pure row model + the source-wiring greps (the composition-test
// convention): the node suite never compiles the SFC, it pins the
// decisions - the panel fetches the module's own /mcp/servers, the
// empty state is contract, the registry entry is an inspector view.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  DOCK_COMPONENTS,
  DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  MENU_DOCKABLES,
  dockableOf,
} from "../app/composables/dockables.ts";
import { ICONS } from "../app/composables/icons.ts";
import {
  MCP_EMPTY_HINT,
  MCP_EMPTY_TEXT,
  serverEnabled,
  serverRows,
  statusDot,
  statusLabel,
  type McpServerWire,
} from "../../harness/mcp/ui/serverRows.ts";
import { reloadMcpServers } from "../../harness/mcp/ui/reload.ts";
import { setMcpServerEnabled } from "../../harness/mcp/ui/toggle.ts";

// module UI source lives at the repo root (webui/test is three
// levels under it)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...p: string[]) => readFileSync(join(...p), "utf8");

// --- the registry entry (the dock contract) -----------------------------

test("mcp is a registered inspector component: singleton, pinned right, unclosable", () => {
  assert.ok((DOCK_COMPONENTS as readonly string[]).includes("mcp"), "the union carries it");
  const d = dockableOf("mcp");
  assert.equal(d.title, "MCP");
  assert.equal(d.area, "right");
  assert.equal(d.policy, "singleton");
  assert.equal(d.closable, false, "unclosable like every inspector view");
  assert.ok(ICONS[d.icon], "the icon is an iconset name (the membership law)");
  assert.ok(MENU_DOCKABLES.includes("mcp"), "the '+' menu offers the tab");
  // card t_180db2cb: notifications joined the inspector after mcp - mcp
  // keeps its slot, the feed tab appended last (registry join order).
  assert.ok(
    INSPECTOR_TAB_COMPONENTS.includes("mcp"),
    "the inspector inventory carries it (joined at t_c7136c50; notifications joined after, t_180db2cb)",
  );
  // the icon key exists exactly once - one name one meaning
  const n = DOCKABLES.filter((x) => x.icon === "mcp").length;
  assert.equal(n, 1, "single registry entry owns the icon name");
});

// --- the pure row model (serverRows.ts) ---------------------------------

test("serverRows maps the wire: config order kept, non-array bodies stay empty", () => {
  assert.deepEqual(serverRows({}), [], "no body");
  assert.deepEqual(serverRows({ servers: "nope" as unknown as [] }), [], "shape junk stays empty");
  const wire = {
    source: "mcp.yaml",
    servers: [
      { name: "obsidian", transport: "http", connected: true, tools: ["obsidian.echo"] },
      { name: "dead", url: "http://x", connected: false, error: "initialize failed" },
    ],
  };
  const rows = serverRows(wire);
  assert.deepEqual(rows.map((r) => r.name), ["obsidian", "dead"], "config order IS the listing order");
});

test("statusLabel: the recorded failure wins, then the connection truth", () => {
  const s = (over: Partial<McpServerWire>): string => statusLabel({ name: "x", ...over });
  assert.equal(s({ connected: true }), "connected");
  assert.equal(s({ connected: false }), "not connected");
  assert.equal(s({ connected: true, error: "tools/list failed" }), "tools/list failed");
  assert.equal(s({ error: "" }), "not connected", "an empty error is no error");
});

test("the empty state is named in the helpers' voice (never composed ad hoc in the SFC)", () => {
  assert.equal(MCP_EMPTY_TEXT, "no MCP servers configured");
  assert.ok(MCP_EMPTY_HINT.includes("mcp.yaml"), "the hint names the config file");
  assert.ok(MCP_EMPTY_HINT.includes("MCP_SERVERS"), "the hint names the env var");
});

// --- the panel source wiring (the composition-test convention) ----------

test("McpServers consumes the module route and the shared row model", () => {
  const src = read(root, "harness", "mcp", "ui", "McpServers.vue");
  assert.ok(src.includes('fetch("/mcp/servers")'), "self-contained inventory fetch");
  assert.ok(src.includes("serverRows"), "the row model lives in the pure helper");
  assert.ok(src.includes("statusLabel"), "the status truth comes from the helper");
  assert.ok(src.includes("MCP_EMPTY_TEXT"), "the empty state text rides the constant");
  assert.ok(src.includes("MCP_EMPTY_HINT"), "the empty state hint rides the constant");
  assert.ok(!src.includes("composables/omega"), "no shell client dependency - module UI carries its own wiring");
  // the one write path is the refresh button's reload (card t_d6e94e79),
  // and it rides the helper - the SFC itself stays GET-only against the
  // read route; PUT exists nowhere in the module UI
  assert.ok(src.includes("reloadMcpServers"), "the refresh rides the reload helper");
  assert.ok(src.includes("setMcpServerEnabled"), "the per-row toggle rides the toggle helper (card t_83f37525)");
  const reloadTs = read(root, "harness", "mcp", "ui", "reload.ts");
  const toggleTs = read(root, "harness", "mcp", "ui", "toggle.ts");
  assert.ok(reloadTs.includes('method: "POST"') && reloadTs.includes('"/mcp/reload"'), "the reload helper POSTs the module's reload route");
  assert.ok(toggleTs.includes('"/mcp/toggle"') && toggleTs.includes("JSON.stringify"), "the toggle helper POSTs the named flag body (card t_83f37525)");
  assert.ok(!src.includes("method:") && !reloadTs.includes("PUT") && !toggleTs.includes("PUT"), "no PUT anywhere in the module UI");
  assert.ok(src.includes(".mcp-list.narrow {"), "its narrow rules ship in its own stylesheet");
});

// --- the reload behavior (card t_d6e94e79) -------------------------------
// The behavior half runs at the helper level (the filesroot.test.ts
// recipe): a fetch-log probe drives reloadMcpServers, no SFC compile.

let savedFetch: typeof globalThis.fetch;

interface ReloadCall {
  url: string;
  method: string;
}

function recordReloadFetch(
  respond: (call: ReloadCall) => Response | Promise<Response>,
): ReloadCall[] {
  const calls: ReloadCall[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (
    url: RequestInfo | URL,
    init?: RequestInit,
  ) => {
    const call: ReloadCall = { url: String(url), method: init?.method ?? "GET" };
    calls.push(call);
    return respond(call);
  }) as typeof globalThis.fetch;
  return calls;
}

test("reloadMcpServers: consume the POST body - no follow-up GET (v0.14.1 review lens)", async () => {
  savedFetch = globalThis.fetch;
  try {
    const calls = recordReloadFetch((_call) =>
      new Response(
        JSON.stringify({ source: "mcp.yaml", servers: [{ name: "echo", transport: "stdio", connected: true, tools: ["echo.echo"] }] }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    const rep = await reloadMcpServers();
    assert.equal(calls.length, 1, "the one round-trip: the POST body IS the fresh report, the re-fetch died");
    assert.equal(calls[0].method, "POST");
    assert.equal(calls[0].url, "/mcp/reload");
    // the returned listing IS the POST body (the fresh bridge's view)
    assert.equal(rep.source, "mcp.yaml");
    assert.ok(Array.isArray(rep.servers) && rep.servers[0].tools?.includes("echo.echo"), "the fresh server's tools ride the POST body");
  } finally {
    globalThis.fetch = savedFetch;
  }
});

test("reloadMcpServers: a failed reload throws and never re-fetches", async () => {
  savedFetch = globalThis.fetch;
  try {
    const calls = recordReloadFetch(() =>
      new Response("mcp reload: read config: parse config", { status: 500 }),
    );
    await assert.rejects(reloadMcpServers(), /POST \/mcp\/reload: 500/,
      "the failure message is what the panel's error line shows");
    assert.equal(calls.length, 1, "no re-fetch after a failed reload - the prior rows stand");
  } finally {
    globalThis.fetch = savedFetch;
  }
});

test("the refresh button renders the FilesTab toolbar pattern", () => {
  const src = read(root, "harness", "mcp", "ui", "McpServers.vue");
  // card t_308e6d9b: the single header row IS the shared TabBand now -
  // the toolbar role/aria-label are the band component's contract, the
  // panel composes the band with its title.
  const band = read(root, "webui", "app", "components", "ui", "TabBand.vue");
  assert.ok(band.includes('role="toolbar"') && band.includes("actions"), "the single header row is the toolbar (the shared band)");
  assert.match(src, /<TabBand[\s\S]{0,80}title="MCP">/, "the composes band carries the tab's title");
  assert.ok(src.includes('aria-label="refresh mcp servers"'), "the refresh action's accessible name");
  assert.ok(src.includes('title="refresh"'), "the hover title");
  assert.ok(src.includes(':disabled="reloading"') && src.includes('@click="reload"'), "the click fires the reload chain");
  assert.ok(src.includes('<Icon') && src.includes('name="refresh"'), "the iconset's refresh glyph (the membership law)");
});

test("the dockable declaration matches the registry voice", () => {
  const decl = JSON.parse(read(root, "harness", "mcp", "ui", "dockable.json"));
  assert.equal(decl.title, "MCP", "the runtime title matches the registry fallback");
  assert.equal(decl.icon, "mcp");
});

// --- the per-server toggle model (card t_83f37525) -----------------------

test("serverEnabled: the absent flag reads as on (old wires keep rendering)", () => {
  assert.equal(serverEnabled({ name: "x" }), true, "an undefined enabled flag is an enabled server");
  assert.equal(serverEnabled({ name: "x", enabled: true }), true);
  assert.equal(serverEnabled({ name: "x", enabled: false }), false);
});

test("statusLabel: the disabled state outranks the connection truth it caused", () => {
  const s = (over: Partial<McpServerWire>): string => statusLabel({ name: "x", ...over });
  assert.equal(s({ connected: true }), "connected");
  assert.equal(s({ connected: false }), "not connected");
  assert.equal(s({ connected: true, error: "tools/list failed" }), "tools/list failed");
  assert.equal(s({ error: "" }), "not connected", "an empty error is no error");
  assert.equal(s({ enabled: false, connected: false }), "disabled", "the skipped server says exactly that");
  assert.equal(
    s({ enabled: false, error: "initialize failed" }),
    "disabled",
    "a disabled server carries no failure - the skip is a config decision",
  );
});

test("statusDot: --ok rides a live connection, --err a recorded failure, dim otherwise", () => {
  const s = (over: Partial<McpServerWire>) => statusDot({ name: "x", ...over });
  assert.equal(s({ connected: true }), "ok");
  assert.equal(s({ connected: false }), "off", "not connected without a recorded failure is neutral");
  assert.equal(s({ error: "tools/list failed" }), "err");
  assert.equal(s({ enabled: false }), "off");
  assert.equal(s({ connected: true, error: "e" }), "err", "a recorded failure outranks the mount state");
});

// --- the toggle behavior (card t_83f37525) --------------------------------
// The fetch-log recipe (the reload behavior tests above): the helper
// drives the one round-trip, no SFC compile.

test("setMcpServerEnabled: consume the POST body - no follow-up GET (v0.14.1 review lens)", async () => {
  const calls: { url: string; method: string; body?: string }[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
    const call = { url: String(url), method: init?.method ?? "GET", body: init?.body == null ? undefined : String(init.body) };
    calls.push(call);
    return new Response(
      JSON.stringify({ source: "mcp.yaml", servers: [{ name: "echo", transport: "stdio", enabled: false, connected: false, tools: [] }] }),
      { status: 200 },
    );
  }) as typeof globalThis.fetch;
  try {
    const rep = await setMcpServerEnabled("echo", false);
    assert.equal(calls.length, 1, "the one round-trip: the POST body IS the fresh report, the re-fetch died");
    assert.equal(calls[0].method, "POST");
    assert.equal(calls[0].url, "/mcp/toggle");
    assert.equal(calls[0].body, JSON.stringify({ name: "echo", enabled: false }), "the body names the server and the target state");
    assert.equal(rep.source, "mcp.yaml");
    assert.equal(rep.servers?.[0]?.enabled, false, "the fresh listing carries the persisted flag");
  } finally {
    globalThis.fetch = savedFetch;
  }
});

test("setMcpServerEnabled: a refused write throws and never re-fetches", async () => {
  const calls: { url: string; method: string }[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
    calls.push({ url: String(url), method: init?.method ?? "GET" });
    // e.g. the MCP_SERVERS / mcp.json / no-config refusals at the wire
    return new Response("mcp toggle: config came from MCP_SERVERS - it is not a file, set the flag there", { status: 400 });
  }) as typeof globalThis.fetch;
  try {
    await assert.rejects(
      setMcpServerEnabled("echo", false),
      /POST \/mcp\/toggle: 400/,
      "the refusal is what the panel's error line shows",
    );
    assert.equal(calls.length, 1, "no re-fetch after a refused toggle - the prior rows stand");
  } finally {
    globalThis.fetch = savedFetch;
  }
});

// --- the visual rebuild (card t_83f37525) ---------------------------------
// The composition-test convention: the node suite never compiles the
// SFC, it pins the decisions. The clipping bug was the old list
// rendering tool names as fixed-width (22px) columns that wrapped mid-
// word with per-line clipping; the new shape is wrapped chips that
// ellipse per chip and NEVER clip vertically. These greps are the law
// that shape stays dead.

test("the clipping shape stays dead: no fixed-width tool columns, chips ellipse", () => {
  const src = read(root, "harness", "mcp", "ui", "McpServers.vue");
  assert.ok(!src.includes("width: 22px"), "the zero-width column root is gone");
  assert.ok(!/\.mcp-tool\s*\{[^}]*height:\s*\d+px/.test(src), "no fixed chip height - nothing can clip vertically");
  assert.match(src, /\.mcp-tool\s*\{[^}]*text-overflow:\s*ellipsis/, "each chip ellipses its own text instead of overflowing");
  assert.match(src, /\.mcp-tool\s*\{[^}]*max-width:\s*100%/, "a chip never exceeds its row (the pane-edge law)");
  assert.match(src, /\.mcp-tools\s*\{[^}]*flex-wrap:\s*wrap/, "the tool container wraps to new lines - no horizontal crush");
  // card t_561dc4e0: the row main no longer wraps its cluster - the row
  // law's head is the two-zone flex (name/subtitle stack left, ONE
  // control right) where NOTHING wraps: the name and the subtitle
  // ellipsize in the shared anatomy, the chips render below the row.
  assert.match(src, /class="mcp-row-main reg-row-head"/, "the row head rides the shared reg-row-head anatomy");
  const anatomy = read(root, "webui", "app", "assets", "css", "registry-row.css");
  assert.match(anatomy, /\.reg-row-name\s*\{[^}]*text-overflow:\s*ellipsis/, "the shared anatomy ellipsizes the name - nothing clips vertically");
  assert.match(anatomy, /\.reg-row-sub-text\s*\{[^}]*text-overflow:\s*ellipsis/, "the shared anatomy ellipsizes the subtitle");
});

test("the from-mcp.yaml caption is dead, the toggle is the shared pill (card t_d93864e7)", () => {
  const src = read(root, "harness", "mcp", "ui", "McpServers.vue");
  // the tab IS the mcp tab: a config-file provenance label is noise
  assert.ok(!src.includes("mcp-source"), "no source caption element left");
  assert.ok(!src.includes("sourceLabel"), "no dead source helper rides the component");
  // the row toggle is the shared pill - the look lives in ui/ once
  assert.match(src, /<TogglePill/, "the row toggle is the shared TogglePill");
  assert.ok(!src.includes(".mcp-toggle"), "no local toggle CSS block - one component, zero copies");
  // the two-step mechanic is DEAD (card t_5051d523: one click commits -
  // the arm step made every row need a double click); the write path
  // stays local to the module
  assert.ok(!src.includes("armed.value"), "no arm step: the first click commits");
  assert.ok(!src.includes("confirm-label"), "no confirm relabel on the shared pill");
  assert.ok(src.includes("/mcp/toggle"), "POST /mcp/toggle stays in the module");
});

test("the row speaks the design tokens: status dot, transport pill, no bare mono dump", () => {
  const src = read(root, "harness", "mcp", "ui", "McpServers.vue");
  assert.match(src, /\.mcp-dot-ok\s*\{[^}]*var\(--ok\)/, "the connected dot rides the ok token");
  assert.match(src, /\.mcp-dot-err\s*\{[^}]*var\(--err\)/, "the failure dot rides the err token");
  assert.match(src, /\.mcp-dot-off\s*\{[^}]*var\(--text-dim\)/, "the neutral dot rides the dim text token");
  assert.match(src, /class="mcp-pill"/, "the transport renders as a badge, not a bare string");
  assert.match(src, /\.mcp-pill\s*\{[^}]*var\(--mono\)/, "the pill speaks the mono token for its small print");
  // card t_561dc4e0: the name lead typography is the shared
  // .reg-row-name (the row law's line 1) - the template rides the
  // anatomy class, the shared file carries the semibold.
  assert.match(src, /class="mcp-name reg-row-name"/, "the server name rides the shared row-law name line");
  const anatomy = read(root, "webui", "app", "assets", "css", "registry-row.css");
  assert.match(anatomy, /\.reg-row-name\s*\{[^}]*font-weight:\s*600/, "the shared anatomy carries the semibold name line");
  assert.match(src, /serverEnabled\(s\)/, "the disabled row renders muted-but-visible in the template");
  assert.match(src, /\.mcp-off \.mcp-name/, "its muted styling ships in the stylesheet");
  // card t_561dc4e0: the state cluster rides the row law's subtitle
  // line (it never wraps off the row), so the old narrow reflow rule
  // died - narrow keeps the condensed scroll body + the anatomy's
  // collapsed-row padding.
  assert.match(src, /\.mcp-list\.narrow \.mcp-body \{/, "the narrow condensed body ships in its own stylesheet");
  assert.match(anatomy, /\.narrow \.reg-row/, "the shared anatomy carries the narrow row rhythm");
});

test("the shell mounts the panel: the outlet template rides app.vue, the route is GET-only", () => {
  const appvue = read(root, "webui", "app", "app.vue");
  assert.match(appvue, /#pane-mcp[\s\S]*?module="mcp"/, "the outlet template hosts the composition panel");
  const plugin = read(root, "harness", "mcp", "plugin.go");
  assert.ok(plugin.includes(`"GET /mcp/servers"`), "the go route pattern is registered in the mount");
  const status = read(root, "harness", "mcp", "status.go");
  assert.ok(
    !status.includes("MethodPut") && !status.includes("PUT"),
    "no write verb vocabulary",
  );
});