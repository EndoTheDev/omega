// The model-switch guard. Picking a model whose context window is smaller than the live
// context truncates the conversation, so that pick is a BLOCKING decision: compact and
// switch / switch anyway / cancel. Pure rules only - ChatTab wires the dialog and the
// requests, the node suite pins every branch here without a DOM.
import { ref } from "vue";

/** one server probe: GET /sessions/{id}/context?model=<slot> */
export interface ContextProbe {
  /** the live context size, estimated from the session's active-path wire */
  tokens: number;
  /** the picked model's context window */
  context_window: number;
}

export type ModelSwitchPlan =
  /** the switch fits - no dialog, apply the pick */
  | { kind: "apply" }
  /** the switch would drop context - ask, with the numbers to show */
  | { kind: "prompt"; tokens: number; window: number; loss: number };

/**
 * planModelSwitch decides whether a pick needs the dialog. A missing probe (no session,
 * or the call failed) applies silently: nothing is measured, nothing is lost. A window of
 * 0 is the server's "provider cannot report one" answer and never triggers a prompt on
 * its own - the server floors it before it reaches here.
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
  /** run the existing compact path; resolves the compact receipt line */
  compact(): Promise<string>;
  /** perform the switch (the tab's update:model emit) */
  apply(): void;
  /** land the receipt where the user reads it */
  receipt?(line: string): void;
}

export interface ModelSwitchResult {
  applied: boolean;
  receipt?: string;
  error?: string;
}

/**
 * runModelSwitchChoice performs one of the three dialog outcomes. A failed compact NEVER
 * applies the switch: the context did not shrink, so switching would be exactly the
 * silent truncation the dialog exists to prevent - the error goes back to the dialog
 * instead. "compact" resolves only after the receipt exists, so the receipt always lands
 * before the switch takes effect.
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
  let receipt = "";
  try {
    receipt = await io.compact();
  } catch (e) {
    return { applied: false, error: e instanceof Error ? e.message : String(e) };
  }
  io.receipt?.(receipt);
  io.apply();
  return { applied: true, receipt };
}

/**
 * true while a model-switch dialog is up. The shell's type-to-capture gate reads it: a
 * printable key belongs to the open dialog, never to the composer behind it.
 */
export const modelSwitchDialogOpen = ref(false);
