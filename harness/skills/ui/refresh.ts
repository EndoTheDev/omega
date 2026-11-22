// omega webui: the skills tab's fetch chain.

export interface SkillInfo {
  name: string;
  description: string;
}

export interface SkillsUpdate {
  skills: SkillInfo[];
  // the fold re-read (/disabled): null means the read never landed.
  fold: string[] | null;
  toggleable: boolean | null;
}

// refreshSkills re-runs the tab's load: GET /skills, then /disabled for the fold.
// Failure throws so the panel's existing error line shows it and the prior rows stand
// (the caller applies the update only on success).
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
  } catch {
    // /disabled unreachable is not fatal to the LIST.
    update.toggleable = false;
  }
  return update;
}