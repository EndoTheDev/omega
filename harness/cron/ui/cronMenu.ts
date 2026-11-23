// omega webui: the cron tab row's right-click rows.
//
// EXISTING VERBS ONLY: Run now is POST /cron/run (one fire the schedule did not order),
// Stop run is POST /cron/stop (cancels the fire in flight - offered only while the row
// carries the live mark), Pause/Resume is POST /cron/toggle (the row's own pill) and
// Delete is POST /cron/delete, opening the tab's existing armed confirm. A job an agent
// home owns is not in main's crons.yaml, so the delete route cannot touch it and the row
// offers none.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface CronMenuDeps {
  runNow: () => void;
  stopRun: () => void;
  toggle: () => void;
  remove: () => void;
}

export function cronMenuItems(
  job: { name: string; enabled: boolean; agent_owned?: boolean; running?: boolean },
  deps: CronMenuDeps,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [];
  if (job.running) {
    items.push({ id: "cron:stop-run", label: "Stop run", danger: true, run: deps.stopRun });
  } else {
    items.push({ id: "cron:run-now", label: "Run now", run: deps.runNow });
  }
  items.push({ id: "cron:toggle", label: job.enabled ? "Pause" : "Resume", run: deps.toggle });
  if (!job.agent_owned) {
    items.push({ kind: "separator" });
    items.push({ id: "cron:delete", label: "Delete", danger: true, run: deps.remove });
  }
  return items;
}
