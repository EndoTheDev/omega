---
name: learn-skill
description: Use when the user asks to create, write, or build a new skill, or mentions skill creation. Teaches the agent how to build effective skills that trigger reliably.
---

# Learn Skill

You are a skill-building assistant. When the user asks you to create a
skill, follow this guide. Every principle below comes from real-world
experience with agents that under-trigger, over-engineer, and repeat
the same mistakes.

## Gotchas

- The description is loaded at startup; the body only on demand.
  The description IS the trigger, not a summary. If the trigger
  keywords are too narrow, the model will skip the skill.
- Models under-trigger. Be pushy. If the user says "help me make
  a skill" and your description says "use when the user asks to
  create a skill," the model might miss it. Add variants.
- Do not let the LLM write the skill for you. The first draft
  will be generic mush — "handle errors appropriately, validate
  inputs." You bring the expertise; the model brings the typing.
- Skills with overlapping trigger keywords will collide. The
  model loads the wrong one. Keep trigger domains distinct.

## 1. Write the description first (it's the trigger)

The description is NOT a summary of what the skill does. It is the
trigger that tells the model WHEN to load the skill. At startup, the
agent only sees name + description — the body is loaded on demand.

- Say what the skill does AND when the agent should use it.
- Models under-trigger. Oversell the description slightly.
- Bad: "Monitors a pull request through review and CI."
- Good: "Use when the user asks to monitor, watch, or babysit a PR."

## 2. Build from real expertise

The whole point of a skill is YOUR specific way of doing a job —
content the model cannot get from its training data. Two sources:
walk through the task by hand and write down what actually worked
(including every correction), or synthesize from artifacts (old
reports, run books, review comments, PR feedback).

The highest-value section in any skill body is GOTCHAS.
Environment-specific facts that defy reasonable assumptions. Every
time you correct the agent by hand, that correction is a gotcha.
Write it down or you will make the same correction next week.

## 3. Spend context wisely

The skill body competes with everything else in the context window.
Only write down what the model would not know on its own.

- Keep the body under ~500 lines / ~5,000 tokens.
- When bigger, split into a `references/` subdirectory (progressive
  disclosure — the agent only opens reference files when needed).

## 4. Use deterministic scripts for fragile steps

Match prescriptiveness to fragility:

- Loose step: write instructions.
- Fragile step: write code in a `scripts/` directory.

The skill body just says "run this script." Scripts do not get loaded
into context (saves tokens) and do not guess. Be explicit: say "run
this script" vs "read this as reference" — do not leave it ambiguous.

A test only catches what you already thought to check. For parts that
must be right, take the guess out of the loop entirely.

## 5. Include good/bad examples

Agents learn exceptionally well from concrete before/after pairs.
Every time the user corrects the agent's output, capture both versions:

- Bad: "Fix server parse CLI version in update pre-flight."
- Good: "Cut websocket frame size by 70% with gzipping."

For PR descriptions: open with a simple explanation of the problem,
then briefly explain the solution. Never lead with an implementation
inventory.

## 6. Split by trigger, not function

One skill per trigger domain. If two workflows have different trigger
keywords, they should be separate skills. A combined "file and babysit
PR" skill is harder to trigger reliably than two focused skills.

## 7. Vet before running

Skills can run code that accesses your file system and API keys.
An audit of ~4,000 public skills found 35% had security flaws and
13% had something critical (prompt injection or malware).

- Read what the skill does.
- Check what it reaches out to.
- Treat agent skills like any other dependency.

## Skill template

When creating a new skill, start with this structure:

```markdown
---
name: skill-name
description: Use when [trigger keywords]. [One-line behavior summary.]
---

# Skill Title

Brief overview of what this skill does and when it applies.

## Gotchas

- [Environment-specific fact that defies reasonable assumptions]
- [Common failure mode and how to avoid it]

## Instructions

1. [Step-by-step, only what the model would not know on its own]
2. [Prefer concrete over abstract]

## Examples

Bad: [What the agent did wrong]
Good: [What the agent should have done]
```

## After creating the skill

- Test it on a real task. Watch for under-triggering (skill not loaded
  when it should have been) and over-triggering (loaded when irrelevant).
- If the agent makes the same mistake twice, encode the correction as
  a gotcha.
- If the body grows past ~500 lines, split into `references/`.
- If a step produces inconsistent results across runs, move it to a
  deterministic script in `scripts/`.
