// omega webui: back/forward navigation history (card t_6215474b). The
// stack model (push on tab activation + pane focus, consecutive-duplicate
// suppressed, cap 100) at the boundary production uses: the reactive
// shell createNavHistory + the pure push/back/forward math + the app
// wiring assertions (app.vue's wrapper routes every activation through
// one record path; the MenuBar cluster seats inline after the brand
// (card t_3aed367e: the wordmark owns the center) with the ends dimmed
// via the disabled attribute).
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

import {
  createNavHistory,
  locateEntry,
  navBack,
  navForward,
  navPush,
  navTabId,
  navEntriesEqual,
  NAV_HISTORY_CAP,
  type NavEntry,
} from "../app/composables/navHistory.ts";
import { makeSplit, type DockPane } from "../app/composables/dock.ts";

const here = dirname(fileURLToPath(import.meta.url));

// a pane literal (dock.ts's makePane mints its own id - these tests pin
// exact pane ids)
function pane(id: string, tabs: DockPane["tabs"]): DockPane {
  return { kind: "pane", id, tabs, active: 0, fixed: null };
}

// --- the pure push model -----------------------------------------------------

describe("nav push model", () => {
  const A: NavEntry = { paneId: "p1", tabId: "chat" };
  const B: NavEntry = { paneId: "p1", tabId: "k2" };
  const C: NavEntry = { paneId: "p2", tabId: "tools" };

  it("dedupes the current entry (re-click pushes nothing)", () => {
    const s = navPush([A], 0, A);
    assert.equal(s.index, 0);
    assert.equal(s.entries.length, 1);
  });

  it("a different entry splices in and discards the forward branch", () => {
    // A -> B -> C (index at A), a push truncates the branch
    let s = navPush([A, B, C], 0, B);
    assert.deepEqual(s.entries.map((e) => e.tabId), ["chat", "k2"]);
    assert.equal(s.index, 1);
  });

  it("cap drops the oldest, index rides the top", () => {
    const many: NavEntry[] = Array.from({ length: NAV_HISTORY_CAP }, (_, i) => ({
      paneId: "p",
      tabId: "t" + i,
    }));
    const s = navPush(many, many.length - 1, { paneId: "p", tabId: "new" });
    assert.equal(s.entries.length, NAV_HISTORY_CAP);
    assert.equal(s.entries[0].tabId, "t1"); // t0 dropped, not `new`
    assert.equal(s.index, NAV_HISTORY_CAP - 1);
    assert.equal(s.entries[s.entries.length - 1].tabId, "new");
  });

  it("back/forward pointers dim at the ends", () => {
    assert.equal(navBack([A], 0), null);
    assert.equal(navForward([A], 0), null);
    assert.equal(navForward([A, B], 0), 1);
    assert.equal(navBack([A, B], 1), 0);
  });
});

// --- tab identity ------------------------------------------------------------

describe("navTabId", () => {
  it("chat keys by the instance key, viewer by its payload, singletons by component", () => {
    assert.equal(navTabId({ component: "chat", key: "k7" }), "k7");
    assert.equal(
      navTabId({ component: "viewer", viewer: { kind: "file", name: "a/b.md" } }),
      "viewer:file:a/b.md",
    );
    assert.equal(navTabId({ component: "browser" }), "browser");
  });

  it("equal entries compare by both coordinates", () => {
    assert.ok(navEntriesEqual({ paneId: "p", tabId: "t" }, { paneId: "p", tabId: "t" }));
    assert.ok(!navEntriesEqual({ paneId: "p", tabId: "t" }, { paneId: "q", tabId: "t" }));
  });
});

// --- entry resolution against a live tree ------------------------------------

describe("locateEntry", () => {
  const tree = makeSplit("row", [
    pane("p1", [{ component: "chat", key: "ka" }, { component: "chat", key: "kb" }]),
    pane("p2", [{ component: "browser" }]),
  ]);

  it("resolves the tab index in its pane", () => {
    assert.equal(locateEntry(tree, { paneId: "p1", tabId: "kb" }), 1);
    assert.equal(locateEntry(tree, { paneId: "p2", tabId: "browser" }), 0);
  });

  it("a dead pane or tab no longer resolves (null)", () => {
    assert.equal(locateEntry(tree, { paneId: "gone", tabId: "chat" }), null);
    assert.equal(locateEntry(tree, { paneId: "p1", tabId: "kclosed" }), null);
  });
});

// --- the reactive shell (the production signal shape) -------------------------

