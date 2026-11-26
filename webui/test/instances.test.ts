// omega webui: the connection list's client half. Three layers, matching the house
// patterns: the fetch wrapper (URL/method under fetch interception), the pure helpers
// (normalize, upsert, remove, the cache round-trip and the failed-fetch floor), and
// source pins that the STATUS BAR carries the control and its list - the menubar pill
// and its add form are gone, and settings is the one write surface.
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  INSTANCE_CACHE_KEY,
  INSTANCE_CHANGED_EVENT,
  INSTANCE_EVENT,
  MAX_INSTANCES,
  fetchInstances,
  instancesAreAppOwned,
  lastUsedInstance,
  loadInstances,
  navigateToInstance,
  normalizeInstanceName,
  normalizeInstanceURL,
  onInstancesReply,
  readInstanceCache,
  removeInstance,
  requestInstances,
  saveInstancesInApp,
  setLastUsedInstance,
  switchInstancesInApp,
  upsertInstance,
  writeInstanceCache,
  type InstanceStorage,
  type SavedInstance,
} from "../app/composables/instances.ts";
import { NARROW_MAX_PX } from "../app/composables/narrow.ts";

const here = dirname(fileURLToPath(import.meta.url));
const modules = join(here, "..", "..");

function read(...parts: string[]): string {
  return readFileSync(join(modules, ...parts), "utf8");
}

function fakeStorage(seed: Record<string, string> = {}): InstanceStorage & { readonly map: Record<string, string> } {
  const map: Record<string, string> = { ...seed };
  return {
    map,
    getItem(key: string) {
      return Object.prototype.hasOwnProperty.call(map, key) ? map[key] : null;
    },
    setItem(key: string, value: string) {
      map[key] = value;
    },
  };
}

function row(name: string, url: string): SavedInstance {
  return { name, url };
}

// fetch wrappers: URL + method + body contract

