// omega webui: THE ICONSET TEST (card t_502b8e81). One iconset, one
// wrapper, one import path - structural pins, since the node suite has
// no DOM:
//   1. every dockable-registry icon name exists in the iconset;
//   2. the Icon wrapper is the ONLY component importing lucide-vue-next;
//   3. the composables map is the only place a lucide component is
//      named, and no app source ships a hand-rolled inline <svg> any
//      more (the grep-checkable acceptance: zero icons outside the
//      wrapper's chain);
//   4. the sidebar tab strips are icon-only chips with accessible names.
// Run: node --test engine/webui/test/
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

import { DOCKABLES } from "../app/composables/dockables.ts";
import { ICON_NAMES } from "../app/composables/icons.ts";

test("every dockable icon is a name in the iconset", () => {
  const set = ICON_NAMES as readonly string[];
  for (const d of DOCKABLES) {
    assert.ok(
      set.includes(d.icon),
      `${d.id}: icon "${d.icon}" is not a vocabulary entry`,
    );
  }
});

test("the sidebar vocabulary covers the tab strip (one name one meaning)", () => {
  const set = new Set(ICON_NAMES as readonly string[]);
  for (const expected of [
    "sidebar", "files", "cron", "logs", "skills", "tools", "agents",
    "continuous", "settings", "kanban", "extensions", "insights",
  ]) {
    assert.ok(set.has(expected), `the tab vocabulary carries "${expected}"`);
  }
  // the chat blocks: thinking gets the brain, tool calls share the
  // tools-tab wrench (the card's one-meaning law)
  assert.ok(set.has("brain"), "thinking block glyph exists");
  assert.equal(
    read("app/composables/icons.ts").includes("tools: Wrench"),
    true,
    "tools (tab AND tool-call block) is the wrench",
  );
});

test("lucide-vue-next is imported ONLY by the iconset composable", () => {
  // grep the SPA source: the vocabulary composable is the ONLY import
  // surface of the package (the wrapper consumes it through the map)
  const scan = [
    "app/app.vue",
    "app/composables/compose.ts",
    "app/composables/dockables.ts",
    "app/composables/icons.ts",
    "app/components/DockPane.vue",
    "app/components/DockGhost.vue",
    "app/components/ChatItem.vue",
    "app/components/ChatTab.vue",
    "app/components/chat/AppComposer.vue",
    "app/components/chat/ChatMessage.vue",
    "app/components/chat/ModelSelect.vue",
    "app/components/chat/ThinkingBlock.vue",
    "app/components/chat/ThinkingSelect.vue",
    "app/components/chat/ToolCallLine.vue",
    "app/components/chat/VariantSwitcher.vue",
    "app/components/chat/WorkspaceSelect.vue",
    "app/components/files/FileTree.vue",
    "app/components/files/FilesTab.vue",
    "app/components/files/InspectorPanel.vue",
    "app/components/files/rowicon.ts",
    "app/components/notifications/NotificationCenter.vue",
    "app/components/settings/SettingsTab.vue",
    "app/components/shell/ExtensionsTab.vue",
    "app/components/shell/MenuBar.vue",
    "app/components/sessions/AppSidebar.vue",
  ];
  const offenders = scan
    .filter((f) => read(f).includes('from "lucide-vue-next"'))
    .sort();
  assert.deepEqual(offenders, ["app/composables/icons.ts"]);
  assert.equal(
    read("app/components/ui/Icon.vue").includes('"../../composables/icons"'),
    true,
    "the wrapper resolves names through the iconset composable",
  );
});

test("zero hand-rolled inline SVG in application components", () => {
  // the acceptance is grep-checkable: no template ships its own <svg>
  for (const f of [
    "app/components/DockPane.vue",
    "app/components/DockGhost.vue",
    "app/components/files/FileTree.vue",
    "app/components/files/rowicon.ts",
    "app/components/notifications/NotificationCenter.vue",
    "app/components/settings/SettingsTab.vue",
    "app/components/shell/ExtensionsTab.vue",
    "app/components/shell/MenuBar.vue",
    "app/components/sessions/AppSidebar.vue",
    "app/components/chat/ToolCallLine.vue",
    "app/components/chat/ThinkingBlock.vue",
  ]) {
    assert.ok(!read(f).includes("<svg"), `${f}: no inline svg`);
  }
});

test("the Icon wrapper renders a named icon through the wrapper contract", () => {
  const icon = read("app/components/ui/Icon.vue");
  // the name prop drives the lookup; sizes/strokes are wrapper props,
  // not per-call-site attrs
  assert.match(icon, /defineProps<\{/, "props declaration");
  assert.match(icon, /name: string/, "the semantic NAME prop");
  assert.match(icon, /:size="size"/, "size flows through the wrapper");
  assert.match(icon, /:stroke-width="strokeWidth"/, "stroke flows through the wrapper");
  assert.match(icon, /aria-hidden="true"/, "glyphs are decoration");
  // unknown names throw at the one place
  assert.match(
    read("app/composables/icons.ts"),
    /throw new Error\(\`iconset: unknown icon/,
    "a typo renders a build bug, not a blank box",
  );
});

test("the chat blocks wear their vocabulary glyphs", () => {
  const tool = read("app/components/chat/ToolCallLine.vue");
  assert.match(tool, /class="tool-ic"/, "the tool call row's icon slot");
  assert.match(tool, /name="tools"/, "tool calls share the tools-tab wrench");
  const think = read("app/components/chat/ThinkingBlock.vue");
  assert.match(think, /name="brain"/, "the thinking block's brain glyph");
});

test("sidebar tab strips are ICON-ONLY chips with accessible names", () => {
  const pane = read("app/components/DockPane.vue");
  // the pinned edges (left/right shells) render the icon instead of text
  assert.match(pane, /iconTabs\s*=/, "the icon-tabs rule exists");
  assert.match(pane, /edge === "left" \|\| props\.node\.edge === "right"/,
    "both sidebars iconified");
  assert.match(pane, /v-if="iconTabs && tabIcon\(t\)"/, "the chip is the icon");
  // the accessible name survives the iconification (title + aria-label)
  assert.match(pane, /:aria-label="iconTabs \? tabTitle\(t\) : undefined"/,
    "aria-label carries the tab name");
  assert.match(pane, /:title="tabTitle\(t\)"/, "the tooltip stays");
});