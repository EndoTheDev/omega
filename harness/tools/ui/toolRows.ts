// omega webui: pure row model for the TOOLS tab's one-domain grouping.

export interface ToolActionWire {
  name: string;
  description: string;
}

export interface ToolWire {
  name: string;
  description: string;
  actions?: ToolActionWire[];
  origin?: string;
  // the fold's verdict on this tool. Documentation on the wire - the row model reads
  // the fold LIST (/disabled) as the single source for row.disabled, never this stamp.
  disabled?: boolean;
}

// Structural RegistryRow.
export interface ToolRow {
  name: string;
  description: string;
  detail: string[];
  disabled: boolean;
}

// nativeTools drops MCP-origin entries from the wire inventory. The filter reads the
// origin FIELD the server stamps (core.Tool.Origin), never the name shape.
export function nativeTools(tools: readonly ToolWire[]): ToolWire[] {
  return tools.filter((t) => t.origin !== "mcp");
}

// domainOf splits the wire name.
export function domainOf(name: string): string {
  const dot = name.indexOf(".");
  return dot < 0 ? "" : name.slice(0, dot);
}

// toolRows builds the grouped row model. The detail lines are the dotted identities
// (domain.action for actions, full tool name for merged sibling tools), each with its
// own one-liner - the same contract the prompt renderer ships. disabled = the fold
// names the row's own tool (parent rows that ARE tools) or every merged member (group
// parents) - the toggle law below in ToolsList.: a dotless domain tool that shares its
// name with a dotted prefix ABSORBS the dotted siblings - detail gains their full
// identities, they never group on their own, and disabled goes group-style (every
// member off) because the toggle fans out to all of them.: the caller feeds the
// FOLD-AWARE inventory (the union before the disabled fold filters it) - a
// fold-disabled tool stays in `tools` and lands in its row like any other member, never
// a row of its own.
export function toolRows(
  tools: readonly ToolWire[],
  fold: readonly string[],
): ToolRow[] {
  const members = new Map<string, string[]>();
  // dotless tool names: a domain that names ITSELF absorbs any dotted siblings riding.
  const domains = new Set<string>();
  for (const t of tools) {
    const d = domainOf(t.name);
    if (!d) {
      domains.add(t.name);
      continue;
    }
    const list = members.get(d) ?? [];
    list.push(t.name);
    members.set(d, list);
  }

  const off = new Set(fold);
  const consumed = new Set<string>();
  const rows: ToolRow[] = [];
  for (const t of tools) {
    if (consumed.has(t.name)) continue;
    const d = domainOf(t.name);
    if (d && domains.has(d)) continue;
    const group = d && (members.get(d)?.length ?? 0) >= 2;
    if (group) {
      // the group parent row: the prefix is the row, each member's full identity is.
      const siblings = tools.filter((s) => domainOf(s.name) === d);
      for (const s of siblings) consumed.add(s.name);
      rows.push({
        name: d,
        description: t.description,
        detail: siblings.map((s) => `${s.name} - ${s.description}`),
        disabled: siblings.every((s) => off.has(s.name)),
      });
      continue;
    }
    const merged = tools.filter((s) => domainOf(s.name) === t.name);
    for (const s of merged) consumed.add(s.name);
    rows.push({
      name: t.name,
      description: t.description,
      detail: [
        ...(t.actions ?? []).map(
          (a) => `${t.name}.${a.name} - ${a.description}`,
        ),
        ...merged.map((s) => `${s.name} - ${s.description}`),
      ],
      disabled:
        merged.length > 0
          ? off.has(t.name) && merged.every((s) => off.has(s.name))
          : off.has(t.name),
    });
  }
  return rows;
}

// toggleTargets fans the parent row's toggle out to the actual tool names the fold.
export function toggleTargets(rowName: string, tools: readonly ToolWire[]): string[] {
  if (tools.some((t) => t.name === rowName)) {
    return [
      rowName,
      ...tools
        .filter((t) => t.name !== rowName && domainOf(t.name) === rowName)
        .map((t) => t.name),
    ];
  }
  return tools.filter((t) => domainOf(t.name) === rowName).map((t) => t.name);
}