// omega webui: the live chip store (card t_896cf5a8, step 5) - the
// pending chip's streaming half.
//
// A yielded / dispatched tool call renders as the pending chip (async
// agent loop phase 3). While the call runs, the chips topic pushes its
// output as it lands (fill false, text appendable, truncated marking the
// producer's cap); when the call settles a fill frame arrives (fill
// true, content the result). The topic is LOSSY by design - a skipped
// frame costs a tail byte, never the truth: the fill and the transcript
// are authoritative and the canvas panel repaints the row anyway.
//
// The store is keyed by TOOL CALL id (the same id the chat panel's rows
// carry), so the chat pane merges streamed text onto the row that owns
// the call. Page-lifetime singleton: the canvas chat panel is one
// conversation for the whole page, exactly like the store.

import { reactive } from "vue";
import type { ChipFrame } from "./socket";

export interface LiveChip {
  /** the accumulated live tail (fill false appends, fill true settles) */
  text: string;
  /** the producer's cap dropped bytes somewhere in this tail */
  truncated: boolean;
  /** the fill arrived: the call settled, its result is truth now */
  settled: boolean;
  /** the fill carried an error */
  isError: boolean;
}

// MAX_LIVE_CHIPS bounds the map: a page that runs hundreds of shell
// calls keeps only the newest tails. Old rows have their result on the
// panel already, so evicting a tail costs nothing visible.
export const MAX_LIVE_CHIPS = 32;

// liveChips is the page's one live-tail map (call id -> chip). Reactive
// so the chat pane's merge recomputes as chunks land.
export const liveChips = reactive(new Map<string, LiveChip>());

// ingestChipFrame folds one frame into the store. fill false appends
// text (the server sends the newly written bytes); fill true settles the
// chip and takes the content as its tail when nothing streamed (a call
// with no live output still shows its result through the fill). Pure
// state work on the map - no timers, no fetches.
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

// resetLiveChips drops every tail - the page is going away, or a test
// needs a clean store.
export function resetLiveChips(): void {
  liveChips.clear();
}

// TRUNCATION_MARK is the streaming path's own honesty (the truncation
// marker law): the producer kept a capped head, so the tail the chip
// holds is not the whole output. The settled result carries the same
// note from the tool itself.
export const TRUNCATION_MARK =
  "\n... [output truncated while streaming: the chip keeps the head bytes - the settled result carries the full text]";

// chipDisplay renders one tail for the row: the text plus the marker when
// the producer capped it. Empty text renders nothing (a chip with no
// output shows no output section). Pure so the node test pins the marker
// placement - it must ride the STREAMING path, not only the fill.
export function chipDisplay(chip: LiveChip | undefined): string {
  if (!chip || !chip.text) return "";
  return chip.truncated ? `${chip.text}${TRUNCATION_MARK}` : chip.text;
}
