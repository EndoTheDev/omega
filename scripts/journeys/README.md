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
name - distinct per concurrent worker), `JOURNEY_TIMEOUT` (wall cap per
recipe), `JOURNEY_JSON=1` (machine summary on the last line),
`JOURNEY_LOCK_WAIT` (seconds to queue instead of refusing).

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
roster agent homes are reaped at every exit path).

Browser-level witnesses exist because a shape is not visible to Go
tests. Where a UI geometry or gesture contract has both halves, the
recipe is the browser half of a pin whose other half lives in
`webui/test/` or a Go test - keep the pair in step.

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
