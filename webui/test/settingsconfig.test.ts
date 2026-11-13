// omega webui: the settings pane's config-file actions (card t_a82d39c8)
// - reset to default, export to yaml, edit as yaml.
//
// The node suite has no DOM, so this file carries two kinds of witness:
// the PURE helpers (the wire calls, the export filename) against a stub
// fetch, and SOURCE pins on SettingsTab.vue for the parts only a browser
// can exercise (the band seat, the native dialog, the two-click reset).
// The live behaviour is the journey/lane probe's job.
// Run: node --test webui/test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

import {
  configExportFilename,
  fetchConfigYaml,
  resetSettings,
  saveConfigYaml,
} from "../app/composables/omega.ts";

// --- a stub fetch ---------------------------------------------------------

type Call = { url: string; init: RequestInit | undefined };

function stubFetch(status: number, body: string, contentType = "application/json"): Call[] {
  const calls: Call[] = [];
  const original = globalThis.fetch;
  globalThis.fetch = (async (url: string, init?: RequestInit) => {
    calls.push({ url, init });
    return new Response(body, { status, headers: { "Content-Type": contentType } });
  }) as typeof globalThis.fetch;
  cleanupFetch.push(() => {
    globalThis.fetch = original;
  });
  return calls;
}

const cleanupFetch: (() => void)[] = [];
// node's test runner has no afterEach that survives a thrown assertion at
// module scope, so restore per test through a helper instead.
function restoreFetch() {
  for (const fn of cleanupFetch.splice(0)) fn();
}

// --- configExportFilename -------------------------------------------------

test("configExportFilename stamps the day and keeps the .yaml suffix", () => {
  const name = configExportFilename(new Date(2026, 10, 4, 13, 5, 0));
  assert.equal(name, "config-2026-11-04.yaml");
  // zero-padded, so two exports in different months sort together
  assert.equal(configExportFilename(new Date(2026, 0, 9)), "config-2026-01-09.yaml");
});

// --- the wire ------------------------------------------------------------

test("fetchConfigYaml reads the document from /settings/raw", async () => {
  const calls = stubFetch(200, "provider:\n  api_key: '****'\n", "application/yaml");
  try {
    const text = await fetchConfigYaml();
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, "/settings/raw");
    assert.equal(calls[0].init, undefined, "a plain GET carries no init");
    assert.equal(text, "provider:\n  api_key: '****'\n");
  } finally {
    restoreFetch();
  }
});

test("saveConfigYaml PUTs the document and normalizes the refreshed payload", async () => {
  const payload = {
    groups: [{ key: "compaction", label: "compaction", knobs: [{ key: "max_turns", label: "max turns", type: "number", value: 12 }] }],
    scopes: [],
  };
  const calls = stubFetch(200, JSON.stringify(payload));
  try {
    const saved = await saveConfigYaml("max_turns: 12\n");
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, "/settings/raw");
    assert.equal(calls[0].init?.method, "PUT");
    assert.equal(calls[0].init?.body, "max_turns: 12\n");
    assert.match(String((calls[0].init?.headers as Record<string, string>)["Content-Type"]), /yaml/);
    // the answer rides the SAME normalizer every other settings read uses
    assert.equal(saved.groups[0].knobs[0].value, 12);
  } finally {
    restoreFetch();
  }
});

test("resetSettings POSTs an empty body - the defaults are the host's", async () => {
  const calls = stubFetch(200, JSON.stringify({ groups: [], scopes: [] }));
  try {
    await resetSettings();
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, "/settings/reset");
    assert.equal(calls[0].init?.method, "POST");
    assert.equal(calls[0].init?.body, undefined, "the client ships no default values");
  } finally {
    restoreFetch();
  }
});

test("a refused config call surfaces the server's own sentence", async () => {
  const cases: [string, () => Promise<unknown>][] = [
    ["config read", () => fetchConfigYaml()],
    ["config save", () => saveConfigYaml("bad")],
    ["config reset", () => resetSettings()],
  ];
  for (const [label, call] of cases) {
    stubFetch(400, "config: provider.model_name is required\n", "text/plain");
    try {
      await assert.rejects(call, (err: Error) => {
        assert.equal(err.message, "config: provider.model_name is required", `${label} keeps the host's words`);
        return true;
      });
    } finally {
      restoreFetch();
    }
  }
});

