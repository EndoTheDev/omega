// omega webui: the content-viewer seam.

import type { InjectionKey, Ref } from "vue";
import type { ViewerRef } from "./dockables.ts";

// the module->shell open event: module code dispatches its own CustomEvent, since
// module-import law forbids importing the shell.
export const VIEWER_OPEN_EVENT = "omega:open-viewer";

export const DOCK_VIEWER_OPEN_KEY: InjectionKey<(v: ViewerRef) => void> =
  Symbol("omega-dock-viewer-open");

/** Files + viewer inject this and send the session ID (never a root!) with /files requests. */
export const FOCUSED_SESSION_KEY: InjectionKey<Readonly<Ref<string | null>>> =
  Symbol("omega-focused-session");

export interface SkillContentPayload {
  name: string;
  description: string;
  content: string;
  truncated: boolean;
}

/** fetch one skill's SKILL.md; a {error} envelope throws its message, never a bare status */
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