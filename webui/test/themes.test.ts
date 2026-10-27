// omega webui: unit tests for the themes pure helpers
// (normalizeThemesPayload, usableThemes, themeSelect wiring).
// Run: node --test webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  normalizeThemesPayload,
  usableThemes,
  themeSelectValue,
  parseThemeSelect,
  applyThemeTokens,
  clearThemeTokens,
} from "../app/composables/omega.ts";

// --- normalizeThemesPayload ---

test("normalize keeps well-formed themes", () => {
  const out = normalizeThemesPayload({
    themes: [
      { id: "palm", name: "Midnight Palm", base: "light", tokens: { accent: "#ff0000" } },
    ],
  });
  assert.equal(out.length, 1);
  assert.deepEqual(out[0], {
    id: "palm",
    name: "Midnight Palm",
    base: "light",
    tokens: { accent: "#ff0000" },
    error: undefined,
  });
});

test("normalize flags broken themes instead of dropping them", () => {
  const out = normalizeThemesPayload({
    themes: [
      { id: "junk", name: "Junk", error: "unknown token" },
      { id: "good", name: "Good", tokens: { z0: "#111111" } },
    ],
  });
  assert.equal(out.length, 2);
  assert.equal(out[0].error, "unknown token");
  assert.deepEqual(out[0].tokens, {});
  assert.equal(out[0].base, "dark"); // malformed base falls back to dark
});

// --- malformed entries are dropped, not thrown on ---
test("normalize drops malformed entries", () => {
  const out = normalizeThemesPayload([
    null,
    "junk",
    { name: "no id" },
    { id: 7, name: "numeric id" },
    { id: "ok" }, // minimal entry: name falls back to id
  ]);
  assert.equal(out.length, 1);
  assert.equal(out[0].id, "ok");
  assert.equal(out[0].name, "ok");
});

test("normalize tolerates total garbage", () => {
  assert.deepEqual(normalizeThemesPayload(null), []);
  assert.deepEqual(normalizeThemesPayload("junk"), []);
  assert.deepEqual(normalizeThemesPayload({ themes: "not an array" }), []);
});

// --- usableThemes ---

test("usableThemes filters only the picker's list", () => {
  const all = normalizeThemesPayload({
    themes: [
      { id: "good", name: "Good", tokens: { accent: "red" } },
      { id: "junk", name: "Junk", error: "invalid yaml" },
    ],
  });
  const usable = usableThemes(all);
  assert.equal(usable.length, 1);
  assert.equal(usable[0].id, "good");
});

// --- theme select wire value ---

test("themeSelectValue and parseThemeSelect round-trip", () => {
  assert.equal(themeSelectValue("palm"), "theme:palm");
  assert.equal(parseThemeSelect("theme:palm"), "palm");
  assert.equal(parseThemeSelect("auto"), null);
  assert.equal(parseThemeSelect("light"), null);
});

// --- token application against a stand-in CSSStyleDeclaration ---

type FakeStyle = Map<string, string>;

function fakeStyle(): FakeStyle {
  const map: FakeStyle = new Map();
  const asDecl = map as unknown as CSSStyleDeclaration;
  (asDecl as Record<string, unknown>).setProperty = (k: string, v: string) => map.set(k, v);
  (asDecl as Record<string, unknown>).removeProperty = (k: string) => map.delete(k);
  return map;
}

test("applyThemeTokens writes overrides into inline style, clear undoes", () => {
  const style = fakeStyle();
  applyThemeTokens(style as unknown as CSSStyleDeclaration, {
    accent: "#00ff00",
    z0: "#000000",
  });
  assert.equal(style.get("--accent"), "#00ff00");
  assert.equal(style.get("--z0"), "#000000");

  clearThemeTokens(style as unknown as CSSStyleDeclaration);
  assert.equal(style.size, 0);
});

test("apply re-apply replaces rather than mixes tokens", () => {
  const style = fakeStyle();
  applyThemeTokens(style as unknown as CSSStyleDeclaration, { accent: "blue", z0: "#111" });
  applyThemeTokens(style as unknown as CSSStyleDeclaration, { accent: "green" });
  assert.equal(style.get("--accent"), "green");
  assert.equal(style.has("--z0"), false);
});