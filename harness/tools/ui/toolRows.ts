// omega webui: pure row model for the TOOLS tab's one-domain grouping
// (card t_e06b38bc). The wire is the one-domain tool law: a
// multi-action domain names ITSELF (files, kanban, memory, skills,
// verdict), so one tool = one parent row with its dotted actions
// nested beneath - the files pattern, the card's Part 1. Separate-verb
// tools that share a dotted prefix (delegate.task + delegate.status,
// web.search + web.fetch) merge under their prefix as the parent row -
// same visual, the prefix IS their domain. A lone dotted tool
// (sessions.search, shell.run) stays its own top-level row: grouping a
// one-member domain would only add a layer. Card t_7871a56a closes the
// collision those two laws could produce: a dotless tool whose name
// equals a dotted prefix (files + files.grep + files.glob - the tools
// capsule ships the dotted pair in the files namespace) absorbs its
// dotted siblings into the DOMAIN row instead of letting both laws
// fire - one row per domain, always. Pure on purpose: no Vue,
// no fetch - the node suite pins the grouping.

export interface ToolActionWire {
  name: string;
  description: string;
}

export interface ToolWire {
  name: string;
  description: string;
  actions?: ToolActionWire[];
  // where the tool registered from (card t_c2da3379): "mcp" = an MCP
  // bridge tool, absent = native. The ONLY key the tools tab's filter
  // reads - native tools are dotted too (sessions.search), so no name
  // heuristics. Native dotted tools stay; MCP-origin tools are
  // excluded and surface in the MCP tab alone.
  origin?: string;
}

// Structural RegistryRow (module UI imports no shell code - the narrow
// law; the shape matches the inference ToolsList consumes).
export interface ToolRow {
  name: string;
  description: string;
  detail: string[];
  disabled: boolean;
}

// nativeTools drops MCP-origin entries from the wire inventory. The
// filter reads the origin FIELD the server stamps (core.Tool.Origin),
// never the name shape. Input is untouched; the wire is the agent's
// full executable registry and stays truthful - the Tools TAB shows
// the native slice, the MCP tab owns MCP display (one surface per
// concern, card t_c2da3379).
export function nativeTools(tools: readonly ToolWire[]): ToolWire[] {
  return tools.filter((t) => t.origin !== "mcp");
}

// domainOf splits the wire name: the prefix before the first dot, "" for
// a dotless name. shell.run -> shell; files -> "".
export function domainOf(name: string): string {
  const dot = name.indexOf(".");
  return dot < 0 ? "" : name.slice(0, dot);
}

// toolRows builds the grouped row model. Order law: every tool keeps
// its /tools inventory position - a group row sits at its FIRST
// member's position and consumes the siblings that follow it. The
// detail lines are the dotted identities (domain.action for actions,
// full tool name for merged sibling tools), each with its own
// one-liner - the same contract the t_73994e55 prompt renderer ships.
// disabled = the fold names the row's own tool (parent rows that ARE
// tools) or every merged member (group parents) - the toggle law
// below in ToolsList. Card t_7871a56a: a dotless domain tool that
// shares its name with a dotted prefix ABSORBS the dotted siblings -
// detail gains their full identities, they never group on their own,
// and disabled goes group-style (every member off) because the toggle
// fans out to all of them.
export function toolRows(
  tools: readonly ToolWire[],
  fold: readonly string[],
): ToolRow[] {
  // member count per dotted domain: 2+ = groupable
  const members = new Map<string, string[]>();
  // dotless tool names: a domain that names ITSELF absorbs any dotted
  // siblings riding the same prefix (the files collision, t_7871a56a)
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
    // a dotted tool whose prefix is a dotless domain tool is absorbed
    // by that domain's row wherever it sits in the inventory - skip it
    // here, the domain row consumes it (t_7871a56a)
    if (d && domains.has(d)) continue;
    const group = d && (members.get(d)?.length ?? 0) >= 2;
    if (group) {
      // the group parent row: the prefix is the row, each member's
      // full identity is a nested detail line
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
    // a tool that is its own domain (dotless): the actions are the
    // dotted sub-rows, and dotted siblings from the same namespace
    // (files.grep/files.glob under files, t_7871a56a) join them as
    // merged members - one row per domain
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

// toggleTargets fans the parent row's toggle out to the actual tool
// names the fold stores: an exact tool name toggles itself; a group
// parent's name (the dotless prefix) is NOT a tool - the PUT goes to
// each member (the config fold keeps exact names, the built-in-wins
// law untouched). Card t_7871a56a: a MERGED domain row (a dotless
// tool that absorbed its dotted siblings, the files shape) is both at
// once - its exact name is a real tool AND its siblings ride the
// same prefix, so the toggle fans out to the domain tool and every
// absorbed member. The webui-only fan-out is the deliberate copy: the
// server stays name-exact.
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