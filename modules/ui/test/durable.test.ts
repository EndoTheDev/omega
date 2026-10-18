// omega webui: unit tests for the status bar durability helpers in
// composables/omega.ts - persistedTelemetry (durable values picked from
// a session row: token total + last-run rates, zeros when absent).
// Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import { persistedTelemetry, type Session } from "../app/composables/omega.ts";

describe("persistedTelemetry", () => {
  it("fresh session row with no run yields zeros - no phantom telemetry", () => {
    const s: Session = {
      id: "s1",
      created_at: "2026-10-18T00:00:00Z",
      updated_at: "2026-10-18T00:00:00Z",
    };
    assert.deepEqual(persistedTelemetry(s), { tokens: 0, tps: 0, cps: 0 });
  });

  it("undefined row yields zeros - never-rendered session shows nothing", () => {
    assert.deepEqual(persistedTelemetry(undefined), { tokens: 0, tps: 0, cps: 0 });
  });

  it("token total is prompt + completion, exactly the row's numbers", () => {
    const s: Session = {
      id: "s1",
      created_at: "2026-10-18T00:00:00Z",
      updated_at: "2026-10-18T00:00:00Z",
      prompt_tokens: 1200,
      completion_tokens: 320,
    };
    assert.equal(persistedTelemetry(s).tokens, 1520);
  });

  it("last_run rates ride through from the persisted block", () => {
    const s: Session = {
      id: "s1",
      created_at: "2026-10-18T00:00:00Z",
      updated_at: "2026-10-18T00:00:00Z",
      prompt_tokens: 100,
      completion_tokens: 200,
      last_run: { tokens_per_second: 41, chunks_per_second: 17 },
    };
    assert.deepEqual(persistedTelemetry(s), { tokens: 300, tps: 41, cps: 17 });
  });

  it("partial last_run blocks degrade to zeros, not NaN", () => {
    const s: Pick<Session, "id" | "created_at" | "updated_at" | "last_run"> & { last_run: {} } = {
      id: "s1",
      created_at: "2026-10-18T00:00:00Z",
      updated_at: "2026-10-18T00:00:00Z",
      last_run: {},
    };
    assert.deepEqual(persistedTelemetry(s), { tokens: 0, tps: 0, cps: 0 });
  });
});