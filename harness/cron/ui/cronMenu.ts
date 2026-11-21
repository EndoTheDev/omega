// omega webui: the cron tab row's right-click rows.
//
// EXISTING VERBS ONLY: Pause/Resume is POST /cron/toggle (the row's own pill), Delete is
// POST /cron/delete and opens the tab's existing armed confirm. A job an agent home owns is
// not in main's crons.yaml, so the delete route cannot touch it and the row offers none.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface CronMenuDeps {
  toggle: () => void;
  remove: () => void;
}

export function cronMenuItems(
  job: { name: string; enabled: boolean; agent_owned?: boolean },
  deps: CronMenuDeps,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [
    { id: "cron:toggle", label: job.enabled ? "Pause" : "Resume", run: deps.toggle },
  ];
  if (!job.agent_owned) {
    items.push({ kind: "separator" });
    items.push({ id: "cron:delete", label: "Delete", danger: true, run: deps.remove });
  }
  return items;
}
