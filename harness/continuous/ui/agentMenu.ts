// omega webui: the agents roster row's right-click rows.
//
// EXISTING VERBS ONLY: New agent opens the panel's own create form (POST /agents), Sleep
// is the panel's own wake control (POST /continuous/{name}/sleep - it ends a running wake,
// so it is offered only on an awake row), and Delete opens the row's existing confirm
// (DELETE /agents/{name} - the home, sessions included). The default agent is
// locked-on, so it carries no Delete: its home is the daemon's root.

import type { ContextMenuEntry } from "../../../webui/app/composables/contextMenu.ts";

export interface AgentMenuDeps {
  newAgent: () => void;
  stop: () => void;
  remove: () => void;
}

export function agentMenuItems(
  agent: { name: string; deletable: boolean; awake?: boolean },
  deps: AgentMenuDeps,
): ContextMenuEntry[] {
  const items: ContextMenuEntry[] = [
    { id: "agent:new", label: "New agent", run: deps.newAgent },
  ];
  if (agent.awake) {
    items.push({ kind: "separator" });
    items.push({ id: "agent:stop", label: "Sleep", danger: true, run: deps.stop });
  }
  if (agent.deletable) {
    items.push({ kind: "separator" });
    items.push({ id: "agent:delete", label: "Delete", danger: true, run: deps.remove });
  }
  return items;
}
