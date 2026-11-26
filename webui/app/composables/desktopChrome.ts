// omega webui: the desktop window's chrome. Inside the Wails window omega's own header
// IS the title bar: the OS owns the hit test, the page owns the pixels.
//
// The page publishes rectangles (device pixels, last region wins) and Wails answers
// WM_NCHITTEST from them, so the three cells in WindowControls.vue are real caption
// buttons - the press is a native one, never a CSS handler. Drag, the double-click
// maximise, the caption system menu and the edge resize borders are the OS's too.
//
// The Core shim (window._wails: environment, flags, invoke) is injected at
// NavigationCompleted, AFTER the SPA's first mount, so detection is the config-ready
// event with a bounded poll behind it. A plain browser tab never matches, which is what
// keeps the lane out of `omega serve`.

import { nextTick, onBeforeUnmount, onMounted, ref, type Ref } from "vue";

/** the DOM event the Core shim fires once window._wails exists */
export const RUNTIME_CONFIG_READY = "wails:runtime-config-ready";

/** the region message's payload version (wails v3.1.0 non-client regions) */
export const NON_CLIENT_REGION_VERSION = 1;

/** the shim lands right after NavigationCompleted; the poll is the backstop */
export const RUNTIME_POLL_MS = 40;
export const RUNTIME_POLL_TRIES = 75;

/** the cells carry the native hit-test kind; the strips carry the drag surface */
export const CAPTION_ATTR = "data-caption-region";
export const CONTROL_ATTR = "data-window-control";

/**
 * The page -> host names the caption cells send. Wails routes a press on one of our
 * published regions to the page as mouse input instead of performing it, so the action
 * is named here and cmd/omega/desktop_windows.go performs it (desktopWindowEvents).
 */
export const WINDOW_ACTION_EVENTS = {
  minimise: "omega:window:minimise",
  "toggle-maximise": "omega:window:toggle-maximise",
  close: "omega:window:close",
} as const;

export type WindowAction = keyof typeof WINDOW_ACTION_EVENTS;

/** the invoke body a cell sends: the framework's bare-name event shortcut */
export function windowActionMessage(action: WindowAction): string {
  return "wails:event:emit:" + WINDOW_ACTION_EVENTS[action];
}

/** the maximize-state feed: Wails' WM_SIZE events, by name */
export const MAXIMISE_EVENT = "windows:WindowMaximise";
export const UNMAXIMISE_EVENT = "windows:WindowUnMaximise";

export type RegionKind = "caption" | "minimize" | "maximize" | "close";

export interface HitRegion {
  kind: RegionKind;
  left: number;
  top: number;
  right: number;
  bottom: number;
}

