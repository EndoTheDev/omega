// omega webui: the MCP tab's ENTRY EDITOR chain. GET /mcp/servers carries the whole entry
// (env and header VALUES masked), so the form saves it back through POST /mcp/save - the
// route `omega mcp add|edit` already rides - and the route swaps each masked value for the
// one the file holds. Transport is identity, exactly like the CLI: command and url never
// swap in one save (delete and add instead).

import type { McpServerWire, McpStatusWire } from "./serverRows";

export const MCP_MASKED = "****";

export interface McpDraft {
  name: string;
  transport: "stdio" | "http";
  command: string;
  args: string;
  env: string;
  url: string;
  headers: string;
  enabled: boolean;
}

export interface McpSaveBody {
  name: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  headers?: Record<string, string>;
  enabled?: boolean;
}

export function draftOf(s: McpServerWire): McpDraft {
  return {
    name: s.name,
    transport: s.transport === "http" ? "http" : "stdio",
    command: s.command ?? "",
    args: (s.args ?? []).join("\n"),
    env: pairsToText(s.env, "="),
    url: s.url ?? "",
    headers: pairsToText(s.headers, ": "),
    enabled: s.enabled !== false,
  };
}

export function pairsToText(map: Record<string, string> | undefined, sep: string): string {
  if (!map) return "";
  return Object.keys(map)
    .sort()
    .map((k) => `${k}${sep}${map[k]}`)
    .join("\n");
}

/** pairLines reads one KEYsepVALUE per line; the first separator splits, so a value may
    carry the separator itself (a token with `=` in it, a header value with `:`). */
export function pairLines(
  text: string,
  sep: string,
  label: string,
): { pairs: Record<string, string>; error: string } {
  const pairs: Record<string, string> = {};
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (line === "") continue;
    const at = line.indexOf(sep);
    if (at <= 0) return { pairs, error: `${label} "${line}": expected KEY${sep}VALUE` };
    pairs[line.slice(0, at).trim()] = line.slice(at + sep.length).trim();
  }
  return { pairs, error: "" };
}

/** draftToBody mirrors the Go schema law the route enforces before any byte is written,
    so a form mistake reads as a form message instead of a 400 round trip. */
export function draftToBody(d: McpDraft): { body: McpSaveBody | null; error: string } {
  const body: McpSaveBody = { name: d.name };
  // a disabled entry stays disabled: the pill owns the flag, the form never flips it.
  if (!d.enabled) body.enabled = false;
  if (d.transport === "stdio") {
    const command = d.command.trim();
    if (command === "") return { body: null, error: "command is required for a stdio entry" };
    const env = pairLines(d.env, "=", "env");
    if (env.error) return { body: null, error: env.error };
    body.command = command;
    const args = d.args
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter((line) => line !== "");
    if (args.length > 0) body.args = args;
    if (Object.keys(env.pairs).length > 0) body.env = env.pairs;
    return { body, error: "" };
  }
  const url = d.url.trim();
  if (!/^https?:\/\//.test(url)) return { body: null, error: "url must be http(s)" };
  const headers = pairLines(d.headers, ":", "header");
  if (headers.error) return { body: null, error: headers.error };
  body.url = url;
  if (Object.keys(headers.pairs).length > 0) body.headers = headers.pairs;
  return { body, error: "" };
}

/** One round trip: the route answers the fresh listing after the bridge swap. A refused
    save throws; the caller lands it on the editor's own error line so the draft survives. */
export async function saveMcpServer(body: McpSaveBody): Promise<McpStatusWire> {
  const res = await fetch("/mcp/save", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`POST /mcp/save: ${res.status}`);
  return (await res.json()) as McpStatusWire;
}
