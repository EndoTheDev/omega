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
  sourceLabel,
  statusDot,
  statusLabel,
  type McpServerWire,
} from "../../../modules/mcp/ui/serverRows.ts";
import { reloadMcpServers } from "../../../modules/mcp/ui/reload.ts";
import { setMcpServerEnabled } from "../../../modules/mcp/ui/toggle.ts";

// module UI source lives at the repo root (engine/webui/test is three
// levels under it)
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");
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

test("sourceLabel echoes where the config came from; unknown falls to the safe line", () => {
  assert.equal(sourceLabel("env"), "MCP_SERVERS");
  assert.equal(sourceLabel("mcp.yaml"), "mcp.yaml");
  assert.equal(sourceLabel("mcp.json"), "mcp.json");
  assert.equal(sourceLabel("none"), "no MCP config found");
  assert.equal(sourceLabel(undefined), "no MCP config found");
  assert.equal(sourceLabel("weird"), "no MCP config found");
});

test("the empty state is named in the helpers' voice (never composed ad hoc in the SFC)", () => {
  assert.equal(MCP_EMPTY_TEXT, "no MCP servers configured");
  assert.ok(MCP_EMPTY_HINT.includes("mcp.yaml"), "the hint names the config file");
  assert.ok(MCP_EMPTY_HINT.includes("MCP_SERVERS"), "the hint names the env var");
});

// --- the panel source wiring (the composition-test convention) ----------

