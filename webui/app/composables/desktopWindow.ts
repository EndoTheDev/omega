// omega webui: the desktop window's own options - the four rows the settings pane shows
// under "desktop". Three are booleans in the app's home config.yaml (desktop.start_minimized,
// desktop.minimize_to_tray, desktop.close_to_tray); the fourth, start with system, is the OS
// autostart registration, which only the app can read. Inside the embedded window the page
// therefore asks the host over the raw message channel - never a server knob. A browser tab
// edits the three toggles of the server it is served by through /settings, and has no
// autostart row at all.

/** the host -> page event every window-options request is answered on */
export const WINDOW_OPTIONS_EVENT = "omega:window-options";

/** the host's patience with one window-options round trip */
export const WINDOW_OPTIONS_TIMEOUT_MS = 5000;

export type DesktopWindowKey =
  | "start_with_system"
  | "start_minimized"
  | "minimize_to_tray"
  | "close_to_tray";

/** the row that is the OS registration, never a config key */
export const DESKTOP_AUTOSTART_ROW: DesktopWindowKey = "start_with_system";

/** the three rows that live in the app's config.yaml, in pane order */
export const DESKTOP_CONFIG_ROWS = [
  "start_minimized",
  "minimize_to_tray",
  "close_to_tray",
] as const;

/** the embedded window's section: the registration first, then the app's three */
export const DESKTOP_WINDOW_ROWS: DesktopWindowKey[] = [
  DESKTOP_AUTOSTART_ROW,
  ...DESKTOP_CONFIG_ROWS,
];

/** the autostart row's words: it has no settings knob, so the schema cannot supply them */
export const DESKTOP_AUTOSTART_LABEL = "start with system";
export const DESKTOP_AUTOSTART_HELP =
  "launch the desktop at login; the OS registration is the truth (regedit or an installer can change it), and the tray's Start with Windows row flips the same entry";

export interface DesktopWindowState {
  start_with_system: boolean;
  start_minimized: boolean;
  minimize_to_tray: boolean;
  close_to_tray: boolean;
  error: string;
}

// desktopWindowState reads one answer. A row the answer never named is off: a refusal must
// never render a row as on because a field was missing.
export function desktopWindowState(data: unknown): DesktopWindowState {
  const doc = (data ?? {}) as Record<string, unknown>;
  return {
    start_with_system: doc.start_with_system === true,
    start_minimized: doc.start_minimized === true,
    minimize_to_tray: doc.minimize_to_tray === true,
    close_to_tray: doc.close_to_tray === true,
    error: typeof doc.error === "string" ? doc.error : "",
  };
}

/** one row's checked state, from the latest answer */
export function desktopWindowChecked(state: DesktopWindowState, key: DesktopWindowKey): boolean {
  return state[key] === true;
}

// --- the host channel -------------------------------------------------------------
//
// The same shape the instance list rides: the Wails shim's one event entry point is wrapped
// per shim (it is re-injected on every navigation), requests queue oldest-first so a write's
// answer can never land on the read before it, and a wait that already timed out has no
// claim on a later answer.

interface WindowHost {
  _wails?: {
    invoke?: (message: string) => void;
    dispatchWailsEvent?: (event: { name?: unknown; data?: unknown }) => void;
  };
}

function hostApi(): NonNullable<WindowHost["_wails"]> | null {
  const api = (globalThis as WindowHost)._wails;
  return api && typeof api.invoke === "function" ? api : null;
}

let armedOn: object | null = null;
const waiters: { at: number; settle: (state: DesktopWindowState) => void }[] = [];

function armHostChannel(): boolean {
  const api = hostApi();
  if (!api) return false;
  if (armedOn === api) return true;
  armedOn = api;
  const previous = api.dispatchWailsEvent;
  api.dispatchWailsEvent = (event) => {
    if (typeof previous === "function") previous.call(api, event);
    if (event?.name !== WINDOW_OPTIONS_EVENT) return;
    nextWaiter()?.(desktopWindowState(event.data));
  };
  return true;
}

function nextWaiter(): ((state: DesktopWindowState) => void) | undefined {
  const now = Date.now();
  while (waiters.length > 0) {
    const head = waiters.shift();
    if (!head) return undefined;
    if (now - head.at <= WINDOW_OPTIONS_TIMEOUT_MS) return head.settle;
  }
  return undefined;
}

function ask(message: object): Promise<DesktopWindowState> {
  const api = hostApi();
  if (!api || !armHostChannel()) {
    return Promise.resolve(desktopWindowState({ error: "this page has no app behind it" }));
  }
  const pending = new Promise<DesktopWindowState>((resolve) => {
    let settled = false;
    const settle = (state: DesktopWindowState) => {
      if (settled) return;
      settled = true;
      resolve(state);
    };
    const entry = { at: Date.now(), settle };
    waiters.push(entry);
    setTimeout(() => {
      const at = waiters.indexOf(entry);
      if (at >= 0) waiters.splice(at, 1);
      settle(
        desktopWindowState({
          error: `the app did not answer within ${WINDOW_OPTIONS_TIMEOUT_MS / 1000}s`,
        }),
      );
    }, WINDOW_OPTIONS_TIMEOUT_MS);
  });
  api.invoke?.(JSON.stringify(message));
  return pending;
}

/** read the app's four window rows */
export function requestDesktopWindowOptions(): Promise<DesktopWindowState> {
  return ask({ omega: "window:get" });
}

/** flip one row; the answer is the re-read state, or the host's refusal */
export function setDesktopWindowOption(
  key: DesktopWindowKey,
  on: boolean,
): Promise<DesktopWindowState> {
  return ask({ omega: "window:set", key, value: on });
}
