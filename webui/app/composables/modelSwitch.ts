// The model-switch guard: a pick whose context window is smaller than the live context
// truncates the conversation, so that pick is a BLOCKING decision. Pure rules only -
// ChatTab wires the dialog and the requests; the node suite pins every branch here.
import { computed, ref } from "vue";

/** one server probe: GET /sessions/{id}/context?model=<slot> */
export interface ContextProbe {
  /** live context size, estimated from the session's active-path wire */
  tokens: number;
  /** the picked slot's context window */
  context_window: number;
}

export type ModelSwitchPlan =
  /** the switch fits - no dialog, apply the pick */
  | { kind: "apply" }
  /** the switch would drop context - ask, with the numbers to show */
  | { kind: "prompt"; tokens: number; window: number; loss: number };

/**
 * A missing probe (no session, failed call) applies silently: nothing measured, nothing
 * lost. A window of 0 is the server's "provider cannot report one" answer, floored there.
 */
export function planModelSwitch(probe: ContextProbe | null): ModelSwitchPlan {
  if (
    !probe ||
    !(probe.context_window > 0) ||
    !(probe.tokens > probe.context_window)
  ) {
    return { kind: "apply" };
  }
  return {
    kind: "prompt",
    tokens: probe.tokens,
    window: probe.context_window,
    loss: probe.tokens - probe.context_window,
  };
}

export type ModelSwitchChoice = "compact" | "anyway" | "cancel";

export interface ModelSwitchIO {
  /** run the existing compact path; resolves the compact's dispatch answer */
  compact(): Promise<string>;
  /** perform the switch (the tab's update:model emit) */
  apply(): void;
  /** land the compact's answer in the pane's status line */
  report?(line: string): void;
  /**
   * re-read the live context after the compact. One that folded nothing frees no room,
   * and without this read "compact and switch" would truncate exactly as silently as
   * the bare pick the dialog exists to stop.
   */
  probe?(): Promise<ContextProbe | null>;
}

export interface ModelSwitchResult {
  applied: boolean;
  report?: string;
  error?: string;
  /** the compact landed but the context still overflows: the switch is HELD */
  stillOver?: { tokens: number; window: number; loss: number };
}

/**
 * One of the three outcomes. A failed compact never applies the switch, and a compact
 * that freed too little holds it with the remaining overflow - "switch anyway" stays the
 * user's explicit door. The compact's answer lands before the switch takes effect.
 */
export async function runModelSwitchChoice(
  choice: ModelSwitchChoice,
  io: ModelSwitchIO,
): Promise<ModelSwitchResult> {
  if (choice === "cancel") return { applied: false };
  if (choice === "anyway") {
    io.apply();
    return { applied: true };
  }
  let report = "";
  try {
    report = await io.compact();
  } catch (e) {
    return { applied: false, error: e instanceof Error ? e.message : String(e) };
  }
  io.report?.(report);
  if (io.probe) {
    const after = await io.probe();
    const plan = planModelSwitch(after);
    if (plan.kind === "prompt") {
      return {
        applied: false,
        report,
        stillOver: { tokens: plan.tokens, window: plan.window, loss: plan.loss },
      };
    }
  }
  io.apply();
  return { applied: true, report };
}

/** open model-switch dialogs, one per chat pane; the shell's type-to-capture gate reads >0 */
const openDialogs = ref(0);
export const modelSwitchDialogOpen = computed(() => openDialogs.value > 0);

export function openModelSwitchDialog(): void {
  openDialogs.value += 1;
}

export function closeModelSwitchDialog(): void {
  openDialogs.value = Math.max(0, openDialogs.value - 1);
}