test("fetchInstances GETs /instances and unwraps the envelope", async () => {
  const seen: { url: string; method: string }[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL, init?: RequestInit) => {
    seen.push({ url: String(url), method: init?.method ?? "GET" });
    return Promise.resolve(new Response(JSON.stringify({ instances: [row("laptop", "http://127.0.0.1:8099")] }), {
      status: 200,
      headers: { "content-type": "application/json" },
    }));
  }) as typeof globalThis.fetch;
  try {
    const list = await fetchInstances();
    assert.equal(seen.length, 1);
    assert.equal(seen[0].url, "/instances");
    assert.equal(seen[0].method, "GET");
    assert.deepEqual(list, [row("laptop", "http://127.0.0.1:8099")]);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("a refused fetch throws instead of reading as an empty list", async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = (() =>
    Promise.resolve(new Response("instances unavailable\n", { status: 501 }))) as typeof globalThis.fetch;
  try {
    await assert.rejects(() => fetchInstances(), /instances: 501/);
  } finally {
    globalThis.fetch = realFetch;
  }
});

// normalizeInstanceURL

test("normalizeInstanceURL trims the trailing slash and lowercases the scheme", () => {
  assert.equal(normalizeInstanceURL("HTTP://127.0.0.1:8099/"), "http://127.0.0.1:8099");
  assert.equal(normalizeInstanceURL("  https://omega.example.test/base/  "), "https://omega.example.test/base");
  assert.equal(normalizeInstanceURL("http://127.0.0.1:8099"), "http://127.0.0.1:8099");
});

test("normalizeInstanceURL takes the bare host:port shorthand", () => {
  assert.equal(normalizeInstanceURL("127.0.0.1:8099"), "http://127.0.0.1:8099");
  assert.equal(normalizeInstanceURL("localhost:8099"), "http://localhost:8099");
});

test("normalizeInstanceURL rejects non-http schemes and spaces", () => {
  assert.throws(() => normalizeInstanceURL("javascript:alert(1)"), /only http and https/);
  assert.throws(() => normalizeInstanceURL("ftp://127.0.0.1:8099"), /only http and https/);
  assert.throws(() => normalizeInstanceURL("file:///etc/passwd"), /only http and https/);
  assert.throws(() => normalizeInstanceURL("http://127.0.0.1:80 99"), /cannot contain spaces/);
  assert.throws(() => normalizeInstanceURL("   "), /required/);
});

test("normalizeInstanceName trims and bounds the name", () => {
  assert.equal(normalizeInstanceName("  laptop  "), "laptop");
  assert.throws(() => normalizeInstanceName("   "), /name is required/);
  assert.throws(() => normalizeInstanceName("n".repeat(65)), /at most 64/);
  assert.equal(normalizeInstanceName("n".repeat(64)).length, 64);
});

// upsert / remove

test("upsertInstance replaces by URL in place, else appends", () => {
  const list = [row("laptop", "http://a.test"), row("pi", "http://b.test")];
  const edited = upsertInstance(list, row("work laptop", "http://a.test"));
  assert.deepEqual(edited, [row("work laptop", "http://a.test"), row("pi", "http://b.test")]);
  const added = upsertInstance(list, row("desk", "http://c.test"));
  assert.deepEqual(added, [row("laptop", "http://a.test"), row("pi", "http://b.test"), row("desk", "http://c.test")]);
  // the input list is never mutated
  assert.deepEqual(list, [row("laptop", "http://a.test"), row("pi", "http://b.test")]);
});

test("removeInstance drops exactly the row with that URL", () => {
  const list = [row("laptop", "http://a.test"), row("pi", "http://b.test")];
  assert.deepEqual(removeInstance(list, "http://a.test"), [row("pi", "http://b.test")]);
  assert.deepEqual(removeInstance(list, "http://missing.test"), list);
});

// the cache

test("cache round-trips the list and lastUsed through the injected storage", () => {
  const storage = fakeStorage();
  assert.deepEqual(readInstanceCache(storage), { list: [], lastUsed: "" });

  writeInstanceCache({ list: [row("pi", "http://b.test")], lastUsed: "http://b.test" }, storage);
  assert.ok(storage.map[INSTANCE_CACHE_KEY], "the cache rides the omega.instances key");
  assert.deepEqual(readInstanceCache(storage), { list: [row("pi", "http://b.test")], lastUsed: "http://b.test" });
  assert.equal(lastUsedInstance(storage), "http://b.test");
});

test("a corrupt or absent cache reads as empty, never as a crash", () => {
  assert.deepEqual(readInstanceCache(fakeStorage({ [INSTANCE_CACHE_KEY]: "{not json" })), { list: [], lastUsed: "" });
  assert.deepEqual(readInstanceCache(fakeStorage({ [INSTANCE_CACHE_KEY]: JSON.stringify({ list: [{ name: "", url: 5 }] }) })), {
    list: [],
    lastUsed: "",
  });
  assert.deepEqual(readInstanceCache(null), { list: [], lastUsed: "" });
  // a blocked store never throws on write
  writeInstanceCache({ list: [], lastUsed: "" }, null);
});

test("setLastUsedInstance keeps the list beside the new mark", () => {
  const storage = fakeStorage();
  writeInstanceCache({ list: [row("pi", "http://b.test")], lastUsed: "" }, storage);
  setLastUsedInstance("http://b.test", storage);
  assert.deepEqual(readInstanceCache(storage), { list: [row("pi", "http://b.test")], lastUsed: "http://b.test" });
});

test("a failed fetch keeps the cache and reports the offline list", async () => {
  const storage = fakeStorage();
  writeInstanceCache({ list: [row("pi", "http://b.test")], lastUsed: "http://b.test" }, storage);

  const realFetch = globalThis.fetch;
  globalThis.fetch = (() => Promise.reject(new Error("network down"))) as typeof globalThis.fetch;
  try {
    const loaded = await loadInstances(storage);
    assert.equal(loaded.offline, true);
    assert.deepEqual(loaded.list, [row("pi", "http://b.test")]);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("a successful origin fetch never rewrites the app's cache", async () => {
  const storage = fakeStorage();
  writeInstanceCache({ list: [row("app", "http://127.0.0.1:18101")], lastUsed: "http://old.test" }, storage);

  let fetched = 0;
  const realFetch = globalThis.fetch;
  globalThis.fetch = (() => {
    fetched++;
    return Promise.resolve(new Response(JSON.stringify({ instances: [row("remote", "http://192.0.2.10:8098")] }), { status: 200 }));
  }) as typeof globalThis.fetch;
  try {
    const loaded = await loadInstances(storage);
    assert.equal(loaded.offline, false);
    assert.equal(fetched, 1, "a browser tab reads its own origin");
    assert.deepEqual(loaded.list, [row("remote", "http://192.0.2.10:8098")], "the panel shows what this serve answers");
    assert.deepEqual(
      readInstanceCache(storage),
      { list: [row("app", "http://127.0.0.1:18101")], lastUsed: "http://old.test" },
      "the app's list is not the origin fetch's to overwrite - the cache is a copy of its own source",
    );
  } finally {
    globalThis.fetch = realFetch;
  }
});

// --- the desktop host's channel: the app owns the list -----------------------------

// fakeDesktopHost installs a Wails shim on globalThis whose invoke records the raw body
// and answers through the same dispatchWailsEvent entry point the real shim delivers on.
function fakeDesktopHost(answer?: (message: Record<string, unknown>) => unknown) {
  const sent: Record<string, unknown>[] = [];
  const api = {
    environment: { OS: "windows" },
    invoke(message: string) {
      const body = JSON.parse(message) as Record<string, unknown>;
      sent.push(body);
      const reply = answer?.(body);
      if (reply) api.dispatchWailsEvent({ name: INSTANCE_EVENT, data: reply });
    },
    dispatchWailsEvent(_event: { name?: unknown; data?: unknown }) {
      // replaced by the composable's own hook on first use
    },
  };
  (globalThis as unknown as { _wails?: unknown })._wails = api;
  return { sent, api };
}

function clearDesktopHost(): void {
  delete (globalThis as unknown as { _wails?: unknown })._wails;
}

test("a desktop window takes the app's list, never the active server's", async () => {
  const storage = fakeStorage();
  const app = [row("main", "http://127.0.0.1:18101"), row("pi", "http://192.0.2.10:8098")];
  const { sent } = fakeDesktopHost((body) => (body.omega === "instances:get" ? { instances: app, error: "" } : null));
  let fetched = 0;
  const realFetch = globalThis.fetch;
  globalThis.fetch = (() => {
    fetched++;
    return Promise.resolve(new Response(JSON.stringify({ instances: [] }), { status: 200 }));
  }) as typeof globalThis.fetch;
  try {
    assert.equal(instancesAreAppOwned(), true, "the shim is the desktop");
    const loaded = await loadInstances(storage);
    assert.equal(fetched, 0, "a desktop page never asks its own origin - that page may be an older build");
    assert.equal(loaded.offline, false);
    assert.deepEqual(loaded.list, app);
    assert.deepEqual(sent, [{ omega: "instances:get" }], "the page asked the app");
    assert.deepEqual(readInstanceCache(storage).list, app, "the app's list is the cache's source");
  } finally {
    globalThis.fetch = realFetch;
    clearDesktopHost();
  }
});

test("a silent app answers the cached list as offline", async () => {
  const storage = fakeStorage();
  writeInstanceCache({ list: [row("main", "http://127.0.0.1:18101")], lastUsed: "" }, storage);
  fakeDesktopHost(() => null);
  try {
    const loaded = await loadInstances(storage);
    assert.equal(loaded.offline, true, "an unanswered host is not an empty list");
    assert.deepEqual(loaded.list, [row("main", "http://127.0.0.1:18101")]);
  } finally {
    clearDesktopHost();
  }
});

test("a save goes to the app and a refusal throws the host's words", async () => {
  const storage = fakeStorage();
  const saved = [row("laptop", "http://127.0.0.1:18101")];
  let refusal = "";
  const { sent } = fakeDesktopHost((body) =>
    body.omega === "instances:save" ? { instances: refusal ? [] : saved, error: refusal } : null,
  );
  try {
    assert.deepEqual(await saveInstancesInApp([row(" laptop ", "HTTP://127.0.0.1:18101/")]), saved);
    assert.deepEqual(sent.at(-1), {
      omega: "instances:save",
      instances: [row(" laptop ", "HTTP://127.0.0.1:18101/")],
    }, "the rows ride the raw body - the host is the validator");

    refusal = "row 0: name is required";
    await assert.rejects(() => saveInstancesInApp([row("", "http://127.0.0.1:18101")]), /name is required/);
  } finally {
    clearDesktopHost();
    void storage;
  }
});

test("a switch names the normalized address to the app", () => {
  const storage = fakeStorage();
  const { sent } = fakeDesktopHost();
  try {
    switchInstancesInApp("HTTP://192.0.2.10:8098/", storage);
    assert.deepEqual(sent, [{ omega: "instances:switch", url: "http://192.0.2.10:8098" }]);
    assert.equal(lastUsedInstance(storage), "http://192.0.2.10:8098", "the target is remembered first");
  } finally {
    clearDesktopHost();
  }
});

test("every answer the app emits reaches the bar's one listener", async () => {
  const seen: unknown[] = [];
  const { api } = fakeDesktopHost((body) => (body.omega === "instances:get" ? { instances: [row("main", "http://127.0.0.1:18101")], error: "" } : null));
  const off = onInstancesReply((reply) => seen.push(reply));
  try {
    const reply = await requestInstances();
    assert.equal(reply.error, "");
    assert.equal(reply.instances.length, 1);
    assert.equal(seen.length, 1, "the subscription and the request share the one answer");

    // an event nobody asked for still repaints: the pane's save lands while the bar is idle
    api.dispatchWailsEvent({
      name: INSTANCE_EVENT,
      data: { instances: [row("pi", "http://192.0.2.10:8098")], error: "" },
    });
    assert.equal(seen.length, 2);
    assert.deepEqual((seen[1] as { instances: SavedInstance[] }).instances, [row("pi", "http://192.0.2.10:8098")]);
  } finally {
    off();
    clearDesktopHost();
  }
});

test("the browser tab's save event has one name, shared by the pane and the bar", () => {
  assert.equal(INSTANCE_CHANGED_EVENT, "omega:instances-changed");
  const bar = read("webui", "app", "components", "shell", "StatusBar.vue");
  assert.match(bar, /window\.addEventListener\(INSTANCE_CHANGED_EVENT, onInstancesChanged\)/, "the bar hears a tab's save");
  assert.match(bar, /window\.removeEventListener\(INSTANCE_CHANGED_EVENT, onInstancesChanged\)/);
  assert.match(bar, /onInstancesReply\(\(reply\) => \{/, "and every app answer repaints the rows");
  const pane = read("webui", "app", "components", "settings", "SettingsTab.vue");
  assert.match(pane, /new CustomEvent\(INSTANCE_CHANGED_EVENT\)/, "the pane announces its own save");
});

test("switching writes lastUsed and navigates to the normalized target", () => {
  const storage = fakeStorage();
  const seen: string[] = [];
  // the seam stands in for window.location.assign: lastUsed lands before the leave.
  navigateToInstance("HTTP://127.0.0.1:8099/", (target) => {
    seen.push(target);
    assert.equal(lastUsedInstance(storage), "http://127.0.0.1:8099", "lastUsed is written BEFORE the navigation");
  }, storage);
  assert.deepEqual(seen, ["http://127.0.0.1:8099"]);
});

test("the cap the panel reports matches the host's", () => {
  assert.equal(MAX_INSTANCES, 32);
});

// source pin: the control lives in the STATUS BAR now - the menubar pill is gone

test("the status bar renders the connection control and the panel's rows", () => {
  const bar = read("webui", "app", "components", "shell", "StatusBar.vue");
  // the trigger: the health dot + the connected instance's name, one click opens the list
  assert.match(bar, /class="seg seg-btn health"/, "the bar's health segment IS the control");
  assert.match(bar, /:aria-label="`Connection: \$\{instanceName\}`"/, "the accessible name is Connection: <name>");
  assert.match(bar, /<span class="hide-sm">\{\{ instanceName \}\}<\/span>/, "the label is the instance's name");
  assert.doesNotMatch(bar, /"online" : "offline"/, "the words died - the dot is the state");
  assert.match(bar, /:class="\[healthy \? 'ok' : 'down', \{ offline \}\]"/, "the dot keeps ok/down, plus offline for a stale list");
  assert.match(bar, /aria-haspopup="dialog"/);
  assert.match(bar, /:aria-expanded="healthOpen"/);
  // the panel: the current row marked, then the saved rows as switches
  assert.match(bar, /<span class="hp-head">connections<\/span>/);
  assert.match(bar, /class="hp-crow hp-cur"\s+aria-current="true"/, "the current connection is marked");
  assert.match(bar, /\{\{ currentOrigin \|\| "this page" \}\}/, "the current row carries its origin");
  assert.match(bar, /v-for="row in savedRows"/, "the saved rows render");
  assert.match(bar, /@click="switchTo\(row\)"/, "a saved row is the switch");
  assert.match(bar, /navigateToInstance\(row\.url\)/, "switching is the full reload");
  assert.match(bar, /v-if="row\.url === lastUsed"/, "the last-used word marks the last switch");
  assert.match(bar, /offline - showing the cached list/, "the stale list says so");
  // ONE write surface: no form, no remove, no reconnect
  assert.doesNotMatch(bar, /<form|<input/, "no add form in the panel");
  assert.doesNotMatch(bar, /reconnect/i, "no reconnect control anywhere");
});

test("the menubar carries no connection control, and the pill component is gone", () => {
  const menubar = read("webui", "app", "components", "shell", "MenuBar.vue");
  assert.doesNotMatch(menubar, /ConnectionMenu/, "the pill is unmounted");
  assert.doesNotMatch(menubar, /currentOrigin/, "and its origin prop died with it");
  assert.ok(
    !existsSync(join(modules, "webui", "app", "components", "shell", "ConnectionMenu.vue")),
    "the component file is deleted, not just unused",
  );

  const app = read("webui", "app", "app.vue");
  const barAt = app.indexOf("<StatusBar");
  assert.ok(barAt > 0, "app.vue still mounts the status bar");
  assert.match(
    app.slice(barAt, app.indexOf("/>", barAt)),
    /:current-origin="currentOrigin"/,
    "the bar is fed the page origin",
  );
  const menuAt = app.indexOf("<MenuBar");
  assert.doesNotMatch(
    app.slice(menuAt, app.indexOf("/>", menuAt)),
    /current-origin/,
    "the menubar is no longer fed the origin",
  );
  assert.match(app, /window\.location\.origin/, "the origin is the page's own");
});

// source pin: the panel's narrow shape. The labelled bar is wider than a phone window,
// so under the squeeze the panel spans the bar instead of a 280px card anchored at its
// left edge - a 340px window never measures past its own width.
test("the status bar panel squeezes below the narrow breakpoint", () => {
  const src = read("webui", "app", "components", "shell", "StatusBar.vue");
  const block = src.match(/@media \(max-width: (\d+)px\) \{([\s\S]*?)\n  \}/);
  assert.ok(block, "the panel carries a width breakpoint");
  const px = Number(block![1]);
  assert.ok(px >= NARROW_MAX_PX, `the squeeze covers the pane-narrow width (breakpoint ${px} >= ${NARROW_MAX_PX})`);
  assert.ok(px <= 480, `the squeeze stays off a desktop bar (breakpoint ${px} <= 480)`);
  assert.match(block![2], /\.health-pop \{[^}]*left: 8px/, "the panel sheds its left anchor");
  assert.match(block![2], /\.health-pop \{[^}]*right: 8px/, "and spans the bar to the right edge");
  assert.match(block![2], /\.health-pop \{[^}]*width: auto/, "a bar-wide sheet, not a 280px box");
  assert.doesNotMatch(src, /class="dot hide-sm"/, "the dot never hides - only the name does");
});
