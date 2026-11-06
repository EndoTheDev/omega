// omega webui: the chat picker groups by provider (card t_1914a8c9).
// The regression: the picker was a flat searchable list - Endo asked for
// the Hermes-desktop shape: models grouped under collapsible provider
// sections, a listing failure shown inline in its section, and footer
// actions (refresh models / add custom model... / edit models...). A
// pick carries the slot grammar ("instance:model" / "default:model")
// the server's resolveSlotModel resolves. These are structural
// source-contract tests (house pattern: read the .vue/.ts, assert the
// wiring); the server-side grammar is Go-tested
// (TestChatModelSlotPickerGrammar, TestModelsGroupedSections).
// Run: node --test test/modelpicker.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));

const picker = () =>
  readFileSync(
    join(root, "app", "components", "chat", "ModelSelect.vue"),
    "utf8",
  );
const app = () => readFileSync(join(root, "app", "app.vue"), "utf8");
const chatTab = () =>
  readFileSync(join(root, "app", "components", "ChatTab.vue"), "utf8");
const composer = () =>
  readFileSync(join(root, "app", "components", "chat", "AppComposer.vue"), "utf8");
const css = () =>
  readFileSync(join(root, "app", "assets", "css", "main.css"), "utf8");

describe("model picker groups by provider (t_1914a8c9)", () => {
  it("ModelSelect renders collapsible provider sections (searchable)", () => {
    const src = picker();
    // one section per provider group ...
    assert.match(src, /v-for="g in visibleGroups"/, "sections iterate");
    assert.match(src, /class="model-section"/, "the section wrapper");
    // ... with a COLLAPSIBLE head (aria-expanded + toggle)
    assert.match(src, /class="model-sec-head"/, "the section header button");
    assert.match(src, /:aria-expanded="isExpanded\(g\)"/, "the collapse state");
    assert.match(src, /function toggle\(/, "the toggle");
    // ... searchable across sections
    assert.match(src, /model-search/, "the search input survives");
    assert.match(
      src,
      /placeholder="filter models"/,
      "the search placeholder survives",
    );
  });

  it("ModelSelect keeps the flat floor: no grouped catalog -> the default section of props.models", () => {
    const src = picker();
    assert.match(
      src,
      /\[\{ name: "default", models: props\.models \}\]/,
      "empty groups fall back to the flat models as the default section",
    );
  });

  it("ModelSelect shows a section's listing failure inline, never a silent drop", () => {
    const src = picker();
    assert.match(src, /v-if="g.error"/, "the error line renders in-section");
    assert.match(src, /class="model-sec-err"/, "the styled error line");
  });

  it("ModelSelect's footer carries the three Hermes actions", () => {
    const src = picker();
    assert.match(src, /class="model-foot"/, "the footer row");
    assert.match(
      src,
      /refresh models/,
      "'refresh models' re-fetches the catalogs",
    );
    assert.match(src, /\$emit\('refresh-models'\)/, "refresh rides the wire");
    assert.match(
      src,
      /add custom model\.\.\./,
      "'add custom model...' opens the add form",
    );
    assert.match(
      src,
      /edit models\.\.\./,
      "'edit models...' opens the editor",
    );
    assert.match(
      src,
      /omega\.model-picker\.customs/,
      "custom models persist under the picker's localStorage key",
    );
  });

  it("ModelSelect's pick emits the slot grammar the server resolves", () => {
    const src = picker();
    // a named section: "instance:model"; default: "default:model"
    assert.match(
      src,
      /g\.name === "default" \? `default:\$\{m\}` : `\$\{g\.name\}:\$\{m\}`/,
      "the pick value must carry the slot prefix server grammar expects",
    );
    assert.match(src, /import type \{ ModelProviderSection \}/, "typed");
  });

  it("app.vue fetches the grouped catalog and threads it down", () => {
    const appSrc = app();
    assert.match(
      appSrc,
      /:groups="modelGroups"/,
      "ChatTab gets the grouped catalog",
    );
    assert.match(
      appSrc,
      /@refresh-models="refreshModels\(\)/,
      "refresh-models re-runs the /models fetch",
    );
    assert.match(
      appSrc,
      /const modelGroups = ref<ModelProviderSection\[\]>\(\[\]\)/,
      "the app holds the sections",
    );
  });

  it("ChatTab and AppComposer pass groups + refresh-models to the picker", () => {
    assert.match(chatTab(), /:groups="groups"/, "ChatTab threads groups");
    assert.match(chatTab(), /\(e: "refresh-models"\)/, "ChatTab re-emits");
    const comp = composer();
    assert.match(comp, /:groups="groups"/, "AppComposer threads groups");
    assert.match(
      comp,
      /@refresh-models="\$emit\('refresh-models'\)"/,
      "AppComposer re-emits refresh",
    );
  });

  it("main.css styles the sections, footer, add form and edit list", () => {
    const src = css();
    assert.match(src, /\.model-body \{/, "the scrolling body");
    assert.match(src, /\.model-sec-head \{/, "the section head");
    assert.match(src, /\.model-foot \{/, "the footer");
    assert.match(src, /\.model-addform/, "the add form");
    assert.match(src, /\.model-editrow \{/, "the edit row");
  });
});
