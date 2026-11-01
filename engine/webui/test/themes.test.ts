// omega webui: unit tests for the themes pure helpers
// (normalizeThemesPayload, usableThemes, the split knobs: color mode +
// theme lists, the persisted-pair migration, the vanished-file fallback).
// Run: node --test engine/webui/test/ (node >= 23 runs TS natively)
import { test } from "node:test";
import assert from "node:assert/strict";

import {
  normalizeThemesPayload,
  usableThemes,
  colorModeOptions,
  themePickOptions,
  resolveThemeId,
  restoreThemeKnobs,
  DEFAULT_THEME_ID,
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

// --- the split picker's option lists ---

test("color mode knob lists auto/dark/light and nothing else", () => {
  assert.deepEqual(colorModeOptions, ["auto", "dark", "light"]);
});

const fileThemes = normalizeThemesPayload({
  themes: [
    { id: "palm", name: "Midnight Palm", base: "light", tokens: { accent: "#ff0000" } },
    { id: "junk", name: "Junk", error: "invalid yaml" },
    { id: "default", name: "Default", base: "dark", tokens: { accent: "#7aa2f7" } },
  ],
});

test("theme knob lists default plus usable files, shadows the seed id", () => {
  const opts = themePickOptions(fileThemes);
  // "default" first; palm usable; junk flagged (hidden, as before);
  // the seeded default.yaml's file entry is shadowed - the picker's
  // "default" names the built-in palettes only
  assert.deepEqual(opts, [
    "default",
    { value: "palm", label: "Midnight Palm" },
  ]);
});

test("theme knob options stay the bare list with no files", () => {
  assert.deepEqual(themePickOptions([]), ["default"]);
});

// --- persisted-pair migration (the old merged select's values) ---

test("migration: stored theme:<id> maps to theme <id> + mode auto", () => {
  assert.deepEqual(restoreThemeKnobs(null, "theme:palm"), {
    mode: "auto",
    theme: "palm",
  });
});

test("migration: stored plain mode maps to theme default + the mode", () => {
  assert.deepEqual(restoreThemeKnobs(null, "dark"), {
    mode: "dark",
    theme: DEFAULT_THEME_ID,
  });
  assert.deepEqual(restoreThemeKnobs(null, "light"), {
    mode: "light",
    theme: DEFAULT_THEME_ID,
  });
  assert.deepEqual(restoreThemeKnobs(null, "auto"), {
    mode: "auto",
    theme: DEFAULT_THEME_ID,
  });
});

test("migration: missing or tampered legacy value defaults both knobs", () => {
  assert.deepEqual(restoreThemeKnobs(null, null), {
    mode: "auto",
    theme: DEFAULT_THEME_ID,
  });
  assert.deepEqual(restoreThemeKnobs(null, "theme:"), {
    mode: "auto",
    theme: DEFAULT_THEME_ID,
  });
});

test("migration: a persisted theme id is idempotent, mode rides along", () => {
  // after the split, the theme-id key exists and the mode key holds a
  // plain mode: both are read back as-is, no second migration
  assert.deepEqual(restoreThemeKnobs("palm", "dark"), {
    mode: "dark",
    theme: "palm",
  });
  assert.deepEqual(restoreThemeKnobs("palm", null), {
    mode: "auto",
    theme: "palm",
  });
});

// --- fallback on a vanished or broken file ---

test("fallback: a vanished file id returns to default", () => {
  assert.equal(resolveThemeId("palm", fileThemes), "palm");
  assert.equal(resolveThemeId("palm", []), DEFAULT_THEME_ID);
  // the flagged junk entry is not usable either
  assert.equal(resolveThemeId("junk", fileThemes), DEFAULT_THEME_ID);
});

test("fallback: the built-in default never falls back", () => {
  assert.equal(resolveThemeId(DEFAULT_THEME_ID, []), DEFAULT_THEME_ID);
  assert.equal(resolveThemeId(DEFAULT_THEME_ID, fileThemes), DEFAULT_THEME_ID);
});

// --- theme select wire value ---

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