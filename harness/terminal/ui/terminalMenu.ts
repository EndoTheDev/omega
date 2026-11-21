// omega webui: the terminal's right-click rows.
//
// The terminal is chrome by the design line, so right-click is its own menu: Copy, Paste,
// Clear. Copy is DISABLED on an empty selection (a row that copies nothing reads broken);
// the menu always has a fireable row, so it opens in every state.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface TerminalMenuDeps {
  /** xterm's own selection state: Copy needs a selection to carry */
  hasSelection: boolean;
  copy: () => void | Promise<void>;
  paste: () => void | Promise<void>;
  clear: () => void;
}

export function terminalMenuItems(deps: TerminalMenuDeps): ContextMenuEntry[] {
  return [
    {
      id: "terminal:copy",
      label: "Copy",
      disabled: !deps.hasSelection,
      run: deps.copy,
    },
    { id: "terminal:paste", label: "Paste", run: deps.paste },
    { kind: "separator" },
    { id: "terminal:clear", label: "Clear", run: deps.clear },
  ];
}
