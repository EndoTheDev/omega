# Validation journeys

Rung 2 of the detection ladder. Go gates catch code-level breaks; these
catch the invisible-to-gates family - correct data behind a lying
presentation (the auto-focus that shipped 100% dead, the poisoned
bundle, the NUXT ReferenceError). A recipe opens the SPA in a real
browser, acts like a customer, asserts the user-visible outcome.

Run them against a live lane. `run.sh` is the only entry point; its
header carries the full usage, the two lanes (Pi per-landing, CI
nightly) and the chrome/daemon/lock contracts.

```bash
scripts/journeys/run.sh                       # full suite vs $JOURNEY_BASE_URL
scripts/journeys/run.sh --base http://127.0.0.1:8099
scripts/journeys/run.sh 84-terminal-divider-tracks
scripts/journeys/run.sh --regression          # the named regression set
JOURNEY_LOCK_WAIT=600 scripts/journeys/run.sh # queue behind a live suite
```

Exit codes: `0` all green, `1` at least one journey red, `2` lane or
browser infra dead, `3` another suite holds the run lock.

Environment: `JOURNEY_BASE_URL`, `JOURNEY_SESSION` (browser session
name - distinct per run), `JOURNEY_CHROME_PORT` (a NAMED port is adopted
- a pre-launched browser, a pinned lane; unset means the run takes a
free port of its own, so a sibling run's chrome is never driven),
`JOURNEY_TIMEOUT` (wall cap per recipe), `JOURNEY_JSON=1` (machine
summary on the last line), `JOURNEY_LOCK_WAIT` (seconds to queue
instead of refusing), `JOURNEY_LOCK_IMPL` (`auto`|`flock`|`mkdir` - pin
the run-lock implementation; `auto` uses flock where the host has it).

## Layout

| Path | What it is |
| ---- | ---------- |
| `run.sh` | suite driver: lock, browser attach, recipe fork, watchdog, release gate |
| `lib.sh` | helper library a recipe sources: `ab`, `js`/`jsq`, `wait_js`, `poll_js`, `assert_eq`/`assert_ne`, `assert_no_console_errors`, `die`, the residue cleanup traps |
| `recipes/` | one file per customer-level contract |
| `regression.list` | the named regression set (`--regression`); adding an entry is how an incident becomes a permanent eval |
| `sweep-chromes.sh`, `sweep-chromes.ps1`, `sweep-daemons.sh` | orphan reaping; see the run-lock and daemon-sweep law in `docs/pipeline.md` |

## Recipes

A recipe is bash: source `lib.sh`, `open_app`, drive the page with `js`
/ `jsq` / `ab`, assert with `assert_eq` / `assert_ne` / `wait_js`, and
`die` on red - `die` captures a screenshot and reaps the run's residue.
File names sort the suite: `NN-slug.sh` or `NN-slug.journey.sh`, both
matched. A recipe keeps nothing behind (sessions `jrn-*`, board tasks,
roster agent homes and the terminal sessions its page loads claim are
reaped at every exit path). The residue path runs at the recipe's return
and again from the subshell's `EXIT` trap, so a recipe that installs its
own `EXIT` trap still gets it - and that trap runs AFTER the residue
path, which means it must not depend on the page (the terminal reap
releases it).

Browser-level witnesses exist because a shape is not visible to Go
tests. Where a UI geometry or gesture contract has both halves, the
recipe is the browser half of a pin whose other half lives in
`webui/test/` or a Go test - keep the pair in step.

### The determinism contract (card t_fdd12843)

A recipe asserts on STATE, never on elapsed time. Four gate reds in
three nights were one root class: a press read mid-unfold
(t_44f55a54), an h+4 clamp offset (113-settings-nav), a poller counted
inside a fixed 17s window (111-socket-push), a browser-state leak
(95-terminal). A sleep between an action and a read is a guess about
how fast the machine is - right in isolation, wrong in a loaded suite.

Three wait shapes, and nothing else:

| Shape | For | Primitive |
| ----- | --- | --------- |
| event | a predicate that becomes true (a drawer opened, a row arrived, a chip flipped, the boot reads resolved) | `wait_js` / `poll_js` / `wait_booted` |
| settle | a VALUE that stops changing (geometry, computed style, a pane chasing a drag) | `wait_settled` / `wait_layout` |
| absence | a predicate that must stay false for a cadence's worth of time (the only claim that needs a duration) | `watch_absent` |

Two more laws ride along: a write is graded by polling the SURFACE that
owns it (`wait_server_col` shape - never "sleep a second, then GET"),
and every journey leaves a clean room (`open_app` clears the client
state a previous recipe left on its first call; `journey_exit_cleanup`
resets panes, viewport and storage on every exit path).

`scripts/recipe-lint.sh` holds the contract over `recipes/*.sh`. It is
grep-class (awk), reports one line per finding, and is advisory-capable
(`--advisory` never exits 1). `scripts/recipe-lint_test.sh` pins its
negative branches and the whole corpus; CI runs it beside the other
script suites.

A wait that genuinely cannot be event-based is justified IN PLACE, and
the justification is part of the evidence log:

```bash
sleep 0.15   # recipe-lint: allow fixed-sleep - input pacing: the real
             # pointer pipeline coalesces a move and a press in one task
```

The marker is rule-scoped on purpose (`allow <rule> - <reason>`): a
blanket allow is a suppression by another name. A marker with no reason
(`bare-justification`) and a marker whose sleep has gone (`stale-allow`)
are both findings.

The fourth rule is the poll-bound ratchet: a bounded for-loop is an
iteration count, so a wait built from one (`for i in 1..10; do
out=$(jsq ...); sleep 1; done`) is a timeout in disguise - green solo,
red on a loaded host. A JS read inside a loop routes through `poll_js` /
`wait_js` / `poll_js_value`, whose budget is `journey_budget`'s floor.
The debt ledger `journeys/recipe-lint.legacy` is EMPTY: every recipe that
carried the shape when the rule landed has been converted. The file stays
(header, no entries) because its count is what the lint reads - a listed
recipe may not gain a site, and an entry that outlives its sites is
`ledger-rot` - so a new line there is a review finding, not a way to land
a bounded loop; convert the wait instead. With no entry left the test's
rot case has nothing to outlive and reports itself unprovable, while the
growth case is proven against exactly this state (any site in an unlisted
recipe reds at its first one).

### The agent-browser version these recipes are written against

**0.42.0.** The CLI is pinned in two places that move together: the
`JOURNEY_AB_VERSION` block in `run.sh` and the `npm i -g
agent-browser@0.42.0` line in `.github/workflows/nightly.yml`.

Why the pin exists: the gesture-class recipes (84 divider drag, 98
board drag) drive real pointer input - mouse down/move/up through the
CLI's own pipeline - and nothing a recipe does through page eval can
stand in for that. A floating CLI therefore silently changes what the
gate measures. On 2026-11-16 the runner's unpinned install moved to
**0.43.0** and the gesture reds came back identical twice
(nightlies 43795957657, 43797271512):

- 84: `right divider inward: got [220] want [160] | gesture-active=0`
  - the drag never engages at all, so no divider movement is measured.
- 98: `mid-drag order for triage never matched in 6s | dragging=[]` -
  the board never sees a drag start.
- 113 undershot one scroll step in the same runs (`scrollTop: 810, want
  814`). It is the least gesture-shaped of the three, so if it survives
  the pin it is a product defect (a real geometry shift) to card on its
  own with the run log - never a recipe to soften.

Same tree, same recipes, 0.42.0 (run 43210278632, 2026-11-12): green.

So: **0.43.0 broke this suite's gesture path.** Do not adapt a recipe to
make it pass on 0.43.0 - that is a separate decision, and on this
evidence a gesture red there is environment, not product. `run.sh` warns
loudly when the installed version differs from the pin, and refuses to
run (exit 2) when `JOURNEY_AB_REQUIRE_VERSION=1` - which the nightly
sets, so the gate cannot drift back to a float unnoticed. A non-CI host
may legitimately differ (the Pi lane carries 0.26.0) and takes the
warning path.

## Related docs

- `docs/pipeline.md` - run lock, chrome reuse, daemon release gate
- `docs/release-gate.md` - incidents become evals, the regression set
- `docs/webui.md` - the geometry contracts the recipes witness
