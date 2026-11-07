# REVIEW.md

The reviewer lens law. Workers and the review pass both load this:
workers so a diff fails these checks before review, reviewers so
findings land in one shared shape. The factory skill points here as
the single authority - this content lives nowhere else.

## What failure looks like (per area)

Each block names a class this repo has actually shipped:

- **webui** - renders wrong, throws on tab switch, drifts from the
  iconset law (icon or size), mode/class drift between shell and
  module UI, bundle stale vs source (the embed-lane miss: source
  edited, static/ never rebuilt - t_0a108f16).
- **Go core/agent** - blocks the agent loop, races the config lock,
  leaks goroutines or zombies, blocks a pipe on a grandchild kill.
- **harness** - corrupts state when a crash lands mid-write, bakes
  config at mount instead of reading it live (the snapshot class -
  t_34cd1921), escapes the capability jail.
- **docs** - lies: a stale claim contradicted by code. Or restates
  a law that already lives elsewhere.

## Finding tags

Every finding is tagged at post:

- `IMPORTANT` - blocks the merge.
- `NIT` - never blocks a merge.
- `PRE-EXISTING` - present at the merge-base; the card did not
  introduce it. Never counted against the author. Does not block
  the card.

## Security lens

Every diff answers one question in the review comment: does the
change touch auth, path handling, subprocess, or config writes? A
yes without analysis is itself a finding. (Receipt: unauthed PUT
/settings hot-setting provider.instances survived to a retroactive
audit on 2026-11-07. This lens exists so review catches it, not a
later audit.)

## Review comment shape

- Lead with a count: `3 findings: 1 important, 2 nits`. Nothing to
  fix is said plainly: `Nothing to fix.`
- Evidence rule: every finding names file + line. No guesses from
  names.
- Flag the CLASS, not the instance: a repeated defect becomes a law
  line in this file or a lint check. Fixes land at the source.

## Reviewer scope

Skip in review: generated bundles, `.worktrees/`, `bin/` (tracked
skills data excepted).

---

Pattern adapted (ideas, not expression) from the Claude Code team's
review writeup: <https://claude.dev/blog/solving-code-review-at-anthropic>