describe("createNavHistory", () => {
  it("back/forward activate the resolved entry; the dims track the ends", () => {
    const activated: NavEntry[] = [];
    const nav = createNavHistory((e) => {
      activated.push(e);
      return true;
    });
    nav.push({ paneId: "p1", tabId: "ta" });
    nav.push({ paneId: "p1", tabId: "tb" });
    nav.push({ paneId: "p2", tabId: "browser" });
    assert.equal(nav.canBack.value, true);
    assert.equal(nav.canForward.value, false);
    nav.back();
    assert.deepEqual(activated, [{ paneId: "p1", tabId: "tb" }]);
    assert.equal(nav.canForward.value, true);
    nav.forward();
    assert.equal(activated.length, 2);
    assert.deepEqual(activated[1], { paneId: "p2", tabId: "browser" });
    nav.back();
    nav.back();
    assert.equal(nav.canBack.value, false);
  });

  it("the activator walks past closed panes/tabs instead of getting stuck", () => {
    const alive = new Set(["b"]);
    const nav = createNavHistory((e) => alive.has(e.tabId));
    nav.push({ paneId: "p", tabId: "a" }); // dies before the walk
    nav.push({ paneId: "p", tabId: "b" });
    nav.push({ paneId: "p", tabId: "c" }); // dies before the walk
    nav.push({ paneId: "p", tabId: "d" });
    nav.back(); // top -> c (dead, cut) -> b (alive)
    assert.equal(nav.index.value, nav.entries.value.findIndex((e) => e.tabId === "b"));
    nav.forward(); // d is dead too - cut, nothing left to advance
    assert.equal(nav.canForward.value, false);
  });

  it("a failed back at the bottom is a no-op", () => {
    const nav = createNavHistory(() => false);
    nav.push({ paneId: "p", tabId: "t" });
    nav.back();
    assert.equal(nav.index.value, 0);
  });
});

// --- the app wiring (production-boundary assertions) ----------------------------

describe("nav cluster wiring", () => {
  const menubar = readFileSync(
    join(here, "../app/components/shell/MenuBar.vue"),
    "utf8",
  );
  const app = readFileSync(join(here, "../app/app.vue"), "utf8");
  const css = readFileSync(join(here, "../app/assets/css/main.css"), "utf8");
  const iconset = readFileSync(join(here, "../app/composables/icons.ts"), "utf8");

  it("the cluster seats inline after the brand; the wordmark owns the center", () => {
    assert.match(menubar, /<NavCluster/);
    assert.match(menubar, /:can-back="canBack"/);
    assert.match(menubar, /@open-palette="\$emit\('open-palette'\)"/);
    // MenuBar DECLARES the dims as props - an undeclared prop keeps
    // canBack undefined in the template and both arrows ship dead
    assert.match(menubar, /canBack: boolean/);
    assert.match(menubar, /canForward: boolean/);
    // card t_3aed367e: the cluster LEFT the menubar's absolute center -
    // the wordmark's seat; the wordmark takes it (dead center on the
    // menubar's full width) as the plain static text 'Omega'
    assert.doesNotMatch(css, /\.nav-cluster\s*\{[^}]*left:\s*50%/);
    assert.match(css, /\.menubar-wordmark\s*\{[^}]*left:\s*50%/);
    assert.match(css, /\.menubar-wordmark\s*\{[^}]*translateX\(-50%\)/);
    assert.match(css, /\.menubar-wordmark\s*\{[^}]*var\(--mono\)/);
    assert.match(menubar, /<span class="menubar-wordmark">Omega<\/span>/);
    // no version anywhere in the menubar - the status bar owns it
    // card t_c5d4f2ad: the far-left brand is the lone omega glyph (the
    // center wordmark below is a SEPARATE span and keeps its 'Omega')
    assert.match(menubar, /<span class="brand">Ω<\/span>/);
    assert.doesNotMatch(menubar, /Omega v?\d+\.\d+/);
    assert.doesNotMatch(menubar, /\/health/);
    // the ruling: disabled attribute dimming, never a click-swallow class
    assert.match(css, /\.nav-btn:disabled\s*\{[^}]*opacity/);
  });

  it("the NavCluster dims via :disabled and carries the Ctrl+K hint", () => {
    const nav = readFileSync(join(here, "../app/components/nav/NavCluster.vue"), "utf8");
    assert.match(nav, /:disabled="!canBack"/);
    assert.match(nav, /:disabled="!canForward"/);
    assert.match(nav, /Command palette \(Ctrl\+K\)/);
    assert.match(nav, /Back \(Alt\+Left\)/);
    assert.match(nav, /Forward \(Alt\+Right\)/);
    // the iconset vocabulary: all three names declared
    for (const n of ["nav-back", "nav-forward", "palette"]) {
      assert.match(iconset, new RegExp(`"${n}":`));
    }
  });

  it("app.vue owns ONE record path and the button/key shared back-forward", () => {
    // every activation op funnels through recordNav
    assert.match(app, /const rawDock = createDock/);
    assert.match(app, /provide\(DOCK_API_KEY, dock\)/);
    for (const op of ["focus", "openTab", "openSingleton", "setComponent"]) {
      assert.match(app, new RegExp(`${op}: \\((paneId|paneId, [a-z]+)\\) =>`));
    }
    assert.match(app, /window\.addEventListener\("keydown", onNavKeys\)/);
    assert.match(app, /onNavKeys[\s\S]{0,1200}ArrowLeft[\s\S]{0,300}nav\.back\(\)/);
    assert.match(app, /onNavKeys[\s\S]{0,1200}ArrowRight[\s\S]{0,300}nav\.forward\(\)/);
    // Ctrl+K (ruling 1) and the palette open both exist
    assert.match(app, /e\.key === "k"/);
    assert.match(app, /function openPalette/);
    // pane focus records (ruling 2), via the existing document click seam
    assert.match(app, /if \(paneId\) recordNav\(paneId\)/);
    assert.match(app, /navTabId\(tab\)/);
  });
});