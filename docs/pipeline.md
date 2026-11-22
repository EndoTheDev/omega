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
   pre-review from card creation time. The gate's operating law - the
   3-round cap, the tag shape (Important / Nit / Pre-existing), the
   no-repeat rule - lives in `docs/release-gate.md`:

   ```
   scripts/review-at-close.sh <task_id>
   ```

   That builds `.worktrees/review-<task_id>`, computes the merge-base
   with dev, and hands over the commit list + full cold diff. Review is
   read-only **except the merge**: an APPROVED verdict lands the card in
   the same session (merger shape A, below) - the reviewer runs no other
   write.

3. **The merge follows the review seat.** An approval that stops at
   "done" waits up to a tick interval for the next merge pass, and the
   wait grows with throughput. So the APPROVED verdict includes the
   landing:

   ```
   bash scripts/factory-merge.sh <task_id>     # the review seat's merge entry
   ```

   Same cycle as the tick's, for one named card (branch merge, gates
   green ON THAT TREE, push, then `bash scripts/factory-prune.sh <id>`).
   A CHANGES REQUESTED verdict, a held pipeline lock or a failed landing
   leaves the card exactly where the previous flow left it.

4. **Train (the fallback sweep)** picks every done card whose branch
   still carries commits not on dev - which is to say, **it merges only
   what the review seat left behind**:

   ```
   scripts/train-pass.sh          # one landing PASS: the whole batch, one battery
   ```

   The pass takes the queue as a BATCH: each branch is rebased onto the
   tip the previous merges left, every card is merged with `merge --no-ff`
   and NO gate between merges, then the full gate battery runs ONCE on the
   final tree (go build/vet/test + `-race` on the packages the batch
   touches - the race leg is SKIPPED with one receipt line on a host with
   no cgo toolchain, see below - dep-law gate, worker fence eval, webui
   node suite, embed-lane static regen; the browser legs are CI-owned and
   never run in the pass, see below), then push
   `origin dev` and one marker ref `refs/train/<seq>-<task_id>` per card at
   its merge commit. Green or red, the state machine is the same for every
   card. A card that conflicts or replays wrong is dropped from the batch
   (strike law below) and the rest still land.

   **A red battery is attributed by the bisect, not by a re-gate.**
   `scripts/train-bisect.sh --chain` walks the batch's merge commits (they
   carry no markers yet - a marker is a landing, and the battery decides
   that), scoring each candidate with the pass's own battery in
   `--gate-only` probe mode. The culprit un-lands with the red-gate
   semantics, the cards after it re-merge onto the new tip, and the battery
   runs once more on that tree before the push. A red the walk cannot pin
   to a card (a pre-existing red on dev, a served-lane red) un-lands the
   whole batch: nothing is pushed ungated. Probe mode keeps the code legs
   and skips the two legs a probe checkout cannot measure - the webui
   node suite and the embed lane both need the main checkout's
   `node_modules`; the journeys are CI-owned and run nowhere locally.

   The queued branch is the row's `branch_name`, or `wt/<task_id>` when the
   row carries none - a scratch-spawned card lands with a NULL
   `branch_name` while its worker still commits to `wt/<task_id>`, and the
   pass repairs the row on the landing instead of needing a hand edit. A
   card the reconciler took out of the queue carries its exclusion event
   and is never resolved back in.

   The marker's ORIGIN copy dies with the card: the prune deletes it
   together with the worktree and the branch (`PRUNED-MARKER`). A consumed
   marker keeps its landing's ancestry reachable from the remote for as
   long as it survives - through a history rewrite it ships the whole
   pre-rewrite tree - so nothing has to sweep stable boundaries. The LOCAL
   ref stays: `train-pass` allocates the next seq from the local refs and
   `train-bisect` walks them. A marker whose commit this clone cannot
   prove is in the merge ref is named (`NOTE <id> marker-kept-unverified`),
   and a remote that will not answer is named the same way
   (`NOTE <id> marker-origin-unreadable`) with the prune exiting held - a
   silent skip is never a reap.

## The review-seat merge (merger shape A)

Ruled 2026-11-17 (card t_5b61405b): the merge follows the review seat.
An APPROVED verdict lands the card in the same session instead of waiting
for the next tick's merge pass; with 3+1 capacity the old gap grew with
throughput.

```
bash scripts/factory-merge.sh <task_id>      # the review seat's entry
```

