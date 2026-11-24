// omega webui: the connection switcher's client half. Three layers, matching the house
// patterns: the fetch wrappers (URL/method/body under fetch interception), the pure
// helpers (normalize, upsert, remove, the cache round-trip and the failed-fetch floor),
// and a source pin that the menubar mounts the menu.
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  INSTANCE_CACHE_KEY,
  MAX_INSTANCES,
  fetchInstances,
  lastUsedInstance,
  loadInstances,
  navigateToInstance,
  normalizeInstanceName,
  normalizeInstanceURL,
  readInstanceCache,
  removeInstance,
  saveInstances,
  setLastUsedInstance,
  upsertInstance,
  writeInstanceCache,
  type InstanceStorage,
  type SavedInstance,
} from "../app/composables/instances.ts";

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

test("saveInstances PUTs the {instances:[...]} envelope and answers the stored list", async () => {
  const seen: { url: string; method: string; body: string; contentType: string }[] = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = ((url: string | URL, init?: RequestInit) => {
    const headers = new Headers(init?.headers);
    seen.push({ url: String(url), method: init?.method ?? "", body: String(init?.body), contentType: headers.get("content-type") ?? "" });
    return Promise.resolve(new Response(JSON.stringify({ instances: [row("pi", "https://omega.example.test")] }), { status: 200 }));
  }) as typeof globalThis.fetch;
  try {
    const saved = await saveInstances([row("pi", "https://omega.example.test")]);
    assert.equal(seen[0].url, "/instances");
    assert.equal(seen[0].method, "PUT");
    assert.equal(seen[0].contentType, "application/json");
    assert.deepEqual(JSON.parse(seen[0].body), { instances: [row("pi", "https://omega.example.test")] });
    assert.deepEqual(saved, [row("pi", "https://omega.example.test")]);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("a refused save throws the host's own sentence", async () => {
  const realFetch = globalThis.fetch;
  globalThis.fetch = (() =>
    Promise.resolve(new Response("instances: row 0: name is required\n", { status: 400 }))) as typeof globalThis.fetch;
  try {
    await assert.rejects(() => saveInstances([row("x", "http://h.test")]), /name is required/);
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

test("a good fetch replaces the cache with the backend list", async () => {
  const storage = fakeStorage();
  writeInstanceCache({ list: [row("stale", "http://old.test")], lastUsed: "http://old.test" }, storage);

  const realFetch = globalThis.fetch;
  globalThis.fetch = (() =>
    Promise.resolve(new Response(JSON.stringify({ instances: [row("laptop", "http://a.test")] }), { status: 200 }))) as typeof globalThis.fetch;
  try {
    const loaded = await loadInstances(storage);
    assert.equal(loaded.offline, false);
    assert.deepEqual(loaded.list, [row("laptop", "http://a.test")]);
    assert.deepEqual(readInstanceCache(storage), { list: [row("laptop", "http://a.test")], lastUsed: "http://old.test" });
  } finally {
    globalThis.fetch = realFetch;
  }
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

test("the cap the menu enforces matches the host's", () => {
  assert.equal(MAX_INSTANCES, 32);
});

// source pin: the menubar mounts the switcher

test("MenuBar mounts ConnectionMenu and app.vue supplies the origin", () => {
  const menubar = read("webui", "app", "components", "shell", "MenuBar.vue");
  assert.match(menubar, /<ConnectionMenu\b/, "MenuBar renders the connection pill");
  assert.match(menubar, /:current-origin="currentOrigin"/, "the pill is fed the page origin");
  assert.match(menubar, /currentOrigin: string/, "the origin rides a declared prop");

  const app = read("webui", "app", "app.vue");
  assert.match(app, /:current-origin="currentOrigin"/, "app.vue passes the origin down");
  assert.match(app, /window\.location\.origin/, "the origin is the page's own");
});