test("McpServers consumes the module route and the shared row model", () => {
  const src = read(root, "modules", "mcp", "ui", "McpServers.vue");
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
  const reloadTs = read(root, "modules", "mcp", "ui", "reload.ts");
  const toggleTs = read(root, "modules", "mcp", "ui", "toggle.ts");
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

test("reloadMcpServers: POST the reload, then re-fetch the listing", async () => {
  savedFetch = globalThis.fetch;
  try {
    const calls = recordReloadFetch((call) => {
      if (call.url === "/mcp/reload") {
        return new Response(
          JSON.stringify({ source: "mcp.yaml", servers: [{ name: "echo", transport: "stdio", connected: true }] }),
          { status: 200 },
        );
      }
      return new Response(
        JSON.stringify({ source: "mcp.yaml", servers: [{ name: "echo", transport: "stdio", connected: true, tools: ["echo.echo"] }] }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    });
    const rep = await reloadMcpServers();
    assert.equal(calls.length, 2, "the ruled two-step: POST the reload, then the re-fetch");
    assert.equal(calls[0].method, "POST");
    assert.equal(calls[0].url, "/mcp/reload");
    assert.equal(calls[1].method, "GET");
    assert.equal(calls[1].url, "/mcp/servers");
    // the returned listing is the re-fetch (the fresh bridge's view)
    assert.equal(rep.source, "mcp.yaml");
    assert.ok(Array.isArray(rep.servers) && rep.servers[0].tools?.includes("echo.echo"), "the fresh server's tools ride the listing");
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
  const src = read(root, "modules", "mcp", "ui", "McpServers.vue");
  // card t_308e6d9b: the single header row IS the shared TabBand now -
  // the toolbar role/aria-label are the band component's contract, the
  // panel composes the band with its title.
  const band = read(root, "engine", "webui", "app", "components", "ui", "TabBand.vue");
  assert.ok(band.includes('role="toolbar"') && band.includes("actions"), "the single header row is the toolbar (the shared band)");
  assert.match(src, /<TabBand[\s\S]{0,80}title="MCP">/, "the composes band carries the tab's title");
  assert.ok(src.includes('aria-label="refresh mcp servers"'), "the refresh action's accessible name");
  assert.ok(src.includes('title="refresh"'), "the hover title");
  assert.ok(src.includes(':disabled="reloading"') && src.includes('@click="reload"'), "the click fires the reload chain");
  assert.ok(src.includes('<Icon') && src.includes('name="refresh"'), "the iconset's refresh glyph (the membership law)");
});

test("the dockable declaration matches the registry voice", () => {
  const decl = JSON.parse(read(root, "modules", "mcp", "ui", "dockable.json"));
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
// drives the two-step chain, no SFC compile.

test("setMcpServerEnabled: POST the named flag, then re-fetch the listing", async () => {
  const calls: { url: string; method: string; body?: string }[] = [];
  savedFetch = globalThis.fetch;
  globalThis.fetch = (async (url: RequestInfo | URL, init?: RequestInit) => {
    const call = { url: String(url), method: init?.method ?? "GET", body: init?.body == null ? undefined : String(init.body) };
    calls.push(call);
    if (call.url === "/mcp/toggle") {
      return new Response(
        JSON.stringify({ source: "mcp.yaml", servers: [{ name: "echo", transport: "stdio", enabled: false, connected: false }] }),
        { status: 200 },
      );
    }
    return new Response(
      JSON.stringify({ source: "mcp.yaml", servers: [{ name: "echo", transport: "stdio", enabled: false, connected: false, tools: [] }] }),
      { status: 200 },
    );
  }) as typeof globalThis.fetch;
  try {
    const rep = await setMcpServerEnabled("echo", false);
    assert.equal(calls.length, 2, "the ruled two-step: POST the toggle, then the re-fetch");
    assert.equal(calls[0].method, "POST");
    assert.equal(calls[0].url, "/mcp/toggle");
    assert.equal(calls[0].body, JSON.stringify({ name: "echo", enabled: false }), "the body names the server and the target state");
    assert.equal(calls[1].method, "GET");
    assert.equal(calls[1].url, "/mcp/servers");
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
  const src = read(root, "modules", "mcp", "ui", "McpServers.vue");
  assert.ok(!src.includes("width: 22px"), "the zero-width column root is gone");
  assert.ok(!/\.mcp-tool\s*\{[^}]*height:\s*\d+px/.test(src), "no fixed chip height - nothing can clip vertically");
  assert.match(src, /\.mcp-tool\s*\{[^}]*text-overflow:\s*ellipsis/, "each chip ellipses its own text instead of overflowing");
  assert.match(src, /\.mcp-tool\s*\{[^}]*max-width:\s*100%/, "a chip never exceeds its row (the pane-edge law)");
  assert.match(src, /\.mcp-tools\s*\{[^}]*flex-wrap:\s*wrap/, "the tool container wraps to new lines - no horizontal crush");
  assert.match(src, /\.mcp-row-main\s*\{[^}]*flex-wrap:\s*wrap/, "the row main line wraps its cluster too");
});

test("the source caption is its own line, never overlapped", () => {
  const src = read(root, "modules", "mcp", "ui", "McpServers.vue");
  assert.match(src, /class="mcp-source">from \{\{ sourceLabel\(source\) \}\}/, "the source echo renders in its own caption element");
  // no negative margins riding the caption (the half-covered bug)
  assert.ok(!/\.mcp-source\s*\{[^}]*margin-?(top|bottom)?:\s*-/.test(src), "the caption never pulls itself under the rows");
});

test("the row speaks the design tokens: status dot, transport pill, no bare mono dump", () => {
  const src = read(root, "modules", "mcp", "ui", "McpServers.vue");
  assert.match(src, /\.mcp-dot-ok\s*\{[^}]*var\(--ok\)/, "the connected dot rides the ok token");
  assert.match(src, /\.mcp-dot-err\s*\{[^}]*var\(--err\)/, "the failure dot rides the err token");
  assert.match(src, /\.mcp-dot-off\s*\{[^}]*var\(--text-dim\)/, "the neutral dot rides the dim text token");
  assert.match(src, /class="mcp-pill"/, "the transport renders as a badge, not a bare string");
  assert.match(src, /\.mcp-pill\s*\{[^}]*var\(--mono\)/, "the pill speaks the mono token for its small print");
  assert.match(src, /\.mcp-name\s*\{[^}]*font-weight:\s*600/, "the server name is the semibold row lead");
  assert.match(src, /serverEnabled\(s\)/, "the disabled row renders muted-but-visible in the template");
  assert.match(src, /\.mcp-off \.mcp-name/, "its muted styling ships in the stylesheet");
  assert.match(src, /\.mcp-list\.narrow \.mcp-state/, "the narrow rules reflow the state line");
});

test("the shell mounts the panel: the outlet template rides app.vue, the route is GET-only", () => {
  const appvue = read(root, "engine", "webui", "app", "app.vue");
  assert.match(appvue, /#pane-mcp[\s\S]*?module="mcp"/, "the outlet template hosts the composition panel");
  const plugin = read(root, "modules", "mcp", "plugin.go");
  assert.ok(plugin.includes(`"GET /mcp/servers"`), "the go route pattern is registered in the mount");
  const status = read(root, "modules", "mcp", "status.go");
  assert.ok(
    !status.includes("MethodPut") && !status.includes("PUT"),
    "no write verb vocabulary",
  );
});