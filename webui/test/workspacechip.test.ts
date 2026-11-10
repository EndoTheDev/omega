// omega webui: the workdir never-blank law (card t_ccf74d88) at its NEW
// home - the status bar's left zone (card t_8fd38f88 moved the workdir
// selector out of the composer). The regression this pins: a bound
// session with an EMPTY row workspace pin has its tool runs rooted at the
// server's default_workspace (the Go side), so the segment must show that
// default - not a blank that hides the folder the commands run in.
// Run: node --test test/workspacechip.test.ts
import { describe, it, afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  defaultWorkspaceNow,
  effectiveWorkdir,
  setDefaultWorkspace,
} from "../app/composables/defaultWorkspace.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string) => readFileSync(join(root, "app", p), "utf8");
const chatTab = () => src("components/ChatTab.vue");
const composer = () => src("components/chat/AppComposer.vue");
const statusBar = () => src("components/shell/StatusBar.vue");
const appVue = () => src("app.vue");

afterEach(() => setDefaultWorkspace(""));

describe("effectiveWorkdir: the never-blank law (t_ccf74d88)", () => {
  it("a bound session's empty row pin falls back to the configured default", () => {
    setDefaultWorkspace("/home/endo");
    assert.equal(
      effectiveWorkdir(true, "", ""),
      "/home/endo",
      "an empty row pin must mirror the configured default, never a blank",
    );
    assert.equal(effectiveWorkdir(true, undefined, undefined), "/home/endo");
  });

  it("a bound session's pin wins over the default", () => {
    setDefaultWorkspace("/home/endo");
    assert.equal(effectiveWorkdir(true, "/tmp/proj", ""), "/tmp/proj");
  });

  it("an unbound tab shows its draft intent, not the default", () => {
    setDefaultWorkspace("/home/endo");
    assert.equal(effectiveWorkdir(false, null, "/tmp/picked"), "/tmp/picked");
    // a deliberate clear ("") stands - the picker renders its own empty state
    assert.equal(effectiveWorkdir(false, null, ""), "");
  });

  it("no default configured: a bound session with no pin reads empty", () => {
    setDefaultWorkspace("");
    assert.equal(effectiveWorkdir(true, "", ""), "");
    assert.equal(defaultWorkspaceNow(), "");
  });
});

describe("the workdir segment's home is the status bar (t_8fd38f88)", () => {
  it("the bar renders the picker in its LEFT zone, bar variant", () => {
    const bar = statusBar();
    assert.match(bar, /class="zone left"/, "the left zone exists");
    const left = bar.slice(bar.indexOf('class="zone left"'));
    assert.match(left, /<WorkspaceSelect[\s\S]{0,200}:workspace="workdir"/, "the picker takes the effective workdir");
    assert.match(left, /variant="bar"/, "the composer's chip treatment is not reused as-is");
    assert.match(left, /@update:workspace="\$emit\('update:workdir', \$event\)"/, "a pick emits up");
  });

  it("the app feeds the bar the focused chat's effective workdir", () => {
    const app = appVue();
    assert.match(app, /:workdir="barWorkdir"/, "the bar is bound to the derivation");
    assert.match(
      app,
      /effectiveWorkdir\([\s\S]{0,160}chatSession\(loc\.tab\)\?\.workspace[\s\S]{0,80}workspaceIntent/,
      "the bar reads the same row-pin / intent pair the composer chip did",
    );
    // the write half still targets the two old sinks
    assert.match(app, /function onBarWorkdir\(v: string\)/, "the bar's pick has a handler");
    assert.match(app, /onChatWorkspace\(loc\.tab, v\)/, "bound: the row PATCH path");
    assert.match(app, /st\.workspaceIntent = v/, "unbound: the draft intent");
  });

  it("the composer chip is GONE from both components", () => {
    assert.doesNotMatch(composer(), /WorkspaceSelect/, "the composer renders no workdir chip");
    assert.doesNotMatch(chatTab(), /workspaceValue/, "ChatTab carries no workdir computation");
    assert.doesNotMatch(chatTab(), /update:workspace/, "ChatTab emits no workspace change");
  });
});
