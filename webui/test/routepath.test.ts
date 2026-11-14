// omega webui: unit tests for the URL<->view route helpers in
// composables/omega.ts (parseSessionPath / sessionPath / isSettingsPath,
// pure functions) plus the /settings landing's wiring in app.vue (card
// t_6d6a6dc4: a reload of /settings keeps the settings tab mounted).
// Run: node --test "test/*.test.ts" (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  isSettingsPath,
  parseSessionPath,
  sessionPath,
  SETTINGS_PATH,
} from "../app/composables/omega.ts";

describe("parseSessionPath", () => {
  it("extracts the id from a /chat/<id> path", () => {
    assert.equal(parseSessionPath("/chat/abc-123"), "abc-123");
  });

  it("returns null for everything that is not /chat/<id>", () => {
    assert.equal(parseSessionPath("/"), null);
    assert.equal(parseSessionPath(""), null);
    assert.equal(parseSessionPath("/chat"), null);
    assert.equal(parseSessionPath("/sessions/s1"), null);
    assert.equal(parseSessionPath("/_nuxt/app.js"), null);
  });

  it("rejects empty ids and nested paths", () => {
    assert.equal(parseSessionPath("/chat/"), null);
    assert.equal(parseSessionPath("/chat/a/b"), null);
  });

  it("never claims the settings path as a session", () => {
    assert.equal(parseSessionPath(SETTINGS_PATH), null);
    assert.equal(parseSessionPath(`${SETTINGS_PATH}/`), null);
  });
});

describe("sessionPath", () => {
  it("builds the /chat/<id> URL", () => {
    assert.equal(sessionPath("abc-123"), "/chat/abc-123");
  });

  it("round-trips with parseSessionPath", () => {
    assert.equal(parseSessionPath(sessionPath("abc-123")), "abc-123");
  });
});

describe("isSettingsPath", () => {
  it("claims the settings view path and its trailing-slash twin", () => {
    assert.equal(isSettingsPath("/settings/view"), true);
    assert.equal(isSettingsPath("/settings/view/"), true);
    assert.equal(SETTINGS_PATH, "/settings/view");
  });

  it("never claims the settings API's own path", () => {
    // /settings is the HTTP API (agent/api/server.go registers it exactly);
    // the SPA only gets the catch-all, so the view path sits one segment
    // deeper
    assert.equal(isSettingsPath("/settings"), false);
    assert.equal(isSettingsPath("/settings/"), false);
  });

  it("claims nothing else - the path carries the VIEW, no id part", () => {
    assert.equal(isSettingsPath("/settings/view/general"), false);
    assert.equal(isSettingsPath("/settings/views"), false);
    assert.equal(isSettingsPath("/chat/settings"), false);
    assert.equal(isSettingsPath("/board"), false);
    assert.equal(isSettingsPath("/"), false);
    assert.equal(isSettingsPath(""), false);
  });

  it("carries no query axis (the section rides ?settings=<id>)", () => {
    // callers hand this helper a PATHNAME only; a pathname string carrying
    // a query is not the route
    assert.equal(isSettingsPath("/settings/view?settings=general"), false);
  });
});

// --- the landing's wiring (source pin) --------------------------------------
// The helpers above cannot prove the app CALLS them, and the node suite has
// no DOM. The landing has exactly two doors (boot, popstate) and one guard
// (the chat URL derivation pauses while the path is up) - pin that shape so
// a rewrite cannot silently drop the route while the helper tests stay green.

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const appSrc = readFileSync(join(root, "webui", "app", "app.vue"), "utf8");

