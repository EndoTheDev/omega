package skills

// Skill is a loaded skill from a skill directory. The YAML frontmatter
// in the skill file provides name and description; the markdown body is
// the skill content injected into the system prompt when invoked. Dir
// is the path to the skill's directory, so the skill can reference its
// own files (scripts, references, templates) by relative path.
//
// Capsule-owned vocabulary (part 3 seam constitution): the type moved
// here from core with the seam interfaces - a capsule-shaped name in
// the substrate is residue. Consumers that touch entries declare their
// own data shapes and resolve through the registry.
type Skill struct {
	Name        string
	Description string
	// Author is the provenance class stamped in frontmatter: "agent"
	// when skills.write created the skill (the curator's
	// agent-created class), empty for hand-written skills.
	Author  string
	Content string
	Dir     string
}