test("a bodyless failure still names the call and status", async () => {
  stubFetch(501, "");
  try {
    await assert.rejects(() => fetchConfigYaml(), /config read: 501/);
  } finally {
    restoreFetch();
  }
});

// --- the pane's SOURCE pins ----------------------------------------------

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");
const tab = read("app/components/settings/SettingsTab.vue");

// the slice of the template the band owns: from the filter bar's opening
// tag to the split's - the contract is that the three actions live THERE.
function bandSlice(): string {
  const start = tab.indexOf('<div class="settings-filterbar">');
  const end = tab.indexOf('<div class="settings-split">');
  assert.ok(start > 0 && end > start, "the band and the split are both in the template");
  return tab.slice(start, end);
}

test("the three config actions live in the BAND, never the scroll body", () => {
  const band = bandSlice();
  for (const action of ["reset to default", "export to yaml", "edit as yaml"]) {
    assert.ok(band.includes(action), `${action} is seated in the settings band`);
  }
  // the menu wrapper and its chip are band children
  assert.match(band, /ref="configMenuEl" class="settings-config"/);
  assert.match(band, /role="menu"/, "the actions ride one anchored menu");
  // and the document body carries none of them
  const body = tab.slice(tab.indexOf('<div class="settings-split">'), tab.indexOf("</aside>"));
  for (const action of ["export to yaml", "edit as yaml"]) {
    assert.ok(!body.includes(action), `${action} must not float in the scroll body`);
  }
});

test("reset ships no client-side default set", () => {
  // the action calls the seam helper and applies the refreshed payload
  assert.match(
    tab,
    /async function onResetToDefaults\(\)[\s\S]{0,700}applyPayload\(await resetSettings\(\)\)/,
    "the reset rides the host's own defaults",
  );
  // a two-click arm: the first click only asks
  assert.match(
    tab,
    /if \(!resetArmed\.value\) \{\s*resetArmed\.value = true;\s*return;\s*\}/,
    "the reset arms before it applies",
  );
  assert.match(tab, /confirm: reset all\?/, "the armed label names what the second click does");
  // never a hand-built values map (a second default set would drift)
  assert.ok(
    !/resetSettings\(\s*\{/.test(tab),
    "resetSettings takes no arguments - the defaults are the host's",
  );
});

test("export downloads the document it fetched", () => {
  assert.match(
    tab,
    /downloadText\(configExportFilename\(\), await fetchConfigYaml\(\)\)/,
    "one fetch, one download - the file the server serves",
  );
  assert.match(tab, /a\.download = name;/, "the anchor carries the download name");
  assert.match(tab, /URL\.revokeObjectURL\(url\)/, "the blob URL is released");
});

test("edit as yaml opens a NATIVE dialog and saves through the write path", () => {
  assert.match(tab, /<dialog[\s\S]{0,400}ref="yamlDialogEl"/, "a real <dialog> element");
  assert.match(tab, /yamlDialogEl\.value\?\.showModal\(\)/, "the modal top layer, not a css overlay");
  assert.match(
    tab,
    /async function saveYaml\(\)[\s\S]{0,700}applyPayload\(await saveConfigYaml\(yamlText\.value\)\)/,
    "the save rides the server's config write path",
  );
  // the editor text is the document; saving re-reads what landed
  assert.match(tab, /const persisted = await fetchConfigYaml\(\);/, "the editor shows the persisted document");
  assert.match(tab, /v-model="yamlText"/, "the textarea edits the document");
  assert.ok(!/yamlText[^;]*readonly/.test(tab), "the editor is editable");
  // the dialog answers Escape/backdrop natively and tracks its own close
  assert.match(tab, /@close="yamlOpen = false"/, "the open state follows the element's own close");
});

test("the config menu closes on an outside click and dies with the pane", () => {
  assert.match(tab, /document\.addEventListener\("click", onDocumentClick\)/);
  assert.match(tab, /document\.removeEventListener\("click", onDocumentClick\)/);
  assert.match(tab, /function onDocumentClick\(event: MouseEvent\)[\s\S]{0,400}configMenuEl\.value/);
});

test("the chip is a real button with its expanded state exposed", () => {
  const band = bandSlice();
  assert.match(band, /aria-haspopup="menu"/, "the chip announces the menu");
  assert.match(band, /:aria-expanded="configMenuOpen"/, "the expanded state is bound, not styled only");
  assert.match(band, /aria-label="config file actions"/, "the chip carries an accessible name");
});
