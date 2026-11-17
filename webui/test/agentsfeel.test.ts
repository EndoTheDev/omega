// omega webui: the AGENTS tab's sessions feel (card t_db378a0d, round 1 of
// the unified theming language).
//
// The sessions sidebar is the REFERENCE and does not move: an agents row
// adopts its interaction vocabulary and nothing else. The ROW LAW anatomy
// (two lines, one right control, semibold name over a dim subtitle, one
// hairline at rest) stays exactly where it was - this card changed the
// interaction layer.
//
// What the node suite can honestly hold:
// - the consumer OPT-IN (feel/expander/activeName are props with the
//   quiet defaults, so the four other registry tabs render byte-identical
//   until their own round);
// - the token facts the vocabulary is made of (chip radius, z1-raised,
//   the accent, the 0.12s rotate) - as source pins on the ONE stylesheet
//   that carries them;
// - the no-toggle law for the default agent, both ends: the row model
//   never sets a toggle, and the shared list never renders one for it;
// - the shell->panel focus seam, both literals identical (the house
//   module-UI law: a module copies the shell's event spelling).
// The rendered result - the hover raising, the hairline fading, the
// chevron turning - is Endo's lane eyeball, and the before/after
// screenshots on the card are its witness.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { FOCUSED_SESSION_EVENT, FOCUSED_SESSION_QUERY_EVENT } from "../app/composables/chat.ts";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const read = (...parts: string[]) => readFileSync(join(root, ...parts), "utf8");

const listSrc = read("webui", "app", "components", "RegistryList.vue");
const panelSrc = read("harness", "continuous", "ui", "AgentsPanel.vue");
const appSrc = read("webui", "app", "app.vue");
const chatSrc = read("webui", "app", "composables", "chat.ts");
const iconsSrc = read("webui", "app", "composables", "icons.ts");

// the five registry consumers' sources - the feel is agents-only this
// round, so every other one must pass no opt-in at all
const QUIET_CONSUMERS: Array<[string, string[]]> = [
  ["skills", ["harness", "skills", "ui", "SkillsList.vue"]],
  ["tools", ["harness", "tools", "ui", "ToolsList.vue"]],
  ["insights", ["harness", "store", "ui", "InsightsTab.vue"]],
];

// --- the opt-in: quiet by default, sessions on request ------------------

test("the sessions feel is a consumer opt-in with the quiet list as its default", () => {
  assert.match(
    listSrc,
    /feel\?: "quiet" \| "sessions"/,
    "the feel is a typed consumer choice",
  );
  assert.match(listSrc, /feel: "quiet"/, "the default is the quiet list");
  assert.match(
    listSrc,
    /'feel-sessions': feel === 'sessions'/,
    "the consumer class is the only way in",
  );
  // not one other registry tab has taken its round yet
  for (const [name, parts] of QUIET_CONSUMERS) {
    const src = read(...parts);
    assert.ok(!src.includes('feel="sessions"'), `${name}: still the quiet list`);
    assert.ok(!src.includes('expander="chevron"'), `${name}: still the text chevron`);
  }
  // and the agents tab asks for the vocabulary
  assert.match(panelSrc, /feel="sessions"/, "the agents tab opts in");
});

test("every feel rule is gated on the consumer class - the quiet list is untouched", () => {
  const style = listSrc.slice(listSrc.indexOf("<style scoped>"));
  const feelRules = style
    .split("}")
    .filter((block) => block.includes("{") && block.includes("--r-chip"))
    .concat(
      style
        .split("}")
        .filter((block) => block.includes("{") && block.includes("--z1-raised"))
        .concat(
          style
            .split("}")
            .filter((block) => block.includes("{") && block.includes("--accent")),
        ),
    );
  assert.ok(feelRules.length >= 3, "the three vocabulary rules exist");
  for (const rule of feelRules) {
    assert.ok(
      rule.includes(".feel-sessions"),
      `an ungated row rule would repaint the other four tabs: ${rule.trim().slice(0, 60)}`,
    );
  }
});

// --- the vocabulary itself (the sessions sidebar's row law) -------------