- **It is the same cycle, not a second one.** The script pins
  `scripts/train-pass.sh` to the named card (`TRAIN_CARD`) and lets it
  land: rebase onto the dev tip, the full gate list green ON THAT TREE,
  `merge --no-ff`, push, marker ref, then `scripts/factory-prune.sh
  <id>`. Strike, demote and un-land laws are the train's own, because the
  code is the train's own.
- **The approval proof is mechanical.** The pin only lands a card the
  board shows as `done` with a live branch, so an unreviewed or running
  card refuses.
- **One author per checkout.** The merge takes the tick's pipeline lock
  (`~/.hermes/cache/scratch/omega-pipeline.lock`). A live tick pass
  defers the review-seat merge (`DEFERRED`, exit 3) rather than authoring
  beside it, and the tick's next merge pass sweeps the card.
- **Idempotent.** A card already in dev answers `ALREADY-MERGED` and still
  runs the prune: the board can never show a double merge, and a second
  attempt is a clean refusal.
- **The rebuild booking is booked, never run.** The script prints
  `REBUILD-DUE` / `REBUILD-NOT-DUE` from the landing's changed files
  against the runtime roots (`cmd/`, `core/`, `agent/`, `harness/`,
  `llm/`, `api/*.go`, `webui/app/`, `_test.go` excluded), and
  `EYEBALL` when UI is in range (the imagery receipt is owed). The lane
  rebuild stays with the tick: product bytes only, batching law untouched.
- **The end state is a chain, not one commit.** A landing normally ends at
  the train's two-parent merge commit. An SPA merge ends one commit past
  it: the gate list's embed-lane rebuild commits the regenerated bundle on
  top, whose parent is that merge commit. Both shapes pass the end-state
  check, and the `MERGED` line names the merge sha plus
  `(+ embed-lane bundle <sha>)` when the bundle commit is there. Any other
  shape - no merge commit in the chain, or a merge that does not sit on the
  pre-landing tip - is a real divergence: `FAILED`, exit 6.
- **The tick's merge pass is the FALLBACK sweep.** It merges only what
  the review seat left behind - a deferred lock, a failed landing, a
  session that ended early - which is exactly what its queue (oldest done
  card whose branch is not yet in dev) already means.
- Greppable lines and exit codes are documented in the script header;
  they follow `factory-prune.sh` conventions (`REFUSED`, `NOTE`,
  `<VERB> <id> ...`).

## The train's environment contract (no caller env required)

Ruled from the first real train pass on winbtw (card t_7acaa267,
2026-11-18): the review seat exports no environment, so a pass that needs
one is a pass that cannot land. Two values used to be assumed, and both
assumptions were wrong on a host whose live home was remapped.

