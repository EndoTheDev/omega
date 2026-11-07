// omega webui: the general browser tab (card t_95c91f9c) - the registry
// contract (main-pane singleton, closable, menu-listed), the pure logic
// (URL normalization, the in-pane history, the refused-embed fallback
// rule), and the structural wiring (the outlet, the capsule route the
// tab drives). Run: node --test test/browser.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  DOCK_COMPONENTS,
  DOCKABLES,
  MENU_DOCKABLES,
  MAIN_DOCKABLES,
  INSPECTOR_TAB_COMPONENTS,
  dockableOf,
} from "../app/composables/dockables.ts";
import { ICON_NAMES } from "../app/composables/icons.ts";
import {
  FALLBACK_AFTER_MS,
  PERSIST_GRACE_MS,
  canStep,
  classifyProbe,
  emptyNav,
  normalizeUrl,
  pushNav,
  stepNav,
} from "../../harness/browser/ui/browserLogic.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

// --- the registry contract -----------------------------------------------------

describe("the browser dockable (t_95c91f9c)", () => {
  it("is in the registry: a main-pane singleton, closable", () => {
    assert.ok((DOCK_COMPONENTS as readonly string[]).includes("browser"));
    const d = dockableOf("browser");
    assert.equal(d.area, "main");
    assert.equal(d.policy, "singleton");
    assert.equal(d.closable, true);
    assert.equal(d.title, "Browser");
    assert.equal(d.icon, "browser");
  });

  it("renders the main pane class (MAIN_DOCKABLES)", () => {
    assert.ok((MAIN_DOCKABLES as readonly string[]).includes("browser"));
    assert.ok(!(INSPECTOR_TAB_COMPONENTS as readonly string[]).includes("browser"));
  });

  it("lists in the '+' / empty-pane menu (openable, excluded from exclusions)", () => {
    assert.ok((MENU_DOCKABLES as readonly string[]).includes("browser"));
  });

  it("carries a tool's honest floor (128px, not the chat wall)", () => {
    assert.equal(dockableOf("browser").minPx, 128);
  });

  it("keeps every registry constraint the suite already pins", () => {
    // one entry, unique ids (the contract test shape), icon declared
    const ids = DOCKABLES.map((d) => d.id);
    assert.equal(new Set(ids).size, ids.length);
    const d = dockableOf("browser");
    assert.ok((ICON_NAMES as readonly string[]).includes(d.icon as string));
    assert.ok(d.title.length > 0);
  });
});

// --- the pure logic -------------------------------------------------------------

describe("browserLogic: url normalization", () => {
  it("prefixes the https law on schemeless input", () => {
    assert.equal(normalizeUrl("example.com/docs"), "https://example.com/docs");
    assert.equal(normalizeUrl("  example.com  "), "https://example.com");
  });
  it("passes schemes through verbatim (the user ruled http when they typed it)", () => {
    assert.equal(normalizeUrl("http://127.0.0.1:8098/health"), "http://127.0.0.1:8098/health");
    assert.equal(normalizeUrl("https://example.com/docs?a=b"), "https://example.com/docs?a=b");
  });
  it("empty input stays empty (no navigation, no crash)", () => {
    assert.equal(normalizeUrl("   "), "");
    assert.equal(normalizeUrl(""), "");
  });
});

