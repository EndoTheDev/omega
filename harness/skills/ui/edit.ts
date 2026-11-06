// omega webui: the Skills tab's SKILL EDITOR chain (card t_0a108f16,
// the CRUD capsule round). The behavior lives in this PURE module (the
// cron edit.ts convention, card t_ecec864e): the node suite pins the
// validation laws and the wire shape with a fetch log - the SFC only
// wires the form and the buttons.
//
// The three write laws the Go face enforces (harness/skills/edit.go),
// mirrored so the editor never ships a skill the provider would
// reject - save: the name is a single folder component (the folder
// name IS the identity, so editing a row keeps its name), the
// description one line, the body non-empty and never opening with a
// --- fence (the frontmatter is composed for the editor).

import type { SkillInfo } from "./refresh";

// SkillDraft is the form shape as the editor fields bind it.
export interface SkillDraft {
  name: string;
  description: string;
  content: string;
}

// fetchSkillDraft prefills the form from the real file: GET
// /skills/content?name= answers the parsed description plus the body
// under the fence (the viewer tab's own route). Unknown name = 404,
// thrown - the editor shows it in its error line.
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

// skillNameLaw is the mirrored folder-name law (validSkillName in
// harness/skills/skills.go): a single directory component, no
// separators, no hidden dot names, no padding whitespace.
export function skillNameLaw(name: string): string {
  const n = name.trim();
  if (n === "") return "name is required";
  if (n === "." || n === "..") return "name must be a single folder name";
  if (n.includes("/") || n.includes("\\")) return "name must be a single folder name";
  if (n.startsWith(".")) return "name must not start with a dot";
  return "";
}

// validateDraft returns "" when the draft obeys the laws the Go
// provider enforces, else the human error line.
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

// saveSkill POSTs the draft to /skills/save (upsert: a new name
// composes the folder, an existing one rewrites the body under the
// fence) and answers the fresh listing in the GET /skills row shape -
// one response carries the state it landed, no re-fetch. A failed POST
// throws, so the prior rows stand.
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

// deleteSkill removes one skill (POST /skills/delete: the server backs
// the folder up and receipts the removal). The fresh listing answers;
// an unknown name throws with the 404 - never a silent no-op.
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