- **The board db resolves itself.** `$KANBAN_DB` > `$HERMES_KANBAN_DB` >
  the live home's `kanban/boards/<board>/kanban.db` (`<board>` is
  `$HERMES_KANBAN_BOARD`, default `project-omega`). The live home follows
  the hermes CLI's own chain: `$HERMES_KANBAN_HOME`, else `$HERMES_HOME`
  (a `profiles/<name>` home names its root), else windows
  `%LOCALAPPDATA%/hermes`, else POSIX `$HOME/.hermes`. The old
  `$HOME/.hermes` default is the LEGACY home on winbtw and made every
  review-seat merge die `board db not found` exit 2 before the queue.
  An explicit `KANBAN_DB` that names no file refuses without falling back
  (landing another board's queue silently is worse than a dead pass), and
  a pass with no candidate names every probed path.
- **The journeys debugging unit's lane env is derived or demanded BEFORE
  the merge.** The battery owns no browser leg (below), so this applies
  exactly when `TRAIN_JOURNEYS_CMD` names one command. Six recipes seed
  agent homes into the serving lane home and die loudly
  when `$OMEGA_HOME`/`$OMEGA_BIN` do not name it (110, 114, 60, 92, 94,
  96). The train derives `<checkout>/bin` when that directory is a serving
  home (`config/config.yaml` plus the `omega` binary the lane's serve was
  launched from), and otherwise refuses exit 2 naming what it probed. The
  same precondition health-checks the lane
  (`$JOURNEY_BASE_URL`, default `http://127.0.0.1:8098`).
- **A precondition refusal is not a strike and not a demote.** Nothing is
  rebased, nothing is merged, the card keeps its status, its branch and
  its verdict. `factory-merge.sh` prints a `NOTE` naming the class, so the
  review seat reads "the environment is wrong", not "the work is wrong".

### The journeys gate is CI-owned (ruling 2026-11-22)

The browser legs of the merge gate run in CI, not in the pass. Every push to
dev triggers the `Journeys regression` job in `.github/workflows/ci.yml`: it
builds the binary, boots a provider-less validation lane and runs
`scripts/journeys/run.sh --regression` inside the runner. The nightly keeps
its full-suite soak unchanged.

- **Why.** The one host that can serve a lane also runs the workers and the
  gateway. A local chrome battery there starves itself and reports the
  starvation as a tree red: journey 118 timed out in `wait_js` at 30s/39s two
  nights running with 47 recipes green around it.
- **Trades (accepted).** A browser red arrives post-hoc, 20-40 min after the
  push, instead of blocking the landing. The mitigations are fix-forward, the
  un-land discipline (the tick reverts the merge exactly like a local red,
  branch kept, card back to ready), the tip check in
  `scripts/factory-dispatch.sh` (no new dispatch while the dev tip's
  regression job is red or in flight), and the nightly soak as the 24h fence.
- **No local chrome at all, not even a subset.** The lane-smoke covers boot
  liveness; any local chrome recreates the starvation class at smaller scale.
- **`TRAIN_JOURNEYS_CMD` is the debugging unit.** One command, hand-run when
  a CI red must be reproduced locally; it is the only way a local chrome
  starts, and the lane-home and lane-health preconditions apply exactly then.
- The authoritative full-suite verdict is the nightly, which builds the lane
  from the tree it tests. The pass's own battery stays: build/vet/test,
  `-race` on touched packages, dep law, the fence eval, the webui node suite,
  the embed lane.
- **The race leg skips, it never reds, on a host with no cgo toolchain.**
  `go test -race` cannot run without one (winbtw has no C compiler and
  `CGO_ENABLED=0`). Ruling 2026-11-10 (card t_7cc44bf1): the leg is
  SKIPPED there, permanently, with one receipt line naming the skip -
  `race leg skipped: no cgo toolchain on this host (t_7cc44bf1, Endo
  2026-11-10)` - and the pass keeps gating. The signal stays on the
  nightly and CI, where cgo exists; a cgo host keeps the hard-fail
  exactly as before, so the skip can never hide a real race. The probe
  is a trivial cgo compile, not `go env CGO_ENABLED` (a file-list build
  reports 0 as "no Go source files", and `CGO_ENABLED=1` can still carry
  an unresolved `CC`).

### The regression set is the journeys gate (ruling 2026-11-19, home moved 2026-11-22)

`regression.list` is the set a recipe enters only after it burned a lane
once, so it is the gate-time minimum. The CI regression job runs exactly that
set (`run.sh --regression`); the full suite is the nightly's job, and the
nightly alone exports the lane env for it.

- **Receipts for the narrowing** (2026-11-19, dev lane serving `9f77f768`):
  the whole regression set ran 12/12 green in 706s, and
  `75-divider-resize` - the red the earlier rejection of narrowing rested
  on - was green on the current lane. The standing red of that week,
  `92-registry-toggles`, is NOT in the set; it is carded as its own product
  finding (t_a68f59b1).
- **`TRAIN_JOURNEYS_CMD` still replaces the whole command** (flags
  allowed) for the hand-run debugging unit, and the lane-home and
  lane-health preconditions above apply to whatever it runs. Widen the gate
  by adding a recipe to `regression.list`, never by asking a local run for
  the full suite.

## The machine fence (worker sessions)

Law text alone did not hold: twice in one weekend a worker invented
landing authority where written law had a gap - t_529628d5 merged, pushed
origin dev and restarted the lane on its own; t_5782d11e ff-merged dev
twice mid-run with stated push intent. The spawn path now stamps every
worker session and `bin/shims/git` sits at the FRONT of its PATH, so
bare `git` in a worker shell is the fence shim.

- Detection is env-scoped: the shim fences only when `OMEGA_WORKER` is
  set. The tick and the main session run unstamped and keep landing,
  pushing and rebuilding by law.
- Always refused: `git push` (any remote, any branch - dry-run included).
- Refused when an argument names dev or master (any spelling: `origin/`,
  `refs/heads/`, refspecs, revision suffixes): merge, rebase, reset,
  pull, fetch, cherry-pick, revert, am - and `branch -f/-m` onto them,
  `update-ref refs/heads/...`, or a commit while a dev/master checkout
  is current. Ordinary work (status, add, commit on the wt branch, tag,
  log, checkout) forwards to real git untouched.
- A refusal prints the law line and exits non-zero: the transcript
  records it. The refusal is mechanical - no card body, memory note or
  prompt unblocks it.
- **The ONE sanctioned door: `scripts/factory-merge.sh`, run by the review
  seat on an APPROVED verdict.** It re-execs once without the stamp and
  without the shim dir on PATH, after the board has proven the card is a
  queued (approved) landing - then the train lands under the review gate's
  authority. Every worker shell outside that door stays fenced: the
  exception is the review seat's landing, not a worker's ambition.
- The stamp is scoped to the dispatch invocation in
  `scripts/factory-dispatch.sh` (kanban workers; a missing shim aborts
  the pass loudly instead of dispatching unfenced) and to the delegate
  module's child env (omega subagents). Unstamped sessions forward to
  real git byte-for-byte.

The dispatch invocation's env dies at the gateway boundary: the worker
client only submits a claim, and the owner builds the repo-writing
session's env itself in the worker interpreter
(`gateway.session_kanban.bind_worker_context`). The stamp therefore
lands THERE, driven by the hermes config knob
`kanban.worker_fence_shim_dir` (the shim dir; unset -> no stamp, so
the main session and any other board on the host stay unfenced). It
applies `OMEGA_WORKER=1`, `OMEGA_WORKER_CARD=<task id>` and prepends
the shim dir ahead of system git on PATH of every bound kanban worker
session - the tick dispatch, the dashboard dispatch, retries included.
`scripts/fence-test.sh` proves the bound env by /proc and the refusal
through bare `git` resolved from exactly that env. The dispatch CLI's
own env prefix stays as a second belt: one spawn funnel, two writes.

Revert: remove `bin/shims/`, the dispatch env prefix, the delegate
stamp, and the bind stamp in the hermes checkout
(`ops/omega-worker-fence`, gateway/session_kanban.py). Nothing
persists; sessions without the stamp run unfenced.

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
- Ownership is not a pid. Pids are recycled, so the recorded holder is
  checked against its own command line (`/proc/<pid>/cmdline`, and the
  holder's cwd for a suite invoked as `./run.sh` from the suite home): a dead
  pid, or a number now worn by something that is not a journey suite, is a
  stale record and is reaped, while a genuine holder is still refused. An
  owner that cannot be identified at all - an unreadable or not-yet-written
  record, the window between `mkdir` and the pid write - is HELD: refusing a
  suite is a nuisance, reaping a live suite's lock lets two suites interleave,
  which is the failure the lock exists to prevent. The recycled-pid wedge hit
  live on 2026-11-17 was the opposite shape (every later suite refused while
  none was running). `JOURNEY_LOCK_IMPL` (`auto`/`flock`/`mkdir`) pins the
  implementation, so the witness exercises the MSYS shape on a host that does
  have flock. `bash scripts/journeys/runlock_test.sh` is its witness.
- A worker verify that may overlap the train pass or the nightly runs
  `JOURNEY_LOCK_WAIT=600 scripts/journeys/run.sh ...` and queues behind
  the live suite instead of colliding with it.
- `scripts/journeys/sweep-chromes.sh` honors the lock: called standalone
  while a suite is live it skips (exit 0); called from the suite (inherited
  fd 9) it proceeds.
- On Windows/MSYS there is no `/proc` view of native processes, so
  `sweep-chromes.sh` hands the reap to `scripts/journeys/sweep-chromes.ps1`.
  That half needs no lock: it only kills a chrome whose parent is dead or
  whose parent pid was recycled, and no live suite owns such a chrome.
  `bash scripts/journeys/sweep_chromes_win_test.sh` is its witness.
- `scripts/journeys/sweep-daemons.sh` closes IDLE agent-browser daemons.
  A daemon outlives its client by design and holds the chrome it launched,
  so the chrome reaper above must never touch that pair - the chrome's parent
  is alive. `run.sh` calls the daemon sweep at the RELEASE GATE: after the
  last recipe, while it still holds the run lock, so no other suite can be
  between recipes. A session is closed only when all three verifiable facts
  hold: the run lock is the caller's (`JOURNEY_GATE_PID` matches the recorded
  holder), no client holds an ESTABLISHED connection on the session's control
  port, and the daemon has been up for `JOURNEY_DAEMON_IDLE_MIN` (default 10)
  minutes - the gate's own `$JOURNEY_SESSION` is exempt from the age rule
  because the suite body has provably ended. Standalone (cron, pre-merge) it
  takes the lock itself and stands down while a suite holds it.
  `bash scripts/journeys/sweep_daemons_test.sh` is its witness.

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

## Capacity per profile (2+1)

`kanban.max_in_progress_per_profile` is 2 with the 2+1 capacity ruling
(2026-11-17 evening): 2 cody implementation seats + 1 vera review lane = 3
concurrent runs max, enforced by the dispatch guard - the per-profile
config knob alone cannot express the 2+1 asymmetry, so the invariant lives
guard-side. The guard clamps the implementation seats to 2 whatever the
knob says (a knob of 3 or 4 cannot buy a third implementation run; the
clamp is loud in the `seats:` line and, on a `--max` pass, in a `clamp:`
line naming the seats arithmetic), and a third legal candidate that finds
no seat is refused by name in the `impl lane over-cap:` readout.

History, because the number moved twice: the knob went 2 -> 3 with the
3+1 ruling (2026-11-15, guard card `t_ce6eea14`, merged `60cb462a`; the
gates for that flip were two parallel webui cards landing back-to-back
clean via the embed lane plus the worker onboarding skill going live).
It came back to 2 with the 2+1 narrowing after three persist-seam waves
in one evening (20:46, 21:11, 21:46 - the third with ZERO tick-side
load) proved that three concurrent workers running gate suites stall the
gateway's worker.persist receipts. Serializing one seat trades an
implementation seat for a seam that never sees its failure shape. The
`trio:` verdict the 3+1 guard carried is gone with the third seat: a
would-be trio is an over-cap shape now, and the head set is a pair at
most (`scripts/factory-dispatch.sh`, guard card `t_4707b735`). Pairs stay
legal on the same disjointness proof; the review lane's cap of 1 is
untouched.

The dispatch guard (`scripts/factory-dispatch.sh`, `config_cap`) treats
the knob as a guard number, never as a hint: a pass whose cap cannot be
read REFUSES to dispatch (one `CONFIG CAP READ FAILED:` line naming the
resolved root and the file it tried, exit 3), and every pass that reads
it prints `config: cap N from <file>` - with the seats line naming the
SOURCE (`config cap N` vs `--max N`, since `--max` overrides for the
pass). A sandbox or CI home that carries no hermes config at all opts
into the documented floor of 2 with `FD_ALLOW_MISSING_CONFIG=1`, loudly;
that hook covers an ABSENT file only, so a present config with a missing
or non-positive knob still refuses.

The guard also holds new work on the dev tip's CI verdict. Before anything
spawns it reads the tip's `Journeys regression` job (`gh run list` for that
sha, then that job's own status) and refuses the pass with one `tip CI: HOLD`
line while the verdict is red or in flight - the merge-before-dispatch law
gaining a leg, so local-green landings never pile onto a tip whose browser
gate is unproven. Green, or absent (no run for that sha: a docs-only push),
proceeds; an unreadable verdict (no `gh`, no auth, no git) also proceeds,
loudly - a missing tool must not freeze the factory. `FD_GH` names the gh
binary the check reads through (the suite's seam).

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

The newest nightly tag AHEAD of master (master is an ancestor of its
commit), older than 48h, fast-forwards master to itself iff ALL of:

- the point is ahead of master (the pick step filters on this: a
  nightly behind master can never ff, it can only red its soak job),
- the soak point is its own green receipt: a nightly tag exists only
  because the nightly suite ran green on it (red or partial = no tag),
  so the soak gate never re-queries CI runs for the point,
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

| Piece                           | Job                                                                         |
| ------------------------------- | --------------------------------------------------------------------------- |
| `docs/release-gate.md`          | the gate's operating law: round cap, noise tags, lens brief, regression set |
| `scripts/embed-lane.sh`         | the ONE owner of committed `api/static/`                                    |
| `scripts/train-pass.sh`         | one mechanical landing PASS (the batch) + marker refs + strikes + bisect attribution + the `--gate-only` probe seam |
| `scripts/factory-merge.sh`      | the review seat's merge entry: train pinned to one APPROVED card, then prune |
| `scripts/train-bisect.sh`       | the marker-chain walk: --list, or bisect by check (--chain for a batch's merges) |
| `scripts/review-at-close.sh`    | fresh review worktree + cold diff vs merge-base                             |
| `scripts/train-dry-run.sh`      | stale wt/\* sweep preview (stage 1, report/apply)                           |
| `scripts/drift-gate.sh`         | hard-fail drift check, dev vs master (10 commits / 7 days)                  |
| `scripts/release-suggest.sh`    | suggested bump + evidence commits, never tags                               |
| `.github/workflows/nightly.yml` | nightly prerelease tag on green dev tip + the soak ff                       |

Journeys (run 3b) wire their suite in as `scripts/journeys/run.sh`;
train-pass already calls that path when it exists.
