// omega webui: the skills tab's fetch chain (card t_ec57d5b8). Lifted
// from the SFC so the refresh button's behavior rides a testable
// helper (the mcp reload.ts precedent, card t_d6e94e79): the node
// suite probes it with a fetch log - the SFC (SkillsList.vue) only
// wires the click, module-UI style.

// the loaded skill inventory, from GET /skills (a live scan: a skill
// dir added after mount shows on the refresh).
export interface SkillInfo {
  name: string;
  description: string;
}

export interface SkillsUpdate {
  skills: SkillInfo[];
  // the fold re-read (/disabled): null means the read never landed
  // (unreachable, 501, a plain non-501 refusal) - the tab keeps the
  // fold it already shows, the enabled/disabled state stays stable.
  fold: string[] | null;
  // null carries the same meaning for the toggle seam flag.
  toggleable: boolean | null;
}

// refreshSkills re-runs the tab's load: GET /skills, then /disabled
// for the fold. Failure throws so the panel's existing error line
// shows it and the prior rows stand (the caller applies the update
// only on success).
export async function refreshSkills(): Promise<SkillsUpdate> {
  const res = await fetch("/skills");
  if (!res.ok) throw new Error(`GET /skills: ${res.status}`);
  const update: SkillsUpdate = {
    skills: (await res.json()) as SkillInfo[],
    fold: null,
    toggleable: null,
  };
  try {
    const dRes = await fetch("/disabled");
    if (dRes.status === 501) {
      update.toggleable = false; // no seam: read-only
    } else if (dRes.ok) {
      const foldBody = (await dRes.json())?.skills;
      update.fold = Array.isArray(foldBody)
        ? foldBody.filter((n): n is string => typeof n === "string")
        : [];
      update.toggleable = true;
    }
    // a plain non-501 refusal is neither: fold and flag keep what the
    // tab shows rather than guessing
  } catch {
    // /disabled unreachable is not fatal to the LIST; render read-only
    // rather than guessing the fold state
    update.toggleable = false;
  }
  return update;
}