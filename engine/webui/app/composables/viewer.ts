// omega webui: the content-viewer seam (card t_36ceb99b). A viewer tab
// shows a FILE (GET /files/read) or a SKILL'S BODY (GET /skills/content).
// Openers: shell components (FilesTab) go through DOCK_VIEWER_OPEN_KEY -
// the same provide/inject shape the chat opener rides; MODULE-owned UI
// (skills tab) imports no shell code, so it hands the open over through
// this window CustomEvent (the kanban-board event seam, Board precedent:
// a deliberate literal copied on both sides, paired by the viewer.test
// pin) - app.vue listens and opens.

import type { InjectionKey } from "vue";
import { validViewerRef, type ViewerRef } from "./dockables.ts";

/** the module->shell open event. THE LITERAL IS COPIED in
    modules/skills/ui/SkillsList.vue (module UI import law); the viewer
    test pins both spellings identical. Detail payload: ViewerRef. */
export const VIEWER_OPEN_EVENT = "omega:open-viewer";

/** module-UI opener: dispatch the open event (never imports shell code) */
export function emitViewerOpen(v: ViewerRef): void {
  if (!validViewerRef(v)) return;
  window.dispatchEvent(new CustomEvent(VIEWER_OPEN_EVENT, { detail: v }));
}

/** the injection key for the app's viewer opener (FilesTab's inject; the
    app always provides, the fallback stays null like the chat opener) */
export const DOCK_VIEWER_OPEN_KEY: InjectionKey<(v: ViewerRef) => void> =
  Symbol("omega-dock-viewer-open");

/** the skill content payload (GET /skills/content) */
export interface SkillContentPayload {
  name: string;
  description: string;
  content: string;
  truncated: boolean;
}

/** fetch one skill's full SKILL.md body. The structured error envelope
    ({error}) throws its message, never a bare status (filesQuery law). */
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