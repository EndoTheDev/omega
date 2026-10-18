// omega webui: unit tests for the card-7 polish helpers in
// composables/omega.ts - message hover timestamps (formatMsgTime), the
// thinking elapsed meter (formatElapsed + createElapsedMeter, fake clock),
// and the transcript scroll math (isAtBottom / pillVisible, plain numbers).
// Run: node --test modules/ui/test/ (node >= 23 runs TS natively)
import { describe, it } from "node:test";
import assert from "node:assert/strict";

import {
  createElapsedMeter,
  formatMsgTime,
  formatElapsed,
  formatDurationMs,
  isAtBottom,
  pillVisible,
  SCROLL_NEAR_PX,
} from "../app/composables/omega.ts";

import type { RenderTimers } from "../app/composables/omega.ts";

// formatMsgTime tests pin local-calendar expectations: an ISO instant is
// re-rendered through the LOCAL getters of two Date objects, so the
// expectations hold on any machine timezone.
function isoAt(y: number, m: number, d: number, h: number, min: number, s = 0): string {
  // build in local time, hand to Date as epoch ms - never UTC-constructed
  return new Date(y, m - 1, d, h, min, s).toISOString();
}

describe("formatMsgTime", () => {
  const now = new Date(2026, 9, 17, 10, 0); // 17 Oct 2026, 10:00 local

  it("same local day renders local HH:MM", () => {
    assert.equal(formatMsgTime(isoAt(2026, 10, 17, 9, 5), now), "09:05");
    assert.equal(formatMsgTime(isoAt(2026, 10, 17, 23, 59), now), "23:59");
  });

  it("older day renders date + time, same year", () => {
    assert.equal(formatMsgTime(isoAt(2026, 10, 16, 23, 58), now), "16 Oct 23:58");
    assert.equal(formatMsgTime(isoAt(2026, 1, 2, 0, 7), now), "2 Jan 00:07");
  });

  it("another year prefixes the year", () => {
    assert.equal(formatMsgTime(isoAt(2025, 12, 31, 22, 1), now), "2025 31 Dec 22:01");
  });

  it("malformed input renders nothing", () => {
    assert.equal(formatMsgTime("not-a-date", now), "");
  });
});

describe("formatElapsed", () => {
  it("below a minute reads as bare seconds", () => {
    assert.equal(formatElapsed(0, 0), "0ms");
    assert.equal(formatElapsed(1000, 1200), "200ms");
    assert.equal(formatElapsed(0, 59000), "59s");
  });

  it("minutes render padded", () => {
    assert.equal(formatElapsed(0, 60000), "1m 00s");
    assert.equal(formatElapsed(0, 71000), "1m 11s");
    assert.equal(formatElapsed(0, 3599000), "59m 59s");
  });

  it("hours swallow the seconds", () => {
    assert.equal(formatElapsed(0, 3600000), "1h 00m");
    assert.equal(formatElapsed(0, 5400000), "1h 30m");
  });

  it("a negative skew clamps to 0s", () => {
    assert.equal(formatElapsed(1000, 500), "0ms");
  });
});

describe("createElapsedMeter (injectable timers)", () => {
  // manual fake clock for the meter tests: set/clear plus a tick() that
  // fires every timer due at or before the advanced time
  function fakeClock() {
    const pending: { fn: () => void; due: number; handle: symbol }[] = [];
    let now = 0;
    return {
      timers: {
        set: (fn: () => void, ms: number) => {
          const handle = Symbol();
          pending.push({ fn, due: now + ms, handle });
          return handle;
        },
        clear: (handle: unknown) => {
          const i = pending.findIndex((p) => p.handle === handle);
          if (i >= 0) pending.splice(i, 1);
        },
      } as RenderTimers,
      now: () => now,
      tick(ms: number) {
        now += ms;
        for (const p of [...pending]) {
          if (p.due <= now) {
            pending.splice(pending.indexOf(p), 1);
            p.fn();
          }
        }
      },
      pendingCount: () => pending.length,
    };
  }

  it("ticks every second from start, labels count up", () => {
    const clock = fakeClock();
    const labels: string[] = [];
    const meter = createElapsedMeter((l) => labels.push(l), clock.now, clock.timers);
    meter.start();
    assert.deepEqual(labels, ["0ms"], "start emits the zero label once");
    clock.tick(1000);
    clock.tick(1000);
    assert.deepEqual(labels, ["0ms", "1s", "2s"]);
  });

  it("stop freezes the final value and clears the timer", () => {
    const clock = fakeClock();
    const labels: string[] = [];
    const meter = createElapsedMeter((l) => labels.push(l), clock.now, clock.timers);
    meter.start();
    clock.tick(1000);
    clock.tick(1000);
    meter.stop();
    const after = labels[labels.length - 1];
    clock.tick(10000);
    assert.equal(labels[labels.length - 1], after, "no further labels after stop");
    assert.equal(clock.pendingCount(), 0, "no timer left behind");
  });

  it("stop before start is a no-op, start twice does not re-zero", () => {
    const clock = fakeClock();
    const labels: string[] = [];
    const meter = createElapsedMeter((l) => labels.push(l), clock.now, clock.timers);
    meter.stop();
    assert.deepEqual(labels, []);
    meter.start();
    meter.start();
    clock.tick(3500);
    assert.deepEqual(labels, ["0ms", "3s"]);
  });
});

