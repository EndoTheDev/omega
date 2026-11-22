// omega webui: the live chip store - the pending chip's streaming half. The topic is
// LOSSY by design - a skipped frame costs a tail byte, never the truth: the fill and
// the transcript are authoritative and the canvas panel repaints the row anyway.
// Page-lifetime singleton: the canvas chat panel is one conversation for the whole
// page, exactly like the store.

import { reactive } from "vue";
import type { ChipFrame } from "./socket";

export interface LiveChip {
  text: string;
  truncated: boolean;
  settled: boolean;
  isError: boolean;
}

export const MAX_LIVE_CHIPS = 32;

// liveChips is the page's one live-tail map (call id -> chip).
export const liveChips = reactive(new Map<string, LiveChip>());

export function ingestChipFrame(frame: ChipFrame | null | undefined): void {
  if (!frame || !frame.call_id) return;
  let chip = liveChips.get(frame.call_id);
  if (!chip) {
    if (liveChips.size >= MAX_LIVE_CHIPS) {
      const oldest = liveChips.keys().next();
      if (!oldest.done) liveChips.delete(oldest.value);
    }
    chip = { text: "", truncated: false, settled: false, isError: false };
    liveChips.set(frame.call_id, chip);
  }
  if (frame.fill === true) {
    chip.settled = true;
    if (frame.is_error === true) chip.isError = true;
    if (!chip.text && frame.content) chip.text = frame.content;
    if (frame.truncated === true) chip.truncated = true;
    return;
  }
  if (frame.text) chip.text += frame.text;
  if (frame.truncated === true) chip.truncated = true;
}

// chipOf reads one call's live state (undefined when nothing streamed).
export function chipOf(callId: string | undefined | null): LiveChip | undefined {
  if (!callId) return undefined;
  return liveChips.get(callId);
}

// resetLiveChips drops every tail.
export function resetLiveChips(): void {
  liveChips.clear();
}

// TRUNCATION_MARK is the streaming path's own honesty (the truncation marker law). The
// settled result is a capped head too (shellOutputCap, then the loop's output cap), so
// the note must not promise the full text - only `shell.read id=<id>` tails the log.
export const TRUNCATION_MARK =
  "\n... [output truncated while streaming: the chip keeps the head bytes - the settled result is a capped head too; shell.read id=<id> tails the full log]";

// chipDisplay renders one tail for the row. Pure so the node test pins the marker
// placement - it must ride the STREAMING path, not only the fill.
export function chipDisplay(chip: LiveChip | undefined): string {
  if (!chip || !chip.text) return "";
  return chip.truncated ? `${chip.text}${TRUNCATION_MARK}` : chip.text;
}
