// omega webui: the content-viewer seam.

import type { InjectionKey, Ref } from "vue";
import type { ViewerRef } from "./dockables.ts";

/**
 * the module->shell open event.
 * The module side dispatches its own CustomEvent against this literal - emitViewerOpen
 * died with the v0.14.1 review lens (zero production callers, and module-import law
 * forbids the shell import anyway).
 */
export const VIEWER_OPEN_EVENT = "omega:open-viewer";

/** the injection key for the app's viewer opener (FilesTab's inject. */
export const DOCK_VIEWER_OPEN_KEY: InjectionKey<(v: ViewerRef) => void> =
  Symbol("omega-dock-viewer-open");

/**
 * the focused-session seam.
 * The Files tab and the viewer tab inject it and send the ID (never a root!) with every
 * /files request - the server resolves the root from the session row, the
 * default_workspace knob, or cwd.
 */
export const FOCUSED_SESSION_KEY: InjectionKey<Readonly<Ref<string | null>>> =
  Symbol("omega-focused-session");

/** the skill content payload (GET /skills/content) */
export interface SkillContentPayload {
  name: string;
  description: string;
  content: string;
  truncated: boolean;
}

/**
 * fetch one skill's full SKILL.md body.
 * The structured error envelope ({error}) throws its message, never a bare status
 * (filesQuery law).
 */
export async function fetchSkillContent(name: string): Promise<SkillContentPayload> {
  const r = await fetch(`/skills/content?name=${encodeURIComponent(name)}`);
  if (!r.ok) {
    let msg = `request: ${r.status}`;
    try {
      const body = (await r.json()) as { error?: unknown };
      if (body && typeof body.error === "string" && body.error !== "") msg = body.error;
    } catch {
      // non-JSON failure body: the status stands
    }
    throw new Error(msg);
  }
  const data = (await r.json()) as Partial<SkillContentPayload>;
  return {
    name: typeof data.name === "string" ? data.name : name,
    description: typeof data.description === "string" ? data.description : "",
    content: typeof data.content === "string" ? data.content : "",
    truncated: !!data.truncated,
  };
}