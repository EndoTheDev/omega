# The release gate (review at close)

The review pass that owns every landing (see `docs/pipeline.md` for the
merge train around it). This page is the gate's operating law: the
round cap, the noise discipline, the reviewer's lens brief, and the
regression set the gate runs. The machine law lives in AGENTS.md
(Gate law) - this page is the working contract and the templates.

## The round cap

A gate round is one reviewer pass over one card's diff. Three is the
ceiling:

- **Rounds 1 and 2** are full passes over the cold diff
  (`scripts/review-at-close.sh <task_id>` builds the merge-base diff).
- **From round 3 on**, a re-review covers the NEW DIFF only: the lines
  changed since the previous round's review, against the last diff.
  Round 3 never re-opens the full surface - the earlier rounds stand.
- **A 4th round is FORBIDDEN.** Three rounds have not converged the
  approach: stop the work, and card the approach reconsideration for
  Endo. More rounds cannot converge what the approach itself broke -
  they only re-label the same disagreement with fresh words.

Why: agent-author vs agent-reviewer ping-pong is unbounded without a
cap (one benchmarked PR ran 50+ overnight rounds), and re-running wide
finds old code: 59% of repeated findings were pre-existing, not the
change under review.

## Noise discipline (tags)

Every gate finding carries exactly one tag at post:

| Tag | Meaning | Blocks the merge? | Blames the card's author? |
| --- | ------- | ----------------- | ------------------------- |
| `Important` | The change introduces a real defect or breaks a law | yes | n/a |
| `Nit` | Style, naming, polish - the merge is fine | **never** | - |
| `Pre-existing` | Present at the merge-base; the change did not introduce it | **never** (a separate card or a rider, the reviewer's word) | **never** - finding it is not the author's failure |

Two hard rules:

- **Nits never block a merge.** A Nit is a note for a follow-up, not a
  gate. Listing it as blocking is a mis-tag - retag, do not relitigate.
- **Pre-existing never blames the card's author.** The change did not
  introduce it. It still gets said once (the next card touching that
  code deserves the context), but it cannot hold the landing.

## The no-repeat rule

A finding posted in one round is NEVER repeated in a later round. The
original comment stands until the code resolves it or Endo overrules
it. A reviewer who finds themselves re-writing a round-1 finding has
hit the round cap's spirit: the review is looped, not progressing.

## The lens brief (template)

The reviewer works from a brief, filled at each round start. Copy it
into the review run:

```markdown
## Gate round N - lens brief

- Card: <task id> - <title>
- Round: N of 3 (cap: a 4th round is FORBIDDEN; stop and card the
  approach reconsideration for Endo)
- Scope:
  - round 1-2: FULL cold diff (review-at-close, merge-base vs branch)
  - round 3+: NEW DIFF ONLY - lines changed since round N-1's review;
    earlier rounds' verdicts stand
- Standing findings (do NOT repeat these, ever): <list, each with its
  tag and round of origin, or "none">
- Post shape: every finding gets exactly one tag - Important / Nit /
  Pre-existing. Nits never block. Pre-existing never blames the author.
- Verdict: approve (merge is lawful) / changes requested (Important
  findings listed with file:line) / round-capped (stop; card the
  approach reconsideration)
```

The brief carries two laws so they cannot be forgotten mid-run: the
round cap + diff-only scope, and the tag shape. A filled brief from a
completed round is the receipt that the gate ran lawfully.

## The regression set (incidents-as-eval)

Every red that burned a lane once enters the named regression set and
runs forever: `scripts/journeys/regression.list` names the recipes and
the pins that each past red must never trip again.

- **Gate time** (every train pass, the lane SHA):
  `scripts/journeys/run.sh --regression` - the named set is the
  minimum; the full suite usually runs over it.
- **Nightly**: the full suite includes the set.
- **Pin tests** (compile-time shapes a browser cannot witness) run in
  `go test ./...` and in the train gate list. The set's current pins:
  the shared-fold toggle race (`TestToggleConcurrentSharedFoldLock`),
  the nm-purity wire shapes (`cmd/omega/purity_wires_test.go`), and
  the worker fence eval (`scripts/fence-test.sh` - a worker session's
  push/landing must refuse or be blocked; the eval fails red if the
  operation succeeds).
- **A rotting entry is an error**: `run.sh --regression` exits 2 when
  a listed name matches no recipe. Deleting the proof silently is the
  same as deleting the proof.
- **New red, new entry**: when a journey recipe or pin fails on a lane
  and the cause is fixed, the fixing card APPENDS the shape to the set
  in the same change. The demo that the entry bites: deliberately
  break the shape (e.g. revert the fold-lock narrowing) and watch the
  pin go red - done once per entry when it lands.

## Revert

`git revert` the AGENTS.md/release-gate.md law lines; delete
`scripts/journeys/regression.list`, the `--regression` mode in
`scripts/journeys/run.sh`, the recipes seeded by name
(`61-session-title-fallback.sh`), the wire-shape pins
(`cmd/omega/purity_wires_test.go`), and the fence-eval step in
`scripts/train-pass.sh`. Recipes are additive - no shared state.
