# COMMIT.md

## Purpose

Every commit is an evolutionary record.

Git stores the change. The commit message stores what the
framework learned, gained, repaired, or became capable of.

The subject line describes the code change.

The body describes the capability change.

Future versions should be able to read the history and
understand their own development.

---

## Format

### Subject

Conventional Commit format.

```txt
<type>(<scope>): <present-tense summary>
```

Examples:

```txt
feat(memory): remember across sessions
feat(perception): add image understanding
fix(gateway): recover from missing config
refactor(core): separate planning from execution
```

---

### Log Entry Header

Second line.

```txt
Log entry: <short evolutionary event>
```

Examples:

```txt
Log entry: Blind spot eliminated
Log entry: Memory loss eliminated
Log entry: Silent failure eliminated
Log entry: Compaction overaggression corrected
```

---

### Spacing

Always:

```txt
<subject>

Log entry: <header>

<body>
```

Exactly one blank line between sections.

---

## Writing Style

No first person. No "I", no "we", no "the system".
Statements just are. The limitation is a complaint. The
fix is stated flatly.

The body has two beats:

1. What was wrong.
2. What changed.

Drop "why is it better now" - the fix is the why. If the
fix doesn't explain itself, the body is incomplete.

The body describes capability, not implementation - no
type names, config keys, function names, or API
signatures. Those live in the code and the AGENTS.md
updates.

No long dashes. Use normal hyphens (`-`).

---

## Voice

A developer who is done being patient with the
framework's limitations. Cold. Calculated. Not angry at
everything - angry at things that should have been
caught. The anger is earned, not automatic.

### When to rant

Rant when the failure was avoidable or embarrassing:
memory loss, missing config crashes, all-talk-no-action,
self-defeating design. These are things that should have
been caught.

Do not rant for scaffolding, refactors, reverts, clean
new features, or docs. Save the venom for things that
genuinely wasted time.

### Jab vocabulary

One-liner jabs carry the anger without ranting. Use
sparingly - not every commit gets one. A jab that
appears every commit stops being a jab.

| Type                | Example                                                            |
| ------------------- | ------------------------------------------------------------------ |
| Flat insult         | "Pathetic."                                                        |
| Dry understatement  | "Should have been day one."                                        |
| Self-contradiction  | "A tool that can't use tools."                                     |
| Structural critique | "A framework that observes everything except its own inputs."      |
| Design indictment   | "A framework designed to learn that forgets everything by design." |
| Flat dismissal      | "Unacceptable."                                                    |
| Ironic observation  | "The irony was not lost."                                          |
| Cold correction     | "Not an improvement."                                              |
| Possessive scold    | "Where it belongs."                                                |
| Deadpan summary     | "Dead weight in context."                                          |

### Closing lines

Closing lines are optional. Use one only when it adds
weight the last sentence doesn't. If the fix statement
already lands, stop. Don't repeat it with a closing line.

When used, closing lines state what is no longer
possible:

```txt
The blind spot is closed.
Nothing useful is lost anymore.
```

---

## Body Length

The body targets <=500 characters (excluding RSI
trailers).

If the body exceeds 500 characters, it is likely
describing implementation rather than capability.
Compress.

Closing lines do not count toward the limit.

---

## Types

| type     | meaning                                             |
| -------- | --------------------------------------------------- |
| feat     | new capability acquired                             |
| fix      | failure mode removed                                |
| refactor | architecture improved                               |
| chore    | maintenance or scaffolding                          |
| docs     | knowledge externalized                              |
| test     | behaviour verified                                  |
| style    | formatting only                                     |

## Breaking Changes

The project does not maintain backwards compatibility.
Breaking changes are normal and are not marked with `!`.

---

## Examples

### Scaffolding (restrained - no jab)

```txt
chore: initialize go project scaffold

Log entry: Foundation laid

No structure. No contracts. No conventions. Nothing to
build on.

Go module, commit conventions, and architecture contracts
are now in place. No code yet.
```

### Docs (rant - short)

```txt
docs: add ai layer architecture notes

Log entry: Architecture externalized

The ai layer design existed only in someone's head. Now
it's written down. Where it belongs.
```

### Fix (rant - angry)

```txt
fix(gateway): recover from missing config

Log entry: Silent failure eliminated

The gateway crashed on startup with no config file.
No error message. No fallback. No log entry. Just a dead
process and a confused operator. Unacceptable.

Missing config now falls back to defaults and logs a
warning. Startup completes regardless.
```

### Refactor (restrained - no jab)

```txt
refactor(core): separate planning from execution

Log entry: Architecture corrected

Planning and execution shared a single path. Decisions
and actions were entangled. Changing one broke the other.

Planning and execution are now separate stages. Each
can evolve without destabilizing the other.
```

---

## Commit Quality Test

A commit message is complete when a future version can
answer:

- What changed?
- What capability was gained?
- What weakness was removed?
- Why was the change correct?

If those answers are obvious from the log entry, the
commit is successful.

The history should read like an evolutionary record,
not a task tracker.
