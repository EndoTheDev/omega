// omega webui: the desktop window's title bar. The lane's pixels are ours; the hit
// test, the drag, the double-click maximise and the system menu are the OS's, so the
// pins split the same way: the pure region/state math runs here, the wiring is pinned
// in source, and a plain browser tab is pinned to render no lane at all.
// Run: node --test webui/test/
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const read = (rel: string) => readFileSync(`${root}/${rel}`, "utf8");

import {
  carveCaption,
  chromeReadyMessage,
  clampToBand,
  hasNativeRegions,
  isDesktopWindow,
  maximisedFromEvent,
  MAXIMISE_EVENT,
  UNMAXIMISE_EVENT,
  nonClientRegionMessage,
  regionKindOf,
  regionOf,
  RUNTIME_CONFIG_READY,
  WINDOW_ACTION_EVENTS,
  windowActionMessage,
} from "../app/composables/desktopChrome.ts";
import { ICON_NAMES, iconOf } from "../app/composables/icons.ts";

describe("the hit-test regions are the page's only native surface", () => {
  it("converts CSS pixels to device pixels, outward", () => {
    const region = regionOf(
      "caption",
      { left: 10.4, top: 0, right: 56.2, bottom: 34.6 },
      2,
    );
    assert.deepEqual(region, {
      kind: "caption",
      left: 20,
      top: 0,
      right: 113,
      bottom: 70,
    });
  });

  it("drops an empty box instead of publishing a degenerate rectangle", () => {
    assert.equal(
      regionOf("close", { left: 40, top: 0, right: 40, bottom: 35 }, 1),
      null,
    );
    assert.equal(
      regionOf("close", { left: 40, top: 35, right: 86, bottom: 35 }, 1),
      null,
    );
  });

  it("sends the versioned message the framework parses", () => {
    const message = nonClientRegionMessage([
      { kind: "caption", left: 0, top: 0, right: 500, bottom: 70 },
      { kind: "minimize", left: 500, top: 0, right: 592, bottom: 70 },
    ]);
    assert.ok(message.startsWith("wails:non-client-region:"));
    const payload = JSON.parse(message.slice("wails:non-client-region:".length));
    assert.equal(payload.version, 1);
    assert.equal(payload.regions.length, 2);
    assert.deepEqual(
      payload.regions.map((r: { kind: string }) => r.kind),
      ["caption", "minimize"],
    );
  });

  it("publishes only the three caption kinds", () => {
    assert.equal(regionKindOf("minimize"), "minimize");
    assert.equal(regionKindOf("maximize"), "maximize");
    assert.equal(regionKindOf("close"), "close");
    assert.equal(regionKindOf("caption"), null);
    assert.equal(regionKindOf(null), null);
  });

  it("clamps a zero-height strip to the header's band", () => {
    const band = { left: 0, top: 0, right: 1280, bottom: 35 };
    assert.deepEqual(
      clampToBand({ left: 100, top: 17, right: 200, bottom: 17 }, band),
      { left: 100, top: 0, right: 200, bottom: 35 },
    );
    assert.deepEqual(
      clampToBand({ left: -40, top: -10, right: 5000, bottom: 900 }, band),
      band,
    );
  });

  it("carves the header's controls out of the caption band", () => {
    const strip = { left: 100, top: 0, right: 900, bottom: 35 };
    // the absolutely-centred cluster sits inside the spacer's box
    const cluster = { left: 400, top: 0, right: 600, bottom: 35 };
    assert.deepEqual(carveCaption(strip, [cluster]), [
      { left: 100, top: 0, right: 400, bottom: 35 },
      { left: 600, top: 0, right: 900, bottom: 35 },
    ]);
  });

  it("a hole on an edge trims one side only, a non-overlapping one changes nothing", () => {
    const strip = { left: 0, top: 0, right: 300, bottom: 35 };
    assert.deepEqual(
      carveCaption(strip, [{ left: -10, top: 0, right: 50, bottom: 35 }]),
      [{ left: 50, top: 0, right: 300, bottom: 35 }],
    );
    assert.deepEqual(
      carveCaption(strip, [{ left: 0, top: 40, right: 300, bottom: 60 }]),
      [strip],
    );
    assert.deepEqual(
      carveCaption(strip, [{ left: -5, top: 0, right: 305, bottom: 35 }]),
      [],
    );
  });
});

