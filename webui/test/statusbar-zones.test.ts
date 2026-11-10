// omega webui: the status bar's two zones (card t_8fd38f88).
// The rulings this pins, one test each:
//   1. TWO zones - LEFT work context (workdir), RIGHT live metrics + build.
//   2. the workdir selector lives in the bar (the composer chip is gone).
//   3. the CLICKABLE SET IS CLOSED: workdir, health, pending.
//   4. a live `N pending` segment, scoped to the OPEN session, click = jump.
//   5. NO model segment in the bar.
//   6. narrow collapses to health dot + workdir (hide-sm on the rest).
//   7. no session label.
// Plus the health popover's facts: the ones with a wire source render, the
// ones without it say "not reported" (never an invented value).
// Run: node --test test/statusbar-zones.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  chatPanelPendingCount,
  renderTranscriptItems,
  transcriptPendingCount,
  type ChatPayloadWire,
  type TranscriptWireItem,
} from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string) => readFileSync(join(root, "app", p), "utf8");
const statusBar = () => src("components/shell/StatusBar.vue");
const appVue = () => src("app.vue");
const css = () => src("assets/css/main.css");

// --- 4a. the count: the panel payload's still-pending calls ---------------

const payload = (rows: ChatPayloadWire["rows"]): ChatPayloadWire => ({
  session: "s1",
  running: false,
  rows,
});

describe("chatPanelPendingCount: the open session's pending calls", () => {
  it("counts a call with no matching result", () => {
    const n = chatPanelPendingCount(
      payload([
        { role: "user", status: "complete", blocks: [{ type: "text", content: "run both" }] },
        {
          role: "assistant",
          status: "complete",
          blocks: [
            { type: "tool_call", tool_call: { id: "c1", name: "delegate.task", arguments: {} } },
            { type: "tool_call", tool_call: { id: "c2", name: "delegate.task", arguments: {} } },
          ],
        },
        { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "c1", content: "ok" }] },
      ]),
    );
    assert.equal(n, 1, "the settled sibling is not pending");
  });

  it("a fully settled conversation counts zero", () => {
    const n = chatPanelPendingCount(
      payload([
        {
          role: "assistant",
          status: "complete",
          blocks: [{ type: "tool_call", tool_call: { id: "c1", name: "shell.run", arguments: {} } }],
        },
        { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "c1", content: "ok" }] },
      ]),
    );
    assert.equal(n, 0);
  });

  it("an in-flight block is RUNNING, not pending (the live tail outranks it)", () => {
    const n = chatPanelPendingCount(
      payload([
        {
          role: "assistant",
          status: "streaming",
          blocks: [{ type: "tool_call", running: true, tool_call: { id: "c1", name: "shell.run", arguments: {} } }],
        },
      ]),
    );
    assert.equal(n, 0);
  });

  it("an id-less call never counts (nothing to match a result against)", () => {
    const n = chatPanelPendingCount(
      payload([
        { role: "assistant", status: "complete", blocks: [{ type: "tool_call", tool_call: { name: "shell.run" } }] },
      ]),
    );
    assert.equal(n, 0);
  });

  it("agrees with the render path's pending derivation", () => {
    // two yielded calls, one woken: the rendered items say one pending,
    // the cheap counter must say the same number
    const wire: TranscriptWireItem[] = [
      { kind: "tool_call", tool_call: { id: "c1", name: "delegate.task", arguments: {} } },
      { kind: "tool_call", tool_call: { id: "c2", name: "delegate.task", arguments: {} } },
      { kind: "tool_result", tool_call_id: "c1", content: "first answer" },
    ];
    assert.equal(transcriptPendingCount(renderTranscriptItems(wire)), 1);
    assert.equal(
      chatPanelPendingCount(
        payload([
          { role: "assistant", status: "complete", blocks: [{ type: "tool_call", tool_call: { id: "c1", name: "delegate.task" } }] },
          { role: "assistant", status: "complete", blocks: [{ type: "tool_call", tool_call: { id: "c2", name: "delegate.task" } }] },
          { role: "tool", status: "complete", blocks: [{ type: "tool_result", tool_call_id: "c1", content: "first answer" }] },
        ]),
      ),
      1,
      "the panel counter matches the render derivation",
    );
  });

  it("the REST restore fallback counts its pending tool items", () => {
    const items = renderTranscriptItems([
      { kind: "tool_call", tool_call: { id: "a", name: "shell.run", arguments: {} } },
      { kind: "tool_call", tool_call: { id: "b", name: "shell.run", arguments: {} } },
      { kind: "tool_call", tool_call: { id: "c", name: "shell.run", arguments: {} } },
      { kind: "tool_result", tool_call_id: "b", content: "ok" },
    ]);
    assert.equal(transcriptPendingCount(items), 2);
    assert.equal(transcriptPendingCount([]), 0);
  });
});

// --- 1/2/3/5/7. the bar's shape ------------------------------------------

