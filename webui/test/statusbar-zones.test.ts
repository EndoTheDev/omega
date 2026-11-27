// omega webui: the status bar's two zones.
// The rulings this pins, one test each:
//   1. TWO zones - LEFT work context (workdir), RIGHT live metrics + build.
//   2. the workdir selector lives in the bar (the composer chip is gone).
//   3. the CLICKABLE SET IS CLOSED: workdir, health, pending. The health segment is
//      the CONNECTION control (dot + instance name) and its open panel carries its
//      own buttons - panel content is not a bar segment.
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

// 4a.

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

// 1/2/3/5/7.

describe("the bar's zones and the closed clickable set", () => {
  it("renders exactly two zones: health + work context LEFT, metrics + version RIGHT", () => {
    const bar = statusBar();
    assert.equal((bar.match(/class="zone left"/g) ?? []).length, 1);
    assert.equal((bar.match(/class="zone right"/g) ?? []).length, 1);
    const left = bar.slice(bar.indexOf('class="zone left"'), bar.indexOf('class="zone right"'));
    // the health segment LEADS the bar, the picker follows
    assert.match(left, /^[^]*class="seg seg-btn health"/, "health is in the left zone");
    assert.ok(
      left.indexOf('class="seg seg-btn health"') < left.indexOf("<WorkspaceSelect"),
      "health is the FIRST thing on the bar, the workdir picker comes after it",
    );
    assert.match(left, /<WorkspaceSelect/);
  });

  it("the version segment is the RIGHTMOST segment, the message count before it", () => {
    const bar = statusBar();
    const right = bar.slice(bar.indexOf('class="zone right"'), bar.indexOf('class="health-pop"'));
    const version = right.indexOf("server build");
    const messages = right.indexOf("{{ messageCount }} messages");
    assert.ok(version > 0, "the version segment is still in the bar");
    assert.ok(messages > 0 && messages < version, "moved AFTER the messages segment");
    assert.doesNotMatch(
      right.slice(version),
      /class="seg( |")/,
      "the version is the last segment in the right zone",
    );
    // the segment reads the short version ALONE - the uptime the bar used to repeat ("·
    // up 8m 20s") moved into the health popover, so formatUp must not render in this
    // segment any more.
    const seg = right.slice(version, right.indexOf("</span>", version));
    assert.match(seg, /\{\{ shortVersion\(version\) \}\}/, "the segment renders the short version");
    assert.doesNotMatch(seg, /formatUp/, "the bar segment no longer renders the uptime");
    assert.doesNotMatch(seg, /· up/, 'no "· up <duration>" tail on the bar');
  });

  it("the clickable set is CLOSED: workdir, health, pending - nothing else", () => {
    const bar = statusBar();
    // the bar's own buttons: everything before the open panel. A button INSIDE the
    // open panel (the connection switches) is panel content, not a bar segment.
    const segments = bar.slice(0, bar.indexOf('class="health-pop"'));
    const buttons = [...segments.matchAll(/<button[\s\S]*?class="([^"]*)"/g)].map((m) => m[1]);
    assert.deepEqual(
      buttons.sort(),
      ["seg seg-btn health", "seg seg-btn pending hide-sm"],
      "exactly the health + pending segments are buttons (workdir rides the picker component)",
    );
    const panel = bar.slice(bar.indexOf('class="health-pop"'));
    const panelButtons = [...panel.matchAll(/<button[\s\S]*?class="([^"]*)"/g)].map((m) => m[1]);
    assert.ok(panelButtons.length > 0, "the panel carries its own buttons");
    for (const cls of panelButtons) {
      assert.ok(!/(^|\s)seg-btn(\s|$)/.test(cls), `a panel button is never a bar segment: ${cls}`);
    }
    const blocks = [...segments.matchAll(/<button[\s\S]*?<\/button>/g)].map((m) => m[0]);
    for (const title of ["tokens used, active session", "model context window", "generation speed, last run", "chunks per second, last run"]) {
      assert.ok(bar.includes(`title="${title}"`), `segment missing: ${title}`);
      for (const b of blocks) assert.ok(!b.includes(title), `${title} must stay a span, never a button`);
    }
  });

  it("no model segment and no session label (rulings 5 and 7)", () => {
    const bar = statusBar();
    assert.doesNotMatch(bar, /<ModelSelect/, "the bar renders no model chip");
    assert.doesNotMatch(bar, /:model=|model-name|:thinking/, "the bar binds no model/effort value");
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

// 6.

describe("narrow collapse (ruling 6)", () => {
  it("every right-zone metric carries hide-sm; the dot and the picker do not", () => {
    const bar = statusBar();
    const right = bar.slice(bar.indexOf('class="zone right"'));
    for (const cls of [
      'class="seg seg-btn pending hide-sm"',
      'class="seg hide-sm"',
    ]) {
      assert.ok(right.includes(cls), `missing narrow-hide segment: ${cls}`);
    }
    // the health button itself does NOT hide - only its word does
    const left = bar.slice(bar.indexOf('class="zone left"'), bar.indexOf('class="zone right"'));
    assert.match(left, /class="seg seg-btn health"\s+type="button"[\s\S]{0,400}<span class="hide-sm">/, "only the label hides, the dot stays");
    assert.equal((left.match(/hide-sm/g) ?? []).length, 1, "the dot is the ONLY thing left on a narrow bar besides the picker");
    assert.match(css(), /\.statusbar \.hide-sm \{ display: none; \}/, "the hide-sm utility still backs the collapse");
  });
});

// 5.

describe("the health popover", () => {
  it("the control is the connection: the dot and the instance's name", () => {
    const bar = statusBar();
    assert.match(bar, /:aria-label="`Connection: \$\{instanceName\}`"/, "the accessible name is the connection");
    assert.match(
      bar,
      /:title="`\$\{instanceName\} - click for connections and server health`"/,
      "the title names the instance and the click",
    );
    assert.doesNotMatch(bar, /aria-label="server health"/, "the old server-health name is gone");
    assert.match(bar, /<span class="hide-sm">\{\{ instanceName \}\}<\/span>/, "the label is the instance's name");
    assert.doesNotMatch(bar, /"online" : "offline"/, "the online/offline words are gone - the dot is the state");
    assert.match(bar, /:class="\[healthy \? 'ok' : 'down', \{ offline \}\]"/, "the dot keeps ok/down, plus offline for a stale list");
  });

  it("lists the connections ABOVE the facts, the current row marked and inert", () => {
    const bar = statusBar();
    const panel = bar.slice(bar.indexOf('class="health-pop"'));
    assert.ok(panel.indexOf("hp-conn") < panel.indexOf('class="hp-key">server'), "connections lead the panel");
    assert.match(panel, /<span class="hp-head">connections<\/span>/);
    assert.match(panel, /class="hp-crow hp-cur"[\s\S]{0,120}aria-current="true"/, "the current row is marked");
    assert.match(panel, /v-for="row in savedRows"/, "the saved rows render as switches");
    assert.match(panel, /v-if="row\.url === lastUsed"/, "the last switch is named on a non-current row");
    assert.match(panel, /offline - showing the cached list/, "a stale cached list says so");
    // an in-flight read is not an empty list: the pending branch precedes the empty one,
    // so a panel that has asked nothing can never read "no saved connections yet"
    assert.match(
      panel,
      /v-else-if="listPending"\s+class="hp-note hp-pending"\s+role="status">\s*checking saved connections/,
      "the pending read names itself",
    );
    const notes = panel.slice(panel.indexOf("offline - showing the cached list"));
    assert.ok(
      notes.indexOf("checking saved connections") < notes.indexOf("no saved connections yet"),
      "empty is the LAST branch - pending never falls through to it",
    );
    assert.match(panel, /const listPending = ref\(true\);/, "the state starts pending");
    assert.equal((panel.match(/listPending\.value = false;/g) ?? []).length, 2, "both read landings settle it - the load and the app's reply");
    assert.doesNotMatch(panel, /<form|<input/, "no add form - settings owns writes");
    assert.doesNotMatch(panel, /(aria-label|title)="[^"]*remove per-row|Remove \$\{/i, "no per-row remove button");
    assert.doesNotMatch(panel, /(aria-label|title)="[^"]*reconnect/i, "no reconnect control");
  });

  it("opens from the health segment and names its role + state to AT", () => {
    const bar = statusBar();
    assert.match(bar, /aria-haspopup="dialog"/);
    assert.match(bar, /:aria-expanded="healthOpen"/);
    assert.match(bar, /class="health-pop"\s+role="dialog"\s+aria-label="server health detail"/);
    assert.match(bar, /function toggleHealth\(\)/);
    assert.match(bar, /closest\("\.health-pop, \.health"\)/, "an outside click closes it");
  });

  it("carries the five ruled facts - and never invents a value it has no source for", () => {
    const bar = statusBar();
    for (const key of ["server", "provider", "database", "uptime", "last error"]) {
      assert.match(bar, new RegExp(`<span class="hp-key">${key}</span>`), `fact row missing: ${key}`);
    }
    assert.match(bar, /\{\{ healthy \? "reachable" : "not reachable" \}\}/);
    // the uptime row is the ONE place formatUp renders. A bare "formatUp appears
    // somewhere" match would pass for the wrong reason now (the bar dropped its copy),
    // so pin the row AND the count: exactly one template read, inside the popover.
    assert.match(
      bar,
      /<span class="hp-key">uptime<\/span>\s*<span class="hp-val">\{\{ formatUp \}\}<\/span>/,
      "the uptime row renders the computed",
    );
    assert.equal((bar.match(/\{\{ formatUp \}\}/g) ?? []).length, 1, "formatUp renders exactly once - the popover row, never the bar");
    assert.match(bar, /\{\{ providerReachable \? "reachable" : "not reachable" \}\}/, "provider row reads its verdict");
    assert.match(bar, /\{\{ databaseHealthy \? "healthy" : "unhealthy" \}\}/, "database row reads its verdict");
    assert.match(bar, /\{\{ lastErrorMessage \|\| "none" \}\}/, "last error row reads the recorded message, or none");
    assert.equal((bar.match(/\{\{ NOT_REPORTED \}\}/g) ?? []).length, 3);
    assert.equal((bar.match(/v-else class="hp-val muted"/g) ?? []).length, 3, "every row has an unsourced branch");
  });

  it("only names the gap while a row is actually unsourced", () => {
    const bar = statusBar();
    assert.match(bar, /<p v-if="unsourcedCount" class="hp-note">/);
    assert.match(bar, /if \(!providerSourced\.value\) n\+\+;/);
    assert.match(bar, /if \(!databaseSourced\.value\) n\+\+;/);
    assert.match(bar, /if \(!lastErrorSourced\.value\) n\+\+;/);
    assert.match(bar, /no source in this server's \/health/);
  });

  it("decides sourced-vs-unsourced from the wire shapes, not from defaults", () => {
    const bar = statusBar();
    assert.match(bar, /typeof props\.providerReachable === "boolean"/);
    assert.match(bar, /typeof props\.databaseHealthy === "boolean"/);
    // an EMPTY last-error message is sourced ("none since boot").
    assert.match(bar, /typeof props\.lastErrorMessage === "string"/);
    assert.match(bar, /providerReachable\?: boolean \| null;/);
    assert.match(bar, /databaseHealthy\?: boolean \| null;/);
    assert.match(bar, /lastErrorMessage\?: string \| null;/);
  });
});

// 5b.

describe("the health facts' wiring (card t_5e2f3095)", () => {
  it("the wire types carry the three optional facts", () => {
    const socket = src("composables/socket.ts");
    assert.match(socket, /provider\?: HealthProviderFact;/);
    assert.match(socket, /database\?: HealthDatabaseFact;/);
    assert.match(socket, /last_error\?: HealthErrorFact;/);
    const omega = src("composables/omega.ts");
    assert.match(omega, /provider\?: \{[\s\S]{0,120}reachable\?: boolean;/);
    assert.match(omega, /database\?: \{ healthy\?: boolean; detail\?: string \};/);
    assert.match(omega, /last_error\?: \{ message\?: string; at\?: number \};/);
  });

  it("ONE copy path feeds all three /health read sites", () => {
    const app = appVue();
    assert.match(app, /function noteHealthFacts\(h: HealthPush\): void \{/);
    assert.equal((app.match(/noteHealthFacts\(/g) ?? []).length, 4, "the helper + its three call sites (boot, catch-up, pushed frame)");
    assert.equal((app.match(/noteHealthFacts\(h\);/g) ?? []).length, 2, "boot read + reconnect catch-up");
    assert.equal((app.match(/noteHealthFacts\(s\);/g) ?? []).length, 1, "the pushed frame");
  });

  it("the shell hands every fact to the bar as an optional prop", () => {
    const app = appVue();
    for (const prop of [
      ":provider-reachable=\"healthProviderReachable\"",
      ":provider-checked-at=\"healthProviderCheckedAt\"",
      ":provider-detail=\"healthProviderDetail\"",
      ":database-healthy=\"healthDatabaseHealthy\"",
      ":database-detail=\"healthDatabaseDetail\"",
      ":last-error-message=\"healthLastErrorMessage\"",
      ":last-error-at=\"healthLastErrorAt\"",
    ]) {
      assert.ok(app.includes(prop), `app.vue does not pass ${prop}`);
    }
  });
});

// the panel rows are live: one source, one listener, no reload

describe("the connection list updates in place", () => {
  it("repaints from every answer the app emits, with one subscription at mount", () => {
    const bar = statusBar();
    assert.match(
      bar,
      /onInstancesReply\(\(reply\) => \{[\s\S]{0,200}rows\.value = reply\.instances;/,
      "the host's answer IS the panel's list",
    );
    assert.match(bar, /offline\.value = false;/, "a answered list is not the stale one");
    assert.match(bar, /lastUsed\.value = lastUsedInstance\(\)/);
    assert.match(bar, /stopInstanceReplies\(\)/, "the subscription goes with the bar");
  });

  it("hears a browser tab's own save and re-reads the source", () => {
    const bar = statusBar();
    assert.match(bar, /window\.addEventListener\(INSTANCE_CHANGED_EVENT, onInstancesChanged\)/);
    assert.match(bar, /async function refreshInstances\(\): Promise<void> \{\s*const loaded = await loadInstances\(\);/);
  });

  it("re-reads when the Wails shim arms, because it lands after the SPA mounts", () => {
    const bar = statusBar();
    assert.match(bar, /window\.addEventListener\(RUNTIME_CONFIG_READY, onRuntimeReady\)/);
    assert.match(
      bar,
      /function onRuntimeReady\(\) \{\s*void refreshInstances\(\);/,
      "the mount read ran before the shim existed, so the verdict re-reads the source",
    );
    assert.match(
      bar,
      /async function refreshInstances\(\): Promise<void> \{\s*const loaded = await loadInstances\(\);\s*armed\.value = loaded\.shell === "armed";/,
      "the bar keeps the verdict the load was decided by",
    );
  });

  it("switches through the awaited verdict - the app on an armed page, the tab itself otherwise", () => {
    const bar = statusBar();
    assert.match(bar, /whenShellDecided\(\)\.then\(/, "the switch waits on the one verdict");
    assert.match(bar, /switchInstancesInApp\(row\.url\)/, "an armed page hands the switch to the app");
    assert.match(bar, /navigateToInstance\(row\.url\)/, "a browser tab navigates itself");
    assert.doesNotMatch(
      bar,
      /instancesAreAppOwned\(\)/,
      "the panel no longer takes a point-in-time read of the shim",
    );
  });
});

// the default marking: home is the app's own origin, marked wherever the app's list renders

describe("the panel marks home as the default", () => {
  it("pins the home row FIRST among the switches, with its default badge", () => {
    const bar = statusBar();
    const panel = bar.slice(bar.indexOf('class="hp-conn"'));
    const home = panel.indexOf('v-if="homeRow"');
    const saved = panel.indexOf('v-for="row in savedRows"');
    assert.ok(home > 0, "the panel renders a home switch row");
    assert.ok(home < saved, "the home row leads the other saved rows");
    const block = panel.slice(home, saved);
    assert.match(block, /class="hp-crow hp-switch"/, "the home row is a switch, not a label");
    assert.match(block, /@click="switchTo\(homeRow\)"/, "clicking it switches home");
    assert.match(block, /<span class="hp-default">default<\/span>/, "the home row carries the default badge");
    assert.match(
      panel,
      /v-if="onHome"\s+class="hp-default">\s*default\s*<\/span>/,
      "the current row is badged default when the page is on home",
    );
  });

  it("renders the badge as TEXT, never a colour and never a tooltip-only mark", () => {
    const bar = statusBar();
    assert.equal(
      (bar.match(/class="hp-default">\s*default\s*<\/span>/g) ?? []).length,
      2,
      "both home rows carry the word",
    );
    // a badge whose only carrier were a title would fail here: the text is in the DOM
    assert.doesNotMatch(bar, /class="hp-default"[^>]*title=/, "the badge is not a tooltip-only marker");
    assert.match(bar, /\.hp-default \{/, "the badge has its own chip style");
    assert.doesNotMatch(bar, /\.hp-default[^}]*#[0-9a-fA-F]{3,8}/, "the badge paints no colour literal");
  });

  it("keeps the current home row inert and marked, and the switch row one click away", () => {
    const bar = statusBar();
    assert.match(bar, /class="hp-crow hp-cur"[\s\S]{0,120}aria-current="true"/, "the current row is still the marked group");
    assert.match(bar, /const onHome = computed\(\(\) => !!homeOrigin\.value && props\.currentOrigin === homeOrigin\.value\)/);
    assert.match(
      bar,
      /homeOrigin\.value && props\.currentOrigin !== homeOrigin\.value\s*\?\s*\{ name: homeName\.value, url: homeOrigin\.value \}\s*:\s*null/,
      "no home switch row is drawn while the page IS home",
    );
  });

  it("takes home from the app's answer only, so a browser tab's list is unchanged", () => {
    const bar = statusBar();
    assert.match(bar, /const homeOrigin = ref\(""\);/, "a page starts with no home");
    assert.equal(
      (bar.match(/homeOrigin\.value = (loaded|reply)\.home \?\? ""/g) ?? []).length,
      2,
      "home is read from the load and every app answer - never from a stored row",
    );
    assert.match(bar, /const homeName = computed\(\s*\(\) => rows\.value\.find\(\(row\) => row\.url === homeOrigin\.value\)\?\.name \?\? "Home"/);
    assert.match(
      bar,
      /row\.url !== props\.currentOrigin && !\(homeOrigin\.value && row\.url === homeOrigin\.value\)/,
      "a stored row naming the home address is folded into the home row",
    );
    // a tab has no home: both gates fall back to the page's own connection
    assert.match(bar, /return rows\.value\.find\(\(row\) => row\.url === props\.currentOrigin\)\?\.name \?\? "This instance"/);
  });
});

const settingsField = () => src("components/settings/InstancesField.vue");

// the per-row state dots: one batch read, one dot per row, never colour alone

describe("the connection rows' state dots", () => {
  it("the panel paints a dot per row and names the state in the row's title", () => {
    const bar = statusBar();
    assert.match(
      bar,
      /class="dot hp-dot"\s*\n\s*:class="dotClass\(currentOrigin\)"/,
      "the current connection's row carries its own dot",
    );
    assert.match(
      bar,
      /class="dot hp-dot"\s*\n\s*:class="dotClass\(row\.url\)"\s*\n\s*aria-hidden="true"/,
      "a saved row carries a dot, and the glyph alone carries no name",
    );
    assert.match(bar, /:title="connectionTitle\(row\.name, row\.url\)"/, "the reason rides the row's title");
    assert.match(
      bar,
      /:aria-label="connectionLabel\(`Switch to \$\{row\.name\}`, row\.url\)"/,
      "the row's accessible name carries the state word",
    );
  });

  it("the poll opens with the panel and dies with it", () => {
    const bar = statusBar();
    assert.match(bar, /watch\(\[healthOpen, honestStates\], \(\[open, honest\]\) => \{/, "the panel's open state and the dot honesty are the poll's switch");
    assert.match(bar, /if \(open && honest\) statusPoll\.start\(statusRows\.value\);/);
    assert.match(bar, /else statusPoll\.stop\(\);/);
    assert.match(bar, /onBeforeUnmount\([\s\S]{0,400}statusPoll\.stop\(\)/, "the bar unmounts the poll");
    assert.match(bar, /v-if="statusNote"/, "an old build's muted note has a home");
    assert.match(
      bar,
      /const statusRows = computed<SavedInstance\[\]>\(\(\) => \{/,
      "the page's own connection rides the batch even when it is not a saved row",
    );
  });

  it("batches no states on a page that cannot see the home's network, and says so", () => {
    const bar = statusBar();
    assert.match(
      bar,
      /const desktopOffHome = computed\(\(\) => armed\.value && !onHome\.value\);/,
      "the dots' honesty is the verdict plus the page's own place",
    );
    assert.match(
      bar,
      /const honestStates = computed\(\(\) => decided\.value && !desktopOffHome\.value\);/,
      "nothing is claimed before the first read carried a verdict",
    );
    assert.match(bar, /states not checked here \(this page cannot see the home's network\)/, "the muted note names the reason");
    // the note rides the existing hp-note idiom, and the batch never starts while hidden
    assert.match(bar, /v-if="desktopOffHome"\s+class="hp-note"/);
    assert.match(bar, /if \(open && honest\) statusPoll\.start/, "no batch opens on a non-home desktop page");
  });

  it("renders a refused switch as words in the connection block", () => {
    const bar = statusBar();
    assert.match(bar, /const switchNote = ref\(""\);/, "the refusal has its own note");
    assert.match(
      bar,
      /onInstancesReply\(\(reply\) => \{[\s\S]{0,320}switchNote\.value = reply\.error;/,
      "the app's error reaches the panel",
    );
    assert.match(
      bar,
      /v-if="switchNote"\s+class="hp-note"\s+role="status">\s*\{\{ switchNote \}\}/,
      "the refusal is announced, not swallowed",
    );
  });

  it("the settings pane paints the same dots as icons whose names carry the word", () => {
    const pane = settingsField();
    assert.match(pane, /class="dot instance-dot"\s*\n\s*:class="dotClass\(row\.url\)"/);
    assert.match(pane, /role="img"\s*\n\s*:aria-label="stateLabel\(row\.url\)"/);
    assert.match(pane, /onMounted\(\(\) => \{[\s\S]{0,120}statusPoll\.start\(rows\.value\)/);
    assert.match(pane, /onBeforeUnmount\(\(\) => \{[\s\S]{0,80}statusPoll\.stop\(\)/);
    assert.match(pane, /v-if="statusNote"/);
  });

  it("the four states are token paints: warn is a token, unverified is a hollow ring", () => {
    const sheet = css();
    assert.match(sheet, /--warn: #[0-9a-fA-F]{6};/, "the warning token ships with its default");
    assert.match(sheet, /\.dot\.warn \{ background: var\(--warn\); \}/);
    assert.match(
      sheet,
      /\.dot\.unverified \{\s*background: transparent;\s*border: 1px solid var\(--text-dim\);\s*\}/,
      "unverified is a ring, not a fourth colour",
    );
  });

  it("neither surface paints a colour literal", () => {
    for (const surface of [statusBar(), settingsField()]) {
      assert.doesNotMatch(
        surface,
        /(?:^|[;{\s])(?:background|background-color|color|border|border-color):\s*#[0-9a-fA-F]{3,8}\b/m,
        "a component paints only with tokens",
      );
    }
  });
});
