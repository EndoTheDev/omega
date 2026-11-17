// omega webui: the SETTINGS WIRE (card t_0a9edc72) - the registry ships
// ONE structure and the surfaces read the SLICES they render.
//
// Before this card the launch fetched the whole settings payload to learn
// one value (server.default_workspace: every group, every knob's help
// line, every agent scope) and the settings pane's scope refresh fetched
// it again to read `.scopes`. The server already had the one-knob door
// (`GET /settings?key=`, card t_013b4643); this card adds the scopes
// slice and points the two readers at their own slices.
//
// Pinned here: the knob normalizer (the single shape both readers share),
// the two lazy reads' URLs + failure path, and the source pins that keep
// a future edit from quietly putting the full document back on the boot
// path (the measured byte counts live in the merge comment).
//
// Run: node --test test/settingswire.test.ts
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import {
  fetchSettingKnob,
  fetchSettingsScopes,
  normalizeSettingKnob,
  normalizeSettingsPayload,
} from "../app/composables/omega.ts";

const HERE = dirname(fileURLToPath(import.meta.url));
const APP = resolve(HERE, "..", "app");
const read = (rel: string) => readFileSync(join(APP, rel), "utf8");

/** a fetch stand-in: records the URL, answers one canned response */
function stubFetch(answer: { ok?: boolean; status?: number; body: unknown }) {
  const calls: string[] = [];
  const saved = globalThis.fetch;
  globalThis.fetch = (async (url: string) => {
    calls.push(String(url));
    const status = answer.status ?? (answer.ok === false ? 500 : 200);
    return {
      ok: answer.ok ?? true,
      status,
      json: async () => answer.body,
      text: async () => (typeof answer.body === "string" ? answer.body : JSON.stringify(answer.body)),
    } as unknown as Response;
  }) as typeof fetch;
  return { calls, restore: () => (globalThis.fetch = saved) };
}

const KNOB = {
  key: "server.default_workspace",
  label: "default workspace",
  type: "text",
  value: "C:/work",
  default: "",
  help: "absolute path",
  live: true,
};

// --- the one knob normalizer ------------------------------------------

test("normalizeSettingKnob keeps the fields a control renders", () => {
  const knob = normalizeSettingKnob(KNOB);
  assert.ok(knob);
  assert.equal(knob.key, "server.default_workspace");
  assert.equal(knob.value, "C:/work");
  assert.equal(knob.default, "", "a default of \"\" survives (it is not \"no default\")");
  assert.equal(knob.help, "absolute path");
  assert.equal(knob.live, true);
});

test("normalizeSettingKnob keeps JSON null as the no-default sentinel", () => {
  const knob = normalizeSettingKnob({ ...KNOB, default: undefined });
  assert.ok(knob);
  assert.equal(knob.default, undefined, "absent default stays absent, never fabricated");
  const nulled = normalizeSettingKnob({ ...KNOB, default: null });
  assert.ok(nulled);
  assert.equal(nulled.default, null, "the pane tells null from false/\"\"/0 (card t_c04c959e)");
});

test("normalizeSettingKnob drops a payload with no renderable control", () => {
  assert.equal(normalizeSettingKnob(null), null);
  assert.equal(normalizeSettingKnob("nope"), null);
  assert.equal(normalizeSettingKnob({ ...KNOB, type: "hologram" }), null, "unknown type = no control");
  assert.equal(normalizeSettingKnob({ ...KNOB, key: undefined }), null);
  assert.equal(normalizeSettingKnob({ ...KNOB, value: undefined }), null);
});

test("the group walk and the one-knob read share ONE normalizer", () => {
  // the group walk's knob and the lazy read's knob must be byte-equal in
  // shape - that is what lets the launch's one value and the pane's rows
  // never disagree about a knob's fields
  const viaGroup = normalizeSettingsPayload({ groups: [{ key: "server", label: "server", knobs: [KNOB] }] });
  assert.deepEqual(viaGroup.groups[0]!.knobs[0], normalizeSettingKnob(KNOB));
});

// --- the two lazy reads ------------------------------------------------

test("fetchSettingKnob reads the server's one-knob door", async () => {
  const stub = stubFetch({ body: KNOB });
  try {
    const knob = await fetchSettingKnob("server.default_workspace");
    assert.deepEqual(stub.calls, ["/settings?key=server.default_workspace"]);
    assert.equal(knob?.value, "C:/work");
  } finally {
    stub.restore();
  }
});

test("fetchSettingKnob encodes a dotted key and reports a refusal", async () => {
  const stub = stubFetch({ ok: false, status: 501, body: "settings not configured" });
  try {
    await assert.rejects(() => fetchSettingKnob("server.bind"), /settings not configured/);
    assert.deepEqual(stub.calls, ["/settings?key=server.bind"]);
  } finally {
    stub.restore();
  }
});

test("fetchSettingsScopes reads the scopes slice and normalizes it", async () => {
  const stub = stubFetch({
    body: {
      groups: [],
      scopes: [{ name: "scout", groups: [{ key: "provider", overridden: true, knobs: [{ key: "provider.model_name", value: "m", overridden: true }] }] }],
    },
  });
  try {
    const scopes = await fetchSettingsScopes();
    assert.deepEqual(stub.calls, ["/settings?scopes=1"]);
    assert.equal(scopes.length, 1);
    assert.equal(scopes[0]!.name, "scout");
    assert.equal(scopes[0]!.groups[0]!.overridden, true);
  } finally {
    stub.restore();
  }
});

test("fetchSettingsScopes answers [] for a host with no agent dirs (R8)", async () => {
  const stub = stubFetch({ body: { groups: [], scopes: [] } });
  try {
    assert.deepEqual(await fetchSettingsScopes(), []);
  } finally {
    stub.restore();
  }
});

// --- the source pins: the boot path never pays for the registry again ---

test("the launch reads ONE knob at boot, not the whole registry", () => {
  const app = read("app.vue");
  assert.match(app, /fetchSettingKnob\(DEFAULT_WORKSPACE_KNOB\)/, "the boot read is the one-knob door");
  assert.doesNotMatch(
    app,
    /fetchSettings\(\)/,
    "the launch must never fetch the full settings payload again (the wire bloat this card removed)",
  );
});

test("the settings pane's scope refresh reads the scopes slice", () => {
  const tab = read("components/settings/SettingsTab.vue");
  assert.match(tab, /applyScopes\(await fetchSettingsScopes\(\)\)/);
  assert.doesNotMatch(
    tab,
    /applyScopes\(\(await fetchSettings\(\)\)\.scopes\)/,
    "the full document for one field is the read this card retired",
  );
  // the mount read stays the FULL document: the one-document law renders
  // every category, so that read is not waste (card t_8b464090)
  assert.match(tab, /applyPayload\(await fetchSettings\(\)\)/, "the pane's document read survives");
});
