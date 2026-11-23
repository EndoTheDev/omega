// The status bar's tok figure has ONE meaning: the session's CUMULATIVE
// provider-reported usage. The store adds every run's prompt + completion to the
// session row (store.AddSessionTokens), so a live run and its reload must render
// the same number. These pins hold both render paths to it.
//
// Run: node --test test/tokenstat.test.ts
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  cacheShare,
  durableTokens,
  estimateTokens,
  formatCacheShare,
  usageTokens,
} from "../app/composables/tokenstat.ts";
import { persistedTelemetry, type Session } from "../app/composables/omega.ts";

const root = fileURLToPath(new URL("..", import.meta.url));
const src = (p: string) => readFileSync(join(root, p), "utf8");

const row = (prompt: number, completion: number): Session => ({
  id: "s1",
  created_at: "2026-11-19T00:00:00Z",
  updated_at: "2026-11-19T00:00:00Z",
  prompt_tokens: prompt,
  completion_tokens: completion,
});

describe("the tok stat: ONE meaning, live == restored", () => {
  it("a live run lands on the same total its reload renders", () => {
    // turn 1: the provider reported 5654 prompt + 118 completion
    const afterTurn1 = persistedTelemetry(row(5654, 118));
    assert.equal(afterTurn1.tokens, 5772, "the session row is cumulative over runs");
    // turn 2 streams on the SAME session: durable base 5772, usage 11433 + 464
    const live = usageTokens(afterTurn1.tokens, 11433, 464);
    // ...and the store's row after that run accumulates the same usage
    const restored = persistedTelemetry(row(5654 + 11433, 118 + 464));
    assert.equal(live.total, 17669);
    assert.equal(restored.tokens, live.total, "live and restored render the SAME number");
    assert.equal(live.estimated, false, "reported usage is never marked as an estimate");
    assert.equal(durableTokens(restored.tokens).total, live.total);
  });

  it("a second run counts ONTO the base, it never replaces it", () => {
    const first = usageTokens(0, 5654, 118);
    const second = usageTokens(first.total, 11433, 464);
    assert.equal(first.total, 5772);
    assert.equal(second.total, first.total + 11433 + 464);
    assert.notEqual(second.total, 11433 + 464, "one run's usage is not the session's figure");
  });

  it("a run start keeps the durable total - no zero, no single-run value", () => {
    assert.deepEqual(durableTokens(17669), { total: 17669, estimated: false });
  });

  it("an estimate is marked, counts only new output, and never goes below the base", () => {
    assert.deepEqual(estimateTokens(5772, 400), { total: 5872, estimated: true });
    assert.deepEqual(estimateTokens(5772, 0), { total: 5772, estimated: true });
    assert.deepEqual(estimateTokens(5772, -900), { total: 5772, estimated: true });
  });
});

describe("one writer, one meaning (source pins)", () => {
  const chat = src("app/composables/chat.ts");
  const bar = src("app/components/shell/StatusBar.vue");
  const app = src("app/app.vue");

  it("chat.ts writes the bar's token figure in exactly ONE place", () => {
    const writes = chat.match(/st\.tokens\s*=/g) ?? [];
    assert.equal(writes.length, 1, "every path paints through paintTokens");
    assert.match(chat, /function paintTokens\(/);
    assert.match(chat, /st\.tokensDurable = /, "the durable base is stamped where the store speaks");
  });

  it("both render paths paint through the helpers", () => {
    assert.match(chat, /paintDurable\(st, durable\.tokens\)/, "restore + run end read the store row");
    assert.match(chat, /paintTokens\(st, usageTokens\(/, "reported usage counts onto the base");
    assert.match(chat, /paintTokens\(st, estimateTokens\(/, "an unreported run stays an estimate");
    assert.doesNotMatch(chat, /Math\.ceil\(chatPanelContent/, "no raw panel estimate outside the helper");
  });

  it("no estimate can reach the store: the webui never writes usage", () => {
    for (const f of [chat, app]) assert.doesNotMatch(f, /method: ?"POST"[\s\S]{0,200}tokens/);
    assert.doesNotMatch(chat, /AddSessionTokens/, "usage is the server's to record");
  });

  it("the bar marks the estimate and keeps the ONE label", () => {
    assert.match(bar, /title="tokens used, active session"/);
    assert.match(bar, /\{\{ tokensEstimated \? "~" : "" \}\}\{\{ tokens \}\} tok/);
    assert.match(bar, /tokensEstimated\?: boolean/);
    assert.match(app, /:tokens-estimated="tokensEstimated"/);
    assert.match(
      app,
      /const tokensEstimated = computed\(\(\) => focusedChatState\.value\.tokensEstimated\)/,
    );
  });
});

// The prompt-cache share is a SECOND, per-response figure: it never joins the
// cumulative token total, and absence (nil) is not a measured zero.
describe("the prompt-cache share", () => {
  const barSrc = src("app/components/shell/StatusBar.vue");
  const appSrc = src("app/app.vue");

  it("paints cached over prompt as a percent with one decimal", () => {
    assert.equal(formatCacheShare(cacheShare(8000, 90000)), "8.9%");
    assert.equal(formatCacheShare(cacheShare(0, 90000)), "0%", "a reported zero IS zero");
    assert.equal(formatCacheShare(cacheShare(90000, 90000)), "100%");
    assert.equal(formatCacheShare(cacheShare(1, 3)), "33.3%");
  });

  it("a nil cache figure renders 'not reported', never 0%", () => {
    const share = cacheShare(null, 90000);
    assert.equal(share, null, "absence stays absence");
    assert.equal(formatCacheShare(share), "not reported");
    assert.notEqual(formatCacheShare(share), formatCacheShare(cacheShare(0, 90000)));
  });

  it("no denominator, no percent", () => {
    assert.equal(cacheShare(0, 0), null);
    assert.equal(cacheShare(5, 0), null);
  });

  it("the wire keeps nil distinct from zero, and both paint paths land on cachePercent", () => {
    const omega = src("app/composables/omega.ts");
    assert.match(omega, /cachedTokens: number \| null/, "the handler type carries the absence");
    assert.match(omega, /p\.cached_count == null \? null : Number\(p\.cached_count\)/);
    const chat = src("app/composables/chat.ts");
    const paints = chat.match(/st\.cachePercent = cacheShare\(/g) ?? [];
    assert.equal(paints.length, 2, "the reattach path and the send path both paint");
    assert.match(chat, /cachePercent: number \| null/);
    assert.match(appSrc, /:cache-percent="cachePercent"/);
    assert.match(barSrc, /formatCacheShare\(props\.cachePercent\)/);
    assert.match(barSrc, /cache \$\{NOT_REPORTED\}/, "the absent figure says so in words");
    assert.doesNotMatch(barSrc, /cached_count as number\) \|\| 0/, "no zero fallback in the bar");
  });
});
