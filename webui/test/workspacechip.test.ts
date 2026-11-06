// omega webui: unit tests for the workspace chip's default fallback
// (card t_ccf74d88). The regression this pins: a bound session with an
// EMPTY row workspace pin has its tool runs rooted at the server's
// default_workspace (the Go side), so the chip must show that default
// - not a blank that hides the folder the commands actually run in.
// Structural source-contract tests (house pattern: read the .vue,
// assert the wiring). Run: node --test test/workspacechip.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));

const chatTab = () =>
  readFileSync(join(root, "app", "components", "ChatTab.vue"), "utf8");

describe("workspace chip default fallback (t_ccf74d88)", () => {
  it("the chip computation names the law it serves (card t_ccf74d88)", () => {
    // the card comment rides directly above the fallback computation
    assert.match(
      chatTab(),
      /\/\/ The chip mirrors[\s\S]{0,1200}?props\.session\?\.workspace \|\| defaultWorkspaceNow\(\);/,
      "the workspaceValue block lost its card comment",
    );
  });

  it("chatTab.vue imports defaultWorkspaceNow (card t_ccf74d88)", () => {
    assert.match(
      chatTab(),
      /import \{ defaultWorkspaceNow \} from "\.\.\/composables\/defaultWorkspace";/,
      "chatTab.vue lost the defaultWorkspaceNow import",
    );
  });

  it("a bound session's chip mirrors the effective workdir, never a blank", () => {
    assert.match(
      chatTab(),
      /return props\.session\?\.workspace \|\| defaultWorkspaceNow\(\);/,
      "a bound session with an empty row pin no longer falls back to the " +
        "configured default, and the chip no longer names it",
    );
  });
});