describe("desktop detection is the shim, never the user agent", () => {
  it("a browser tab has no window._wails", () => {
    assert.equal(isDesktopWindow({}), false);
    assert.equal(isDesktopWindow(undefined), false);
    assert.equal(isDesktopWindow({ _wails: {} }), false);
    assert.equal(
      isDesktopWindow({ _wails: { environment: { OS: "linux" } } }),
      false,
    );
  });

  it("the Wails window on Windows answers true", () => {
    assert.equal(
      isDesktopWindow({ _wails: { environment: { OS: "windows" } } }),
      true,
    );
  });

  it("the native region route needs the flag AND the invoke", () => {
    const base = { environment: { OS: "windows" }, invoke: () => {} };
    assert.equal(
      hasNativeRegions({ _wails: { ...base, flags: { nonClientRegionTracking: true } } }),
      true,
    );
    assert.equal(
      hasNativeRegions({ _wails: { ...base, flags: { nonClientRegionTracking: false } } }),
      false,
    );
    assert.equal(
      hasNativeRegions({ _wails: { environment: { OS: "windows" } } }),
      false,
    );
    assert.equal(hasNativeRegions({}), false);
  });

  it("listens for the boot event the Core shim fires", () => {
    assert.equal(RUNTIME_CONFIG_READY, "wails:runtime-config-ready");
  });
});

describe("the maximize glyph follows the window, not the click count", () => {
  it("maps the framework's WM_SIZE events", () => {
    assert.equal(maximisedFromEvent(MAXIMISE_EVENT), true);
    assert.equal(maximisedFromEvent(UNMAXIMISE_EVENT), false);
    assert.equal(maximisedFromEvent("windows:WindowMinimise"), null);
    assert.equal(maximisedFromEvent(undefined), null);
  });

  it("names the windows events, not a counter", () => {
    assert.equal(MAXIMISE_EVENT, "windows:WindowMaximise");
    assert.equal(UNMAXIMISE_EVENT, "windows:WindowUnMaximise");
  });
});

describe("WindowControls.vue: three equal cells, no fakes", () => {
  const src = read("app/components/shell/WindowControls.vue");

  it("renders the three caption controls with accessible names", () => {
    for (const kind of ["minimize", "maximize", "close"]) {
      assert.ok(
        src.includes(`data-window-control="${kind}"`),
        `the ${kind} cell publishes its native kind`,
      );
    }
    for (const name of ['aria-label="Minimise"', 'aria-label="Close"']) {
      assert.ok(src.includes(name), `missing ${name}`);
    }
    assert.ok(
      src.includes(`:aria-label="maximised ? 'Restore' : 'Maximise'"`),
      "the maximize cell renames itself from the real state",
    );
  });

  it("draws no glyph of its own: the iconset renders them", () => {
    assert.ok(src.includes(`from "../ui/Icon.vue"`), "the shared Icon wrapper");
    assert.ok(!src.includes("<svg"), "no inline svg");
    assert.ok(!src.includes("lucide"), "no direct lucide import");
  });

  it("names its action to the host instead of guessing one", () => {
    for (const action of ["minimise", "toggle-maximise", "close"]) {
      assert.ok(
        src.includes(`@click="$emit('${action}')"`),
        `the ${action} cell emits its action`,
      );
    }
    assert.ok(!src.includes("wails:"), "the host names live in the composable");
  });

  it("exists only inside the Wails window", () => {
    assert.ok(src.includes(`v-if="desktop"`), "a browser tab renders no lane");
  });

  it("flips the restore glyph from the state prop", () => {
    assert.ok(
      src.includes(`:name="maximised ? 'copy' : 'square'"`),
      "square at rest, copy when restored",
    );
  });
});

describe("MenuBar.vue mounts the lane and marks the drag surface", () => {
  const src = read("app/components/shell/MenuBar.vue");

  it("mounts WindowControls at the header's right end and routes its actions", () => {
    assert.ok(src.includes(`from "./WindowControls.vue"`));
    assert.ok(/<WindowControls\b/.test(src));
    for (const action of ["minimise", "toggle-maximise", "close"]) {
      assert.ok(
        src.includes(`@${action}="act('${action}')"`),
        `the ${action} press reaches the host`,
      );
    }
  });

  it("marks exactly the two non-interactive strips as caption", () => {
    const marks = src.match(/\sdata-caption-region/g) ?? [];
    assert.equal(marks.length, 2, "brand + spacer, nothing else");
    assert.ok(
      /class="brand"\s+data-caption-region/.test(src),
      "the brand span is a caption strip",
    );
    assert.ok(
      /class="menubar-spacer"\s+data-caption-region/.test(src),
      "the spacer is a caption strip",
    );
  });

  it("keeps every control clickable: they sit outside the caption strips", () => {
    const controls = src.match(/class="icon-btn edge-toggle"/g) ?? [];
    assert.equal(controls.length, 4, "three edge toggles + the settings gear");
    for (const label of [
      "Toggle left sidebar",
      "Toggle bottom panel",
      "Toggle right sidebar",
      "Open settings",
    ]) {
      assert.ok(src.includes(label), `missing the ${label} control`);
    }
  });
});

