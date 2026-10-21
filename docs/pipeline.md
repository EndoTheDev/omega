# The landing pipeline (merge train)

How card work reaches dev: one mechanical, no-LLM train pass. This page
is the operator + worker contract for pipeline v2 round 3 (train, marker
refs, review-at-close); round 1 documented CI dev signal + the stale
wt sweep, round 2 the embed lane.

## Card flow

1. **Worker** does the work on the card's branch (`wt/<task-id>`,
   created by the board's worktree workspace at claim time). The worker
   runs its own gates (go build/vet/test, embed lane on the source side)
   but NEVER lands on dev and NEVER pushes the card branch. When done:
   `hermes kanban request-review`.
2. **Reviewer** reviews at close with a fresh cold diff, never a stale
   pre-review from card creation time:

   ```
   scripts/review-at-close.sh <task_id>
   ```

   That builds `.worktrees/review-<task-id>`, computes the merge-base
   with dev, and hands over the commit list + full cold diff. Review is
   read-only (see CONTRIBUTING.md). Approval closes the card: done.
3. **Train** picks the oldest done card whose branch still carries
   commits not on dev:

   ```
   scripts/train-pass.sh          # one landing per pass
   ```

   Rebase onto the dev tip, full gates on the merged tree (go
   build/vet/test + `-race` on touched packages, dep-law gate, webui node
   suite, embed-lane static regen, validation journeys), then
   `merge --no-ff`, push `origin dev`, marker ref
   `refs/train/<seq>-<task_id>` at the merge commit. Green or red, the
   state machine is the same for every card.

## Failure laws

- **Rebase conflict:** the pass aborts, the card keeps its done status
  and a strike comment. Strike 2 = the card auto-demotes to ready with
  the conflict note (a worker rerun rebases onto the new dev tip and
  re-reviews). Strike 3 = a reconciler agent card is created instead of
  another retry, and the original card is excluded from the queue
  (its branch stays for archaeology).
- **Red gate:** the merge is un-landed (dev resets to the pre-pass tip),
  the failure tail goes on the card as evidence, the card goes back to
  ready, and the rebased branch is KEPT - the red is the work's problem
  against dev, not the rebase's. A regression fix enters the queue as a
  normal card with the evidence in its body and outranks by severity.
- **The train never rewrites:** origin/dev must be an ancestor of local
  dev before the pass runs; a diverged origin stops the pass loudly.

## Marker-bisect law

Every landing leaves its marker, so "which landing introduced this" is
mechanical, always:

```
scripts/train-bisect.sh --list              # the chain in landing order
scripts/train-bisect.sh <bad-sha> --cmd ./failing-check.sh
```

The bisect probes the landing trees only (each marker = the tree that
landed), binary-searches for the first landing whose tree fails the
check, and names the culprit with its marker ref. A landing tree includes
its embed-lane regen commit, so a "correct source, broken bundle" case
bisects to the landing that shipped it.

## Cap raise (workers per profile)

`kanban.max_in_progress_per_profile` moves 2 -> 4 only after both gates:

- (a) two parallel webui cards landed back-to-back clean via the embed
  lane - MET (pipeline v2 proof A + proof B).
- (b) the worker onboarding skill is live - hermes-side, pending. Until
  it lands, the cap stays at 2 and the flip is recorded, not applied.

## The wiring pieces

| Piece                | Job                                                     |
| -------------------- | ------------------------------------------------------- |
| `scripts/embed-lane.sh` | the ONE owner of committed `modules/http_channel/static/` |
| `scripts/train-pass.sh` | one mechanical landing per pass + marker refs + strikes |
| `scripts/train-bisect.sh` | the marker-chain walk: --list, or bisect by check   |
| `scripts/review-at-close.sh` | fresh review worktree + cold diff vs merge-base |
| `scripts/train-dry-run.sh` | stale wt/* sweep preview (stage 1, report/apply)   |

Journeys (run 3b) wire their suite in as `scripts/journeys/run.sh`;
train-pass already calls that path when it exists.