// omega webui: the skills tab row's right-click rows.
//
// EXISTING VERBS ONLY: Open is the row's own click (the skill viewer tab), Export is GET
// /skills/export (the folder as a tar.gz download), Enable or Disable is the row's own
// pill (PUT /disabled/skills), Archive is POST /skills/archive (the folder moved into
// .archive, backed up and ledgered - reversible), and Delete opens the tab's existing
// armed confirm (POST /skills/delete through the editor) - one confirm governs both
// doors. A lane without the disabled seam offers no toggle at all: the verb could not
// act, so the row is absent rather than dead.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface SkillMenuDeps {
  open: () => void;
  exportSkill: () => void;
  toggle: () => void;
  archive: () => void;
  remove: () => void;
}

export function skillMenuItems(
  skill: { disabled: boolean; toggleable: boolean },
  deps: SkillMenuDeps,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [{ id: "skill:open", label: "Open", run: deps.open }];
  items.push({ id: "skill:export", label: "Export", run: deps.exportSkill });
  if (skill.toggleable) {
    items.push({
      id: "skill:toggle",
      label: skill.disabled ? "Enable" : "Disable",
      run: deps.toggle,
    });
  }
  items.push({ kind: "separator" });
  items.push({ id: "skill:archive", label: "Archive", run: deps.archive });
  items.push({ id: "skill:delete", label: "Delete", danger: true, run: deps.remove });
  return items;
}