describe("main.css: the lane geometry", () => {
  const css = read("app/assets/css/main.css");
  const lane = /\.window-controls \{([^}]*)\}/.exec(css)?.[1] ?? "";
  const cell = /\.win-cell \{([^}]*)\}/.exec(css)?.[1] ?? "";

  it("keeps the bar's one height number", () => {
    assert.ok(css.includes("--menubar-h: 35px"), "--menubar-h is a contract");
  });

  it("sizes each cell to the header's full height", () => {
    assert.ok(cell.includes("height: var(--menubar-h)"));
    assert.ok(cell.includes("width: 46px"));
  });

  it("reserves no fixed lane width: three equal cells size it", () => {
    assert.ok(lane.length > 0, "the lane has its own rule");
    assert.ok(!/(^|\s)width:/.test(lane), "the lane's width comes from its cells");
  });

  it("reaches the window edge and hovers to the error token on close", () => {
    assert.ok(lane.includes("margin-right: -12px"));
    assert.ok(css.includes(".win-close:hover { background: var(--err)"));
  });
});

describe("the caption actions agree across the tree", () => {
  const chrome = read("app/composables/desktopChrome.ts");
  const go = readFileSync(`${root}/../cmd/omega/desktop_windows.go`, "utf8");

  it("names exactly the three actions the host performs", () => {
    const names = Object.values(WINDOW_ACTION_EVENTS);
    assert.equal(new Set(names).size, 3);
    for (const name of names) {
      assert.ok(chrome.includes(name), `the composable names ${name}`);
      assert.ok(
        go.includes(name),
        `cmd/omega/desktop_windows.go performs ${name} - the two sides must not drift`,
      );
    }
  });

  it("sends the bare-name event shortcut, never a payload", () => {
    assert.equal(
      windowActionMessage("close"),
      "wails:event:emit:omega:window:close",
    );
    assert.equal(
      windowActionMessage("toggle-maximise"),
      "wails:event:emit:omega:window:toggle-maximise",
    );
  });
});

describe("the iconset carries the four caption glyphs", () => {
  it("names them, and they resolve", () => {
    const names = ICON_NAMES as readonly string[];
    for (const name of ["minus", "square", "copy", "x"]) {
      assert.ok(names.includes(name), `iconset has ${name}`);
      assert.ok(iconOf(name), `iconset resolves ${name}`);
    }
  });
});

describe("desktopChrome.ts: the framework, not a runtime bundle", () => {
  const src = read("app/composables/desktopChrome.ts");

  it("imports no Wails runtime package", () => {
    assert.ok(!src.includes(`"@wailsio/runtime"`), "the Core shim is the whole runtime");
  });

  it("publishes regions and follows the window events", () => {
    assert.ok(src.includes("wails:non-client-region:"));
    assert.ok(src.includes("dispatchWailsEvent"), "the event entry point it installs");
    assert.ok(src.includes("wails:drag"), "the path-B drag invokes");
  });
});

describe("the chrome announcement the host's frame rule waits for", () => {
  const src = read("app/composables/desktopChrome.ts");

  it("says exactly what the host decodes", () => {
    assert.equal(chromeReadyMessage(), `{"omega":"chrome:ready"}`);
  });

  it("arms the lane on ANY origin, because the shim decides what a desktop is", () => {
    // the shim is injected at NavigationCompleted on every navigation, whatever the page
    assert.equal(isDesktopWindow({ _wails: { environment: { OS: "windows" } } }), true);
    assert.equal(isDesktopWindow({ _wails: { environment: { OS: "windows" } }, location: { origin: "http://192.0.2.10:8098" } }), true);
    assert.equal(isDesktopWindow({ _wails: { environment: { OS: "darwin" } } }), false);
  });

  it("posts the announcement once per arming, before the regions", () => {
    assert.match(src, /let chromeReadySent = false;/, "the composable remembers that it spoke");
    assert.match(src, /if \(!chromeReadySent\) \{[\s\S]{0,120}chromeReadySent = true;[\s\S]{0,120}shim\.invoke\?\.\(chromeReadyMessage\(\)\);/, "one post per arm");
    assert.match(src, /desktop\.value = true;/, "the lane renders on every desktop page, not only where the native route is on");
  });
});