describe("the bar's zones and the closed clickable set", () => {
  it("renders exactly two zones, left (work context) and right (metrics)", () => {
    const bar = statusBar();
    assert.equal((bar.match(/class="zone left"/g) ?? []).length, 1);
    assert.equal((bar.match(/class="zone right"/g) ?? []).length, 1);
    // the left zone is the workdir picker, nothing else
    const left = bar.slice(bar.indexOf('class="zone left"'), bar.indexOf('class="zone right"'));
    assert.match(left, /<WorkspaceSelect/);
    assert.doesNotMatch(left, /<button/, "the left zone holds no other control");
  });

  it("the clickable set is CLOSED: workdir, health, pending - nothing else", () => {
    const bar = statusBar();
    // every <button in the bar, by its class list
    const buttons = [...bar.matchAll(/<button[\s\S]*?class="([^"]*)"/g)].map((m) => m[1]);
    assert.deepEqual(
      buttons.sort(),
      ["seg seg-btn health", "seg seg-btn pending hide-sm"],
      "exactly the health + pending segments are buttons (workdir rides the picker component)",
    );
    // display-only segments stay spans - no button block carries them
    const blocks = [...bar.matchAll(/<button[\s\S]*?<\/button>/g)].map((m) => m[0]);
    for (const title of ["tokens used, active session", "model context window", "generation speed, last run", "chunks per second, last run"]) {
      assert.ok(bar.includes(`title="${title}"`), `segment missing: ${title}`);
      for (const b of blocks) assert.ok(!b.includes(title), `${title} must stay a span, never a button`);
    }
  });

  it("no model segment and no session label (rulings 5 and 7)", () => {
    const bar = statusBar();
    assert.doesNotMatch(bar, /<ModelSelect/, "the bar renders no model chip");
    assert.doesNotMatch(bar, /:model=|model-name|:thinking/, "the bar binds no model/effort value");
    // the open tab names the session; the bar takes no session value at all
    assert.doesNotMatch(bar, /sessionRef|:session=|session-title|sessionLabel/, "the bar takes no session prop");
  });

  it("the pending segment is scoped to the open session and jumps to it", () => {
    const bar = statusBar();
    assert.match(bar, /v-if="pending > 0"/, "hidden at zero");
    assert.match(bar, /\{\{ pending \}\} pending/, "the count renders");
    assert.match(bar, /@click="\$emit\('focus-pending'\)"/, "the click emits the jump");
    // the app's wiring: the panel payload only when it COVERS the focused
    // session, the REST restore otherwise (never a machine-wide count)
    const app = appVue();
    assert.match(
      app,
      /payload\.session === focusedSessionId\.value[\s\S]{0,120}chatPanelPendingCount\(payload\)/,
      "the count reads the focused session's panel payload",
    );
    assert.match(app, /transcriptPendingCount\(focusedChatState\.value\.restored\)/, "restore fallback");
    const jump = app.slice(app.indexOf("function focusPendingCalls"));
    assert.match(jump, /dock\.focus\(loc\.paneId, loc\.index\)/, "the click focuses the pane");
  });
});

// --- 6. narrow collapses to health dot + workdir -------------------------

describe("narrow collapse (ruling 6)", () => {
  it("every right-zone metric carries hide-sm; the dot and the picker do not", () => {
    const bar = statusBar();
    const right = bar.slice(bar.indexOf('class="zone right"'));
    // the version/build, messages, tokens, ctx, rates and the pending count all hide
    for (const cls of [
      'class="seg seg-btn pending hide-sm"',
      'class="seg hide-sm"',
    ]) {
      assert.ok(right.includes(cls), `missing narrow-hide segment: ${cls}`);
    }
    // the health button itself does NOT hide - only its word does
    assert.match(right, /class="seg seg-btn health"\s+type="button"[\s\S]{0,400}<span class="hide-sm">/, "only the label hides, the dot stays");
    // the workdir picker sits outside .right, so no rule can hide it
    const left = bar.slice(bar.indexOf('class="zone left"'), bar.indexOf('class="zone right"'));
    assert.doesNotMatch(left, /hide-sm/, "the workdir segment never hides");
    // and the existing utility does the hiding (no second mechanism)
    assert.match(css(), /\.statusbar \.hide-sm \{ display: none; \}/, "the hide-sm utility still backs the collapse");
  });
});

// --- 5. the health popover's facts ---------------------------------------

describe("the health popover", () => {
  it("opens from the health segment and names its role + state to AT", () => {
    const bar = statusBar();
    assert.match(bar, /aria-haspopup="dialog"/);
    assert.match(bar, /:aria-expanded="healthOpen"/);
    assert.match(bar, /class="health-pop"\s+role="dialog"\s+aria-label="server health detail"/);
    assert.match(bar, /function toggleHealth\(\)/);
    assert.match(bar, /closest\("\.health-pop, \.health"\)/, "an outside click closes it");
  });

  it("carries the four ruled facts - and never invents a value it has no source for", () => {
    const bar = statusBar();
    for (const key of ["server", "provider", "database", "uptime", "last error"]) {
      assert.match(bar, new RegExp(`<span class="hp-key">${key}</span>`), `fact row missing: ${key}`);
    }
    // the sourced facts read the real /health body
    assert.match(bar, /\{\{ healthy \? "reachable" : "not reachable" \}\}/);
    assert.match(bar, /\{\{ formatUp \}\}/);
    // the unsourced three say so, with the reason in the footnote
    assert.equal((bar.match(/\{\{ NOT_REPORTED \}\}/g) ?? []).length, 3);
    assert.match(bar, /provider, database and last error: this build's \/health reports/);
    assert.match(bar, /const NOT_REPORTED = "not reported";/);
  });
});