describe("/settings landing wiring (app.vue)", () => {
  it("imports the path helper from the one route module", () => {
    assert.match(appSrc, /\n\s*isSettingsPath,\n/);
  });

  it("lands the route at both doors: boot and popstate", () => {
    const pop = appSrc.match(/async function onPop\(\)[\s\S]*?\n  \}\n/)?.[0] ?? "";
    assert.match(pop, /isSettingsPath\(window\.location\.pathname\)/);
    const boot =
      appSrc.match(/onMounted\([\s\S]*?isSettingsPath\(window\.location\.pathname\)/)?.[0] ?? "";
    assert.ok(boot.length > 0, "the boot door reads the path");
  });

  it("opens the tab through the gear's opener, never a second opener", () => {
    const land = appSrc.match(/function landSettingsRoute\(\)[\s\S]*?\n  \}/)?.[0] ?? "";
    assert.match(land, /settingsRoute\.value = true/);
    assert.match(land, /openSettingsTab\(\)/);
    // a stale /board overlay must not shadow the tab the path just opened
    assert.match(land, /mainView\.value = "chat"/);
  });

  it("the chat URL derivation pauses while the landing owns the path", () => {
    const watcher = appSrc.match(/watch\(urlPath, \(p\) => \{[\s\S]*?\n  \}\);/)?.[0] ?? "";
    assert.match(watcher, /if \(settingsRoute\.value\) return;/);
  });

  it("a chat tab in focus hands the URL back to chat", () => {
    const refocus = appSrc.match(/function refocusFrom\([\s\S]*?\n  \}\n/)?.[0] ?? "";
    assert.match(refocus, /settingsRoute\.value = false/);
  });

  // the fallback branch (no chat tab in the clicked pane) also resolves a
  // CHAT focus - closing the settings tab from a landing arrives here via
  // the dock-tree watcher, and the mark must go or the URL freezes on
  // /settings/view and a reload re-opens it (vera round 2)
  it("the refocus FALLBACK releases the landing, on live dock state", () => {
    const refocus = appSrc.match(/function refocusFrom\([\s\S]*?\n  \}\n/)?.[0] ?? "";
    const fallback =
      refocus.slice(refocus.indexOf("no chat focus in this gesture")) || "";
    assert.ok(fallback.length > 0, "the fallback branch is where the release lives");
    assert.match(fallback, /settingsRoute\.value = false/);
    // live state, not a blanket clear: the same watcher runs right after the
    // landing OPENS its tab (a mounted settings tab is not a release)
    assert.match(fallback, /settingsRoute\.value && !settingsTabMounted\(\)/);
    // the release adopts the tab the dock is SHOWING, so the URL names what
    // the user is looking at and not the boot-resolved focus
    assert.match(fallback, /dock\.activeChatTab\(\)/);
    // the mark falls before the focus transition, so the focus watcher's
    // composer gate (suppressed while the landing is up) sees it clear
    const release = fallback.slice(fallback.indexOf("settingsTabMounted()"));
    assert.ok(
      release.indexOf("settingsRoute.value = false") < release.indexOf("focusedKey.value = showing"),
      "the release clears the mark before adopting the showing focus",
    );
    const helper = appSrc.match(/function settingsTabMounted\(\)[^}]*\}/)?.[0] ?? "";
    assert.match(helper, /findComponentTab\(dock\.tree\.value, "settings"\)/);
  });

  // the sidebar's row click is the PRIMARY session-navigation gesture and it
  // routes straight into openChatTab (both the desktop and the mobile pane
  // bind @open), not through a dock-pane gesture - so the openers must clear
  // the mark themselves or the landing outlives the landing (vera round 1)
  it("both sidebar panes route their three chat gestures through the openers", () => {
    const opens = appSrc.match(/@open="openChatTab"/g) ?? [];
    const news = appSrc.match(/@new="openChatTab\(null\)"/g) ?? [];
    const ephemerals = appSrc.match(/@new-ephemeral="openEphemeralTab"/g) ?? [];
    assert.equal(opens.length, 2, "desktop + mobile session list");
    assert.equal(news.length, 2);
    assert.equal(ephemerals.length, 2);
  });

  it("the chat openers clear the mark - before their focus-existing returns", () => {
    const open = appSrc.match(/function openChatTab\([\s\S]*?\n  \}\n/)?.[0] ?? "";
    assert.match(open, /settingsRoute\.value = false/);
    // the focus-existing / focus-draft branches return early, so a late
    // clear would miss the very gesture this pin exists for
    assert.ok(
      open.indexOf("settingsRoute.value = false") <
        open.indexOf("if (sessionId)"),
      "the clear precedes the first early return",
    );
    assert.ok(
      open.indexOf("settingsRoute.value = false") <
        open.indexOf("focusedKey.value"),
      "the clear precedes the focus assignment (the URL watcher reads it)",
    );
    const ephemeral =
      appSrc.match(/function openEphemeralTab\([\s\S]*?\n  \}\n/)?.[0] ?? "";
    assert.match(ephemeral, /settingsRoute\.value = false/);
  });

  it("a released landing WRITES the path the pause held", () => {
    // the pause suppresses every derivation, so a release whose derived path
    // did not change (a draft tab focused before and after) writes nothing
    // on its own - the sidebar's '+' on a landing would keep /settings/view
    const release =
      appSrc.match(/watch\(settingsRoute, \(up\) => \{[\s\S]*?\n  \}\);/)?.[0] ?? "";
    assert.match(release, /if \(up\) return;/);
    assert.match(
      release,
      /if \(!isSettingsPath\(window\.location\.pathname\)\) return;/,
      "only a landing that still owns the URL writes",
    );
    assert.match(release, /history\.replaceState\(\{\}, "", urlPath\.value\)/);
  });

  it("nothing WRITES the settings path (a landing route, like /board)", () => {
    // the path is entered by the address bar, a bookmark or a link - never
    // minted by the app on open/close
    assert.doesNotMatch(appSrc, /(?:replaceState|pushState)\([^)]*SETTINGS_PATH/);
    assert.doesNotMatch(appSrc, /(?:replaceState|pushState)\([^)]*"\/settings\/view"/);
  });
});
