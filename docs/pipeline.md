# The landing pipeline (merge train)

How card work reaches dev: one mechanical, no-LLM train pass. This page
is the operator + worker contract for pipeline v2 round 3 (train, marker
refs, review-at-close); round 1 documented CI dev signal + the stale
wt sweep, round 2 the embed lane, round 4 the nightly channel + drift
gate + soak automation.

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

## Journey suite run lock (worker verify law)

`scripts/journeys/run.sh` flocks one global run lock
(`/tmp/omega-journeys.lock`) for the whole suite body. Two suites running
at once interleave via shared chrome/agent-browser infra and mutual false
reds (proven 2026-10-28 during the v0.13.0 gate), so the suite never runs
concurrently:

- The default behavior is refuse: a second suite exits 3 with the holder
  pid, having created no evidence.
- A worker verify that may overlap the train pass or the nightly runs
  `JOURNEY_LOCK_WAIT=600 scripts/journeys/run.sh ...` and queues behind
  the live suite instead of colliding with it.
- `scripts/journeys/sweep-chromes.sh` honors the lock: called standalone
  while a suite is live it skips (exit 0); called from the suite (inherited
  fd 9) it proceeds.

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

# Round 4: nightly channel, drift gate, soak automation

The loop that makes stable releases cheap. Patch releases are nearly
automated; minor/major get a suggestion a human confirms; the stable
tag is Endo's word forever - version is a promise.

## The versioning law

- After stable `vX.Y.Z` ships, nightlies run at `vX.Y.(Z+1)-nightly.*`
  (one per day). The HYPHEN makes nightly a semver prerelease that sorts
  BELOW `vX.Y.Z`, and the prerelease flag keeps it out of the "Latest"
  release the updater's stable channel reads. (`vX.Y.Z+build` was
  rejected: `+` build metadata is ignored by semver precedence - the
  updater compare would break.)
- The next stable tag is the newest SOAKED nightly's commit re-tagged to
  that same number (`v0.9.1-nightly.20261022` -> stable `v0.9.1`) once
  Endo names it.
- Minor/major bumps are human-only, but auto-SUGGESTED:
  `scripts/release-suggest.sh` scans commits since the last stable tag
  and prints the suggested bump (BREAKING CHANGE marker or
  removed/renamed API surface -> major; any `feat:` -> minor; else
  patch) plus the evidence commits that drove it. The suggestion never
  tags anything - the release prompt renders it, Endo confirms or
  overrides in one word.

## The nightly channel (`.github/workflows/nightly.yml`)

Cron `30 3 * * *` (workflow_dispatch for manual runs too), against the
dev tip (pinned - scheduled runs otherwise default to master):

1. go vet + go test on dev tip.
2. validation journeys (`scripts/journeys/run.sh`) against a booted
   lane with no provider - model-driven journeys SKIP loudly, the
   presentation journeys decide the build.
3. Green -> tag `vX.Y.(Z+1)-nightly.YYYYMMDD` and push it. `release.yml`
   fires on the tag push for free and ships the 5-platform assets
   flagged prerelease (the Discord announcement step is quarantined to
   stable releases - the word is the marketing moment).
4. Red or partial -> NO tag. The run summary carries an alert block and
   GitHub notifies the failed scheduled run; master keeps its current
   soak point.
5. Green re-run on a day that already has its nightly skips loudly
   (one nightly per day).

## The drift gate (`scripts/drift-gate.sh`, wired hard in ci.yml)

CI on every dev push fails RED when dev is over 10 commits or over 7
days ahead of master. The report table (commits / days / merge-base /
verdict) lands in the run summary. Red is not an insult, it is the
tripwire: it means master is falling behind and a sync is due - a
release round, or the nightly soak ff. An unmeasurable base (missing
origin/master) is exit 2 = red: unknowable drift is never green.

## The soak law (the scheduled `soak` job in nightly.yml)

The newest nightly tag older than 48h fast-forwards master to itself iff
ALL of:

- nightly CI is green on the soak point (red or missing runs = hold),
- its release exists with assets and the prerelease flag on
  (channel separation is load-bearing - a stable-channel leak blocks),
- zero open regression reports are triaged against it (any hold = ff
  blocked until the fix re-soaks),
- master is still an ancestor of the soak point (never force; when
  stable already moved past it there is nothing to ff).

On green the job ff's master to the nightly and prints the release
prompt (suggested bump + evidence) in the run summary - the next stable
release round starts pre-filled. Every outcome writes its block into
the summary: no nightly yet / soaking `Nh` / soak green / blocked-why.
The soak definition Endo ratified: CI green + journeys + Endo's eyeball
round + external regression reports - the eyeball is what the 48h and
the human confirm are for.

## Updater channels

`omega update --nightly`, `omega.bak` rollback and the semver compare
ride the companion card (golang.org/x/mod/semver or equivalent - string
equality died on arrival). The prerelease flag above is what keeps the
stable channel from ever seeing a nightly.

## The wiring pieces

| Piece                | Job                                                     |
| -------------------- | ------------------------------------------------------- |
| `scripts/embed-lane.sh` | the ONE owner of committed `engine/http_channel/static/` |
| `scripts/train-pass.sh` | one mechanical landing per pass + marker refs + strikes |
| `scripts/train-bisect.sh` | the marker-chain walk: --list, or bisect by check   |
| `scripts/review-at-close.sh` | fresh review worktree + cold diff vs merge-base |
| `scripts/train-dry-run.sh` | stale wt/* sweep preview (stage 1, report/apply)   |
| `scripts/drift-gate.sh` | hard-fail drift check, dev vs master (10 commits / 7 days) |
| `scripts/release-suggest.sh` | suggested bump + evidence commits, never tags |
| `.github/workflows/nightly.yml` | nightly prerelease tag on green dev tip + the soak ff |

Journeys (run 3b) wire their suite in as `scripts/journeys/run.sh`;
train-pass already calls that path when it exists.