describe("isAtBottom", () => {
  const H = 400; // scrollHeight
  const C = 200; // clientHeight

  it("at the exact bottom is true", () => {
    assert.equal(isAtBottom(H - C, C, H), true);
  });

  it("within the threshold is true", () => {
    assert.equal(isAtBottom(H - C - 1, C, H), true);
    assert.equal(isAtBottom(H - C - (SCROLL_NEAR_PX - 1), C, H), true);
  });

  it("at exactly the threshold is not - the check is strict", () => {
    assert.equal(isAtBottom(H - C - SCROLL_NEAR_PX, C, H), false);
  });

  it("beyond the threshold is false", () => {
    assert.equal(isAtBottom(H - C - SCROLL_NEAR_PX - 1, C, H), false);
    assert.equal(isAtBottom(0, C, H), false);
  });

  it("a custom threshold applies", () => {
    assert.equal(isAtBottom(H - C - 60, C, H, 80), true);
    assert.equal(isAtBottom(H - C - 90, C, H, 80), false);
  });

  it("a non-scrollable container counts as at bottom", () => {
    assert.equal(isAtBottom(0, 200, 200), true);
    assert.equal(isAtBottom(0, 200, 100), true);
  });
});

describe("pillVisible", () => {
  const H = 400;
  const C = 200;

  it("scrolled up shows the pill, stream or not", () => {
    assert.equal(pillVisible(0, C, H), true);
    assert.equal(pillVisible(50, C, H), true);
  });

  it("pinned to the bottom hides the pill", () => {
    assert.equal(pillVisible(H - C, C, H), false);
    assert.equal(pillVisible(H - C - (SCROLL_NEAR_PX - 1), C, H), false);
    assert.equal(pillVisible(H - C - SCROLL_NEAR_PX, C, H), true);
  });

  it("a non-scrollable transcript never shows the pill", () => {
    assert.equal(pillVisible(0, 200, 200), false);
  });
});
// formatDurationMs: server-measured durations (restored sessions carry
// real elapsed on the wire) render in the same voice as formatElapsed
// (timing persistence, in-app comment session 04ea63f3)
describe("formatDurationMs", () => {
  it("renders seconds, minutes, hours like the live meter", () => {
    assert.equal(formatDurationMs(0), "0ms");
    assert.equal(formatDurationMs(5_000), "5s");
    assert.equal(formatDurationMs(62_000), "1m 02s");
    assert.equal(formatDurationMs(3_600_000), "1h 00m");
  });
});

// sub-second spans render milliseconds, not a floored "0s" (in-app
// comment 4: fast thinking/tool/response phases are real measurements)
describe("formatElapsed sub-second", () => {
  it("shows milliseconds under one second", () => {
    assert.equal(formatElapsed(0, 208), "208ms");
    assert.equal(formatElapsed(0, 999), "999ms");
    assert.equal(formatElapsed(0, 0), "0ms");
  });
  it("second+ spans unchanged", () => {
    assert.equal(formatElapsed(0, 5_000), "5s");
    assert.equal(formatElapsed(0, 62_000), "1m 02s");
  });
});
