// omega webui: the tools tab row's right-click rows.
//
// EXISTING VERBS ONLY: Enable or Disable is the row's own pill (PUT /disabled/tools; a
// group parent fans out over its exact tool names through the tab's own toggle chain).
// That is the only verb the tools tab carries, so a lane without the disabled seam
// registers nothing and the browser's own menu stays.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface ToolMenuDeps {
  toggle: () => void;
}

export function toolMenuItems(
  tool: { disabled: boolean; toggleable: boolean },
  deps: ToolMenuDeps,
): ContextMenuEntry[] {
  if (!tool.toggleable) return [];
  return [
    {
      id: "tool:toggle",
      label: tool.disabled ? "Enable" : "Disable",
      run: deps.toggle,
    },
  ];
}
