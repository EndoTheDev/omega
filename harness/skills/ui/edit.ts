// omega webui: the Skills tab's SKILL EDITOR chain. The three write laws the Go face
// enforces (harness/skills/edit.go), mirrored so the editor never ships a skill the
// provider would reject - save: the name is a single folder component (the folder name
// IS the identity, so editing a row keeps its name), the description one line, the body
// non-empty and never opening with a --- fence (the frontmatter is composed for the
// editor).

import type { SkillInfo } from "./refresh";

// SkillDraft is the form shape as the editor fields bind it.
export interface SkillDraft {
  name: string;
  description: string;
  content: string;
}

// fetchSkillDraft prefills the form from the real file.
export async function fetchSkillDraft(name: string): Promise<SkillDraft> {
  const res = await fetch(`/skills/content?name=${encodeURIComponent(name)}`);
  if (!res.ok) throw new Error(`GET /skills/content: ${res.status}`);
  const body = await res.json();
  return {
    name: String(body.name ?? name),
    description: String(body.description ?? ""),
    content: String(body.content ?? ""),
  };
}

// emptyDraft: a new skill starts nameless, undescribed, empty-bodied.
export function emptyDraft(): SkillDraft {
  return { name: "", description: "", content: "" };
}

// skillNameLaw is the mirrored folder-name law.
export function skillNameLaw(name: string): string {
  const n = name.trim();
  if (n === "") return "name is required";
  if (n === "." || n === "..") return "name must be a single folder name";
  if (n.includes("/") || n.includes("\\")) return "name must be a single folder name";
  if (n.startsWith(".")) return "name must not start with a dot";
  return "";
}

// validateDraft returns "" when the draft obeys the laws the Go provider enforces.
export function validateDraft(d: SkillDraft): string {
  const nameMsg = skillNameLaw(d.name);
  if (nameMsg) return nameMsg;
  const desc = d.description.trim();
  if (desc === "") return "description is required";
  if (desc.includes("\n") || desc.includes("\r")) return "description must be one line";
  if (d.content.trim() === "") return "body is required";
  if (d.content.replace(/^\s+/, "").startsWith("---"))
    return `body must not start with a "---" fence - the frontmatter is composed for you`;
  return "";
}

// saveSkill POSTs the draft to /skills/save.
export async function saveSkill(d: SkillDraft): Promise<SkillInfo[]> {
  const res = await fetch("/skills/save", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      name: d.name.trim(),
      description: d.description.trim(),
      content: d.content,
    }),
  });
  if (!res.ok) {
    const detail = await res.text().catch(() => "");
    throw new Error(`POST /skills/save: ${res.status}${detail ? ` - ${detail.trim()}` : ""}`);
  }
  return (await res.json()) as SkillInfo[];
}

// deleteSkill removes one skill (POST /skills/delete. The fresh listing answers; an
// unknown name throws with the 404 - never a silent no-op.
export async function deleteSkill(name: string): Promise<SkillInfo[]> {
  const res = await fetch("/skills/delete", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) {
    const detail = await res.text().catch(() => "");
    throw new Error(`POST /skills/delete: ${res.status}${detail ? ` - ${detail.trim()}` : ""}`);
  }
  return (await res.json()) as SkillInfo[];
}