test("the hover raise speaks the sessions row law", () => {
  assert.match(listSrc, /border-radius: var\(--r-chip\)/, "the chip radius");
  assert.match(
    listSrc,
    /\.registry-list\.feel-sessions \.registry-row:hover/,
    "the hover lands on the row, not on the name alone",
  );
  assert.match(listSrc, /background: var\(--z1-raised\)/, "the raised layer");
  assert.match(
    listSrc,
    /\.registry-row:hover:not\(\.registry-off\) \.reg-row-name[\s\S]{0,120}?opacity: 1/,
    "the name brightens on hover (the rest opacity is 0.9)",
  );
  assert.match(
    listSrc,
    /\.registry-row-open[\s\S]{0,80}?cursor: pointer|registry-row-open \{\n\s*cursor: pointer/,
    "an openable row reads as a pointer",
  );
});

test("the hairline fades under the raised chip and is untouched at rest", () => {
  // the fade is a border-color write ONLY on the hover/active state; the
  // ROW LAW's border-bottom declaration itself is never re-defined here
  assert.match(
    listSrc,
    /\.registry-row:hover,\n\s*\.registry-list\.feel-sessions \.registry-row\.reg-row-active \{\n\s*background: var\(--z1-raised\);[\s\S]{0,900}?border-bottom-color: transparent/,
    "the seam fades out from under the raised chip",
  );
  const restBorder = listSrc.match(/\.registry-list\.feel-sessions \.registry-row \{[^}]*\}/);
  assert.ok(restBorder, "the rest rule exists");
  assert.ok(
    !restBorder![0].includes("border-bottom"),
    "the rest state never re-declares the hairline - the anatomy owns it",
  );
});

test("the seam ABOVE the raised chip fades too - the chip crosses no border", () => {
  // round 1 faded only the row's own border-bottom, so the preceding
  // row's seam stayed drawn across the chip's top edge (review finding 2)
  const seam = listSrc.match(
    /\.registry-list\.feel-sessions \.registry-row:has\(\+ \.registry-row:hover\),\n\s*\.registry-list\.feel-sessions \.registry-row:has\(\+ \.registry-row\.reg-row-active\) \{\n\s*border-bottom-color: transparent;/,
  );
  assert.ok(
    seam,
    "the preceding sibling's seam fades for BOTH raises (hover and the permanently raised active row)",
  );
  // and every :has() rule rides the consumer gate like the rest of the feel
  const hasRules = listSrc.match(/[^{}]*:has\([^{}]*\{[^}]*\}/g) ?? [];
  assert.ok(hasRules.length > 0, "the sibling-seam rule is a real rule");
  for (const rule of hasRules) {
    assert.ok(
      rule.includes(".feel-sessions"),
      `an ungated :has rule would repaint the other four tabs: ${rule.trim().slice(0, 60)}`,
    );
  }
});

test("the chip is a DRESS: the resting row keeps the row law's own box", () => {
  // round 1 wore the negative margin at rest, widening the quiet list's
  // separator ~16px - "at rest the hairline is untouched" is literal
  const restRule = listSrc.match(/\.registry-list\.feel-sessions \.registry-row \{[^}]*\}/);
  assert.ok(restRule, "the rest rule exists");
  for (const decl of ["margin:", "padding:", "border-radius:"]) {
    assert.ok(
      !restRule![0].includes(decl),
      `rest must not carry ${decl.trim()}`,
    );
  }
  // the raise is where the geometry lives
  const raise = listSrc.slice(
    listSrc.indexOf(".registry-list.feel-sessions .registry-row:hover,"),
    listSrc.indexOf(".registry-list.feel-sessions .registry-row:hover:not("),
  );
  for (const decl of ["border-radius: var(--r-chip)", "margin: 0 -8px", "padding: 5px 8px"]) {
    assert.ok(raise.includes(decl), `the raise carries ${decl}`);
  }
});

test("the active row is the open one: raised surface, accent name", () => {
  assert.match(listSrc, /'reg-row-active': activeName === r\.name/, "the row class");
  assert.match(
    listSrc,
    /\.registry-row\.reg-row-active \.reg-row-name \{\n\s*color: var\(--accent\)/,
    "the open row's name is the accent at rest, not only on hover",
  );
  // and the panel feeds it from the shell's focused session
  assert.match(panelSrc, /:active-name="activeAgentName"/, "the panel passes the open agent");
  assert.match(
    panelSrc,
    /canonicalOf\(a\.name\) === id/,
    "the active row is the agent whose CANONICAL chat is focused (card t_10ac04f0 - not the latest session)",
  );
});

// --- the expander: the sessions chevron, per consumer ------------------

test("the expander glyph is per consumer: text at home, the lucide chevron on request", () => {
  assert.match(listSrc, /expander\?: "text" \| "chevron"/, "a typed glyph choice");
  assert.match(listSrc, /expander: "text"/, "the registry family's own glyph is the default");
  assert.match(
    listSrc,
    /<template v-else>\{\{ isExpanded\(r\.name\) \? "▾" : "▸" \}\}<\/template>/,
    "the text chevron survives for the four tabs that have not had their round",
  );
  assert.match(panelSrc, /expander="chevron"/, "the agents tab asks for the chevron");
  assert.match(iconsSrc, /ChevronRight/, "the icon exists in the iconset");
  assert.match(iconsSrc, /"chevron-right": ChevronRight/, "and is addressable by name");
  // 12px, the sessions sidebar's size, stroke, currentColor
  assert.match(listSrc, /name="chevron-right"\n\s+:size="12"/, "12px like the bucket chevron");
  assert.match(
    listSrc,
    /\.registry-expander-icon \{\n\s*display: block;\n\s*transition: transform 0\.12s ease;/,
    "the sessions transition verbatim",
  );
  assert.match(
    listSrc,
    /\.registry-expander-icon\.open \{\n\s*transform: rotate\(90deg\)/,
    "a quarter turn while the row is open",
  );
});

// --- the default agent has no toggle at all ----------------------------

test("the default agent renders NO toggle - the main badge is the reason", () => {
  assert.match(
    panelSrc,
    /noToggle: a\.name === DefaultAgentName/,
    "the row model marks the default agent",
  );
  assert.match(
    listSrc,
    /v-if="toggleable && !r\.noToggle"/,
    "the pill is never rendered for a no-toggle row",
  );
  assert.match(
    listSrc,
    /v-else-if="showState && !r\.noToggle"/,
    "the read-only state text is not rendered either",
  );
  assert.match(
    listSrc,
    /if \(r\.noToggle\) return;/,
    "and the write path refuses it by another door",
  );
  // the old locked pill is GONE, not merely unused: no locked aria, no
  // locked class, no locked branch left to rot
  assert.ok(!listSrc.includes("toggle-locked"), "the dead locked class died");
  assert.ok(!listSrc.includes("locked on"), "the locked aria died with the pill");
  assert.ok(!panelSrc.includes("locked: a.name"), "the row model carries no lock flag");
  // the badge stays, and now explains the absence
  assert.match(panelSrc, /class="agents-main-badge"/, "the main tag stays");
  assert.match(
    panelSrc,
    /always on, so it carries no toggle/,
    "the badge's title answers the missing control",
  );
  // a non-default row keeps its toggle: the gate reads noToggle only
  assert.ok(
    !/toggleable && !.*DefaultAgentName/.test(listSrc),
    "the shared list knows no agent names - the row model decides",
  );
});

// --- the shell -> panel focus seam -------------------------------------

test("the focused-session event spells the same in the shell and the panel", () => {
  assert.equal(FOCUSED_SESSION_EVENT, "omega:focused-session");
  assert.match(chatSrc, /FOCUSED_SESSION_EVENT = "omega:focused-session"/);
  assert.match(
    panelSrc,
    /FOCUSED_SESSION_EVENT = "omega:focused-session"/,
    "the panel copies the literal (module UI imports no shell code)",
  );
  assert.match(
    appSrc,
    /new CustomEvent\(FOCUSED_SESSION_EVENT, \{ detail: id \?\? "" \}\)/,
    "the shell announces the focused session id",
  );
  assert.match(
    panelSrc,
    /window\.addEventListener\(FOCUSED_SESSION_EVENT, onFocusedSession\)/,
    "the panel listens",
  );
  assert.match(
    panelSrc,
    /window\.removeEventListener\(FOCUSED_SESSION_EVENT, onFocusedSession\)/,
    "and stops listening when it unmounts",
  );
  // the opening announcement: a panel mounted while a session already has
  // the focus would otherwise never hear a change
  assert.match(
    appSrc,
    /onMounted\(\(\) => announceFocusedSession\(focusedSessionId\.value\)\)/,
    "the shell announces once on mount too",
  );
});

test("the seam has a PULL: a panel that mounts late asks for the id", () => {
  // The dock renders only the ACTIVE tab's pane slot (DockPane's v-if),
  // so AgentsPanel unmounts on switch-away and mounts again later - both
  // pushes above are spent by then. The panel asks; the shell answers
  // (review round 1, finding 1).
  assert.equal(FOCUSED_SESSION_QUERY_EVENT, "omega:query-focused-session");
  assert.match(chatSrc, /FOCUSED_SESSION_QUERY_EVENT = "omega:query-focused-session"/);
  assert.match(
    panelSrc,
    /FOCUSED_SESSION_QUERY_EVENT = "omega:query-focused-session"/,
    "the panel copies the literal (module UI imports no shell code)",
  );
  // the pull is dispatched AFTER the listener is up: the shell answers
  // synchronously, so an early ask is answered to nobody
  const mount = panelSrc.slice(
    panelSrc.indexOf("onMounted(() => {"),
    panelSrc.indexOf("onUnmounted(() => {"),
  );
  const listen = mount.indexOf(
    "window.addEventListener(FOCUSED_SESSION_EVENT, onFocusedSession)",
  );
  const ask = mount.indexOf(
    "window.dispatchEvent(new CustomEvent(FOCUSED_SESSION_QUERY_EVENT))",
  );
  assert.ok(listen >= 0, "the panel listens on mount");
  assert.ok(ask > listen, "and asks on mount, listener first");
  // the shell answers with what it is showing right now
  assert.match(
    appSrc,
    /import \{[\s\S]*?FOCUSED_SESSION_QUERY_EVENT,[\s\S]*?\} from "\.\/composables\/chat";/,
    "the shell imports the query literal (one spelling, not a second copy)",
  );
  assert.match(
    appSrc,
    /window\.addEventListener\(FOCUSED_SESSION_QUERY_EVENT, onFocusedSessionQuery\)/,
    "the shell answers the pull",
  );
  assert.match(
    appSrc,
    /function onFocusedSessionQuery\(\): void \{\n\s*announceFocusedSession\(focusedSessionId\.value\);/,
    "the answer is the live focused id, not a cached one",
  );
  assert.match(
    appSrc,
    /window\.removeEventListener\(\s*FOCUSED_SESSION_QUERY_EVENT,\s*onFocusedSessionQuery,?\s*\)/,
    "and stops answering when the shell unmounts",
  );
});