export interface ClientBox {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** the slice of the injected shim this module reads */
export interface WailsHost {
  _wails?: {
    environment?: { OS?: string };
    flags?: Record<string, unknown>;
    invoke?: (message: string) => void;
    dispatchWailsEvent?: (event: { name?: unknown }) => void;
  };
}

/** true only inside the Windows Wails window; a browser tab is never a desktop */
export function isDesktopWindow(host: unknown): boolean {
  return (host as WailsHost | undefined)?._wails?.environment?.OS === "windows";
}

/** the native hit-test route: the composition-hosted controller answers the regions */
export function hasNativeRegions(host: unknown): boolean {
  const api = (host as WailsHost | undefined)?._wails;
  return api?.flags?.nonClientRegionTracking === true && typeof api.invoke === "function";
}

/** a CSS-pixel box as the device-pixel rectangle the hit test takes, or null when empty */
export function regionOf(
  kind: RegionKind,
  box: ClientBox,
  dpr: number,
): HitRegion | null {
  const left = Math.floor(box.left * dpr);
  const top = Math.floor(box.top * dpr);
  const right = Math.ceil(box.right * dpr);
  const bottom = Math.ceil(box.bottom * dpr);
  if (right <= left || bottom <= top) return null;
  return { kind, left, top, right, bottom };
}

/** the attribute value a cell publishes; anything else publishes nothing */
export function regionKindOf(value: string | null): RegionKind | null {
  if (value === "minimize" || value === "maximize" || value === "close") return value;
  return null;
}

/**
 * The caption band a strip occupies: the strip's own horizontal span inside the
 * header's, carried over the header's FULL height - a zero-height text wrapper still
 * drags across the bar.
 */
export function clampToBand(box: ClientBox, band: ClientBox): ClientBox {
  return {
    left: Math.max(box.left, band.left),
    top: band.top,
    right: Math.min(box.right, band.right),
    bottom: band.bottom,
  };
}

/**
 * A caption strip cut around the header's controls: the bar is ONE row, so a hole
 * splits the band into a left and a right part and nothing else is captioned. Without
 * this the absolutely-centred nav cluster sits inside the spacer's box and would lose
 * its clicks to the drag surface.
 */
export function carveCaption(box: ClientBox, holes: ClientBox[]): ClientBox[] {
  let parts: ClientBox[] = [box];
  for (const hole of holes) {
    const kept: ClientBox[] = [];
    for (const part of parts) {
      const clear =
        hole.right <= part.left ||
        hole.left >= part.right ||
        hole.bottom <= part.top ||
        hole.top >= part.bottom;
      if (clear) {
        kept.push(part);
        continue;
      }
      if (hole.left > part.left) kept.push({ ...part, right: hole.left });
      if (hole.right < part.right) kept.push({ ...part, left: hole.right });
    }
    parts = kept;
  }
  return parts.filter((part) => part.right > part.left && part.bottom > part.top);
}

/** the whole "wails:non-client-region:" message body */
export function nonClientRegionMessage(regions: HitRegion[]): string {
  return (
    "wails:non-client-region:" +
    JSON.stringify({ version: NON_CLIENT_REGION_VERSION, regions })
  );
}

/** the maximize state an event name carries; null for every unrelated event */
export function maximisedFromEvent(name: unknown): boolean | null {
  if (name === MAXIMISE_EVENT) return true;
  if (name === UNMAXIMISE_EVENT) return false;
  return null;
}

export interface DesktopChrome {
  /** the lane renders (the header is a title bar) */
  desktop: Ref<boolean>;
  /** the window's REAL maximize state, from the framework's own events */
  maximised: Ref<boolean>;
  /** send one caption-cell press to the host, which performs it */
  act: (action: WindowAction) => void;
}

/**
 * Watch for the Wails window and arm the chrome: publish the hit-test regions from the
 * marked elements, follow the real maximize state, and - where the native region route
 * is absent - fall back to the page-side drag invokes.
 */
export function useDesktopChrome(): DesktopChrome {
  const desktop = ref(false);
  const maximised = ref(false);
  const host = () => globalThis as unknown as WailsHost;
  const api = () => host()._wails;
  const act = (action: WindowAction) => {
    api()?.invoke?.(windowActionMessage(action));
  };
  if (typeof window === "undefined") return { desktop, maximised, act };

  let timer: ReturnType<typeof setInterval> | null = null;
  let frame = 0;
  let live = true;
  const detach: Array<() => void> = [];

  function stopPolling(): void {
    if (timer !== null) clearInterval(timer);
    timer = null;
  }

  function publish(): void {
    const shim = api();
    if (!live || !shim?.invoke || shim.flags?.nonClientRegionTracking !== true) return;
    const dpr = window.devicePixelRatio || 1;
    const regions: HitRegion[] = [];
    const header = document.querySelector(".menubar");
    const band = header?.getBoundingClientRect();
    if (!band) return;
    // the header's controls are holes in the caption band: the cluster is absolutely
    // centred, so its box sits INSIDE the spacer's flow box
    const holes: ClientBox[] = [];
    for (const el of document.querySelectorAll(".menubar button, .menubar .nav-cluster")) {
      holes.push(el.getBoundingClientRect());
    }
    // captions first: a later region wins an overlap, so the cells keep their press
    for (const el of document.querySelectorAll(`[${CAPTION_ATTR}]`)) {
      const strip = clampToBand(el.getBoundingClientRect(), band);
      for (const part of carveCaption(strip, holes)) {
        const region = regionOf("caption", part, dpr);
        if (region) regions.push(region);
      }
    }
    for (const el of document.querySelectorAll(`[${CONTROL_ATTR}]`)) {
      const kind = regionKindOf(el.getAttribute(CONTROL_ATTR));
      const region = kind ? regionOf(kind, el.getBoundingClientRect(), dpr) : null;
      if (region) regions.push(region);
    }
    shim.invoke(nonClientRegionMessage(regions));
  }

  function schedulePublish(): void {
    if (!live || frame) return;
    // a timer, not rAF: the scheduler pauses rAF while the window is hidden (the tray
    // keeps an omega window hidden, and it must be true again when it is shown)
    frame = setTimeout(() => {
      frame = 0;
      publish();
    }, 0) as unknown as number;
  }

  // Wails buffers every window event AND every ExecJS until the page announces the
  // runtime (@wailsio/runtime sends this at import); without it no window event is
  // ever delivered. This shim is the page's stand-in for that import.
  function announceRuntime(shim: NonNullable<WailsHost["_wails"]>): void {
    shim.invoke?.("wails:runtime:ready");
  }

  // Wails' event delivery calls this entry point when it exists; installing it is how a
  // page with no @wailsio/runtime still hears the window's real state changes.
  function followWindowState(shim: NonNullable<WailsHost["_wails"]>): void {
    const previous = shim.dispatchWailsEvent;
    shim.dispatchWailsEvent = (event) => {
      if (typeof previous === "function") previous.call(shim, event);
      const state = maximisedFromEvent(event?.name);
      if (state === null) return;
      maximised.value = state;
      // a maximized client area is re-inset to the work area: the rects moved
      schedulePublish();
    };
  }

  // No native region route: the caption strips carry the drag themselves.
  function wireDragFallback(): void {
    for (const el of document.querySelectorAll(`[${CAPTION_ATTR}]`)) {
      const down = (event: Event) => {
        if ((event as MouseEvent).button !== 0) return;
        api()?.invoke?.("wails:drag");
      };
      const double = () => api()?.invoke?.("wails:drag:doubleclick");
      el.addEventListener("mousedown", down);
      el.addEventListener("dblclick", double);
      detach.push(() => {
        el.removeEventListener("mousedown", down);
        el.removeEventListener("dblclick", double);
      });
    }
  }

  function watchResolution(): void {
    if (typeof window.matchMedia !== "function") return;
    let query: MediaQueryList | null = null;
    let stop = false;
    const attach = () => {
      if (stop) return;
      query = window.matchMedia(`(resolution: ${window.devicePixelRatio || 1}dppx)`);
      query.addEventListener("change", onChange, { once: true });
    };
    const onChange = () => {
      attach();
      schedulePublish();
    };
    attach();
    detach.push(() => {
      stop = true;
      query?.removeEventListener("change", onChange);
    });
  }

  function activate(): void {
    const shim = api();
    if (!live || !shim || !isDesktopWindow(globalThis)) return;
    stopPolling();
    followWindowState(shim);
    announceRuntime(shim);

    const onResize = () => schedulePublish();
    window.addEventListener("resize", onResize);
    detach.push(() => window.removeEventListener("resize", onResize));
    if (typeof ResizeObserver !== "undefined") {
      const header = document.querySelector(".menubar");
      if (header) {
        const observer = new ResizeObserver(() => schedulePublish());
        observer.observe(header);
        detach.push(() => observer.disconnect());
      }
    }
    watchResolution();

    if (hasNativeRegions(globalThis)) {
      desktop.value = true;
      // the lane is a render away: publish once it has its boxes
      void nextTick(schedulePublish);
    } else {
      wireDragFallback();
    }
  }

  function start(): void {
    const found = () => {
      if (!isDesktopWindow(globalThis)) return false;
      activate();
      return true;
    };
    if (found()) return;
    window.addEventListener(RUNTIME_CONFIG_READY, found);
    detach.push(() => window.removeEventListener(RUNTIME_CONFIG_READY, found));
    let tries = 0;
    timer = setInterval(() => {
      if (found() || ++tries >= RUNTIME_POLL_TRIES) stopPolling();
    }, RUNTIME_POLL_MS);
  }

  onMounted(start);
  onBeforeUnmount(() => {
    live = false;
    stopPolling();
    if (frame) clearTimeout(frame);
    for (const off of detach.splice(0)) off();
  });

  return { desktop, maximised, act };
}
