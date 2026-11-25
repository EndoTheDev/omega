// omega webui: the update channel's wire shapes and the pane's client.
//
// ONE engine serves the settings pane, the status chip and the desktop tray. The pane
// never downloads or installs by itself: every action posts to /updates and renders the
// status the server answers, so the same contract drives every surface.

export interface UpdateReceipt {
  version: string;
  outcome: "pass" | "rollback";
  why?: string;
  restored?: string[];
  at: number;
}

export interface UpdateStatus {
  state: string;
  current: string;
  available?: string;
  channel: string;
  managed: boolean;
  auto_check: boolean;
  auto_update: boolean;
  behind?: number;
  progress?: number;
  staged_version?: string;
  error?: string;
  receipt?: UpdateReceipt;
  checked_at?: number;
  active_runs: number;
}

/** the states during which the server is mid-flight: the pane polls, the chip waits. */
export const UPDATE_IN_FLIGHT: readonly string[] = ["checking", "staging", "preflight", "finalizing"];

export function updateInFlight(st: UpdateStatus | null): boolean {
  return st !== null && UPDATE_IN_FLIGHT.includes(st.state);
}

/** a staged, pre-flight-green release is the only finalize-legal state. */
export function updateCanFinalize(st: UpdateStatus | null): boolean {
  return st !== null && st.state === "ready" && (st.staged_version ?? "") !== "";
}

/** the pane's one-line state, with the facts that make it true. */
export function updateHeadline(st: UpdateStatus | null): string {
  if (!st) return "reading the update channel...";
  switch (st.state) {
    case "idle":
      return st.managed
        ? "no check yet - the channel is read every six hours"
        : `this build is not release-managed (${st.current}) - it never self-updates`;
    case "checking":
      return "checking the release channel...";
    case "up-to-date":
      return `up to date on the ${st.channel} channel (${st.current})`;
    case "available":
      return `${st.available ?? "a release"} is available on the ${st.channel} channel` +
        (st.behind && st.behind > 1 ? ` - ${st.behind} releases behind` : "");
    case "staging":
      return `downloading ${st.available ?? "the release"}... ${st.progress ?? 0}%`;
    case "preflight":
      return "pre-flight: booting the staged binary against a copy of the store...";
    case "ready":
      return `${st.staged_version ?? "the release"} is staged and pre-flight green - restart to install`;
    case "finalizing":
      return "restarting into the new version...";
    case "failed":
      return st.error || "the update channel failed";
    default:
      return st.state;
  }
}

/** a rollback receipt in plain words: version, why, what was restored. */
export function updateReceiptLine(r: UpdateReceipt | undefined): string {
  if (!r) return "";
  if (r.outcome === "pass") return `omega ${r.version} installed and healthy.`;
  const restored = r.restored && r.restored.length > 0 ? r.restored.join(", ") : "nothing";
  return `omega ${r.version} rolled back: ${r.why ?? "the health window failed"}. Restored: ${restored}.`;
}

/** the quiet gate's sentence: a live run makes the restart a prompt. */
export function updateRestartPrompt(st: UpdateStatus | null): string {
  const runs = st?.active_runs ?? 0;
  if (runs === 0) return "Restart omega now?";
  return `${runs} run${runs === 1 ? "" : "s"} active - restarting ends ${runs === 1 ? "it" : "them"}. Restart anyway?`;
}

async function readStatus(r: Response): Promise<UpdateStatus> {
  const body = (await r.json()) as UpdateStatus | { error?: string; state?: string };
  if (!r.ok) {
    const err = (body as { error?: string }).error;
    const st = body as UpdateStatus;
    if (err && st && typeof st.state === "string") {
      // the server answered a full status with a failure inside: render it, do not throw
      return { ...st, error: err };
    }
    throw new Error(err || `updates: ${r.status}`);
  }
  return body as UpdateStatus;
}

export async function fetchUpdateStatus(): Promise<UpdateStatus> {
  return readStatus(await fetch("/updates/status"));
}

export async function postUpdate(path: string, body: Record<string, unknown> = {}): Promise<UpdateStatus> {
  return readStatus(
    await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }),
  );
}

export async function checkForUpdates(): Promise<UpdateStatus> {
  return postUpdate("/updates/check");
}

export async function stageUpdate(): Promise<UpdateStatus> {
  return postUpdate("/updates/stage");
}

export async function finalizeUpdate(force: boolean): Promise<UpdateStatus> {
  return postUpdate("/updates/finalize", { force });
}

export async function discardStagedUpdate(): Promise<UpdateStatus> {
  return postUpdate("/updates/discard");
}