describe("browserLogic: the in-pane history", () => {
  it("pushes, collapses consecutive duplicates, truncates the forward tail", () => {
    let h = emptyNav();
    h = pushNav(h, "https://a.test");
    h = pushNav(h, "https://b.test");
    assert.deepEqual(h, { urls: ["https://a.test", "https://b.test"], idx: 1 });

    // duplicate never stacks twice
    h = pushNav(h, "https://b.test");
    assert.deepEqual(h, { urls: ["https://a.test", "https://b.test"], idx: 1 });

    // back then a fresh commit rewrites the forward tail
    const back = stepNav(h, -1)!;
    assert.deepEqual(back, { urls: ["https://a.test", "https://b.test"], idx: 0 });
    h = pushNav(back, "https://c.test");
    assert.deepEqual(h, { urls: ["https://a.test", "https://c.test"], idx: 1 });
  });

  it("steps at edges are null and the buttons say so", () => {
    const h = pushNav(emptyNav(), "https://a.test");
    assert.equal(stepNav(h, -1), null);
    assert.equal(stepNav(h, +1), null);
    assert.equal(canStep(h, -1), false);
    assert.equal(canStep(h, +1), false);
    const h2 = pushNav(h, "https://b.test");
    assert.ok(stepNav(h2, -1) !== null);
    assert.equal(canStep(h2, +1), false);
    assert.equal(canStep(h2, -1), true);
  });

  it("never stacks the empty string", () => {
    const h = pushNav(emptyNav(), "");
    assert.deepEqual(h, emptyNav());
  });
});

describe("browserLogic: the refused-embed fallback", () => {
  it("the window is a real constant the component timers read", () => {
    assert.equal(typeof FALLBACK_AFTER_MS, "number");
    assert.ok(FALLBACK_AFTER_MS > 1000 && FALLBACK_AFTER_MS < 10000);
  });

  it("the persist grace is real and short (the probe vetoes inside it)", () => {
    assert.equal(typeof PERSIST_GRACE_MS, "number");
    assert.ok(PERSIST_GRACE_MS > 0 && PERSIST_GRACE_MS < FALLBACK_AFTER_MS);
  });

  it("the fallback rule is probe-gated, the timer stays the slow path", () => {
    // the component's wiring, pinned structurally: beginEmbed arms the
    // window AND fires the probe; only a definitive negative flips the
    // fallback; a refused load event may NOT heal it (probeRefused) and
    // may NOT persist
    const src = read("../harness/browser/ui/BrowserTab.vue");
    assert.ok(src.includes("FALLBACK_AFTER_MS"), "the constant is used");
    assert.ok(src.includes("onFrameLoad"), "the load handler exists");
    const win = src.match(/fallbackTimer = setTimeout\(\(\) => \{[\s\S]*?FALLBACK_AFTER_MS/);
    assert.ok(win, "the timer arms the window");
    assert.ok(win![0].includes("pending.value"), "the timer only fires while pending");
    assert.ok(src.includes("runProbe"), "the probe rides every commit");
    assert.ok(
      /if \(probeRefused\.value\)[\s\S]{0,200}return/.test(src),
      "a refused load event is ignored",
    );
    assert.ok(src.includes("refused.value = false"), "a late decision clears the fallback");
    assert.ok(src.includes("window.open"), "open in a new tab exists");
    // card t_a92450ea: the ONE copy path - copyText, awaited
    assert.ok(src.includes("copyText(address.value)"), "copy URL exists");
  });

  it("the honesty note lives in the logic file (probe: headers only, never content)", () => {
    const logic = read("../harness/browser/ui/browserLogic.ts");
    assert.match(logic, /Chromium fires the load event/);
    assert.match(logic, /the probe reads headers, never content/);
  });
});

describe("browserLogic: the probe verdict", () => {
  it("only a definitive false refuses; null and junk stay unknown", () => {
    assert.equal(classifyProbe({ frameable: false }), "refused");
    assert.equal(classifyProbe({ frameable: true }), "ok");
    assert.equal(classifyProbe({ frameable: null }), "unknown");
    assert.equal(classifyProbe({}), "unknown");
    assert.equal(classifyProbe(null), "unknown");
    assert.equal(classifyProbe("junk"), "unknown");
  });
});

// --- the structural wiring -------------------------------------------------------

describe("browser wiring", () => {
  it("the outlet template mounts the manifest's browser view", () => {
    const app = read("app/app.vue");
    assert.ok(app.includes('#pane-browser'), "the outlet exists");
    assert.ok(app.includes(':is="browserTabView"'), "the outlet binds the tabs manifest's view (card t_617c2ad0)");
    assert.ok(!app.includes("components/browser"), "no hardcoded capsule path rides the shell");
  });
});