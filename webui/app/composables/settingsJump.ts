// omega webui: the settings pane's JUMP REQUESTS (card t_81d0901b). The
// palette's /modules, /extensions and /panels commands open the settings
// tab and land on its MODULES section - the surface those commands named
// before the modules cockpit left the secondary pane. A jump can arrive
// while the settings tab is being MINTED (the command opens it in the same
// tick), so the request is a shared SIGNAL the pane consumes on its next
// tick, not a window event a not-yet-mounted listener would miss.
//
// One pending jump at a time: a second request before the pane consumed
// the first simply replaces it (last click wins, the same law the rest of
// the shell's openers follow).

import { ref, type Ref } from "vue";

const pending = ref<string | null>(null);

/** ask the settings pane to jump to `section` (a category id). */
export function jumpToSettingsSection(section: string): void {
  pending.value = section;
}

/** the pane's read side: the pending jump, cleared by the consumer. */
export function useSettingsJump(): Ref<string | null> {
  return pending;
}
